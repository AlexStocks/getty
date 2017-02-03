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

func (s *session) Reset() {
	s.name = defaultSessionName
	s.once = sync.Once{}
	s.done = make(chan gxsync.Empty)
	// s.errFlag = false
	s.period = period
	s.wait = pendingDuration
	s.attrs = make(map[string]interface{})
	s.grNum = 0

	s.SetWriteDeadline(netIOTimeout)
	s.SetReadDeadline(netIOTimeout)
}

// func (s *session) SetConn(conn net.Conn) { s.gettyConn = newGettyConn(conn) }
func (s *session) Conn() net.Conn {
	if tc, ok := s.Connection.(*gettyTCPConn); ok {
		return tc.conn
	}

	if wc, ok := s.Connection.(*gettyWSConn); ok {
		return wc.conn.UnderlyingConn()
	}

	return nil
}

func (s *session) gettyConn() *gettyConn {
	if tc, ok := s.Connection.(*gettyTCPConn); ok {
		return &(tc.gettyConn)
	}

	if wc, ok := s.Connection.(*gettyWSConn); ok {
		return &(wc.gettyConn)
	}

	return nil
}

// return the connect statistic data
func (s *session) Stat() string {
	var conn *gettyConn
	if conn = s.gettyConn(); conn == nil {
		return ""
	}
	return fmt.Sprintf(
		outputFormat,
		s.sessionToken(),
		atomic.LoadUint32(&(conn.readCount)),
		atomic.LoadUint32(&(conn.writeCount)),
		atomic.LoadUint32(&(conn.readPkgCount)),
		atomic.LoadUint32(&(conn.writePkgCount)),
	)
}

// check whether the session has been closed.
func (s *session) IsClosed() bool {
	select {
	case <-s.done:
		return true

	default:
		return false
	}
}

// set maximum pacakge length of every pacakge in (EventListener)OnMessage(@pkgs)
func (s *session) SetMaxMsgLen(length int) { s.maxMsgLen = int32(length) }

// set session name
func (s *session) SetName(name string) { s.name = name }

// set EventListener
func (s *session) SetEventListener(listener EventListener) {
	s.listener = listener
}

// set package handler
func (s *session) SetPkgHandler(handler ReadWriter) {
	s.reader = handler
	s.writer = handler
	// s.pkgHandler = handler
}

// set Reader
func (s *session) SetReader(reader Reader) {
	s.reader = reader
}

// set Writer
func (s *session) SetWriter(writer Writer) {
	s.writer = writer
}

// period is in millisecond. Websocket session will send ping frame automatically every peroid.
func (s *session) SetCronPeriod(period int) {
	if period < 1 {
		panic("@period < 1")
	}

	s.lock.Lock()
	s.period = time.Duration(period) * time.Millisecond
	s.lock.Unlock()
}

// set @session's read queue size
func (s *session) SetRQLen(readQLen int) {
	if readQLen < 1 {
		panic("@readQLen < 1")
	}

	s.lock.Lock()
	s.rQ = make(chan interface{}, readQLen)
	log.Info("%s, [session.SetRQLen] rQ{len:%d, cap:%d}", s.Stat(), len(s.rQ), cap(s.rQ))
	s.lock.Unlock()
}

// set @session's Write queue size
func (s *session) SetWQLen(writeQLen int) {
	if writeQLen < 1 {
		panic("@writeQLen < 1")
	}

	s.lock.Lock()
	s.wQ = make(chan interface{}, writeQLen)
	log.Info("%s, [session.SetWQLen] wQ{len:%d, cap:%d}", s.Stat(), len(s.wQ), cap(s.wQ))
	s.lock.Unlock()
}

// set maximum wait time when session got error or got exit signal
func (s *session) SetWaitTime(waitTime time.Duration) {
	if waitTime < 1 {
		panic("@wait < 1")
	}

	s.lock.Lock()
	s.wait = waitTime
	s.lock.Unlock()
}

// set attribute of key @session:key
func (s *session) GetAttribute(key string) interface{} {
	var ret interface{}
	s.lock.RLock()
	ret = s.attrs[key]
	s.lock.RUnlock()
	return ret
}

// get attribute of key @session:key
func (s *session) SetAttribute(key string, value interface{}) {
	s.lock.Lock()
	s.attrs[key] = value
	s.lock.Unlock()
}

// delete attribute of key @session:key
func (s *session) RemoveAttribute(key string) {
	s.lock.Lock()
	delete(s.attrs, key)
	s.lock.Unlock()
}

func (s *session) sessionToken() string {
	return fmt.Sprintf("{%s:%d:%s<->%s}", s.name, s.ID(), s.LocalAddr(), s.RemoteAddr())
}

// Queued Write, for handler
func (s *session) WritePkg(pkg interface{}) error {
	if s.IsClosed() {
		return ErrSessionClosed
	}

	defer func() {
		if r := recover(); r != nil {
			const size = 64 << 10
			rBuf := make([]byte, size)
			rBuf = rBuf[:runtime.Stack(rBuf, false)]
			log.Error("[session.WritePkg] panic session %s: err=%#v\n%s", s.sessionToken(), r, rBuf)
		}
	}()

	var d = s.writeDeadline()
	if d > netIOTimeout {
		d = netIOTimeout
	}
	select {
	case s.wQ <- pkg:
		break // for possible gen a new pkg

	// default:
	// case <-time.After(s.wDeadline):
	// case <-time.After(netIOTimeout):
	case <-wheel.After(d):
		log.Warn("%s, [session.WritePkg] wQ{len:%d, cap:%d}", s.Stat(), len(s.wQ), cap(s.wQ))
		return ErrSessionBlocked
	}

	return nil
}

// for codecs
func (s *session) WriteBytes(pkg []byte) error {
	if s.IsClosed() {
		return ErrSessionClosed
	}

	// s.conn.SetWriteDeadline(time.Now().Add(s.wDeadline))
	return s.Connection.Write(pkg)
}

// Write multiple packages at once
func (s *session) WriteBytesArray(pkgs ...[]byte) error {
	if s.IsClosed() {
		return ErrSessionClosed
	}
	// s.conn.SetWriteDeadline(time.Now().Add(s.wDeadline))

	if len(pkgs) == 1 {
		return s.Connection.Write(pkgs[0])
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

	// return s.Connection.Write(arr)
	return s.WriteBytes(arr)
}

// func (s *session) RunEventLoop() {
func (s *session) run() {
	if s.rQ == nil || s.wQ == nil {
		errStr := fmt.Sprintf("session{name:%s, rQ:%#v, wQ:%#v}",
			s.name, s.rQ, s.wQ)
		log.Error(errStr)
		panic(errStr)
	}
	if s.Connection == nil || s.listener == nil || s.writer == nil {
		errStr := fmt.Sprintf("session{name:%s, conn:%#v, listener:%#v, writer:%#v}",
			s.name, s.Connection, s.listener, s.writer)
		log.Error(errStr)
		panic(errStr)
	}

	// call session opened
	s.UpdateActive()
	if err := s.listener.OnOpen(s); err != nil {
		s.Close()
		return
	}

	atomic.AddInt32(&(s.grNum), 2)
	go s.handleLoop()
	go s.handlePackage()
}

func (s *session) handleLoop() {
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
			log.Error("[session.handleLoop] panic session %s: err=%#v\n%s", s.sessionToken(), r, rBuf)
		}

		grNum = atomic.AddInt32(&(s.grNum), -1)
		// if !s.errFlag {
		s.listener.OnClose(s)
		// }
		log.Info("%s, [session.handleLoop] goroutine exit now, left gr num %d", s.Stat(), grNum)
		s.gc()
	}()

	wsConn, wsFlag = s.Connection.(*gettyWSConn)
	flag = true // do not do any read/Write/cron operation while got Write error
	// ticker = time.NewTicker(s.period) // use wheel instead, 2016/09/26
LOOP:
	for {
		// A select blocks until one of its cases can run, then it executes that case.
		// It choose one at random if multiple are ready. Otherwise it choose default branch if none is ready.
		select {
		case <-s.done:
			// 这个分支确保(session)handleLoop gr在(session)handlePackage gr之后退出
			// once.Do(func() { ticker.Stop() }) // use wheel instead, 2016/09/26
			if atomic.LoadInt32(&(s.grNum)) == 1 { // make sure @(session)handlePackage goroutine has been closed.
				if len(s.rQ) == 0 && len(s.wQ) == 0 {
					log.Info("%s, [session.handleLoop] got done signal. Both rQ and wQ are nil.", s.Stat())
					break LOOP
				}
				counter.Start()
				// if time.Since(start).Nanoseconds() >= s.wait.Nanoseconds() {
				if counter.Count() > s.wait.Nanoseconds() {
					log.Info("%s, [session.handleLoop] got done signal ", s.Stat())
					break LOOP
				}
			}

		case inPkg = <-s.rQ:
			// 这个条件分支通过(session)rQ排空确保(session)handlePackage gr不会阻塞在(session)rQ上
			if flag {
				s.listener.OnMessage(s, inPkg)
				s.incReadPkgCount()
			} else {
				log.Info("[session.handleLoop] drop readin package{%#v}", inPkg)
			}

		case outPkg = <-s.wQ:
			if flag {
				if err = s.writer.Write(s, outPkg); err != nil {
					log.Error("%s, [session.handleLoop] = error{%+v}", s.sessionToken(), err)
					s.stop()
					flag = false
					// break LOOP
				}
				s.incWritePkgCount()
			} else {
				log.Info("[session.handleLoop] drop writeout package{%#v}", outPkg)
			}

		// case <-ticker.C: // use wheel instead, 2016/09/26
		case <-wheel.After(s.period):
			if flag {
				if wsFlag {
					err = wsConn.writePing()
					log.Debug("wsConn.writePing() = error{%#v}", err)
					if err != nil {
						log.Warn("wsConn.writePing() = error{%#v}", err)
					}
				}
				s.listener.OnCron(s)
			}
		}
	}
	// once.Do(func() { ticker.Stop() }) // use wheel instead, 2016/09/26
}

func (s *session) handlePackage() {
	var (
		err error
	)

	defer func() {
		var grNum int32

		if r := recover(); r != nil {
			const size = 64 << 10
			rBuf := make([]byte, size)
			rBuf = rBuf[:runtime.Stack(rBuf, false)]
			log.Error("[session.handlePackage] panic session %s: err=%#v\n%s", s.sessionToken(), r, rBuf)
		}

		grNum = atomic.AddInt32(&(s.grNum), -1)
		log.Info("%s, [session.handlePackage] gr will exit now, left gr num %d", s.sessionToken(), grNum)
		s.stop()
		// if s.errFlag {
		if err != nil {
			log.Error("%s, [session.handlePackage] error{%#v}", s.sessionToken(), err)
			s.listener.OnError(s, err)
		}
	}()

	if _, ok := s.Connection.(*gettyTCPConn); ok {
		if s.reader == nil {
			errStr := fmt.Sprintf("session{name:%s, conn:%#v, reader:%#v}", s.name, s.Connection, s.reader)
			log.Error(errStr)
			panic(errStr)
		}

		err = s.handleTCPPackage()
	} else if _, ok := s.Connection.(*gettyWSConn); ok {
		err = s.handleWSPackage()
	}
}

// get package from tcp stream(packet)
func (s *session) handleTCPPackage() error {
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
	conn = s.Connection.(*gettyTCPConn)
	for {
		if s.IsClosed() {
			err = nil
			break // 退出前不再读取任何packet，buf中剩余的stream bytes也不可能凑够一个package, 所以直接退出
		}

		bufLen = 0
		for {
			// for clause for the network timeout condition check
			// s.conn.SetReadDeadline(time.Now().Add(s.rDeadline))
			bufLen, err = conn.read(buf)
			if err != nil {
				if nerr, ok = err.(net.Error); ok && nerr.Timeout() {
					break
				}
				log.Error("%s, [session.conn.read] = error{%v}", s.sessionToken(), err)
				// for (Codec)OnErr
				// s.errFlag = true
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
			// pkg, err = s.pkgHandler.Read(s, pktBuf)
			pkg, pkgLen, err = s.reader.Read(s, pktBuf.Bytes())
			if err == nil && s.maxMsgLen > 0 && pkgLen > int(s.maxMsgLen) {
				err = ErrMsgTooLong
			}
			if err != nil {
				log.Warn("%s, [session.handleTCPPackage] = len{%d}, error{%+v}", s.sessionToken(), pkgLen, err)
				// for (Codec)OnErr
				// s.errFlag = true
				exit = true
				break
			}
			if pkg == nil {
				break
			}
			s.UpdateActive()
			s.rQ <- pkg
			pktBuf.Next(pkgLen)
		}
		if exit {
			break
		}
	}

	return err
}

// get package from websocket stream
func (s *session) handleWSPackage() error {
	var (
		ok           bool
		err          error
		nerr         net.Error
		length       int
		conn         *gettyWSConn
		pkg          []byte
		unmarshalPkg interface{}
	)

	conn = s.Connection.(*gettyWSConn)
	for {
		if s.IsClosed() {
			break
		}
		pkg, err = conn.read()
		if nerr, ok = err.(net.Error); ok && nerr.Timeout() {
			continue
		}
		if err != nil {
			log.Warn("%s, [session.handleWSPackage] = error{%+v}", s.sessionToken(), err)
			// s.errFlag = true
			return err
		}
		s.UpdateActive()
		if s.reader != nil {
			unmarshalPkg, length, err = s.reader.Read(s, pkg)
			if err == nil && s.maxMsgLen > 0 && length > int(s.maxMsgLen) {
				err = ErrMsgTooLong
			}
			if err != nil {
				log.Warn("%s, [session.handleWSPackage] = len{%d}, error{%+v}", s.sessionToken(), length, err)
				continue
			}
			s.rQ <- unmarshalPkg
		} else {
			s.rQ <- pkg
		}
	}

	return nil
}

func (s *session) stop() {
	select {
	case <-s.done: // s.done is a blocked channel. if it has not been closed, the default branch will be invoked.
		return

	default:
		s.once.Do(func() {
			// let read/Write timeout asap
			if conn := s.Conn(); conn != nil {
				conn.SetReadDeadline(time.Now().Add(s.readDeadline()))
				conn.SetWriteDeadline(time.Now().Add(s.writeDeadline()))
			}
			close(s.done)
		})
	}
}

func (s *session) gc() {
	s.lock.Lock()
	if s.attrs != nil {
		s.attrs = nil
		close(s.wQ)
		s.wQ = nil
		close(s.rQ)
		s.rQ = nil
		s.Connection.close((int)((int64)(s.wait)))
	}
	s.lock.Unlock()
}

// s function will be invoked by NewSessionCallback(if return error is not nil) or (session)handleLoop automatically.
// It is goroutine-safe to be invoked many times.
func (s *session) Close() {
	s.stop()
	log.Info("%s closed now, its current gr num %d", s.sessionToken(), atomic.LoadInt32(&(s.grNum)))
}
