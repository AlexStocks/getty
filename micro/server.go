package micro

import (
	"github.com/AlexStocks/goext/net"
	"strconv"
	"strings"
	"time"
)

import (
	"github.com/AlexStocks/getty/rpc"
	"github.com/AlexStocks/goext/database/registry"
	"github.com/AlexStocks/goext/database/registry/etcdv3"
	"github.com/AlexStocks/goext/database/registry/zookeeper"
	jerrors "github.com/juju/errors"
)

// Server micro service provider
type Server struct {
	*rpc.Server
	// registry
	regConf  RegistryConfig
	registry gxregistry.Registry
	attr     gxregistry.ServiceAttr
	nodes    []*gxregistry.Node
}

// NewServer initialize a micro service provider
func NewServer(conf *rpc.ServerConfig, regConf *RegistryConfig) (*Server, error) {
	var (
		err       error
		rpcServer *rpc.Server
		registry  gxregistry.Registry
		nodes     []*gxregistry.Node
	)

	if err = regConf.CheckValidity(); err != nil {
		return nil, jerrors.Trace(err)
	}

	if rpcServer, err = rpc.NewServer(conf); err != nil {
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

	for _, p := range conf.Ports {
		port, err := strconv.Atoi(p)
		if err != nil {
			return nil, jerrors.Errorf("illegal port %s", p)
		}

		nodes = append(nodes,
			&gxregistry.Node{
				// use host port as part of NodeID to defeat the case: on process listens on many ports
				ID:      regConf.NodeID + "@" + gxnet.HostAddress(conf.Host, port),
				Address: conf.Host,
				Port:    int32(port),
			},
		)
	}

	return &Server{
		Server:   rpcServer,
		regConf:  *regConf,
		registry: registry,
		nodes:    nodes,

		attr: gxregistry.ServiceAttr{
			Group:    regConf.IDC,
			Role:     gxregistry.SRT_Provider,
			Protocol: regConf.Codec,
		},
	}, nil
}

// Register the @rcvr
func (s *Server) Register(rcvr rpc.GettyRPCService) error {
	if err := s.Server.Register(rcvr); err != nil {
		return jerrors.Trace(err)
	}

	attr := s.attr
	attr.Service = rcvr.Service()
	attr.Version = rcvr.Version()
	service := gxregistry.Service{Attr: &attr, Nodes: s.nodes}
	if err := s.registry.Register(service); err != nil {
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
