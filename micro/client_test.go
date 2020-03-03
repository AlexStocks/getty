package micro

import (
    "testing"
)

import (
    "github.com/AlexStocks/getty/rpc"
    "github.com/stretchr/testify/assert"
)

var (
    client *Client
    ClientConfig *rpc.ClientConfig
    ConsumerConfig *ConsumerRegistryConfig
)

func initClientConf() {
    ClientConfig = &rpc.ClientConfig{}
    ClientConfig.AppName = "MICRO-CLIENT"
    ClientConfig.Host = "127.0.0.1"
    ClientConfig.SessionTimeout = "20s"
    ClientConfig.FailFastTimeout = "3s"
    ClientConfig.HeartbeatPeriod = "10s"

    ClientConfig.GettySessionParam.CompressEncoding = true
    ClientConfig.GettySessionParam.TcpNoDelay = true
    ClientConfig.GettySessionParam.TcpRBufSize = 262144
    ClientConfig.GettySessionParam.TcpWBufSize = 524288
    ClientConfig.GettySessionParam.SessionName = "getty-micro-server"
    ClientConfig.GettySessionParam.TcpReadTimeout = "2s"
    ClientConfig.GettySessionParam.TcpWriteTimeout = "5s"
    ClientConfig.GettySessionParam.PkgWQSize = 512
    ClientConfig.GettySessionParam.WaitTimeout = "1s"
    ClientConfig.GettySessionParam.TcpKeepAlive = true
    ClientConfig.GettySessionParam.KeepAlivePeriod = "120s"
    ClientConfig.GettySessionParam.MaxMsgLen = 1024

    ConsumerConfig = &ConsumerRegistryConfig{}
    ConsumerConfig.RegistryConfig.Root = "/getty-micro"
    ConsumerConfig.RegistryConfig.KeepaliveTimeout = 10
    ConsumerConfig.RegistryConfig.RegAddr = "127.0.0.1:2181"
    ConsumerConfig.RegistryConfig.Type = "zookeeper"
    ConsumerConfig.Group = "bj-yizhuang"
}

func TestNewClient(t *testing.T) {
    var err error
    initClientConf()
    client, err = NewClient(ClientConfig, ConsumerConfig)
    assert.Nil(t, err)
}
