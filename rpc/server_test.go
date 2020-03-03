package rpc

import (
    "testing"
    "time"
)

import (
    "github.com/stretchr/testify/assert"
)

var (
    serverConf      *ServerConfig
    clientConf      *ClientConfig
)

type (
    TestReq struct {}
    TestRsp struct {}
    EventReq struct {}
)

type TestService struct {
    i int
}

func (r *TestService) Service() string {
    return "TestService"
}

func (r *TestService) Version() string {
    return "v1.0"
}

func (r *TestService) Test(req *TestReq, rsp *TestRsp) error {
    return nil
}

func (r *TestService) Add(req *TestReq, rsp *TestRsp) error {
    return nil
}

func (r *TestService) Err(req *TestReq, rsp *TestRsp) error {
    return nil
}

func (r *TestService) Event(req *TestReq) error {
    return nil
}

func initConf() {
    serverConf = &ServerConfig{}
    serverConf.AppName = "RPC-SERVER"
    serverConf.Host = "127.0.0.1"
    serverConf.Ports = []string{"10001", "20002"}

    serverConf.SessionTimeout = "20s"
    serverConf.SessionNumber = 700
    serverConf.FailFastTimeout = "3s"

    serverConf.GettySessionParam.CompressEncoding = true
    serverConf.GettySessionParam.TcpNoDelay = true
    serverConf.GettySessionParam.PkgWQSize = 10
    serverConf.GettySessionParam.TcpReadTimeout = "2s"
    serverConf.GettySessionParam.TcpWriteTimeout = "5s"
    serverConf.GettySessionParam.WaitTimeout = "1s"
    serverConf.GettySessionParam.TcpKeepAlive = true
    serverConf.GettySessionParam.KeepAlivePeriod = "120s"
    serverConf.GettySessionParam.MaxMsgLen = 1024

    clientConf = &ClientConfig{}
    clientConf.AppName = "RPC-SERVER"
    clientConf.Host = "127.0.0.1"
    clientConf.ConnectionNum = 2
    clientConf.HeartbeatPeriod = "10s"

    clientConf.SessionTimeout = "20s"
    clientConf.FailFastTimeout = "3s"

    clientConf.GettySessionParam.CompressEncoding = true
    clientConf.GettySessionParam.TcpNoDelay = true
    clientConf.GettySessionParam.TcpReadTimeout = "2s"
    clientConf.GettySessionParam.TcpWriteTimeout = "5s"
    clientConf.GettySessionParam.PkgWQSize = 10
    clientConf.GettySessionParam.WaitTimeout = "1s"
    clientConf.GettySessionParam.TcpKeepAlive = true
    clientConf.GettySessionParam.KeepAlivePeriod = "120s"
    clientConf.GettySessionParam.MaxMsgLen = 1024
    return
}

func TestNewServer(t *testing.T) {
    initConf()
    server, err := NewServer(serverConf)
    assert.Nil(t, err)
    err = server.Register(&TestService{})
    assert.Nil(t, err)
    assert.NotNil(t, server.serviceMap)
    assert.NotNil(t, server.rpcHandler)
    assert.NotNil(t, server.pkgHandler)
    server.Start()
    time.Sleep(500e6)
    client, err := NewClient(clientConf)
    assert.Nil(t, err)
    assert.NotNil(t, client)
    server.Stop()
    assert.Nil(t, server.tcpServerList)
}
