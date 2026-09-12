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
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

import (
	"github.com/gorilla/websocket"
)

var (
	errTestReadFailure      = errors.New("test read failure")
	errUnexpectedSecondRead = errors.New("unexpected second read")
	errTestPartialWrite     = errors.New("test partial write failure")
)

// Regression test for #97: size the UDP read buffer from configured limits, not unread data.
func TestUDPReadBufferSize(t *testing.T) {
	tests := []struct {
		name      string
		maxMsgLen int32
		want      int
	}{
		{name: "tiny message", maxMsgLen: 1, want: 2},
		{name: "below crossover", maxMsgLen: maxReadBufLen - 1, want: 2 * (maxReadBufLen - 1)},
		{name: "at crossover", maxMsgLen: maxReadBufLen, want: 2 * maxReadBufLen},
		{name: "above crossover", maxMsgLen: maxReadBufLen + 1, want: 2*maxReadBufLen + 1},
		{name: "zero message limit", maxMsgLen: 0, want: 64 * 1024},
		{name: "negative message limit", maxMsgLen: -1, want: 64 * 1024},
		{name: "large message", maxMsgLen: 128 * 1024, want: 64 * 1024},
		{name: "maximum message limit", maxMsgLen: math.MaxInt32, want: 64 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := udpReadBufferSize(tt.maxMsgLen); got != tt.want {
				t.Fatalf("udpReadBufferSize(%d) = %d, want %d", tt.maxMsgLen, got, tt.want)
			}
		})
	}
}

func TestSetMaxMsgLenNormalizesLimits(t *testing.T) {
	type testCase struct {
		name   string
		length int
		want   int32
	}
	tests := []testCase{
		{name: "negative becomes unlimited", length: -1, want: 0},
		{name: "zero remains unlimited", length: 0, want: 0},
	}
	if strconv.IntSize == 64 {
		oversized := int64(math.MaxInt32) + 1
		tests = append(tests, testCase{name: "oversized value is clamped", length: int(oversized), want: math.MaxInt32})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss := &session{}
			ss.SetMaxMsgLen(tt.length)
			if ss.maxMsgLen != tt.want {
				t.Fatalf("SetMaxMsgLen(%d) stored %d, want %d", tt.length, ss.maxMsgLen, tt.want)
			}
		})
	}
}

type recordingErrorReader struct {
	dataLengths chan<- int
}

func (r recordingErrorReader) Read(_ Session, data []byte) (any, int, error) {
	r.dataLengths <- len(data)
	return nil, 0, errTestReadFailure
}

func TestHandleUDPPackageUsesConfiguredReadBuffer(t *testing.T) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}

	sender, err := net.DialUDP("udp", nil, listener.LocalAddr().(*net.UDPAddr))
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	defer func() {
		if err := sender.Close(); err != nil {
			t.Errorf("close UDP sender: %v", err)
		}
	}()

	dataLengths := make(chan int, 1)
	ss := newUDPSession(listener, newServer(UDP_ENDPOINT)).(*session)
	ss.SetMaxMsgLen(1)
	ss.SetReader(recordingErrorReader{dataLengths: dataLengths})
	want := udpReadBufferSize(1)
	if want != 2 {
		t.Fatalf("udpReadBufferSize(1) = %d, want 2", want)
	}

	handlerDone := make(chan error, 1)
	go func() {
		handlerDone <- ss.handleUDPPackage()
	}()

	handlerStopped := false
	stopHandler := func() bool {
		if handlerStopped {
			return true
		}
		_ = listener.Close()
		select {
		case <-handlerDone:
			handlerStopped = true
			return true
		case <-time.After(time.Second):
			return false
		}
	}
	defer func() {
		if !stopHandler() {
			t.Error("handleUDPPackage did not return after closing the UDP listener")
		}
	}()

	if _, err := sender.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-dataLengths:
		if got != want {
			t.Fatalf("Reader data length = %d, want udpReadBufferSize(1) = %d", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("Reader did not receive the UDP datagram")
	}

	if !stopHandler() {
		t.Fatal("handleUDPPackage did not return after closing the UDP listener")
	}
}

type errorReader struct{}

func (errorReader) Read(Session, []byte) (any, int, error) {
	return nil, 0, errTestReadFailure
}

type timeoutTestWriter struct{}

func (timeoutTestWriter) Write(Session, any) ([]byte, error) {
	return []byte("x"), nil
}

type timeoutTestCall struct {
	observed time.Duration
	release  chan struct{}
}

type timeoutTestNetConn struct {
	owner   *gettyTCPConn
	entered chan *timeoutTestCall
}

func (c *timeoutTestNetConn) Write(p []byte) (int, error) {
	call := &timeoutTestCall{
		observed: c.owner.WriteTimeout(),
		release:  make(chan struct{}),
	}
	c.entered <- call
	<-call.release
	return len(p), nil
}

func (*timeoutTestNetConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (*timeoutTestNetConn) Close() error                     { return nil }
func (*timeoutTestNetConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*timeoutTestNetConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*timeoutTestNetConn) SetDeadline(time.Time) error      { return nil }
func (*timeoutTestNetConn) SetReadDeadline(time.Time) error  { return nil }
func (*timeoutTestNetConn) SetWriteDeadline(time.Time) error { return nil }

type eofDataNetConn struct {
	data  []byte
	reads int
}

func (c *eofDataNetConn) Read(p []byte) (int, error) {
	c.reads++
	if c.reads == 1 {
		return copy(p, c.data), io.EOF
	}
	return 0, errUnexpectedSecondRead
}

func (*eofDataNetConn) Write(p []byte) (int, error)      { return len(p), nil }
func (*eofDataNetConn) Close() error                     { return nil }
func (*eofDataNetConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*eofDataNetConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*eofDataNetConn) SetDeadline(time.Time) error      { return nil }
func (*eofDataNetConn) SetReadDeadline(time.Time) error  { return nil }
func (*eofDataNetConn) SetWriteDeadline(time.Time) error { return nil }

type wholeFrameReader struct{}

func (wholeFrameReader) Read(_ Session, data []byte) (any, int, error) {
	return string(data), len(data), nil
}

type recordingEventListener struct {
	messages []any
}

func (*recordingEventListener) OnOpen(Session) error   { return nil }
func (*recordingEventListener) OnClose(Session)        {}
func (*recordingEventListener) OnError(Session, error) {}
func (*recordingEventListener) OnCron(Session)         {}
func (l *recordingEventListener) OnMessage(_ Session, v any) {
	l.messages = append(l.messages, v)
}

type blockingCronEventListener struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
}

func (*blockingCronEventListener) OnOpen(Session) error   { return nil }
func (*blockingCronEventListener) OnClose(Session)        {}
func (*blockingCronEventListener) OnError(Session, error) {}
func (l *blockingCronEventListener) OnCron(Session) {
	l.enteredOnce.Do(func() { close(l.entered) })
	<-l.release
}
func (*blockingCronEventListener) OnMessage(Session, any) {}

type resetBarrierNetConn struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once
}

func (c *resetBarrierNetConn) Read([]byte) (int, error) {
	c.enteredOnce.Do(func() { close(c.entered) })
	<-c.release
	return 0, io.EOF
}

func (*resetBarrierNetConn) Write(p []byte) (int, error)      { return len(p), nil }
func (*resetBarrierNetConn) Close() error                     { return nil }
func (*resetBarrierNetConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*resetBarrierNetConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*resetBarrierNetConn) SetDeadline(time.Time) error      { return nil }
func (*resetBarrierNetConn) SetReadDeadline(time.Time) error  { return nil }
func (*resetBarrierNetConn) SetWriteDeadline(time.Time) error { return nil }

func (c *resetBarrierNetConn) releaseRead() {
	c.releaseOnce.Do(func() { close(c.release) })
}

// fragmentGateNetConn records the size of every Write and parks the first one
// until released, so a test can freeze WriteBytes inside its maxPacketLen
// fragmenting loop and observe whether another writer slips in.
type fragmentGateNetConn struct {
	mu           sync.Mutex
	writes       []int
	entered      chan int
	releaseFirst chan struct{}
	firstOnce    sync.Once
	firstDone    chan struct{}
}

func newFragmentGateNetConn() *fragmentGateNetConn {
	return &fragmentGateNetConn{
		entered:      make(chan int, 8),
		releaseFirst: make(chan struct{}),
		firstDone:    make(chan struct{}),
	}
}

func (c *fragmentGateNetConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes = append(c.writes, len(p))
	c.mu.Unlock()
	c.entered <- len(p)
	c.firstOnce.Do(func() {
		<-c.releaseFirst
		close(c.firstDone)
	})
	<-c.firstDone
	return len(p), nil
}

func (*fragmentGateNetConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (*fragmentGateNetConn) Close() error                     { return nil }
func (*fragmentGateNetConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*fragmentGateNetConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*fragmentGateNetConn) SetDeadline(time.Time) error      { return nil }
func (*fragmentGateNetConn) SetReadDeadline(time.Time) error  { return nil }
func (*fragmentGateNetConn) SetWriteDeadline(time.Time) error { return nil }

func (c *fragmentGateNetConn) writeSizes() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.writes...)
}

// TestSendWaitsForWriteBytesFragments is the regression for issue #131: a
// public session.Send must not interleave with the maxPacketLen-sized fragments
// of a concurrent WriteBytes, otherwise the peer decodes a corrupted byte
// stream. The gate freezes WriteBytes after its first fragment, so the write
// order is observed deterministically instead of by timing luck.
func TestSendWaitsForWriteBytesFragments(t *testing.T) {
	netConn := newFragmentGateNetConn()
	ss := newTCPSession(netConn, nil).(*session)

	bigDone := make(chan error, 1)
	go func() {
		_, err := ss.WriteBytes(make([]byte, maxPacketLen+1))
		bigDone <- err
	}()

	select {
	case n := <-netConn.entered:
		if n != maxPacketLen {
			t.Fatalf("first fragment size = %d, want %d", n, maxPacketLen)
		}
	case <-time.After(time.Second):
		t.Fatal("WriteBytes did not enter the first fragment")
	}

	sendDone := make(chan error, 1)
	go func() {
		_, err := ss.Send([]byte("direct"))
		sendDone <- err
	}()

	select {
	case n := <-netConn.entered:
		close(netConn.releaseFirst)
		<-bigDone
		<-sendDone
		t.Fatalf("Send wrote %d bytes while a WriteBytes fragment was in flight", n)
	case <-time.After(50 * time.Millisecond):
	}

	close(netConn.releaseFirst)
	if err := <-bigDone; err != nil {
		t.Fatalf("WriteBytes failed: %v", err)
	}

	select {
	case n := <-netConn.entered:
		if n != 1 {
			t.Fatalf("WriteBytes trailing fragment size = %d, want 1", n)
		}
	case <-time.After(time.Second):
		t.Fatal("WriteBytes did not write its trailing fragment")
	}

	select {
	case err := <-sendDone:
		if err != nil {
			t.Fatalf("Send failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not complete after WriteBytes released packetLock")
	}

	select {
	case n := <-netConn.entered:
		if n != len("direct") {
			t.Fatalf("direct Send wrote %d bytes, want %d", n, len("direct"))
		}
	case <-time.After(time.Second):
		t.Fatal("direct Send never reached the connection")
	}

	if got, want := netConn.writeSizes(), []int{maxPacketLen, 1, len("direct")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wire write order = %v, want %v", got, want)
	}
}

func TestConcurrentWritePkgTimeoutRestoration(t *testing.T) {
	netConn := &timeoutTestNetConn{entered: make(chan *timeoutTestCall, 2)}
	ss := newTCPSession(netConn, nil).(*session)
	ss.writer = timeoutTestWriter{}
	conn := ss.Connection.(*gettyTCPConn)
	netConn.owner = conn
	initialTimeout := conn.WriteTimeout()

	firstDone := make(chan error, 1)
	go func() {
		_, _, err := ss.WritePkg("first", 3*time.Second)
		firstDone <- err
	}()
	firstCall := <-netConn.entered

	secondDone := make(chan error, 1)
	go func() {
		_, _, err := ss.WritePkg("second", 5*time.Second)
		secondDone <- err
	}()

	select {
	case secondCall := <-netConn.entered:
		close(firstCall.release)
		<-firstDone
		close(secondCall.release)
		<-secondDone
		t.Fatal("second timed write entered while the first still owned the shared write timeout")
	case <-time.After(50 * time.Millisecond):
	}

	if firstCall.observed != 3*time.Second {
		t.Fatalf("first write observed timeout %v, want %v", firstCall.observed, 3*time.Second)
	}
	close(firstCall.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	var secondCall *timeoutTestCall
	select {
	case secondCall = <-netConn.entered:
	case <-time.After(time.Second):
		t.Fatal("second timed write did not enter after the first completed")
	}
	if secondCall.observed != 5*time.Second {
		t.Fatalf("second write observed timeout %v, want %v", secondCall.observed, 5*time.Second)
	}
	close(secondCall.release)
	if err := <-secondDone; err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	if got := conn.WriteTimeout(); got != initialTimeout {
		t.Fatalf("write timeout after concurrent calls = %v, want %v", got, initialTimeout)
	}
}

func TestResetWaitsForPackageLoop(t *testing.T) {
	netConn := &resetBarrierNetConn{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	defer netConn.releaseOnce.Do(func() { close(netConn.release) })

	ss := newTCPSession(netConn, newServer(TCP_SERVER)).(*session)
	ss.SetReader(wholeFrameReader{})
	ss.SetWriter(timeoutTestWriter{})
	ss.SetEventListener(&recordingEventListener{})
	ss.run()

	select {
	case <-netConn.entered:
	case <-time.After(time.Second):
		t.Fatal("package loop did not enter Read")
	}

	resetDone := make(chan struct{})
	go func() {
		ss.Reset()
		close(resetDone)
	}()

	select {
	case <-resetDone:
		t.Fatal("Reset returned while the package loop was still running")
	case <-time.After(50 * time.Millisecond):
	}

	ss.Close()
	netConn.releaseRead()
	select {
	case <-resetDone:
	case <-time.After(time.Second):
		t.Fatal("Reset did not return after Close released the package loop")
	}
	if ss.Connection != nil {
		t.Fatal("Reset did not clear the session connection")
	}
	if got := ss.grNum.Load(); got != 0 {
		t.Fatalf("goroutine count after Reset = %d, want 0", got)
	}
}

func TestResetWaitsForActiveHeartbeat(t *testing.T) {
	listener := &blockingCronEventListener{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	ss := newTCPSession(&eofDataNetConn{}, newServer(TCP_SERVER)).(*session)
	ss.SetEventListener(listener)
	lifecycle := ss.lifecycle
	ctx := &heartbeatContext{session: ss, lifecycle: lifecycle}

	heartbeatDone := make(chan error, 1)
	go func() {
		heartbeatDone <- heartbeat(0, time.Time{}, ctx)
	}()
	select {
	case <-listener.entered:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not enter OnCron")
	}

	resetDone := make(chan struct{})
	go func() {
		ss.Reset()
		close(resetDone)
	}()
	select {
	case <-resetDone:
		t.Fatal("Reset returned while the heartbeat callback was still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(listener.release)
	select {
	case err := <-heartbeatDone:
		if err != nil {
			t.Fatalf("heartbeat returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not return")
	}
	select {
	case <-resetDone:
	case <-time.After(time.Second):
		t.Fatal("Reset did not return after the heartbeat callback completed")
	}

	if err := heartbeat(0, time.Time{}, ctx); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("stale heartbeat returned %v, want %v", err, ErrSessionClosed)
	}
}

func TestHandleTCPPackageProcessesDataAndStopsOnEOF(t *testing.T) {
	netConn := &eofDataNetConn{data: []byte("final frame")}
	ss := newTCPSession(netConn, newServer(TCP_SERVER)).(*session)
	listener := &recordingEventListener{}
	ss.SetReader(wholeFrameReader{})
	ss.SetEventListener(listener)

	if err := ss.handleTCPPackage(); err != nil {
		t.Fatalf("handleTCPPackage returned error: %v", err)
	}
	if netConn.reads != 1 {
		t.Fatalf("underlying Read calls = %d, want 1", netConn.reads)
	}
	if len(listener.messages) != 1 || listener.messages[0] != "final frame" {
		t.Fatalf("delivered messages = %#v, want [\"final frame\"]", listener.messages)
	}
}

func TestHandlePackageWithNilListenerDoesNotPanicOnError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = listener.Close()
	}()

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = clientConn.Close()
	}()

	var serverConn net.Conn
	select {
	case err := <-acceptErr:
		t.Fatal(err)
	case serverConn = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for server connection")
	}

	ss := newTCPSession(serverConn, newServer(TCP_SERVER)).(*session)
	ss.SetReader(errorReader{})
	ss.SetWaitTime(time.Second)
	ss.grNum.Add(1)

	if _, err := clientConn.Write([]byte("trigger read error")); err != nil {
		t.Fatal(err)
	}

	ss.handlePackage()
}

// Regression test for #122: promoted Connection methods must not panic after
// the session has cleared its embedded Connection via gc().
func TestSetMethodsAfterGCDoNotPanic(t *testing.T) {
	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()

	ss := newTCPSession(c1, newServer(TCP_SERVER)).(*session)
	ss.gc()
	if ss.Connection != nil {
		t.Fatal("gc did not clear the session connection")
	}

	ss.SetReadTimeout(time.Second)
	ss.SetWriteTimeout(time.Second)
	ss.SetCompressType(CompressZip)
	ss.CloseConn(0)
}

// closeOnOpenListener closes the session from OnOpen and still reports success,
// the pattern that used to leave run() installing a heartbeat timer on an
// already closed session (#129).
type closeOnOpenListener struct{}

func (*closeOnOpenListener) OnOpen(ss Session) error { ss.Close(); return nil }
func (*closeOnOpenListener) OnClose(Session)         {}
func (*closeOnOpenListener) OnError(Session, error)  {}
func (*closeOnOpenListener) OnCron(Session)          {}
func (*closeOnOpenListener) OnMessage(Session, any)  {}

// Regression test for #129: OnOpen closed the session, so stop() ran
// stopHeartbeat() while heartbeatTimer was nil. run() then installed a
// TimerLoop that no later stop() could remove, pinning the session in the
// global timer wheel forever.
func TestRunDoesNotInstallHeartbeatAfterClose(t *testing.T) {
	ss := newTCPSession(&eofDataNetConn{}, newServer(TCP_SERVER)).(*session)
	ss.SetReader(wholeFrameReader{})
	ss.SetWriter(timeoutTestWriter{})
	ss.SetEventListener(&closeOnOpenListener{})

	ss.run()
	ss.grWG.Wait()

	ss.lock.RLock()
	timer := ss.heartbeatTimer
	ss.lock.RUnlock()
	if timer != nil {
		t.Fatal("run() installed a heartbeat timer on an already closed session; nothing can stop it")
	}
}

// Regression test for #129: a repeat stop() used to return as soon as s.done
// was closed, so handlePackage's `s.stop(); s.gc()` defer could clear s.attrs
// while the first stop() was still inside its once body, dropping the
// reconnect. The parked body below is where that lookup happens.
func TestRepeatStopWaitsForCloseSequence(t *testing.T) {
	ss := newTCPSession(&eofDataNetConn{}, newServer(TCP_SERVER)).(*session)

	ss.closeCallbackMutex.Lock()

	firstDone := make(chan struct{})
	go func() {
		ss.stop()
		close(firstDone)
	}()

	select {
	case <-ss.done:
	case <-time.After(time.Second):
		ss.closeCallbackMutex.Unlock()
		t.Fatal("first stop() did not close the session")
	}

	secondDone := make(chan struct{})
	go func() {
		ss.stop()
		close(secondDone)
	}()

	select {
	case <-secondDone:
		ss.closeCallbackMutex.Unlock()
		<-firstDone
		t.Fatal("repeat stop() returned while the close sequence was still running")
	case <-time.After(50 * time.Millisecond):
	}

	ss.closeCallbackMutex.Unlock()

	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("repeat stop() did not return after the close sequence completed")
	}
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first stop() did not return")
	}
}

// closeConnRecorder captures the waitSec handed to Connection.CloseConn.
type closeConnRecorder struct {
	*gettyTCPConn
	waits chan int
}

func (c *closeConnRecorder) CloseConn(waitSec int) {
	c.waits <- waitSec
}

// endPointCallbackConn mimics gettyUDPConn.Send, which calls back
// session.EndPoint() and therefore re-enters s.lock (issue #112).
type endPointCallbackConn struct {
	*gettyTCPConn
	ss     *session
	locked chan bool
}

func (c *endPointCallbackConn) Send(pkg any) (int, error) {
	// A writer's TryLock fails while any reader holds s.lock, so this reports
	// whether session.Send kept s.lock held across the Connection call.
	acquired := c.ss.lock.TryLock()
	if acquired {
		c.ss.lock.Unlock()
	}
	c.locked <- acquired
	_ = c.ss.EndPoint() // the recursive RLock that deadlocked before the fix
	body, _ := pkg.([]byte)
	return len(body), nil
}

// Regression test for #112: session.Send used to hold s.lock across
// Connection.Send. gettyUDPConn.Send calls back EndPoint(), which takes
// s.lock.RLock again, and that recursive RLock deadlocks behind a queued
// writer. The stub reports the lock state from inside the call, so no timing
// luck is involved.
func TestSendDoesNotHoldSessionLockAcrossConnectionSend(t *testing.T) {
	conn := &endPointCallbackConn{
		gettyTCPConn: newGettyTCPConn(&eofDataNetConn{}),
		locked:       make(chan bool, 1),
	}
	ss := newSession(newServer(UDP_ENDPOINT), conn)
	conn.ss = ss

	if _, err := ss.Send([]byte("direct")); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if acquired := <-conn.locked; !acquired {
		t.Fatal("session.Send held s.lock across Connection.Send: EndPoint() would recurse and deadlock behind a queued writer")
	}
}

// Regression test for #112: gc() passed the pending duration to CloseConn
// without converting nanoseconds to seconds, so int(wait) overflowed the
// int32 linger field and Close blocked for minutes.
func TestGCClosesConnWithSeconds(t *testing.T) {
	rec := &closeConnRecorder{
		gettyTCPConn: newGettyTCPConn(&eofDataNetConn{}),
		waits:        make(chan int, 1),
	}
	ss := newSession(nil, rec)

	ss.gc()

	select {
	case waitSec := <-rec.waits:
		if want := int(pendingDuration / time.Second); waitSec != want {
			t.Fatalf("CloseConn waitSec = %d, want %d (seconds, not nanoseconds)", waitSec, want)
		}
	case <-time.After(time.Second):
		t.Fatal("gc() did not call Connection.CloseConn")
	}
}

// Regression test for #112: WriteTimeout used to be the promoted Connection
// method with no nil guard, so stop() crashed once gc()/Reset() had cleared
// s.Connection.
func TestWriteTimeoutIsNilSafeAfterReset(t *testing.T) {
	ss := newTCPSession(&eofDataNetConn{}, newServer(TCP_SERVER)).(*session)
	ss.Reset()

	if got := ss.WriteTimeout(); got != 0 {
		t.Fatalf("WriteTimeout after Reset = %v, want 0", got)
	}
}

// Regression test for #112: Conn()/Stat() read s.Connection without s.lock
// while gc()/Reset() write it. Relies on the race detector (CI runs
// `make test-race`).
func TestStatAndConnAreSafeDuringGC(t *testing.T) {
	ss := newTCPSession(&eofDataNetConn{}, newServer(TCP_SERVER)).(*session)

	// Both goroutines are released by the same barrier and joined before the
	// test returns: the reads are guaranteed to overlap the write rather than
	// possibly finishing first, and neither goroutine outlives the test.
	start := make(chan struct{})
	readsDone := make(chan struct{})
	gcDone := make(chan struct{})
	go func() {
		defer close(readsDone)
		<-start
		for i := 0; i < 1000; i++ {
			_ = ss.Stat()
			_ = ss.Conn()
		}
	}()
	go func() {
		defer close(gcDone)
		<-start
		ss.gc()
	}()
	close(start)

	select {
	case <-gcDone:
	case <-time.After(5 * time.Second):
		t.Fatal("gc() did not return")
	}
	select {
	case <-readsDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Stat/Conn did not return while gc() was running")
	}
}

// newWSTestConnPair returns a connected pair of WebSocket connections: the
// server side, to be wrapped into the session under test, and the client side,
// used to observe the exact message framing that session produces.
func newWSTestConnPair(t *testing.T) (server, client *websocket.Conn) {
	t.Helper()

	var (
		upgrader = websocket.Upgrader{}
		serverCh = make(chan *websocket.Conn, 1)
		errCh    = make(chan error, 1)
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}
		serverCh <- conn
	}))
	t.Cleanup(srv.Close)

	client, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket test server: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	t.Cleanup(func() { _ = client.Close() })

	select {
	case server = <-serverCh:
	case upgradeErr := <-errCh:
		t.Fatalf("upgrade websocket test connection: %v", upgradeErr)
	case <-time.After(5 * time.Second):
		t.Fatal("websocket test server did not hand over the upgraded connection")
	}
	t.Cleanup(func() { _ = server.Close() })

	return server, client
}

// readWSMessage reads one message from the peer side of a test WS pair.
func readWSMessage(t *testing.T, conn *websocket.Conn) (int, []byte) {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	messageType, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	return messageType, message
}

// Regression test for #128 item 1: WriteBytes used to cut every payload above
// maxPacketLen into maxPacketLen-sized conn.Send calls. On WS each call is its
// own message and the peer's reader has no cross-message buffering, so such a
// package could never be reassembled. A WS payload has to leave in one message.
func TestWSWriteBytesKeepsLargePackageInSingleMessage(t *testing.T) {
	serverConn, clientConn := newWSTestConnPair(t)
	ss := newWSSession(serverConn, nil).(*session)

	pkg := make([]byte, maxPacketLen+4096)
	for i := range pkg {
		pkg[i] = byte(i)
	}

	n, err := ss.WriteBytes(pkg)
	if err != nil {
		t.Fatalf("WriteBytes failed: %v", err)
	}
	if n != len(pkg) {
		t.Fatalf("WriteBytes reported %d bytes, want %d", n, len(pkg))
	}

	messageType, message := readWSMessage(t, clientConn)
	if messageType != websocket.BinaryMessage {
		t.Fatalf("peer message type = %d, want %d", messageType, websocket.BinaryMessage)
	}
	if !bytes.Equal(message, pkg) {
		t.Fatalf("peer received a %d byte message, want the whole %d byte package in a single message",
			len(message), len(pkg))
	}

	// The package must not be followed by a trailing fragment on the wire.
	if err := clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, extra, err := clientConn.ReadMessage(); err == nil {
		t.Fatalf("WriteBytes sent %d extra bytes in a second WS message", len(extra))
	}
}

// Regression test for #128 item 2: WriteBytesArray merged the whole batch into
// one buffer for every non-TCP connection, so on WS N packages arrived as one
// message and the peer's reader dropped everything after the first package
// without any error. Each package must be its own WS message.
func TestWSWriteBytesArraySendsOneMessagePerPackage(t *testing.T) {
	serverConn, clientConn := newWSTestConnPair(t)
	ss := newWSSession(serverConn, nil).(*session)

	pkgA := []byte("package-a")
	pkgB := []byte("package-bb")

	n, err := ss.WriteBytesArray(pkgA, pkgB)
	if err != nil {
		t.Fatalf("WriteBytesArray failed: %v", err)
	}
	if want := len(pkgA) + len(pkgB); n != want {
		t.Fatalf("WriteBytesArray reported %d bytes, want %d", n, want)
	}

	for i, want := range [][]byte{pkgA, pkgB} {
		_, got := readWSMessage(t, clientConn)
		if !bytes.Equal(got, want) {
			t.Fatalf("WS message %d = %q, want package %q: a batch must not be merged into one message",
				i, got, want)
		}
	}
}

// partialFrameReader implements the "need more data" case of the Reader
// contract: a message too short to hold a frame yields (nil, 0, nil).
type partialFrameReader struct {
	frameLen int
}

func (r partialFrameReader) Read(_ Session, data []byte) (any, int, error) {
	if len(data) < r.frameLen {
		return nil, 0, nil
	}
	return string(data[:r.frameLen]), r.frameLen, nil
}

// wsMessageRecorder records every package handed to OnMessage.
type wsMessageRecorder struct {
	mu       sync.Mutex
	messages []any
	notify   chan struct{}
}

func (*wsMessageRecorder) OnOpen(Session) error   { return nil }
func (*wsMessageRecorder) OnClose(Session)        {}
func (*wsMessageRecorder) OnError(Session, error) {}
func (*wsMessageRecorder) OnCron(Session)         {}
func (l *wsMessageRecorder) OnMessage(_ Session, v any) {
	l.mu.Lock()
	l.messages = append(l.messages, v)
	l.mu.Unlock()
	select {
	case l.notify <- struct{}{}:
	default:
	}
}

func (l *wsMessageRecorder) recorded() []any {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]any(nil), l.messages...)
}

// Regression test for #128 item 3: handleWSPackage dispatched the result of the
// reader unconditionally, so an undecided reader (nil, 0, nil) delivered a nil
// package to OnMessage. The incomplete message is followed by a complete one,
// which also pins that the read loop keeps reading instead of breaking out.
func TestWSHandlePackageSkipsNilPkg(t *testing.T) {
	serverConn, clientConn := newWSTestConnPair(t)
	ss := newWSSession(serverConn, nil).(*session)
	ss.SetReader(partialFrameReader{frameLen: len("real")})
	recorder := &wsMessageRecorder{notify: make(chan struct{}, 4)}
	ss.SetEventListener(recorder)

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		_ = ss.handleWSPackage()
	}()
	t.Cleanup(func() {
		ss.Close()
		select {
		case <-handlerDone:
		case <-time.After(5 * time.Second):
			t.Error("handleWSPackage did not return after the session was closed")
		}
	})

	for _, message := range []string{"no", "real"} {
		if err := clientConn.WriteMessage(websocket.BinaryMessage, []byte(message)); err != nil {
			t.Fatalf("write websocket message %q: %v", message, err)
		}
	}

	select {
	case <-recorder.notify:
	case <-time.After(5 * time.Second):
		t.Fatal("OnMessage was never called for the complete package")
	}

	if got := recorder.recorded(); len(got) != 1 || got[0] != "real" {
		t.Fatalf("delivered packages = %#v, want [real] only: an undecided reader must not deliver a nil package", got)
	}
}

// partialWriteNetConn accepts only a prefix of the first buffer and then fails,
// the state net.Buffers.WriteTo leaves behind on a short write.
type partialWriteNetConn struct {
	mu     sync.Mutex
	writes int
	prefix int
}

func (c *partialWriteNetConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes++
	first := c.writes == 1
	c.mu.Unlock()

	if first {
		return c.prefix, errTestPartialWrite
	}
	return 0, errTestPartialWrite
}

func (*partialWriteNetConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (*partialWriteNetConn) Close() error                     { return nil }
func (*partialWriteNetConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*partialWriteNetConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*partialWriteNetConn) SetDeadline(time.Time) error      { return nil }
func (*partialWriteNetConn) SetReadDeadline(time.Time) error  { return nil }
func (*partialWriteNetConn) SetWriteDeadline(time.Time) error { return nil }

// Regression test for #130 item 4: a batch that fails part-way was reported as 0
// bytes written even though writev had already put a prefix on the wire. A
// caller treating 0 as "nothing sent" resends that prefix and desynchronizes
// the peer's decoder.
func TestWriteBytesArrayReportsPartialTCPWrite(t *testing.T) {
	netConn := &partialWriteNetConn{prefix: 2}
	ss := newTCPSession(netConn, nil).(*session)

	n, err := ss.WriteBytesArray([]byte("aaaa"), []byte("bbbb"))
	if err == nil {
		t.Fatal("WriteBytesArray returned no error although the connection write failed")
	}
	if n != 2 {
		t.Fatalf("WriteBytesArray reported %d bytes, want the 2 bytes the connection wrote before failing", n)
	}
}

// mergePathConn is neither *gettyTCPConn nor *gettyWSConn, so WriteBytesArray
// takes its merge fallback. The first Send of the merged buffer succeeds and
// the second one reports a short write.
type mergePathConn struct {
	*gettyTCPConn
	mu    sync.Mutex
	calls int
}

func (c *mergePathConn) Send(pkg any) (int, error) {
	body, ok := pkg.([]byte)
	if !ok {
		return 0, fmt.Errorf("mergePathConn.Send: unexpected pkg type %T", pkg)
	}

	c.mu.Lock()
	c.calls++
	call := c.calls
	c.mu.Unlock()

	if call == 1 {
		return len(body), nil
	}
	return 2, errTestPartialWrite
}

// Regression test for #130 item 4 (merge fallback): WriteBytes reports the
// bytes it wrote before failing - here the first maxPacketLen fragment of the
// merged buffer - and WriteBytesArray used to discard that count.
func TestWriteBytesArrayReportsPartialMergedWrite(t *testing.T) {
	conn := &mergePathConn{gettyTCPConn: newGettyTCPConn(&partialWriteNetConn{prefix: 1})}
	ss := newSession(nil, conn)

	n, err := ss.WriteBytesArray(make([]byte, 10000), make([]byte, 10000))
	if err == nil {
		t.Fatal("WriteBytesArray returned no error although the merged write failed")
	}
	if n != maxPacketLen {
		t.Fatalf("WriteBytesArray reported %d bytes, want the %d bytes written before the failing fragment",
			n, maxPacketLen)
	}
}

// Regression test for #130 item 6: a UDP session without a Reader dove straight
// into the datagram loop, so the misconfiguration only surfaced as a nil
// dereference panic once a datagram arrived (and as an unreadable stack even
// then). It must fail like the TCP branch does. No datagram is sent here, so
// passing means the session reported the missing reader instead of parking in
// the read loop. The panic itself is swallowed by handlePackage's own recover.
func TestUDPHandlePackageWithoutReaderFailsConfiguration(t *testing.T) {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udpConn.Close() }()

	ss := newUDPSession(udpConn, newServer(UDP_ENDPOINT)).(*session)

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		ss.handlePackage()
	}()
	t.Cleanup(func() {
		ss.Close()
		select {
		case <-handlerDone:
		case <-time.After(5 * time.Second):
			t.Error("handlePackage did not return after the session was closed")
		}
	})

	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("handlePackage parked in the UDP read loop with a nil reader; " +
			"a misconfigured session must report the missing reader up front")
	}
}

// gatedWriteConn wraps the socket underneath a websocket client connection and
// parks its first Write after arm() until releaseFirst(), so a test can hold a
// write in flight and observe what a concurrent writer is allowed to do.
//
// The handshake goes through the same connection, which is why the gate is armed
// only after Dial returns.
type gatedWriteConn struct {
	net.Conn

	mu      sync.Mutex
	armed   bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *gatedWriteConn) arm() {
	c.mu.Lock()
	if !c.armed {
		c.armed = true
		c.entered = make(chan struct{})
		c.release = make(chan struct{})
	}
	c.mu.Unlock()
}

func (c *gatedWriteConn) waitEntered(t *testing.T) {
	t.Helper()

	c.mu.Lock()
	entered := c.entered
	c.mu.Unlock()
	if entered == nil {
		t.Fatal("gated connection was never armed")
	}

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no write reached the gated connection")
	}
}

func (c *gatedWriteConn) releaseFirst() {
	c.mu.Lock()
	release := c.release
	c.mu.Unlock()
	if release != nil {
		close(release)
	}
}

func (c *gatedWriteConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	armed, entered, release := c.armed, c.entered, c.release
	c.mu.Unlock()

	if armed {
		c.once.Do(func() {
			close(entered)
			<-release
		})
	}
	return c.Conn.Write(p)
}

// newGatedWSPair returns a session over a websocket connection whose first write
// after arm() is parked, plus the gated socket and the peer side of the pair.
func newGatedWSPair(t *testing.T) (ss *session, gated *gatedWriteConn, peer *websocket.Conn) {
	t.Helper()

	var (
		upgrader = websocket.Upgrader{}
		serverCh = make(chan *websocket.Conn, 1)
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverCh <- conn
	}))
	t.Cleanup(srv.Close)

	gatedCh := make(chan *gatedWriteConn, 1)
	dialer := websocket.Dialer{
		NetDial: func(network, addr string) (net.Conn, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			raw, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			conn := &gatedWriteConn{Conn: raw}
			gatedCh <- conn
			return conn, nil
		},
	}
	clientWS, resp, err := dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket test server: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	t.Cleanup(func() { _ = clientWS.Close() })
	gated = <-gatedCh

	select {
	case peer = <-serverCh:
	case <-time.After(5 * time.Second):
		t.Fatal("websocket test server did not hand over the upgraded connection")
	}
	t.Cleanup(func() { _ = peer.Close() })

	return newWSSession(clientWS, nil).(*session), gated, peer
}

// Regression test for the ws batch lock: WriteBytesArray sends one message per
// pkg, so it must hold packetLock exclusively. A read lock lets another writer
// start in the gap between two of the batch's messages - one WriteMessage at a
// time is not enough, because the gap between messages is exactly the window -
// and the peer then sees a batch it cannot recognise. The tcp path cannot do
// that, since its whole batch is a single conn.Send.
//
// The batch is parked inside its first write while the probe runs, so this says
// something about a real in-flight write rather than about the code's shape. A
// message-order assertion cannot be made deterministic here: gettyWSConn.writeLock
// serialises the concurrent WriteMessage as long as a write is parked inside a
// message, and between two messages the winner is a scheduler coin toss.
func TestWSBatchWriteHoldsExclusiveLock(t *testing.T) {
	ss, gated, _ := newGatedWSPair(t)
	gated.arm()

	batchDone := make(chan error, 1)
	go func() {
		_, err := ss.WriteBytesArray([]byte("A1"), []byte("A2"))
		batchDone <- err
	}()
	gated.waitEntered(t)

	if ss.packetLock.TryRLock() {
		ss.packetLock.RUnlock()
		gated.releaseFirst()
		<-batchDone
		t.Fatal("another writer could take the read lock while a ws batch was mid-flight: the batch is not protected across its messages")
	}

	gated.releaseFirst()
	if err := <-batchDone; err != nil {
		t.Fatalf("WriteBytesArray: %v", err)
	}
}
