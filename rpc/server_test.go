package rpc

import (
	"testing"
	"time"
)

import (
	"github.com/stretchr/testify/assert"
)

type (
	TestReq  struct{}
	TestRsp  struct{}
	EventReq struct{}
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

func buildServerConf() *ServerConfig {
	return &ServerConfig{
		AppName:         "RPC-SERVER",
		Host:            "127.0.0.1",
		Ports:           []string{"10001", "20002"},
		SessionTimeout:  "20s",
		SessionNumber:   700,
		FailFastTimeout: "3s",
		GettySessionParam: GettySessionParam{
			CompressEncoding: true,
			TcpNoDelay:       true,
			PkgWQSize:        10,
			TcpReadTimeout:   "2s",
			TcpWriteTimeout:  "5s",
			WaitTimeout:      "1s",
			TcpKeepAlive:     true,
			KeepAlivePeriod:  "120s",
			MaxMsgLen:        1024,
		},
	}
}

func TestNewServer(t *testing.T) {
	var (
		clientConf *ClientConfig
		serverConf *ServerConfig
	)
	serverConf = buildServerConf()
	clientConf = buildClientConf()
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
