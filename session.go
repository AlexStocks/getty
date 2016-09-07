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
	"sync"
	"sync/atomic"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
)

const (
	maxReadBufLen      = 4 * 1024
	netIOTimeout       = 100e6    // 100ms
	peroid             = 60 * 1e9 // 1 minute
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

// getty base session
type Session struct {
	name string
	// net read write
	gettyConn
	pkgHandler ReadWriter
	listener   EventListener
	once       sync.Once
	done       chan struct{}
	readerDone chan struct{} // end reader

	peroid    time.Duration
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
		name:       defaultSessionName,
		gettyConn:  newGettyConn(conn),
		done:       make(chan struct{}),
		readerDone: make(chan struct{}),
		peroid:     peroid,
		rDeadline:  netIOTimeout,
		wDeadline:  netIOTimeout,
		wait:       pendingDuration,
		attrs:      make(map[string]interface{}),
	}
}

func (this *Session) Reset() {
	this.name = defaultSessionName
	this.done = make(chan struct{})
	this.readerDone = make(chan struct{})
	this.peroid = peroid
	this.rDeadline = netIOTimeout
	this.wDeadline = netIOTimeout
	this.wait = pendingDuration
	this.attrs = make(map[string]interface{})
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
	this.peroid = time.Duration(period) * time.Millisecond
	this.lock.Unlock()
}

// set @Session's read queue size
func (this *Session) SetRQLen(readQLen int) {
	if readQLen < 1 {
		panic("@readQLen < 1")
	}

	this.lock.Lock()
	this.rQ = make(chan interface{}, readQLen)
	this.lock.Unlock()
}

// set @Session's write queue size
func (this *Session) SetWQLen(writeQLen int) {
	if writeQLen < 1 {
		panic("@writeQLen < 1")
	}

	this.lock.Lock()
	this.wQ = make(chan interface{}, writeQLen)
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
	return fmt.Sprintf(
		"{%s, %d, %s <-> %s}",
		this.name,
		this.ID,
		this.conn.LocalAddr().String(),
		this.conn.RemoteAddr().String(),
	)
}

// Queued write, for handler
func (this *Session) WritePkg(pkg interface{}) error {
	if this.IsClosed() {
		return ErrSessionClosed
	}

	defer func() {
		if err := recover(); err != nil {
			log.Error("%s [session.WritePkg] err=%+v\n", this.sessionToken(), err)
		}
	}()

	select {
	case this.wQ <- pkg:
		break // for possible gen a new pkg
	default:
		return ErrSessionBlocked
	}

	return nil
}

// for codecs
func (this *Session) WriteBytes(pkg []byte) error {
	this.conn.SetWriteDeadline(time.Now().Add(this.rDeadline))
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
		err    error
		start  time.Time
		ticker *time.Ticker
		inPkg  interface{}
		outPkg interface{}
	)

	defer func() {
		var grNum int32
		if err := recover(); err != nil {
			log.Error("%s, [session.handleLoop] err=%+v\n", this.sessionToken(), err)
		}

		grNum = atomic.AddInt32(&(this.grNum), -1)
		this.listener.OnClose(this)
		log.Info("statistic{%s}, [session.handleLoop] goroutine exit now, left gr num %d", this.Stat(), grNum)
		this.Close()
	}()

	ticker = time.NewTicker(this.peroid)
LOOP:
	for {
		select {
		case <-this.done:
			log.Info("%s, [session.handleLoop] got done signal ", this.Stat())
			break LOOP
		case inPkg = <-this.rQ:

			this.listener.OnMessage(this, inPkg)
			this.incReadPkgCount()

		case outPkg = <-this.wQ:
			if err = this.pkgHandler.Write(this, outPkg); err != nil {
				log.Error("%s, [session.handleLoop] = error{%+v}", this.sessionToken(), err)
				break LOOP
			}
			this.incWritePkgCount()
		case <-ticker.C:
			this.listener.OnCron(this)
		}
	}
	ticker.Stop()

	this.stop()
	// wait for reader goroutine closed
	<-this.readerDone

	// process pending pkg
	start = time.Now()
LAST:
	for {
		if time.Since(start).Nanoseconds() >= this.wait.Nanoseconds() {
			break
		}

		select {
		case outPkg = <-this.wQ:
			if err = this.pkgHandler.Write(this, outPkg); err != nil {
				break LAST
			}
			this.incWritePkgCount()
		case inPkg = <-this.rQ:

			this.listener.OnMessage(this, inPkg)
			this.incReadPkgCount()

		default:
			log.Info("%s, [session.handleLoop] default", this.sessionToken())
			break LAST
		}
	}
}

// get package from tcp stream(packet)
func (this *Session) handlePackage() {
	var (
		err     error
		nerr    net.Error
		ok      bool
		exit    bool
		errFlag bool
		len     int
		buf     []byte
		pktBuf  *bytes.Buffer
		pkg     interface{}
	)

	defer func() {
		var grNum int32
		if err := recover(); err != nil {
			log.Error("%s, [session.handlePackage] = err{%+v}", this.sessionToken(), err)
		}
		this.stop()
		close(this.readerDone)
		grNum = atomic.AddInt32(&(this.grNum), -1)
		log.Info("%s, [session.handlePackage] gr will exit now, left gr num %d", this.sessionToken(), grNum)
		if errFlag {
			log.Info("%s, [session.handlePackage] errFlag", this.sessionToken())
			this.listener.OnError(this, err)
		}
	}()

	buf = make([]byte, maxReadBufLen)
	pktBuf = new(bytes.Buffer)
	for {
		if this.IsClosed() {
			exit = true
		}

		for {
			if exit {
				break // 退出前不再读取任何packet，跳到下个for-loop处理完pktBuf中的stream
			}
			this.conn.SetReadDeadline(time.Now().Add(this.rDeadline))
			len, err = this.read(buf)
			if err != nil {
				if nerr, ok = err.(net.Error); ok && nerr.Timeout() {
					break
				}
				log.Error("%s, [session.conn.read] = error{%v}", this.sessionToken(), err)
				// 遇到网络错误的时候，handlePackage能够及时退出，但是handleLoop的第一个for-select因为要处理(Codec)OnMessage
				// 导致程序不能及时退出，此处添加(Codec)OnError调用以及时通知getty调用者
				// AS, 2016/08/21
				errFlag = true
				exit = true
			}
			break
		}
		if 0 < len {
			pktBuf.Write(buf[:len])
		}
		for {
			if pktBuf.Len() <= 0 {
				break
			}
			// pkg, err = this.pkgHandler.Read(this, pktBuf)
			pkg, len, err = this.pkgHandler.Read(this, pktBuf.Bytes())
			if err != nil {
				log.Info("%s, [session.pkgHandler.Read] = error{%+v}", this.sessionToken(), err)
				errFlag = true
				exit = true
				break
			}
			if pkg == nil {
				break
			}
			this.rQ <- pkg
			pktBuf.Next(len)
		}
		if exit {
			break
		}
	}
}

func (this *Session) stop() {
	select {
	case <-this.done:
		return
	default:
		this.once.Do(func() { close(this.done) })
	}
}

// this function will be invoked by NewSessionCallback(if return error is not nil) or (Session)handleLoop automatically.
// It is goroutine-safe to be invoked many times.
func (this *Session) Close() error {
	this.stop()
	log.Info("%s closed now, its current gr num %d",
		this.sessionToken(), atomic.LoadInt32(&(this.grNum)))
	this.lock.Lock()
	if this.attrs != nil {
		this.attrs = nil
		select {
		case <-this.readerDone:
		default:
			close(this.readerDone)
		}
		close(this.wQ)
		this.wQ = nil
		close(this.rQ)
		this.rQ = nil
		this.gettyConn.close((int)((int64)(this.wait)))
	}
	this.lock.Unlock()

	return nil
}
