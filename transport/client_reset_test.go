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
	"testing"
	"time"
)

// closeClientPoolSession follows the admitted session's EOF/OnClose path. It
// waits outside the callback and leaves pool maintenance to the caller.
func closeClientPoolSession(t *testing.T, c *client, reset bool) *session {
	t.Helper()

	closed := make(chan struct{})
	request := make(chan (<-chan error), 1)
	s := newTCPSession(&eofDataNetConn{}, c).(*session)
	s.SetAttribute(sessionClientKey, c)
	s.SetAttribute(ignoreReconnectKey, false)
	s.SetPkgHandler(wholeBufferReadWriter{})
	s.SetEventListener(&resetContractListener{target: "close", hook: func(ss Session) {
		if reset {
			request <- ss.(AsyncResetter).ResetAsync()
		}
		close(closed)
	}})
	c.Lock()
	c.ssMap[s] = struct{}{}
	c.Unlock()
	s.run()

	waitForReset(t, closed)
	if reset {
		waitForReset(t, <-request)
	} else {
		s.grWG.Wait()
		s.lifecycle.wait()
	}
	if s.Conn() != nil {
		t.Fatal("closed session still has a connection")
	}
	if reset && s.IsClosed() {
		t.Fatal("Reset changed its public IsClosed behavior")
	}
	return s
}

func TestClientPoolDropsClosedAndResetSessions(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "close"
		if reset {
			name = "reset"
		}
		t.Run(name, func(t *testing.T) {
			c := newClient(TCP_CLIENT, WithServerAddress("127.0.0.1:1"), WithConnectionNumber(1))
			t.Cleanup(c.Close)
			s := closeClientPoolSession(t, c, reset)
			if n := c.sessionNum(); n != 0 {
				t.Fatalf("pool counts %d sessions after EOF/reset; Conn=%v IsClosed=%v",
					n, s.Conn(), s.IsClosed())
			}
		})
	}
}

func TestClientPoolRefillsAfterCloseAndReset(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "close"
		if reset {
			name = "reset"
		}
		t.Run(name, func(t *testing.T) {
			builder := &countingTLSConfigBuilder{}
			c := newFailingReconnectClient(builder, time.Millisecond, 1)
			t.Cleanup(c.Close)
			closeClientPoolSession(t, c, reset)
			if got := builder.calls.Load(); got != 0 {
				t.Fatalf("EOF triggered %d automatic dial attempts; want 0", got)
			}
			// Do not call sessionNum first: RunEventLoop must find the gap itself.
			c.RunEventLoop(func(Session) error { return nil })
			if got := builder.calls.Load(); got != 1 {
				t.Fatalf("RunEventLoop made %d replacement dial attempts; want 1", got)
			}
		})
	}
}

func TestTCPClientAdmitsReplacementAfterReset(t *testing.T) {
	for _, entry := range []string{"RunEventLoop", "connect"} {
		t.Run(entry, func(t *testing.T) {
			c, _, _, release, _ := newBlockingReconnectClient(t)
			release()
			old := closeClientPoolSession(t, c, true)

			// In particular, direct connect must prune the old empty session
			// without sessionNum having already removed it.
			if entry == "RunEventLoop" {
				c.RunEventLoop(c.newSession)
			} else if !c.connect() {
				t.Fatal("connect rejected the replacement for an empty reset session")
			}

			c.Lock()
			total := len(c.ssMap)
			_, retained := c.ssMap[old]
			var replacement Session
			for ss := range c.ssMap {
				replacement = ss
			}
			c.Unlock()
			if total != 1 || retained {
				t.Fatalf("pool has %d sessions; old reset session retained=%v", total, retained)
			}
			if replacement.Conn() == nil || replacement.IsClosed() {
				t.Fatal("replacement was admitted without a live connection")
			}

			// The same admission path must still reject a surplus connection.
			if c.connect() {
				t.Fatal("connect admitted a session above WithConnectionNumber")
			}
			c.Lock()
			total = len(c.ssMap)
			c.Unlock()
			if total != 1 {
				t.Fatalf("pool size after surplus connect = %d; want 1", total)
			}
		})
	}
}
