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
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
)

// tcpLimitTestConn supplies controlled read batches and can return data with EOF.
type tcpLimitTestConn struct {
	eofDataNetConn
	chunks      [][]byte
	dataWithEOF bool
}

// Read preserves batch boundaries while respecting the destination buffer size.
func (c *tcpLimitTestConn) Read(p []byte) (int, error) {
	c.reads++
	if len(c.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[0])
	c.chunks[0] = c.chunks[0][n:]
	if len(c.chunks[0]) == 0 {
		c.chunks = c.chunks[1:]
	}
	if c.dataWithEOF && len(c.chunks) == 0 {
		return n, io.EOF
	}
	return n, nil
}

// tcpLimitLineReader waits for a newline without knowing the final package size.
type tcpLimitLineReader struct {
	largest int
}

// Read returns complete lines, including their newline, or requests more data.
func (r *tcpLimitLineReader) Read(_ Session, data []byte) (any, int, error) {
	r.largest = max(r.largest, len(data))
	end := bytes.IndexByte(data, '\n')
	if end < 0 {
		return nil, 0, nil
	}
	return string(data[:end+1]), end + 1, nil
}

// TestTCPIncompleteReceiveLimit checks that unframed input stops growing before
// the stream ends, while complete messages in the same read remain deliverable.
func TestTCPIncompleteReceiveLimit(t *testing.T) {
	for _, test := range []struct {
		name        string
		limit       int
		chunks      []string
		dataWithEOF bool
		want        []any
		wantError   bool
		wantReads   int
	}{
		{
			name: "growing incomplete message", limit: 1024,
			chunks:    []string{strings.Repeat("a", 512), strings.Repeat("a", 512), "a", strings.Repeat("a", 64*1024)},
			wantError: true, wantReads: 3,
		},
		{
			name: "incomplete message above read buffer size", limit: 2 * maxReadBufLen,
			chunks:    []string{strings.Repeat("a", 64*1024)},
			wantError: true, wantReads: 3,
		},
		{
			name: "incomplete at limit", limit: 4, chunks: []string{"aaaa"},
			dataWithEOF: true, wantReads: 1,
		},
		{
			name: "incomplete above limit with EOF", limit: 4, chunks: []string{"aaaaa"},
			dataWithEOF: true, wantError: true, wantReads: 1,
		},
		{
			name: "complete at limit across reads", limit: 4, chunks: []string{"aaa", "\n"},
			dataWithEOF: true, want: []any{"aaa\n"}, wantReads: 2,
		},
		{
			name: "complete above limit", limit: 4, chunks: []string{"aaaa\n"},
			wantError: true, wantReads: 1,
		},
		{
			name: "multiple legal messages in one read", limit: 4, chunks: []string{"aaa\nbbb\nccc\n"},
			dataWithEOF: true, want: []any{"aaa\n", "bbb\n", "ccc\n"}, wantReads: 1,
		},
		{
			name: "legal batch then completed remainder", limit: 4, chunks: []string{"aaa\nbbb\ncc", "c\n"},
			dataWithEOF: true, want: []any{"aaa\n", "bbb\n", "ccc\n"}, wantReads: 2,
		},
		{
			name: "legal message then oversized remainder", limit: 4, chunks: []string{"aaa\nbbbbb", "\n"},
			want: []any{"aaa\n"}, wantError: true, wantReads: 1,
		},
		{
			name: "unlimited incomplete message", chunks: []string{strings.Repeat("a", 64*1024)},
			dataWithEOF: true, wantReads: 64 * 1024 / maxReadBufLen,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &tcpLimitTestConn{dataWithEOF: test.dataWithEOF}
			for _, chunk := range test.chunks {
				conn.chunks = append(conn.chunks, []byte(chunk))
			}
			ss := newTCPSession(conn, nil).(*session)
			ss.SetMaxMsgLen(test.limit)
			reader := &tcpLimitLineReader{}
			listener := &recordingEventListener{}
			ss.SetReader(reader)
			ss.SetEventListener(listener)

			err := ss.handleTCPPackage()
			if (err != nil) != test.wantError {
				t.Fatalf("handleTCPPackage error = %v, want error=%t; largest buffer=%d", err, test.wantError, reader.largest)
			}
			if err != nil && !strings.Contains(err.Error(), "max message len") {
				t.Fatalf("expected a size error, got %v", err)
			}
			if conn.reads != test.wantReads {
				t.Fatalf("read calls = %d, want %d", conn.reads, test.wantReads)
			}
			if !reflect.DeepEqual(listener.messages, test.want) {
				t.Fatalf("delivered messages = %#v, want %#v", listener.messages, test.want)
			}
			if test.limit > 0 && reader.largest > test.limit+maxReadBufLen {
				t.Fatalf("buffer grew to %d, beyond limit plus one read batch", reader.largest)
			}
		})
	}
}
