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

type MockService struct {
	i int
}

func (r *MockService) Service() string {
	return "MockService"
}

func (r *MockService) Version() string {
	return "v1.0"
}

func (r *MockService) Test(req *TestReq, rsp *TestRsp) error {
	return nil
}

func (r *MockService) Add(req *TestReq, rsp *TestRsp) error {
	return nil
}

func (r *MockService) Err(req *TestReq, rsp *TestRsp) error {
	return nil
}

func (r *MockService) Event(req *TestReq) error {
	return nil
}

const (
	ServerHost = "127.0.0.1"
	ServerPort = "65432"
)

func buildServerConfig() *ServerConfig {
	return &ServerConfig{
		AppName:         "RPC-SERVER",
		Host:            ServerHost,
		Ports:           []string{ServerPort},
		SessionTimeout:  "180s",
		sessionTimeout:  time.Second * 180,
		SessionNumber:   1,
		FailFastTimeout: "3s",
		failFastTimeout: time.Second * 3,
		GettySessionParam: GettySessionParam{
			CompressEncoding: false,
			TcpNoDelay:       true,
			TcpKeepAlive:     true,
			KeepAlivePeriod:  "120s",
			keepAlivePeriod:  time.Second * 120,
			TcpRBufSize:      262144,
			TcpWBufSize:      524288,
			PkgRQSize:        1024,
			PkgWQSize:        512,
			TcpReadTimeout:   "1s",
			TcpWriteTimeout:  "3s",
			WaitTimeout:      "1s",
			MaxMsgLen:        102400,
			SessionName:      "getty-rpc-server",
		},
	}
}

func TestNewServer(t *testing.T) {
	var (
		clientConf *ClientConfig
		serverConf *ServerConfig
	)
	serverConf = buildServerConfig()
	clientConf = buildClientConfig()
	server, err := NewServer(serverConf)
	assert.Nil(t, err)
	err = server.Register(&MockService{})
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
