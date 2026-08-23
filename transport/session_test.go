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
	"errors"
	"io"
	"math"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

var (
	errTestReadFailure      = errors.New("test read failure")
	errUnexpectedSecondRead = errors.New("unexpected second read")
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
	defer c1.Close()
	defer c2.Close()

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
