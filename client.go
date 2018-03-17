/******************************************************
# DESC       : getty client
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2016-09-01 21:32
# FILE       : client.go
******************************************************/

package getty

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"sync"
	"time"
)

import (
	"encoding/pem"
	"github.com/AlexStocks/goext/sync"
	log "github.com/AlexStocks/log4go"
	"github.com/gorilla/websocket"
)

const (
	connInterval   = 3e9 // 3s
	connectTimeout = 5e9
	maxTimes       = 10
)

/////////////////////////////////////////
// getty tcp client
/////////////////////////////////////////

type client struct {
	ClientOptions

	// net
	sync.Mutex
	endPointType EndPointType

	newSession NewSessionCallback
	ssMap      map[Session]gxsync.Empty

	sync.Once
	done chan gxsync.Empty
	wg   sync.WaitGroup
}

func (c *client) init(opts ...ClientOption) {
	for _, opt := range opts {
		opt(&(c.ClientOptions))
	}
}

// NewTcpClient function builds a tcp client.
func NewTCPClient(opts ...ClientOption) Client {
	c := &client{
		endPointType: TCP_CLIENT,
		done:         make(chan gxsync.Empty),
	}

	c.init(opts...)

	if c.number <= 0 || c.addr == "" {
		panic(fmt.Sprintf("@connNum:%d, @serverAddr:%s", c.number, c.addr))
	}

	c.ssMap = make(map[Session]gxsync.Empty, c.number)

	return c
}

// NewUdpClient function builds a udp client
func NewUDPClient(opts ...ClientOption) Client {
	c := &client{
		done: make(chan gxsync.Empty),
	}

	c.init(opts...)

	c.endPointType = UNCONNECTED_UDP_CLIENT
	if len(c.addr) != 0 {
		if c.number <= 0 {
			panic(fmt.Sprintf("getty will build a preconected connection by @serverAddr:%s while @connNum is %d",
				c.addr, c.number))
		}

		c.endPointType = CONNECTED_UDP_CLIENT
	}
	c.ssMap = make(map[Session]gxsync.Empty, c.number)

	return c
}

// NewWsClient function builds a ws client.
func NewWSClient(opts ...ClientOption) Client {
	c := &client{
		endPointType: WS_CLIENT,
		done:         make(chan gxsync.Empty),
	}

	c.init(opts...)

	if c.number <= 0 || c.addr == "" {
		panic(fmt.Sprintf("@connNum:%d, @serverAddr:%s", c.number, c.addr))
	}
	if !strings.HasPrefix(c.addr, "ws://") {
		panic(fmt.Sprintf("the prefix @serverAddr:%s is not ws://", c.addr))
	}

	c.ssMap = make(map[Session]gxsync.Empty, c.number)

	return c
}

// NewWSSClient function builds a wss client.
func NewWSSClient(opts ...ClientOption) Client {
	c := &client{
		endPointType: WSS_CLIENT,
		done:         make(chan gxsync.Empty),
	}

	c.init(opts...)

	if c.number <= 0 || c.addr == "" || c.cert == "" {
		panic(fmt.Sprintf("@connNum:%d, @serverAddr:%s, @cert:%s", c.number, c.addr, c.cert))
	}
	if !strings.HasPrefix(c.addr, "wss://") {
		panic(fmt.Sprintf("the prefix @serverAddr:%s is not wss://", c.addr))
	}

	c.ssMap = make(map[Session]gxsync.Empty, c.number)

	return c
}

func (c client) Type() EndPointType {
	return c.endPointType
}

func (c *client) dialTCP() Session {
	var (
		err  error
		conn net.Conn
	)

	for {
		if c.IsClosed() {
			return nil
		}
		conn, err = net.DialTimeout("tcp", c.addr, connectTimeout)
		if err == nil && conn.LocalAddr().String() == conn.RemoteAddr().String() {
			err = errSelfConnect
		}
		if err == nil {
			return NewTCPSession(conn)
		}

		log.Info("net.DialTimeout(addr:%s, timeout:%v) = error{%v}", c.addr, err)
		time.Sleep(connInterval)
	}
}

func (c *client) dialUDP() Session {
	var (
		err       error
		conn      *net.UDPConn
		localAddr *net.UDPAddr
		peerAddr  *net.UDPAddr
	)

	localAddr = &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	peerAddr, _ = net.ResolveUDPAddr("udp", c.addr)
	for {
		if c.IsClosed() {
			return nil
		}
		if UNCONNECTED_UDP_CLIENT == c.endPointType {
			conn, err = net.ListenUDP("udp", localAddr)
		} else {
			conn, err = net.DialUDP("udp", localAddr, peerAddr)
			if err == nil && conn.LocalAddr().String() == conn.RemoteAddr().String() {
				err = errSelfConnect
			}
			peerAddr = nil // for connected session
		}
		if err == nil {
			return NewUDPSession(conn, peerAddr)
		}

		log.Info("net.DialTimeout(addr:%s, timeout:%v) = error{%v}", c.addr, err)
		time.Sleep(connInterval)
	}
}

func (c *client) dialWS() Session {
	var (
		err    error
		dialer websocket.Dialer
		conn   *websocket.Conn
		ss     Session
	)

	dialer.EnableCompression = true
	for {
		if c.IsClosed() {
			return nil
		}
		conn, _, err = dialer.Dial(c.addr, nil)
		log.Info("websocket.dialer.Dial(addr:%s) = error{%v}", c.addr, err)
		if err == nil && conn.LocalAddr().String() == conn.RemoteAddr().String() {
			err = errSelfConnect
		}
		if err == nil {
			ss = NewWSSession(conn)
			if ss.(*session).maxMsgLen > 0 {
				conn.SetReadLimit(int64(ss.(*session).maxMsgLen))
			}

			return ss
		}

		log.Info("websocket.dialer.Dial(addr:%s) = error{%v}", c.addr, err)
		time.Sleep(connInterval)
	}
}

func (c *client) dialWSS() Session {
	var (
		err      error
		root     *x509.Certificate
		roots    []*x509.Certificate
		certPool *x509.CertPool
		config   *tls.Config
		dialer   websocket.Dialer
		conn     *websocket.Conn
		ss       Session
	)

	dialer.EnableCompression = true

	config = &tls.Config{
		InsecureSkipVerify: true,
	}

	if c.cert != "" {
		certPEMBlock, err := ioutil.ReadFile(c.cert)
		if err != nil {
			panic(fmt.Sprintf("ioutil.ReadFile(cert:%s) = error{%#v}", c.cert, err))
		}

		var cert tls.Certificate
		for {
			var certDERBlock *pem.Block
			certDERBlock, certPEMBlock = pem.Decode(certPEMBlock)
			if certDERBlock == nil {
				break
			}
			if certDERBlock.Type == "CERTIFICATE" {
				cert.Certificate = append(cert.Certificate, certDERBlock.Bytes)
			}
		}
		config.Certificates = make([]tls.Certificate, 1)
		config.Certificates[0] = cert
	}

	certPool = x509.NewCertPool()
	for _, c := range config.Certificates {
		roots, err = x509.ParseCertificates(c.Certificate[len(c.Certificate)-1])
		if err != nil {
			panic(fmt.Sprintf("error parsing server's root cert: %v\n", err))
		}
		for _, root = range roots {
			certPool.AddCert(root)
		}
	}
	config.InsecureSkipVerify = true
	config.RootCAs = certPool

	// dialer.EnableCompression = true
	dialer.TLSClientConfig = config
	for {
		if c.IsClosed() {
			return nil
		}
		conn, _, err = dialer.Dial(c.addr, nil)
		if err == nil && conn.LocalAddr().String() == conn.RemoteAddr().String() {
			err = errSelfConnect
		}
		if err == nil {
			ss = NewWSSession(conn)
			if ss.(*session).maxMsgLen > 0 {
				conn.SetReadLimit(int64(ss.(*session).maxMsgLen))
			}
			ss.SetName(defaultWSSSessionName)

			return ss
		}

		log.Info("websocket.dialer.Dial(addr:%s) = error{%v}", c.addr, err)
		time.Sleep(connInterval)
	}
}

func (c *client) dial() Session {
	switch c.endPointType {
	case TCP_CLIENT:
		return c.dialTCP()
	case UNCONNECTED_UDP_CLIENT, CONNECTED_UDP_CLIENT:
		return c.dialUDP()
	case WS_CLIENT:
		return c.dialWS()
	case WSS_CLIENT:
		return c.dialWSS()
	}

	return nil
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

func (c *client) connect() {
	var (
		err error
		ss  Session
	)

	for {
		ss = c.dial()
		if ss == nil {
			// client has been closed
			break
		}
		err = c.newSession(ss)
		if err == nil {
			// ss.RunEventLoop()
			ss.(*session).run()
			c.Lock()
			c.ssMap[ss] = gxsync.Empty{}
			c.Unlock()
			break
		}
		// don't distinguish between tcp connection and websocket connection. Because
		// gorilla/websocket/conn.go:(Conn)Close also invoke net.Conn.Close()
		ss.Conn().Close()
	}
}

func (c *client) RunEventLoop(newSession NewSessionCallback) {
	c.Lock()
	c.newSession = newSession
	c.Unlock()

	c.wg.Add(1)
	// a for-loop goroutine to make sure the connection is valid
	go func() {
		var num, max, times int
		defer c.wg.Done()

		c.Lock()
		max = c.number
		c.Unlock()
		// log.Info("maximum client connection number:%d", max)
		for {
			if c.IsClosed() {
				log.Warn("client{peer:%s} goroutine exit now.", c.addr)
				break
			}

			num = c.sessionNum()
			// log.Info("current client connction number:%d", num)
			if max <= num {
				times++
				if maxTimes < times {
					times = maxTimes
				}
				time.Sleep(time.Duration(int64(times) * connInterval))
				continue
			}
			times = 0
			c.connect()
			if c.endPointType == UNCONNECTED_UDP_CLIENT || c.endPointType == CONNECTED_UDP_CLIENT {
				break
			}
			// time.Sleep(c.interval) // build c.number connections asap
		}
	}()
}

func (c *client) stop() {
	select {
	case <-c.done:
		return
	default:
		c.Once.Do(func() {
			close(c.done)
			c.Lock()
			for s := range c.ssMap {
				s.Close()
			}
			c.ssMap = nil
			c.Unlock()
		})
	}
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
