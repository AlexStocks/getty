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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

import (
	"github.com/gorilla/websocket"
)

// newCompressedWSTestConnPair negotiates permessage-deflate on both endpoints.
func newCompressedWSTestConnPair(t *testing.T) (server, client *websocket.Conn) {
	t.Helper()
	upgrader := websocket.Upgrader{EnableCompression: true}
	serverCh := make(chan *websocket.Conn, 1)
	errCh := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}
		serverCh <- conn
	}))
	t.Cleanup(srv.Close)
	dialer := websocket.Dialer{EnableCompression: true, HandshakeTimeout: 3 * time.Second}
	client, resp, err := dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	t.Cleanup(func() { _ = client.Close() })
	select {
	case server = <-serverCh:
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("websocket upgrade did not complete")
	}
	t.Cleanup(func() { _ = server.Close() })
	return server, client
}

// wsReceiveLimitReader records decoder calls while reporting only a short
// package, so its reported length cannot enforce the WebSocket message limit.
type wsReceiveLimitReader struct {
	calls int
}

// Read returns at most one byte from the message as an application package.
func (r *wsReceiveLimitReader) Read(_ Session, data []byte) (any, int, error) {
	r.calls++
	n := min(1, len(data))
	return bytes.Clone(data[:n]), n, nil
}

// TestWSDecompressedReceiveLimit checks decoded message boundaries for both
// endpoints, with and without an application decoder or wire compression.
func TestWSDecompressedReceiveLimit(t *testing.T) {
	for _, role := range []string{"server", "client"} {
		for _, compressed := range []bool{false, true} {
			for _, withReader := range []bool{false, true} {
				for _, test := range []struct {
					name  string
					limit int
					size  int
				}{
					{name: "empty", limit: 1024},
					{name: "below", limit: 1024, size: 1023},
					{name: "at", limit: 1024, size: 1024},
					{name: "above", limit: 1024, size: 1025},
					{name: "large", limit: 1024, size: 64 * 1024},
					{name: "unlimited", size: 64 * 1024},
				} {
					name := fmt.Sprintf("%s/compressed=%t/reader=%t/%s", role, compressed, withReader, test.name)
					t.Run(name, func(t *testing.T) {
						server, client := newCompressedWSTestConnPair(t)
						receiver, sender := server, client
						if role == "client" {
							receiver, sender = client, server
						}
						ss := newWSSession(receiver, nil).(*session)
						ss.SetMaxMsgLen(test.limit)
						applyWSReadLimit(ss)
						listener := &recordingEventListener{}
						ss.SetEventListener(listener)
						reader := &wsReceiveLimitReader{}
						if withReader {
							ss.SetReader(reader)
						}
						payload := bytes.Repeat([]byte("a"), test.size)
						sender.EnableWriteCompression(compressed)
						if err := sender.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
							t.Fatal(err)
						}
						written := make(chan error, 1)
						go func() {
							written <- sender.WriteMessage(websocket.BinaryMessage, payload)
							_ = sender.Close()
						}()
						// Close a stalled transport to bound test failures without
						// introducing the read timeouts that the session retries.
						timer := time.AfterFunc(3*time.Second, func() { _ = receiver.Close() })
						defer timer.Stop()
						err := ss.handleWSPackage()
						if writeErr := <-written; writeErr != nil {
							t.Fatal(writeErr)
						}
						if test.limit > 0 && test.size > test.limit {
							if !errors.Is(err, websocket.ErrReadLimit) {
								t.Fatalf("handleWSPackage error = %v, want ErrReadLimit", err)
							}
							if len(listener.messages) != 0 || reader.calls != 0 {
								t.Fatalf("oversized message reached application: messages=%d, decoder calls=%d", len(listener.messages), reader.calls)
							}
							return
						}
						if len(listener.messages) != 1 {
							t.Fatalf("received %d messages, want 1; read error: %v", len(listener.messages), err)
						}
						want := payload
						if withReader {
							want = payload[:min(1, len(payload))]
							if reader.calls != 1 {
								t.Fatalf("decoder calls = %d, want 1", reader.calls)
							}
						}
						if got := listener.messages[0].([]byte); !bytes.Equal(got, want) {
							t.Fatalf("received %d bytes, want %d", len(got), len(want))
						}
					})
				}
			}
		}
	}
}

// TestWSDecompressedReceiveLimitBeforeMessageEnd verifies that decompression
// stops at the limit even when the peer withholds the final continuation frame.
func TestWSDecompressedReceiveLimitBeforeMessageEnd(t *testing.T) {
	server, client := newCompressedWSTestConnPair(t)
	ss := newWSSession(client, nil).(*session)
	ss.SetMaxMsgLen(1024)
	applyWSReadLimit(ss)
	listener := &recordingEventListener{}
	ss.SetEventListener(listener)

	var compressed bytes.Buffer
	encoder, err := flate.NewWriter(&compressed, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encoder.Write(bytes.Repeat([]byte("a"), 64*1024)); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Flush(); err != nil {
		t.Fatal(err)
	}
	// A non-final compressed binary frame, small enough for a one-byte length.
	wire := bytes.Clone(compressed.Bytes())
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	if len(wire) >= 126 {
		t.Fatalf("compressed test fragment is too large: %d", len(wire))
	}
	if err := server.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := server.UnderlyingConn().Write(append([]byte{0x42, byte(len(wire))}, wire...)); err != nil {
		t.Fatal(err)
	}
	// Close the transport on failure rather than injecting a read timeout:
	// the session retries timeouts, but Gorilla cannot recover from them.
	timer := time.AfterFunc(3*time.Second, func() { _ = client.Close() })
	defer timer.Stop()
	if err := ss.handleWSPackage(); !errors.Is(err, websocket.ErrReadLimit) {
		t.Fatalf("incomplete compressed message error = %v, want ErrReadLimit before message ends", err)
	}
	if len(listener.messages) != 0 {
		t.Fatal("incomplete oversized message reached OnMessage")
	}
}

// TestWSReceiveLimitConcurrentUpdate drives SetMaxMsgLen while handleWSPackage
// reads messages. Run it with -race (make test-race): the receive loop must
// snapshot the limit under the same lock the setter uses instead of reading the
// field directly.
func TestWSReceiveLimitConcurrentUpdate(t *testing.T) {
	server, client := newCompressedWSTestConnPair(t)
	ss := newWSSession(client, nil).(*session)
	ss.SetMaxMsgLen(1024)
	applyWSReadLimit(ss)
	ss.SetEventListener(&recordingEventListener{})

	const messageCount = 200
	if err := server.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for range messageCount {
			if err := server.WriteMessage(websocket.BinaryMessage, []byte("x")); err != nil {
				return
			}
		}
		_ = server.Close()
	}()

	stop := make(chan struct{})
	setterDone := make(chan struct{})
	go func() {
		defer close(setterDone)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				ss.SetMaxMsgLen(1024 + i%2)
			}
		}
	}()

	if err := ss.handleWSPackage(); err == nil {
		t.Fatal("handleWSPackage returned nil after the peer closed the connection")
	}
	close(stop)
	<-setterDone
	<-writeDone
}
