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
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

import (
	"github.com/golang/snappy"

	perrors "github.com/pkg/errors"
)

type blockingSnappyWriter struct {
	entered chan struct{}
	release chan struct{}
}

var errFlushWriter = errors.New("flush writer failure")

type flushErrorWriter struct{}

func (flushErrorWriter) Write([]byte) (int, error) {
	return 0, errFlushWriter
}

func (w *blockingSnappyWriter) Write(p []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	return len(p), nil
}

type timeoutAccessorNetConn struct{}

func (*timeoutAccessorNetConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (*timeoutAccessorNetConn) Write(p []byte) (int, error)      { return len(p), nil }
func (*timeoutAccessorNetConn) Close() error                     { return nil }
func (*timeoutAccessorNetConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*timeoutAccessorNetConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*timeoutAccessorNetConn) SetDeadline(time.Time) error      { return nil }
func (*timeoutAccessorNetConn) SetReadDeadline(time.Time) error  { return nil }
func (*timeoutAccessorNetConn) SetWriteDeadline(time.Time) error { return nil }

func TestConnectionTimeoutAccessorsDoNotCopyAtomicState(t *testing.T) {
	conn := newGettyTCPConn(&timeoutAccessorNetConn{})
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 10000; i++ {
			conn.rLastDeadline.Store(time.Unix(0, int64(i)))
			conn.wLastDeadline.Store(time.Unix(0, int64(i)))
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 10000; i++ {
			_ = conn.ReadTimeout()
			_ = conn.WriteTimeout()
		}
	}()

	close(start)
	wg.Wait()
}

func TestWriteFlushersReturnConsumedBytesOnFlushError(t *testing.T) {
	payload := []byte("payload")
	tests := []struct {
		name   string
		writer io.Writer
	}{
		{
			name: "flate",
			writer: func() io.Writer {
				writer, err := flate.NewWriter(flushErrorWriter{}, flate.DefaultCompression)
				if err != nil {
					t.Fatal(err)
				}
				return &writeFlusher{flusher: writer}
			}(),
		},
		{
			name:   "snappy",
			writer: newSnappyWriteFlusher(snappy.NewBufferedWriter(flushErrorWriter{})),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			n, err := test.writer.Write(payload)
			if !errors.Is(err, errFlushWriter) {
				t.Fatalf("Write error = %v, want %v", err, errFlushWriter)
			}
			if n != len(payload) {
				t.Fatalf("Write returned %d bytes after consuming %d", n, len(payload))
			}
		})
	}
}

func TestSnappyWriteFlusherCloseWaitsForWrite(t *testing.T) {
	underlying := &blockingSnappyWriter{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(underlying.release) }) }
	defer release()

	writer := newSnappyWriteFlusher(snappy.NewBufferedWriter(underlying))
	writeDone := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("payload"))
		writeDone <- err
	}()

	select {
	case <-underlying.entered:
	case <-time.After(time.Second):
		t.Fatal("snappy write did not reach the underlying writer")
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- writer.Close()
	}()

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before the active Write completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	release()
	if err := <-writeDone; err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func newTCPConnPair(t *testing.T) (*gettyTCPConn, *gettyTCPConn) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	accepted := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept()
		accepted <- acceptResult{conn: conn, err: err}
	}()

	clientRaw, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	result := <-accepted
	if result.err != nil {
		_ = clientRaw.Close()
		t.Fatal(result.err)
	}

	client := newGettyTCPConn(clientRaw)
	server := newGettyTCPConn(result.conn)
	t.Cleanup(func() {
		client.CloseConn(0)
		server.CloseConn(0)
	})
	return client, server
}

// startReceiver continuously decodes from conn until wantLen bytes have been
// read. A tiny buffer forces reads to cross codec block boundaries.
func startReceiver(conn *gettyTCPConn, wantLen int, bufSize int) (<-chan []byte, <-chan error) {
	gotCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		var got []byte
		buf := make([]byte, bufSize)
		for len(got) < wantLen {
			n, err := conn.recv(buf)
			if err != nil {
				errCh <- err
				return
			}
			if n == 0 {
				continue
			}
			got = append(got, buf[:n]...)
		}
		gotCh <- got
	}()
	return gotCh, errCh
}

// TestSendMixedSingleAndBatchOverCompressNoneCodec pins the #102/#107 wire
// format: SetCompressType(CompressNone) installs a real flate codec, so every
// Send path, including [][]byte, must go through the codec writer. Otherwise
// one connection carries a corrupt mix of coded []byte sends and raw
// [][]byte sends and the peer's decoder fails.
func TestSendMixedSingleAndBatchOverCompressNoneCodec(t *testing.T) {
	client, server := newTCPConnPair(t)
	client.SetCompressType(CompressNone)
	server.SetCompressType(CompressNone)

	single := []byte("single-packet-")
	batch := [][]byte{
		[]byte("batch-part-0-"),
		[]byte("batch-part-1-"),
		[]byte("batch-part-2"),
	}
	tail := []byte("-tail")
	expected := bytes.Join([][]byte{
		single,
		batch[0],
		batch[1],
		batch[2],
		tail,
	}, nil)

	gotCh, errCh := startReceiver(server, len(expected), 3)
	for _, pkg := range []any{single, batch, tail} {
		if _, err := client.Send(pkg); err != nil {
			t.Fatalf("Send(%T) failed: %v", pkg, err)
		}
	}

	select {
	case got := <-gotCh:
		if !bytes.Equal(got, expected) {
			t.Fatalf("decoded stream mismatch:\n got: %q\nwant: %q", got, expected)
		}
	case err := <-errCh:
		t.Fatalf("peer failed to decode codec stream: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the decoded stream")
	}
}

// TestSendBatchRawWithoutCompressType keeps the raw writev path covered: when
// SetCompressType was never called, a [][]byte send must reach the peer
// untouched (net.Buffers.WriteTo(t.conn), a single writev on *net.TCPConn).
func TestSendBatchRawWithoutCompressType(t *testing.T) {
	client, server := newTCPConnPair(t)
	if client.codecEnabled {
		t.Fatal("new connection unexpectedly has a codec installed")
	}

	batch := [][]byte{
		[]byte("raw-part-0-"),
		[]byte("raw-part-1-"),
		[]byte("raw-part-2"),
	}
	expected := bytes.Join(batch, nil)

	gotCh, errCh := startReceiver(server, len(expected), 3)
	if _, err := client.Send(batch); err != nil {
		t.Fatalf("Send([][]byte) failed: %v", err)
	}

	select {
	case got := <-gotCh:
		if !bytes.Equal(got, expected) {
			t.Fatalf("raw stream mismatch:\n got: %q\nwant: %q", got, expected)
		}
	case err := <-errCh:
		t.Fatalf("peer failed to read raw stream: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the raw stream")
	}
}

// codecStallCases covers both codec families: they take different branches in
// SetCompressType and both latch IO errors, so the stall handling has to hold
// for each.
var codecStallCases = []struct {
	name     string
	compress CompressType
}{
	{name: "flate", compress: CompressZip},
	{name: "snappy", compress: CompressSnappy},
}

// halfCodecBlock returns the first half of a valid, flushed codec block: a prefix
// the decoder cannot finish decoding, i.e. exactly what a peer that dies
// mid-write leaves behind.
func halfCodecBlock(t *testing.T, c CompressType, payload string) []byte {
	t.Helper()

	var (
		buf    bytes.Buffer
		writer interface {
			io.Writer
			Flush() error
		}
	)
	if c == CompressSnappy {
		writer = snappy.NewBufferedWriter(&buf)
	} else {
		flateWriter, err := flate.NewWriter(&buf, int(c))
		if err != nil {
			t.Fatal(err)
		}
		writer = flateWriter
	}
	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	block := buf.Bytes()
	if len(block) < 4 {
		t.Fatalf("codec block too small to truncate: %d bytes", len(block))
	}
	return block[:len(block)/2]
}

// assertCodecStreamBroken checks the error a stalled codec stream must produce.
// The last two assertions are the actual encoding of the fix: session.handleTCPPackage
// classifies read errors by perrors.Cause(), retrying net.Error timeouts and
// treating io.EOF as a clean peer shutdown. A stalled codec stream must fall
// into neither bucket, otherwise the session keeps reading from a decoder that
// has already latched the error.
func assertCodecStreamBroken(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("stalled codec stream returned no error")
	}
	if !errors.Is(err, ErrCodecStreamBroken) {
		t.Fatalf("error = %v, want ErrCodecStreamBroken", err)
	}
	cause := perrors.Cause(err)
	if netErr, ok := cause.(net.Error); ok && netErr.Timeout() {
		t.Fatalf("cause %v is a retryable net.Error timeout; the session would keep using the dead codec", cause)
	}
	if cause == io.EOF {
		t.Fatalf("cause %v would be treated as a clean peer shutdown", cause)
	}
}

// TestCodecRecvStalledPeerBreaksStream covers the P1: a peer that sends half a
// codec block and then goes silent must not block the reader forever. The read
// deadline has to reach the socket even though a codec is installed, and the
// resulting timeout must terminate the connection instead of being retried on a
// decoder that has already latched the error.
func TestCodecRecvStalledPeerBreaksStream(t *testing.T) {
	for _, test := range codecStallCases {
		t.Run(test.name, func(t *testing.T) {
			client, server := newTCPConnPair(t)
			client.SetReadTimeout(20 * time.Millisecond)
			client.SetCompressType(test.compress)
			client.codecStallTimeout = 200 * time.Millisecond

			if _, err := server.conn.Write(halfCodecBlock(t, test.compress, "stalled-peer-payload")); err != nil {
				t.Fatalf("peer write failed: %v", err)
			}
			// the peer now stalls: no more bytes, and the connection stays open.

			// recv runs in a goroutine so a regression (no deadline on the codec
			// stream) fails the test in seconds instead of hanging until the test
			// binary panics.
			recvErr := make(chan error, 1)
			go func() {
				buf := make([]byte, 64)
				for callsLeft := 1000; callsLeft > 0; callsLeft-- {
					if _, err := client.recv(buf); err != nil {
						recvErr <- err
						return
					}
				}
				recvErr <- nil
			}()

			var err error
			select {
			case err = <-recvErr:
			case <-time.After(3 * time.Second):
				t.Fatal("recv never returned: the read deadline did not reach the socket")
			}

			assertCodecStreamBroken(t, err)
			if !client.codecBroken.Load() {
				t.Fatal("connection was not latched as broken")
			}
			// the codec connection must be terminated, not just marked unusable:
			// the peer has to observe the close instead of a silent open socket.
			if err := client.conn.SetReadDeadline(time.Now()); err == nil {
				t.Fatal("underlying conn is still open after a stalled read")
			}
			_ = server.conn.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := server.conn.Read(make([]byte, 8)); err == nil {
				t.Fatal("peer read still succeeded after the codec connection was terminated")
			}
			// a broken stream must fail fast instead of touching the codec again
			if _, err := client.recv(make([]byte, 64)); !errors.Is(err, ErrCodecStreamBroken) {
				t.Fatalf("recv after break = %v, want ErrCodecStreamBroken", err)
			}
			if _, err := client.Send([]byte("nope")); !errors.Is(err, ErrCodecStreamBroken) {
				t.Fatalf("Send after break = %v, want ErrCodecStreamBroken", err)
			}
		})
	}
}

// TestCodecSendStalledPeerBreaksStream covers the write half of the P1: a peer
// that stops reading must not block Send forever. net.Pipe is unbuffered, so a
// write blocks until the peer reads - exactly the stalled-reader condition.
func TestCodecSendStalledPeerBreaksStream(t *testing.T) {
	for _, test := range codecStallCases {
		t.Run(test.name, func(t *testing.T) {
			clientRaw, peerRaw := net.Pipe()
			t.Cleanup(func() {
				_ = clientRaw.Close()
				_ = peerRaw.Close()
			})

			client := newGettyTCPConn(clientRaw)
			client.SetWriteTimeout(50 * time.Millisecond)
			client.SetCompressType(test.compress)
			// zero write progress for this long means the peer stopped reading
			client.codecStallTimeout = 200 * time.Millisecond

			sendErr := make(chan error, 1)
			go func() {
				_, err := client.Send([]byte("peer never reads this"))
				sendErr <- err
			}()

			var err error
			select {
			case err = <-sendErr:
			case <-time.After(3 * time.Second):
				t.Fatal("Send never returned: the write deadline did not reach the socket")
			}
			assertCodecStreamBroken(t, err)
			if !client.codecBroken.Load() {
				t.Fatal("connection was not latched as broken")
			}
			// the codec connection must be terminated: the stalled peer has to
			// observe the close instead of a still-open pipe.
			if err := client.conn.SetWriteDeadline(time.Now()); err == nil {
				t.Fatal("underlying conn is still open after a stalled write")
			}
			if _, err := peerRaw.Write([]byte("ping")); err == nil {
				t.Fatal("peer write still succeeded after the codec connection was terminated")
			}

			// the second Send must fail fast rather than block on the dead codec
			start := time.Now()
			if _, err = client.Send([]byte("still nope")); !errors.Is(err, ErrCodecStreamBroken) {
				t.Fatalf("Send after break = %v, want ErrCodecStreamBroken", err)
			}
			if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
				t.Fatalf("Send after break blocked for %s, want an immediate failure", elapsed)
			}
		})
	}
}

// TestCodecRecvIdleThenResume is the regression guard for the deadline value: a
// codec stream must survive an idle period of many read timeouts. The read
// timeout is a poll interval (session.handleTCPPackage retries it), so arming it
// on a codec stream would kill every idle compressed connection - flate/snappy
// latch the timeout and never decode again.
func TestCodecRecvIdleThenResume(t *testing.T) {
	for _, test := range codecStallCases {
		t.Run(test.name, func(t *testing.T) {
			client, server := newTCPConnPair(t)
			client.SetReadTimeout(20 * time.Millisecond)
			server.SetWriteTimeout(time.Second)
			client.SetCompressType(test.compress)
			server.SetCompressType(test.compress)
			client.codecStallTimeout = 2 * time.Second

			const idle = 200 * time.Millisecond // 10 read timeouts
			payload := []byte("packet-after-idle")
			writeErr := make(chan error, 1)
			go func() {
				time.Sleep(idle)
				_, err := server.Send(payload)
				writeErr <- err
			}()

			got := make([]byte, 0, len(payload))
			buf := make([]byte, 64)
			for len(got) < len(payload) {
				n, err := client.recv(buf)
				if err != nil {
					t.Fatalf("recv after %s of idling failed: %v", idle, err)
				}
				got = append(got, buf[:n]...)
			}
			if err := <-writeErr; err != nil {
				t.Fatalf("peer Send failed: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("decoded %q, want %q", got, payload)
			}
			if client.codecBroken.Load() {
				t.Fatal("an idle codec stream was wrongly latched as broken")
			}
		})
	}
}

// TestCodecRecvIdleBeyondStallTimeoutStaysHealthy pins the stall/idle
// distinction: CodecStallTimeout only applies to a stream that stalled in the
// middle of a codec block. A connection that never received a byte, or that is
// idle between fully decoded packets, must survive silence far beyond the
// stall timeout and resume normally - previously any silence longer than the
// timeout was misclassified as a broken stream and the socket was closed.
func TestCodecRecvIdleBeyondStallTimeoutStaysHealthy(t *testing.T) {
	for _, test := range codecStallCases {
		t.Run(test.name, func(t *testing.T) {
			client, server := newTCPConnPair(t)
			client.SetReadTimeout(20 * time.Millisecond)
			client.SetCompressType(test.compress)
			client.codecStallTimeout = 100 * time.Millisecond
			server.SetWriteTimeout(time.Second)
			server.SetCompressType(test.compress)

			payload := []byte("packet-after-long-idle")
			expected := bytes.Repeat(payload, 2)
			gotCh, errCh := startReceiver(client, len(expected), 8)

			// silence with zero bytes ever received, 4x the stall timeout
			const idle = 400 * time.Millisecond
			select {
			case err := <-errCh:
				t.Fatalf("connection that never received a byte was killed after idling: %v", err)
			case <-time.After(idle):
			}
			if client.codecBroken.Load() {
				t.Fatal("never-used codec stream was latched as broken by pure idleness")
			}
			if _, err := server.Send(payload); err != nil {
				t.Fatalf("peer Send after idle failed: %v", err)
			}

			// idle again between two fully decoded packets, then resume
			select {
			case err := <-errCh:
				t.Fatalf("connection idling between packets was killed: %v", err)
			case <-time.After(idle):
			}
			if client.codecBroken.Load() {
				t.Fatal("codec stream idling between packets was latched as broken")
			}
			if _, err := server.Send(payload); err != nil {
				t.Fatalf("peer Send after second idle failed: %v", err)
			}

			select {
			case got := <-gotCh:
				if !bytes.Equal(got, expected) {
					t.Fatalf("decoded stream mismatch:\n got: %q\nwant: %q", got, expected)
				}
			case err := <-errCh:
				t.Fatalf("peer failed to decode after idle periods: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("timed out waiting for the decoded stream")
			}
			if client.codecBroken.Load() {
				t.Fatal("healthy idle connection ended up latched as broken")
			}
		})
	}
}

// TestCodecRecvStallAcrossRecvBoundaryBreaksStream pins the exactness of the
// stall detection: the peer flushes one complete packet plus the first half of
// the next block in a single burst, then dies. The remainder is delivered to
// the decoder from the poller's own read-ahead buffer after the first packet
// decoded, so the following silence must still be classified as a mid-block
// stall - with a hidden bufio between poller and decoder this case was
// indistinguishable from idleness and hung until session close.
func TestCodecRecvStallAcrossRecvBoundaryBreaksStream(t *testing.T) {
	for _, test := range codecStallCases {
		t.Run(test.name, func(t *testing.T) {
			client, server := newTCPConnPair(t)
			client.SetReadTimeout(20 * time.Millisecond)
			client.SetCompressType(test.compress)
			client.codecStallTimeout = 200 * time.Millisecond

			// one codec stream: full flushed block for payload1, then payload2's
			// block truncated in half - exactly what a peer that dies mid-write
			// leaves after a healthy packet.
			payload1 := []byte("complete-first-packet")
			var (
				buf    bytes.Buffer
				writer interface {
					io.Writer
					Flush() error
				}
			)
			if test.compress == CompressSnappy {
				writer = snappy.NewBufferedWriter(&buf)
			} else {
				flateWriter, err := flate.NewWriter(&buf, int(test.compress))
				if err != nil {
					t.Fatal(err)
				}
				writer = flateWriter
			}
			if _, err := writer.Write(payload1); err != nil {
				t.Fatal(err)
			}
			if err := writer.Flush(); err != nil {
				t.Fatal(err)
			}
			firstLen := buf.Len()
			if _, err := writer.Write([]byte("second-packet-that-never-finishes")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Flush(); err != nil {
				t.Fatal(err)
			}
			second := buf.Bytes()[firstLen:]
			if len(second) < 4 {
				t.Fatalf("second codec block too small to truncate: %d bytes", len(second))
			}
			burst := buf.Bytes()[:firstLen+len(second)/2]

			if _, err := server.conn.Write(burst); err != nil {
				t.Fatalf("peer write failed: %v", err)
			}
			// the peer now dies: no more bytes, connection stays open.

			recvErr := make(chan error, 1)
			go func() {
				var got []byte
				buf := make([]byte, 64)
				for {
					n, err := client.recv(buf)
					if err != nil {
						recvErr <- err
						return
					}
					got = append(got, buf[:n]...)
					if len(got) > len(payload1) {
						recvErr <- fmt.Errorf("decoded beyond the first packet: %q", got)
						return
					}
				}
			}()

			var err error
			select {
			case err = <-recvErr:
			case <-time.After(3 * time.Second):
				t.Fatal("recv never returned: the cross-recv stall was classified as idleness")
			}
			assertCodecStreamBroken(t, err)
			if !client.codecBroken.Load() {
				t.Fatal("connection was not latched as broken")
			}
		})
	}
}

// TestCodecSendSlowPeerBeyondWriteTimeoutSurvives is the write-side twin of the
// idle/stall distinction: a peer that drains slowly but steadily must not be
// killed just because one compressed burst takes longer than wTimeout - only
// zero progress for codecStallTimeout may break the stream.
func TestCodecSendSlowPeerBeyondWriteTimeoutSurvives(t *testing.T) {
	for _, test := range codecStallCases {
		t.Run(test.name, func(t *testing.T) {
			clientRaw, peerRaw := net.Pipe()
			t.Cleanup(func() {
				_ = clientRaw.Close()
				_ = peerRaw.Close()
			})

			client := newGettyTCPConn(clientRaw)
			client.SetWriteTimeout(20 * time.Millisecond)
			client.SetCompressType(test.compress)
			client.codecStallTimeout = 500 * time.Millisecond

			// the peer drains a few bytes at a time, far slower than wTimeout
			// allows for the whole burst, but never stops for a stall window.
			peerDone := make(chan struct{})
			go func() {
				defer close(peerDone)
				buf := make([]byte, 8)
				for {
					if _, err := peerRaw.Read(buf); err != nil {
						return
					}
					time.Sleep(30 * time.Millisecond)
				}
			}()

			payload := bytes.Repeat([]byte("slow-but-alive-"), 20) // 300 bytes
			sendErr := make(chan error, 1)
			go func() {
				_, err := client.Send(payload)
				sendErr <- err
			}()

			select {
			case err := <-sendErr:
				if err != nil {
					t.Fatalf("Send to a slow but draining peer failed: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Send never completed against a slow but draining peer")
			}
			if client.codecBroken.Load() {
				t.Fatal("slow but draining peer was latched as a broken stream")
			}
			_ = clientRaw.Close()
			<-peerDone
		})
	}
}

// TestRawConnRecvTimeoutStaysRetryable pins the other half of the contract: on a
// raw connection a read timeout is still a benign, retryable net.Error - it is
// the poll that lets session.handleTCPPackage notice a closed session.
func TestRawConnRecvTimeoutStaysRetryable(t *testing.T) {
	client, _ := newTCPConnPair(t)
	client.SetReadTimeout(50 * time.Millisecond)

	_, err := client.recv(make([]byte, 64))
	if err == nil {
		t.Fatal("recv on a silent peer returned no error")
	}
	if errors.Is(err, ErrCodecStreamBroken) {
		t.Fatalf("raw conn read timeout reported as a broken codec stream: %v", err)
	}
	netErr, ok := perrors.Cause(err).(net.Error)
	if !ok || !netErr.Timeout() {
		t.Fatalf("cause = %v, want a net.Error timeout", perrors.Cause(err))
	}
	if client.codecBroken.Load() {
		t.Fatal("raw conn was latched as broken")
	}
}

// TestCodecRecvPeerCloseKeepsErrorIdentity pins the deliberate scope of the
// latch: only a timeout means "stalled mid-stream". A clean peer close must keep
// its own EOF-family error, which session.handleTCPPackage matches on to skip
// reconnecting, and must not latch the connection - otherwise a concurrent
// WritePkg would report ErrCodecStreamBroken instead of the real cause.
func TestCodecRecvPeerCloseKeepsErrorIdentity(t *testing.T) {
	for _, test := range codecStallCases {
		t.Run(test.name, func(t *testing.T) {
			client, server := newTCPConnPair(t)
			client.SetReadTimeout(time.Second)
			client.SetCompressType(test.compress)
			server.CloseConn(0)

			_, err := client.recv(make([]byte, 64))
			if err == nil {
				t.Fatal("recv on a closed peer returned no error")
			}
			if errors.Is(err, ErrCodecStreamBroken) {
				t.Fatalf("clean peer close reported as a stalled codec stream: %v", err)
			}
			if client.codecBroken.Load() {
				t.Fatal("clean peer close latched the connection as broken")
			}
		})
	}
}

// TestSetCompressTypeAfterIORejected pins the codec configuration contract:
// once a connection has sent or received, SetCompressType must panic instead
// of replacing the codec under in-flight IO and desynchronizing the peer.
func TestSetCompressTypeAfterIORejected(t *testing.T) {
	client, server := newTCPConnPair(t)

	payload := []byte("started")
	gotCh, errCh := startReceiver(server, len(payload), 8)
	if _, err := client.Send(payload); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	select {
	case <-gotCh:
	case err := <-errCh:
		t.Fatalf("peer failed to read: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the payload")
	}

	for name, conn := range map[string]*gettyTCPConn{"sender": client, "receiver": server} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("SetCompressType on a started %s did not panic", name)
				}
			}()
			conn.SetCompressType(CompressSnappy)
		}()
	}
}

// TestSetCompressTypeConcurrentWithSend is the race regression for the PR#107
// review: SetCompressType(CompressSnappy) racing with Send([]byte) on a started
// stream must be rejected, stay race-free under `go test -race` and leave the
// raw stream intact.
func TestSetCompressTypeConcurrentWithSend(t *testing.T) {
	client, server := newTCPConnPair(t)

	payload := []byte("payload-")
	const sends = 100
	expected := bytes.Repeat(payload, sends+1)
	gotCh, errCh := startReceiver(server, len(expected), 16)

	// start the stream, so the SetCompressType below is guaranteed to be late
	if _, err := client.Send(payload); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	start := make(chan struct{})
	panicked := make(chan bool, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < sends; i++ {
			if _, err := client.Send(payload); err != nil {
				t.Errorf("Send failed: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		defer func() { panicked <- recover() != nil }()
		<-start
		client.SetCompressType(CompressSnappy)
	}()
	close(start)
	wg.Wait()

	if !<-panicked {
		t.Fatal("late SetCompressType was not rejected")
	}
	if client.codecEnabled {
		t.Fatal("rejected SetCompressType still installed a codec")
	}
	select {
	case got := <-gotCh:
		if !bytes.Equal(got, expected) {
			t.Fatalf("stream corrupted:\n got: %q\nwant: %q", got, expected)
		}
	case err := <-errCh:
		t.Fatalf("peer failed to read: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the stream")
	}
}

// TestUDPSetCompressTypeHasNoWireEffect pins the UDP contract: compression is
// not supported, the call records the type (and warns) instead of silently
// pretending or panicking under existing callers.
func TestUDPSetCompressTypeHasNoWireEffect(t *testing.T) {
	raw, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	conn := newGettyUDPConn(raw)
	t.Cleanup(func() { conn.CloseConn(0) })

	conn.SetCompressType(CompressSnappy)
	if conn.compress != CompressSnappy {
		t.Fatalf("compress = %d, want %d recorded", conn.compress, CompressSnappy)
	}
}
