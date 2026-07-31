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
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"time"
)

import (
	gxbytes "github.com/dubbogo/gost/bytes"
	gxcontext "github.com/dubbogo/gost/context"
	gxtime "github.com/dubbogo/gost/time"

	"github.com/gorilla/websocket"

	perrors "github.com/pkg/errors"

	uatomic "go.uber.org/atomic"
)

import (
	log "github.com/AlexStocks/getty/util"
)

const (
	maxReadBufLen   = 4 * 1024
	netIOTimeout    = 1e9      // 1s
	period          = 60 * 1e9 // 1 minute
	pendingDuration = 3e9
	// MaxWheelTimeSpan 900s, 15 minute
	MaxWheelTimeSpan = 900e9
	maxPacketLen     = 16 * 1024

	defaultTLSHandshakeTimeout = time.Second * 3

	defaultSessionName    = "session"
	defaultTCPSessionName = "tcp-session"
	defaultUDPSessionName = "udp-session"
	defaultWSSessionName  = "ws-session"
	defaultWSSSessionName = "wss-session"
	outputFormat          = "session %s, Read Bytes: %d, Write Bytes: %d, Read Pkgs: %d, Write Pkgs: %d"
)

var defaultTimerWheel *gxtime.TimerWheel

func init() {
	gxtime.InitDefaultTimerWheel()
	defaultTimerWheel = gxtime.GetDefaultTimerWheel()
}

// Session wrap connection between the server and the client
type Session interface {
	Connection
	Reset()
	Conn() net.Conn
	Stat() string
	IsClosed() bool
	// EndPoint get endpoint type
	EndPoint() EndPoint
	SetMaxMsgLen(int)
	SetName(string)
	SetEventListener(EventListener)
	SetPkgHandler(ReadWriter)
	SetReader(Reader)
	SetWriter(Writer)
	SetCronPeriod(int)
	SetWaitTime(time.Duration)
	GetAttribute(any) any
	SetAttribute(any, any)
	RemoveAttribute(any)

	// WritePkg the Writer will invoke this function. Pls attention that if timeout is less than 0, WritePkg will send @pkg asap.
	// for udp session, the first parameter should be UDPContext.
	// totalBytesLength: @pkg stream bytes length after encoding @pkg.
	// sendBytesLength: stream bytes length that sent out successfully.
	// err: maybe it has illegal data, encoding error, or write out system error.
	WritePkg(pkg any, timeout time.Duration) (totalBytesLength int, sendBytesLength int, err error)
	WriteBytes([]byte) (int, error)
	WriteBytesArray(...[]byte) (int, error)
	Close()

	AddCloseCallback(handler, key any, callback CallBackFunc)
	RemoveCloseCallback(handler, key any)
}

// getty base session
type session struct {
	name     string
	endPoint EndPoint

	// net read Write
	Connection

	listener EventListener

	// codec
	reader Reader // @reader should be nil when @conn is a gettyWSConn object.
	writer Writer

	// handle logic
	maxMsgLen int32

	// heartbeat
	period         time.Duration
	heartbeatTimer *gxtime.Timer
	lifecycle      *sessionLifecycle

	// done
	wait time.Duration
	once *sync.Once
	done chan struct{}

	// attribute
	attrs *gxcontext.ValuesContext

	// goroutines sync
	grNum      uatomic.Int32
	grWG       sync.WaitGroup
	lock       sync.RWMutex
	packetLock sync.RWMutex

	// callbacks
	closeCallback      callbacks
	closeCallbackMutex sync.RWMutex
}

type sessionLifecycle struct {
	lock   sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func newSessionLifecycle() *sessionLifecycle {
	return &sessionLifecycle{}
}

func (l *sessionLifecycle) acquire() bool {
	l.lock.Lock()
	defer l.lock.Unlock()
	if l.closed {
		return false
	}
	l.wg.Add(1)
	return true
}

func (l *sessionLifecycle) release() {
	l.wg.Done()
}

func (l *sessionLifecycle) addClosingTask() {
	l.lock.Lock()
	l.wg.Add(1)
	l.lock.Unlock()
}

func (l *sessionLifecycle) close() {
	l.lock.Lock()
	l.closed = true
	l.lock.Unlock()
}

func (l *sessionLifecycle) wait() {
	l.wg.Wait()
}

type heartbeatContext struct {
	session   *session
	lifecycle *sessionLifecycle
}

func newSession(endPoint EndPoint, conn Connection) *session {
	ss := &session{
		name:     defaultSessionName,
		endPoint: endPoint,

		Connection: conn,

		maxMsgLen: maxReadBufLen,

		period:    period,
		lifecycle: newSessionLifecycle(),

		once:  &sync.Once{},
		done:  make(chan struct{}),
		wait:  pendingDuration,
		attrs: gxcontext.NewValuesContext(context.Background()),
	}

	ss.Connection.SetSession(ss)
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
	lifecycle := s.stopHeartbeat()
	s.grWG.Wait()
	if lifecycle != nil {
		lifecycle.wait()
	}
	// #105: Reset() previously did `*s = session{...}` without holding s.lock,
	// racing with concurrent readers. Reset the fields individually under the
	// lock instead; replacing the whole struct would also copy the mutexes
	// (flagged by `go vet`).
	s.closeCallbackMutex.Lock()
	defer s.closeCallbackMutex.Unlock()
	s.lock.Lock()
	defer s.lock.Unlock()
	s.name = defaultSessionName
	s.endPoint = nil
	s.Connection = nil
	s.listener = nil
	s.reader = nil
	s.writer = nil
	s.maxMsgLen = 0
	s.once = &sync.Once{}
	s.done = make(chan struct{})
	s.period = period
	s.heartbeatTimer = nil
	s.lifecycle = newSessionLifecycle()
	s.wait = pendingDuration
	s.attrs = gxcontext.NewValuesContext(context.Background())
	s.grNum.Store(0)
	s.closeCallback = callbacks{}
}

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
	s.lock.RLock()
	defer s.lock.RUnlock()
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

// Stat get the connect statistic data
func (s *session) Stat() string {
	var conn *gettyConn
	if conn = s.gettyConn(); conn == nil {
		return ""
	}
	return fmt.Sprintf(
		outputFormat,
		s.sessionToken(),
		conn.readBytes.Load(),
		conn.writeBytes.Load(),
		conn.readPkgNum.Load(),
		conn.writePkgNum.Load(),
	)
}

// IsClosed check whether the session has been closed.
func (s *session) IsClosed() bool {
	s.lock.RLock()
	done := s.done
	s.lock.RUnlock()
	select {
	case <-done:
		return true

	default:
		return false
	}
}

// SetMaxMsgLen set maximum package length of every package in (EventListener)OnMessage(@pkgs)
func (s *session) SetMaxMsgLen(length int) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.maxMsgLen = int32(length)
}

// SetName set session name
func (s *session) SetName(name string) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.name = name
}

// SetEventListener set event listener
func (s *session) SetEventListener(listener EventListener) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.listener = listener
}

// SetPkgHandler set package handler
func (s *session) SetPkgHandler(handler ReadWriter) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.reader = handler
	s.writer = handler
}

func (s *session) SetReader(reader Reader) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.reader = reader
}

func (s *session) SetWriter(writer Writer) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.writer = writer
}

// SetCronPeriod period is in millisecond. Websocket session will send ping frame automatically every peroid.
func (s *session) SetCronPeriod(period int) {
	if period < 1 {
		panic("@period < 1")
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	s.period = time.Duration(period) * time.Millisecond
}

// SetWaitTime set maximum wait time when session got error or got exit signal
func (s *session) SetWaitTime(waitTime time.Duration) {
	if waitTime < 1 {
		panic("@wait < 1")
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	s.wait = waitTime
}

// GetAttribute get attribute of key @session:key
func (s *session) GetAttribute(key any) any {
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

// SetAttribute set attribute of key @session:key
func (s *session) SetAttribute(key any, value any) {
	s.lock.Lock()
	if s.attrs != nil {
		s.attrs.Set(key, value)
	}
	s.lock.Unlock()
}

// RemoveAttribute remove attribute of key @session:key
func (s *session) RemoveAttribute(key any) {
	s.lock.Lock()
	if s.attrs != nil {
		s.attrs.Delete(key)
	}
	s.lock.Unlock()
}

func (s *session) sessionToken() string {
	s.lock.RLock()
	done := s.done
	conn := s.Connection
	name := s.name
	endPoint := s.endPoint
	s.lock.RUnlock()

	select {
	case <-done:
		return "session-closed"
	default:
	}
	if conn == nil || endPoint == nil {
		return "session-closed"
	}

	return fmt.Sprintf("{%s:%s:%d:%s<->%s}",
		name, endPoint.EndPointType(), conn.ID(), conn.LocalAddr(), conn.RemoteAddr())
}

func (s *session) WritePkg(pkg any, timeout time.Duration) (pkgBytesLenth int, successCount int, err error) {
	if pkg == nil {
		return 0, 0, fmt.Errorf("@pkg is nil")
	}
	if s.IsClosed() {
		return 0, 0, ErrSessionClosed
	}

	defer func() {
		if r := recover(); r != nil {
			const size = 64 << 10
			rBuf := make([]byte, size)
			rBuf = rBuf[:runtime.Stack(rBuf, false)]
			err = perrors.WithStack(fmt.Errorf("[session.WritePkg] panic session %s: err=%v\n%s", s.sessionToken(), r, rBuf))
			log.Error(err)
		}
	}()

	pkgBytes, err := s.writer.Write(s, pkg)
	if err != nil {
		log.Warnf("%s, [session.WritePkg] session.writer.Write(@pkg:%#v) = error:%+v", s.Stat(), pkg, err)
		return len(pkgBytes), 0, perrors.WithStack(err)
	}
	var udpCtxPtr *UDPContext
	if udpCtx, ok := pkg.(UDPContext); ok {
		udpCtxPtr = &udpCtx
	} else if udpCtxP, ok := pkg.(*UDPContext); ok {
		udpCtxPtr = udpCtxP
	}
	if udpCtxPtr != nil {
		udpCtxPtr.Pkg = pkgBytes
		pkg = *udpCtxPtr
	} else {
		pkg = pkgBytes
	}
	if 0 < timeout {
		s.packetLock.Lock()
		defer s.packetLock.Unlock()
	} else {
		s.packetLock.RLock()
		defer s.packetLock.RUnlock()
	}
	// #103: read s.Connection under s.lock to guard against concurrent gc()
	// which sets s.Connection = nil; a bare s.Connection.Send below could
	// otherwise nil-deref. The obtained conn/gc pointers stay valid even if
	// gc() later nils the field, because they reference the underlying obj.
	s.lock.RLock()
	conn := s.Connection
	gc := s.gettyConn()
	s.lock.RUnlock()
	if conn == nil || gc == nil {
		return 0, 0, ErrSessionClosed
	}
	if 0 < timeout {
		// #103: save & restore so a per-call timeout does not permanently
		// rewrite the connection's write deadline for subsequent writes.
		origWriteTimeout := gc.WriteTimeout()
		gc.SetWriteTimeout(timeout)
		defer gc.SetWriteTimeout(origWriteTimeout)
	}
	successCount, err = conn.Send(pkg)
	if err != nil {
		log.Warnf("%s, [session.WritePkg] @s.Connection.Write(pkg:%#v) = err:%+v", s.Stat(), pkg, err)
		return len(pkgBytes), successCount, perrors.WithStack(err)
	}
	return len(pkgBytes), successCount, nil
}

// WriteBytes for codecs
func (s *session) WriteBytes(pkg []byte) (int, error) {
	if s.IsClosed() {
		return 0, ErrSessionClosed
	}

	leftPackageSize, totalSize, writeSize := len(pkg), len(pkg), 0
	if leftPackageSize > maxPacketLen {
		s.packetLock.Lock()
		defer s.packetLock.Unlock()
	} else {
		s.packetLock.RLock()
		defer s.packetLock.RUnlock()
	}

	// #103: guard s.Connection against concurrent gc() nil-ing it.
	s.lock.RLock()
	conn := s.Connection
	s.lock.RUnlock()
	if conn == nil {
		return 0, ErrSessionClosed
	}

	for leftPackageSize > maxPacketLen {
		_, err := conn.Send(pkg[writeSize:(writeSize + maxPacketLen)])
		if err != nil {
			return writeSize, perrors.Wrapf(err, "s.Connection.Write(pkg len:%d)", len(pkg))
		}
		leftPackageSize -= maxPacketLen
		writeSize += maxPacketLen
	}

	if leftPackageSize == 0 {
		return writeSize, nil
	}

	_, err := conn.Send(pkg[writeSize:])
	if err != nil {
		return writeSize, perrors.Wrapf(err, "s.Connection.Write(pkg len:%d)", len(pkg))
	}

	return totalSize, nil
}

// WriteBytesArray Write multiple packages at once. so we invoke write sys.call just one time.
func (s *session) WriteBytesArray(pkgs ...[]byte) (int, error) {
	if s.IsClosed() {
		return 0, ErrSessionClosed
	}
	if len(pkgs) == 1 {
		return s.WriteBytes(pkgs[0])
	}

	// reduce syscall and memcopy for multiple packages
	// #103: guard s.Connection against concurrent gc() nil-ing it.
	s.lock.RLock()
	conn := s.Connection
	s.lock.RUnlock()
	if conn == nil {
		return 0, ErrSessionClosed
	}
	if _, ok := conn.(*gettyTCPConn); ok {
		s.packetLock.RLock()
		defer s.packetLock.RUnlock()
		lg, err := conn.Send(pkgs)
		if err != nil {
			return 0, perrors.Wrapf(err, "s.Connection.Write(pkgs num:%d)", len(pkgs))
		}
		return lg, nil
	}

	// get len
	var (
		l      int
		wlg    int
		err    error
		length int
		arrp   *[]byte
		arr    []byte
	)
	length = 0
	for i := 0; i < len(pkgs); i++ {
		length += len(pkgs[i])
	}

	// merge the pkgs
	arrp = gxbytes.AcquireBytes(length)
	defer gxbytes.ReleaseBytes(arrp)
	arr = *arrp

	l = 0
	for i := 0; i < len(pkgs); i++ {
		copy(arr[l:], pkgs[i])
		l += len(pkgs[i])
	}

	wlg, err = s.WriteBytes(arr)
	if err != nil {
		return 0, perrors.WithStack(err)
	}

	num := len(pkgs) - 1
	for i := 0; i < num; i++ {
		s.IncWritePkgNum()
	}

	return wlg, nil
}

func heartbeat(_ gxtime.TimerID, _ time.Time, arg any) error {
	ctx, _ := arg.(*heartbeatContext)
	if ctx == nil || ctx.session == nil || ctx.lifecycle == nil {
		return ErrSessionClosed
	}
	ss := ctx.session

	f := func() {
		if !ctx.lifecycle.acquire() {
			return
		}
		defer ctx.lifecycle.release()

		ss.lock.RLock()
		conn := ss.Connection
		listener := ss.listener
		ss.lock.RUnlock()
		if conn == nil || listener == nil {
			return
		}

		wsConn, wsFlag := conn.(*gettyWSConn)
		if wsFlag {
			err := wsConn.writePing()
			if err != nil {
				log.Warnf("wsConn.writePing() = error:%+v", perrors.WithStack(err))
			}
		}

		listener.OnCron(ss)
	}

	// if enable task pool, run @f asynchronously.
	ss.lock.RLock()
	endPoint := ss.endPoint
	ss.lock.RUnlock()
	if endPoint == nil {
		return ErrSessionClosed
	}
	if taskPool := endPoint.GetTaskPool(); taskPool != nil {
		taskPool.AddTaskAlways(f)
		return nil
	}
	f()
	return nil
}

// func (s *session) RunEventLoop() {
func (s *session) run() {
	if s.Connection == nil || s.listener == nil || s.writer == nil {
		errStr := fmt.Sprintf("session{name:%s, conn:%#v, listener:%#v, writer:%#v}",
			s.name, s.Connection, s.listener, s.writer)
		log.Error(errStr)
		panic(errStr)
	}

	// call session opened
	s.UpdateActive()
	if err := s.listener.OnOpen(s); err != nil {
		log.Errorf("[OnOpen] session %s, error: %#v", s.Stat(), err)
		s.Close()
		return
	}

	s.lock.Lock()
	lifecycle := s.lifecycle
	timer, err := defaultTimerWheel.AddTimer(
		heartbeat,
		gxtime.TimerLoop,
		s.period,
		&heartbeatContext{session: s, lifecycle: lifecycle},
	)
	if err == nil {
		s.heartbeatTimer = timer
	}
	s.lock.Unlock()
	if err != nil {
		panic(fmt.Sprintf("failed to add session %s to defaultTimerWheel err:%v", s.Stat(), err))
	}

	s.grNum.Add(1)
	s.grWG.Add(1)
	// start read gr
	go func() {
		defer s.grWG.Done()
		s.handlePackage()
	}()
}

func (s *session) addTask(pkg any) {
	s.lock.RLock()
	lifecycle := s.lifecycle
	endPoint := s.endPoint
	s.lock.RUnlock()

	f := func() {
		if lifecycle == nil || !lifecycle.acquire() {
			return
		}
		defer lifecycle.release()

		// If the session is closed, there is no need to perform CPU-intensive operations.
		if s.IsClosed() {
			log.Errorf("%s Session is closed", s.sessionToken())
			return
		}
		s.lock.RLock()
		listener := s.listener
		s.lock.RUnlock()
		if listener == nil {
			return
		}
		listener.OnMessage(s, pkg)
		s.IncReadPkgNum()
	}
	if endPoint != nil {
		if taskPool := endPoint.GetTaskPool(); taskPool != nil {
			taskPool.AddTaskAlways(f)
			return
		}
	}
	f()
}

func (s *session) handlePackage() {
	var err error

	defer func() {
		if r := recover(); r != nil {
			const size = 64 << 10
			rBuf := make([]byte, size)
			rBuf = rBuf[:runtime.Stack(rBuf, false)]
			log.Errorf("[session.handlePackage] panic session %s: err=%s\n%s", s.sessionToken(), r, rBuf)
		}
		grNum := s.grNum.Add(-1)
		log.Infof("%s, [session.handlePackage] gr will exit now, left gr num %d", s.sessionToken(), grNum)
		s.stop()
		if err != nil {
			log.Errorf("%s, [session.handlePackage] error:%+v", s.sessionToken(), perrors.WithStack(err))
			if s != nil && s.listener != nil {
				s.listener.OnError(s, err)
			}
		}
		if s.listener != nil {
			s.listener.OnClose(s)
		}
		s.gc()
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
		pktBuf   *gxbytes.Buffer
		pkg      any
	)

	pktBuf = gxbytes.NewBuffer(nil)

	conn = s.Connection.(*gettyTCPConn)
	if tlsConn, ok := conn.conn.(*tls.Conn); ok {
		tlsHandshaketime := defaultTLSHandshakeTimeout
		if s.ReadTimeout() > 0 {
			tlsHandshaketime = s.ReadTimeout()
		}
		ctx, cancel := context.WithTimeout(context.Background(), tlsHandshaketime)
		defer cancel()
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			log.Errorf("[tlsConn.HandshakeContext] = error:%+v", err)
			return perrors.Wrap(err, "tlsConn.HandshakeContext")
		}
	}
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
			buf = pktBuf.WriteNextBegin(maxReadBufLen)
			bufLen, err = conn.recv(buf)
			if err != nil {
				if netError, ok = perrors.Cause(err).(net.Error); ok && netError.Timeout() {
					break
				}
				if perrors.Cause(err) == io.EOF {
					log.Infof("%s, session.conn read EOF, client send over, session exit", s.sessionToken())
					//when read EOF, means that the peer has closed the connection, stop to reconnect to maintain the connection pool.
					s.SetAttribute(ignoreReconnectKey, true)
					err = nil
					exit = true
					if bufLen != 0 {
						// Process the bytes returned with EOF below, then exit
						// without issuing another read on the closed stream.
						log.Infof("%s, session.conn read EOF, while the bufLen(%d) is non-zero.", s.sessionToken(), bufLen)
					}
					break
				}
				log.Errorf("%s, [session.conn.read] = error:%+v", s.sessionToken(), perrors.WithStack(err))
				exit = true
			}
			break
		}
		if bufLen != 0 {
			if _, werr := pktBuf.WriteNextEnd(bufLen); werr != nil {
				log.Warnf("%s, pktBuf.WriteNextEnd(%d) error:%+v", s.sessionToken(), bufLen, perrors.WithStack(werr))
			}
			for pktBuf.Len() > 0 {
				pkg, pkgLen, err = s.reader.Read(s, pktBuf.Bytes())
				// for case 3/case 4
				if err == nil && s.maxMsgLen > 0 && pkgLen > int(s.maxMsgLen) {
					err = perrors.Errorf("pkgLen %d > session max message len %d", pkgLen, s.maxMsgLen)
				}
				// handle case 1
				if err != nil {
					log.Warnf("%s, [session.handleTCPPackage] = len{%d}, error:%+v",
						s.sessionToken(), pkgLen, perrors.WithStack(err))
					exit = true
					break
				}
				// handle case 2/case 3
				if pkg == nil {
					break
				}
				// handle case 4
				s.UpdateActive()
				s.addTask(pkg)
				pktBuf.Next(pkgLen)
				// continue to handle case 5
			}
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
		ok        bool
		err       error
		netError  net.Error
		conn      *gettyUDPConn
		bufLen    int
		maxBufLen int
		bufp      *[]byte
		buf       []byte
		addr      *net.UDPAddr
		pkgLen    int
		pkg       any
	)

	conn = s.Connection.(*gettyUDPConn)
	maxBufLen = int(s.maxMsgLen + maxReadBufLen)
	if int(s.maxMsgLen<<1) < bufLen {
		maxBufLen = int(s.maxMsgLen << 1)
	}
	bufp = gxbytes.AcquireBytes(maxBufLen)
	defer gxbytes.ReleaseBytes(bufp)
	buf = *bufp
	for !s.IsClosed() {

		bufLen, addr, err = conn.recv(buf)
		log.Debugf("conn.read() = bufLen:%d, addr:%#v, err:%+v", bufLen, addr, perrors.WithStack(err))
		if netError, ok = perrors.Cause(err).(net.Error); ok && netError.Timeout() {
			continue
		}
		if err != nil {
			log.Errorf("%s, [session.handleUDPPackage] = len:%d, error:%+v",
				s.sessionToken(), bufLen, perrors.WithStack(err))
			err = perrors.Wrapf(err, "conn.read()")
			break
		}

		if bufLen == 0 {
			log.Errorf("conn.read() = bufLen:%d, addr:%s, err:%+v", bufLen, addr, perrors.WithStack(err))
			continue
		}

		if bufLen == len(connectPingPackage) && bytes.Equal(connectPingPackage, buf[:bufLen]) {
			log.Infof("got %s connectPingPackage", addr)
			continue
		}

		pkg, pkgLen, err = s.reader.Read(s, buf[:bufLen])
		log.Debugf("s.reader.Read() = pkg:%#v, pkgLen:%d, err:%+v", pkg, pkgLen, perrors.WithStack(err))
		if err == nil && s.maxMsgLen > 0 && bufLen > int(s.maxMsgLen) {
			err = perrors.Errorf("Message Too Long, bufLen %d, session max message len %d", bufLen, s.maxMsgLen)
		}
		if err != nil {
			log.Warnf("%s, [session.handleUDPPackage] = len:%d, error:%+v",
				s.sessionToken(), pkgLen, perrors.WithStack(err))
			continue
		}
		if pkgLen == 0 {
			log.Errorf("s.reader.Read() = pkg:%#v, pkgLen:%d, err:%+v", pkg, pkgLen, perrors.WithStack(err))
			continue
		}

		s.UpdateActive()
		s.addTask(UDPContext{Pkg: pkg, PeerAddr: addr})
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
		unmarshalPkg any
	)

	conn = s.Connection.(*gettyWSConn)
	for !s.IsClosed() {
		pkg, err = conn.recv()
		if netError, ok = perrors.Cause(err).(net.Error); ok && netError.Timeout() {
			continue
		}
		if err != nil {
			log.Warnf("%s, [session.handleWSPackage] = error:%+v",
				s.sessionToken(), perrors.WithStack(err))
			return perrors.WithStack(err)
		}
		s.UpdateActive()
		if s.reader != nil {
			unmarshalPkg, length, err = s.reader.Read(s, pkg)
			if err == nil && s.maxMsgLen > 0 && length > int(s.maxMsgLen) {
				err = perrors.Errorf("Message Too Long, length %d, session max message len %d", length, s.maxMsgLen)
			}
			if err != nil {
				log.Warnf("%s, [session.handleWSPackage] = len:%d, error:%+v",
					s.sessionToken(), length, perrors.WithStack(err))
				continue
			}

			s.addTask(unmarshalPkg)
		} else {
			s.addTask(pkg)
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
				if err := conn.SetReadDeadline(now.Add(s.ReadTimeout())); err != nil {
					log.Warnf("failed to set read deadline: %+v", err)
				}
				if err := conn.SetWriteDeadline(now.Add(s.WriteTimeout())); err != nil {
					log.Warnf("failed to set write deadline: %+v", err)
				}
			}
			close(s.done)
			lifecycle := s.stopHeartbeat()

			s.closeCallbackMutex.RLock()
			closeCallbacks := s.closeCallback
			s.closeCallbackMutex.RUnlock()
			if lifecycle != nil {
				lifecycle.addClosingTask()
			}

			go func(sessionToken string, closeCallbacks callbacks, lifecycle *sessionLifecycle) {
				if lifecycle != nil {
					defer lifecycle.release()
				}
				defer func() {
					if r := recover(); r != nil {
						const size = 64 << 10
						rBuf := make([]byte, size)
						rBuf = rBuf[:runtime.Stack(rBuf, false)]
						err := perrors.WithStack(fmt.Errorf("[session.invokeCloseCallbacks] panic session %s: err=%v\n%s",
							sessionToken, r, rBuf))
						log.Error(err)
					}
				}()

				closeCallbacks.Invoke()
			}(s.sessionToken(), closeCallbacks, lifecycle)

			clt, cltFound := s.GetAttribute(sessionClientKey).(*client)
			ignoreReconnect, flagFound := s.GetAttribute(ignoreReconnectKey).(bool)
			if cltFound && flagFound && !ignoreReconnect {
				clt.runReconnect()
			}
		})
	}
}

func (s *session) stopHeartbeat() *sessionLifecycle {
	s.lock.Lock()
	timer := s.heartbeatTimer
	s.heartbeatTimer = nil
	lifecycle := s.lifecycle
	s.lock.Unlock()

	if timer != nil {
		timer.Stop()
	}
	if lifecycle != nil {
		lifecycle.close()
	}
	return lifecycle
}

func (s *session) gc() {
	var conn Connection
	var wait time.Duration
	var lifecycle *sessionLifecycle

	s.lock.Lock()
	if s.attrs != nil {
		s.attrs = nil
		conn = s.Connection
		s.Connection = nil
		wait = s.wait
		lifecycle = s.lifecycle
	}
	s.lock.Unlock()
	if conn != nil && lifecycle != nil {
		lifecycle.addClosingTask()
	}

	go func(conn Connection, wait time.Duration, lifecycle *sessionLifecycle) {
		if conn != nil && lifecycle != nil {
			defer lifecycle.release()
		}
		if conn != nil {
			conn.CloseConn(int(wait))
		}
	}(conn, wait, lifecycle)
}

// Close will be invoked by NewSessionCallback(if return error is not nil)
// or (session)handleLoop automatically. It's thread safe.
func (s *session) Close() {
	s.stop()
	log.Infof("%s closed now. its current gr num is %d", s.sessionToken(), s.grNum.Load())
}

// GetActive return connection's time
func (s *session) GetActive() time.Time {
	if s == nil {
		return launchTime
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if s.Connection != nil {
		return s.Connection.GetActive()
	}
	return launchTime
}

// UpdateActive update connection's active time
func (s *session) UpdateActive() {
	if s == nil {
		return
	}
	s.lock.RLock()
	defer s.lock.RUnlock()

	if s.Connection != nil {
		s.Connection.UpdateActive()
	}
}

func (s *session) ID() uint32 {
	if s == nil {
		return 0
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if s.Connection != nil {
		return s.Connection.ID()
	}
	return 0
}

func (s *session) LocalAddr() string {
	if s == nil {
		return ""
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if s.Connection != nil {
		return s.Connection.LocalAddr()
	}
	return ""
}

func (s *session) RemoteAddr() string {
	if s == nil {
		return ""
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if s.Connection != nil {
		return s.Connection.RemoteAddr()
	}
	return ""
}

func (s *session) IncReadPkgNum() {
	if s == nil {
		return
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if s.Connection != nil {
		s.Connection.IncReadPkgNum()
	}
}

func (s *session) IncWritePkgNum() {
	if s == nil {
		return
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if s.Connection != nil {
		s.Connection.IncWritePkgNum()
	}
}

func (s *session) Send(pkg any) (int, error) {
	if s == nil {
		return 0, nil
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if s.Connection != nil {
		return s.Connection.Send(pkg)
	}
	return 0, nil
}

func (s *session) ReadTimeout() time.Duration {
	if s == nil {
		return time.Duration(0)
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if s.Connection != nil {
		return s.Connection.ReadTimeout()
	}
	return time.Duration(0)
}

func (s *session) SetSession(ss Session) {
	if s == nil {
		return
	}
	s.lock.RLock()
	if s.Connection != nil {
		s.Connection.SetSession(ss)
	}
	s.lock.RUnlock()
}
