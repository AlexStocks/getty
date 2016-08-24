/******************************************************
# DESC       : session
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2016-08-17 11:21
# FILE       : session.go
******************************************************/

package getty

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
)

const (
	maxReadBufLen      = 4 * 1024
	netIOTimeout       = 100e6    // 100ms
	cronPeriod         = 60 * 1e9 // 1 minute
	pendingDuration    = 3e9
	defaultSessionName = "Session"
	outputFormat       = "session %s, Read Count: %d, Write Count: %d, Read Pkg Count: %d, Write Pkg Count: %d"
)

var (
	connId            uint32 = 0
	ErrSessionClosed         = errors.New("Session Already Closed")
	ErrSessionBlocked        = errors.New("Session full blocked")
)

/////////////////////////////////////////
// getty connection
/////////////////////////////////////////

type gettyConn struct {
	conn          net.Conn
	Id            uint32
	readCount     uint32 // read() count
	writeCount    uint32 // write() count
	readPkgCount  uint32 // send pkg count
	writePkgCount uint32 // recv pkg count
}

func newGettyConn(conn net.Conn) *gettyConn {
	return &gettyConn{conn: conn, Id: atomic.AddUint32(&connId, 1)}
}

func (this *gettyConn) read(p []byte) (int, error) {
	atomic.AddUint32(&this.readCount, 1)
	return this.conn.Read(p)
}

func (this *gettyConn) write(p []byte) (int, error) {
	atomic.AddUint32(&this.writeCount, 1)
	return this.conn.Write(p)
}

func (this *gettyConn) incReadPkgCount() {
	atomic.AddUint32(&this.readPkgCount, 1)
}

func (this *gettyConn) incWritePkgCount() {
	atomic.AddUint32(&this.writePkgCount, 1)
}

/////////////////////////////////////////
// session
/////////////////////////////////////////

// getty base session
type Session struct {
	name string
	// net read write
	*gettyConn
	pkgHandler ReadWriter
	listener   EventListener
	done       chan struct{}
	readerDone chan struct{} // end reader

	cronPeriod    time.Duration
	readDeadline  time.Duration
	writeDeadline time.Duration
	closeWait     time.Duration
	reqQ          chan interface{}
	rspQ          chan interface{}

	// attribute
	attrs map[string]interface{}
	// goroutines sync
	grNum int32
	lock sync.RWMutex
}

func NewSession(conn net.Conn) *Session {
	return &Session{
		name:          defaultSessionName,
		gettyConn:     newGettyConn(conn),
		done:          make(chan struct{}),
		readerDone:    make(chan struct{}),
		cronPeriod:    cronPeriod,
		readDeadline:  netIOTimeout,
		writeDeadline: netIOTimeout,
		closeWait:     pendingDuration,
		attrs:         make(map[string]interface{}),
	}
}

func (this *Session) Conn() net.Conn { return this.conn }

func (this *Session) Stat() string {
	return fmt.Sprintf(
		outputFormat,
		this.sessionToken(),
		atomic.LoadUint32(&(this.readCount)),
		atomic.LoadUint32(&(this.writeCount)),
		atomic.LoadUint32(&(this.readPkgCount)),
		atomic.LoadUint32(&(this.writePkgCount)),
	)
}

func (this *Session) IsClosed() bool {
	select {
	case <-this.done:
		return true
	default:
		return false
	}

	return false
}

func (this *Session) SetName(name string) { this.name = name }

func (this *Session) SetEventListener(listener EventListener) {
	this.listener = listener
}

// set package handler
func (this *Session) SetPkgHandler(handler ReadWriter) {
	this.pkgHandler = handler
}

// period is in millisecond
func (this *Session) SetCronPeriod(period int) {
	if period < 1 {
		panic("@period < 1")
	}

	this.lock.Lock()
	this.cronPeriod = time.Duration(period) * time.Millisecond
	this.lock.Unlock()
}

func (this *Session) SetReadChanLen(readChanLen int) {
	if readChanLen < 1 {
		panic("@readChanLen < 1")
	}

	this.lock.Lock()
	this.reqQ = make(chan interface{}, readChanLen)
	this.lock.Unlock()
}

func (this *Session) SetWriteChanLen(writeChanLen int) {
	if writeChanLen < 1 {
		panic("@writeChanLen < 1")
	}

	this.lock.Lock()
	this.rspQ = make(chan interface{}, writeChanLen)
	this.lock.Unlock()
}

func (this *Session) SetReadDeadline(readDeadline time.Duration) {
	if readDeadline < 1 {
		panic("@readDeadline < 1")
	}

	this.lock.Lock()
	this.readDeadline = readDeadline
	if this.writeDeadline == 0 {
		this.writeDeadline = readDeadline
	}
	this.lock.Unlock()
}

func (this *Session) SetWriteDeadline(writeDeadline time.Duration) {
	if writeDeadline < 1 {
		panic("@writeDeadline < 1")
	}

	this.lock.Lock()
	this.writeDeadline = writeDeadline
	this.lock.Unlock()
}

func (this *Session) SetCloseWait(closeWait time.Duration) {
	if closeWait < 1 {
		panic("@closeWait < 1")
	}

	this.lock.Lock()
	this.closeWait = closeWait
	this.lock.Unlock()
}

func (this *Session) GetAttribute(key string) interface{} {
	var ret interface{}
	this.lock.RLock()
	ret = this.attrs[key]
	this.lock.RUnlock()
	return ret
}

func (this *Session) SetAttribute(key string, value interface{}) {
	this.lock.Lock()
	this.attrs[key] = value
	this.lock.Unlock()
}

func (this *Session) RemoveAttribute(key string) {
	this.lock.Lock()
	delete(this.attrs, key)
	this.lock.Unlock()
}

func (this *Session) stop() {
	select {
	case <-this.done:
		return
	default:
		close(this.done)
	}
}

func (this *Session) Close() {
	this.stop()
	log.Info("%s closed now, its current gr num %d",
		this.sessionToken(), atomic.LoadInt32(&(this.grNum)))
}

func (this *Session) sessionToken() string {
	return fmt.Sprintf(
		"%s:%d:%s:%s",
		this.name, this.Id,
		this.conn.LocalAddr().String(),
		this.conn.RemoteAddr().String(),
	)
}

// Queued write, for handler
func (this *Session) WritePkg(pkg interface{}) error {
	if this.IsClosed() {
		return ErrSessionClosed
	}

	defer func() {
		if err := recover(); err != nil {
			log.Error("%s [session.WritePkg] err=%+v\n", this.sessionToken(), err)
		}
	}()

	select {
	case this.rspQ <- pkg:
		break // for possible gen a new pkg
	default:
		return ErrSessionBlocked
	}

	return nil
}

// for codecs
func (this *Session) WriteBytes(pkg []byte) error {
	this.conn.SetWriteDeadline(time.Now().Add(this.readDeadline))
	_, err := this.write(pkg)
	return err
}

func (this *Session) dispose() {
	this.conn.Close()
}

func (this *Session) RunEventloop() {
	if this.reqQ == nil || this.rspQ == nil {
		errStr := fmt.Sprintf("Session{name:%s, reqQ:%#v, rspQ:%#v}",
			this.name, this.reqQ, this.rspQ)
		log.Error(errStr)
		panic(errStr)
	}
	if this.conn == nil || this.pkgHandler == nil {
		errStr := fmt.Sprintf("Session{name:%s, conn:%#v, pkgHandler:%#v}",
			this.name, this.conn, this.pkgHandler)
		log.Error(errStr)
		panic(errStr)
	}

	atomic.AddInt32(&(this.grNum), 2)
	go this.handleLoop()
	go this.handlePackage()
}

func (this *Session) handleLoop() {
	var (
		err    error
		start  time.Time
		ticker *time.Ticker
		reqPkg interface{}
		rspPkg interface{}
	)

	defer func() {
		var grNum int32
		if err := recover(); err != nil {
			log.Error("%s, [session.handleLoop] err=%+v\n", this.sessionToken(), err)
		}
		close(this.rspQ)
		close(this.reqQ)
		grNum = atomic.AddInt32(&(this.grNum), -1)
		if this.listener != nil {
			this.listener.OnClose(this)
		}
		// real close connection, dispose会调用(conn)Close,
		this.dispose()
		log.Info("statistic{%s}, [session.handleLoop] goroutine exit now, left gr num %d", this.Stat(), grNum)
	}()

	// call session opened
	if this.listener != nil {
		this.listener.OnOpen(this)
	}

	ticker = time.NewTicker(this.cronPeriod)
LOOP:
	for {
		select {
		case <-this.done:
			log.Info("%s, [session.handleLoop] got done signal ", this.Stat())
			break LOOP
		case reqPkg = <-this.reqQ:
			if this.listener != nil {
				this.incReadPkgCount()
				this.listener.OnMessage(this, reqPkg)
			}
		case rspPkg = <-this.rspQ:
			if err = this.pkgHandler.Write(this, rspPkg); err != nil {
				log.Error("%s, [session.handleLoop] = error{%+v}", this.sessionToken(), err)
				break LOOP
			}
			this.incWritePkgCount()
		case <-ticker.C:
			if this.listener != nil {
				this.listener.OnCron(this)
			}
		}
	}
	ticker.Stop()

	this.stop()
	// wait for reader goroutine closed
	<-this.readerDone

	// process pending pkg
	start = time.Now()
LAST:
	for {
		if time.Since(start).Nanoseconds() >= this.closeWait.Nanoseconds() {
			break
		}

		select {
		case rspPkg = <-this.rspQ:
			if err = this.pkgHandler.Write(this, rspPkg); err != nil {
				break LAST
			}
			this.incWritePkgCount()
		case reqPkg = <-this.reqQ:
			if this.listener != nil {
				this.incReadPkgCount()
				this.listener.OnMessage(this, reqPkg)
			}
		default:
			log.Info("%s, [session.handleLoop] default", this.sessionToken())
			break LAST
		}
	}
}

// get package from tcp stream(packet)
func (this *Session) handlePackage() {
	var (
		err       error
		nerr      net.Error
		ok        bool
		exit      bool
		reconnect bool
		len       int
		buf       []byte
		pktBuf    *bytes.Buffer
		pkg       interface{}
	)

	defer func() {
		var grNum int32
		if err := recover(); err != nil {
			log.Error("%s, [session.handlePackage] = err{%+v}", this.sessionToken(), err)
		}
		this.stop()
		close(this.readerDone)
		grNum = atomic.AddInt32(&(this.grNum), -1)
		log.Info("%s, [session.handlePackage] gr will exit now, left gr num %d", this.sessionToken(), grNum)
		if reconnect && this.listener != nil {
			log.Info("%s, [session.handlePackage] reconnect", this.sessionToken())
			this.listener.OnError(this, nerr)
		}
	}()

	buf = make([]byte, maxReadBufLen)
	pktBuf = new(bytes.Buffer)
	for {
		if this.IsClosed() {
			exit = true
		}

		for {
			if exit {
				break // 退出前不再读取任何packet，跳到下个for-loop处理完pktBuf中的stream
			}
			this.conn.SetReadDeadline(time.Now().Add(this.readDeadline))
			len, err = this.read(buf)
			if err != nil {
				if nerr, ok = err.(net.Error); ok && nerr.Timeout() {
					break
				}
				log.Error("%s, [session.conn.read] = error{%v}", this.sessionToken(), err)
				// 遇到网络错误的时候，handlePackage能够及时退出，但是handleLoop的第一个for-select因为要处理(Codec)OnMessage
				// 导致程序不能及时退出，此处添加(Codec)OnError调用以及时通知getty调用者
				// AS, 2016/08/21
				reconnect = true
				exit = true
			}
			break
		}
		if 0 < len {
			pktBuf.Write(buf[:len])
		}
		for {
			if pktBuf.Len() <= 0 {
				break
			}
			pkg, err = this.pkgHandler.Read(this, pktBuf)
			if err != nil {
				log.Info("%s, [session.pkgHandler.Read] = error{%+v}", this.sessionToken(), err)
				exit = true
				break
			}
			if pkg == nil {
				break
			}
			this.reqQ <- pkg
		}
		if exit {
			break
		}
	}
}
