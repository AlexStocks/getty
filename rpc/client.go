package rpc

import (
	"math/rand"
	"sync"
	"time"
)

import (
	"github.com/AlexStocks/getty"
	"github.com/AlexStocks/goext/sync/atomic"
	jerrors "github.com/juju/errors"
)

var (
	errInvalidCodecType  = jerrors.New("illegal CodecType")
	errInvalidAddress    = jerrors.New("remote address invalid or empty")
	errSessionNotExist   = jerrors.New("session not exist")
	errClientClosed      = jerrors.New("client closed")
	errClientReadTimeout = jerrors.New("client read timeout")
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Client struct {
	conf     ClientConfig
	pool     *gettyRPCClientConnPool
	sequence gxatomic.Uint64

	pendingLock      sync.RWMutex
	pendingResponses map[SequenceType]*PendingResponse
}

func NewClient(conf *ClientConfig) (*Client, error) {
	if err := conf.CheckValidity(); err != nil {
		return nil, jerrors.Trace(err)
	}

	c := &Client{
		pendingResponses: make(map[SequenceType]*PendingResponse),
		conf:             *conf,
	}
	c.pool = newGettyRPCClientConnPool(c, conf.PoolSize, time.Duration(int(time.Second)*conf.PoolTTL))

	return c, nil
}

func (c *Client) Call(typ CodecType, addr, service, method string, args interface{}, reply interface{}) error {
	if !typ.CheckValidity() {
		return errInvalidCodecType
	}

	b := &GettyRPCRequest{}
	b.header.Service = service
	b.header.Method = method
	b.header.CallType = CT_TwoWay
	if reply == nil {
		b.header.CallType = CT_TwoWayNoReply
	}
	b.body = args

	rsp := NewPendingResponse()
	rsp.reply = reply

	var (
		err     error
		session getty.Session
		conn    *gettyRPCClientConn
	)
	conn, session, err = c.selectSession(typ, addr)
	if err != nil || session == nil {
		return errSessionNotExist
	}
	defer c.pool.release(conn, err)

	if err = c.transfer(session, typ, b, rsp); err != nil {
		return jerrors.Trace(err)
	}

	select {
	case <-getty.GetTimeWheel().After(c.conf.GettySessionParam.tcpReadTimeout):
		err = errClientReadTimeout
		c.removePendingResponse(SequenceType(rsp.seq))
	case <-rsp.done:
		err = rsp.err
	}

	return jerrors.Trace(err)
}

func (c *Client) Close() {
	if c.pool != nil {
		c.pool.close()
	}
	c.pool = nil
}

func (c *Client) selectSession(typ CodecType, addr string) (*gettyRPCClientConn, getty.Session, error) {
	rpcConn, err := c.pool.getConn(typ.String(), addr)
	if err != nil {
		return nil, nil, jerrors.Trace(err)
	}
	return rpcConn, rpcConn.selectSession(), nil
}

func (c *Client) heartbeat(session getty.Session, typ CodecType) error {
	rsp := NewPendingResponse()
	return c.transfer(session, typ, nil, rsp)
}

func (c *Client) transfer(session getty.Session, typ CodecType, req *GettyRPCRequest, rsp *PendingResponse) error {
	var (
		sequence uint64
		err      error
		pkg      GettyPackage
	)

	sequence = c.sequence.Add(1)
	pkg.H.Magic = MagicType(gettyPackageMagic)
	pkg.H.LogID = LogIDType(randomID())
	pkg.H.Sequence = SequenceType(sequence)
	pkg.H.Command = gettyCmdHbRequest
	pkg.H.CodecType = typ
	if req != nil {
		pkg.H.Command = gettyCmdRPCRequest
		pkg.B = req
	}

	rsp.seq = sequence
	c.addPendingResponse(rsp)

	err = session.WritePkg(pkg, 0)
	if err != nil {
		c.removePendingResponse(SequenceType(rsp.seq))
	}

	return jerrors.Trace(err)
}

// func (c *Client) PendingResponseCount() int {
// 	c.pendingLock.RLock()
// 	defer c.pendingLock.RUnlock()
// 	return len(c.pendingResponses)
// }

func (c *Client) addPendingResponse(pr *PendingResponse) {
	c.pendingLock.Lock()
	defer c.pendingLock.Unlock()
	c.pendingResponses[SequenceType(pr.seq)] = pr
}

func (c *Client) removePendingResponse(seq SequenceType) *PendingResponse {
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

// func (c *Client) ClearPendingResponses() map[SequenceType]*PendingResponse {
// 	c.pendingLock.Lock()
// 	defer c.pendingLock.Unlock()
// 	presps := c.pendingResponses
// 	c.pendingResponses = nil
// 	return presps
// }
