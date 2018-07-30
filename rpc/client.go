package rpc

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

import (
	"github.com/AlexStocks/getty"
	"github.com/AlexStocks/goext/database/filter"
	"github.com/AlexStocks/goext/database/filter/pool"
	"github.com/AlexStocks/goext/database/registry"
	"github.com/AlexStocks/goext/database/registry/etcdv3"
	"github.com/AlexStocks/goext/database/registry/zookeeper"
	"github.com/AlexStocks/goext/net"
	"github.com/AlexStocks/goext/sync/atomic"
	jerrors "github.com/juju/errors"
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
	conf     *ClientConfig
	pool     *gettyRPCClientConnPool
	sequence gxatomic.Uint64

	pendingLock      sync.RWMutex
	pendingResponses map[uint64]*PendingResponse

	// registry
	registry gxregistry.Registry
	sa       gxregistry.ServiceAttr
	filter   gxfilter.Filter
}

func NewClient(confFile string) (*Client, error) {
	conf := loadClientConf(confFile)
	c := &Client{
		pendingResponses: make(map[uint64]*PendingResponse),
		conf:             conf,
	}
	c.pool = newGettyRPCClientConnPool(c, conf.PoolSize, time.Duration(int(time.Second)*conf.PoolTTL))

	if len(c.conf.Registry.Addr) == 0 {
		if conf.codecType = String2CodecType(conf.CodecType); conf.codecType == gettyCodecUnknown {
			return nil, jerrors.New(fmt.Sprintf(ErrIllegalConf+"codec type %s", conf.CodecType))
		}
		if conf.ServerPort == 0 {
			return nil, jerrors.New(fmt.Sprintf(ErrIllegalConf + "both registry addr and ServerPort is nil"))
		}

		_, err := c.pool.getConn(conf.CodecType, gxnet.HostAddress(conf.ServerHost, conf.ServerPort))

		return c, jerrors.Trace(err)
	}

	var err error
	var registry gxregistry.Registry
	addrList := strings.Split(c.conf.Registry.Addr, ",")
	switch c.conf.Registry.Type {
	case "etcd":
		registry, err = gxetcd.NewRegistry(
			gxregistry.WithAddrs(addrList...),
			gxregistry.WithTimeout(time.Duration(int(time.Second)*c.conf.Registry.KeepaliveTimeout)),
			gxregistry.WithRoot(c.conf.Registry.Root),
		)
	case "zookeeper":
		registry, err = gxzookeeper.NewRegistry(
			gxregistry.WithAddrs(addrList...),
			gxregistry.WithTimeout(time.Duration(int(time.Second)*c.conf.Registry.KeepaliveTimeout)),
			gxregistry.WithRoot(c.conf.Registry.Root),
		)
	default:
		return nil, jerrors.New(fmt.Sprintf(ErrIllegalConf+"registry type %s", c.conf.Registry.Type))
	}
	if err != nil {
		return nil, jerrors.Trace(err)
	}

	c.registry = registry
	if c.registry != nil {
		c.filter, err = gxpool.NewFilter(
			gxfilter.WithBalancerMode(gxfilter.SM_Hash),
			gxfilter.WithRegistry(c.registry),
			gxpool.WithTTL(time.Duration(int(time.Second)*c.conf.Registry.KeepaliveTimeout)),
		)
		if err == nil {
			return nil, jerrors.Trace(err)
		}

		service := gxregistry.Service{
			Attr: &gxregistry.ServiceAttr{
				Group:    c.conf.Registry.IDC,
				Role:     gxregistry.SRT_Consumer,
				Protocol: c.conf.CodecType,
			},
			Nodes: []*gxregistry.Node{&gxregistry.Node{
				ID:      c.conf.Registry.NodeID,
				Address: c.conf.Host,
				Port:    0,
			},
			},
		}
		if err = c.registry.Register(service); err != nil {
			return nil, jerrors.Trace(err)
		}
	}

	return c, nil
}

func (c *Client) Call(addr, protocol, service, method string, args interface{}, reply interface{}) error {
	b := &GettyRPCRequest{}
	b.header.Service = service
	b.header.Method = method
	b.header.CallType = gettyTwoWay
	if reply == nil {
		b.header.CallType = gettyTwoWayNoReply
	}
	b.body = args

	resp := NewPendingResponse()
	resp.reply = reply

	session := c.selectSession(protocol, addr)
	if session == nil {
		return errSessionNotExist
	}

	if err := c.transfer(session, b, resp); err != nil {
		return jerrors.Trace(err)
	}
	<-resp.done

	return jerrors.Trace(resp.err)
}

func (c *Client) Close() {
	if c.pool != nil {
		c.pool.close()
	}
	c.pool = nil
	if c.registry != nil {
		c.registry.Close()
	}
	c.registry = nil
}

func (c *Client) selectSession(protocol, addr string) getty.Session {
	rpcConn, err := c.pool.getConn(protocol, addr)
	if err != nil {
		return nil
	}
	return rpcConn.selectSession()
}

func (c *Client) heartbeat(session getty.Session) error {
	resp := NewPendingResponse()
	return c.transfer(session, nil, resp)
}

func (c *Client) transfer(session getty.Session, req *GettyRPCRequest, resp *PendingResponse) error {
	var (
		sequence uint64
		err      error
		pkg      GettyPackage
	)

	sequence = c.sequence.Add(1)
	pkg.H.Magic = gettyPackageMagic
	pkg.H.LogID = (uint32)(randomID())
	pkg.H.Sequence = sequence
	pkg.H.Command = gettyCmdHbRequest
	pkg.H.CodecType = c.conf.codecType
	if req != nil {
		pkg.H.Command = gettyCmdRPCRequest
		pkg.B = req
	}

	resp.seq = sequence
	c.AddPendingResponse(resp)

	err = session.WritePkg(pkg, 0)
	if err != nil && resp != nil {
		c.RemovePendingResponse(resp.seq)
	}
	return jerrors.Trace(err)
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
