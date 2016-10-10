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
	"net"
	"sync/atomic"
)

import (
	"github.com/gorilla/websocket"
)

var (
// ErrInvalidConnection = errors.New("connection has been closed.")
)

type iConn interface {
	incReadPkgCount()
	incWritePkgCount()
	write(p []byte) error
	close(int)
}

/////////////////////////////////////////
// getty connection
/////////////////////////////////////////

var (
	connID uint32
)

type gettyConn struct {
	ID            uint32
	readCount     uint32 // read() count
	writeCount    uint32 // write() count
	readPkgCount  uint32 // send pkg count
	writePkgCount uint32 // recv pkg count
	local         string // local address
	peer          string // peer address
}

func (this *gettyConn) incReadPkgCount() {
	atomic.AddUint32(&this.readPkgCount, 1)
}

func (this *gettyConn) incWritePkgCount() {
	atomic.AddUint32(&this.writePkgCount, 1)
}

func (this *gettyConn) write([]byte) error {
	return nil
}

func (this *gettyConn) close(int) {}

/////////////////////////////////////////
// getty tcp connection
/////////////////////////////////////////

type gettyTCPConn struct {
	gettyConn
	conn net.Conn
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
		conn: conn,
		gettyConn: gettyConn{
			ID:    atomic.AddUint32(&connID, 1),
			local: localAddr,
			peer:  peerAddr,
		},
	}
}

// tcp connection read
func (this *gettyTCPConn) read(p []byte) (int, error) {
	// if this.conn == nil {
	//	return 0, ErrInvalidConnection
	// }
	// atomic.AddUint32(&this.readCount, 1)
	atomic.AddUint32(&this.readCount, (uint32)(len(p)))
	return this.conn.Read(p)
}

// tcp connection write
func (this *gettyTCPConn) write(p []byte) error {
	// if this.conn == nil {
	//	return 0, ErrInvalidConnection
	// }

	// atomic.AddUint32(&this.writeCount, 1)
	atomic.AddUint32(&this.writeCount, (uint32)(len(p)))
	_, err := this.conn.Write(p)
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
	conn websocket.Conn
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

	return &gettyWSConn{
		conn: *conn,
		gettyConn: gettyConn{
			ID:    atomic.AddUint32(&connID, 1),
			local: localAddr,
			peer:  peerAddr,
		},
	}
}

// websocket connection read
func (this *gettyWSConn) read() ([]byte, error) {
	// l, b, e := this.conn.ReadMessage()
	_, b, e := this.conn.ReadMessage()
	if e == nil {
		// atomic.AddUint32(&this.readCount, (uint32)(l))
		atomic.AddUint32(&this.readPkgCount, 1)
	}

	return b, e
}

// websocket connection write
func (this *gettyWSConn) write(p []byte) error {
	// atomic.AddUint32(&this.writeCount, 1)
	atomic.AddUint32(&this.writeCount, (uint32)(len(p)))
	return this.conn.WriteMessage(len(p), p)
}

// close websocket connection
func (this *gettyWSConn) close(waitSec int) {
	this.conn.UnderlyingConn().(*net.TCPConn).SetLinger(waitSec)
	this.conn.Close()
}
