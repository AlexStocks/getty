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
	"github.com/AlexStocks/goext/sync"
	log "github.com/AlexStocks/log4go"
	"github.com/gorilla/websocket"
)

const (
	defaultInterval = 3e9 // 3s
	connectTimeout  = 5e9
	maxTimes        = 10
)

/////////////////////////////////////////
// getty tcp client
/////////////////////////////////////////

type Client struct {
	// net
	sync.Mutex
	number     int
	interval   time.Duration
	addr       string
	newSession NewSessionCallback
	sessionMap map[Session]gxsync.Empty

	sync.Once
	done chan gxsync.Empty
	wg   sync.WaitGroup

	// for wss client
	certFile string
}

// NewClient function builds a tcp & ws client.
// @connNum is connection number.
// @connInterval is reconnect sleep interval when getty fails to connect the server.
// @serverAddr is server address.
func NewClient(connNum int, connInterval time.Duration, serverAddr string) *Client {
	if connNum < 0 {
		connNum = 1
	}
	if connInterval < defaultInterval {
		connInterval = defaultInterval
	}

	return &Client{
		number:     connNum,
		interval:   connInterval,
		addr:       serverAddr,
		sessionMap: make(map[Session]gxsync.Empty, connNum),
		done:       make(chan gxsync.Empty),
	}
}

// NewClient function builds a wss client.
// @connNum is connection number.
// @connInterval is reconnect sleep interval when getty fails to connect the server.
// @serverAddr is server address.
// @ cert is certificate file
func NewWSSClient(connNum int, connInterval time.Duration, serverAddr string, cert string) *Client {
	if connNum < 0 {
		connNum = 1
	}
	if connInterval < defaultInterval {
		connInterval = defaultInterval
	}

	return &Client{
		number:     connNum,
		interval:   connInterval,
		addr:       serverAddr,
		sessionMap: make(map[Session]gxsync.Empty, connNum),
		done:       make(chan gxsync.Empty),
		certFile:   cert,
	}
}

func (this *Client) dialTCP() Session {
	var (
		err  error
		conn net.Conn
	)

	for {
		if this.IsClosed() {
			return nil
		}
		conn, err = net.DialTimeout("tcp", this.addr, connectTimeout)
		if err == nil && conn.LocalAddr().String() == conn.RemoteAddr().String() {
			err = errSelfConnect
		}
		if err == nil {
			return NewTCPSession(conn)
		}

		log.Info("net.DialTimeout(addr:%s, timeout:%v) = error{%v}", this.addr, err)
		time.Sleep(this.interval)
		continue
	}
}

func (this *Client) dialWS() Session {
	var (
		err     error
		dialer  websocket.Dialer
		conn    *websocket.Conn
		session Session
	)

	dialer.EnableCompression = true
	for {
		if this.IsClosed() {
			return nil
		}
		conn, _, err = dialer.Dial(this.addr, nil)
		if err == nil && conn.LocalAddr().String() == conn.RemoteAddr().String() {
			err = errSelfConnect
		}
		if err == nil {
			session = NewWSSession(conn)
			if session.(*session).maxMsgLen > 0 {
				conn.SetReadLimit(int64(session.(*session).maxMsgLen))
			}

			return session
		}

		log.Info("websocket.dialer.Dial(addr:%s) = error{%v}", this.addr, err)
		time.Sleep(this.interval)
		continue
	}
}

func (this *Client) dialWSS() Session {
	var (
		err      error
		certPem  []byte
		certPool *x509.CertPool
		dialer   websocket.Dialer
		conn     *websocket.Conn
		session  Session
	)

	dialer.EnableCompression = true
	certPem, err = ioutil.ReadFile(this.certFile)
	if err != nil {
		panic(fmt.Errorf("ioutil.ReadFile(certFile{%s}) = err{%#v}", this.certFile, err))
	}
	certPool = x509.NewCertPool()
	if ok := certPool.AppendCertsFromPEM(certPem); !ok {
		panic("failed to parse root certificate")
	}

	// dialer.EnableCompression = true
	dialer.TLSClientConfig = &tls.Config{RootCAs: certPool}
	for {
		if this.IsClosed() {
			return nil
		}
		conn, _, err = dialer.Dial(this.addr, nil)
		if err == nil && conn.LocalAddr().String() == conn.RemoteAddr().String() {
			err = errSelfConnect
		}
		if err == nil {
			session = NewWSSession(conn)
			if session.(*session).maxMsgLen > 0 {
				conn.SetReadLimit(int64(session.(*session).maxMsgLen))
			}

			return session
		}

		log.Info("websocket.dialer.Dial(addr:%s) = error{%v}", this.addr, err)
		time.Sleep(this.interval)
		continue
	}
}

func (this *Client) dial() Session {
	if strings.HasPrefix(this.addr, "ws") {
		return this.dialWS()
	} else if strings.HasPrefix(this.addr, "wss") {
		return this.dialWSS()
	}

	return this.dialTCP()
}

func (this *Client) sessionNum() int {
	var num int

	this.Lock()
	for s := range this.sessionMap {
		if s.IsClosed() {
			delete(this.sessionMap, s)
		}
	}
	num = len(this.sessionMap)
	this.Unlock()

	return num
}

func (this *Client) connect() {
	var (
		err     error
		session Session
	)

	for {
		session = this.dial()
		if session == nil {
			// client has been closed
			break
		}
		err = this.newSession(session)
		if err == nil {
			// session.RunEventLoop()
			session.(*session).run()
			this.Lock()
			this.sessionMap[session] = gxsync.Empty{}
			this.Unlock()
			break
		}
		// don't distinguish between tcp connection and websocket connection. Because
		// gorilla/websocket/conn.go:(Conn)Close also invoke net.Conn.Close()
		session.Conn().Close()
	}
}

func (this *Client) RunEventLoop(newSession NewSessionCallback) {
	this.Lock()
	this.newSession = newSession
	this.Unlock()

	this.wg.Add(1)
	go func() {
		var num, max, times int
		defer this.wg.Done()

		this.Lock()
		max = this.number
		this.Unlock()
		// log.Info("maximum client connection number:%d", max)
		for {
			if this.IsClosed() {
				log.Warn("client{peer:%s} goroutine exit now.", this.addr)
				break
			}

			num = this.sessionNum()
			// log.Info("current client connction number:%d", num)
			if max <= num {
				times++
				if maxTimes < times {
					times = maxTimes
				}
				time.Sleep(time.Duration(int64(times) * defaultInterval))
				continue
			}
			times = 0
			this.connect()
			// time.Sleep(this.interval) // build this.number connections asap
		}
	}()
}

func (this *Client) stop() {
	select {
	case <-this.done:
		return
	default:
		this.Once.Do(func() {
			close(this.done)
			this.Lock()
			for s := range this.sessionMap {
				s.Close()
			}
			this.sessionMap = nil
			this.Unlock()
		})
	}
}

func (this *Client) IsClosed() bool {
	select {
	case <-this.done:
		return true
	default:
		return false
	}
}

func (this *Client) Close() {
	this.stop()
	this.wg.Wait()
}
