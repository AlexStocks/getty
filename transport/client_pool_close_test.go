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
	"sync"
	"testing"
	"time"
)

func TestClientCloseDuringCallbackReset(t *testing.T) {
	builder := &countingTLSConfigBuilder{}
	c := newFailingReconnectClient(builder, time.Millisecond, 1)
	t.Cleanup(c.Close)

	requests := make(chan (<-chan error), 1)
	releaseCallback := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCallback) }) }
	t.Cleanup(release)

	s := newTCPSession(&eofDataNetConn{}, c).(*session)
	s.SetAttribute(sessionClientKey, c)
	s.SetAttribute(ignoreReconnectKey, false)
	s.SetPkgHandler(resetContractCodec{})
	s.SetEventListener(&resetContractListener{
		target: "close",
		hook: func(ss Session) {
			requests <- ss.(AsyncResetter).ResetAsync()
			<-releaseCallback
		},
	})
	c.ssMap[s] = struct{}{}
	s.run()

	var resetDone <-chan error
	select {
	case resetDone = <-requests:
	case <-time.After(3 * time.Second):
		t.Fatal("OnClose did not request reset")
	}

	// Close must return while OnClose still holds the reset worker open.
	closeDone := make(chan struct{})
	go func() {
		c.Close()
		close(closeDone)
	}()
	waitForReset(t, closeDone)
	c.Lock()
	poolGone := c.ssMap == nil
	c.Unlock()
	if !poolGone {
		t.Fatal("Close retained the client pool")
	}

	release()
	waitForReset(t, resetDone)
	c.Lock()
	poolGone = c.ssMap == nil
	c.Unlock()
	if !poolGone {
		t.Fatal("reset recreated the closed client pool")
	}

	runDone := make(chan struct{})
	go func() {
		c.RunEventLoop(func(Session) error { return nil })
		close(runDone)
	}()
	waitForReset(t, runDone)
	if got := builder.calls.Load(); got != 0 {
		t.Fatalf("closed client made %d dial attempts, want 0", got)
	}
}

func TestClientCloseBeforeConnectAdmission(t *testing.T) {
	c, _, callbackEntered, release, callbacks := newBlockingReconnectClient(t)
	connectDone := make(chan bool, 1)
	go func() { connectDone <- c.connect() }()
	waitForReset(t, callbackEntered)

	// The callback has a live candidate, but has not let connect admit it yet.
	closeDone := make(chan struct{})
	go func() {
		c.Close()
		close(closeDone)
	}()
	waitForReset(t, closeDone)
	release()
	select {
	case connected := <-connectDone:
		if connected {
			t.Fatal("connect admitted a session after Client.Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connect did not return after its callback was released")
	}
	c.Lock()
	poolGone := c.ssMap == nil
	c.Unlock()
	if !poolGone {
		t.Fatal("connect recreated the closed client pool")
	}
	if got := callbacks.Load(); got != 1 {
		t.Fatalf("NewSessionCallback ran %d times, want 1", got)
	}
}
