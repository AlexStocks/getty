/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package getty

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

import (
	gxbytes "github.com/dubbogo/gost/bytes"
	gxnet "github.com/dubbogo/gost/net"
	gxsync "github.com/dubbogo/gost/sync"
	gxtime "github.com/dubbogo/gost/time"

	"github.com/gorilla/websocket"

	perrors "github.com/pkg/errors"
)

import (
	log "github.com/AlexStocks/getty/util"
)

const (
	defaultReconnectInterval    = 3e8 // 300ms
	connectInterval             = 5e8 // 500ms
	connectTimeout              = 3e9
	defaultMaxReconnectAttempts = 50
	maxBackOffTimes             = 10
)

var (
	sessionClientKey   = "session-client-owner"
	connectPingPackage = []byte("connect-ping")

	clientID           = EndPointID(0)
	ignoreReconnectKey = "ignore-reconnect"
)

type Client interface {
	EndPoint
}

type client struct {
	ClientOptions

	// endpoint ID
	endPointID EndPointID

	// net
	sync.Mutex
	endPointType EndPointType

	newSession NewSessionCallback
	ssMap      map[Session]struct{}

	sync.Once
	done chan struct{}
	wg   sync.WaitGroup
}

func (c *client) init(opts ...ClientOption) {
	for _, opt := range opts {
		opt(&(c.ClientOptions))
	}
}

func newClient(t EndPointType, opts ...ClientOption) *client {
	c := &client{
		endPointID:   atomic.AddInt32(&clientID, 1),
		endPointType: t,
		done:         make(chan struct{}),
	}

	c.init(opts...)

	if c.number <= 0 || c.addr == "" {
		panic(fmt.Sprintf("client type:%s, @connNum:%d, @serverAddr:%s", t, c.number, c.addr))
	}

	c.ssMap = make(map[Session]struct{}, c.number)

	return c
}

// NewTCPClient builds a tcp client.
func NewTCPClient(opts ...ClientOption) Client {
	return newClient(TCP_CLIENT, opts...)
}

// NewUDPClient builds a connected udp client
func NewUDPClient(opts ...ClientOption) Client {
	return newClient(UDP_CLIENT, opts...)
}

// NewWSClient builds a ws client.
func NewWSClient(opts ...ClientOption) Client {
	c := newClient(WS_CLIENT, opts...)

	if !strings.HasPrefix(c.addr, "ws://") {
		panic(fmt.Sprintf("the prefix @serverAddr:%s is not ws://", c.addr))
	}

	return c
}

// NewWSSClient function builds a wss client.
func NewWSSClient(opts ...ClientOption) Client {
	c := newClient(WSS_CLIENT, opts...)

	if c.cert == "" {
		panic(fmt.Sprintf("@cert:%s", c.cert))
	}
	if !strings.HasPrefix(c.addr, "wss://") {
		panic(fmt.Sprintf("the prefix @serverAddr:%s is not wss://", c.addr))
	}

	return c
}

func (c *client) ID() EndPointID {
	return c.endPointID
}

func (c *client) EndPointType() EndPointType {
	return c.endPointType
}

func (c *client) dialTCP() Session {
	var (
		err  error
		conn net.Conn
	)

	// #106: this function performs a SINGLE dial attempt and returns nil on
	// failure. The bounded retry/back-off is owned by reConnect() (which
	// honors maxReconnectAttempts). Previously dialTCP had an unbounded
	// for{} loop here that never returned on failure, making
	// WithReconnectAttempts a no-op and hanging clients/tests forever when
	// the target was unreachable.
	if c.IsClosed() {
		return nil
	}
	if c.sslEnabled {
		// #101: guard against a TLS config builder that returns (nil, nil);
		// previously a nil config with nil err fell through and the nil conn
		// was dereferenced below (conn.RemoteAddr) -> panic.
		sslConfig, buildTlsConfErr := c.tlsConfigBuilder.BuildTlsConfig()
		if buildTlsConfErr != nil {
			err = buildTlsConfErr
		} else if sslConfig == nil {
			err = fmt.Errorf("tlsConfigBuilder returned nil config without error")
		} else {
			d := &net.Dialer{Timeout: connectTimeout}
			conn, err = tls.DialWithDialer(d, "tcp", c.addr, sslConfig)
		}
	} else {
		conn, err = net.DialTimeout("tcp", c.addr, connectTimeout)
	}
	if err == nil && gxnet.IsSameAddr(conn.RemoteAddr(), conn.LocalAddr()) {
		_ = conn.Close()
		err = errSelfConnect
	}
	if err == nil {
		return newTCPSession(conn, c)
	}

	log.Infof("net.DialTimeout(addr:%s, timeout:%v) = error:%+v", c.addr, connectTimeout, perrors.WithStack(err))
	return nil
}

func (c *client) dialUDP() Session {
	var (
		err       error
		conn      *net.UDPConn
		localAddr *net.UDPAddr
		peerAddr  *net.UDPAddr
		length    int
		bufp      *[]byte
		buf       []byte
	)

	// #106: single attempt; reConnect() owns bounded retry/back-off.
	if c.IsClosed() {
		return nil
	}
	bufp = gxbytes.GetBytes(128)
	defer gxbytes.PutBytes(bufp)
	buf = *bufp
	localAddr = &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	peerAddr, _ = net.ResolveUDPAddr("udp", c.addr)
	conn, err = net.DialUDP("udp", localAddr, peerAddr)
	if err == nil && gxnet.IsSameAddr(conn.RemoteAddr(), conn.LocalAddr()) {
		_ = conn.Close()
		err = errSelfConnect
	}
	if err != nil {
		// #104: the format string referenced a timeout verb but no argument
		// was supplied; UDP dial has no timeout, so drop the verb.
		log.Warnf("net.DialUDP(addr:%s) = error:%+v", c.addr, perrors.WithStack(err))
		return nil
	}

	// check connection alive by write/read action
	if err := conn.SetWriteDeadline(time.Now().Add(1e9)); err != nil {
		log.Warnf("failed to set write deadline: %+v", err)
	}
	if length, err = conn.Write(connectPingPackage[:]); err != nil {
		_ = conn.Close()
		log.Warnf("conn.Write(%s) = {length:%d, err:%+v}", string(connectPingPackage), length, perrors.WithStack(err))
		return nil
	}
	if err := conn.SetReadDeadline(time.Now().Add(1e9)); err != nil {
		log.Warnf("failed to set read deadline: %+v", err)
	}
	length, err = conn.Read(buf)
	if netErr, ok := perrors.Cause(err).(net.Error); ok && netErr.Timeout() {
		err = nil
	}
	if err != nil {
		log.Infof("conn{%#v}.Read() = {length:%d, err:%+v}", conn, length, perrors.WithStack(err))
		_ = conn.Close()
		return nil
	}
	return newUDPSession(conn, c)
}

func (c *client) dialWS() Session {
	var (
		err    error
		dialer websocket.Dialer
		conn   *websocket.Conn
		ss     Session
	)

	// #106: single attempt; reConnect() owns bounded retry/back-off.
	if c.IsClosed() {
		return nil
	}
	dialer.EnableCompression = true
	conn, _, err = dialer.Dial(c.addr, nil)
	if err == nil && gxnet.IsSameAddr(conn.RemoteAddr(), conn.LocalAddr()) {
		_ = conn.Close()
		err = errSelfConnect
	}
	if err == nil {
		ss = newWSSession(conn, c)
		if ss.(*session).maxMsgLen > 0 {
			conn.SetReadLimit(int64(ss.(*session).maxMsgLen))
		}

		return ss
	}

	log.Infof("websocket.dialer.Dial(addr:%s) = error:%+v", c.addr, perrors.WithStack(err))
	return nil
}

func (c *client) buildWSSClientTLSConfig() (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.cert == "" {
		return config, nil
	}

	certPEM, err := os.ReadFile(c.cert)
	if err != nil {
		return nil, perrors.Wrapf(err, "os.ReadFile(cert:%s)", c.cert)
	}
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(certPEM) {
		return nil, fmt.Errorf("failed to parse root certificate: %s", c.cert)
	}
	config.RootCAs = certPool
	return config, nil
}

func (c *client) dialWSS() Session {
	var (
		err    error
		config *tls.Config
		dialer websocket.Dialer
		conn   *websocket.Conn
		ss     Session
	)

	// #106: single attempt; reConnect() owns bounded retry/back-off.
	if c.IsClosed() {
		return nil
	}
	dialer.EnableCompression = true

	config, err = c.buildWSSClientTLSConfig()
	if err != nil {
		log.Errorf("build WSS client TLS config = error:%+v", perrors.WithStack(err))
		return nil
	}

	dialer.TLSClientConfig = config
	conn, _, err = dialer.Dial(c.addr, nil)
	if err == nil && gxnet.IsSameAddr(conn.RemoteAddr(), conn.LocalAddr()) {
		_ = conn.Close()
		err = errSelfConnect
	}
	if err == nil {
		ss = newWSSession(conn, c)
		if ss.(*session).maxMsgLen > 0 {
			conn.SetReadLimit(int64(ss.(*session).maxMsgLen))
		}
		ss.SetName(defaultWSSSessionName)

		return ss
	}

	log.Infof("websocket.dialer.Dial(addr:%s) = error:%+v", c.addr, perrors.WithStack(err))
	return nil
}

func (c *client) dial() Session {
	switch c.endPointType {
	case TCP_CLIENT:
		return c.dialTCP()
	case UDP_CLIENT:
		return c.dialUDP()
	case WS_CLIENT:
		return c.dialWS()
	case WSS_CLIENT:
		return c.dialWSS()
	}

	return nil
}

func (c *client) GetTaskPool() gxsync.GenericTaskPool {
	return c.tPool
}

func (c *client) sessionNum() int {
	var num int

	c.Lock()
	for s := range c.ssMap {
		if s.IsClosed() {
			delete(c.ssMap, s)
		}
	}
	num = len(c.ssMap)
	c.Unlock()

	return num
}

func (c *client) connect() bool {
	var (
		err error
		ss  Session
	)

	ss = c.dial()
	if ss == nil {
		// client has been closed
		return false
	}
	err = c.newSession(ss)
	if err == nil {
		// Set the reconnect attributes before run(): session.stop() decides
		// whether to reconnect from these attributes, so a connection that
		// dies right after run() must already carry them, otherwise the
		// reconnect is silently skipped and the pool stays short by one
		// (AlexStocks/getty issue #121 defect 3).
		ss.SetAttribute(sessionClientKey, c)
		ss.SetAttribute(ignoreReconnectKey, false)
		ss.(*session).run()
		c.Lock()
		if c.ssMap == nil {
			// the pool is gone (client.stop() already ran): drop the reconnect
			// attributes like client.stop() does, so closing this session does
			// not trigger a reconnect, then close it to avoid leaking the
			// already-started session (AlexStocks/getty issue #121 defect 1).
			ss.RemoveAttribute(sessionClientKey)
			ss.RemoveAttribute(ignoreReconnectKey)
			c.Unlock()
			ss.Close()
			return false
		}
		if ss.IsClosed() {
			// Closed during run() (e.g. OnOpen called Close): its stop() has
			// already triggered a reconnect pass, so keep the dead session out
			// of the pool and report the connect as failed.
			ss.RemoveAttribute(sessionClientKey)
			ss.RemoveAttribute(ignoreReconnectKey)
			c.Unlock()
			return false
		}
		c.ssMap[ss] = struct{}{}
		c.Unlock()
		return true
	}
	// don't distinguish between tcp connection and websocket connection. Because
	// gorilla/websocket/conn.go:(Conn)Close also invoke net.Conn.Close()
	if cerr := ss.Conn().Close(); cerr != nil {
		log.Warnf("failed to close conn: %+v", cerr)
	}
	return false
}

// there are two methods to keep connection pool. the first approach is like
// redigo's lazy connection pool(https://github.com/gomodule/redigo/blob/master/redis/pool.go:),
// in which you should apply testOnBorrow to check alive of the connection.
// the second way is as follows. @RunEventLoop detects the aliveness of the connection
// in regular time interval.
// the active method maybe overburden the cpu slightly.
// however, you can get a active tcp connection very quickly.
func (c *client) RunEventLoop(newSession NewSessionCallback) {
	c.Lock()
	c.newSession = newSession
	c.Unlock()
	<-c.runReconnect()
}

// runReconnect starts a reconnect loop only while the client is open. The
// client lock serializes WaitGroup.Add with stop, which prevents Add racing
// with Close's Wait.
func (c *client) runReconnect() <-chan struct{} {
	done := make(chan struct{})
	c.Lock()
	select {
	case <-c.done:
		c.Unlock()
		close(done)
		return done
	default:
		c.wg.Add(1)
	}
	c.Unlock()

	go func() {
		defer c.wg.Done()
		defer close(done)
		c.reConnect()
	}()
	return done
}

// a for-loop connect to make sure the connection pool is valid
func (c *client) reConnect() {
	var reconnectAttempts int
	reconnectInterval := c.reconnectInterval
	if reconnectInterval == 0 {
		reconnectInterval = defaultReconnectInterval
	}
	maxReconnectAttempts := c.maxReconnectAttempts
	if maxReconnectAttempts == 0 {
		maxReconnectAttempts = defaultMaxReconnectAttempts
	}
	connPoolSize := c.number
	for reconnectAttempts < maxReconnectAttempts {
		if c.IsClosed() {
			log.Warnf("client{peer:%s} goroutine exit now.", c.addr)
			return
		}

		if connPoolSize <= c.sessionNum() {
			return
		}
		if c.connect() {
			reconnectAttempts = 0
			continue
		}
		reconnectAttempts++
		if c.IsClosed() || connPoolSize <= c.sessionNum() || reconnectAttempts >= maxReconnectAttempts {
			return
		}

		backOffTimes := reconnectAttempts
		if maxBackOffTimes < backOffTimes {
			backOffTimes = maxBackOffTimes
		}
		backoff := time.Duration(int64(backOffTimes) * int64(reconnectInterval))
		select {
		case <-c.done:
			return
		case <-gxtime.After(backoff):
		}
	}
}

func (c *client) stop() {
	c.Do(func() {
		c.Lock()
		close(c.done)
		sessions := c.ssMap
		c.ssMap = nil
		c.Unlock()

		for s := range sessions {
			s.RemoveAttribute(sessionClientKey)
			s.RemoveAttribute(ignoreReconnectKey)
			s.Close()
		}
	})
}

func (c *client) IsClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *client) Close() {
	c.stop()
	c.wg.Wait()
}
