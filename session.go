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
	"fmt"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

import (
	"github.com/gorilla/websocket"
	perrors "github.com/pkg/errors"
)

const (
	maxReadBufLen         = 4 * 1024
	netIOTimeout          = 1e9      // 1s
	period                = 60 * 1e9 // 1 minute
	pendingDuration       = 3e9
	defaultSessionName    = "session"
	defaultTCPSessionName = "tcp-session"
	defaultUDPSessionName = "udp-session"
	defaultWSSessionName  = "ws-session"
	defaultWSSSessionName = "wss-session"
	outputFormat          = "session %s, Read Bytes: %d, Write Bytes: %d, Read Pkgs: %d, Write Pkgs: %d"
)

/////////////////////////////////////////
// session
/////////////////////////////////////////

// getty base session
type session struct {
	name      string
	endPoint  EndPoint
	maxMsgLen int32
	// net read Write
	Connection
	// pkgHandler ReadWriter
	reader   Reader // @reader should be nil when @conn is a gettyWSConn object.
	writer   Writer
	listener EventListener
	once     sync.Once
	done     chan struct{}
	// errFlag  bool

	period time.Duration
	wait   time.Duration
	rQ     chan interface{}
	wQ     chan interface{}

	// attribute
	attrs *ValuesContext
	// goroutines sync
	grNum int32
	lock  sync.RWMutex
}

func newSession(endPoint EndPoint, conn Connection) *session {
	ss := &session{
		name:       defaultSessionName,
		endPoint:   endPoint,
		maxMsgLen:  maxReadBufLen,
		Connection: conn,
		done:       make(chan struct{}),
		period:     period,
		wait:       pendingDuration,
		attrs:      NewValuesContext(nil),
	}

	ss.Connection.setSession(ss)
	ss.SetWriteTimeout(netIOTimeout)
	ss.SetReadTimeout(netIOTimeout)

	return ss
}

func newTCPSession(conn net.Conn, endPoint EndPoint) Session {
	c := newGettyTCPConn(conn)
	session := newSession(endPoint, c)
	session.name = defaultTCPSessionName

	return session
}

func newUDPSession(conn *net.UDPConn, endPoint EndPoint) Session {
	c := newGettyUDPConn(conn)
	session := newSession(endPoint, c)
	session.name = defaultUDPSessionName

	return session
}

func newWSSession(conn *websocket.Conn, endPoint EndPoint) Session {
	c := newGettyWSConn(conn)
	session := newSession(endPoint, c)
	session.name = defaultWSSessionName

	return session
}

func (s *session) Reset() {
	s.name = defaultSessionName
	s.once = sync.Once{}
	s.done = make(chan struct{})
	// s.errFlag = false
	s.period = period
	s.wait = pendingDuration
	s.attrs = NewValuesContext(nil)
	s.grNum = 0

	s.SetWriteTimeout(netIOTimeout)
	s.SetReadTimeout(netIOTimeout)
}

// func (s *session) SetConn(conn net.Conn) { s.gettyConn = newGettyConn(conn) }
func (s *session) Conn() net.Conn {
	if tc, ok := s.Connection.(*gettyTCPConn); ok {
		return tc.conn
	}

	if uc, ok := s.Connection.(*gettyUDPConn); ok {
		return uc.conn
	}

	if wc, ok := s.Connection.(*gettyWSConn); ok {
		return wc.conn.UnderlyingConn()
	}

	return nil
}

func (s *session) EndPoint() EndPoint {
	return s.endPoint
}

func (s *session) gettyConn() *gettyConn {
	if tc, ok := s.Connection.(*gettyTCPConn); ok {
		return &(tc.gettyConn)
	}

	if uc, ok := s.Connection.(*gettyUDPConn); ok {
		return &(uc.gettyConn)
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
		atomic.LoadUint32(&(conn.readBytes)),
		atomic.LoadUint32(&(conn.writeBytes)),
		atomic.LoadUint32(&(conn.readPkgNum)),
		atomic.LoadUint32(&(conn.writePkgNum)),
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
	s.lock.Unlock()
	log.Debugf("%s, [session.SetRQLen] rQ{len:%d, cap:%d}", s.Stat(), len(s.rQ), cap(s.rQ))
}

// set @session's Write queue size
func (s *session) SetWQLen(writeQLen int) {
	if writeQLen < 1 {
		panic("@writeQLen < 1")
	}

	s.lock.Lock()
	s.wQ = make(chan interface{}, writeQLen)
	s.lock.Unlock()
	log.Debugf("%s, [session.SetWQLen] wQ{len:%d, cap:%d}", s.Stat(), len(s.wQ), cap(s.wQ))
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
func (s *session) GetAttribute(key interface{}) interface{} {
	s.lock.RLock()
	if s.attrs == nil {
		s.lock.RUnlock()
		return nil
	}
	ret, flag := s.attrs.Get(key)
	s.lock.RUnlock()

	if !flag {
		return nil
	}

	return ret
}

// get attribute of key @session:key
func (s *session) SetAttribute(key interface{}, value interface{}) {
	s.lock.Lock()
	if s.attrs != nil {
		s.attrs.Set(key, value)
	}
	s.lock.Unlock()
}

// delete attribute of key @session:key
func (s *session) RemoveAttribute(key interface{}) {
	s.lock.Lock()
	if s.attrs != nil {
		s.attrs.Delete(key)
	}
	s.lock.Unlock()
}

func (s *session) sessionToken() string {
	return fmt.Sprintf("{%s:%s:%d:%s<->%s}",
		s.name, s.EndPoint().EndPointType(), s.ID(), s.LocalAddr(), s.RemoteAddr())
}

func (s *session) WritePkg(pkg interface{}, timeout time.Duration) error {
	if s.IsClosed() {
		return ErrSessionClosed
	}

	defer func() {
		if r := recover(); r != nil {
			const size = 64 << 10
			rBuf := make([]byte, size)
			rBuf = rBuf[:runtime.Stack(rBuf, false)]
			log.Errorf("[session.WritePkg] panic session %s: err=%s\n%s", s.sessionToken(), r, rBuf)
		}
	}()

	var err error
	if timeout <= 0 {
		if err = s.writer.Write(s, pkg); err != nil {
			s.incWritePkgNum()
			// gxlog.CError("after incWritePkgNum, ss:%s", s.Stat())
		}
		return perrors.WithStack(err)
	}
	select {
	case s.wQ <- pkg:
		break // for possible gen a new pkg

	case <-time.After(timeout):
		log.Warnf("%s, [session.WritePkg] wQ{len:%d, cap:%d}", s.Stat(), len(s.wQ), cap(s.wQ))
		return ErrSessionBlocked
	}

	return nil
}

// for codecs
func (s *session) WriteBytes(pkg []byte) error {
	if s.IsClosed() {
		return ErrSessionClosed
	}

	// s.conn.SetWriteTimeout(time.Now().Add(s.wTimeout))
	if _, err := s.Connection.Write(pkg); err != nil {
		return perrors.Wrapf(err, "s.Connection.Write(pkg len:%d)", len(pkg))
	}

	s.incWritePkgNum()

	return nil
}

// Write multiple packages at once
func (s *session) WriteBytesArray(pkgs ...[]byte) error {
	if s.IsClosed() {
		return ErrSessionClosed
	}
	// s.conn.SetWriteTimeout(time.Now().Add(s.wTimeout))
	if len(pkgs) == 1 {
		// return s.Connection.Write(pkgs[0])
		return s.WriteBytes(pkgs[0])
	}

	// get len
	var (
		l      int
		err    error
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
	if err = s.WriteBytes(arr); err != nil {
		return perrors.WithStack(err)
	}

	num := len(pkgs) - 1
	for i := 0; i < num; i++ {
		s.incWritePkgNum()
		// gxlog.CError("after write, ss:%s", s.Stat())
	}

	return nil
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
		counter CountWatch
		inPkg   interface{}
		outPkg  interface{}
	)

	defer func() {
		var grNum int32

		if r := recover(); r != nil {
			const size = 64 << 10
			rBuf := make([]byte, size)
			rBuf = rBuf[:runtime.Stack(rBuf, false)]
			log.Errorf("[session.handleLoop] panic session %s: err=%s\n%s", s.sessionToken(), r, rBuf)
		}

		grNum = atomic.AddInt32(&(s.grNum), -1)
		// if !s.errFlag {
		s.listener.OnClose(s)
		// }
		log.Infof("%s, [session.handleLoop] goroutine exit now, left gr num %d", s.Stat(), grNum)
		s.gc()
	}()

	flag = true // do not do any read/Write/cron operation while got Write error
	wsConn, wsFlag = s.Connection.(*gettyWSConn)
LOOP:
	for {
		// A select blocks until one of its cases is ready to run.
		// It choose one at random if multiple are ready. Otherwise it choose default branch if none is ready.
		select {
		case <-s.done:
			// this case branch assure the (session)handleLoop gr will exit before (session)handlePackage gr.
			if atomic.LoadInt32(&(s.grNum)) == 1 { // make sure @(session)handlePackage goroutine has been closed.
				if len(s.rQ) == 0 && len(s.wQ) == 0 {
					log.Infof("%s, [session.handleLoop] got done signal. Both rQ and wQ are nil.", s.Stat())
					break LOOP
				}
				counter.Start()
				// if time.Since(start).Nanoseconds() >= s.wait.Nanoseconds() {
				if counter.Count() > s.wait.Nanoseconds() {
					log.Infof("%s, [session.handleLoop] got done signal ", s.Stat())
					break LOOP
				}
			}

		case inPkg = <-s.rQ:
			// read the s.rQ and assure (session)handlePackage gr will not block by (session)rQ.
			if flag {
				log.Debugf("%#v <-s.rQ", inPkg)
				pkg := inPkg
				// go s.listener.OnMessage(s, pkg)
				s.listener.OnMessage(s, pkg)
				s.incReadPkgNum()
			} else {
				log.Infof("[session.handleLoop] drop readin package{%#v}", inPkg)
			}

		case outPkg = <-s.wQ:
			if flag {
				if err = s.writer.Write(s, outPkg); err != nil {
					log.Errorf("%s, [session.handleLoop] = error:%+v", s.sessionToken(), err)
					s.stop()
					flag = false
					// break LOOP
				}
				s.incWritePkgNum()
			} else {
				log.Infof("[session.handleLoop] drop writeout package{%#v}", outPkg)
			}

		case <-time.After(s.period):
			if flag {
				if wsFlag {
					err := wsConn.writePing()
					if err != nil {
						log.Warnf("wsConn.writePing() = error{%s}", err)
					}
				}
				s.listener.OnCron(s)
			}
		}
	}
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
			log.Errorf("[session.handlePackage] panic session %s: err=%s\n%s", s.sessionToken(), r, rBuf)
		}

		grNum = atomic.AddInt32(&(s.grNum), -1)
		log.Infof("%s, [session.handlePackage] gr will exit now, left gr num %d", s.sessionToken(), grNum)
		s.stop()
		if err != nil {
			log.Errorf("%s, [session.handlePackage] error:%+v", s.sessionToken(), err)
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
	} else if _, ok := s.Connection.(*gettyUDPConn); ok {
		err = s.handleUDPPackage()
	} else {
		panic(fmt.Sprintf("unknown type session{%#v}", s))
	}
}

// get package from tcp stream(packet)
func (s *session) handleTCPPackage() error {
	var (
		ok       bool
		err      error
		netError net.Error
		conn     *gettyTCPConn
		exit     bool
		bufLen   int
		pkgLen   int
		buf      []byte
		pktBuf   *bytes.Buffer
		pkg      interface{}
	)

	buf = make([]byte, maxReadBufLen)
	pktBuf = new(bytes.Buffer)
	conn = s.Connection.(*gettyTCPConn)
	for {
		if s.IsClosed() {
			err = nil
			// do not handle the left stream in pktBuf and exit asap.
			// it is impossible packing a package by the left stream.
			break
		}

		bufLen = 0
		for {
			// for clause for the network timeout condition check
			// s.conn.SetReadTimeout(time.Now().Add(s.rTimeout))
			bufLen, err = conn.read(buf)
			if err != nil {
				if netError, ok = perrors.Cause(err).(net.Error); ok && netError.Timeout() {
					break
				}
				log.Errorf("%s, [session.conn.read] = error:%+v", s.sessionToken(), err)
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
			continue // just continue if session can not read no more stream bytes.
		}
		pktBuf.Write(buf[:bufLen])
		for {
			if pktBuf.Len() <= 0 {
				break
			}
			// pkg, err = s.pkgHandler.Read(s, pktBuf)
			pkg, pkgLen, err = s.reader.Read(s, pktBuf.Bytes())
			if err == nil && s.maxMsgLen > 0 && pkgLen > int(s.maxMsgLen) {
				err = perrors.Errorf("pkgLen %d > session max message len %d", pkgLen, s.maxMsgLen)
			}
			// handle case 1
			if err != nil {
				log.Warnf("%s, [session.handleTCPPackage] = len{%d}, error:%+v",
					s.sessionToken(), pkgLen, err)
				// for (Codec)OnErr
				// s.errFlag = true
				exit = true
				break
			}
			// handle case 2
			if pkg == nil {
				break
			}
			// handle case 3
			s.UpdateActive()
			s.rQ <- pkg
			pktBuf.Next(pkgLen)
			// continue to handle case 4
		}
		if exit {
			break
		}
	}

	return perrors.WithStack(err)
}

// get package from udp packet
func (s *session) handleUDPPackage() error {
	var (
		ok       bool
		err      error
		netError net.Error
		conn     *gettyUDPConn
		bufLen   int
		buf      []byte
		addr     *net.UDPAddr
		pkgLen   int
		pkg      interface{}
	)

	conn = s.Connection.(*gettyUDPConn)
	bufLen = int(s.maxMsgLen + maxReadBufLen)
	if int(s.maxMsgLen<<1) < bufLen {
		bufLen = int(s.maxMsgLen << 1)
	}
	buf = make([]byte, bufLen)
	for {
		if s.IsClosed() {
			break
		}

		bufLen, addr, err = conn.read(buf)
		log.Debugf("conn.read() = bufLen:%d, addr:%#v, err:%+v", bufLen, addr, err)
		if netError, ok = perrors.Cause(err).(net.Error); ok && netError.Timeout() {
			continue
		}
		if err != nil {
			log.Errorf("%s, [session.handleUDPPackage] = len{%d}, error{%+s}",
				s.sessionToken(), bufLen, err)
			err = perrors.Wrapf(err, "conn.read()")
			break
		}

		if bufLen == 0 {
			log.Errorf("conn.read() = bufLen:%d, addr:%s, err:%+v", bufLen, addr, err)
			continue
		}

		if bufLen == len(connectPingPackage) && bytes.Equal(connectPingPackage, buf[:bufLen]) {
			log.Infof("got %s connectPingPackage", addr)
			continue
		}

		pkg, pkgLen, err = s.reader.Read(s, buf[:bufLen])
		log.Debugf("s.reader.Read() = pkg:%#v, pkgLen:%d, err:%+v", pkg, pkgLen, err)
		if err == nil && s.maxMsgLen > 0 && bufLen > int(s.maxMsgLen) {
			err = perrors.Errorf("Message Too Long, bufLen %d, session max message len %d", bufLen, s.maxMsgLen)
		}
		if err != nil {
			log.Warnf("%s, [session.handleUDPPackage] = len{%d}, error:%+v",
				s.sessionToken(), pkgLen, err)
			continue
		}
		if pkgLen == 0 {
			log.Errorf("s.reader.Read() = pkg:%#v, pkgLen:%d, err:%+v", pkg, pkgLen, err)
			continue
		}

		s.UpdateActive()
		s.rQ <- UDPContext{Pkg: pkg, PeerAddr: addr}
	}

	return perrors.WithStack(err)
}

// get package from websocket stream
func (s *session) handleWSPackage() error {
	var (
		ok           bool
		err          error
		netError     net.Error
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
		if netError, ok = perrors.Cause(err).(net.Error); ok && netError.Timeout() {
			continue
		}
		if err != nil {
			log.Warnf("%s, [session.handleWSPackage] = error{%+s}",
				s.sessionToken(), err)
			// s.errFlag = true
			return perrors.WithStack(err)
		}
		s.UpdateActive()
		if s.reader != nil {
			unmarshalPkg, length, err = s.reader.Read(s, pkg)
			if err == nil && s.maxMsgLen > 0 && length > int(s.maxMsgLen) {
				err = perrors.Errorf("Message Too Long, length %d, session max message len %d", length, s.maxMsgLen)
			}
			if err != nil {
				log.Warnf("%s, [session.handleWSPackage] = len{%d}, error:%+v",
					s.sessionToken(), length, err)
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
			now := time.Now()
			if conn := s.Conn(); conn != nil {
				conn.SetReadDeadline(now.Add(s.readTimeout()))
				conn.SetWriteDeadline(now.Add(s.writeTimeout()))
			}
			close(s.done)
			c := s.GetAttribute(sessionClientKey)
			if clt, ok := c.(*client); ok {
				clt.reConnect()
			}
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

// Close will be invoked by NewSessionCallback(if return error is not nil)
// or (session)handleLoop automatically. It's thread safe.
func (s *session) Close() {
	s.stop()
	log.Infof("%s closed now. its current gr num is %d", s.sessionToken(), atomic.LoadInt32(&(s.grNum)))
}
