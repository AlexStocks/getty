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

package benchmark

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

import (
	getty "github.com/AlexStocks/getty/transport"
)

// The benchmarks in this file measure the write path of one getty session
// against a peer that throws the bytes away. The cost of a real socket write is
// part of every number - that is the point: they measure what a caller of the
// public API actually pays. Compare them against each other and against the
// same benchmark on another revision, not against another machine.
// BenchmarkSessionWriteBytes is the entry point codecs use for the bulk path.
// Sizes on both sides of maxPacketLen are covered because the lock and the
// number of socket writes change exactly at that boundary.
func BenchmarkSessionWriteBytes(b *testing.B) {
	for _, size := range sizes {
		b.Run(sizeName(size), func(b *testing.B) {
			b.StopTimer()
			ss := newWriteSession(b, nopCodec{}, getty.CompressNone)
			payload := benchPayload(size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.StartTimer()

			for i := 0; i < b.N; i++ {
				if _, err := ss.WriteBytes(payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSessionWritePkg adds the codec call and, for a positive timeout, the
// packetLock write lock plus the connection-level write deadline save/restore.
func BenchmarkSessionWritePkg(b *testing.B) {
	for _, timeout := range []time.Duration{0, time.Second} {
		for _, size := range sizes {
			b.Run(fmt.Sprintf("%s/timeout=%s", sizeName(size), timeout), func(b *testing.B) {
				b.StopTimer()
				ss := newWriteSession(b, nopCodec{}, getty.CompressNone)
				payload := benchPayload(size)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.StartTimer()

				for i := 0; i < b.N; i++ {
					if _, _, err := ss.WritePkg(payload, timeout); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkSessionSend is the raw public write entry point, kept next to
// WriteBytes so the cost of the two paths can be compared directly.
func BenchmarkSessionSend(b *testing.B) {
	for _, size := range sizes {
		b.Run(sizeName(size), func(b *testing.B) {
			b.StopTimer()
			ss := newWriteSession(b, nopCodec{}, getty.CompressNone)
			payload := benchPayload(size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.StartTimer()

			for i := 0; i < b.N; i++ {
				if _, err := ss.Send(payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSessionWriteParallel drives the same session from GOMAXPROCS
// goroutines. Below maxPacketLen all writers hold the packetLock read lock and
// run concurrently; above it the fragmenting writer takes the write lock and
// serializes them, which is the contention the packetLock exists for.
func BenchmarkSessionWriteParallel(b *testing.B) {
	for _, size := range sizes {
		b.Run(sizeName(size), func(b *testing.B) {
			b.StopTimer()
			ss := newWriteSession(b, nopCodec{}, getty.CompressNone)
			payload := benchPayload(size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.StartTimer()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := ss.WriteBytes(payload); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// BenchmarkSessionWriteBytesArray covers the batched path. For TCP it goes to
// conn.Send([][]byte) under one packetLock read lock, so the whole array is
// written before any other writer can interleave.
//
// The batch is rebuilt from one backing array on every iteration on purpose:
// WriteBytesArray hands the caller's slice to net.Buffers, whose WriteTo
// consumes it ("the buffers are consumed as they are written"), truncating the
// caller's own []byte headers in place. A caller that reuses a batch therefore
// writes zero bytes from the second call on, with a nil error. Re-slicing keeps
// this benchmark measuring getty instead of that footgun, without copying any
// payload.
func BenchmarkSessionWriteBytesArray(b *testing.B) {
	const pkgSize = 1 << 10
	for _, n := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("%dpkg", n), func(b *testing.B) {
			b.StopTimer()
			ss := newWriteSession(b, nopCodec{}, getty.CompressNone)
			backing := benchPayload(n * pkgSize)
			pkgs := make([][]byte, n)
			b.SetBytes(int64(n * pkgSize))
			b.ReportAllocs()
			b.StartTimer()

			for i := 0; i < b.N; i++ {
				for j := range pkgs {
					pkgs[j] = backing[j*pkgSize : (j+1)*pkgSize]
				}
				if _, err := ss.WriteBytesArray(pkgs...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSessionCompression isolates the connection-level codec: each write
// flushes one compressed frame. "raw" installs no codec at all, which is
// getty's default.
func BenchmarkSessionCompression(b *testing.B) {
	for _, tc := range []struct {
		name     string
		compress getty.CompressType
	}{
		{"raw", getty.CompressNone},
		{"zip", getty.CompressZip},
		{"bestspeed", getty.CompressBestSpeed},
		{"bestcompression", getty.CompressBestCompression},
		{"huffman", getty.CompressHuffman},
		{"snappy", getty.CompressSnappy},
	} {
		for _, size := range []int{1 << 10, 64 << 10} {
			b.Run(tc.name+"/"+sizeName(size), func(b *testing.B) {
				b.StopTimer()
				ss := newWriteSession(b, nopCodec{}, tc.compress)
				payload := benchPayload(size)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.StartTimer()

				for i := 0; i < b.N; i++ {
					if _, err := ss.WriteBytes(payload); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkSessionAccessors covers the session methods that take the session
// lock on every call, so a change in lock scope on them is visible. They do not
// touch the socket, but they need a live session to run against.
func BenchmarkSessionAccessors(b *testing.B) {
	b.StopTimer()
	ss := newWriteSession(b, nopCodec{}, getty.CompressNone)

	// an ordered slice, not a map: these sub-benchmarks share one session, so
	// whichever runs first pays its cold start, and a map would reshuffle that
	// cost between runs of what is meant to be a comparable measurement
	benches := []struct {
		name string
		fn   func()
	}{
		{"IsClosed", func() { _ = ss.IsClosed() }},
		{"Stat", func() { _ = ss.Stat() }},
		{"EndPoint", func() { _ = ss.EndPoint() }},
		{"WriteTimeout", func() { _ = ss.WriteTimeout() }},
		{"GetAttribute", func() { _ = ss.GetAttribute("missing") }},
	}
	for _, bc := range benches {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				bc.fn()
			}
		})
	}
}

// BenchmarkSessionLifecycle measures the fixed cost of taking on a connection:
// dial, accept, session callback, and teardown. The client side is a bare
// socket so the number is the server's session lifecycle plus the handshake.
func BenchmarkSessionLifecycle(b *testing.B) {
	b.StopTimer()
	silenceLogs()

	srv := getty.NewTCPServer(getty.WithLocalAddress("127.0.0.1:0"))
	accepted := make(chan struct{}, 64)
	srv.RunEventLoop(func(ss getty.Session) error {
		ss.SetPkgHandler(nopCodec{})
		ss.SetEventListener(discardListener{})
		accepted <- struct{}{}
		return nil
	})
	addr := srv.(getty.StreamServer).Listener().Addr().String()
	b.Cleanup(srv.Close)
	// A bare net.Dial has no timeout, so a stalled connect would be charged to
	// the measured loop.
	dialer := &net.Dialer{Timeout: dialTimeout}
	ctx := context.Background()
	b.ReportAllocs()
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			b.Fatalf("dial %s: %v", addr, err)
		}
		<-accepted
		// Reset rather than FIN: a benchmark that dials in a tight loop would
		// otherwise fill the ephemeral port range with TIME_WAIT sockets and
		// fail with "can't assign requested address" long before benchtime.
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		if err := conn.Close(); err != nil {
			b.Fatalf("conn.Close: %v", err)
		}
	}
}

// newUDPWriteSession returns a getty UDP client session pointed at a socket
// that drains, so the datagram path can be measured with the syscall included.
func newUDPWriteSession(b *testing.B) getty.Session {
	b.Helper()
	silenceLogs()

	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		b.Skipf("loopback udp unavailable: %v", err)
	}
	go func() {
		buf := make([]byte, 64<<10)
		for {
			if _, _, err := peer.ReadFromUDP(buf); err != nil {
				return
			}
		}
	}()

	ready := make(chan getty.Session, 1)
	cli := getty.NewUDPClient(
		getty.WithServerAddress(peer.LocalAddr().String()),
		getty.WithConnectionNumber(1),
	)
	go cli.RunEventLoop(func(ss getty.Session) error {
		ss.SetPkgHandler(udpCodec{})
		ss.SetEventListener(discardListener{})
		ss.SetReadTimeout(time.Minute)
		ss.SetWriteTimeout(time.Minute)
		ready <- ss
		return nil
	})
	b.Cleanup(func() {
		cli.Close()
		_ = peer.Close()
	})

	select {
	case ss := <-ready:
		return ss
	case <-time.After(dialTimeout):
		b.Fatal("udp client session did not come up")
		return nil
	}
}

// BenchmarkUDPSend measures the datagram path. Sizes stay under the smallest
// common datagram limit (macOS caps a UDP datagram at 9216 bytes by default),
// so the write is always measurable here.
func BenchmarkUDPSend(b *testing.B) {
	b.StopTimer()
	ss := newUDPWriteSession(b)

	for _, size := range []int{64, 1 << 10, 4 << 10} {
		b.Run(sizeName(size), func(b *testing.B) {
			b.StopTimer()
			ctx := getty.UDPContext{Pkg: benchPayload(size)}
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.StartTimer()

			for i := 0; i < b.N; i++ {
				if _, _, err := ss.WritePkg(ctx, 0); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
