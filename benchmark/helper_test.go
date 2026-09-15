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

package benchmark

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

import (
	getty "github.com/AlexStocks/getty/transport"
	gettylog "github.com/AlexStocks/getty/util"
)

const (
	// maxPacketLen mirrors getty's unexported fragment size: WriteBytes above
	// it switches from a single read-locked send to the write-locked
	// fragmenting loop, so benchmarks cover both sides of that boundary.
	maxPacketLen = 16 * 1024
	// wsPath is the websocket endpoint both sides of the ws echo agree on.
	wsPath = "/bench-echo"
	// replyBuffer bounds the outstanding echo replies a client can have; keep
	// every in-flight value well below it so the reply listener never blocks
	// the session read loop.
	replyBuffer = 4096
	// dialTimeout bounds how long a benchmark waits for a client pool.
	dialTimeout = 10 * time.Second
	// replyTimeout turns "the peer never echoed" into a failure instead of a
	// hang.
	replyTimeout = 30 * time.Second
	// udpReplyTimeout bounds one datagram round trip. udp may lose a datagram,
	// so a udp benchmark retries instead of waiting out replyTimeout.
	udpReplyTimeout = time.Second
	// udpWarmupAttempts bounds the pre-measurement round trip.
	udpWarmupAttempts = 5
	// udpMaxRetries bounds the resends for one lost reply inside the loop.
	udpMaxRetries = 3
	// maxMsgLen must exceed every echoed payload: session.handleTCPPackage and
	// handleWSPackage drop any pkg larger than maxMsgLen, whose default is 4KB,
	// so a 16KB echo would silently never come back.
	maxMsgLen = 2 << 20
)

// sizes spans both sides of maxPacketLen.
var sizes = []int{64, 1 << 10, maxPacketLen, maxPacketLen + 1, 64 << 10, 1 << 20}

// Echo payload sizes per transport. ws stays under the 4KB default maxMsgLen: a
// ws *client* has gorilla's read limit fixed from the default maxMsgLen while
// the session is built, before NewSessionCallback can raise it, so a larger
// echo is rejected with "read limit exceeded" no matter what the callback does.
var (
	tcpEchoSizes = []int{64, 1 << 10, maxPacketLen, 64 << 10}
	wsEchoSizes  = []int{64, 1 << 10, 3 << 10}
)

func echoSizes(transport string) []int {
	if transport == "ws" {
		return wsEchoSizes
	}
	return tcpEchoSizes
}

// silenceLogs keeps getty's logging out of the numbers. gettyTCPConn.Send logs
// on every call, and the libraries also log benign teardown events (a closed
// http.Server), which would otherwise dominate the measurement and garble the
// output. Failures surface through b.Fatal and the dial/reply timeouts instead.
var silenceLogsOnce sync.Once

func silenceLogs() {
	silenceLogsOnce.Do(func() {
		if err := gettylog.SetLoggerLevel(gettylog.LoggerLevelFatal); err != nil {
			panic(err)
		}
		_ = gettylog.SetLoggerCallerDisable()
	})
}

func benchPayload(n int) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(i)
	}
	return p
}

// sizeName keeps the maxPacketLen boundary distinguishable from maxPacketLen+1.
func sizeName(n int) string {
	switch {
	case n == maxPacketLen+1:
		return "16K+1"
	case n >= 1<<20:
		return fmt.Sprintf("%dM", n>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%dK", n>>10)
	default:
		return strconv.Itoa(n)
	}
}

// nopCodec is the cheapest codec there is: Read never completes a pkg, so a
// session using it only drains its peer, and Write passes a []byte straight
// through. The write-path benchmarks use it so the codec is not part of what
// they measure.
type nopCodec struct{}

func (nopCodec) Read(getty.Session, []byte) (any, int, error) { return nil, 0, nil }

func (nopCodec) Write(_ getty.Session, pkg any) ([]byte, error) {
	body, ok := pkg.([]byte)
	if !ok {
		return nil, fmt.Errorf("nopCodec: unexpected pkg type %T", pkg)
	}
	return body, nil
}

// udpCodec unwraps the UDPContext a udp session must be written with.
type udpCodec struct{}

func (udpCodec) Read(getty.Session, []byte) (any, int, error) { return nil, 0, nil }

func (udpCodec) Write(_ getty.Session, pkg any) ([]byte, error) {
	ctx, ok := pkg.(getty.UDPContext)
	if !ok {
		return nil, fmt.Errorf("udpCodec: unexpected pkg type %T", pkg)
	}
	body, ok := ctx.Pkg.([]byte)
	if !ok {
		return nil, fmt.Errorf("udpCodec: unexpected UDPContext.Pkg type %T", ctx.Pkg)
	}
	return body, nil
}

// udpEchoCodec turns each datagram into one pkg, and unwraps the UDPContext a
// udp session must be written with. handleUDPPackage hands OnMessage a
// UDPContext{Pkg: <what Read returned>, PeerAddr: <sender>}, so echoing is just
// writing that same context back.
type udpEchoCodec struct{}

func (udpEchoCodec) Read(_ getty.Session, data []byte) (any, int, error) {
	body := make([]byte, len(data))
	copy(body, data)
	return body, len(data), nil
}

func (udpEchoCodec) Write(_ getty.Session, pkg any) ([]byte, error) {
	ctx, ok := pkg.(getty.UDPContext)
	if !ok {
		return nil, fmt.Errorf("udpEchoCodec: unexpected pkg type %T", pkg)
	}
	body, ok := ctx.Pkg.([]byte)
	if !ok {
		return nil, fmt.Errorf("udpEchoCodec: unexpected UDPContext.Pkg type %T", ctx.Pkg)
	}
	return body, nil
}

// udpEchoListener writes each datagram back to whoever sent it, and counts what
// it saw.
type udpEchoListener struct{ recv *atomic.Int64 }

func (udpEchoListener) OnOpen(getty.Session) error   { return nil }
func (udpEchoListener) OnClose(getty.Session)        {}
func (udpEchoListener) OnError(getty.Session, error) {}
func (udpEchoListener) OnCron(getty.Session)         {}
func (l udpEchoListener) OnMessage(ss getty.Session, pkg any) {
	ctx, ok := pkg.(getty.UDPContext)
	if !ok {
		return
	}
	if l.recv != nil {
		l.recv.Add(1)
	}
	_, _, _ = ss.WritePkg(ctx, 0)
}

// lengthPrefixedCodec frames each pkg as a 4-byte big-endian length plus body.
// It is the smallest codec a getty user would write, so the echo benchmarks
// measure getty rather than a serialization library.
type lengthPrefixedCodec struct{}

func (lengthPrefixedCodec) Read(_ getty.Session, data []byte) (any, int, error) {
	if len(data) < 4 {
		return nil, 0, nil
	}
	n := int(binary.BigEndian.Uint32(data[:4]))
	if n < 0 || len(data) < 4+n {
		return nil, 0, nil
	}
	body := make([]byte, n)
	copy(body, data[4:4+n])
	return body, 4 + n, nil
}

func (lengthPrefixedCodec) Write(_ getty.Session, pkg any) ([]byte, error) {
	body, ok := pkg.([]byte)
	if !ok {
		return nil, fmt.Errorf("lengthPrefixedCodec: unexpected pkg type %T", pkg)
	}
	frame := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(body)))
	copy(frame[4:], body)
	return frame, nil
}

// jsonPkg is what the JSON codec benchmark round-trips.
type jsonPkg struct {
	Body []byte `json:"body"`
}

// jsonCodec keeps the same length framing and adds a JSON encode/decode inside
// it.
type jsonCodec struct{}

func (jsonCodec) Read(ss getty.Session, data []byte) (any, int, error) {
	body, n, err := lengthPrefixedCodec{}.Read(ss, data)
	if body == nil || err != nil {
		return nil, n, err
	}
	var pkg jsonPkg
	if err := json.Unmarshal(body.([]byte), &pkg); err != nil {
		return nil, 0, err
	}
	return &pkg, n, nil
}

func (jsonCodec) Write(_ getty.Session, pkg any) ([]byte, error) {
	body, err := json.Marshal(pkg)
	if err != nil {
		return nil, err
	}
	return lengthPrefixedCodec{}.Write(nil, body)
}

// discardListener does nothing: the sessions that use it only send.
type discardListener struct{}

func (discardListener) OnOpen(getty.Session) error   { return nil }
func (discardListener) OnClose(getty.Session)        {}
func (discardListener) OnError(getty.Session, error) {}
func (discardListener) OnCron(getty.Session)         {}
func (discardListener) OnMessage(getty.Session, any) {}

// echoListener writes every decoded pkg straight back, so a benchmark measures
// the full write-codec-conn-read-codec round trip on both sides.
type echoListener struct{}

func (echoListener) OnOpen(getty.Session) error   { return nil }
func (echoListener) OnClose(getty.Session)        {}
func (echoListener) OnError(getty.Session, error) {}
func (echoListener) OnCron(getty.Session)         {}
func (echoListener) OnMessage(ss getty.Session, pkg any) {
	_, _, _ = ss.WritePkg(pkg, 0)
}

// replyListener signals the benchmark goroutine once per echoed pkg.
type replyListener struct {
	replies chan<- struct{}
	recv    *atomic.Int64
}

func (replyListener) OnOpen(getty.Session) error   { return nil }
func (replyListener) OnClose(getty.Session)        {}
func (replyListener) OnError(getty.Session, error) {}
func (replyListener) OnCron(getty.Session)         {}
func (l replyListener) OnMessage(getty.Session, any) {
	if l.recv != nil {
		l.recv.Add(1)
	}
	l.replies <- struct{}{}
}

// drainListener is a raw TCP listener that throws away everything it receives.
// It stands in for the peer in the write-path benchmarks: they measure a getty
// client session writing to a socket, and the peer must neither apply
// backpressure of its own nor add getty work to the measurement.
func drainListener(tb testing.TB) (string, func()) {
	tb.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("net.Listen: %v", err)
	}

	var (
		conns    sync.Map
		acceptWG sync.WaitGroup
	)
	acceptWG.Add(1)
	go func() {
		defer acceptWG.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Store(conn, struct{}{})
			go func(c net.Conn) {
				_, _ = io.Copy(io.Discard, c)
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		_ = ln.Close()
		// The accept goroutine must be gone before the range below, otherwise it
		// can store a connection that nothing will ever close.
		acceptWG.Wait()
		conns.Range(func(k, _ any) bool {
			_ = k.(net.Conn).Close()
			return true
		})
	}
}

// newWriteSession returns a getty TCP client session whose peer discards every
// byte, so WriteBytes/WritePkg/Send can be measured against a real socket.
// compress == getty.CompressNone leaves the connection raw: getty only installs
// its flate/snappy codec when SetCompressType is called, and passing
// CompressNone there would install flate at NoCompression level instead.
func newWriteSession(b *testing.B, codec getty.ReadWriter, compress getty.CompressType) getty.Session {
	b.Helper()
	silenceLogs()

	addr, closeDrain := drainListener(b)
	ready := make(chan getty.Session, 1)
	cli := getty.NewTCPClient(getty.WithServerAddress(addr), getty.WithConnectionNumber(1))
	go cli.RunEventLoop(func(ss getty.Session) error {
		ss.SetPkgHandler(codec)
		ss.SetEventListener(discardListener{})
		ss.SetReadTimeout(time.Minute)
		ss.SetWriteTimeout(time.Minute)
		if compress != getty.CompressNone {
			// Must happen before any IO on the connection.
			ss.SetCompressType(compress)
		}
		ready <- ss
		return nil
	})
	b.Cleanup(func() {
		cli.Close()
		closeDrain()
	})

	select {
	case ss := <-ready:
		return ss
	case <-time.After(dialTimeout):
		b.Fatal("client session did not come up")
		return nil
	}
}

// latencyRecorder keeps one sample per round trip so a benchmark can report
// tail percentiles. A mean hides the p99 that RPC users actually feel, and
// ns/op alone cannot show it. Samples are written into a slice preallocated to
// b.N, so recording costs an append into existing capacity, not an allocation.
type latencyRecorder struct {
	samples []time.Duration
}

func newLatencyRecorder(n int) *latencyRecorder {
	return &latencyRecorder{samples: make([]time.Duration, 0, n)}
}

func (l *latencyRecorder) record(d time.Duration) {
	l.samples = append(l.samples, d)
}

// report publishes p50/p99/p999 as user metrics. It stops the benchmark timer
// first: the copy and the O(n log n) sort below would otherwise be charged to
// ns/op, which would make two runs with different b.N incomparable. It is a
// no-op when nothing was recorded, so a benchmark that fails early does not
// report zeros.
func (l *latencyRecorder) report(b *testing.B) {
	if len(l.samples) == 0 {
		return
	}
	b.StopTimer()
	sorted := append([]time.Duration(nil), l.samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if sorted[len(sorted)-1] == 0 {
		// Some platforms report a zero interval for a fast loop (coarse timer
		// inside a VM, for instance). Publishing p50=0 would be worse than
		// publishing nothing, so leave the percentiles out and say so.
		b.ReportMetric(0, "p50-ns(clock-too-coarse)")
		return
	}
	percentile := func(p float64) float64 {
		idx := int(float64(len(sorted)-1) * p)
		return float64(sorted[idx].Nanoseconds())
	}
	b.ReportMetric(percentile(0.5), "p50-ns")
	b.ReportMetric(percentile(0.99), "p99-ns")
	b.ReportMetric(percentile(0.999), "p999-ns")
}

// benchEcho is a live getty client/server echo pair over loopback.
type benchEcho struct {
	srv        getty.Server
	cli        getty.Client
	sessions   []getty.Session
	replies    chan struct{}
	replyTimer *time.Timer
	// serverRecv and clientRecv exist so that a udp round trip that never
	// completes can say *where* it broke instead of just timing out.
	serverRecv *atomic.Int64
	clientRecv *atomic.Int64
}

// newBenchEcho starts a server and a client pool of conns sessions, all sharing
// codec.
func newBenchEcho(b *testing.B, transport string, codec getty.ReadWriter, compress getty.CompressType, conns int) *benchEcho {
	b.Helper()
	silenceLogs()

	e := &benchEcho{
		replies:    make(chan struct{}, replyBuffer),
		replyTimer: time.NewTimer(replyTimeout),
		serverRecv: &atomic.Int64{},
		clientRecv: &atomic.Int64{},
	}
	// Registered before anything can fail, so a dial timeout or an unsupported
	// transport still tears down whatever was already started instead of leaving
	// a server and its event loop behind for the rest of the run.
	b.Cleanup(e.close)

	// udp is one shared server session over a packet conn, so it has no pool and
	// no per-connection framing: the datagram is the message.
	if transport == "udp" {
		return newBenchUDPEcho(b, e, codec)
	}

	switch transport {
	case "tcp":
		e.srv = getty.NewTCPServer(getty.WithLocalAddress("127.0.0.1:0"))
	case "ws":
		e.srv = getty.NewWSServer(
			getty.WithLocalAddress("127.0.0.1:0"),
			getty.WithWebsocketServerPath(wsPath),
		)
	default:
		b.Fatalf("unknown transport %q", transport)
	}
	e.srv.RunEventLoop(func(ss getty.Session) error {
		setHandler(ss, codec, echoListener{}, compress)
		return nil
	})

	addr := e.srv.(getty.StreamServer).Listener().Addr().String()
	switch transport {
	case "tcp":
		e.cli = getty.NewTCPClient(getty.WithServerAddress(addr), getty.WithConnectionNumber(conns))
	case "ws":
		e.cli = getty.NewWSClient(
			getty.WithServerAddress("ws://"+addr+wsPath),
			getty.WithConnectionNumber(conns),
		)
	}
	ready := make(chan getty.Session, conns)
	// RunEventLoop blocks until the pool is dialled; run it in the background
	// so a server that never accepts cannot pin the benchmark setup forever.
	go e.cli.RunEventLoop(func(ss getty.Session) error {
		setHandler(ss, codec, replyListener{replies: e.replies, recv: e.clientRecv}, compress)
		ready <- ss
		return nil
	})

	timeout := time.After(dialTimeout)
	for i := 0; i < conns; i++ {
		select {
		case ss := <-ready:
			e.sessions = append(e.sessions, ss)
		case <-timeout:
			b.Fatalf("%s: only %d of %d client sessions came up", transport, i, conns)
		}
	}

	// Drain anything sent during the handshake so the first measured iteration
	// starts from an empty reply queue.
	for drained := false; !drained; {
		select {
		case <-e.replies:
		default:
			drained = true
		}
	}

	return e
}

// newBenchUDPEcho builds the udp variant: a packet endpoint serving one shared
// session, and one connected udp client. Compression is not applied - getty does
// not support it on udp.
func newBenchUDPEcho(b *testing.B, e *benchEcho, codec getty.ReadWriter) *benchEcho {
	b.Helper()

	e.srv = getty.NewUDPEndPoint(getty.WithLocalAddress("127.0.0.1:0"))
	e.srv.RunEventLoop(func(ss getty.Session) error {
		setHandler(ss, codec, udpEchoListener{recv: e.serverRecv}, getty.CompressNone)
		return nil
	})
	addr := e.srv.(getty.PacketServer).PacketConn().LocalAddr().String()

	ready := make(chan getty.Session, 1)
	e.cli = getty.NewUDPClient(getty.WithServerAddress(addr), getty.WithConnectionNumber(1))
	go e.cli.RunEventLoop(func(ss getty.Session) error {
		setHandler(ss, codec, replyListener{replies: e.replies, recv: e.clientRecv}, getty.CompressNone)
		ready <- ss
		return nil
	})

	select {
	case ss := <-ready:
		e.sessions = append(e.sessions, ss)
	case <-time.After(dialTimeout):
		b.Fatal("udp client session did not come up")
	}

	e.warmUpUDP(b)
	return e
}

// A udp round trip is not guaranteed, and the client's own dial does a
// write-then-read handshake with a one second deadline before the session even
// exists (transport/client.go dialUDP), so the first datagrams of a benchmark
// are the most likely to be dropped or eaten. Establishing one round trip
// before measuring keeps the measured loop from timing out on a cold start, and
// the counters below say exactly where it broke when it cannot.
func (e *benchEcho) warmUpUDP(b *testing.B) {
	b.Helper()
	payload := benchPayload(16)
	for attempt := 1; attempt <= udpWarmupAttempts; attempt++ {
		if _, _, err := e.sessions[0].WritePkg(getty.UDPContext{Pkg: payload}, 0); err != nil {
			b.Fatalf("udp warmup write: %v", err)
		}
		if e.waitReplyWithin(udpReplyTimeout) {
			return
		}
	}
	reason := fmt.Sprintf("udp echo round trip could not be established in %d attempts: the server received %d datagrams, the client received %d. "+
		"The udp endpoint round trip needs a look before this case can be measured; skipping it rather than failing every other case. "+
		"Environment: %s/%s go%s",
		udpWarmupAttempts, e.serverRecv.Load(), e.clientRecv.Load(),
		runtime.GOOS, runtime.GOARCH, strings.TrimPrefix(runtime.Version(), "go"))
	// testing buffers a skip's message and its "--- SKIP:" line and only prints
	// them with -v, and none of the make targets pass -v - so a case that did
	// not run would vanish from `make bench` output entirely. Write the reason
	// to stderr as well, where it is always visible.
	fmt.Fprintln(os.Stderr, "benchmark: skipping", b.Name()+":", reason)
	b.Skip(reason)
}

func setHandler(ss getty.Session, codec getty.ReadWriter, listener getty.EventListener, compress getty.CompressType) {
	ss.SetPkgHandler(codec)
	ss.SetEventListener(listener)
	ss.SetMaxMsgLen(maxMsgLen)
	ss.SetReadTimeout(time.Minute)
	ss.SetWriteTimeout(time.Minute)
	if compress != getty.CompressNone {
		ss.SetCompressType(compress)
	}
}

// waitReply blocks until the peer echoed one pkg, and fails the benchmark
// instead of hanging forever if it never does.
//
// It reuses one timer rather than calling time.After: waitReply runs inside the
// measured loop, and a fresh timer per echoed pkg would put its allocation and
// its runtime timer churn into ns/op and allocs/op.
func (e *benchEcho) waitReply(b *testing.B) {
	b.Helper()
	if !e.waitReplyWithin(replyTimeout) {
		b.Fatalf("no echo came back within %s: the session dropped the pkg (maxMsgLen) or the peer died", replyTimeout)
	}
}

// waitReplyWithin is waitReply with a caller-chosen bound, so a udp benchmark can
// treat a missing datagram as a loss instead of a 30 second wall.
func (e *benchEcho) waitReplyWithin(d time.Duration) bool {
	if !e.replyTimer.Stop() {
		select {
		case <-e.replyTimer.C:
		default:
		}
	}
	e.replyTimer.Reset(d)

	select {
	case <-e.replies:
		return true
	case <-e.replyTimer.C:
		return false
	}
}

// close is idempotent and tolerates a partially built pair, because it can run
// after any early failure in newBenchEcho.
func (e *benchEcho) close() {
	if e.cli != nil {
		e.cli.Close()
	}
	if e.srv != nil {
		e.srv.Close()
	}
	// Give the session goroutines a chance to leave the timer wheel before the
	// next benchmark builds its own pair.
	for _, ss := range e.sessions {
		if ss != nil {
			ss.Close()
		}
	}
}
