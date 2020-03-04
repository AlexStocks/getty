package micro

import (
    "testing"
)

import (
    "github.com/AlexStocks/getty/rpc"
    "github.com/stretchr/testify/assert"
)

func buildClientConf() *rpc.ClientConfig{
    return &rpc.ClientConfig{
        AppName:         "MICRO-CLIENT",
        Host:            "127.0.0.1",
        SessionTimeout:  "20s",
        FailFastTimeout: "3s",
        HeartbeatPeriod: "10s",

        GettySessionParam: rpc.GettySessionParam{
            CompressEncoding: true,
            TcpNoDelay:       true,
            TcpRBufSize:      262144,
            TcpWBufSize:      524288,
            SessionName:      "getty-micro-server",
            TcpReadTimeout:   "2s",
            TcpWriteTimeout:  "5s",
            PkgWQSize:        512,
            WaitTimeout:      "1s",
            TcpKeepAlive:     true,
            KeepAlivePeriod:  "120s",
            MaxMsgLen:        1024,
        },
    }
}

func buildConsumerRegistryConfig() *ConsumerRegistryConfig{
    return &ConsumerRegistryConfig{
        Group: "bj-yizhuang",
        RegistryConfig: RegistryConfig {
            Root:             "/getty-micro",
            KeepaliveTimeout: 10,
            RegAddr:          "127.0.0.1:2181",
            Type:             "zookeeper",
        },
    }
}

func TestNewClient(t *testing.T) {
    var (
        err error
        clientConf *rpc.ClientConfig
        consumerReistryConfig *ConsumerRegistryConfig
    )
    clientConf = buildClientConf()
    consumerReistryConfig = buildConsumerRegistryConfig()
    _, err = NewClient(clientConf, consumerReistryConfig)
    assert.Nil(t, err)
}
