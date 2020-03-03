package micro

import (
    "testing"
)

import (
    "github.com/AlexStocks/getty/rpc"
    "github.com/stretchr/testify/assert"
)

var (
    server *Server
)

type microConfig struct {
    rpc.ServerConfig   `yaml:"core" json:"core, omitempty"`
    Registry ProviderRegistryConfig `yaml:"registry" json:"registry, omitempty"`
}

var (
    conf *microConfig
)

func initServerConf() {
    conf = &microConfig{}
    conf.AppName = "MICRO-SERVER"
    conf.Host = "127.0.0.1"
    conf.Ports = []string{"10000", "20000"}
    conf.SessionNumber = 700
    conf.SessionTimeout = "20s"
    conf.FailFastTimeout = "3s"

    conf.GettySessionParam.CompressEncoding = true
    conf.GettySessionParam.TcpNoDelay = true
    conf.GettySessionParam.TcpRBufSize = 262144
    conf.GettySessionParam.TcpWBufSize = 524288
    conf.GettySessionParam.SessionName = "getty-micro-server"
    conf.GettySessionParam.TcpReadTimeout = "2s"
    conf.GettySessionParam.TcpWriteTimeout = "5s"
    conf.GettySessionParam.PkgWQSize = 512
    conf.GettySessionParam.WaitTimeout = "1s"
    conf.GettySessionParam.TcpKeepAlive = true
    conf.GettySessionParam.KeepAlivePeriod = "120s"
    conf.GettySessionParam.MaxMsgLen = 1024

    conf.Registry.RegistryConfig.Type = "zookeeper"
    conf.Registry.RegistryConfig.RegAddr = "127.0.0.1:2181"
    conf.Registry.RegistryConfig.KeepaliveTimeout = 10
    conf.Registry.RegistryConfig.Root = "/getty-micro"

    serviceArray := []ServiceConfig{
        {
            "127.0.0.1",
            10000,
            "bj-yizhuang",
            "srv-node-127.0.0.1:10000-pb",
            "protobuf",
            "TestService",
            "v1.0",
            "{\"group_id\":1, \"node_id\":0}",
        },
        {
            "127.0.0.1",
            20000,
            "bj-yizhuang",
            "srv-node-127.0.0.1:20000-pb",
            "protobuf",
            "TestService",
            "v1.0",
            "{\"group_id\":2, \"node_id\":0}",
        },
        {
            "127.0.0.1",
            10000,
            "bj-yizhuang",
            "srv-node-127.0.0.1:10000-json",
            "json",
            "TestService",
            "v1.0",
            "{\"group_id\":1, \"node_id\":0}",
        },
        {
            "127.0.0.1",
            20000,
            "bj-yizhuang",
            "srv-node-127.0.0.1:20000-json",
            "json",
            "TestService",
            "v1.0",
            "{\"group_id\":2, \"node_id\":0}",
        },
    }
    conf.Registry.ServiceArray = serviceArray
    return
}

func TestNewServer(t *testing.T) {
    var err error
    initServerConf()
    server, err = NewServer(&conf.ServerConfig, &conf.Registry)
    assert.Nil(t, err)

    server.Start()
    assert.Nil(t, err)
    server.Stop()
}
