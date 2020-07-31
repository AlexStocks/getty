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

package micro

import (
	"context"
	"strings"
	"time"
)

import (
	"github.com/AlexStocks/goext/context"
	"github.com/AlexStocks/goext/database/filter"
	"github.com/AlexStocks/goext/database/filter/pool"
	"github.com/AlexStocks/goext/database/registry"
	"github.com/AlexStocks/goext/database/registry/etcdv3"
	"github.com/AlexStocks/goext/database/registry/zookeeper"
	"github.com/AlexStocks/goext/net"

	jerrors "github.com/juju/errors"
)

import (
	"github.com/AlexStocks/getty/rpc"
)

////////////////////////////////
// Options
////////////////////////////////

type ClientOptions struct {
	hash gxfilter.ServiceHash
}

type ClientOption func(*ClientOptions)

func WithServiceHash(hash gxfilter.ServiceHash) ClientOption {
	return func(o *ClientOptions) {
		o.hash = hash
	}
}

////////////////////////////////
// meta data
////////////////////////////////

const (
	DefaultMetaKey = "getty-micro-meta-key"
)

func GetServiceNodeMetadata(service *gxregistry.Service) string {
	if service != nil && len(service.Nodes) == 1 && service.Nodes[0].Metadata != nil {
		return service.Nodes[0].Metadata[DefaultMetaKey]
	}

	return ""
}

////////////////////////////////
// Client
////////////////////////////////

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
func NewClient(conf *rpc.ClientConfig, regConf *ConsumerRegistryConfig, opts ...ClientOption) (*Client, error) {
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

	regAddrList := strings.Split(regConf.RegAddr, ",")
	switch regConf.Type {
	case "etcd":
		registry, err = gxetcd.NewRegistry(
			gxregistry.WithAddrs(regAddrList...),
			gxregistry.WithTimeout(time.Duration(1e9*regConf.KeepaliveTimeout)),
			gxregistry.WithRoot(regConf.Root),
		)
	case "zookeeper":
		registry, err = gxzookeeper.NewRegistry(
			gxregistry.WithAddrs(regAddrList...),
			gxregistry.WithTimeout(time.Duration(1e9*regConf.KeepaliveTimeout)),
			gxregistry.WithRoot(regConf.Root),
		)
	default:
		return nil, jerrors.Errorf(ErrIllegalConf+"registry type %s", regConf.Type)
	}
	if err != nil {
		return nil, jerrors.Trace(err)
	}

	serviceAttrFilter := gxregistry.ServiceAttr{
		Role: gxregistry.SRT_Provider,
	}
	gxctx := gxcontext.NewValuesContext(nil)
	gxctx.Set(gxpool.GxfilterServiceAttrKey, serviceAttrFilter)

	if filter, err = gxpool.NewFilter(
		gxfilter.WithRegistry(registry),
		gxfilter.WithContext(gxctx),
		gxpool.WithTTL(time.Duration(1e9*regConf.KeepaliveTimeout)),
	); err != nil {
		return nil, jerrors.Trace(err)
	}

	// service := gxregistry.Service{
	// 	Attr: &gxregistry.ServiceAttr{
	// 		Group:    regConf.IDC,
	// 		Role:     gxregistry.SRT_Consumer,
	// 		Protocol: regConf.Codec,
	// 	},
	// 	Nodes: []*gxregistry.Node{
	// 		&gxregistry.Node{
	// 			ID:      regConf.NodeID,
	// 			Address: conf.Host,
	// 			// Port:    0,
	// 		},
	// 	},
	// }
	// if err = registry.Register(service); err != nil {
	// 	return nil, jerrors.Trace(err)
	// }

	clt := &Client{
		Client:   rpcClient,
		registry: registry,
		attr: gxregistry.ServiceAttr{
			Group: regConf.Group,
		},
		filter: filter,
		svcMap: make(map[gxregistry.ServiceAttr]*gxfilter.ServiceArray),
	}

	for _, o := range opts {
		o(&(clt.ClientOptions))
	}

	return clt, nil
}

func (c *Client) getServiceAddr(ctx context.Context, typ rpc.CodecType, service, version string) (string, error) {
	attr := c.attr
	attr.Service = service
	attr.Protocol = typ.String()
	attr.Role = gxregistry.SRT_Provider
	attr.Version = version

	var addr string

	flag := false
	svcArray, ok := c.svcMap[attr]
	if !ok {
		flag = true
	} else {
		if ok = c.filter.CheckServiceAlive(attr, svcArray); !ok {
			flag = true
		}
	}
	if flag {
		var err error
		if svcArray, err = c.filter.Filter(attr); err != nil {
			return addr, jerrors.Trace(err)
		}

		c.svcMap[attr] = svcArray
	}

	svc, err := svcArray.Select(ctx, c.ClientOptions.hash)
	if err != nil {
		return addr, jerrors.Trace(err)
	}

	if len(svc.Nodes) != 1 {
		return addr, jerrors.Errorf("illegal service %#v", svc)
	}

	return gxnet.HostAddress(svc.Nodes[0].Address, int(svc.Nodes[0].Port)), nil
}

func (c *Client) CallOneway(ctx context.Context, typ rpc.CodecType, service, version, method string,
	args interface{}, opts ...rpc.CallOption) error {

	addr, err := c.getServiceAddr(ctx, typ, service, version)
	if err != nil {
		return jerrors.Trace(err)
	}

	return jerrors.Trace(c.Client.CallOneway(typ, addr, service, method, args, opts...))
}

func (c *Client) Call(ctx context.Context, typ rpc.CodecType, service, version, method string,
	args interface{}, reply interface{}, opts ...rpc.CallOption) error {

	addr, err := c.getServiceAddr(ctx, typ, service, version)
	if err != nil {
		return jerrors.Trace(err)
	}

	return jerrors.Trace(c.Client.Call(typ, addr, service, method, args, reply, opts...))
}

func (c *Client) AsyncCall(ctx context.Context, typ rpc.CodecType, service, version, method string,
	args interface{}, callback rpc.AsyncCallback, reply interface{}, opts ...rpc.CallOption) error {
	addr, err := c.getServiceAddr(ctx, typ, service, version)
	if err != nil {
		return jerrors.Trace(err)
	}

	return jerrors.Trace(c.Client.AsyncCall(typ, addr, service, method, args, callback, reply, opts...))
}

func (c *Client) Close() {
	c.filter.Close()
	c.registry.Close()
	c.Client.Close()
}
