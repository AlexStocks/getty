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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

import (
	"github.com/gorilla/websocket"
)

// newWSClientConn returns a getty WebSocket connection backed by a test server.
func newWSClientConn(t *testing.T) *gettyWSConn {
	t.Helper()

	upgrader := websocket.Upgrader{}
	serverConnCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnCh <- conn
	}))
	t.Cleanup(srv.Close)

	clientWS, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	serverWS := <-serverConnCh
	t.Cleanup(func() {
		_ = clientWS.Close()
		_ = serverWS.Close()
	})
	return newGettyWSConn(clientWS)
}

// TestWSConcurrentWriteDeadlines exercises all Getty WebSocket write paths
// together. Run with -race to detect unsynchronized Gorilla deadline updates.
func TestWSConcurrentWriteDeadlines(t *testing.T) {
	for _, concurrentClose := range []bool{false, true} {
		t.Run(fmt.Sprintf("close=%t", concurrentClose), func(t *testing.T) {
			server, peer := newWSTestConnPair(t)
			ss := newWSSession(server, nil).(*session)
			conn := ss.Connection.(*gettyWSConn)
			ss.SetWriteTimeout(3 * time.Second)
			payload := []byte("concurrent websocket payload")
			const (
				iterations = 100
				// A negative linger restores the OS default close. Linger 0 sends
				// an RST, which can discard the close frame and turn the expected
				// clean close into 1006 on the peer.
				closeWaitSec = -1
			)

			type received struct {
				messages, pings, pongs int
				err                    error
			}
			result := make(chan received, 1)
			if err := peer.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			go func() {
				var got received
				// Count control frames without writing replies on the peer.
				peer.SetPingHandler(func(string) error { got.pings++; return nil })
				peer.SetPongHandler(func(string) error { got.pongs++; return nil })
				peer.SetCloseHandler(func(int, string) error { return nil })
				for {
					messageType, data, err := peer.ReadMessage()
					if err != nil {
						if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
							got.err = err
						}
						result <- got
						return
					}
					if messageType != websocket.BinaryMessage || !bytes.Equal(data, payload) {
						got.err = fmt.Errorf("unexpected frame: type=%d, payload=%q", messageType, data)
						result <- got
						return
					}
					got.messages++
				}
			}()

			// send writes one payload through the public Session.Send entry point.
			send := func() error {
				n, err := ss.Send(payload)
				if err == nil && n != len(payload) {
					return fmt.Errorf("Send returned %d bytes, want %d", n, len(payload))
				}
				return err
			}
			writers := []func() error{send, send, conn.writePing, func() error {
				return conn.writePong([]byte("pong"))
			}}
			start := make(chan struct{})
			var ready, done sync.WaitGroup
			ready.Add(len(writers))
			for _, write := range writers {
				done.Add(1)
				go func() {
					defer done.Done()
					// Every path performs one write before the concurrent close,
					// ensuring the connection is healthy before shutdown begins.
					if err := write(); err != nil {
						t.Errorf("initial write: %v", err)
					}
					ready.Done()
					<-start
					for i := 0; i < iterations; i++ {
						if err := write(); err != nil {
							if !concurrentClose || !errors.Is(err, websocket.ErrCloseSent) {
								t.Errorf("concurrent write: %v", err)
							}
							return
						}
					}
				}()
			}
			ready.Wait()
			if concurrentClose {
				done.Add(1)
				go func() {
					defer done.Done()
					<-start
					conn.CloseConn(closeWaitSec)
				}()
			}
			close(start)
			done.Wait()
			if !concurrentClose {
				conn.CloseConn(closeWaitSec)
			}
			got := <-result
			if got.err != nil {
				t.Fatal(got.err)
			}
			if concurrentClose {
				if got.messages < 2 || got.pings < 1 || got.pongs < 1 {
					t.Fatalf("missing writes before close: %+v", got)
				}
			} else if got.messages != 2*(iterations+1) || got.pings != iterations+1 || got.pongs != iterations+1 {
				t.Fatalf("lost or duplicated frames: %+v", got)
			}
		})
	}
}

// TestWSSendReturnsZeroOnError pins the write count contract: a failed
// WriteMessage delivers nothing, so Send must report 0 written bytes -
// returning len(p) alongside the error inflates the caller's success count
// (session.WritePkg uses it as successCount).
func TestWSSendReturnsZeroOnError(t *testing.T) {
	client := newWSClientConn(t)

	payload := []byte("ws-payload")
	if n, err := client.Send(payload); err != nil || n != len(payload) {
		t.Fatalf("healthy Send = (%d, %v), want (%d, nil)", n, err, len(payload))
	}
	if got := client.writePkgNum.Load(); got != 1 {
		t.Fatalf("writePkgNum = %d after one successful Send, want 1", got)
	}

	_ = client.conn.UnderlyingConn().Close()
	n, err := client.Send(payload)
	if err == nil {
		t.Fatal("Send on a closed websocket returned no error")
	}
	if n != 0 {
		t.Fatalf("failed Send reported %d written bytes, want 0", n)
	}
	if got := client.writePkgNum.Load(); got != 1 {
		t.Fatalf("failed Send bumped writePkgNum to %d, want it unchanged at 1", got)
	}
}
