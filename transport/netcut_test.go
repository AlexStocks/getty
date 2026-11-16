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
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

type netCutMode uint8

const (
	netCutFIN netCutMode = iota
	netCutRST
)

const netCutWaitTimeout = 5 * time.Second

// netCutServer accepts real TCP connections and immediately terminates each
// one with the selected peer-close mode. Its accept counter and change signal
// let tests synchronize on observed connections instead of sleeping.
type netCutServer struct {
	listener *net.TCPListener
	mode     netCutMode

	mu           sync.Mutex
	accepts      int
	acceptsCh    chan struct{}
	operationErr error

	closeOnce sync.Once
	closeErr  error
	wg        sync.WaitGroup
}

func newNetCutServer(t *testing.T, mode netCutMode) *netCutServer {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen for net cut fixture: %v", err)
	}

	cut := &netCutServer{
		listener:  listener,
		mode:      mode,
		acceptsCh: make(chan struct{}),
	}
	cut.wg.Add(1)
	go cut.acceptLoop()
	t.Cleanup(func() {
		if err := cut.Close(); err != nil {
			t.Errorf("close net cut fixture: %v", err)
		}
	})
	return cut
}

func (cut *netCutServer) acceptLoop() {
	defer cut.wg.Done()
	for {
		conn, err := cut.listener.AcceptTCP()
		if err != nil {
			return
		}

		if cut.mode == netCutRST {
			if err := conn.SetLinger(0); err != nil {
				cut.recordError(fmt.Errorf("set zero linger for peer RST: %w", err))
			}
		}
		if err := conn.Close(); err != nil {
			cut.recordError(fmt.Errorf("close accepted peer connection: %w", err))
		}
		cut.recordAccept()
	}
}

func (cut *netCutServer) recordError(err error) {
	cut.mu.Lock()
	defer cut.mu.Unlock()
	cut.operationErr = errors.Join(cut.operationErr, err)
}

func (cut *netCutServer) recordAccept() {
	cut.mu.Lock()
	cut.accepts++
	close(cut.acceptsCh)
	cut.acceptsCh = make(chan struct{})
	cut.mu.Unlock()
}

func (cut *netCutServer) Addr() string {
	return cut.listener.Addr().String()
}

// Dial opens a loopback TCP connection to the fixture and arranges for the
// connection to be closed during test cleanup, including on failure paths.
func (cut *netCutServer) Dial(t *testing.T) *net.TCPConn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", cut.Addr(), netCutWaitTimeout)
	if err != nil {
		t.Fatalf("dial net cut fixture at %s: %v", cut.Addr(), err)
	}
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		_ = conn.Close()
		t.Fatalf("net cut connection has type %T, want *net.TCPConn", conn)
	}
	t.Cleanup(func() { _ = tcpConn.Close() })
	return tcpConn
}

// AcceptCount reports the number of connections accepted and terminated by
// the fixture.
func (cut *netCutServer) AcceptCount() int {
	cut.mu.Lock()
	defer cut.mu.Unlock()
	return cut.accepts
}

// WaitForAccepts blocks until at least want connections have been accepted.
// The timeout is only a failure bound; the change channel provides the actual
// synchronization.
func (cut *netCutServer) WaitForAccepts(t *testing.T, want int) {
	t.Helper()
	timer := time.NewTimer(netCutWaitTimeout)
	defer timer.Stop()
	for {
		cut.mu.Lock()
		if cut.accepts >= want {
			cut.mu.Unlock()
			return
		}
		changed := cut.acceptsCh
		cut.mu.Unlock()

		select {
		case <-changed:
		case <-timer.C:
			t.Fatalf("timed out waiting for %d accepts; got %d", want, cut.AcceptCount())
		}
	}
}

// Err reports a fixture-side error, including a failure to configure RST.
func (cut *netCutServer) Err() error {
	cut.mu.Lock()
	defer cut.mu.Unlock()
	return cut.operationErr
}

// Close closes the listener and waits for its accept goroutine. It is safe to
// call explicitly and again from t.Cleanup.
func (cut *netCutServer) Close() error {
	cut.closeOnce.Do(func() { cut.closeErr = cut.listener.Close() })
	cut.wg.Wait()
	return cut.closeErr
}

func readPeerCloseError(t *testing.T, conn *net.TCPConn) error {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(netCutWaitTimeout)); err != nil {
		t.Fatalf("set peer-close read deadline: %v", err)
	}
	buf := make([]byte, 1)
	n, err := conn.Read(buf)
	if n != 0 {
		t.Fatalf("peer-close read returned %d bytes, want 0", n)
	}
	return err
}

func TestPeerFINProducesEOF(t *testing.T) {
	cut := newNetCutServer(t, netCutFIN)
	conn := cut.Dial(t)
	cut.WaitForAccepts(t, 1)

	err := readPeerCloseError(t, conn)
	t.Logf("peer FIN read error: %v", err)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("peer FIN read error = %v, want io.EOF", err)
	}
	if got := cut.AcceptCount(); got != 1 {
		t.Fatalf("accept count = %d, want 1", got)
	}
	if err := cut.Err(); err != nil {
		t.Fatalf("fixture error: %v", err)
	}
}

func TestPeerRSTProducesNonEOFError(t *testing.T) {
	cut := newNetCutServer(t, netCutRST)
	conn := cut.Dial(t)
	cut.WaitForAccepts(t, 1)

	err := readPeerCloseError(t, conn)
	t.Logf("peer RST read error: %v", err)
	if err == nil {
		t.Fatal("peer RST read returned nil error, want a reset error")
	}
	if errors.Is(err, io.EOF) {
		t.Fatalf("peer RST read error = %v, want non-EOF error", err)
	}
	if got := cut.AcceptCount(); got != 1 {
		t.Fatalf("accept count = %d, want 1", got)
	}
	if err := cut.Err(); err != nil {
		t.Fatalf("fixture error: %v", err)
	}
}

func TestNetCutCountsThreeAcceptedConnections(t *testing.T) {
	cut := newNetCutServer(t, netCutFIN)
	clients := make([]*net.TCPConn, 3)
	for i := range clients {
		clients[i] = cut.Dial(t)
		cut.WaitForAccepts(t, i+1)
	}

	for i, conn := range clients {
		err := readPeerCloseError(t, conn)
		if !errors.Is(err, io.EOF) {
			t.Errorf("connection %d read error = %v, want io.EOF", i+1, err)
		}
	}
	if got := cut.AcceptCount(); got != 3 {
		t.Fatalf("accept count = %d, want 3", got)
	}
}

func TestNetCutHighVolumeFINCleansUp(t *testing.T) {
	const connections = 200
	goroutinesBefore := runtime.NumGoroutine()
	started := time.Now()
	cut := newNetCutServer(t, netCutFIN)
	clients := make([]*net.TCPConn, connections)
	for i := range clients {
		clients[i] = cut.Dial(t)
	}
	cut.WaitForAccepts(t, connections)

	for i, conn := range clients {
		err := readPeerCloseError(t, conn)
		if !errors.Is(err, io.EOF) {
			t.Fatalf("connection %d read error = %v, want io.EOF", i+1, err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("close client connection %d: %v", i+1, err)
		}
	}
	if got := cut.AcceptCount(); got != connections {
		t.Fatalf("accept count = %d, want %d", got, connections)
	}
	if elapsed := time.Since(started); elapsed >= netCutWaitTimeout {
		t.Fatalf("%d FIN cycles took %v, want less than %v", connections, elapsed, netCutWaitTimeout)
	}
	if err := cut.Close(); err != nil {
		t.Fatalf("close net cut listener: %v", err)
	}
	listener, err := net.Listen("tcp", cut.Addr())
	if err != nil {
		t.Fatalf("fixture address %s remains occupied after Close: %v", cut.Addr(), err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener rebound at %s: %v", cut.Addr(), err)
	}

	if got := runtime.NumGoroutine(); got > goroutinesBefore {
		t.Fatalf("goroutine count after closing fixture = %d, want at most baseline %d", got, goroutinesBefore)
	}
}
