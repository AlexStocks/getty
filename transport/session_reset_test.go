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
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

import (
	gxsync "github.com/dubbogo/gost/sync"
)

// resetContractListener invokes a test hook from a selected lifecycle callback.
type resetContractListener struct {
	target string
	hook   func(Session)
}

// call dispatches only the callback selected by the test.
func (l *resetContractListener) call(name string, s Session) {
	if name == l.target {
		l.hook(s)
	}
}

// OnOpen exercises reset before the read goroutine has started.
func (l *resetContractListener) OnOpen(s Session) error { l.call("open", s); return nil }

// OnMessage exercises reset while the package loop owns lifecycle work.
func (l *resetContractListener) OnMessage(s Session, _ any) { l.call("message", s) }

// OnError exercises reset in the package loop's shutdown defer.
func (l *resetContractListener) OnError(s Session, _ error) { l.call("error", s) }

// OnClose exercises the original self-wait regression.
func (l *resetContractListener) OnClose(s Session) { l.call("close", s) }

// OnCron exercises reset while the heartbeat owns lifecycle work.
func (l *resetContractListener) OnCron(s Session) { l.call("cron", s) }

// resetContractCodec can request reset from a reader or writer callback.
type resetContractCodec struct {
	hook func(Session)
	fail bool
}

// Read returns a complete frame or the test error after invoking the hook.
func (c resetContractCodec) Read(s Session, data []byte) (any, int, error) {
	if c.hook != nil {
		c.hook(s)
	}
	if c.fail {
		return nil, 0, errTestReadFailure
	}
	return string(data), len(data), nil
}

// Write invokes the test hook before returning the encoded payload.
func (c resetContractCodec) Write(s Session, _ any) ([]byte, error) {
	if c.hook != nil {
		c.hook(s)
	}
	return []byte("payload"), nil
}

// waitForReset bounds a test's wait for asynchronous cleanup and reset.
func waitForReset[T any](t *testing.T, done <-chan T) {
	t.Helper()
	select {
	case result := <-done:
		if err, ok := any(result).(error); ok && err != nil {
			t.Fatalf("reset failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reset did not complete")
	}
}

// TestResetCallbackContract checks prompt rejection of synchronous reset and
// deferred completion of asynchronous reset in every managed callback path.
func TestResetCallbackContract(t *testing.T) {
	for _, target := range []string{"open", "message", "error", "close", "cron", "registered close", "reader", "writer", "pooled message", "pooled cron"} {
		t.Run(target, func(t *testing.T) {
			endpoint := newServer(TCP_SERVER)
			if target == "pooled message" || target == "pooled cron" {
				pool := gxsync.NewTaskPoolSimple(1)
				t.Cleanup(pool.Close)
				endpoint = newServer(TCP_SERVER, WithServerTaskPool(pool))
			}
			ss := newTCPSession(&eofDataNetConn{data: []byte("frame")}, endpoint).(*session)
			oldLifecycle := ss.lifecycle
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			type request struct {
				err         error
				done, again <-chan error
			}
			entered := make(chan request, 1)
			hook := func(s Session) {
				err := s.(TryResetter).TryReset()
				// The compatibility path must return rather than wait for the
				// callback that is calling it; it starts the asynchronous reset.
				s.Reset()
				done := s.(AsyncResetter).ResetAsync()
				entered <- request{err, done, s.(AsyncResetter).ResetAsync()}
				<-release
			}
			listener := &resetContractListener{target: target, hook: hook}
			switch target {
			case "pooled message":
				listener.target = "message"
			case "pooled cron":
				listener.target = "cron"
			}
			ss.SetEventListener(listener)
			ss.SetPkgHandler(resetContractCodec{fail: target == "error"})
			switch target {
			case "registered close":
				ss.AddCloseCallback("reset", "test", func() { hook(ss) })
			case "reader":
				ss.SetReader(resetContractCodec{hook: hook})
			case "writer":
				ss.SetWriter(resetContractCodec{hook: hook})
			}
			operationDone := make(chan struct{})
			go func() {
				defer close(operationDone)
				switch target {
				case "cron", "pooled cron":
					_ = heartbeat(0, time.Time{}, &heartbeatContext{session: ss, lifecycle: oldLifecycle})
				case "pooled message":
					ss.addTask("frame")
				case "writer":
					_, _, _ = ss.WritePkg("payload", 0)
				default:
					ss.run()
				}
			}()
			var req request
			select {
			case req = <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("callback did not return from Reset/ResetAsync")
			}
			if !errors.Is(req.err, ErrResetDuringCallback) {
				t.Fatalf("TryReset error = %v, want ErrResetDuringCallback", req.err)
			}
			if req.done == req.again {
				t.Fatal("concurrent reset requests must have independent results")
			}
			// Session-wide activity also makes external Reset asynchronous.
			externalReset := make(chan struct{})
			go func() {
				ss.Reset()
				close(externalReset)
			}()
			select {
			case <-externalReset:
			case <-time.After(3 * time.Second):
				t.Fatal("external Reset blocked while a callback was active")
			}
			select {
			case <-req.done:
				t.Fatal("reset completed before its requesting callback returned")
			default:
			}
			ss.lock.RLock()
			sameGeneration := ss.lifecycle == oldLifecycle && ss.listener == listener
			ss.lock.RUnlock()
			if !sameGeneration {
				t.Fatal("reset cleared the active callback's generation")
			}
			unblock()
			waitForReset(t, req.done)
			waitForReset(t, req.again)
			waitForReset(t, operationDone)
			waitForReset(t, externalReset)
			if ss.Connection != nil || ss.listener != nil || ss.lifecycle == oldLifecycle {
				t.Fatal("completion notification preceded state reset")
			}
			if ss.callbackDepth.Load() != 0 || ss.grNum.Load() != 0 {
				t.Fatal("reset completed with active callbacks or read goroutines")
			}
			if oldLifecycle.acquire() {
				oldLifecycle.release()
				t.Fatal("old lifecycle accepted new work")
			}
		})
	}
}

// TestResetAsyncWaitsForWrites ensures completion also fences Send/WriteBytes,
// whose in-flight operations are protected by packetLock rather than lifecycle.
func TestResetAsyncWaitsForWrites(t *testing.T) {
	conn := newFragmentGateNetConn()
	ss := newTCPSession(conn, nil).(*session)
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		_, _ = ss.WriteBytes([]byte("payload"))
	}()
	select {
	case <-conn.entered:
	case <-time.After(3 * time.Second):
		close(conn.releaseFirst)
		t.Fatal("write did not enter transport")
	}
	done := ss.ResetAsync()
	select {
	case <-done:
		close(conn.releaseFirst)
		waitForReset(t, writeDone)
		t.Fatal("reset completed while a write was in progress")
	case <-time.After(30 * time.Millisecond):
	}
	close(conn.releaseFirst)
	waitForReset(t, done)
	waitForReset(t, writeDone)
}

// resetCleanupConn holds connection cleanup open to test reset's completion fence.
type resetCleanupConn struct {
	*gettyTCPConn
	entered chan struct{}
	release chan struct{}
}

// CloseConn blocks until the test allows resource cleanup to complete.
func (c *resetCleanupConn) CloseConn(int) { close(c.entered); <-c.release }

// TestResetAsyncWaitsForGC ensures notification includes asynchronous cleanup,
// even when gc detached the connection before the reset request arrived.
func TestResetAsyncWaitsForGC(t *testing.T) {
	conn := &resetCleanupConn{gettyTCPConn: newGettyTCPConn(&eofDataNetConn{}), entered: make(chan struct{}), release: make(chan struct{})}
	ss := newSession(nil, conn)
	ss.gc()
	waitForReset(t, conn.entered)
	done := ss.ResetAsync()
	select {
	case <-done:
		close(conn.release)
		t.Fatal("reset completed before connection cleanup")
	case <-time.After(30 * time.Millisecond):
	}
	close(conn.release)
	waitForReset(t, done)
}

// TestExternalResetDefersForActiveCallback checks that external Reset returns
// early while cleanup still waits for OnCron.
func TestExternalResetDefersForActiveCallback(t *testing.T) {
	listener := &blockingCronEventListener{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	ss := newTCPSession(&eofDataNetConn{}, newServer(TCP_SERVER)).(*session)
	ss.SetEventListener(listener)
	ctx := &heartbeatContext{session: ss, lifecycle: ss.lifecycle}
	heartbeatDone := make(chan error, 1)
	go func() {
		heartbeatDone <- heartbeat(0, time.Time{}, ctx)
	}()
	select {
	case <-listener.entered:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not enter OnCron")
	}

	resetReturned := make(chan struct{})
	go func() {
		ss.Reset()
		close(resetReturned)
	}()
	select {
	case <-resetReturned:
	case <-time.After(3 * time.Second):
		t.Fatal("external Reset blocked while a callback was active")
	}

	if err := ss.TryReset(); !errors.Is(err, ErrResetDuringCallback) {
		t.Fatalf("external TryReset = %v, want ErrResetDuringCallback", err)
	}
	result := ss.ResetAsync()
	select {
	case <-result:
		t.Fatal("reset completed before callback returned")
	default:
	}
	close(listener.release)
	select {
	case err := <-heartbeatDone:
		if err != nil {
			t.Fatalf("heartbeat returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not return")
	}
	waitForReset(t, result)
	if ss.Connection != nil || ss.listener != nil {
		t.Fatal("reset result arrived before session state was cleared")
	}
}

// panicCloseNetConn makes the reset worker panic before session state is reset.
type panicCloseNetConn struct {
	eofDataNetConn
}

// Close panics so the reset worker has to report a failed reset.
func (*panicCloseNetConn) Close() error { panic("test close panic") }

// TestResetReportsWorkerPanic ensures a panicking reset is observable and is not
// reported as a successful reset.
func TestResetReportsWorkerPanic(t *testing.T) {
	ss := newTCPSession(&panicCloseNetConn{}, nil).(*session)
	err := ss.TryReset()
	if err == nil {
		t.Fatal("TryReset returned nil after reset worker panic")
	}
	if !strings.Contains(err.Error(), "reset panic") {
		t.Fatalf("TryReset error = %v, want reset panic", err)
	}
	if ss.Connection == nil {
		t.Fatal("failed reset unexpectedly cleared the session connection")
	}
}

// TestResetSynchronousCompletion checks the non-callback API and repeat resets.
func TestResetSynchronousCompletion(t *testing.T) {
	ss := newTCPSession(&eofDataNetConn{}, nil).(*session)
	for range 2 {
		ss.SetMaxWriteMsgLen(123)
		if err := ss.TryReset(); err != nil {
			t.Fatal(err)
		}
		if ss.Connection != nil || ss.maxWriteMsgLen != 0 {
			t.Fatal("Reset did not clear session state")
		}
	}
	// Reset keeps the source-compatible signature and blocks outside callbacks.
	ss.SetMaxWriteMsgLen(123)
	ss.Reset()
	if ss.Connection != nil || ss.maxWriteMsgLen != 0 {
		t.Fatal("Reset did not clear session state")
	}
}

// TestResetAcrossSessions checks that only the target session's callbacks
// determine whether Reset waits, even when invoked from another session's callback.
func TestResetAcrossSessions(t *testing.T) {
	for _, active := range []bool{false, true} {
		name := "target idle"
		if active {
			name = "target callback active"
		}
		t.Run(name, func(t *testing.T) {
			conn := &resetCleanupConn{gettyTCPConn: newGettyTCPConn(&eofDataNetConn{}), entered: make(chan struct{}), release: make(chan struct{})}
			b := newSession(newServer(TCP_SERVER), conn)
			var releaseOnce sync.Once
			releaseCleanup := func() { releaseOnce.Do(func() { close(conn.release) }) }
			defer releaseCleanup()
			listener := &blockingCronEventListener{entered: make(chan struct{}), release: make(chan struct{})}
			var callbackOnce sync.Once
			releaseCallback := func() { callbackOnce.Do(func() { close(listener.release) }) }
			defer releaseCallback()
			bDone := make(chan struct{})
			if active {
				b.SetEventListener(listener)
				ctx := &heartbeatContext{session: b, lifecycle: b.lifecycle}
				go func() {
					defer close(bDone)
					_ = heartbeat(0, time.Time{}, ctx)
				}()
				waitForReset(t, listener.entered)
			}
			a := newTCPSession(&eofDataNetConn{}, newServer(TCP_SERVER)).(*session)
			returned := make(chan struct{})
			a.SetEventListener(&resetContractListener{target: "cron", hook: func(Session) { b.Reset() }})
			ctx := &heartbeatContext{session: a, lifecycle: a.lifecycle}
			go func() {
				defer close(returned)
				_ = heartbeat(0, time.Time{}, ctx)
			}()
			waitForReset(t, conn.entered)
			result := b.ResetAsync() // Join the cleanup held open by conn.release.
			if active {
				waitForReset(t, returned)
			} else {
				select {
				case <-returned:
					t.Fatal("A's callback made idle B.Reset return before cleanup")
				case <-time.After(30 * time.Millisecond):
				}
			}
			releaseCleanup()
			if active {
				select {
				case <-result:
					t.Fatal("B reset completed before B's callback returned")
				case <-time.After(30 * time.Millisecond):
				}
				releaseCallback()
				waitForReset(t, bDone)
			}
			waitForReset(t, result)
			waitForReset(t, returned)
			if b.Connection != nil || b.listener != nil {
				t.Fatal("B reset completed before clearing state")
			}
			waitForReset(t, a.ResetAsync())
		})
	}
}

// callResetDeep keeps real stack frames between a callback and Reset.
//
//go:noinline
func callResetDeep(s *session, depth int) int {
	if depth == 0 {
		s.Reset()
		return 0
	}
	return 1 + callResetDeep(s, depth-1)
}

// TestResetDeepCallback checks that reset does not depend on stack depth.
func TestResetDeepCallback(t *testing.T) {
	ss := newTCPSession(&eofDataNetConn{}, newServer(TCP_SERVER)).(*session)
	result := make(chan (<-chan error), 1)
	ss.SetEventListener(&resetContractListener{target: "cron", hook: func(Session) {
		callResetDeep(ss, 128)
		result <- ss.ResetAsync()
	}})
	ctx := &heartbeatContext{session: ss, lifecycle: ss.lifecycle}
	go func() { _ = heartbeat(0, time.Time{}, ctx) }()
	select {
	case done := <-result:
		waitForReset(t, done)
	case <-time.After(3 * time.Second):
		t.Fatal("Reset blocked inside a deep callback")
	}
}

// TestCallbackDepthUnwinds verifies nested callbacks release the count on panic.
func TestCallbackDepthUnwinds(t *testing.T) {
	ss := newTCPSession(&eofDataNetConn{}, nil).(*session)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected callback panic")
			}
		}()
		ss.invokeCallback(func() {
			ss.invokeCallback(func() {
				if ss.callbackDepth.Load() != 2 {
					t.Error("nested callbacks not counted")
				}
				panic("callback panic")
			})
		})
	}()
	if ss.callbackDepth.Load() != 0 {
		t.Fatal("panic leaked callback count")
	}
	waitForReset(t, ss.ResetAsync())
}

// TestResetResultSurvivesRetry keeps a failed result unread until a later reset
// succeeds, proving results belong to requests rather than mutable session state.
func TestResetResultSurvivesRetry(t *testing.T) {
	ss := newTCPSession(&panicCloseNetConn{}, nil).(*session)
	first := ss.ResetAsync()
	deadline := time.After(3 * time.Second)
	for len(first) == 0 {
		select {
		case <-deadline:
			t.Fatal("first reset did not publish its result")
		case <-time.After(time.Millisecond):
		}
	}
	// The failed reset stopped the lifecycle but did not clear the connection.
	ss.lifecycle.wait()
	ss.lock.Lock()
	ss.Connection = newGettyTCPConn(&eofDataNetConn{})
	ss.lock.Unlock()
	waitForReset(t, ss.ResetAsync())
	if err := <-first; err == nil || !strings.Contains(err.Error(), "reset panic") {
		t.Fatalf("first reset result overwritten: %v", err)
	}
}
