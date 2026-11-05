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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

import (
	"github.com/gorilla/websocket"
)

import (
	logutil "github.com/AlexStocks/getty/util"
)

// serverStartupCloseListener supplies startup behavior and observes read cleanup.
type serverStartupCloseListener struct {
	recordingEventListener
	onOpen func() error
	closed chan struct{}
}

// OnOpen runs the startup scenario selected by the test.
func (l *serverStartupCloseListener) OnOpen(Session) error { return l.onOpen() }

// OnClose signals that the session's read loop reached its cleanup path.
func (l *serverStartupCloseListener) OnClose(Session) { close(l.closed) }

// TestServerCloseStartupTransports exercises both initialization callbacks over
// real TCP/UDP/WS/WSS endpoints. Subprocesses contain any self-wait regression.
func TestServerCloseStartupTransports(t *testing.T) {
	const scenarioEnv = "GETTY_TEST_SERVER_CLOSE_STARTUP"
	scenario := os.Getenv(scenarioEnv)
	if scenario == "" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		for _, protocol := range []string{"TCP", "UDP", "WS", "WSS"} {
			for _, phase := range []string{"new-session", "open"} {
				for _, caller := range []string{"callback", "external"} {
					name := protocol + "/" + phase + "/" + caller
					t.Run(name, func(t *testing.T) {
						cmd := exec.CommandContext(t.Context(), binary,
							"-test.run=^TestServerCloseStartupTransports$", "-test.timeout=15s")
						cmd.Env = append(os.Environ(), scenarioEnv+"="+name)
						if output, err := cmd.CombinedOutput(); err != nil {
							t.Fatalf("startup close failed: %v\n%s", err, output)
						}
					})
				}
			}
		}
		return
	}

	capture := &logLineRecorder{}
	previousLogger := logutil.GetLogger()
	logutil.SetLogger(capture)
	defer logutil.SetLogger(previousLogger)

	parts := strings.Split(scenario, "/")
	protocol, phase, caller := parts[0], parts[1], parts[2]
	kind := map[string]EndPointType{
		"TCP": TCP_SERVER, "UDP": UDP_ENDPOINT, "WS": WS_SERVER, "WSS": WSS_SERVER,
	}[protocol]
	opts := []ServerOption{WithLocalAddress("127.0.0.1:0")}
	var roots *x509.CertPool
	if protocol == "WS" || protocol == "WSS" {
		opts = append(opts, WithWebsocketServerPath("/ws"))
	}
	if protocol == "WSS" {
		ca := newTestCertificateAuthority(t)
		certPath, keyPath := writeTLSPair(t, t.TempDir(), "server", ca.issue(t, "startup close", true))
		opts = append(opts, WithWebsocketServerCert(certPath), WithWebsocketServerPrivateKey(keyPath))
		roots = x509.NewCertPool()
		roots.AddCert(ca.cert)
	}
	srv := newServer(kind, opts...)
	entered := make(chan struct{})
	returned := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	opened := make(chan struct{})
	closed := make(chan struct{})
	sessions := make(chan Session, 1)
	var opens atomic.Int32
	hook := func() {
		close(entered)
		if caller == "callback" {
			srv.Close()
			srv.Close()
			close(returned)
		}
		<-release
		close(exited)
	}
	listener := &serverStartupCloseListener{
		closed: closed,
		onOpen: func() error {
			opens.Add(1)
			close(opened)
			if phase == "open" {
				hook()
			}
			return nil
		},
	}
	srv.RunEventLoop(func(ss Session) error {
		ss.SetPkgHandler(wholeBufferReadWriter{})
		ss.SetEventListener(listener)
		sessions <- ss
		if phase == "new-session" {
			hook()
		}
		return nil
	})

	var tcp net.Conn
	var ws *websocket.Conn
	var err error
	switch protocol {
	case "TCP":
		tcp, err = net.DialTimeout("tcp", srv.Listener().Addr().String(), 3*time.Second)
	case "WS", "WSS":
		scheme := "ws"
		if protocol == "WSS" {
			scheme = "wss"
		}
		dialer := websocket.Dialer{
			HandshakeTimeout: 3 * time.Second,
			TLSClientConfig:  &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		}
		ws, _, err = dialer.Dial(scheme+"://"+srv.Listener().Addr().String()+"/ws", nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	waitForReset(t, entered)
	ss := (<-sessions).(*session)
	if caller == "callback" {
		waitForReset(t, returned)
	}

	// All external callers must return while initialization is held open. For
	// WS/WSS, this also proves HTTP handlers are not part of the loop wait group.
	var closers sync.WaitGroup
	closers.Add(4)
	for range 4 {
		go func() { defer closers.Done(); srv.Close() }()
	}
	externalDone := make(chan struct{})
	go func() { closers.Wait(); close(externalDone) }()
	waitForReset(t, externalDone)
	if !srv.IsClosed() {
		t.Fatal("Close returned without initiating shutdown")
	}
	if srv.startEventLoop() {
		srv.wg.Done()
		t.Fatal("closed server registered another event loop")
	}
	select {
	case <-exited:
		t.Fatal("initialization was not held open during Close")
	default:
	}
	close(release)
	waitForReset(t, exited)
	loopsDone := make(chan struct{})
	go func() { srv.wg.Wait(); close(loopsDone) }()
	waitForReset(t, loopsDone)
	srv.Close()
	if (protocol == "TCP" || protocol == "UDP") && phase == "new-session" {
		if capture.contains(".Accept() = err") || capture.contains(".newSession(ss{") {
			t.Fatalf("normal startup shutdown logged as a failure: %v", capture.snapshot())
		}
	}
	if protocol == "TCP" {
		defer func() { _ = tcp.Close() }()
		assertPeerClosed(t, tcp, "Server.Close during initialization")
	}
	if protocol == "TCP" || protocol == "UDP" {
		if phase == "new-session" && opens.Load() != 0 {
			t.Fatal("OnOpen started after NewSessionCallback closed the server")
		}
		if phase == "open" {
			waitForReset(t, closed)
			ss.grWG.Wait()
			ss.lifecycle.wait()
		}
	} else {
		// Upgraded websocket connections are hijacked HTTP connections. Close
		// waits for Serve, not for these handlers or established sessions.
		waitForReset(t, opened)
		_ = ws.Close()
		waitForReset(t, closed)
		ss.grWG.Wait()
		ss.lifecycle.wait()
	}
}

// TestServerCloseWaitsAfterStartup verifies that the early-return exception ends
// when initialization returns, even if another registered loop remains active.
func TestServerCloseWaitsAfterStartup(t *testing.T) {
	srv := newServer(TCP_SERVER)
	if !srv.startEventLoop() {
		t.Fatal("could not register event loop")
	}
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = local.Close(); _ = remote.Close() })
	srv.streamListener = &singleAcceptListener{conn: &startupDistinctAddrConn{Conn: local}}
	entered := make(chan struct{})
	releaseInit := make(chan struct{})
	releaseLoop := make(chan struct{})
	initDone := make(chan struct{})
	var initOnce, loopOnce sync.Once
	finishInit := func() { initOnce.Do(func() { close(releaseInit) }) }
	finishLoop := func() { loopOnce.Do(func() { close(releaseLoop) }) }
	t.Cleanup(finishLoop)
	t.Cleanup(finishInit)
	go func() {
		defer srv.wg.Done()
		// Server shutdown short-circuits run; this fixture intentionally omits session handlers.
		_, _ = srv.accept(func(Session) error {
			close(entered)
			<-releaseInit
			return nil
		})
		close(initDone)
		<-releaseLoop
	}()
	waitForReset(t, entered)
	first := make(chan struct{})
	go func() { srv.Close(); close(first) }()
	waitForReset(t, first)
	finishInit()
	waitForReset(t, initDone)
	second := make(chan struct{})
	go func() { srv.Close(); close(second) }()
	select {
	case <-second:
		t.Fatal("Close stopped waiting after initialization finished")
	case <-time.After(50 * time.Millisecond):
	}
	finishLoop()
	waitForReset(t, second)
}

// TestServerStartupFailureReleasesActivity covers both callbacks' error and panic
// paths, which must not leave later external Close calls in early-return mode.
func TestServerStartupFailureReleasesActivity(t *testing.T) {
	for _, phase := range []string{"new-session", "open"} {
		for _, panics := range []bool{false, true} {
			name := phase + "/error"
			if panics {
				name = phase + "/panic"
			}
			t.Run(name, func(t *testing.T) {
				srv := newServer(TCP_SERVER)
				local, remote := net.Pipe()
				t.Cleanup(func() { _ = local.Close(); _ = remote.Close() })
				srv.streamListener = &singleAcceptListener{conn: &startupDistinctAddrConn{Conn: local}}
				var ss Session
				fail := func() error {
					if panics {
						panic("startup failure")
					}
					return errors.New("startup failure")
				}
				listener := &serverStartupCloseListener{closed: make(chan struct{}), onOpen: fail}
				_, err := srv.accept(func(candidate Session) error {
					ss = candidate
					ss.SetPkgHandler(wholeBufferReadWriter{})
					ss.SetEventListener(listener)
					if phase == "new-session" {
						return fail()
					}
					return nil
				})
				if phase == "new-session" && err == nil {
					t.Fatal("initialization failure was lost")
				}
				if phase == "open" && !ss.IsClosed() {
					t.Fatal("OnOpen failure left the session open")
				}
				if !srv.startEventLoop() {
					t.Fatal("initialization failure unexpectedly closed the server")
				}
				var doneOnce sync.Once
				finish := func() { doneOnce.Do(srv.wg.Done) }
				t.Cleanup(finish)
				closeDone := make(chan struct{})
				go func() { srv.Close(); close(closeDone) }()
				waitForReset(t, srv.done)
				select {
				case <-closeDone:
					t.Fatal("failed initialization left Close in early-return mode")
				case <-time.After(50 * time.Millisecond):
				}
				finish()
				waitForReset(t, closeDone)
				if phase == "open" {
					ss.(*session).grWG.Wait()
					ss.(*session).lifecycle.wait()
				}
			})
		}
	}
}

// startupDeadlineConn holds session shutdown after OnOpen has returned, without
// keeping the initialization callback itself active.
type startupDeadlineConn struct {
	net.Conn
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

// SetReadDeadline exposes the point where session shutdown/read startup pauses.
func (c *startupDeadlineConn) SetReadDeadline(deadline time.Time) error {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return c.Conn.SetReadDeadline(deadline)
}

// TestServerCloseWaitsDuringPostCallbackCleanup distinguishes active callbacks
// from the rest of initialization: cleanup must retain normal Close waiting.
func TestServerCloseWaitsDuringPostCallbackCleanup(t *testing.T) {
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = local.Close(); _ = remote.Close() })
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)
	conn := &startupDeadlineConn{
		Conn: &startupDistinctAddrConn{Conn: local}, entered: make(chan struct{}), release: release,
	}
	srv := newServer(TCP_SERVER)
	srv.streamListener = &singleAcceptListener{conn: conn}
	sessions := make(chan *session, 1)
	srv.runTCPEventLoop(func(ss Session) error {
		ss.SetPkgHandler(wholeBufferReadWriter{})
		ss.SetEventListener(&serverStartupCloseListener{
			onOpen: func() error { srv.Close(); return nil }, closed: make(chan struct{}),
		})
		sessions <- ss.(*session)
		return nil
	})
	waitForReset(t, conn.entered)
	ss := <-sessions
	closed := make(chan struct{})
	go func() { srv.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned during cleanup with no initialization callback active")
	case <-time.After(50 * time.Millisecond):
	}
	finish()
	waitForReset(t, closed)
	ss.grWG.Wait()
	ss.lifecycle.wait()
}

// TestServerCloseAndResetFromOnOpen keeps startup cleanup within the session's
// tracked lifecycle, including when Reset is requested from the same callback.
func TestServerCloseAndResetFromOnOpen(t *testing.T) {
	for i := range 200 {
		srv := newServer(TCP_SERVER)
		local, remote := net.Pipe()
		srv.streamListener = &singleAcceptListener{conn: &startupDistinctAddrConn{Conn: local}}
		requests := make(chan (<-chan error), 1)
		sessions := make(chan *session, 1)
		srv.runTCPEventLoop(func(ss Session) error {
			sessions <- ss.(*session)
			ss.SetPkgHandler(wholeBufferReadWriter{})
			ss.SetEventListener(&serverStartupCloseListener{
				closed: make(chan struct{}),
				onOpen: func() error {
					requests <- ss.(*session).ResetAsync()
					srv.Close()
					return nil
				},
			})
			return nil
		})
		ss := <-sessions
		if err := <-(<-requests); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		_ = remote.Close()
		if ss.IsClosed() || ss.Conn() != nil {
			t.Fatalf("iteration %d: server startup cleanup changed the completed reset state", i)
		}
	}
}

// TestServerCloseRacesEventLoopRegistration preserves registration/Wait ordering
// while repeated Close calls compete with startup on an otherwise idle server.
func TestServerCloseRacesEventLoopRegistration(t *testing.T) {
	for range 50 {
		srv := newServer(TCP_SERVER)
		start := make(chan struct{})
		var workers sync.WaitGroup
		workers.Add(5)
		go func() {
			defer workers.Done()
			<-start
			if srv.startEventLoop() {
				srv.wg.Done()
			}
		}()
		for range 4 {
			go func() { defer workers.Done(); <-start; srv.Close() }()
		}
		close(start)
		workers.Wait()
		if srv.startEventLoop() {
			srv.wg.Done()
			t.Fatal("closed server accepted a late event-loop registration")
		}
	}
}
