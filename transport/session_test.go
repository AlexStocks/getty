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
	"io"
	"net"
	"testing"
	"time"
)

var errTestReadFailure = errors.New("test read failure")

type errorReader struct{}

func (errorReader) Read(Session, []byte) (any, int, error) {
	return nil, 0, errTestReadFailure
}

type timeoutTestWriter struct{}

func (timeoutTestWriter) Write(Session, any) ([]byte, error) {
	return []byte("x"), nil
}

type timeoutTestCall struct {
	observed time.Duration
	release  chan struct{}
}

type timeoutTestNetConn struct {
	owner   *gettyTCPConn
	entered chan *timeoutTestCall
}

func (c *timeoutTestNetConn) Write(p []byte) (int, error) {
	call := &timeoutTestCall{
		observed: c.owner.WriteTimeout(),
		release:  make(chan struct{}),
	}
	c.entered <- call
	<-call.release
	return len(p), nil
}

func (*timeoutTestNetConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (*timeoutTestNetConn) Close() error                     { return nil }
func (*timeoutTestNetConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*timeoutTestNetConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*timeoutTestNetConn) SetDeadline(time.Time) error      { return nil }
func (*timeoutTestNetConn) SetReadDeadline(time.Time) error  { return nil }
func (*timeoutTestNetConn) SetWriteDeadline(time.Time) error { return nil }

func TestConcurrentWritePkgTimeoutRestoration(t *testing.T) {
	netConn := &timeoutTestNetConn{entered: make(chan *timeoutTestCall, 2)}
	ss := newTCPSession(netConn, nil).(*session)
	ss.writer = timeoutTestWriter{}
	conn := ss.Connection.(*gettyTCPConn)
	netConn.owner = conn
	initialTimeout := conn.WriteTimeout()

	firstDone := make(chan error, 1)
	go func() {
		_, _, err := ss.WritePkg("first", 3*time.Second)
		firstDone <- err
	}()
	firstCall := <-netConn.entered

	secondDone := make(chan error, 1)
	go func() {
		_, _, err := ss.WritePkg("second", 5*time.Second)
		secondDone <- err
	}()

	select {
	case secondCall := <-netConn.entered:
		close(firstCall.release)
		<-firstDone
		close(secondCall.release)
		<-secondDone
		t.Fatal("second timed write entered while the first still owned the shared write timeout")
	case <-time.After(50 * time.Millisecond):
	}

	if firstCall.observed != 3*time.Second {
		t.Fatalf("first write observed timeout %v, want %v", firstCall.observed, 3*time.Second)
	}
	close(firstCall.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	var secondCall *timeoutTestCall
	select {
	case secondCall = <-netConn.entered:
	case <-time.After(time.Second):
		t.Fatal("second timed write did not enter after the first completed")
	}
	if secondCall.observed != 5*time.Second {
		t.Fatalf("second write observed timeout %v, want %v", secondCall.observed, 5*time.Second)
	}
	close(secondCall.release)
	if err := <-secondDone; err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	if got := conn.WriteTimeout(); got != initialTimeout {
		t.Fatalf("write timeout after concurrent calls = %v, want %v", got, initialTimeout)
	}
}

func TestHandlePackageWithNilListenerDoesNotPanicOnError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = listener.Close()
	}()

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = clientConn.Close()
	}()

	var serverConn net.Conn
	select {
	case err := <-acceptErr:
		t.Fatal(err)
	case serverConn = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for server connection")
	}

	ss := newTCPSession(serverConn, newServer(TCP_SERVER)).(*session)
	ss.SetReader(errorReader{})
	ss.SetWaitTime(time.Second)
	ss.grNum.Add(1)

	if _, err := clientConn.Write([]byte("trigger read error")); err != nil {
		t.Fatal(err)
	}

	ss.handlePackage()
}
