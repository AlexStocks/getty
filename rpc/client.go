package rpc

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

import (
	jerrors "github.com/juju/errors"
)

import (
	"github.com/AlexStocks/getty"
	"github.com/AlexStocks/goext/net"
	log "github.com/AlexStocks/log4go"
)

var (
	errInvalidAddress  = jerrors.New("remote address invalid or empty")
	errSessionNotExist = jerrors.New("session not exist")
	errClientClosed    = jerrors.New("client closed")
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Client struct {
	conf        *Config
	lock        sync.RWMutex
	sessions    []*rpcSession
	gettyClient getty.Client

	sequence uint64

	pendingLock      sync.RWMutex
	pendingResponses map[uint64]*PendingResponse

	sendLock sync.Mutex
}

func NewClient(conf *Config) *Client {
	c := &Client{
		pendingResponses: make(map[uint64]*PendingResponse),
		conf:             conf,
		gettyClient: getty.NewTCPClient(
			getty.WithServerAddress(gxnet.HostAddress(conf.ServerHost, conf.ServerPort)),
			getty.WithConnectionNumber((int)(conf.ConnectionNum)),
		),
	}
	c.gettyClient.RunEventLoop(c.newSession)
	for {
		if c.isAvailable() {
			break
		}
		time.Sleep(1e6)
	}
	log.Info("client init ok")

	return c
}

func (c *Client) newSession(session getty.Session) error {
	var (
		ok      bool
		tcpConn *net.TCPConn
	)

	if conf.GettySessionParam.CompressEncoding {
		session.SetCompressType(getty.CompressZip)
	}

	if tcpConn, ok = session.Conn().(*net.TCPConn); !ok {
		panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp connection\n", session.Stat(), session.Conn()))
	}

	tcpConn.SetNoDelay(conf.GettySessionParam.TcpNoDelay)
	tcpConn.SetKeepAlive(conf.GettySessionParam.TcpKeepAlive)
	if conf.GettySessionParam.TcpKeepAlive {
		tcpConn.SetKeepAlivePeriod(conf.GettySessionParam.keepAlivePeriod)
	}
	tcpConn.SetReadBuffer(conf.GettySessionParam.TcpRBufSize)
	tcpConn.SetWriteBuffer(conf.GettySessionParam.TcpWBufSize)

	session.SetName(conf.GettySessionParam.SessionName)
	session.SetMaxMsgLen(conf.GettySessionParam.MaxMsgLen)
	session.SetPkgHandler(NewRpcClientPacketHandler())
	session.SetEventListener(NewRpcClientHandler(c))
	session.SetRQLen(conf.GettySessionParam.PkgRQSize)
	session.SetWQLen(conf.GettySessionParam.PkgWQSize)
	session.SetReadTimeout(conf.GettySessionParam.tcpReadTimeout)
	session.SetWriteTimeout(conf.GettySessionParam.tcpWriteTimeout)
	session.SetCronPeriod((int)(conf.heartbeatPeriod.Nanoseconds() / 1e6))
	session.SetWaitTime(conf.GettySessionParam.waitTimeout)
	log.Debug("client new session:%s\n", session.Stat())

	return nil
}

func (c *Client) Sequence() uint64 {
	return atomic.AddUint64(&c.sequence, 1)
}

func (c *Client) Call(service, method string, args interface{}, reply interface{}) error {
	req := NewRpcRequest(nil)
	req.header.Service = service
	req.header.Method = method
	if reply == nil {
		req.header.CallType = RequestSendOnly
	}
	req.body = args

	resp := NewPendingResponse()
	resp.reply = reply

	session := c.selectSession()
	if session != nil {
		if err := c.transfer(session, req, resp); err != nil {
			return err
		}
		<-resp.done
		return resp.err
	}

	return errSessionNotExist
}

func (c *Client) isAvailable() bool {
	if c.selectSession() == nil {
		return false
	}

	return true
}

func (c *Client) Close() {
	var sessions *[]*rpcSession

	c.lock.Lock()
	if c.gettyClient != nil {
		sessions = &(c.sessions)
		c.sessions = nil
		c.gettyClient.Close()
		c.gettyClient = nil
		c.sessions = c.sessions[:0]
	}
	c.lock.Unlock()

	if sessions != nil {
		for _, s := range *sessions {
			log.Info("close client session{%s, last active:%s, request number:%d}",
				s.session.Stat(), s.session.GetActive().String(), s.reqNum)
			s.session.Close()
		}
	}
}

func (c *Client) selectSession() getty.Session {
	c.lock.RLock()
	defer c.lock.RUnlock()
	if c.sessions == nil {
		return nil
	}

	count := len(c.sessions)
	if count == 0 {
		return nil
	}
	return c.sessions[rand.Int31n(int32(count))].session
}

func (c *Client) addSession(session getty.Session) {
	log.Debug("add session{%s}", session.Stat())
	if session == nil {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	if c.sessions == nil {
		return
	}

	c.sessions = append(c.sessions, &rpcSession{session: session})
}

func (c *Client) removeSession(session getty.Session) {
	if session == nil {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	if c.sessions == nil {
		return
	}

	for i, s := range c.sessions {
		if s.session == session {
			c.sessions = append(c.sessions[:i], c.sessions[i+1:]...)
			log.Debug("delete session{%s}, its index{%d}", session.Stat(), i)
			break
		}
	}
	log.Info("after remove session{%s}, left session number:%d", session.Stat(), len(c.sessions))
}

func (c *Client) updateSession(session getty.Session) {
	if session == nil {
		return
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.sessions == nil {
		return
	}

	for i, s := range c.sessions {
		if s.session == session {
			c.sessions[i].reqNum++
			break
		}
	}
}

func (c *Client) getClientRpcSession(session getty.Session) (rpcSession, error) {
	var (
		err        error
		rpcSession rpcSession
	)
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.sessions == nil {
		return rpcSession, errClientClosed
	}

	err = errSessionNotExist
	for _, s := range c.sessions {
		if s.session == session {
			rpcSession = *s
			err = nil
			break
		}
	}
	return rpcSession, err
}

func (c *Client) ping(session getty.Session) error {
	req := NewRpcRequest(nil)
	req.header.Service = "go"
	req.header.Method = "ping"
	req.header.CallType = RequestSendOnly
	req.body = nil

	resp := NewPendingResponse()
	return c.transfer(session, req, resp)
}

func (c *Client) transfer(session getty.Session, req *RpcRequest, resp *PendingResponse) error {
	var (
		sequence uint64
		err      error
	)

	sequence = c.Sequence()
	req.header.Seq = sequence
	resp.seq = sequence
	c.AddPendingResponse(resp)

	c.sendLock.Lock()
	defer c.sendLock.Unlock()
	err = session.WritePkg(req, 0)
	if err != nil {
		c.RemovePendingResponse(resp.seq)
	}
	return err
}

func (c *Client) PendingResponseCount() int {
	c.pendingLock.RLock()
	defer c.pendingLock.RUnlock()
	return len(c.pendingResponses)
}
func (c *Client) AddPendingResponse(pr *PendingResponse) {
	c.pendingLock.Lock()
	defer c.pendingLock.Unlock()
	c.pendingResponses[pr.seq] = pr
}

func (c *Client) RemovePendingResponse(seq uint64) *PendingResponse {
	c.pendingLock.Lock()
	defer c.pendingLock.Unlock()
	if c.pendingResponses == nil {
		return nil
	}
	if presp, ok := c.pendingResponses[seq]; ok {
		delete(c.pendingResponses, seq)
		return presp
	}
	return nil
}

func (c *Client) ClearPendingResponses() map[uint64]*PendingResponse {
	c.pendingLock.Lock()
	defer c.pendingLock.Unlock()
	presps := c.pendingResponses
	c.pendingResponses = nil
	return presps
}
