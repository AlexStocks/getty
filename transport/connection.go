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
	"compress/flate"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

import (
	"github.com/golang/snappy"

	"github.com/gorilla/websocket"

	perrors "github.com/pkg/errors"

	uatomic "go.uber.org/atomic"
)

import (
	log "github.com/AlexStocks/getty/util"
)

var (
	launchTime = time.Now()
	connID     uatomic.Uint32
)

// ErrCodecStreamBroken is returned once a codec(compressed) stream cannot be
// trusted any more: a read/write timed out or failed in the middle of a codec
// block, so the decoder/encoder state and the bytes already on the wire are
// desynchronized. flate/snappy latch such errors forever, hence the connection
// is unusable and the session must be closed and rebuilt. It deliberately does
// not unwrap to a net.Error, so session.handleTCPPackage treats it as fatal
// instead of as a benign read timeout that can be retried.
var ErrCodecStreamBroken = errors.New("getty: codec stream is broken, connection must be closed")

// CodecStallTimeout is the read deadline used on a codec(compressed) stream,
// and thus the upper bound on how long a stalled peer can block a read.
//
// It exists because the connection read timeout (see SetReadTimeout, 1s by
// default) is a poll interval, not an idle timeout: session.handleTCPPackage
// retries after every read timeout. A codec stream cannot be retried
// (flate/snappy latch read errors forever), so arming rTimeout on it would tear
// down every compressed connection that stays idle for one second. Hence codec
// reads get this much wider deadline instead, and hitting it means the peer
// really stalled mid-stream: the connection is declared broken.
//
// Keep it well above the read timeout and above the application heartbeat
// period, otherwise idle compressed connections get killed. Set it to 0 to arm
// no read deadline at all on codec streams (a stalled peer then blocks until
// the session is closed). It is read once, when SetCompressType installs the
// codec, so set it before creating connections.
var CodecStallTimeout = 5 * time.Minute

// Connection wrap some connection params and operations
type Connection interface {
	ID() uint32
	// SetCompressType sets the compress type. It must be called before any
	// recv/Send on the connection, typically in NewSessionCallback; a TCP
	// connection panics if it is called after IO started.
	SetCompressType(CompressType)
	LocalAddr() string
	RemoteAddr() string
	// IncReadPkgNum increases connection's read pkg number
	IncReadPkgNum()
	// IncWritePkgNum increases connection's write pkg number
	IncWritePkgNum()
	// UpdateActive update session's active time
	UpdateActive()
	// GetActive get session's active time
	GetActive() time.Time
	// ReadTimeout gets deadline for the future read calls.
	ReadTimeout() time.Duration
	// SetReadTimeout sets deadline for the future read calls.
	SetReadTimeout(time.Duration)
	// WriteTimeout gets deadline for the future write calls.
	WriteTimeout() time.Duration
	// SetWriteTimeout sets deadline for the future write calls.
	SetWriteTimeout(time.Duration)
	// Send pkg data to peer
	Send(any) (int, error)
	// CloseConn close connection
	CloseConn(int)
	// SetSession sets related session
	SetSession(Session)
}

// ///////////////////////////////////////
// getty connection
// ///////////////////////////////////////

type gettyConn struct {
	id       uint32
	compress CompressType
	// codecEnabled reports whether reader/writer have been replaced by a codec.
	// compress == CompressNone is not a substitute: CompressNone is
	// flate.NoCompression(0), so SetCompressType(CompressNone) still installs a
	// flate codec that frames the stream into deflate blocks. Such a stream is
	// stateful and must not be treated like a raw connection.
	codecEnabled  bool
	readBytes     uatomic.Uint32   // read bytes
	writeBytes    uatomic.Uint32   // write bytes
	readPkgNum    uatomic.Uint32   // send pkg number
	writePkgNum   uatomic.Uint32   // recv pkg number
	active        uatomic.Int64    // last active, in milliseconds
	rTimeout      uatomic.Duration // network current limiting
	wTimeout      uatomic.Duration
	rLastDeadline uatomic.Time // last network read time
	wLastDeadline uatomic.Time // last network write time
	local         string       // local address
	peer          string       // peer address
	ss            Session
}

func (c *gettyConn) ID() uint32 {
	return c.id
}

func (c *gettyConn) LocalAddr() string {
	return c.local
}

func (c *gettyConn) RemoteAddr() string {
	return c.peer
}

func (c *gettyConn) IncReadPkgNum() {
	c.readPkgNum.Add(1)
}

func (c *gettyConn) IncWritePkgNum() {
	c.writePkgNum.Add(1)
}

func (c *gettyConn) UpdateActive() {
	c.active.Store(int64(time.Since(launchTime)))
}

func (c *gettyConn) GetActive() time.Time {
	return launchTime.Add(time.Duration(c.active.Load()))
}

// removed unused methods send/close

func (c *gettyConn) ReadTimeout() time.Duration {
	return c.rTimeout.Load()
}

func (c *gettyConn) SetSession(ss Session) {
	c.ss = ss
}

// SetReadTimeout Pls do not set read deadline for websocket connection. AlexStocks 20180310
// gorilla/websocket/conn.go:NextReader will always fail when got a timeout error.
//
// Pls do not set read deadline when using compression. AlexStocks 20180314.
func (c *gettyConn) SetReadTimeout(rTimeout time.Duration) {
	if rTimeout < 1 {
		panic("@rTimeout < 1")
	}

	c.rTimeout.Store(rTimeout)
	if c.wTimeout.Load() == 0 {
		c.wTimeout.Store(rTimeout)
	}
}

func (c *gettyConn) WriteTimeout() time.Duration {
	return c.wTimeout.Load()
}

// SetWriteTimeout Pls do not set write deadline for websocket connection. AlexStocks 20180310
// gorilla/websocket/conn.go:NextWriter will always fail when got a timeout error.
//
// Pls do not set write deadline when using compression. AlexStocks 20180314.
func (c *gettyConn) SetWriteTimeout(wTimeout time.Duration) {
	if wTimeout < 1 {
		panic("@wTimeout < 1")
	}

	c.wTimeout.Store(wTimeout)
	if c.rTimeout.Load() == 0 {
		c.rTimeout.Store(wTimeout)
	}
}

/////////////////////////////////////////
// getty tcp connection
/////////////////////////////////////////

type gettyTCPConn struct {
	gettyConn
	// lock guards the codec fields below; streamStarted is set by the first
	// recv/Send and freezes the codec configuration (see SetCompressType).
	lock          sync.Mutex
	streamStarted bool
	reader        io.Reader
	writer        io.Writer
	conn          net.Conn // immutable after construction, closed via closeOnce
	closeOnce     sync.Once
	// codecBroken is set once a codec read/write failed in the middle of the
	// stream. Written by the read goroutine and read by writer goroutines, so
	// it has to be atomic. Once set, recv/Send fail fast: touching the codec
	// again would only produce more garbage on the wire.
	codecBroken uatomic.Bool
	// codecStallTimeout is the per-conn copy of CodecStallTimeout, taken when
	// the codec is installed. 0 arms no read deadline on the codec stream.
	codecStallTimeout time.Duration
}

// create gettyTCPConn
func newGettyTCPConn(conn net.Conn) *gettyTCPConn {
	if conn == nil {
		panic("newGettyTCPConn(conn):@conn is nil")
	}
	var localAddr, peerAddr string
	//  check conn.LocalAddr or conn.RemoteAddr is nil to defeat panic on 2016/09/27
	if conn.LocalAddr() != nil {
		localAddr = conn.LocalAddr().String()
	}
	if conn.RemoteAddr() != nil {
		peerAddr = conn.RemoteAddr().String()
	}

	return &gettyTCPConn{
		conn:   conn,
		reader: io.Reader(conn),
		writer: io.Writer(conn),
		gettyConn: gettyConn{
			id:       connID.Add(1),
			rTimeout: *uatomic.NewDuration(netIOTimeout),
			wTimeout: *uatomic.NewDuration(netIOTimeout),
			local:    localAddr,
			peer:     peerAddr,
			compress: CompressNone,
		},
	}
}

// buffersWriter is implemented by writers that can take a whole batch at once.
// The compress writers implement it so a batch becomes one compressed block
// instead of one block per packet.
type buffersWriter interface {
	WriteBuffers(buffers [][]byte) (int64, error)
}

// for zip compress
type writeFlusher struct {
	flusher *flate.Writer
	lock    sync.Mutex
}

func (t *writeFlusher) Write(p []byte) (int, error) {
	var (
		n   int
		err error
	)
	t.lock.Lock()
	defer t.lock.Unlock()
	n, err = t.flusher.Write(p)
	if err != nil {
		return n, perrors.WithStack(err)
	}
	if err := t.flusher.Flush(); err != nil {
		return n, perrors.WithStack(err)
	}

	return n, nil
}

// WriteBuffers writes the whole batch into the compressor and flushes once at
// the end. Calling Write per buffer would flush every time and degrade into N
// separate deflate blocks: N syscalls, and no way to exploit patterns that
// repeat across the packets.
func (t *writeFlusher) WriteBuffers(buffers [][]byte) (int64, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	var total int64
	for _, b := range buffers {
		n, err := t.flusher.Write(b)
		total += int64(n)
		if err != nil {
			return total, perrors.WithStack(err)
		}
	}
	return total, perrors.WithStack(t.flusher.Flush())
}

// for snappy compress. #102: snappy.NewBufferedWriter buffers writes and only
// emits data on Flush, so small packets would sit in the buffer forever if not
// flushed after every Write. This wrapper flushes on every Write, mirroring the
// flate writeFlusher behavior above.
type snappyWriteFlusher struct {
	writer *snappy.Writer
	lock   sync.Mutex
}

func newSnappyWriteFlusher(w *snappy.Writer) *snappyWriteFlusher {
	return &snappyWriteFlusher{writer: w}
}

func (s *snappyWriteFlusher) Write(p []byte) (int, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	n, err := s.writer.Write(p)
	if err != nil {
		return n, perrors.WithStack(err)
	}
	if err := s.writer.Flush(); err != nil {
		return n, perrors.WithStack(err)
	}
	return n, nil
}

// WriteBuffers mirrors writeFlusher.WriteBuffers: write the whole batch, flush
// once at the end, so a batch leaves as a single snappy block.
func (s *snappyWriteFlusher) WriteBuffers(buffers [][]byte) (int64, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	var total int64
	for _, b := range buffers {
		n, err := s.writer.Write(b)
		total += int64(n)
		if err != nil {
			return total, perrors.WithStack(err)
		}
	}
	return total, perrors.WithStack(s.writer.Flush())
}

func (s *snappyWriteFlusher) Close() error {
	s.lock.Lock()
	defer s.lock.Unlock()
	return perrors.WithStack(s.writer.Close())
}

// SetCompressType set compress type(tcp: zip/snappy, websocket:zip).
// It must be called before the first recv/Send on the connection, typically
// inside NewSessionCallback; there is no codec renegotiation protocol, so
// switching the stream format after IO started would desynchronize the peer's
// decoder. Calling it on a started stream panics.
func (t *gettyTCPConn) SetCompressType(c CompressType) {
	t.lock.Lock()
	defer t.lock.Unlock()
	if t.streamStarted {
		log.Errorf("SetCompressType(%d) called after IO started on connection{local:%s, peer:%s}", c, t.local, t.peer)
		panic("SetCompressType must be called before any recv/Send on the connection, e.g. in NewSessionCallback")
	}
	switch c {
	case CompressNone, CompressZip, CompressBestSpeed, CompressBestCompression, CompressHuffman:
		ioReader := io.Reader(t.conn)
		t.reader = flate.NewReader(ioReader)

		ioWriter := io.Writer(t.conn)
		w, err := flate.NewWriter(ioWriter, int(c))
		if err != nil {
			panic(fmt.Sprintf("flate.NewReader(flate.DefaultCompress) = err(%s)", err))
		}
		t.writer = &writeFlusher{flusher: w}

	case CompressSnappy:
		ioReader := io.Reader(t.conn)
		t.reader = snappy.NewReader(ioReader)
		ioWriter := io.Writer(t.conn)
		// #102: wrap the buffered snappy writer so every Write is flushed,
		// otherwise small packets never leave the internal buffer.
		t.writer = newSnappyWriteFlusher(snappy.NewBufferedWriter(ioWriter))

	default:
		panic(fmt.Sprintf("illegal comparess type %d", c))
	}
	// Both branches replaced reader/writer with a codec (CompressNone included,
	// see the codecEnabled comment), so the conn is no longer raw: all IO must
	// go through t.reader/t.writer, and a read/write error can no longer be
	// retried on this stream.
	t.codecEnabled = true
	t.codecStallTimeout = CodecStallTimeout
	t.compress = c
}

// readDeadlineTimeout returns the deadline to arm before a read.
//
// A raw conn uses rTimeout, whose timeouts session.handleTCPPackage simply
// retries. A codec stream cannot be retried, so it gets CodecStallTimeout
// instead - see that variable for why rTimeout is unusable here. The larger of
// the two wins: raising rTimeout widens the codec bound, and lowering
// CodecStallTimeout tightens it.
func (t *gettyTCPConn) readDeadlineTimeout() time.Duration {
	timeout := t.rTimeout.Load()
	if !t.codecEnabled {
		return timeout
	}
	if t.codecStallTimeout <= 0 {
		// stall cap disabled: no deadline, the read blocks until data arrives
		// or session.stop() arms a deadline to unblock it.
		return 0
	}
	if timeout < t.codecStallTimeout {
		return t.codecStallTimeout
	}
	return timeout
}

// beginRecv/beginSend mark the stream as started and snapshot the codec state,
// so the blocking IO below runs without the lock and cannot race with
// SetCompressType.
func (t *gettyTCPConn) beginRecv() (io.Reader, time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.streamStarted = true
	return t.reader, t.readDeadlineTimeout()
}

func (t *gettyTCPConn) beginSend() (io.Writer, bool) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.streamStarted = true
	return t.writer, t.codecEnabled
}

// codecIOError maps a timeout on a codec stream to the fatal
// ErrCodecStreamBroken and latches the connection as broken.
//
// A timed out read leaves half a block in the decoder and misaligns every byte
// after it; a timed out write leaves half a block on the wire and misaligns the
// peer's decoder. Either way the stream cannot be reused, so the timeout is
// reported as ErrCodecStreamBroken instead of as a net.Error: that is what makes
// session.handleTCPPackage treat it as fatal and close the session (a client
// then reconnects) rather than retry the read.
//
// The socket is closed as well, so the codec connection is terminated rather
// than left half-alive: it unblocks any concurrent Read/Write and lets the
// session tear down even when the stalled side is the writer. CloseConn is not
// used here because closing the snappy writer can block on the very peer that
// stalled; closing the raw conn is enough, and CloseConn skips the broken
// writer when it eventually runs.
//
// Any other failure keeps its own identity - io.EOF above all, which the session
// read loop matches on to detect a clean peer shutdown. Those need no latch:
// flate/snappy record the failure internally and emit nothing more, every
// further Read/Write just returns it again.
func (t *gettyTCPConn) codecIOError(err error) error {
	if err == nil || !t.codecEnabled || !isTimeoutError(err) {
		return perrors.WithStack(err)
	}

	t.codecBroken.Store(true)
	_ = t.conn.Close()
	return perrors.Wrapf(ErrCodecStreamBroken, "codec stream stalled: %v", err)
}

// codecReadError is codecIOError for the read path, where a timeout raised while
// the session is closing must be passed through: session.stop() deliberately
// arms a read deadline to unblock this goroutine, and that timeout is a shutdown
// signal, not a stalled peer. Latching it would make every normal close of a
// compressed session report an error to the listener. The write path has no such
// exemption - a timed out write desynchronizes the peer whether we are closing
// or not.
func (t *gettyTCPConn) codecReadError(err error) error {
	if t.codecEnabled && isTimeoutError(err) && t.sessionClosing() {
		return perrors.WithStack(err)
	}
	return t.codecIOError(err)
}

func (t *gettyTCPConn) sessionClosing() bool {
	// t.ss is set by newSession before any IO goroutine starts; it is nil only
	// for a connection used without a session (unit tests).
	ss := t.ss
	return ss != nil && ss.IsClosed()
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded)
}

// tcp connection read
func (t *gettyTCPConn) recv(p []byte) (int, error) {
	var (
		err         error
		currentTime time.Time
		length      int
	)

	if t.codecBroken.Load() {
		return 0, perrors.WithStack(ErrCodecStreamBroken)
	}

	reader, timeout := t.beginRecv()

	// set read timeout deadline
	if timeout > 0 {
		// Set Deadline every time, since golang has fixed the performance issue
		// See https://github.com/golang/go/issues/15133#issuecomment-271571395 for details
		currentTime = time.Now()
		if err = t.conn.SetReadDeadline(currentTime.Add(timeout)); err != nil {
			// just a timeout error
			return 0, perrors.WithStack(err)
		}
		t.rLastDeadline.Store(currentTime)
	}

	length, err = reader.Read(p)
	t.readBytes.Add(uint32(length))
	return length, t.codecReadError(err)
}

// tcp connection write
func (t *gettyTCPConn) Send(pkg any) (int, error) {
	var (
		err         error
		currentTime time.Time
		ok          bool
		p           []byte
		length      int
		lg          int64
	)

	if t.codecBroken.Load() {
		return 0, perrors.WithStack(ErrCodecStreamBroken)
	}

	writer, codecEnabled := t.beginSend()

	// The write deadline applies to a codec stream too: unlike a read, a write
	// only blocks when the peer stopped reading, so it never fires on an idle
	// connection and there is nothing to poll for. Skipping it here used to make
	// SetWriteTimeout - and the per-call WritePkg(pkg, timeout) - silently
	// ineffective, letting a stalled peer block writers forever.
	if t.wTimeout.Load() > 0 {
		// Set Deadline every time, since golang has fixed the performance issue
		// See https://github.com/golang/go/issues/15133#issuecomment-271571395 for details
		currentTime = time.Now()
		if err = t.conn.SetWriteDeadline(currentTime.Add(t.wTimeout.Load())); err != nil {
			return 0, perrors.WithStack(err)
		}
		t.wLastDeadline.Store(currentTime)
	}

	if buffers, ok := pkg.([][]byte); ok {
		// #102: when a codec is installed the [][]byte path must go through
		// t.writer (the codec writer), otherwise it writes raw frames directly
		// to t.conn and the peer receives a corrupt mix of coded and raw data.
		if !codecEnabled {
			// only a raw conn here, so writev the whole batch in one syscall.
			netBuf := net.Buffers(buffers)
			lg, err = netBuf.WriteTo(t.conn)
		} else if bw, ok := writer.(buffersWriter); ok {
			lg, err = bw.WriteBuffers(buffers)
		} else {
			for _, b := range buffers {
				var n int
				n, err = writer.Write(b)
				if err != nil {
					break
				}
				lg += int64(n)
			}
		}
		if err == nil {
			t.writeBytes.Add((uint32)(lg))
			t.writePkgNum.Add((uint32)(len(buffers)))
		}
		log.Debugf("localAddr: %s, remoteAddr:%s, now:%s, length:%d, err:%s",
			t.conn.LocalAddr(), t.conn.RemoteAddr(), currentTime, length, err)
		return int(lg), t.codecIOError(err)
	}

	if p, ok = pkg.([]byte); ok {
		length, err = writer.Write(p)
		if err == nil {
			t.writeBytes.Add((uint32)(len(p)))
			t.writePkgNum.Add(1)
		}
		log.Debugf("localAddr: %s, remoteAddr:%s, now:%s, length:%d, err:%v",
			t.conn.LocalAddr(), t.conn.RemoteAddr(), currentTime, length, err)
		return length, t.codecIOError(err)
	}

	return 0, perrors.Errorf("illegal @pkg{%#v} type", pkg)
}

// close tcp connection
func (t *gettyTCPConn) CloseConn(waitSec int) {
	t.closeOnce.Do(func() {
		t.lock.Lock()
		writer := t.writer
		t.lock.Unlock()
		// #102: snappy writer is now wrapped in *snappyWriteFlusher.
		// A broken codec must not be flushed: the stream is already
		// desynchronized, and Close would only push more garbage into a socket
		// that may still be stalled.
		if !t.codecBroken.Load() {
			if writer, ok := writer.(*snappyWriteFlusher); ok {
				if err := writer.Close(); err != nil {
					log.Errorf("snappy.Writer.Close() = error:%+v", err)
				}
			}
		}
		// #103: do not hard-assert *tls.Conn; use safe type assertions so a
		// non-TLS, non-TCP conn does not panic here.
		if conn, ok := t.conn.(*net.TCPConn); ok {
			_ = conn.SetLinger(waitSec)
			_ = conn.Close()
		} else if tlsConn, ok := t.conn.(*tls.Conn); ok {
			_ = tlsConn.Close()
		} else {
			_ = t.conn.Close()
		}
	})
}

// ///////////////////////////////////////
// getty udp connection
// ///////////////////////////////////////

type UDPContext struct {
	Pkg      any
	PeerAddr *net.UDPAddr
}

func (c UDPContext) String() string {
	return fmt.Sprintf("{pkg:%#v, peer addr:%s}", c.Pkg, c.PeerAddr)
}

type gettyUDPConn struct {
	gettyConn
	compressType CompressType
	conn         *net.UDPConn // for server; immutable after construction, closed via closeOnce
	closeOnce    sync.Once
}

// create gettyUDPConn
func newGettyUDPConn(conn *net.UDPConn) *gettyUDPConn {
	if conn == nil {
		panic("newGettyUDPConn(conn):@conn is nil")
	}

	var localAddr, peerAddr string
	if conn.LocalAddr() != nil {
		localAddr = conn.LocalAddr().String()
	}

	if conn.RemoteAddr() != nil {
		// connected udp
		peerAddr = conn.RemoteAddr().String()
	}

	return &gettyUDPConn{
		conn: conn,
		gettyConn: gettyConn{
			id:       connID.Add(1),
			rTimeout: *uatomic.NewDuration(netIOTimeout),
			wTimeout: *uatomic.NewDuration(netIOTimeout),
			local:    localAddr,
			peer:     peerAddr,
			compress: CompressNone,
		},
	}
}

func (u *gettyUDPConn) SetCompressType(c CompressType) {
	switch c {
	case CompressNone, CompressZip, CompressBestSpeed, CompressBestCompression, CompressHuffman, CompressSnappy:
		u.compressType = c

	default:
		panic(fmt.Sprintf("illegal comparess type %d", c))
	}
}

// udp connection read
func (u *gettyUDPConn) recv(p []byte) (int, *net.UDPAddr, error) {
	if u.rTimeout.Load() > 0 {
		// Set Deadline every time, since golang has fixed the performance issue
		// See https://github.com/golang/go/issues/15133#issuecomment-271571395 for details
		currentTime := time.Now()
		if err := u.conn.SetReadDeadline(currentTime.Add(u.rTimeout.Load())); err != nil {
			return 0, nil, perrors.WithStack(err)
		}
		u.rLastDeadline.Store(currentTime)
	}

	length, addr, err := u.conn.ReadFromUDP(p) // connected udp also can get return @addr
	log.Debugf("ReadFromUDP(p:%d) = {length:%d, peerAddr:%s, error:%v}", len(p), length, addr, err)
	if err == nil {
		u.readBytes.Add(uint32(length))
	}

	return length, addr, perrors.WithStack(err)
}

// write udp packet, @ctx should be of type UDPContext
func (u *gettyUDPConn) Send(udpCtx any) (int, error) {
	var (
		err         error
		currentTime time.Time
		length      int
		ok          bool
		ctx         UDPContext
		buf         []byte
		peerAddr    *net.UDPAddr
	)

	if ctx, ok = udpCtx.(UDPContext); !ok {
		return 0, perrors.Errorf("illegal @udpCtx{%s} type, @udpCtx type:%T", udpCtx, udpCtx)
	}
	if buf, ok = ctx.Pkg.([]byte); !ok {
		return 0, perrors.Errorf("illegal @udpCtx.Pkg{%#v} type", udpCtx)
	}
	if u.ss.EndPoint().EndPointType() == UDP_ENDPOINT {
		peerAddr = ctx.PeerAddr
		if peerAddr == nil {
			return 0, ErrNullPeerAddr
		}
	}

	if u.wTimeout.Load() > 0 {
		// Set Deadline every time, since golang has fixed the performance issue
		// See https://github.com/golang/go/issues/15133#issuecomment-271571395 for details
		currentTime = time.Now()
		if err = u.conn.SetWriteDeadline(currentTime.Add(u.wTimeout.Load())); err != nil {
			return 0, perrors.WithStack(err)
		}
		u.wLastDeadline.Store(currentTime)
	}

	if length, _, err = u.conn.WriteMsgUDP(buf, nil, peerAddr); err == nil {
		u.writeBytes.Add((uint32)(len(buf)))
		u.writePkgNum.Add(1)
	}
	log.Debugf("WriteMsgUDP(peerAddr:%s) = {length:%d, error:%v}", peerAddr, length, err)

	return length, perrors.WithStack(err)
}

// close udp connection
func (u *gettyUDPConn) CloseConn(_ int) {
	u.closeOnce.Do(func() {
		_ = u.conn.Close()
	})
}

// ///////////////////////////////////////
// getty websocket connection
// ///////////////////////////////////////

type gettyWSConn struct {
	gettyConn
	writeLock sync.Mutex
	readLock  sync.Mutex
	conn      *websocket.Conn
}

// create websocket connection
func newGettyWSConn(conn *websocket.Conn) *gettyWSConn {
	if conn == nil {
		panic("newGettyWSConn(conn):@conn is nil")
	}
	var localAddr, peerAddr string
	//  check conn.LocalAddr or conn.RemoetAddr is nil to defeat panic on 2016/09/27
	if conn.LocalAddr() != nil {
		localAddr = conn.LocalAddr().String()
	}
	if conn.RemoteAddr() != nil {
		peerAddr = conn.RemoteAddr().String()
	}

	gettyWSConn := &gettyWSConn{
		conn: conn,
		gettyConn: gettyConn{
			id:       connID.Add(1),
			rTimeout: *uatomic.NewDuration(netIOTimeout),
			wTimeout: *uatomic.NewDuration(netIOTimeout),
			local:    localAddr,
			peer:     peerAddr,
			compress: CompressNone,
		},
	}
	conn.EnableWriteCompression(false)
	conn.SetPingHandler(gettyWSConn.handlePing)
	conn.SetPongHandler(gettyWSConn.handlePong)

	return gettyWSConn
}

// SetCompressType set compress type
func (w *gettyWSConn) SetCompressType(c CompressType) {
	switch c {
	case CompressNone, CompressZip, CompressBestSpeed, CompressBestCompression, CompressHuffman:
		w.conn.EnableWriteCompression(true)
		if err := w.conn.SetCompressionLevel(int(c)); err != nil {
			log.Warnf("failed to set compression level: %+v", err)
		}

	default:
		panic(fmt.Sprintf("illegal comparess type %d", c))
	}
	w.compress = c
}

func (w *gettyWSConn) handlePing(message string) error {
	err := w.writePong([]byte(message))
	if err == websocket.ErrCloseSent {
		err = nil
		//	change the error checking from "e.Temporary()" to "e.Timeout()".
		//  as per https://github.com/golang/go/issues/45729,
		//  Timeout() correctly captures subset of Temporary() errors that could be retried.
		//  The rest of Temporary() errors should not be retried anyway (like syscall errors, out of file descriptors)
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		err = nil
	}
	if err == nil {
		w.UpdateActive()
	}

	return perrors.WithStack(err)
}

func (w *gettyWSConn) handlePong(string) error {
	w.UpdateActive()
	return nil
}

// websocket connection read
func (w *gettyWSConn) recv() ([]byte, error) {
	// Pls do not set read deadline when using ReadMessage. AlexStocks 20180310
	// gorilla/websocket/conn.go:NextReader will always fail when got a timeout error.
	_, b, e := w.threadSafeReadMessage() // the first return value is message type.
	if e == nil {
		w.readBytes.Add((uint32)(len(b)))
	} else {
		if websocket.IsUnexpectedCloseError(e, websocket.CloseGoingAway) {
			log.Warnf("websocket unexpected CloseConn error: %v", e)
		}
	}

	return b, perrors.WithStack(e)
}

func (w *gettyWSConn) updateWriteDeadline() error {
	var (
		err         error
		currentTime time.Time
	)

	if w.wTimeout.Load() > 0 {
		// Set Deadline every time, since golang has fixed the performance issue
		// See https://github.com/golang/go/issues/15133#issuecomment-271571395 for details
		currentTime = time.Now()
		if err = w.conn.SetWriteDeadline(currentTime.Add(w.wTimeout.Load())); err != nil {
			return perrors.WithStack(err)
		}
		w.wLastDeadline.Store(currentTime)
	}

	return nil
}

// websocket connection write
func (w *gettyWSConn) Send(pkg any) (int, error) {
	var (
		err error
		ok  bool
		p   []byte
	)

	if p, ok = pkg.([]byte); !ok {
		return 0, perrors.Errorf("illegal @pkg{%#v} type", pkg)
	}

	if err := w.updateWriteDeadline(); err != nil {
		log.Warnf("failed to update write deadline: %+v", err)
	}
	if err = w.threadSafeWriteMessage(websocket.BinaryMessage, p); err == nil {
		w.writeBytes.Add((uint32)(len(p)))
		w.writePkgNum.Add(1)
	}
	return len(p), perrors.WithStack(err)
}

func (w *gettyWSConn) writePing() error {
	if err := w.updateWriteDeadline(); err != nil {
		log.Warnf("failed to update write deadline: %+v", err)
	}
	return perrors.WithStack(w.threadSafeWriteMessage(websocket.PingMessage, []byte{}))
}

func (w *gettyWSConn) writePong(message []byte) error {
	if err := w.updateWriteDeadline(); err != nil {
		log.Warnf("failed to update write deadline: %+v", err)
	}
	return perrors.WithStack(w.threadSafeWriteMessage(websocket.PongMessage, message))
}

// close websocket connection
func (w *gettyWSConn) CloseConn(waitSec int) {
	if err := w.updateWriteDeadline(); err != nil {
		log.Warnf("failed to update write deadline: %+v", err)
	}
	if err := w.threadSafeWriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye-bye!!!")); err != nil {
		log.Warnf("failed to send close message: %+v", err)
	}
	conn := w.conn.UnderlyingConn()
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetLinger(waitSec)
	} else if wsConn, ok := conn.(*tls.Conn); ok {
		_ = wsConn.CloseWrite()
	}
	if err := w.conn.Close(); err != nil {
		log.Warnf("failed to close websocket conn: %+v", err)
	}
}

// uses a mutex(writeLock) to ensure that only one thread can send a message at a time, preventing race conditions.
func (w *gettyWSConn) threadSafeWriteMessage(messageType int, data []byte) error {
	w.writeLock.Lock()
	defer w.writeLock.Unlock()
	if err := w.conn.WriteMessage(messageType, data); err != nil {
		return err
	}
	return nil
}

// uses a mutex(readLock) to ensure that only one thread can read a message at a time, preventing race conditions.
func (w *gettyWSConn) threadSafeReadMessage() (int, []byte, error) {
	w.readLock.Lock()
	defer w.readLock.Unlock()
	messageType, readBytes, err := w.conn.ReadMessage()
	if err != nil {
		return messageType, nil, err
	}
	return messageType, readBytes, nil
}
