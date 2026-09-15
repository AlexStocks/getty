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

// Package benchmark holds getty's benchmarks. They are a separate package on
// purpose: they exercise nothing but the public API, exactly like a user of the
// library, and the library's own unit tests stay free of measurement code.
//
// There are two kinds of measurement here, answering two different questions.
//
// # What does a code path cost
//
// The *_test.go files are ordinary Go benchmarks. Each case runs in this process
// against a loopback socket and reports ns/op, MB/s, allocs/op and the p50, p99
// and p999 latency of one operation, which is what you compare against the same
// benchmark on another revision:
//
//	make bench          # every case, -benchtime=1s
//	make bench-echo     # only the client/server echo cases
//	make bench-stable   # same as bench with -count=5, for numbers you quote
//
// Every case touches a real socket, so one run is noisy - a difference under a
// few percent means nothing until it survives repetition. Compare runs with
// benchstat rather than by eye:
//
//	make bench-stable > old.txt
//	git switch my-change
//	make bench-stable > new.txt
//	benchstat old.txt new.txt
//
// # How much traffic can getty serve
//
// ./server is a standalone getty process, meant to be driven by an external load
// generator so that the client shares neither the server's scheduler nor its
// heap. tcpkali is the generator the sibling apache/dubbo-getty benchmark uses:
//
//	make bench-server                 # terminal 1
//	tcpkali --workers 4 -c 100 -T 30s -e -m "PING\n" 127.0.0.1:12345   # terminal 2
//
// -mode sink consumes without framing and reports receive throughput; -mode echo
// frames on '\n' and writes each message back. tcpkali can also report latency
// percentiles for the echo mode, since the echoed message is a usable marker:
//
//	tcpkali --workers 4 -c 10 -T 30s -e -m "PING\n" --latency-marker PING \
//		--latency-percentiles 50,99,99.9 127.0.0.1:12345
//
// Run ./server -h for the rest of the flags (compression, log level, message
// size limit).
//
// Neither kind of measurement is part of the CI gate: their absolute numbers
// move with the machine, and a benchmark that fails on a slow runner teaches
// nothing.
package benchmark
