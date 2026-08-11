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
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

import (
	"github.com/golang/snappy"
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
