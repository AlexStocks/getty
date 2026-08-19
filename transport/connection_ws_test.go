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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

import (
	"github.com/gorilla/websocket"
)

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
