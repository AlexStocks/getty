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

func buildClientConf() *rpc.ClientConfig {
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

func buildConsumerRegistryConfig() *ConsumerRegistryConfig {
	return &ConsumerRegistryConfig{
		Group: "bj-yizhuang",
		RegistryConfig: RegistryConfig{
			Root:             "/getty-micro",
			KeepaliveTimeout: 10,
			RegAddr:          "127.0.0.1:2181",
			Type:             "zookeeper",
		},
	}
}

func TestNewClient(t *testing.T) {
	var (
		err                   error
		clientConf            *rpc.ClientConfig
		consumerReistryConfig *ConsumerRegistryConfig
	)
	clientConf = buildClientConf()
	consumerReistryConfig = buildConsumerRegistryConfig()
	_, err = NewClient(clientConf, consumerReistryConfig)
	assert.Nil(t, err)
}
