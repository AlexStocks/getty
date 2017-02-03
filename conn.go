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

type CompressType byte

const (
	CompressNone   CompressType = 0x00
	CompressZip                 = 0x01
	CompressSnappy              = 0x02
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

func (this *gettyConn) ID() uint32 {
	return this.id
}

func (this *gettyConn) LocalAddr() string {
	return this.local
}

func (this *gettyConn) RemoteAddr() string {
	return this.peer
}

func (this *gettyConn) incReadPkgCount() {
	atomic.AddUint32(&this.readPkgCount, 1)
}

func (this *gettyConn) incWritePkgCount() {
	atomic.AddUint32(&this.writePkgCount, 1)
}

func (this *gettyConn) UpdateActive() {
	atomic.StoreInt64(&(this.active), int64(time.Since(launchTime)))
}

func (this *gettyConn) GetActive() time.Time {
	return launchTime.Add(time.Duration(atomic.LoadInt64(&(this.active))))
}

func (this *gettyConn) Write([]byte) error {
	return nil
}

func (this *gettyConn) close(int) {}

func (this gettyConn) readDeadline() time.Duration {
	return this.rDeadline
}

func (this *gettyConn) SetReadDeadline(rDeadline time.Duration) {
	if rDeadline < 1 {
		panic("@rDeadline < 1")
	}

	this.rDeadline = rDeadline
	if this.wDeadline == 0 {
		this.wDeadline = rDeadline
	}
}

func (this gettyConn) writeDeadline() time.Duration {
	return this.wDeadline
}

func (this *gettyConn) SetWriteDeadline(wDeadline time.Duration) {
	if wDeadline < 1 {
		panic("@wDeadline < 1")
	}

	this.wDeadline = wDeadline
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

func (this *writeFlusher) Write(p []byte) (int, error) {
	var (
		n   int
		err error
	)

	n, err = this.flusher.Write(p)
	if err != nil {
		return n, err
	}
	if err := this.flusher.Flush(); err != nil {
		return 0, err
	}

	return n, nil
}

// set compress type(tcp: zip/snappy, websocket:zip)
func (this *gettyTCPConn) SetCompressType(t CompressType) {
	switch {
	case t == CompressZip:
		this.reader = flate.NewReader(this.conn)

		w, err := flate.NewWriter(this.conn, flate.DefaultCompression)
		if err != nil {
			panic(fmt.Sprintf("flate.NewReader(flate.DefaultCompress) = err(%s)", err))
		}
		this.writer = &writeFlusher{flusher: w}

	case t == CompressSnappy:
		this.reader = snappy.NewReader(this.conn)
		this.writer = snappy.NewWriter(this.conn)
	}
}

// tcp connection read
func (this *gettyTCPConn) read(p []byte) (int, error) {
	// if this.conn == nil {
	//	return 0, ErrInvalidConnection
	// }

	// atomic.AddUint32(&this.readCount, 1)
	// l, e := this.conn.Read(p)
	l, e := this.reader.Read(p)
	atomic.AddUint32(&this.readCount, uint32(l))
	return l, e
}

// tcp connection write
func (this *gettyTCPConn) Write(p []byte) error {
	// if this.conn == nil {
	//	return 0, ErrInvalidConnection
	// }

	// atomic.AddUint32(&this.writeCount, 1)
	atomic.AddUint32(&this.writeCount, (uint32)(len(p)))
	// _, err := this.conn.Write(p)
	_, err := this.writer.Write(p)

	return err
}

// close tcp connection
func (this *gettyTCPConn) close(waitSec int) {
	// if tcpConn, ok := this.conn.(*net.TCPConn); ok {
	// tcpConn.SetLinger(0)
	// }

	if this.conn != nil {
		this.conn.(*net.TCPConn).SetLinger(waitSec)
		this.conn.Close()
		this.conn = nil
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

// set compress type(tcp: zip/snappy, websocket:zip)
func (this *gettyWSConn) SetCompressType(t CompressType) {
	switch {
	case t == CompressZip:
		this.conn.EnableWriteCompression(true)
	case t == CompressSnappy:
		this.conn.EnableWriteCompression(true)
	default:
		this.conn.EnableWriteCompression(false)
	}
}

func (this *gettyWSConn) handlePing(message string) error {
	err := this.conn.WriteMessage(websocket.PongMessage, []byte(message))
	if err == websocket.ErrCloseSent {
		err = nil
	} else if e, ok := err.(net.Error); ok && e.Temporary() {
		err = nil
	}
	if err == nil {
		this.UpdateActive()
	}

	return err
}

func (this *gettyWSConn) handlePong(string) error {
	this.UpdateActive()
	return nil
}

// websocket connection read
func (this *gettyWSConn) read() ([]byte, error) {
	// this.conn.SetReadDeadline(time.Now().Add(this.rDeadline))
	_, b, e := this.conn.ReadMessage() // the first return value is message type.
	if e == nil {
		// atomic.AddUint32(&this.readCount, (uint32)(l))
		atomic.AddUint32(&this.readPkgCount, 1)
	} else {
		if websocket.IsUnexpectedCloseError(e, websocket.CloseGoingAway) {
			log.Warn("websocket unexpected close error: %v", e)
		}
	}

	return b, e
}

// websocket connection write
func (this *gettyWSConn) Write(p []byte) error {
	// atomic.AddUint32(&this.writeCount, 1)
	atomic.AddUint32(&this.writeCount, (uint32)(len(p)))
	// this.conn.SetWriteDeadline(time.Now().Add(this.wDeadline))
	return this.conn.WriteMessage(websocket.BinaryMessage, p)
}

func (this *gettyWSConn) writePing() error {
	return this.conn.WriteMessage(websocket.PingMessage, []byte{})
}

// close websocket connection
func (this *gettyWSConn) close(waitSec int) {
	this.conn.WriteMessage(websocket.CloseMessage, []byte("bye-bye!!!"))
	this.conn.UnderlyingConn().(*net.TCPConn).SetLinger(waitSec)
	this.conn.Close()
}
