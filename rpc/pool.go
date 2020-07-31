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

package rpc

import (
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

import (
	"github.com/AlexStocks/getty/transport"
)

type gettyRPCClient struct {
	once     sync.Once
	protocol string
	addr     string
	active   int64 // 为0，则说明没有被创建或者被销毁了

	pool *gettyRPCClientPool

	lock        sync.RWMutex
	gettyClient getty.Client
	sessions    []*rpcSession
}

var (
	errClientPoolClosed = jerrors.New("client pool closed")
)

func newGettyRPCClient(pool *gettyRPCClientPool, protocol, addr string) (*gettyRPCClient, error) {
	c := &gettyRPCClient{
		protocol: protocol,
		addr:     addr,
		pool:     pool,
		gettyClient: getty.NewTCPClient(
			getty.WithServerAddress(addr),
			getty.WithConnectionNumber(pool.rpcClient.conf.ConnectionNum),
		),
	}
	go c.gettyClient.RunEventLoop(c.newSession)
	idx := 1
	for {
		idx++
		if c.isAvailable() {
			break
		}

		if idx > 2000 {
			c.gettyClient.Close()
			return nil, jerrors.New(fmt.Sprintf("failed to create client connection to %s in 3 seconds", addr))
		}
		time.Sleep(1e6)
	}
	log.Info("client init ok")
	c.updateActive(time.Now().Unix())

	return c, nil
}

func (c *gettyRPCClient) updateActive(active int64) {
	atomic.StoreInt64(&c.active, active)
}

func (c *gettyRPCClient) getActive() int64 {
	return atomic.LoadInt64(&c.active)
}

func (c *gettyRPCClient) newSession(session getty.Session) error {
	var (
		ok      bool
		tcpConn *net.TCPConn
		conf    ClientConfig
	)

	conf = c.pool.rpcClient.conf
	if conf.GettySessionParam.CompressEncoding {
		session.SetCompressType(getty.CompressZip)
	}

	if tcpConn, ok = session.Conn().(*net.TCPConn); !ok {
		panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp connection\n", session.Stat(), session.Conn()))
	}

	tcpConn.SetNoDelay(conf.GettySessionParam.TcpNoDelay)
	tcpConn.SetKeepAlive(conf.GettySessionParam.TcpKeepAlive)
	if conf.GettySessionParam.TcpKeepAlive {
		tcpConn.SetKeepAlivePeriod(conf.GettySessionParam.keepAlivePeriod)
	}
	tcpConn.SetReadBuffer(conf.GettySessionParam.TcpRBufSize)
	tcpConn.SetWriteBuffer(conf.GettySessionParam.TcpWBufSize)

	session.SetName(conf.GettySessionParam.SessionName)
	session.SetMaxMsgLen(conf.GettySessionParam.MaxMsgLen)
	session.SetPkgHandler(rpcClientPackageHandler)
	session.SetEventListener(NewRpcClientHandler(c))
	// session.SetRQLen(conf.GettySessionParam.PkgRQSize)
	session.SetWQLen(conf.GettySessionParam.PkgWQSize)
	session.SetReadTimeout(conf.GettySessionParam.tcpReadTimeout)
	session.SetWriteTimeout(conf.GettySessionParam.tcpWriteTimeout)
	session.SetCronPeriod((int)(conf.heartbeatPeriod.Nanoseconds() / 1e6))
	session.SetWaitTime(conf.GettySessionParam.waitTimeout)
	log.Debug("client new session:%s\n", session.Stat())

	return nil
}

func (c *gettyRPCClient) selectSession() getty.Session {
	c.lock.RLock()
	defer c.lock.RUnlock()

	count := len(c.sessions)
	if count == 0 {
		return nil
	}
	return c.sessions[rand.Int31n(int32(count))].session
}

func (c *gettyRPCClient) addSession(session getty.Session) {
	log.Debug("add session{%s}", session.Stat())
	if session == nil {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.sessions = append(c.sessions, &rpcSession{session: session})
}

func (c *gettyRPCClient) removeSession(session getty.Session) {
	if session == nil {
		return
	}

	var removeFlag bool
	func() {
		c.lock.Lock()
		defer c.lock.Unlock()
		if len(c.sessions) == 0 {
			return
		}

		for i, s := range c.sessions {
			if s.session == session {
				c.sessions = append(c.sessions[:i], c.sessions[i+1:]...)
				log.Debug("delete session{%s}, its index{%d}", session.Stat(), i)
				break
			}
		}
		log.Info("after remove session{%s}, left session number:%d", session.Stat(), len(c.sessions))
		if len(c.sessions) == 0 {
			removeFlag = true
		}
	}()
	if removeFlag {
		c.close() // -> pool.remove(c)
	}
}

func (c *gettyRPCClient) updateSession(session getty.Session) {
	if session == nil {
		return
	}

	var rs *rpcSession
	func() {
		c.lock.RLock()
		defer c.lock.RUnlock()
		if c.sessions == nil {
			return
		}

		for i, s := range c.sessions {
			if s.session == session {
				rs = c.sessions[i]
				break
			}
		}
	}()
	if rs != nil {
		rs.AddReqNum(1)
	}
}

func (c *gettyRPCClient) getClientRpcSession(session getty.Session) (rpcSession, error) {
	var (
		err        error
		rpcSession rpcSession
	)
	c.lock.Lock()
	defer c.lock.Unlock()
	if len(c.sessions) == 0 {
		return rpcSession, errClientClosed
	}

	err = errSessionNotExist
	for _, s := range c.sessions {
		if s.session == session {
			rpcSession = *s
			err = nil
			break
		}
	}

	return rpcSession, jerrors.Trace(err)
}

func (c *gettyRPCClient) isAvailable() bool {
	if c.selectSession() == nil {
		return false
	}

	return true
}

func (c *gettyRPCClient) close() error {
	err := jerrors.Errorf("close gettyRPCClient{%#v} again", c)
	c.once.Do(func() {
		// delete @c from client pool
		c.pool.remove(c)

		var (
			gettyClient getty.Client
			sessions    []*rpcSession
		)
		func() {
			c.lock.Lock()
			defer c.lock.Unlock()

			gettyClient = c.gettyClient
			c.gettyClient = nil

			sessions = make([]*rpcSession, 0, len(c.sessions))
			for _, s := range c.sessions {
				sessions = append(sessions, s)
			}
			c.sessions = c.sessions[:0]
		}()

		c.updateActive(0)

		go func() {
			if gettyClient != nil {
				gettyClient.Close()
			}
			for _, s := range sessions {
				log.Info("close client session{%s, last active:%s, request number:%d}",
					s.session.Stat(), s.session.GetActive().String(), s.GetReqNum())
				s.session.Close()
			}
		}()

		err = nil
	})

	return err
}

type rpcClientArray struct {
	lock  sync.RWMutex
	array []*gettyRPCClient
}

func newRpcClientArray() *rpcClientArray {
	return &rpcClientArray{
		array: make([]*gettyRPCClient, 0, 8),
	}
}

func (a *rpcClientArray) Size() int {
	a.lock.RLock()
	defer a.lock.RUnlock()

	return len(a.array)
}

func (a *rpcClientArray) Put(clt *gettyRPCClient) {
	a.lock.Lock()
	defer a.lock.Unlock()

	for i := range a.array {
		if a.array[i] == clt {
			return
		}
	}

	a.array = append(a.array, clt)
}

func (a *rpcClientArray) Get(key string, pool *gettyRPCClientPool) *gettyRPCClient {
	now := time.Now().Unix()

	var array []*gettyRPCClient
	defer func() {
		go func() {
			if len(array) != 0 {
				for i := range array {
					array[i].close() // -> pool.remove(c)
				}
			}
		}()
	}()

	a.lock.Lock()
	defer a.lock.Unlock()

	for len(a.array) > 0 {
		conn := a.array[len(a.array)-1]
		a.array = a.array[:len(a.array)-1]
		pool.connMap.Store(key, a)

		if d := now - conn.getActive(); d > pool.ttl {
			array = append(array, conn)
			continue
		}

		conn.updateActive(now)
		return conn
	}

	return nil
}

func (a *rpcClientArray) Remove(key string, conn *gettyRPCClient, p *gettyRPCClientPool) {
	if a.Size() <= 0 {
		return
	}
	a.lock.Lock()
	defer a.lock.Unlock()

	for idx, c := range a.array {
		if conn == c {
			a.array = append(a.array[:idx], a.array[idx+1:]...)
			p.connMap.Store(key, a)
			break
		}
	}
}

func (a *rpcClientArray) Close() {
	var array []*gettyRPCClient
	func() {
		a.lock.Lock()
		defer a.lock.Unlock()

		array = make([]*gettyRPCClient, 0, len(a.array))
		for i := range a.array {
			array = append(array, a.array[i])
		}

		a.array = a.array[:0]
	}()
	for i := range array {
		array[i].close()
	}
}

type gettyRPCClientPool struct {
	rpcClient *Client
	size      int   // []*gettyRPCClient数组的size
	ttl       int64 // 每个gettyRPCClient的有效期时间. pool对象会在getConn时执行ttl检查

	connMap RPCClientMap // 从[]*gettyRPCClient 可见key是连接地址，而value是对应这个地址的连接数组
}

func newGettyRPCClientConnPool(rpcClient *Client, size int, ttl time.Duration) *gettyRPCClientPool {
	return &gettyRPCClientPool{
		rpcClient: rpcClient,
		size:      size,
		ttl:       int64(ttl.Seconds()),
	}
}

func (p *gettyRPCClientPool) close() {
	p.connMap.Range(func(key string, connArray *rpcClientArray) bool {
		connArray.Close()
		return true
	})
}

func (p *gettyRPCClientPool) get(protocol, addr string) (*gettyRPCClient, error) {
	var builder strings.Builder

	builder.WriteString(addr)
	builder.WriteString("@")
	builder.WriteString(protocol)

	key := builder.String()
	connArray, ok := p.connMap.Load(key)
	if ok {
		clt := connArray.Get(key, p)
		if clt != nil {
			return clt, nil
		}
	}

	// create new conn
	rpcClient, err := newGettyRPCClient(p, protocol, addr)
	return rpcClient, jerrors.Trace(err)
}

func (p *gettyRPCClientPool) put(conn *gettyRPCClient) {
	if conn == nil || conn.getActive() == 0 {
		return
	}

	var builder strings.Builder

	builder.WriteString(conn.addr)
	builder.WriteString("@")
	builder.WriteString(conn.protocol)

	key := builder.String()
	connArray, ok := p.connMap.Load(key)
	if !ok {
		connArray = newRpcClientArray()
	}
	if connArray.Size() >= p.size {
		conn.close()
		return
	}

	connArray.Put(conn)
	oldConnArray, loaded := p.connMap.LoadOrStore(key, connArray)
	if loaded {
		oldConnArray.Put(conn)
	}
}

func (p *gettyRPCClientPool) remove(conn *gettyRPCClient) {
	if conn == nil || conn.getActive() == 0 {
		return
	}

	var builder strings.Builder

	builder.WriteString(conn.addr)
	builder.WriteString("@")
	builder.WriteString(conn.protocol)

	key := builder.String()
	connArray, ok := p.connMap.Load(key)
	if !ok {
		return
	}
	connArray.Remove(key, conn, p)
}
