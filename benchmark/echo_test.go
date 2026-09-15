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
	"fmt"
	"testing"
	"time"
)

import (
	getty "github.com/AlexStocks/getty/transport"
)

// These benchmarks run a real getty client/server echo pair over loopback, so a
// single ns/op mixes getty with the kernel and the scheduler. Use them to
// compare configurations and revisions measured in the same run, never as an
// absolute throughput number.
//
// Starting the pair is expensive, so every case stops the timer for setup and
// restarts it with b.StartTimer() before the measured loop.
// BenchmarkEchoPingPong is one request in flight at a time: ns/op is the full
// round trip (client codec -> conn -> server codec -> echo -> client codec).
func BenchmarkEchoPingPong(b *testing.B) {
	for _, transport := range []string{"tcp", "ws"} {
		for _, size := range echoSizes(transport) {
			b.Run(transport+"/"+sizeName(size), func(b *testing.B) {
				b.StopTimer()
				e := newBenchEcho(b, transport, lengthPrefixedCodec{}, getty.CompressNone, 1)
				ss := e.sessions[0]
				payload := benchPayload(size)
				latency := newLatencyRecorder(b.N)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.StartTimer()

				for i := 0; i < b.N; i++ {
					start := time.Now()
					if _, _, err := ss.WritePkg(payload, 0); err != nil {
						b.Fatal(err)
					}
					e.waitReply(b)
					latency.record(time.Since(start))
				}
				latency.report(b)
			})
		}
	}
}

// BenchmarkEchoPipeline keeps `inflight` requests in flight, which is what a
// real client does; MB/s becomes meaningful as the round trip stops being the
// limit.
func BenchmarkEchoPipeline(b *testing.B) {
	for _, inflight := range []int{1, 8, 64} {
		for _, size := range []int{1 << 10, 16 << 10, 64 << 10} {
			b.Run(fmt.Sprintf("inflight=%d/%s", inflight, sizeName(size)), func(b *testing.B) {
				b.StopTimer()
				e := newBenchEcho(b, "tcp", lengthPrefixedCodec{}, getty.CompressNone, 1)
				ss := e.sessions[0]
				payload := benchPayload(size)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.StartTimer()

				sent, recv := 0, 0
				for recv < b.N {
					for sent < b.N && sent-recv < inflight {
						if _, _, err := ss.WritePkg(payload, 0); err != nil {
							b.Fatal(err)
						}
						sent++
					}
					e.waitReply(b)
					recv++
				}
			})
		}
	}
}

// BenchmarkEchoCodec compares the minimal binary framing with the same framing
// plus an encoding/json pass, so the codec cost is visible next to the
// framework cost.
func BenchmarkEchoCodec(b *testing.B) {
	for _, tc := range []struct {
		name  string
		codec getty.ReadWriter
		pkg   func([]byte) any
	}{
		{"binary", lengthPrefixedCodec{}, func(p []byte) any { return p }},
		{"json", jsonCodec{}, func(p []byte) any { return &jsonPkg{Body: p} }},
	} {
		for _, size := range []int{1 << 10, 16 << 10} {
			b.Run(tc.name+"/"+sizeName(size), func(b *testing.B) {
				b.StopTimer()
				e := newBenchEcho(b, "tcp", tc.codec, getty.CompressNone, 1)
				ss := e.sessions[0]
				pkg := tc.pkg(benchPayload(size))
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.StartTimer()

				for i := 0; i < b.N; i++ {
					if _, _, err := ss.WritePkg(pkg, 0); err != nil {
						b.Fatal(err)
					}
					e.waitReply(b)
				}
			})
		}
	}
}

// BenchmarkEchoCompression runs the round trip through the connection codec.
func BenchmarkEchoCompression(b *testing.B) {
	for _, tc := range []struct {
		name     string
		compress getty.CompressType
	}{
		{"raw", getty.CompressNone},
		{"zip", getty.CompressZip},
		{"snappy", getty.CompressSnappy},
	} {
		for _, size := range []int{1 << 10, 16 << 10} {
			b.Run(tc.name+"/"+sizeName(size), func(b *testing.B) {
				b.StopTimer()
				e := newBenchEcho(b, "tcp", lengthPrefixedCodec{}, tc.compress, 1)
				ss := e.sessions[0]
				payload := benchPayload(size)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.StartTimer()

				for i := 0; i < b.N; i++ {
					if _, _, err := ss.WritePkg(payload, 0); err != nil {
						b.Fatal(err)
					}
					e.waitReply(b)
				}
			})
		}
	}
}

// BenchmarkEchoUDP is the datagram round trip: one connected udp client against
// a packet endpoint that writes each datagram back. Sizes stay under the
// smallest common datagram limit (macOS caps one at 9216 bytes by default).
func BenchmarkEchoUDP(b *testing.B) {
	for _, size := range []int{64, 1 << 10, 4 << 10} {
		b.Run(sizeName(size), func(b *testing.B) {
			b.StopTimer()
			e := newBenchEcho(b, "udp", udpEchoCodec{}, getty.CompressNone, 1)
			ss := e.sessions[0]
			payload := benchPayload(size)
			latency := newLatencyRecorder(b.N)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.StartTimer()

			// udp may lose a datagram, so a missing reply is a bounded retry, not
			// a 30 second wait: the loss is counted and reported instead of
			// turning the whole suite red.
			ctx := getty.UDPContext{Pkg: payload}
			var lost int64
			for i := 0; i < b.N; i++ {
				start := time.Now()
				if _, _, err := ss.WritePkg(ctx, 0); err != nil {
					b.Fatal(err)
				}
				for attempt := 1; !e.waitReplyWithin(udpReplyTimeout); attempt++ {
					lost++
					if attempt > udpMaxRetries {
						b.Fatalf("udp echo lost %d consecutive replies (server received %d datagrams, client received %d)",
							attempt, e.serverRecv.Load(), e.clientRecv.Load())
					}
					if _, _, err := ss.WritePkg(ctx, 0); err != nil {
						b.Fatal(err)
					}
				}
				latency.record(time.Since(start))
			}
			if lost > 0 {
				b.ReportMetric(float64(lost), "lost")
			}
			latency.report(b)
		})
	}
}

// BenchmarkEchoConnections spreads one request at a time over a pool of conns
// sessions, exposing the per-connection fixed cost of the pool.
func BenchmarkEchoConnections(b *testing.B) {
	const size = 1 << 10
	for _, conns := range []int{1, 8, 64} {
		b.Run(fmt.Sprintf("conns=%d", conns), func(b *testing.B) {
			b.StopTimer()
			e := newBenchEcho(b, "tcp", lengthPrefixedCodec{}, getty.CompressNone, conns)
			payload := benchPayload(size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.StartTimer()

			for i := 0; i < b.N; i++ {
				if _, _, err := e.sessions[i%conns].WritePkg(payload, 0); err != nil {
					b.Fatal(err)
				}
				e.waitReply(b)
			}
		})
	}
}
