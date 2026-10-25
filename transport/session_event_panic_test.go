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
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

import (
	gxsync "github.com/dubbogo/gost/sync"
)

const callbackPanicEnv = "GETTY_TEST_EVENT_PANIC"

// A parent-goroutine recover cannot catch a panic escaping Getty's read loop
// or shared timer wheel. Each regression therefore runs in its own process.
func TestEventCallbackPanicContained(t *testing.T) {
	for _, stage := range []string{"close", "error", "error-close", "cron", "pooled-cron"} {
		t.Run(stage, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestEventCallbackPanicChild$", "-test.timeout=15s")
			cmd.Env = append(os.Environ(), callbackPanicEnv+"="+stage)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("callback panic regression failed: %v\n%s", err, output)
			}
			callbacks := []string{"OnClose"}
			switch stage {
			case "error":
				callbacks = []string{"OnError"}
			case "error-close":
				callbacks = []string{"OnError", "OnClose"}
			case "cron", "pooled-cron":
				callbacks = []string{"OnCron"}
			}
			for _, callback := range callbacks {
				for _, want := range []string{"[session." + callback + "] panic session", "test " + callback + " panic", "session_event_panic_test.go:"} {
					if !strings.Contains(string(output), want) {
						t.Errorf("missing panic context or stack %q in output:\n%s", want, output)
					}
				}
			}
		})
	}
}

// eventPanicConn records transport closure independently of Session.Reset.
type eventPanicConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (c *eventPanicConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { close(c.closed) })
	return err
}

type eventPanicReadConn struct{ eofDataNetConn }

func (*eventPanicReadConn) Read([]byte) (int, error) { return 0, errTestReadFailure }

type eventPanicListener struct {
	recordingEventListener
	stage  string
	events chan string
}

func (l *eventPanicListener) OnError(Session, error) {
	l.events <- "error"
	if strings.HasPrefix(l.stage, "error") {
		panic("test OnError panic")
	}
}

func (l *eventPanicListener) OnClose(Session) {
	l.events <- "close"
	if l.stage == "close" || l.stage == "error-close" {
		panic("test OnClose panic")
	}
}

func (l *eventPanicListener) OnCron(Session) {
	l.events <- "cron"
	panic("test OnCron panic")
}

type eventPanicHealthyListener struct {
	recordingEventListener
	ticks    chan time.Time
	messages chan any
}

func (l *eventPanicHealthyListener) OnCron(Session) {
	select {
	case l.ticks <- time.Now():
	default:
	}
}

func (l *eventPanicHealthyListener) OnMessage(_ Session, msg any) { l.messages <- msg }

func waitForEventPanic[T any](t *testing.T, ch <-chan T, name string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
		var zero T
		return zero
	}
}

// Reset remains external to callbacks; callback self-wait is tracked by #153.
func waitForEventPanicReset(t *testing.T, ss *session) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		ss.Reset()
		close(done)
	}()
	waitForEventPanic(t, done, "external reset")
}

func TestEventCallbackPanicChild(t *testing.T) {
	stage := os.Getenv(callbackPanicEnv)
	if stage == "" {
		t.Skip("subprocess only")
	}

	// A second session shares the actual default timer wheel. It must still
	// receive messages and fresh heartbeats after the faulty session is gone.
	healthyConn, peer := net.Pipe()
	defer func() { _ = peer.Close() }()
	healthy := newTCPSession(healthyConn, newServer(TCP_SERVER)).(*session)
	live := &eventPanicHealthyListener{ticks: make(chan time.Time, 8), messages: make(chan any, 1)}
	healthy.SetEventListener(live)
	healthy.SetReader(wholeFrameReader{})
	healthy.SetWriter(timeoutTestWriter{})
	healthy.SetCronPeriod(10)
	healthy.SetReadTimeout(time.Hour)
	healthy.run()
	defer func() {
		healthy.Close()
		_ = healthyConn.Close()
		waitForEventPanicReset(t, healthy)
	}()
	waitForEventPanic(t, live.ticks, "initial healthy heartbeat")

	endpoint := newServer(TCP_SERVER)
	var raw net.Conn = &eofDataNetConn{}
	switch stage {
	case "error", "error-close":
		raw = &eventPanicReadConn{}
	case "cron", "pooled-cron":
		local, remote := net.Pipe()
		defer func() { _ = remote.Close() }()
		raw = local
		if stage == "pooled-cron" {
			pool := gxsync.NewTaskPoolSimple(1)
			defer pool.Close()
			endpoint = newServer(TCP_SERVER, WithServerTaskPool(pool))
		}
	case "close":
	default:
		t.Fatalf("unknown subprocess mode %q", stage)
	}
	conn := &eventPanicConn{Conn: raw, closed: make(chan struct{})}
	ss := newTCPSession(conn, endpoint).(*session)
	lifecycle := ss.lifecycle
	listener := &eventPanicListener{stage: stage, events: make(chan string, 16)}
	ss.SetEventListener(listener)
	ss.SetReader(wholeFrameReader{})
	ss.SetWriter(timeoutTestWriter{})
	// Closing after a cron panic must release a blocked read immediately;
	// waiting for the normal read deadline would stall this test for an hour.
	ss.SetReadTimeout(time.Hour)
	if strings.Contains(stage, "cron") {
		ss.SetCronPeriod(10)
	}
	registeredClose := make(chan struct{})
	ss.AddCloseCallback("panic-test", "cleanup", func() { close(registeredClose) })
	ss.run()
	// Panic handling itself must close the socket, stop the read loop and
	// release lifecycle work before an external Reset is allowed to help.
	done := make(chan struct{})
	go func() {
		ss.grWG.Wait()
		lifecycle.wait()
		close(done)
	}()
	waitForEventPanic(t, done, "faulty session cleanup")
	if !ss.IsClosed() {
		t.Error("faulty session is still open")
	}
	waitForEventPanic(t, conn.closed, "transport close")
	waitForEventPanic(t, registeredClose, "registered close callback")
	if ss.Connection != nil || ss.heartbeatTimer != nil || ss.grNum.Load() != 0 {
		t.Fatal("panic leaked connection, timer or read goroutine")
	}
	if lifecycle.acquire() {
		lifecycle.release()
		t.Fatal("closed lifecycle still accepts work")
	}
	var events []string
	for len(listener.events) > 0 {
		events = append(events, <-listener.events)
	}
	want := "[close]"
	if strings.HasPrefix(stage, "error") {
		want = "[error close]"
	} else if strings.Contains(stage, "cron") {
		// Raw transport close may also report an error to the read loop.
		if len(events) == 3 {
			want = "[cron error close]"
		} else {
			want = "[cron close]"
		}
	}
	if fmt.Sprint(events) != want {
		t.Fatalf("callback order = %v, want %s", events, want)
	}
	waitForEventPanicReset(t, ss)
	if ss.listener != nil || ss.reader != nil || ss.writer != nil {
		t.Fatal("reset did not clear callback resources")
	}

	afterPanic := time.Now()
	if err := peer.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Write([]byte("still alive")); err != nil {
		t.Fatal(err)
	}
	if got := waitForEventPanic(t, live.messages, "healthy message"); got != "still alive" {
		t.Fatalf("healthy message = %v", got)
	}
	for !waitForEventPanic(t, live.ticks, "healthy heartbeat after panic").After(afterPanic) {
	}
	if healthy.IsClosed() {
		t.Fatal("callback panic closed another session")
	}
}
