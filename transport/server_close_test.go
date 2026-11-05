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
	"net"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// TestServerCloseInsideTCPStartup preserves issue #162's two direct-call probes.
// A child process contains a self-wait regression and its blocked goroutine.
func TestServerCloseInsideTCPStartup(t *testing.T) {
	phase := os.Getenv("GETTY_TEST_TCP_STARTUP_CLOSE")
	if phase == "" {
		for _, phase := range []string{"new-session", "open"} {
			t.Run(phase, func(t *testing.T) {
				binary, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.CommandContext(t.Context(), binary,
					"-test.run=^TestServerCloseInsideTCPStartup$", "-test.timeout=10s")
				cmd.Env = append(os.Environ(), "GETTY_TEST_TCP_STARTUP_CLOSE="+phase)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("startup callback did not complete: %v\n%s", err, output)
				}
			})
		}
		return
	}
	local, remote := net.Pipe()
	defer func() { _ = local.Close(); _ = remote.Close() }()
	// net.Pipe addresses compare equal, so use distinct endpoint names.
	conn := &startupDistinctAddrConn{Conn: local}
	srv := newServer(TCP_SERVER)
	srv.streamListener = &singleAcceptListener{conn: conn}
	entered := make(chan struct{})
	returned := make(chan struct{})
	closeServer := func() {
		close(entered)
		srv.Close()
		close(returned)
	}
	listener := &resetContractListener{target: "open", hook: func(Session) { closeServer() }}
	if phase == "new-session" {
		listener.target = ""
	}
	srv.runTCPEventLoop(func(s Session) error {
		s.SetPkgHandler(wholeBufferReadWriter{})
		s.SetEventListener(listener)
		if phase == "new-session" {
			closeServer()
		}
		return nil
	})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("startup callback was not invoked")
	}
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		stack := make([]byte, 64<<10)
		stack = stack[:runtime.Stack(stack, true)]
		t.Fatalf("Server.Close waits for its own accept goroutine\n%s", stack)
	}
}

// startupDistinctAddrConn prevents net.Pipe's equal addresses looking like a
// self-connection to the real TCP accept path.
type startupDistinctAddrConn struct{ net.Conn }

// LocalAddr gives the in-memory server connection its own endpoint address.
func (*startupDistinctAddrConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 10001}
}

// RemoteAddr supplies a client endpoint distinct from LocalAddr.
func (*startupDistinctAddrConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 10002}
}
