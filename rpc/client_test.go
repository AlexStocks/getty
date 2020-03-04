package rpc

import (
    "net"
    "testing"
    "time"
)

import (
    log "github.com/AlexStocks/log4go"
    jerrors "github.com/juju/errors"
    "github.com/stretchr/testify/suite"
)

func buildClientConf() *ClientConfig{
    return &ClientConfig{
        AppName:         "RPC-SERVER",
        Host:            "127.0.0.1",
        ConnectionNum:   2,
        HeartbeatPeriod: "10s",

        SessionTimeout:  "20s",
        FailFastTimeout: "3s",
        GettySessionParam: GettySessionParam{
            CompressEncoding: true,
            TcpNoDelay:       true,
            TcpReadTimeout:   "2s",
            TcpWriteTimeout:  "5s",
            PkgWQSize:        10,
            WaitTimeout:      "1s",
            TcpKeepAlive:     true,
            KeepAlivePeriod:  "120s",
            MaxMsgLen:        1024,
        },
    }
}

type ClientTestSuite struct {
    suite.Suite
    client  *Client
    server  *Server
    clientConf *ClientConfig
    serverConf *ServerConfig
}

func (suite *ClientTestSuite) SetupTest() {
    var err error
    suite.serverConf = buildServerConf()
    suite.clientConf = buildClientConf()
    suite.server, err = NewServer(suite.serverConf)
    suite.Nil(err)
    err = suite.server.Register(&TestService{})
    suite.Nil(err)
    suite.server.Start()
    suite.client, err = NewClient(suite.clientConf)
    suite.Nil(err)
}

func (suite *ClientTestSuite) TearDownTest() {
    suite.server.Stop()
    suite.Nil(suite.server.tcpServerList)
    suite.client.Close()
    suite.Nil(suite.client.pool)
}

func (suite *ClientTestSuite) TestClient_Json_CallOneway() {
    var err error
    ts := TestService{}
    addr := net.JoinHostPort(suite.serverConf.Host, suite.serverConf.Ports[0])

    eventReq := EventReq{}
    err = suite.client.CallOneway(CodecJson, addr, ts.Service(), "Event", &eventReq,
        WithCallRequestTimeout(100e6), WithCallResponseTimeout(100e6))
    suite.Nil(err)
}

func (suite *ClientTestSuite) TestClient_Json_Call() {
    var err error
    ts := TestService{}
    addr := net.JoinHostPort(suite.serverConf.Host, suite.serverConf.Ports[0])

    testReq := TestReq{}
    testRsp := TestRsp{}
    err = suite.client.Call(CodecJson, addr, ts.Service(), "Test", &testReq, &testRsp,
        WithCallRequestTimeout(100e6), WithCallResponseTimeout(100e6))
    suite.Nil(err)
}

func (suite *ClientTestSuite) TestClient_Json_AsyncCall() {
    var err error
    ts := TestService{}
    addr := net.JoinHostPort(suite.serverConf.Host, suite.serverConf.Ports[0])

    testReq := TestReq{}
    testRsp := TestRsp{}
    err = suite.client.AsyncCall(CodecJson, addr,
        ts.Service(), "Add", &testReq, Callback, &testRsp,
        WithCallRequestTimeout(100e6), WithCallResponseTimeout(100e6),
        WithCallMeta("hello", "Service::Add::Json"))
    suite.Nil(err)
}

func TestClientTestSuite(t *testing.T) {
    suite.Run(t, new(ClientTestSuite))
}

func Callback(rsp CallResponse) {
    log.Info("method:%s, cost time span:%s, error:%s, reply:%#v",
        rsp.Opts.Meta["hello"].(string),
        time.Since(rsp.Start),
        jerrors.ErrorStack(rsp.Cause),
        rsp.Reply)
}
