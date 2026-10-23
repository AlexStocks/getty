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
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

import (
	"github.com/stretchr/testify/assert"
)

type listenerTLSConfigBuilder struct {
	config *tls.Config
	err    error
	calls  int
}

// BuildTlsConfig records each invocation and returns the configured test result.
func (b *listenerTLSConfigBuilder) BuildTlsConfig() (*tls.Config, error) {
	b.calls++
	return b.config, b.err
}

// TestTCPListenerRejectsInvalidTLSConfig verifies that invalid TLS configurations
// fail without publishing a listener for empty, bare-IP, and explicit addresses.
func TestTCPListenerRejectsInvalidTLSConfig(t *testing.T) {
	buildErr := errors.New("test certificate failure")
	for _, address := range []string{"", "127.0.0.1", "127.0.0.1:0"} {
		for _, test := range []struct {
			name   string
			config *tls.Config
			err    error
		}{
			{name: "builder error", err: buildErr},
			{name: "nil config"},
			{name: "no certificate", config: &tls.Config{}},
		} {
			t.Run(address+"/"+test.name, func(t *testing.T) {
				builder := &listenerTLSConfigBuilder{config: test.config, err: test.err}
				srv := newServer(TCP_SERVER, WithLocalAddress(address),
					WithServerSslEnabled(true), WithServerTlsConfigBuilder(builder))
				t.Cleanup(srv.Close)

				err := srv.listenTCP()
				if err == nil {
					t.Fatal("listenTCP accepted an invalid TLS configuration")
				}
				if test.err != nil && !errors.Is(err, test.err) {
					t.Fatalf("listenTCP error = %v, want %v", err, test.err)
				}
				if builder.calls != 1 {
					t.Fatalf("TLS builder calls = %d, want 1", builder.calls)
				}
				if srv.Listener() != nil {
					t.Fatal("listenTCP published a listener despite an invalid TLS configuration")
				}
			})
		}
	}
}

// TestTCPListenerTransport verifies plain TCP and certificate-verified TLS
// communication for empty, bare-IP, and explicit addresses.
func TestTCPListenerTransport(t *testing.T) {
	ca := newTestCertificateAuthority(t)
	certificate := ca.issue(t, "getty test server", true)
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)

	for _, address := range []string{"", "127.0.0.1", "127.0.0.1:0"} {
		for _, secure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/TLS=%t", address, secure), func(t *testing.T) {
				builder := &listenerTLSConfigBuilder{config: &tls.Config{
					Certificates: []tls.Certificate{certificate},
					MinVersion:   tls.VersionTLS12,
				}}
				srv := newServer(TCP_SERVER, WithLocalAddress(address),
					WithServerSslEnabled(secure), WithServerTlsConfigBuilder(builder))
				t.Cleanup(srv.Close)
				if err := srv.listenTCP(); err != nil {
					t.Fatal(err)
				}
				wantCalls := 0
				if secure {
					wantCalls = 1
				}
				if builder.calls != wantCalls {
					t.Fatalf("TLS builder calls = %d, want %d", builder.calls, wantCalls)
				}

				listener := srv.Listener()
				addr := listener.Addr().(*net.TCPAddr)
				if addr.Port == 0 {
					t.Fatal("listenTCP did not allocate a port")
				}
				if address != "" && !addr.IP.Equal(net.ParseIP("127.0.0.1")) {
					t.Fatalf("listener address = %v, want loopback", addr)
				}
				payload := []byte("hello")
				received := make(chan error, 1)
				go func() {
					conn, err := listener.Accept()
					if err != nil {
						received <- err
						return
					}
					defer func() { _ = conn.Close() }()
					if _, isTLS := conn.(*tls.Conn); isTLS != secure {
						received <- fmt.Errorf("accepted connection type = %T, TLS enabled = %t", conn, secure)
						return
					}
					if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
						received <- err
						return
					}
					buf := make([]byte, len(payload))
					_, err = io.ReadFull(conn, buf)
					if err == nil && !bytes.Equal(buf, payload) {
						err = fmt.Errorf("received %q, want %q", buf, payload)
					}
					received <- err
				}()

				dialer := &net.Dialer{Timeout: 3 * time.Second}
				peer := net.JoinHostPort("127.0.0.1", fmt.Sprint(addr.Port))
				var conn net.Conn
				var err error
				if secure {
					conn, err = tls.DialWithDialer(dialer, "tcp", peer, &tls.Config{
						RootCAs: roots, MinVersion: tls.VersionTLS12,
					})
				} else {
					conn, err = dialer.Dial("tcp", peer)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = conn.Close() }()
				if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					t.Fatal(err)
				}
				if _, err := conn.Write(payload); err != nil {
					t.Fatal(err)
				}
				if err := <-received; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func testTCPServer(t *testing.T, address string) {
	var (
		server           *server
		serverMsgHandler MessageHandler
	)

	func() {
		server = newServer(
			TCP_SERVER,
			WithLocalAddress(address),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		server.RunEventLoop(newServerSession)
		assert.True(t, server.ID() > 0)
		assert.True(t, server.EndPointType() == TCP_SERVER)
		assert.NotNil(t, server.Listener())
		assert.Nil(t, server.PacketConn())
	}()
	time.Sleep(500e6)

	addr := server.streamListener.Addr().String()
	t.Logf("@address:%s, tcp server addr: %v", address, addr)
	clt := newClient(TCP_CLIENT,
		WithServerAddress(addr),
		WithReconnectInterval(5e8),
		WithConnectionNumber(1),
	)
	assert.NotNil(t, clt)
	assert.True(t, clt.ID() > 0)
	assert.Equal(t, clt.EndPointType(), TCP_CLIENT)

	var msgHandler MessageHandler
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	clt.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	clt.Close()
	assert.True(t, clt.IsClosed())

	server.Close()
	assert.True(t, server.IsClosed())
}

// testTCPTlsServer connects Getty client and server sessions over mutual TLS
// using certificates issued by a temporary test certificate authority.
func testTCPTlsServer(t *testing.T, address string) {
	t.Helper()

	var (
		server           *server
		serverMsgHandler MessageHandler
	)
	ca := newTestCertificateAuthority(t)
	dir := t.TempDir()
	serverPemPath, serverKeyPath := writeTLSPair(t, dir, "server", ca.issue(t, "getty test server", true))
	clientPemPath, clientKeyPath := writeTLSPair(t, dir, "client", ca.issue(t, "getty test client", false))
	caPemPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPemPath, ca.pem, 0o600); err != nil {
		t.Fatal(err)
	}

	configBuilder := &ServerTlsConfigBuilder{
		ServerKeyCertChainPath:        serverPemPath,
		ServerPrivateKeyPath:          serverKeyPath,
		ServerTrustCertCollectionPath: caPemPath,
	}

	func() {
		server = newServer(
			TCP_SERVER,
			WithLocalAddress(address),
			WithServerSslEnabled(true),
			WithServerTlsConfigBuilder(configBuilder),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		server.RunEventLoop(newServerSession)
		assert.True(t, server.ID() > 0)
		assert.True(t, server.EndPointType() == TCP_SERVER)
		assert.NotNil(t, server.Listener())
		assert.Nil(t, server.PacketConn())
	}()
	time.Sleep(500e6)

	addr := server.streamListener.Addr().String()
	t.Logf("@address:%s, tcp server addr: %v", address, addr)
	clientConfig := &ClientTlsConfigBuilder{
		ClientKeyCertChainPath:        clientPemPath,
		ClientTrustCertCollectionPath: caPemPath,
		ClientPrivateKeyPath:          clientKeyPath,
	}

	clt := newClient(TCP_CLIENT,
		WithServerAddress(addr),
		WithReconnectInterval(5e8),
		WithConnectionNumber(1),
		WithClientSslEnabled(true),
		WithClientTlsConfigBuilder(clientConfig),
	)
	assert.NotNil(t, clt)
	assert.True(t, clt.ID() > 0)
	assert.Equal(t, clt.EndPointType(), TCP_CLIENT)

	var msgHandler MessageHandler
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	clt.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	clt.Close()
	assert.True(t, clt.IsClosed())

	server.Close()
	assert.True(t, server.IsClosed())
}

func testUDPServer(t *testing.T, address string) {
	var (
		server           *server
		serverMsgHandler MessageHandler
	)
	func() {
		server = newServer(
			UDP_ENDPOINT,
			WithLocalAddress(address),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		server.RunEventLoop(newServerSession)
		assert.True(t, server.ID() > 0)
		assert.True(t, server.EndPointType() == UDP_ENDPOINT)
		assert.NotNil(t, server.pktListener)
	}()
	time.Sleep(500e6)

	addr := server.pktListener.LocalAddr().String()
	t.Logf("@address:%s, udp server addr: %v", address, addr)
}

func TestServerCloseKeepsPublishedListener(t *testing.T) {
	t.Run("TCP", func(t *testing.T) {
		server := newServer(TCP_SERVER, WithLocalAddress("127.0.0.1:0"))
		if err := server.listen(); err != nil {
			t.Fatal(err)
		}
		listener := server.Listener()
		if listener == nil {
			t.Fatal("listen did not publish the TCP listener")
		}

		server.Close()

		if got := server.Listener(); got != listener {
			t.Fatalf("Listener() after Close = %v, want the published listener %v", got, listener)
		}
	})

	t.Run("UDP", func(t *testing.T) {
		server := newServer(UDP_ENDPOINT, WithLocalAddress("127.0.0.1:0"))
		if err := server.listen(); err != nil {
			t.Fatal(err)
		}
		listener := server.PacketConn()
		if listener == nil {
			t.Fatal("listen did not publish the UDP listener")
		}

		server.Close()

		if got := server.PacketConn(); got != listener {
			t.Fatalf("PacketConn() after Close = %v, want the published listener %v", got, listener)
		}
	})
}

func TestServerClosePreventsLateListenerPublication(t *testing.T) {
	t.Run("already closed", func(t *testing.T) {
		server := newServer(TCP_SERVER, WithLocalAddress("127.0.0.1:0"))
		server.Close()
		server.RunEventLoop(func(Session) error { return nil })
		if server.Listener() != nil {
			t.Fatal("closed server opened a listener")
		}
	})

	t.Run("TCP", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		server := newServer(TCP_SERVER)
		server.lock.Lock()
		closeDone := make(chan struct{})
		go func() {
			server.Close()
			close(closeDone)
		}()
		select {
		case <-server.done:
		case <-time.After(time.Second):
			server.lock.Unlock()
			t.Fatal("Close did not start shutdown")
		}

		publishDone := make(chan error, 1)
		go func() {
			publishDone <- server.publishStreamListener(listener)
		}()
		server.lock.Unlock()

		if err := <-publishDone; !errors.Is(err, errServerClosed) {
			t.Fatalf("publishStreamListener returned %v, want %v", err, errServerClosed)
		}
		<-closeDone
		if server.Listener() != nil {
			t.Fatal("closed server published a late TCP listener")
		}
		if _, err := listener.Accept(); err == nil {
			t.Fatal("late TCP listener remained open after rejected publication")
		}
	})

	t.Run("UDP", func(t *testing.T) {
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		server := newServer(UDP_ENDPOINT)
		server.lock.Lock()
		closeDone := make(chan struct{})
		go func() {
			server.Close()
			close(closeDone)
		}()
		select {
		case <-server.done:
		case <-time.After(time.Second):
			server.lock.Unlock()
			t.Fatal("Close did not start shutdown")
		}

		publishDone := make(chan error, 1)
		go func() {
			publishDone <- server.publishPacketListener(listener)
		}()
		server.lock.Unlock()

		if err := <-publishDone; !errors.Is(err, errServerClosed) {
			t.Fatalf("publishPacketListener returned %v, want %v", err, errServerClosed)
		}
		<-closeDone
		if server.PacketConn() != nil {
			t.Fatal("closed server published a late UDP listener")
		}
		if _, _, err := listener.ReadFrom(make([]byte, 1)); err == nil {
			t.Fatal("late UDP listener remained open after rejected publication")
		}
	})
}

// TestServer exercises TCP and UDP listeners with automatically assigned ports
// and a TLS-enabled TCP server bound to a bare IP address.
func TestServer(t *testing.T) {
	var addr string

	testTCPServer(t, addr)
	testUDPServer(t, addr)

	addr = "127.0.0.1:0"
	testTCPServer(t, addr)
	testUDPServer(t, addr)

	addr = "127.0.0.1"
	testTCPServer(t, addr)
	testUDPServer(t, addr)
	testTCPTlsServer(t, addr)
}

// Regression test for #97: normal WSS shutdown must not panic on http.ErrServerClosed.
func TestWSSServerCloseDoesNotPanic(t *testing.T) {
	certPath, err := filepath.Abs("../examples/profiles/wss/server_cert/server.crt")
	if err != nil {
		t.Fatal(err)
	}
	keyPath, err := filepath.Abs("../examples/profiles/wss/server_cert/server.key")
	if err != nil {
		t.Fatal(err)
	}

	server := newServer(
		WSS_SERVER,
		WithLocalAddress("127.0.0.1:0"),
		WithWebsocketServerPath("/ws"),
		WithWebsocketServerCert(certPath),
		WithWebsocketServerPrivateKey(keyPath),
	)
	server.RunEventLoop(func(Session) error { return nil })

	closeServer := func() {
		t.Helper()
		closed := make(chan struct{})
		go func() {
			server.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(2 * time.Second):
			t.Error("WSS server Close did not return")
		}
	}
	defer func() {
		if !server.IsClosed() {
			closeServer()
		}
	}()

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(certPEM) {
		t.Fatal("failed to parse WSS server certificate")
	}
	clientConn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", server.Listener().Addr().String(), &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootCAs,
	})
	if err != nil {
		t.Fatalf("TLS handshake with WSS server failed: %v", err)
	}
	if err := clientConn.Close(); err != nil {
		t.Fatalf("close TLS client connection: %v", err)
	}

	closeServer()
}

func TestWSServeWSRequestClosesSelfConnectConn(t *testing.T) {
	server := newServer(WS_SERVER, WithWebsocketServerPath("/ws"))
	newSessionCalled := false
	handler := newWSHandler(server, func(Session) error {
		newSessionCalled = true
		return errors.New("self-connect request should not create session")
	})

	conn := &selfConnectConn{addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 65000}}
	rw := &hijackResponseWriter{
		header: make(http.Header),
		conn:   conn,
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	handler.serveWSRequest(rw, req)

	if !strings.HasPrefix(conn.writes.String(), "HTTP/1.1 101 Switching Protocols\r\n") {
		t.Fatalf("expected websocket upgrade to succeed, got response %q", conn.writes.String())
	}
	if newSessionCalled {
		t.Fatal("expected self-connect websocket request to be rejected before session creation")
	}
	if !conn.closed {
		t.Fatal("expected self-connect websocket connection to be closed")
	}
}

type hijackResponseWriter struct {
	header http.Header
	conn   *selfConnectConn
	status int
}

func (w *hijackResponseWriter) Header() http.Header {
	return w.header
}

func (w *hijackResponseWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *hijackResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *hijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

type selfConnectConn struct {
	writes bytes.Buffer
	addr   net.Addr
	closed bool
}

func (c *selfConnectConn) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (c *selfConnectConn) Write(p []byte) (int, error) {
	return c.writes.Write(p)
}

func (c *selfConnectConn) Close() error {
	c.closed = true
	return nil
}

func (c *selfConnectConn) LocalAddr() net.Addr {
	return c.addr
}

func (c *selfConnectConn) RemoteAddr() net.Addr {
	return c.addr
}

func (c *selfConnectConn) SetDeadline(time.Time) error {
	return nil
}

func (c *selfConnectConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *selfConnectConn) SetWriteDeadline(time.Time) error {
	return nil
}

// singleAcceptListener hands out one already-built connection, so accept() can
// be driven without a real listener.
type singleAcceptListener struct{ conn net.Conn }

func (l *singleAcceptListener) Accept() (net.Conn, error) { return l.conn, nil }
func (*singleAcceptListener) Close() error                { return nil }
func (*singleAcceptListener) Addr() net.Addr              { return &net.TCPAddr{} }

// Regression test for #123: accept() logged the self-connect and returned
// without closing the connection it had just accepted. The accept loop simply
// continues, so that descriptor leaked for the life of the process.
func TestAcceptClosesSelfConnect(t *testing.T) {
	// one addr for both directions is what makes it a self-connect
	conn := &selfConnectConn{addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 65000}}
	srv := newServer(TCP_SERVER)
	srv.streamListener = &singleAcceptListener{conn: conn}

	ss, err := srv.accept(func(Session) error { return nil })
	if !errors.Is(err, errSelfConnect) {
		t.Fatalf("accept() error = %v, want %v", err, errSelfConnect)
	}
	if ss != nil {
		t.Fatalf("accept() returned session %v for a self-connect", ss)
	}
	if !conn.closed {
		t.Fatal("accept() left the self-connect connection open: the fd is leaked")
	}
}

// Regression test for #125: a udp endpoint's newSession callback returning an
// error used to panic inside the goroutine RunEventLoop spawned, and a panic
// there cannot be recovered by any caller - a transient error in a user callback
// killed the process. It now logs and stops serving, like the tcp accept path.
func TestUDPNewSessionErrorDoesNotPanic(t *testing.T) {
	srv := NewUDPEndPoint(WithLocalAddress("127.0.0.1:0"))
	srv.RunEventLoop(func(Session) error { return errors.New("callback failed") })

	// Close waits for the session goroutine (s.wg), so reaching the end of this
	// test means it did not panic. Before the fix it panicked here and took the
	// whole test binary down.
	srv.Close()

	if !srv.IsClosed() {
		t.Fatal("udp endpoint does not report itself closed")
	}
}

// TestServerSSLRequiresTLSConfigBuilder mirrors the client-side check added for
// #125: a server built with sslEnabled and no tlsConfigBuilder reached a nil
// interface method call inside listenTCP, so a configuration mistake surfaced as
// a segfault raised from the event loop instead of as a message naming the
// missing option.
func TestServerSSLRequiresTLSConfigBuilder(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("NewTCPServer(sslEnabled, no tlsConfigBuilder) did not panic")
		}
		if !strings.Contains(fmt.Sprint(r), "tlsConfigBuilder") {
			t.Fatalf("panic value %v does not name the missing option", r)
		}
	}()

	NewTCPServer(
		WithLocalAddress("127.0.0.1:1"),
		WithServerSslEnabled(true),
	)
}

// TestServerSSLFlagIsOnlyRequiredWhereItIsRead pins the scope of the check
// above: sslEnabled reaches a listener only through listenTCP, which serves tcp,
// ws and wss; the udp endpoint never reads the field, so it must not be refused.
// A ws server does read it, so that combination is still refused, and the message
// has to point at the option that is actually missing.
func TestServerSSLFlagIsOnlyRequiredWhereItIsRead(t *testing.T) {
	NewUDPEndPoint(WithLocalAddress("127.0.0.1:1"), WithServerSslEnabled(true))

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("NewWSServer(sslEnabled, no tlsConfigBuilder) did not panic")
		}
		if !strings.Contains(fmt.Sprint(r), "WithServerTlsConfigBuilder") {
			t.Fatalf("panic value %v does not name the missing option", r)
		}
	}()

	NewWSServer(WithLocalAddress("127.0.0.1:1"), WithWebsocketServerPath("/ws"), WithServerSslEnabled(true))
}

// callbackProbeReadWriter and callbackProbeListener are the minimum a session
// needs to run: session.run() refuses to start without a package handler and an
// event listener.
type callbackProbeReadWriter struct{}

func (callbackProbeReadWriter) Read(Session, []byte) (any, int, error) { return nil, 0, nil }
func (callbackProbeReadWriter) Write(Session, any) ([]byte, error)     { return []byte{}, nil }

type callbackProbeListener struct{ panicOnOpen bool }

func (l callbackProbeListener) OnOpen(Session) error {
	if l.panicOnOpen {
		panic("OnOpen blew up")
	}
	return nil
}
func (callbackProbeListener) OnClose(Session)        {}
func (callbackProbeListener) OnError(Session, error) {}
func (callbackProbeListener) OnCron(Session)         {}
func (callbackProbeListener) OnMessage(Session, any) {}

// TestUserCallbackPanicsAreContained: both user callbacks run on a goroutine this
// library started, where nothing recovers a panic, so before the fix either one
// took the whole process down - while the same callback on a ws server only
// dropped its own connection. Each panic now closes the connection it belongs to
// and the accept loop keeps serving.
func TestUserCallbackPanicsAreContained(t *testing.T) {
	srv := newServer(TCP_SERVER, WithLocalAddress("127.0.0.1:0"))
	calls := 0
	srv.RunEventLoop(func(ss Session) error {
		// the accept loop calls this serially, so no synchronisation is needed
		calls++
		switch calls {
		case 1:
			panic("newSession callback blew up")
		case 2:
			ss.SetPkgHandler(callbackProbeReadWriter{})
			ss.SetEventListener(callbackProbeListener{panicOnOpen: true})
		default:
			ss.SetPkgHandler(callbackProbeReadWriter{})
			ss.SetEventListener(callbackProbeListener{})
		}

		return nil
	})
	defer srv.Close()
	addr := srv.Listener().Addr().String()

	conn1, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer func() { _ = conn1.Close() }()
	assertPeerClosed(t, conn1, "a panic in NewSessionCallback")

	conn2, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial %s after the callback panic: %v", addr, err)
	}
	defer func() { _ = conn2.Close() }()
	assertPeerClosed(t, conn2, "a panic in OnOpen")

	// the accept loop survived both panics, so this connection is served and
	// stays open
	conn3, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial %s after both callback panics: %v", addr, err)
	}
	defer func() { _ = conn3.Close() }()
	_ = conn3.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err = conn3.Read(make([]byte, 1)); err == nil || !isTimeout(err) {
		t.Fatalf("the third connection was not served: the accept loop stopped (read error %v)", err)
	}
}

// assertPeerClosed fails when the peer kept the connection open, which is what a
// read that times out means.
func assertPeerClosed(t *testing.T, conn net.Conn, who string) {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatalf("connection read a byte after %s", who)
	} else if isTimeout(err) {
		t.Fatalf("the connection is still open after %s", who)
	}
}

func isTimeout(err error) bool {
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout()
	}

	return false
}

// persistentAcceptError mimics a lasting accept failure that is not a timeout,
// such as EMFILE/ENFILE when the process runs out of file descriptors.
type persistentAcceptError struct{}

func (persistentAcceptError) Error() string   { return "accept: too many open files" }
func (persistentAcceptError) Timeout() bool   { return false }
func (persistentAcceptError) Temporary() bool { return true }

// errorAcceptListener is a net.Listener whose Accept always fails with a
// non-timeout error, and which counts its calls.
type errorAcceptListener struct {
	calls atomic.Int32
}

func (l *errorAcceptListener) Accept() (net.Conn, error) {
	l.calls.Add(1)
	return nil, persistentAcceptError{}
}

func (l *errorAcceptListener) Close() error { return nil }

func (l *errorAcceptListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4zero, Port: 0}
}

// TestTCPAcceptBacksOffOnPersistentError is the regression test for #130
// defect 1. Accept errors other than a timeout used to take the
// log-and-continue branch, which left delay at 0, so Accept was re-issued at
// full speed - spinning the CPU and flooding the log exactly while the process
// was out of resources. Because gxtime.After's timer wheel is coarse, the test
// counts how many Accept calls a fixed window allows instead of measuring one
// interval: a loop that backs off can only issue a handful of calls in 300ms
// (the first delay is 5ms and doubles from there), while a spinning one re-calls
// Accept as fast as the CPU and the logger allow.
func TestTCPAcceptBacksOffOnPersistentError(t *testing.T) {
	srv := newServer(TCP_SERVER, WithLocalAddress("127.0.0.1:0"))
	listener := &errorAcceptListener{}
	srv.streamListener = listener
	srv.runTCPEventLoop(func(Session) error { return nil })
	defer srv.Close()

	time.Sleep(300 * time.Millisecond)
	calls := int(listener.calls.Load())
	srv.Close()

	assert.GreaterOrEqual(t, calls, 2, "the accept loop stopped retrying")
	assert.LessOrEqual(t, calls, 50,
		"the accept loop made %d Accept calls in 300ms: a persistent non-timeout error is not backed off", calls)
}

// TestWebsocketServerRequiresPath: an empty path reaches ServeMux.HandleFunc(""),
// which panics - and that registration happens in the goroutine RunEventLoop
// starts, so the process died with "http: invalid pattern" and the caller never
// learned which option was missing.
func TestWebsocketServerRequiresPath(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("newServer(WS_SERVER) without WithWebsocketServerPath did not panic")
		}
		if !strings.Contains(fmt.Sprint(r), "WithWebsocketServerPath") {
			t.Fatalf("panic value %v does not name the missing option", r)
		}
	}()

	newServer(WS_SERVER, WithLocalAddress("127.0.0.1:1"))
}

// TestEventLoopRegistrationIsAtomicWithClose pins the ordering RunEventLoop and
// Close() rely on. RunEventLoop publishes the listener first and used to add to
// s.wg only afterwards, so a Close() landing in between closed done, returned
// from wg.Wait() while the counter was still zero, and had the event loop
// registered behind it - an accept loop outliving the close, and a WaitGroup
// addition racing a Wait. Registration now tests done and increments the counter
// under the lock stop() also takes: a loop registered before Close() is waited
// for, and a closed server refuses to register one.
func TestEventLoopRegistrationIsAtomicWithClose(t *testing.T) {
	srv := newServer(TCP_SERVER)

	if !srv.startEventLoop() {
		t.Fatal("startEventLoop refused to register an event loop on an open server")
	}

	closed := make(chan struct{})
	go func() {
		srv.Close()
		close(closed)
	}()

	select {
	case <-closed:
		t.Fatal("Close() returned while a registered event loop was still running")
	case <-time.After(300 * time.Millisecond):
	}

	srv.wg.Done()

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close() did not return after the registered event loop finished")
	}

	if srv.startEventLoop() {
		t.Fatal("startEventLoop registered an event loop on a closed server")
	}
}
