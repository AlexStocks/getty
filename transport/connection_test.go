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
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

import (
	"github.com/golang/snappy"
)

type blockingSnappyWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (w *blockingSnappyWriter) Write(p []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	return len(p), nil
}

type timeoutAccessorNetConn struct{}

func (*timeoutAccessorNetConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (*timeoutAccessorNetConn) Write(p []byte) (int, error)      { return len(p), nil }
func (*timeoutAccessorNetConn) Close() error                     { return nil }
func (*timeoutAccessorNetConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*timeoutAccessorNetConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*timeoutAccessorNetConn) SetDeadline(time.Time) error      { return nil }
func (*timeoutAccessorNetConn) SetReadDeadline(time.Time) error  { return nil }
func (*timeoutAccessorNetConn) SetWriteDeadline(time.Time) error { return nil }

func TestConnectionTimeoutAccessorsDoNotCopyAtomicState(t *testing.T) {
	conn := newGettyTCPConn(&timeoutAccessorNetConn{})
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 10000; i++ {
			conn.rLastDeadline.Store(time.Unix(0, int64(i)))
			conn.wLastDeadline.Store(time.Unix(0, int64(i)))
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 10000; i++ {
			_ = conn.ReadTimeout()
			_ = conn.WriteTimeout()
		}
	}()

	close(start)
	wg.Wait()
}

func TestSnappyWriteFlusherCloseWaitsForWrite(t *testing.T) {
	underlying := &blockingSnappyWriter{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	defer func() {
		select {
		case <-underlying.release:
		default:
			close(underlying.release)
		}
	}()

	writer := newSnappyWriteFlusher(snappy.NewBufferedWriter(underlying))
	writeDone := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("payload"))
		writeDone <- err
	}()

	select {
	case <-underlying.entered:
	case <-time.After(time.Second):
		t.Fatal("snappy write did not reach the underlying writer")
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- writer.Close()
	}()

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before the active Write completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(underlying.release)
	if err := <-writeDone; err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}
