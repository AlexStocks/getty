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

package rpc

import (
	"net"
	"testing"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

import (
	rpcservice "github.com/AlexStocks/getty/examples/rpc/service"
)

func buildClientConfig() *ClientConfig {
	return &ClientConfig{
		AppName:         "RPC-SERVER",
		Host:            "127.0.0.1",
		ConnectionNum:   1,
		HeartbeatPeriod: "30s",
		SessionTimeout:  "300s",
		FailFastTimeout: "3s",
		PoolSize:        64,
		PoolTTL:         600,
		GettySessionParam: GettySessionParam{
			CompressEncoding: false,
			TcpNoDelay:       true,
			TcpKeepAlive:     true,
			KeepAlivePeriod:  "120s",
			TcpReadTimeout:   "2s",
			TcpWriteTimeout:  "5s",
			TcpRBufSize:      262144,
			TcpWBufSize:      524288,
			PkgRQSize:        1024,
			PkgWQSize:        10,
			WaitTimeout:      "1s",
			MaxMsgLen:        8388608,
			SessionName:      "getty-rpc-client",
		},
	}
}

type ClientTestSuite struct {
	suite.Suite
	client     *Client
	server     *Server
	clientConf *ClientConfig
	serverConf *ServerConfig
}

func (suite *ClientTestSuite) SetupTest() {
	var err error
	suite.serverConf = buildServerConfig()
	suite.clientConf = buildClientConfig()
	suite.server, err = NewServer(suite.serverConf)
	suite.Nil(err)
	err = suite.server.Register(&MockService{})
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
	ts := MockService{}
	addr := net.JoinHostPort(suite.serverConf.Host, suite.serverConf.Ports[0])

	eventReq := EventReq{}
	err = suite.client.CallOneway(CodecJson, addr, ts.Service(), "Event", &eventReq,
		WithCallRequestTimeout(100e6), WithCallResponseTimeout(100e6))
	suite.Nil(err)
}

func (suite *ClientTestSuite) TestClient_Json_Call() {
	var err error
	ts := MockService{}
	addr := net.JoinHostPort(suite.serverConf.Host, suite.serverConf.Ports[0])

	testReq := TestReq{}
	testRsp := rpcservice.TestRsp{}
	err = suite.client.Call(CodecJson, addr, ts.Service(), "Test", &testReq, &testRsp,
		WithCallRequestTimeout(100e6), WithCallResponseTimeout(100e6))
	suite.Nil(err)
}

func (suite *ClientTestSuite) TestClient_Json_AsyncCall() {
	var err error
	ts := MockService{}
	addr := net.JoinHostPort(suite.serverConf.Host, suite.serverConf.Ports[0])

	testReq := TestReq{}
	testRsp := rpcservice.TestRsp{}
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

// client-server test

func buildServer(t *testing.T) *Server {
	var (
		err    error
		conf   *ServerConfig
		server *Server
	)

	conf = buildServerConfig()
	err = conf.CheckValidity()
	assert.Nil(t, err)

	server, err = NewServer(conf)
	assert.Nil(t, err)
	err = server.Register(&rpcservice.TestService{})
	if err != nil {
		panic(jerrors.ErrorStack(err))
	}

	return server
}

func testProtobuf(t *testing.T, client *Client) {
	ts := rpcservice.TestService{}
	testReq := rpcservice.TestReq{"aaa", "bbb", "ccc"}
	testRsp := rpcservice.TestRsp{}
	addr := net.JoinHostPort(ServerHost, ServerPort)

	eventReq := rpcservice.EventReq{A: "hello"}
	err := client.CallOneway(CodecProtobuf,
		addr, ts.Service(), "Event", &eventReq,
		WithCallRequestTimeout(100e6),
		WithCallResponseTimeout(100e6))
	assert.Nil(t, err)

	err = client.Call(CodecProtobuf,
		addr, ts.Service(), "Test", &testReq, &testRsp,
		WithCallRequestTimeout(500e6),
		WithCallResponseTimeout(500e6))
	assert.Nil(t, err)

	addReq := rpcservice.AddReq{1, 10}
	addRsp := rpcservice.AddRsp{}
	err = client.Call(CodecProtobuf,
		addr, ts.Service(), "Add", &addReq, &addRsp,
		WithCallRequestTimeout(500e6),
		WithCallResponseTimeout(500e6))
	assert.Nil(t, err)

	errReq := rpcservice.ErrReq{1}
	errRsp := rpcservice.ErrRsp{}
	err = client.Call(CodecProtobuf,
		addr, ts.Service(), "Err", &errReq, &errRsp,
		WithCallRequestTimeout(500e6),
		WithCallResponseTimeout(500e6))
	assert.NotNil(t, err)
}

func testAsyncProtobuf(t *testing.T, client *Client) {
	ts := rpcservice.TestService{}
	testReq := rpcservice.TestReq{"aaa", "bbb", "ccc"}
	testRsp := rpcservice.TestRsp{}
	addr := net.JoinHostPort(ServerHost, ServerPort)

	err := client.AsyncCall(CodecProtobuf, addr,
		ts.Service(), "Test", &testReq, Callback, &testRsp,
		WithCallRequestTimeout(100e6),
		WithCallResponseTimeout(100e6),
		WithCallMeta("hello", "Service::Test::Protobuf"))
	assert.Nil(t, err)

	addReq := rpcservice.AddReq{1, 10}
	addRsp := rpcservice.AddRsp{}
	err = client.AsyncCall(CodecProtobuf, addr,
		ts.Service(), "Add", &addReq, Callback, &addRsp,
		WithCallRequestTimeout(100e6),
		WithCallResponseTimeout(100e6),
		WithCallMeta("hello", "Service::Add::Protobuf"))
	assert.Nil(t, err)

	errReq := rpcservice.ErrReq{1}
	errRsp := rpcservice.ErrRsp{}
	err = client.AsyncCall(CodecProtobuf,
		addr, ts.Service(), "Err", &errReq, Callback, &errRsp,
		WithCallRequestTimeout(100e6),
		WithCallResponseTimeout(100e6),
		WithCallMeta("hello", "Service::Err::protobuf"))
	assert.Nil(t, err)
}

func TestClient(t *testing.T) {
	// build server
	server := buildServer(t)
	go server.Start()
	time.Sleep(1e9)
	defer server.Stop()

	// build client
	clientConfig := buildClientConfig()
	assert.Nil(t, clientConfig.CheckValidity())
	client, err := NewClient(clientConfig)
	assert.Nil(t, err)
	defer client.Close()

	testProtobuf(t, client)
	testAsyncProtobuf(t, client)
}
