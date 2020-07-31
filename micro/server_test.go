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

package micro

import (
	"testing"
)

import (
	"github.com/AlexStocks/getty/rpc"
	"github.com/stretchr/testify/assert"
)

func buildProviderRegistryConfig() *ProviderRegistryConfig {
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
	return &ProviderRegistryConfig{
		RegistryConfig: RegistryConfig{
			Type:             "zookeeper",
			RegAddr:          "127.0.0.1:2181",
			KeepaliveTimeout: 10,
			Root:             "/getty-micro",
		},
		ServiceArray: serviceArray,
	}
}

func buildServerConf() *rpc.ServerConfig {
	return &rpc.ServerConfig{
		AppName:         "MICRO-SERVER",
		Host:            "127.0.0.1",
		Ports:           []string{"10000", "20000"},
		SessionNumber:   700,
		SessionTimeout:  "20s",
		FailFastTimeout: "3s",
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

func TestNewServer(t *testing.T) {
	var (
		err                    error
		providerRegistryConfig *ProviderRegistryConfig
		serverConfig           *rpc.ServerConfig
	)
	providerRegistryConfig = buildProviderRegistryConfig()
	serverConfig = buildServerConf()
	server, err := NewServer(serverConfig, providerRegistryConfig)
	assert.Nil(t, err)

	server.Start()
	assert.Nil(t, err)
	server.Stop()
}
