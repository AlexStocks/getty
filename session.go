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
	"github.com/AlexStocks/goext/sync"
	"github.com/AlexStocks/goext/time"
	log "github.com/AlexStocks/log4go"
	"github.com/gorilla/websocket"
)

const (
	maxReadBufLen      = 4 * 1024
	netIOTimeout       = 1e9      // 1s
	period             = 60 * 1e9 // 1 minute
	pendingDuration    = 3e9
	defaultSessionName = "session"
	outputFormat       = "session %s, Read Count: %d, Write Count: %d, Read Pkg Count: %d, Write Pkg Count: %d"
)

/////////////////////////////////////////
// session
/////////////////////////////////////////

var (
	ErrSessionClosed  = errors.New("session Already Closed")
	ErrSessionBlocked = errors.New("session Full Blocked")
	ErrMsgTooLong     = errors.New("Message Too Long")
)

var (
	wheel = gxtime.NewWheel(gxtime.TimeMillisecondDuration(100), 1200) // wheel longest span is 2 minute
)

type Session interface {
	Connection
	Reset()
	Conn() net.Conn
	Stat() string
	IsClosed() bool

	SetMaxMsgLen(int)
	SetName(string)
	SetEventListener(EventListener)
	SetPkgHandler(ReadWriter)
	SetReader(Reader)
	SetWriter(Writer)
	SetCronPeriod(int)
	SetRQLen(int)
	SetWQLen(int)
	SetWaitTime(time.Duration)

	GetAttribute(string) interface{}
	SetAttribute(string, interface{})
	RemoveAttribute(string)

	WritePkg(interface{}) error
	WriteBytes([]byte) error
	WriteBytesArray(...[]byte) error
	Close()
}

// getty base session
type session struct {
	name      string
	maxMsgLen int32
	// net read Write
	Connection
	// pkgHandler ReadWriter
	reader   Reader // @reader should be nil when @conn is a gettyWSConn object.
	writer   Writer
	listener EventListener
	once     sync.Once
	done     chan gxsync.Empty
	// errFlag  bool

	period time.Duration
	wait   time.Duration
	rQ     chan interface{}
	wQ     chan interface{}

	// attribute
	attrs map[string]interface{}
	// goroutines sync
	grNum int32
	lock  sync.RWMutex
}

func NewSession() Session {
	session := &session{
		name:   defaultSessionName,
		done:   make(chan gxsync.Empty),
		period: period,
		wait:   pendingDuration,
		attrs:  make(map[string]interface{}),
	}

	session.SetWriteDeadline(netIOTimeout)
	session.SetReadDeadline(netIOTimeout)

	return session
}

func NewTCPSession(conn net.Conn) Session {
	session := &session{
		name:       defaultSessionName,
		Connection: newGettyTCPConn(conn),
		done:       make(chan gxsync.Empty),
		period:     period,
		wait:       pendingDuration,
		attrs:      make(map[string]interface{}),
	}

	session.SetWriteDeadline(netIOTimeout)
	session.SetReadDeadline(netIOTimeout)

	return session
}

func NewWSSession(conn *websocket.Conn) Session {
	session := &session{
		name:       defaultSessionName,
		Connection: newGettyWSConn(conn),
		done:       make(chan gxsync.Empty),
		period:     period,
		wait:       pendingDuration,
		attrs:      make(map[string]interface{}),
	}

	session.SetWriteDeadline(netIOTimeout)
	session.SetReadDeadline(netIOTimeout)

	return session
}

func (this *session) Reset() {
	this.name = defaultSessionName
	this.once = sync.Once{}
	this.done = make(chan gxsync.Empty)
	// this.errFlag = false
	this.period = period
	this.wait = pendingDuration
	this.attrs = make(map[string]interface{})
	this.grNum = 0

	this.SetWriteDeadline(netIOTimeout)
	this.SetReadDeadline(netIOTimeout)
}

// func (this *session) SetConn(conn net.Conn) { this.gettyConn = newGettyConn(conn) }
func (this *session) Conn() net.Conn {
	if tc, ok := this.Connection.(*gettyTCPConn); ok {
		return tc.conn
	}

	if wc, ok := this.Connection.(*gettyWSConn); ok {
		return wc.conn.UnderlyingConn()
	}

	return nil
}

func (this *session) gettyConn() *gettyConn {
	if tc, ok := this.Connection.(*gettyTCPConn); ok {
		return &(tc.gettyConn)
	}

	if wc, ok := this.Connection.(*gettyWSConn); ok {
		return &(wc.gettyConn)
	}

	return nil
}

// return the connect statistic data
func (this *session) Stat() string {
	var conn *gettyConn
	if conn = this.gettyConn(); conn == nil {
		return ""
	}
	return fmt.Sprintf(
		outputFormat,
		this.sessionToken(),
		atomic.LoadUint32(&(conn.readCount)),
		atomic.LoadUint32(&(conn.writeCount)),
		atomic.LoadUint32(&(conn.readPkgCount)),
		atomic.LoadUint32(&(conn.writePkgCount)),
	)
}

// check whether the session has been closed.
func (this *session) IsClosed() bool {
	select {
	case <-this.done:
		return true

	default:
		return false
	}
}

// set maximum pacakge length of every pacakge in (EventListener)OnMessage(@pkgs)
func (this *session) SetMaxMsgLen(length int) { this.maxMsgLen = int32(length) }

// set session name
func (this *session) SetName(name string) { this.name = name }

// set EventListener
func (this *session) SetEventListener(listener EventListener) {
	this.listener = listener
}

// set package handler
func (this *session) SetPkgHandler(handler ReadWriter) {
	this.reader = handler
	this.writer = handler
	// this.pkgHandler = handler
}

// set Reader
func (this *session) SetReader(reader Reader) {
	this.reader = reader
}

// set Writer
func (this *session) SetWriter(writer Writer) {
	this.writer = writer
}

// period is in millisecond. Websocket session will send ping frame automatically every peroid.
func (this *session) SetCronPeriod(period int) {
	if period < 1 {
		panic("@period < 1")
	}

	this.lock.Lock()
	this.period = time.Duration(period) * time.Millisecond
	this.lock.Unlock()
}

// set @session's read queue size
func (this *session) SetRQLen(readQLen int) {
	if readQLen < 1 {
		panic("@readQLen < 1")
	}

	this.lock.Lock()
	this.rQ = make(chan interface{}, readQLen)
	log.Info("%s, [session.SetRQLen] rQ{len:%d, cap:%d}", this.Stat(), len(this.rQ), cap(this.rQ))
	this.lock.Unlock()
}

// set @session's Write queue size
func (this *session) SetWQLen(writeQLen int) {
	if writeQLen < 1 {
		panic("@writeQLen < 1")
	}

	this.lock.Lock()
	this.wQ = make(chan interface{}, writeQLen)
	log.Info("%s, [session.SetWQLen] wQ{len:%d, cap:%d}", this.Stat(), len(this.wQ), cap(this.wQ))
	this.lock.Unlock()
}

// set maximum wait time when session got error or got exit signal
func (this *session) SetWaitTime(waitTime time.Duration) {
	if waitTime < 1 {
		panic("@wait < 1")
	}

	this.lock.Lock()
	this.wait = waitTime
	this.lock.Unlock()
}

// set attribute of key @session:key
func (this *session) GetAttribute(key string) interface{} {
	var ret interface{}
	this.lock.RLock()
	ret = this.attrs[key]
	this.lock.RUnlock()
	return ret
}

// get attribute of key @session:key
func (this *session) SetAttribute(key string, value interface{}) {
	this.lock.Lock()
	this.attrs[key] = value
	this.lock.Unlock()
}

// delete attribute of key @session:key
func (this *session) RemoveAttribute(key string) {
	this.lock.Lock()
	delete(this.attrs, key)
	this.lock.Unlock()
}

func (this *session) sessionToken() string {
	return fmt.Sprintf("{%s:%d:%s<->%s}", this.name, this.ID(), this.LocalAddr(), this.RemoteAddr())
}

// Queued Write, for handler
func (this *session) WritePkg(pkg interface{}) error {
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

	var d = this.writeDeadline()
	if d > netIOTimeout {
		d = netIOTimeout
	}
	select {
	case this.wQ <- pkg:
		break // for possible gen a new pkg

	// default:
	// case <-time.After(this.wDeadline):
	// case <-time.After(netIOTimeout):
	case <-wheel.After(d):
		log.Warn("%s, [session.WritePkg] wQ{len:%d, cap:%d}", this.Stat(), len(this.wQ), cap(this.wQ))
		return ErrSessionBlocked
	}

	return nil
}

// for codecs
func (this *session) WriteBytes(pkg []byte) error {
	if this.IsClosed() {
		return ErrSessionClosed
	}

	// this.conn.SetWriteDeadline(time.Now().Add(this.wDeadline))
	return this.Connection.Write(pkg)
}

// Write multiple packages at once
func (this *session) WriteBytesArray(pkgs ...[]byte) error {
	if this.IsClosed() {
		return ErrSessionClosed
	}
	// this.conn.SetWriteDeadline(time.Now().Add(this.wDeadline))

	if len(pkgs) == 1 {
		return this.Connection.Write(pkgs[0])
	}

	// get len
	var (
		l      int
		length uint32
		arr    []byte
	)
	length = 0
	for i := 0; i < len(pkgs); i++ {
		length += uint32(len(pkgs[i]))
	}

	// merge the pkgs
	arr = make([]byte, length)
	l = 0
	for i := 0; i < len(pkgs); i++ {
		copy(arr[l:], pkgs[i])
		l += len(pkgs[i])
	}

	// return this.Connection.Write(arr)
	return this.WriteBytes(arr)
}

// func (this *session) RunEventLoop() {
func (this *session) run() {
	if this.rQ == nil || this.wQ == nil {
		errStr := fmt.Sprintf("session{name:%s, rQ:%#v, wQ:%#v}",
			this.name, this.rQ, this.wQ)
		log.Error(errStr)
		panic(errStr)
	}
	if this.Connection == nil || this.listener == nil || this.writer == nil {
		errStr := fmt.Sprintf("session{name:%s, conn:%#v, listener:%#v, writer:%#v}",
			this.name, this.Connection, this.listener, this.writer)
		log.Error(errStr)
		panic(errStr)
	}

	// call session opened
	this.UpdateActive()
	if err := this.listener.OnOpen(this); err != nil {
		this.Close()
		return
	}

	atomic.AddInt32(&(this.grNum), 2)
	go this.handleLoop()
	go this.handlePackage()
}

func (this *session) handleLoop() {
	var (
		err    error
		flag   bool
		wsFlag bool
		wsConn *gettyWSConn
		// start  time.Time
		counter gxtime.CountWatch
		// ticker  *time.Ticker // use wheel instead, 2016/09/26
		inPkg  interface{}
		outPkg interface{}
		// once   sync.Once // use wheel instead, 2016/09/26
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
		// if !this.errFlag {
		this.listener.OnClose(this)
		// }
		log.Info("%s, [session.handleLoop] goroutine exit now, left gr num %d", this.Stat(), grNum)
		this.gc()
	}()

	wsConn, wsFlag = this.Connection.(*gettyWSConn)
	flag = true // do not do any read/Write/cron operation while got Write error
	// ticker = time.NewTicker(this.period) // use wheel instead, 2016/09/26
LOOP:
	for {
		// A select blocks until one of its cases can run, then it executes that case.
		// It choose one at random if multiple are ready. Otherwise it choose default branch if none is ready.
		select {
		case <-this.done:
			// 这个分支确保(session)handleLoop gr在(session)handlePackage gr之后退出
			// once.Do(func() { ticker.Stop() }) // use wheel instead, 2016/09/26
			if atomic.LoadInt32(&(this.grNum)) == 1 { // make sure @(session)handlePackage goroutine has been closed.
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
			// 这个条件分支通过(session)rQ排空确保(session)handlePackage gr不会阻塞在(session)rQ上
			if flag {
				this.listener.OnMessage(this, inPkg)
				this.incReadPkgCount()
			} else {
				log.Info("[session.handleLoop] drop readin package{%#v}", inPkg)
			}

		case outPkg = <-this.wQ:
			if flag {
				if err = this.writer.Write(this, outPkg); err != nil {
					log.Error("%s, [session.handleLoop] = error{%+v}", this.sessionToken(), err)
					this.stop()
					flag = false
					// break LOOP
				}
				this.incWritePkgCount()
			} else {
				log.Info("[session.handleLoop] drop writeout package{%#v}", outPkg)
			}

		// case <-ticker.C: // use wheel instead, 2016/09/26
		case <-wheel.After(this.period):
			if flag {
				if wsFlag {
					err = wsConn.writePing()
					log.Debug("wsConn.writePing() = error{%#v}", err)
					if err != nil {
						log.Warn("wsConn.writePing() = error{%#v}", err)
					}
				}
				this.listener.OnCron(this)
			}
		}
	}
	// once.Do(func() { ticker.Stop() }) // use wheel instead, 2016/09/26
}

func (this *session) handlePackage() {
	var (
		err error
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
		this.stop()
		// if this.errFlag {
		if err != nil {
			log.Error("%s, [session.handlePackage] error{%#v}", this.sessionToken(), err)
			this.listener.OnError(this, err)
		}
	}()

	if _, ok := this.Connection.(*gettyTCPConn); ok {
		if this.reader == nil {
			errStr := fmt.Sprintf("session{name:%s, conn:%#v, reader:%#v}", this.name, this.Connection, this.reader)
			log.Error(errStr)
			panic(errStr)
		}

		err = this.handleTCPPackage()
	} else if _, ok := this.Connection.(*gettyWSConn); ok {
		err = this.handleWSPackage()
	}
}

// get package from tcp stream(packet)
func (this *session) handleTCPPackage() error {
	var (
		ok     bool
		err    error
		nerr   net.Error
		conn   *gettyTCPConn
		exit   bool
		bufLen int
		pkgLen int
		buf    []byte
		pktBuf *bytes.Buffer
		pkg    interface{}
	)

	buf = make([]byte, maxReadBufLen)
	pktBuf = new(bytes.Buffer)
	conn = this.Connection.(*gettyTCPConn)
	for {
		if this.IsClosed() {
			err = nil
			break // 退出前不再读取任何packet，buf中剩余的stream bytes也不可能凑够一个package, 所以直接退出
		}

		bufLen = 0
		for {
			// for clause for the network timeout condition check
			// this.conn.SetReadDeadline(time.Now().Add(this.rDeadline))
			bufLen, err = conn.read(buf)
			if err != nil {
				if nerr, ok = err.(net.Error); ok && nerr.Timeout() {
					break
				}
				log.Error("%s, [session.conn.read] = error{%v}", this.sessionToken(), err)
				// for (Codec)OnErr
				// this.errFlag = true
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
			pkg, pkgLen, err = this.reader.Read(this, pktBuf.Bytes())
			if err == nil && this.maxMsgLen > 0 && pkgLen > int(this.maxMsgLen) {
				err = ErrMsgTooLong
			}
			if err != nil {
				log.Warn("%s, [session.handleTCPPackage] = len{%d}, error{%+v}", this.sessionToken(), pkgLen, err)
				// for (Codec)OnErr
				// this.errFlag = true
				exit = true
				break
			}
			if pkg == nil {
				break
			}
			this.UpdateActive()
			this.rQ <- pkg
			pktBuf.Next(pkgLen)
		}
		if exit {
			break
		}
	}

	return err
}

// get package from websocket stream
func (this *session) handleWSPackage() error {
	var (
		ok           bool
		err          error
		nerr         net.Error
		length       int
		conn         *gettyWSConn
		pkg          []byte
		unmarshalPkg interface{}
	)

	conn = this.Connection.(*gettyWSConn)
	for {
		if this.IsClosed() {
			break
		}
		pkg, err = conn.read()
		if nerr, ok = err.(net.Error); ok && nerr.Timeout() {
			continue
		}
		if err != nil {
			log.Warn("%s, [session.handleWSPackage] = error{%+v}", this.sessionToken(), err)
			// this.errFlag = true
			return err
		}
		this.UpdateActive()
		if this.reader != nil {
			unmarshalPkg, length, err = this.reader.Read(this, pkg)
			if err == nil && this.maxMsgLen > 0 && length > int(this.maxMsgLen) {
				err = ErrMsgTooLong
			}
			if err != nil {
				log.Warn("%s, [session.handleWSPackage] = len{%d}, error{%+v}", this.sessionToken(), length, err)
				continue
			}
			this.rQ <- unmarshalPkg
		} else {
			this.rQ <- pkg
		}
	}

	return nil
}

func (this *session) stop() {
	select {
	case <-this.done: // this.done is a blocked channel. if it has not been closed, the default branch will be invoked.
		return

	default:
		this.once.Do(func() {
			// let read/Write timeout asap
			if conn := this.Conn(); conn != nil {
				conn.SetReadDeadline(time.Now().Add(this.readDeadline()))
				conn.SetWriteDeadline(time.Now().Add(this.writeDeadline()))
			}
			close(this.done)
		})
	}
}

func (this *session) gc() {
	this.lock.Lock()
	if this.attrs != nil {
		this.attrs = nil
		close(this.wQ)
		this.wQ = nil
		close(this.rQ)
		this.rQ = nil
		this.Connection.close((int)((int64)(this.wait)))
	}
	this.lock.Unlock()
}

// this function will be invoked by NewSessionCallback(if return error is not nil) or (session)handleLoop automatically.
// It is goroutine-safe to be invoked many times.
func (this *session) Close() {
	this.stop()
	log.Info("%s closed now, its current gr num %d", this.sessionToken(), atomic.LoadInt32(&(this.grNum)))
}
