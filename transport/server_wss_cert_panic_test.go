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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWSSCertificateErrorsPanicOnCallerStackBeforeListen(t *testing.T) {
	certPath, err := filepath.Abs("../examples/profiles/wss/server_cert/server.crt")
	if err != nil {
		t.Fatal(err)
	}
	keyPath, err := filepath.Abs("../examples/profiles/wss/server_cert/server.key")
	if err != nil {
		t.Fatal(err)
	}
	missingCert := filepath.Join(t.TempDir(), "missing.crt")
	missingKey := filepath.Join(t.TempDir(), "missing.key")
	missingCA := filepath.Join(t.TempDir(), "missing-ca.crt")
	invalidCA := filepath.Join(t.TempDir(), "invalid-ca.crt")
	if err := os.WriteFile(invalidCA, []byte("not a PEM certificate"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name      string
		cert      string
		key       string
		ca        string
		wantPanic []string
	}{
		{
			name:      "missing certificate and key",
			cert:      missingCert,
			key:       missingKey,
			wantPanic: []string{missingCert, missingKey},
		},
		{
			name:      "missing CA file",
			cert:      certPath,
			key:       keyPath,
			ca:        missingCA,
			wantPanic: []string{missingCA},
		},
		{
			name:      "invalid CA PEM",
			cert:      certPath,
			key:       keyPath,
			ca:        invalidCA,
			wantPanic: []string{invalidCA},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			addr := wssUnusedTCPAddress(t)
			srv := newServer(
				WSS_SERVER,
				WithLocalAddress(addr),
				WithWebsocketServerPath("/ws"),
				WithWebsocketServerCert(test.cert),
				WithWebsocketServerPrivateKey(test.key),
				WithWebsocketServerRootCert(test.ca),
			)
			t.Cleanup(srv.Close)

			goroutinesBefore := runtime.NumGoroutine()
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				srv.RunEventLoop(func(Session) error { return nil })
			}()
			if recovered == nil {
				t.Fatal("RunEventLoop did not panic for invalid WSS certificate configuration")
			}
			for _, want := range test.wantPanic {
				if !strings.Contains(fmt.Sprint(recovered), want) {
					t.Errorf("panic %q does not include configured path %q", recovered, want)
				}
			}

			if listener := srv.Listener(); listener != nil {
				t.Fatalf("Listener() = %v after certificate panic, want nil", listener)
			}
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				t.Fatalf("address %s is occupied after certificate panic: %v", addr, err)
			}
			_ = listener.Close()

			closed := make(chan struct{})
			go func() {
				srv.Close()
				close(closed)
			}()
			select {
			case <-closed:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Close did not return promptly after recovering from certificate panic")
			}

			deadline := time.Now().Add(500 * time.Millisecond)
			for runtime.NumGoroutine() > goroutinesBefore && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if got := runtime.NumGoroutine(); got > goroutinesBefore {
				t.Fatalf("goroutine count after recovered certificate panic = %d, want at most baseline %d", got, goroutinesBefore)
			}
		})
	}
}

func wssUnusedTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
