/******************************************************
# DESC       : session
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2016-08-17 11:21
# FILE       : session.go
******************************************************/

package getty

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

import (
	"github.com/AlexStocks/goext/time"
	log "github.com/AlexStocks/log4go"
)

const (
	maxReadBufLen      = 4 * 1024
	netIOTimeout       = 100e6    // 100ms
	period             = 60 * 1e9 // 1 minute
	pendingDuration    = 3e9
	defaultSessionName = "Session"
	outputFormat       = "session %s, Read Count: %d, Write Count: %d, Read Pkg Count: %d, Write Pkg Count: %d"
)

var (
	ErrInvalidConnection = errors.New("connection has been closed.")
)

/////////////////////////////////////////
// getty connection
/////////////////////////////////////////

var (
	connID uint32
)

type gettyConn struct {
	conn          net.Conn
	ID            uint32
	readCount     uint32 // read() count
	writeCount    uint32 // write() count
	readPkgCount  uint32 // send pkg count
	writePkgCount uint32 // recv pkg count
}

func newGettyConn(conn net.Conn) gettyConn {
	return gettyConn{conn: conn, ID: atomic.AddUint32(&connID, 1)}
}

func (this *gettyConn) read(p []byte) (int, error) {
	// if this.conn == nil {
	//	return 0, ErrInvalidConnection
	// }
	// atomic.AddUint32(&this.readCount, 1)
	atomic.AddUint32(&this.readCount, (uint32)(len(p)))
	return this.conn.Read(p)
}

func (this *gettyConn) write(p []byte) (int, error) {
	// if this.conn == nil {
	//	return 0, ErrInvalidConnection
	// }

	// atomic.AddUint32(&this.writeCount, 1)
	atomic.AddUint32(&this.writeCount, (uint32)(len(p)))
	return this.conn.Write(p)
}

func (this *gettyConn) close(waitSec int) {
	// if tcpConn, ok := this.conn.(*net.TCPConn); ok {
	// tcpConn.SetLinger(0)
	// }
	if this.conn != nil {
		this.conn.(*net.TCPConn).SetLinger(waitSec)
		this.conn.Close()
		this.conn = nil
	}
}

func (this *gettyConn) incReadPkgCount() {
	atomic.AddUint32(&this.readPkgCount, 1)
}

func (this *gettyConn) incWritePkgCount() {
	atomic.AddUint32(&this.writePkgCount, 1)
}

/////////////////////////////////////////
// session
/////////////////////////////////////////

var (
	ErrSessionClosed  = errors.New("Session Already Closed")
	ErrSessionBlocked = errors.New("Session full blocked")
)

type empty struct{}

// getty base session
type Session struct {
	name string
	// net read write
	gettyConn
	pkgHandler ReadWriter
	listener   EventListener
	once       sync.Once
	done       chan empty
	errFlag    bool

	period    time.Duration
	rDeadline time.Duration
	wDeadline time.Duration
	wait      time.Duration
	rQ        chan interface{}
	wQ        chan interface{}

	// attribute
	attrs map[string]interface{}
	// goroutines sync
	grNum int32
	lock  sync.RWMutex
}

func NewSession(conn net.Conn) *Session {
	return &Session{
		name:      defaultSessionName,
		gettyConn: newGettyConn(conn),
		done:      make(chan empty),
		period:    period,
		rDeadline: netIOTimeout,
		wDeadline: netIOTimeout,
		wait:      pendingDuration,
		attrs:     make(map[string]interface{}),
	}
}

func (this *Session) Reset() {
	this.name = defaultSessionName
	this.once = sync.Once{}
	this.done = make(chan empty)
	this.errFlag = false
	this.period = period
	this.rDeadline = netIOTimeout
	this.wDeadline = netIOTimeout
	this.wait = pendingDuration
	this.attrs = make(map[string]interface{})
	this.grNum = 0
}

func (this *Session) SetConn(conn net.Conn) { this.gettyConn = newGettyConn(conn) }
func (this *Session) Conn() net.Conn        { return this.conn }

// return the connect statistic data
func (this *Session) Stat() string {
	return fmt.Sprintf(
		outputFormat,
		this.sessionToken(),
		atomic.LoadUint32(&(this.readCount)),
		atomic.LoadUint32(&(this.writeCount)),
		atomic.LoadUint32(&(this.readPkgCount)),
		atomic.LoadUint32(&(this.writePkgCount)),
	)
}

// check whether the session has been closed.
func (this *Session) IsClosed() bool {
	select {
	case <-this.done:
		return true

	default:
		return false
	}
}

func (this *Session) SetName(name string) { this.name = name }

func (this *Session) SetEventListener(listener EventListener) {
	this.listener = listener
}

// set package handler
func (this *Session) SetPkgHandler(handler ReadWriter) {
	this.pkgHandler = handler
}

// period is in millisecond
func (this *Session) SetCronPeriod(period int) {
	if period < 1 {
		panic("@period < 1")
	}

	this.lock.Lock()
	this.period = time.Duration(period) * time.Millisecond
	this.lock.Unlock()
}

// set @Session's read queue size
func (this *Session) SetRQLen(readQLen int) {
	if readQLen < 1 {
		panic("@readQLen < 1")
	}

	this.lock.Lock()
	this.rQ = make(chan interface{}, readQLen)
	log.Info("%s, [session.SetRQLen] rQ{len:%d, cap:%d}", this.Stat(), len(this.rQ), cap(this.rQ))
	this.lock.Unlock()
}

// set @Session's write queue size
func (this *Session) SetWQLen(writeQLen int) {
	if writeQLen < 1 {
		panic("@writeQLen < 1")
	}

	this.lock.Lock()
	this.wQ = make(chan interface{}, writeQLen)
	log.Info("%s, [session.SetWQLen] wQ{len:%d, cap:%d}", this.Stat(), len(this.wQ), cap(this.wQ))
	this.lock.Unlock()
}

// SetReadDeadline sets deadline for the future read calls.
func (this *Session) SetReadDeadline(rDeadline time.Duration) {
	if rDeadline < 1 {
		panic("@rDeadline < 1")
	}

	this.lock.Lock()
	this.rDeadline = rDeadline
	if this.wDeadline == 0 {
		this.wDeadline = rDeadline
	}
	this.lock.Unlock()
}

// SetWriteDeadlile sets deadline for the future read calls.
func (this *Session) SetWriteDeadline(wDeadline time.Duration) {
	if wDeadline < 1 {
		panic("@wDeadline < 1")
	}

	this.lock.Lock()
	this.wDeadline = wDeadline
	this.lock.Unlock()
}

// set maximum wait time when session got error or got exit signal
func (this *Session) SetWaitTime(waitTime time.Duration) {
	if waitTime < 1 {
		panic("@wait < 1")
	}

	this.lock.Lock()
	this.wait = waitTime
	this.lock.Unlock()
}

func (this *Session) GetAttribute(key string) interface{} {
	var ret interface{}
	this.lock.RLock()
	ret = this.attrs[key]
	this.lock.RUnlock()
	return ret
}

func (this *Session) SetAttribute(key string, value interface{}) {
	this.lock.Lock()
	this.attrs[key] = value
	this.lock.Unlock()
}

func (this *Session) RemoveAttribute(key string) {
	this.lock.Lock()
	delete(this.attrs, key)
	this.lock.Unlock()
}

func (this *Session) sessionToken() string {
	var localAddr, peerAddr string
	if this.conn != nil {
		localAddr = this.conn.LocalAddr().String()
		peerAddr = this.conn.RemoteAddr().String()
	}
	return fmt.Sprintf("{%s, %d, %s <-> %s}", this.name, this.ID, localAddr, peerAddr)
}

// Queued write, for handler
func (this *Session) WritePkg(pkg interface{}) error {
	if this.IsClosed() {
		return ErrSessionClosed
	}

	defer func() {
		if r := recover(); r != nil {
			const size = 64 << 10
			rBuf := make([]byte, size)
			rBuf = rBuf[:runtime.Stack(rBuf, false)]
			log.Error("[session.WritePkg] panic session %s: err=%#v\n%s", this.sessionToken(), r, rBuf)
		}
	}()

	select {
	case this.wQ <- pkg:
		break // for possible gen a new pkg

	// default:
	// case <-time.After(this.wDeadline):
	case <-time.After(netIOTimeout):
		log.Warn("%s, [session.WritePkg] wQ{len:%d, cap:%d}", this.Stat(), len(this.wQ), cap(this.wQ))
		return ErrSessionBlocked
	}

	return nil
}

// for codecs
func (this *Session) WriteBytes(pkg []byte) error {
	this.conn.SetWriteDeadline(time.Now().Add(this.wDeadline))
	_, err := this.write(pkg)
	return err
}

func (this *Session) RunEventLoop() {
	if this.rQ == nil || this.wQ == nil {
		errStr := fmt.Sprintf("Session{name:%s, rQ:%#v, wQ:%#v}",
			this.name, this.rQ, this.wQ)
		log.Error(errStr)
		panic(errStr)
	}
	if this.conn == nil || this.listener == nil || this.pkgHandler == nil {
		errStr := fmt.Sprintf("Session{name:%s, conn:%#v, listener:%#v, pkgHandler:%#v}",
			this.name, this.conn, this.listener, this.pkgHandler)
		log.Error(errStr)
		panic(errStr)
	}

	// call session opened
	if err := this.listener.OnOpen(this); err != nil {
		this.Close()
		return
	}

	atomic.AddInt32(&(this.grNum), 2)
	go this.handleLoop()
	go this.handlePackage()
}

func (this *Session) handleLoop() {
	var (
		err  error
		flag bool
		// start  time.Time
		counter gxtime.CountWatch
		ticker  *time.Ticker
		inPkg   interface{}
		outPkg  interface{}
		once    sync.Once
	)

	defer func() {
		var grNum int32

		if r := recover(); r != nil {
			const size = 64 << 10
			rBuf := make([]byte, size)
			rBuf = rBuf[:runtime.Stack(rBuf, false)]
			log.Error("[session.handleLoop] panic session %s: err=%#v\n%s", this.sessionToken(), r, rBuf)
		}

		grNum = atomic.AddInt32(&(this.grNum), -1)
		if !this.errFlag {
			this.listener.OnClose(this)
		}
		log.Info("%s, [session.handleLoop] goroutine exit now, left gr num %d", this.Stat(), grNum)
		this.gc()
	}()

	flag = true // do not do any read/write/cron operation while got write error
	ticker = time.NewTicker(this.period)
LOOP:
	for {
		select {
		case <-this.done:
			// 这个分支确保(Session)handleLoop gr在(Session)handlePackage gr之后退出
			once.Do(func() { ticker.Stop() })
			if atomic.LoadInt32(&(this.grNum)) == 1 { // make sure @(Session)handlePackage goroutine has been closed.
				if len(this.rQ) == 0 && len(this.wQ) == 0 {
					log.Info("%s, [session.handleLoop] got done signal. Both rQ and wQ are nil.", this.Stat())
					break LOOP
				}
				counter.Start()
				// if time.Since(start).Nanoseconds() >= this.wait.Nanoseconds() {
				if counter.Count() > this.wait.Nanoseconds() {
					log.Info("%s, [session.handleLoop] got done signal ", this.Stat())
					break LOOP
				}
			}

		case inPkg = <-this.rQ:
			// 这个条件分支通过(Session)rQ排空确保(Session)handlePackage gr不会阻塞在(Session)rQ上
			if flag {
				this.listener.OnMessage(this, inPkg)
				this.incReadPkgCount()
			} else {
				log.Info("[session.handleLoop] drop readin package{%#v}", inPkg)
			}

		case outPkg = <-this.wQ:
			if flag {
				if err = this.pkgHandler.Write(this, outPkg); err != nil {
					log.Error("%s, [session.handleLoop] = error{%+v}", this.sessionToken(), err)
					this.stop()
					flag = false
					// break LOOP
				}
				this.incWritePkgCount()
			} else {
				log.Info("[session.handleLoop] drop writeout package{%#v}", outPkg)
			}

		case <-ticker.C:
			if flag {
				this.listener.OnCron(this)
			}
		}
	}
	once.Do(func() { ticker.Stop() })
	// ticker.Stop()
}

// get package from tcp stream(packet)
func (this *Session) handlePackage() {
	var (
		err     error
		nerr    net.Error
		ok      bool
		exit    bool
		errFlag bool
		bufLen  int
		pkgLen  int
		buf     []byte
		pktBuf  *bytes.Buffer
		pkg     interface{}
	)

	defer func() {
		var grNum int32

		if r := recover(); r != nil {
			const size = 64 << 10
			rBuf := make([]byte, size)
			rBuf = rBuf[:runtime.Stack(rBuf, false)]
			log.Error("[session.handlePackage] panic session %s: err=%#v\n%s", this.sessionToken(), r, rBuf)
		}

		grNum = atomic.AddInt32(&(this.grNum), -1)
		log.Info("%s, [session.handlePackage] gr will exit now, left gr num %d", this.sessionToken(), grNum)
		this.errFlag = errFlag
		this.stop()
		if errFlag {
			log.Error("%s, [session.handlePackage] error{%#v}", this.sessionToken(), err)
			this.listener.OnError(this, err)
		}
	}()

	buf = make([]byte, maxReadBufLen)
	pktBuf = new(bytes.Buffer)
	for {
		if this.IsClosed() {
			break // 退出前不再读取任何packet，buf中剩余的stream bytes也不可能凑够一个package, 所以直接退出
		}

		bufLen = 0
		for { // for clause for the network timeout condition check
			this.conn.SetReadDeadline(time.Now().Add(this.rDeadline))
			bufLen, err = this.read(buf)
			if err != nil {
				if nerr, ok = err.(net.Error); ok && nerr.Timeout() {
					break
				}
				log.Error("%s, [session.conn.read] = error{%v}", this.sessionToken(), err)
				// for (Codec)OnErr
				errFlag = true
				exit = true
			}
			break
		}
		if exit {
			break
		}
		if 0 == bufLen {
			continue // just continue if connection has read no more stream bytes.
		}
		pktBuf.Write(buf[:bufLen])
		for {
			if pktBuf.Len() <= 0 {
				break
			}
			// pkg, err = this.pkgHandler.Read(this, pktBuf)
			pkg, pkgLen, err = this.pkgHandler.Read(this, pktBuf.Bytes())
			if err != nil {
				log.Info("%s, [session.pkgHandler.Read] = error{%+v}", this.sessionToken(), err)
				// for (Codec)OnErr
				errFlag = true
				exit = true
				break
			}
			if pkg == nil {
				break
			}
			this.rQ <- pkg
			pktBuf.Next(pkgLen)
		}
		if exit {
			break
		}
	}
}

func (this *Session) stop() {
	select {
	case <-this.done: // this.done is a blocked channel. if it has not been closed, the default branch will be invoked.
		return

	default:
		this.once.Do(func() { close(this.done) })
	}
}

func (this *Session) gc() {
	this.lock.Lock()
	if this.attrs != nil {
		this.attrs = nil
		close(this.wQ)
		this.wQ = nil
		close(this.rQ)
		this.rQ = nil
		this.gettyConn.close((int)((int64)(this.wait)))
	}
	this.lock.Unlock()
}

// this function will be invoked by NewSessionCallback(if return error is not nil) or (Session)handleLoop automatically.
// It is goroutine-safe to be invoked many times.
func (this *Session) Close() {
	this.stop()
	log.Info("%s closed now, its current gr num %d", this.sessionToken(), atomic.LoadInt32(&(this.grNum)))
}
