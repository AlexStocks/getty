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

// Command server is the getty side of the load-generator benchmark: a
// standalone process so the client that drives it runs outside this binary,
// with its own scheduler and heap. This is the arrangement that answers "how
// much traffic can getty serve", as opposed to the in-process benchmarks in
// ../*.go, which answer "what does this code path cost".
//
// Run it in one terminal and a load generator such as tcpkali in another:
//
//	go run ./benchmark/server -addr 127.0.0.1:12345 -mode echo
//	tcpkali --workers 4 -c 100 -T 30s -e -m "PING\n" 127.0.0.1:12345
//
// -mode sink consumes everything without framing and reports receive
// throughput; -mode echo frames on '\n' (tcpkali's message separator) and
// writes each message back.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

import (
	getty "github.com/AlexStocks/getty/transport"
	gettylog "github.com/AlexStocks/getty/util"
)

var (
	addr        = flag.String("addr", "127.0.0.1:12345", "listen address")
	mode        = flag.String("mode", "echo", "echo (frame on \\n and write back) or sink (consume and drop)")
	compress    = flag.String("compress", "", "zip or snappy; empty leaves the connection raw")
	maxMsgLen   = flag.Int("max_msg_len", 1<<20, "session max message length")
	statsEvery  = flag.Duration("stats", 5*time.Second, "how often to print counters, 0 disables")
	readTimeout = flag.Duration("read_timeout", time.Minute, "session read timeout")

	// logLevel defaults to error on purpose: getty logs every write at debug
	// level, which costs more than the write itself for small messages. Run
	// with -log_level debug to see that for yourself.
	logLevel = flag.String("log_level", "error", "debug|info|warn|error|fatal")
)

type counters struct {
	msgs  atomic.Int64
	bytes atomic.Int64
}

// lineCodec frames on '\n' and keeps a copy of the message, so it stays valid
// after the session reuses its read buffer.
type lineCodec struct{ c *counters }

func (l lineCodec) Read(_ getty.Session, data []byte) (any, int, error) {
	idx := bytes.IndexByte(data, '\n')
	if idx < 0 {
		return nil, 0, nil
	}
	line := make([]byte, idx+1)
	copy(line, data[:idx+1])
	l.c.msgs.Add(1)
	l.c.bytes.Add(int64(idx + 1))
	return line, idx + 1, nil
}

func (lineCodec) Write(_ getty.Session, pkg any) ([]byte, error) {
	body, ok := pkg.([]byte)
	if !ok {
		return nil, fmt.Errorf("lineCodec: unexpected pkg type %T", pkg)
	}
	return body, nil
}

// sinkPkg is a non-nil placeholder: handleTCPPackage stops consuming the read
// buffer as soon as a codec returns a nil pkg ("need more data"), so a sink
// codec that returned nil would let the buffer grow until the session hit
// maxMsgLen and closed.
var sinkPkg = struct{}{}

// sinkCodec consumes whatever arrived and delivers nothing: it measures the
// read path with no framing or echo work in the way.
type sinkCodec struct{ c *counters }

func (s sinkCodec) Read(_ getty.Session, data []byte) (any, int, error) {
	s.c.bytes.Add(int64(len(data)))
	return sinkPkg, len(data), nil
}

func (s sinkCodec) Write(_ getty.Session, pkg any) ([]byte, error) {
	body, ok := pkg.([]byte)
	if !ok {
		return nil, fmt.Errorf("sinkCodec: unexpected pkg type %T", pkg)
	}
	return body, nil
}

type echoListener struct{}

func (echoListener) OnOpen(getty.Session) error   { return nil }
func (echoListener) OnClose(getty.Session)        {}
func (echoListener) OnError(getty.Session, error) {}
func (echoListener) OnCron(getty.Session)         {}
func (echoListener) OnMessage(ss getty.Session, pkg any) {
	_, _, _ = ss.WritePkg(pkg, 0)
}

type sinkListener struct{}

func (sinkListener) OnOpen(getty.Session) error   { return nil }
func (sinkListener) OnClose(getty.Session)        {}
func (sinkListener) OnError(getty.Session, error) {}
func (sinkListener) OnCron(getty.Session)         {}
func (sinkListener) OnMessage(getty.Session, any) {}

func main() {
	flag.Parse()

	if err := setLogLevel(*logLevel); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	// Validate the flags the way -mode and -log_level are validated. A bad
	// -compress used to be rejected per connection inside the session callback,
	// whose error is only logged at warn level - below this program's default
	// -log_level error - so the server accepted and dropped every connection and
	// the cause never appeared anywhere.
	var compressType getty.CompressType
	switch *compress {
	case "":
		compressType = getty.CompressNone
	case "zip":
		compressType = getty.CompressZip
	case "snappy":
		compressType = getty.CompressSnappy
	default:
		fmt.Fprintf(os.Stderr, "unknown -compress %q, want zip or snappy\n", *compress)
		os.Exit(2)
	}

	c := &counters{}
	var (
		handler  getty.ReadWriter
		listener getty.EventListener
	)
	switch *mode {
	case "echo":
		handler, listener = lineCodec{c}, echoListener{}
	case "sink":
		handler, listener = sinkCodec{c}, sinkListener{}
	default:
		fmt.Fprintf(os.Stderr, "unknown -mode %q, want echo or sink\n", *mode)
		os.Exit(2)
	}

	srv := getty.NewTCPServer(getty.WithLocalAddress(*addr))
	srv.RunEventLoop(func(ss getty.Session) error {
		ss.SetPkgHandler(handler)
		ss.SetEventListener(listener)
		ss.SetMaxMsgLen(*maxMsgLen)
		ss.SetReadTimeout(*readTimeout)
		ss.SetWriteTimeout(time.Minute)
		if compressType != getty.CompressNone {
			ss.SetCompressType(compressType)
		}
		return nil
	})

	fmt.Printf("getty server listening on %s mode=%s compress=%q\n",
		srv.(getty.StreamServer).Listener().Addr().String(), *mode, *compress)
	_ = os.Stdout.Sync()

	if *statsEvery > 0 {
		go reportStats(c, *statsEvery)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	srv.Close()
}

func setLogLevel(level string) error {
	levels := map[string]gettylog.LoggerLevel{
		"debug": gettylog.LoggerLevelDebug,
		"info":  gettylog.LoggerLevelInfo,
		"warn":  gettylog.LoggerLevelWarn,
		"error": gettylog.LoggerLevelError,
		"fatal": gettylog.LoggerLevelFatal,
	}
	l, ok := levels[level]
	if !ok {
		return fmt.Errorf("unknown -log_level %q", level)
	}
	if err := gettylog.SetLoggerLevel(l); err != nil {
		return fmt.Errorf("SetLoggerLevel(%s): %w", level, err)
	}
	return nil
}

func reportStats(c *counters, every time.Duration) {
	var lastBytes, lastMsgs int64
	last := time.Now()
	for range time.Tick(every) {
		now := time.Now()
		elapsed := now.Sub(last).Seconds()
		bytes, msgs := c.bytes.Load(), c.msgs.Load()
		fmt.Printf("[%s] recv %.2f Mbit/s (%.2f MB/s) msgs/s=%.0f total=%.1f MB\n",
			now.Format("15:04:05"),
			float64(bytes-lastBytes)*8/elapsed/1e6,
			float64(bytes-lastBytes)/elapsed/1e6,
			float64(msgs-lastMsgs)/elapsed,
			float64(bytes)/1e6)
		_ = os.Stdout.Sync()
		lastBytes, lastMsgs, last = bytes, msgs, now
	}
}
