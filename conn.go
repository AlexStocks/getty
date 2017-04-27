/******************************************************
# DESC       : tcp/websocket connection
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2016-08-17 11:21
# FILE       : conn.go
******************************************************/

package getty

import (
	// "errors"
	"compress/flate"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
	"github.com/golang/snappy"
	"github.com/gorilla/websocket"
)

var (
	launchTime time.Time = time.Now()

// ErrInvalidConnection = errors.New("connection has been closed.")
)

/////////////////////////////////////////
// compress
/////////////////////////////////////////

type CompressType int

const (
	CompressNone            CompressType = flate.NoCompression      // 0
	CompressZip                          = flate.DefaultCompression // -1
	CompressBestSpeed                    = flate.BestSpeed          // 1
	CompressBestCompression              = flate.BestCompression    // 9
	CompressHuffman                      = flate.HuffmanOnly        // -2
	CompressSnappy                       = 10
)

/////////////////////////////////////////
// connection interfacke
/////////////////////////////////////////

type Connection interface {
	ID() uint32
	SetCompressType(t CompressType)
	LocalAddr() string
	RemoteAddr() string
	incReadPkgCount()
	incWritePkgCount()
	// update session's active time
	UpdateActive()
	// get session's active time
	GetActive() time.Time
	readDeadline() time.Duration
	// SetReadDeadline sets deadline for the future read calls.
	SetReadDeadline(time.Duration)
	writeDeadline() time.Duration
	// SetWriteDeadlile sets deadline for the future read calls.
	SetWriteDeadline(time.Duration)
	Write(p []byte) error
	// don't distinguish between tcp connection and websocket connection. Because
	// gorilla/websocket/conn.go:(Conn)Close also invoke net.Conn.Close
	close(int)
}

/////////////////////////////////////////
// getty connection
/////////////////////////////////////////

var (
	connID uint32
)

type gettyConn struct {
	id            uint32
	compress      CompressType
	padding1      uint8
	padding2      uint16
	readCount     uint32        // read() count
	writeCount    uint32        // write() count
	readPkgCount  uint32        // send pkg count
	writePkgCount uint32        // recv pkg count
	active        int64         // last active, in milliseconds
	rDeadline     time.Duration // network current limiting
	wDeadline     time.Duration
	local         string // local address
	peer          string // peer address
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

func (c *gettyConn) incReadPkgCount() {
	atomic.AddUint32(&c.readPkgCount, 1)
}

func (c *gettyConn) incWritePkgCount() {
	atomic.AddUint32(&c.writePkgCount, 1)
}

func (c *gettyConn) UpdateActive() {
	atomic.StoreInt64(&(c.active), int64(time.Since(launchTime)))
}

func (c *gettyConn) GetActive() time.Time {
	return launchTime.Add(time.Duration(atomic.LoadInt64(&(c.active))))
}

func (c *gettyConn) Write([]byte) error {
	return nil
}

func (c *gettyConn) close(int) {}

func (c gettyConn) readDeadline() time.Duration {
	return c.rDeadline
}

func (c *gettyConn) SetReadDeadline(rDeadline time.Duration) {
	if rDeadline < 1 {
		panic("@rDeadline < 1")
	}

	c.rDeadline = rDeadline
	if c.wDeadline == 0 {
		c.wDeadline = rDeadline
	}
}

func (c gettyConn) writeDeadline() time.Duration {
	return c.wDeadline
}

func (c *gettyConn) SetWriteDeadline(wDeadline time.Duration) {
	if wDeadline < 1 {
		panic("@wDeadline < 1")
	}

	c.wDeadline = wDeadline
}

/////////////////////////////////////////
// getty tcp connection
/////////////////////////////////////////

type gettyTCPConn struct {
	gettyConn
	reader io.Reader
	writer io.Writer
	conn   net.Conn
}

// create gettyTCPConn
func newGettyTCPConn(conn net.Conn) *gettyTCPConn {
	if conn == nil {
		panic("newGettyTCPConn(conn):@conn is nil")
	}
	var localAddr, peerAddr string
	//  check conn.LocalAddr or conn.RemoetAddr is nil to defeat panic on 2016/09/27
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
			id:       atomic.AddUint32(&connID, 1),
			local:    localAddr,
			peer:     peerAddr,
			compress: CompressNone,
		},
	}
}

// for zip compress
type writeFlusher struct {
	flusher *flate.Writer
}

func (t *writeFlusher) Write(p []byte) (int, error) {
	var (
		n   int
		err error
	)

	n, err = t.flusher.Write(p)
	if err != nil {
		return n, err
	}
	if err := t.flusher.Flush(); err != nil {
		return 0, err
	}

	return n, nil
}

// set compress type(tcp: zip/snappy, websocket:zip)
func (t *gettyTCPConn) SetCompressType(c CompressType) {
	switch c {
	case CompressNone, CompressZip, CompressBestSpeed, CompressBestCompression, CompressHuffman:
		t.reader = flate.NewReader(t.conn)

		w, err := flate.NewWriter(t.conn, int(c))
		if err != nil {
			panic(fmt.Sprintf("flate.NewReader(flate.DefaultCompress) = err(%s)", err))
		}
		t.writer = &writeFlusher{flusher: w}

	case CompressSnappy:
		t.reader = snappy.NewReader(t.conn)
		// t.writer = snappy.NewWriter(t.conn)
		t.writer = snappy.NewBufferedWriter(t.conn)

	default:
		panic(fmt.Sprintf("illegal comparess type %d", c))
	}
}

// tcp connection read
func (t *gettyTCPConn) read(p []byte) (int, error) {
	// if t.conn == nil {
	//	return 0, ErrInvalidConnection
	// }

	// atomic.AddUint32(&t.readCount, 1)
	// l, e := t.conn.Read(p)
	l, e := t.reader.Read(p)
	atomic.AddUint32(&t.readCount, uint32(l))
	return l, e
}

// tcp connection write
func (t *gettyTCPConn) Write(p []byte) error {
	// if t.conn == nil {
	//	return 0, ErrInvalidConnection
	// }

	// atomic.AddUint32(&t.writeCount, 1)
	atomic.AddUint32(&t.writeCount, (uint32)(len(p)))
	// _, err := t.conn.Write(p)
	_, err := t.writer.Write(p)

	return err
}

// close tcp connection
func (t *gettyTCPConn) close(waitSec int) {
	// if tcpConn, ok := t.conn.(*net.TCPConn); ok {
	// tcpConn.SetLinger(0)
	// }

	if t.conn != nil {
		if writer, ok := t.writer.(*snappy.Writer); ok {
			if err := writer.Close(); err != nil {
				log.Error("snappy.Writer.Close() = error{%v}", err)
			}
		}
		t.conn.(*net.TCPConn).SetLinger(waitSec)
		t.conn.Close()
		t.conn = nil
	}
}

/////////////////////////////////////////
// getty websocket connection
/////////////////////////////////////////

type gettyWSConn struct {
	gettyConn
	conn *websocket.Conn
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
			id:    atomic.AddUint32(&connID, 1),
			local: localAddr,
			peer:  peerAddr,
		},
	}
	conn.EnableWriteCompression(false)
	conn.SetPingHandler(gettyWSConn.handlePing)
	conn.SetPongHandler(gettyWSConn.handlePong)

	return gettyWSConn
}

// set compress type
func (w *gettyWSConn) SetCompressType(c CompressType) {
	switch c {
	case CompressNone, CompressZip, CompressBestSpeed, CompressBestCompression, CompressHuffman:
		w.conn.EnableWriteCompression(true)
		w.conn.SetCompressionLevel(int(c))

	default:
		panic(fmt.Sprintf("illegal comparess type %d", c))
	}
}

func (w *gettyWSConn) handlePing(message string) error {
	err := w.conn.WriteMessage(websocket.PongMessage, []byte(message))
	if err == websocket.ErrCloseSent {
		err = nil
	} else if e, ok := err.(net.Error); ok && e.Temporary() {
		err = nil
	}
	if err == nil {
		w.UpdateActive()
	}

	return err
}

func (w *gettyWSConn) handlePong(string) error {
	w.UpdateActive()
	return nil
}

// websocket connection read
func (w *gettyWSConn) read() ([]byte, error) {
	// w.conn.SetReadDeadline(time.Now().Add(w.rDeadline))
	_, b, e := w.conn.ReadMessage() // the first return value is message type.
	if e == nil {
		// atomic.AddUint32(&w.readCount, (uint32)(l))
		atomic.AddUint32(&w.readPkgCount, 1)
	} else {
		if websocket.IsUnexpectedCloseError(e, websocket.CloseGoingAway) {
			log.Warn("websocket unexpected close error: %v", e)
		}
	}

	return b, e
}

// websocket connection write
func (w *gettyWSConn) Write(p []byte) error {
	// atomic.AddUint32(&w.writeCount, 1)
	atomic.AddUint32(&w.writeCount, (uint32)(len(p)))
	// w.conn.SetWriteDeadline(time.Now().Add(w.wDeadline))
	return w.conn.WriteMessage(websocket.BinaryMessage, p)
}

func (w *gettyWSConn) writePing() error {
	return w.conn.WriteMessage(websocket.PingMessage, []byte{})
}

// close websocket connection
func (w *gettyWSConn) close(waitSec int) {
	w.conn.WriteMessage(websocket.CloseMessage, []byte("bye-bye!!!"))
	conn := w.conn.UnderlyingConn()
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetLinger(waitSec)
	} else if wsConn, ok := conn.(*tls.Conn); ok {
		wsConn.CloseWrite()
	}
	w.conn.Close()
}
