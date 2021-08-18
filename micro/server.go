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
	"net"
	"strconv"
	"strings"
	"time"
)

import (
	"github.com/AlexStocks/goext/database/registry"
	"github.com/AlexStocks/goext/database/registry/etcdv3"
	"github.com/AlexStocks/goext/database/registry/zookeeper"
	"github.com/AlexStocks/goext/net"
	"github.com/AlexStocks/goext/strings"
	jerrors "github.com/juju/errors"
)

import (
	"github.com/AlexStocks/getty/rpc"
)

// Server micro service provider
type Server struct {
	*rpc.Server
	// registry
	regConf  ProviderRegistryConfig
	registry gxregistry.Registry
}

// NewServer initialize a micro service provider
func NewServer(conf *rpc.ServerConfig, regConf *ProviderRegistryConfig) (*Server, error) {
	var (
		err       error
		rpcServer *rpc.Server
		registry  gxregistry.Registry
	)

	if err = regConf.CheckValidity(); err != nil {
		return nil, jerrors.Trace(err)
	}

	if rpcServer, err = rpc.NewServer(conf); err != nil {
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

	var localAddrArr []string
	for _, p := range conf.Ports {
		port, err := strconv.Atoi(p)
		if err != nil {
			return nil, jerrors.Trace(err)
		}

		if port <= 0 || 65535 < port {
			return nil, jerrors.Errorf("illegal port %s", p)
		}

		localAddrArr = append(localAddrArr, net.JoinHostPort(conf.Host, p))
	}

	for _, svr := range regConf.ServiceArray {
		addr := gxnet.HostAddress(svr.LocalHost, svr.LocalPort)
		if ok := gxstrings.Contains(gxstrings.Strings2Ifs(localAddrArr), addr); !ok {
			return nil, jerrors.Errorf("can not find ServiceConfig addr %s in conf address array %#v",
				addr, localAddrArr)
		}
	}

	return &Server{
		Server:   rpcServer,
		regConf:  *regConf,
		registry: registry,
	}, nil
}

// Register the @rcvr
func (s *Server) Register(rcvr rpc.GettyRPCService) error {
	var (
		flag bool
		attr gxregistry.ServiceAttr
	)

	attr.Role = gxregistry.SRT_Provider
	attr.Service = rcvr.Service()
	attr.Version = rcvr.Version()
	for _, c := range s.regConf.ServiceArray {
		if c.Service == rcvr.Service() && c.Version == rcvr.Version() {
			flag = true
			attr.Group = c.Group
			attr.Protocol = c.Protocol

			node := &gxregistry.Node{
				ID:      c.NodeID,
				Address: c.LocalHost,
				Port:    int32(c.LocalPort),
			}
			if len(c.Meta) != 0 {
				node.Metadata = map[string]string{DefaultMetaKey: c.Meta}
			}

			service := gxregistry.Service{Attr: &attr}
			service.Nodes = append(service.Nodes, node)
			if err := s.registry.Register(service); err != nil {
				return jerrors.Trace(err)
			}
		}
	}
	if !flag {
		return jerrors.Errorf("can not find @rcvr{service:%s, version:%s} in registry config:%#v",
			rcvr.Service(), rcvr.Version(), s.regConf)
	}

	if err := s.Server.Register(rcvr); err != nil {
		return jerrors.Trace(err)
	}

	return nil
}

// func (s *Server) Start() {
// 	s.Server.Start()
// }

// func (s *Server) Stop() {
// 	s.Server.Stop()
// }
