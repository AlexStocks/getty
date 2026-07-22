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
	"net"
	"testing"
	"time"
)

var errTestReadFailure = errors.New("test read failure")

type errorReader struct{}

func (errorReader) Read(Session, []byte) (any, int, error) {
	return nil, 0, errTestReadFailure
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
