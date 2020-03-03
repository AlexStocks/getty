package rpc

import (
    "testing"
    "time"
)

import (
    "github.com/stretchr/testify/assert"
)

var serverConf      *ServerConfig

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

func initServerConf() {
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
    return
}

func TestNewServer(t *testing.T) {
    initServerConf()
    initClientConf()
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
