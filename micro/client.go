package micro

import (
	"context"
	"strings"
	"time"
)

import (
	"github.com/AlexStocks/getty/rpc"
	"github.com/AlexStocks/goext/database/filter"
	"github.com/AlexStocks/goext/database/filter/pool"
	"github.com/AlexStocks/goext/database/registry"
	"github.com/AlexStocks/goext/database/registry/etcdv3"
	"github.com/AlexStocks/goext/database/registry/zookeeper"
	"github.com/AlexStocks/goext/net"

	jerrors "github.com/juju/errors"
)

type ClientOption func(*ClientOptions)

type ClientOptions struct {
	hash gxfilter.ServiceHash
}

func WithServiceHash(hash gxfilter.ServiceHash) ClientOption {
	return func(o *ClientOptions) {
		o.hash = hash
	}
}

type Client struct {
	ClientOptions
	*rpc.Client
	// registry
	registry gxregistry.Registry
	attr     gxregistry.ServiceAttr
	filter   gxfilter.Filter
	svcMap   map[gxregistry.ServiceAttr]*gxfilter.ServiceArray
}

// NewServer initialize a micro service consumer
func NewClient(conf *rpc.ClientConfig, regConf *RegistryConfig, opts ...ClientOption) (*Client, error) {
	var (
		err       error
		rpcClient *rpc.Client
		registry  gxregistry.Registry
		filter    gxfilter.Filter
	)

	if err = regConf.CheckValidity(); err != nil {
		return nil, jerrors.Trace(err)
	}

	if rpcClient, err = rpc.NewClient(conf); err != nil {
		return nil, jerrors.Trace(err)
	}

	addrList := strings.Split(regConf.Addr, ",")
	switch regConf.Type {
	case "etcd":
		registry, err = gxetcd.NewRegistry(
			gxregistry.WithAddrs(addrList...),
			gxregistry.WithTimeout(time.Duration(1e9*regConf.KeepaliveTimeout)),
			gxregistry.WithRoot(regConf.Root),
		)
	case "zookeeper":
		registry, err = gxzookeeper.NewRegistry(
			gxregistry.WithAddrs(addrList...),
			gxregistry.WithTimeout(time.Duration(1e9*regConf.KeepaliveTimeout)),
			gxregistry.WithRoot(regConf.Root),
		)
	default:
		return nil, jerrors.Errorf(ErrIllegalConf+"registry type %s", regConf.Type)
	}
	if err != nil {
		return nil, jerrors.Trace(err)
	}

	if filter, err = gxpool.NewFilter(
		gxfilter.WithRegistry(registry),
		gxpool.WithTTL(time.Duration(1e9*regConf.KeepaliveTimeout)),
	); err != nil {
		return nil, jerrors.Trace(err)
	}

	service := gxregistry.Service{
		Attr: &gxregistry.ServiceAttr{
			Group:    regConf.IDC,
			Role:     gxregistry.SRT_Consumer,
			Protocol: regConf.Codec,
		},
		Nodes: []*gxregistry.Node{
			&gxregistry.Node{
				ID:      regConf.NodeID,
				Address: conf.Host,
				// Port:    0,
			},
		},
	}
	if err = registry.Register(service); err != nil {
		return nil, jerrors.Trace(err)
	}

	clt := &Client{
		Client:   rpcClient,
		registry: registry,
		filter:   filter,
	}

	for _, o := range opts {
		o(&(clt.ClientOptions))
	}

	return clt, nil
}

func (c *Client) Call(ctx context.Context, typ rpc.CodecType, service, method string, args interface{}, reply interface{}) error {
	attr := c.attr
	attr.Service = service
	attr.Protocol = typ.String()

	flag := false
	svcArray, ok := c.svcMap[attr]
	if !ok {
		flag = true
	} else {
		if ok = c.filter.CheckServiceAlive(attr, svcArray); !ok {
			flag = true
		}
	}
	var err error
	if flag {
		if svcArray, err = c.filter.Filter(attr); err != nil {
			return jerrors.Trace(err)
		}

		c.svcMap[attr] = svcArray
	}

	svc, err := svcArray.Select(ctx, c.ClientOptions.hash)
	if err != nil {
		return jerrors.Trace(err)
	}

	return jerrors.Trace(c.Client.Call(typ,
		gxnet.HostAddress(svc.Nodes[0].Address, int(svc.Nodes[0].Port)),
		service, method, args, reply))
}

func (c *Client) Close() {
	c.filter.Close()
	c.registry.Close()
	c.Client.Close()
}
