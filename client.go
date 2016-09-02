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
	"net"
	"sync"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
)

const (
	defaultInterval = 3e9 // 3s
)

type empty struct{}

type Client struct {
	// net
	sync.Mutex
	number     int
	interval   time.Duration
	addr       string
	newSession SessionCallback
	sessionMap map[*Session]empty

	sync.Once
	done chan struct{}
	wg   sync.WaitGroup
}

// NewClient function builds a client.
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
		sessionMap: make(map[*Session]empty, connNum),
		done:       make(chan struct{}),
	}
}

func (this *Client) dial() net.Conn {
	var (
		err  error
		conn net.Conn
	)

	for {
		if this.IsClosed() {
			return nil
		}
		conn, err = net.DialTimeout("tcp", this.addr, this.interval)
		if err == nil {
			return conn
		}

		log.Info("net.Connect(addr:%s, timeout:%v) = error{%v}", this.addr, err)
		time.Sleep(this.interval)
		continue
	}
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
		err  error
		conn net.Conn
		ss   *Session
	)

	for {
		conn = this.dial()
		if conn == nil {
			// client has been closed
			break
		}
		ss = NewSession(conn)
		err = this.newSession(ss)
		if err == nil {
			ss.RunEventloop()
			this.Lock()
			this.sessionMap[ss] = empty{}
			this.Unlock()
			break
		}
		conn.Close()
	}
}

func (this *Client) RunEventLoop(newSession SessionCallback) {
	this.Lock()
	this.newSession = newSession
	this.Unlock()

	this.wg.Add(1)
	go func() {
		var num, max int
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
				time.Sleep(this.interval)
				continue
			}
			this.connect()
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
