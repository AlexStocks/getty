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

package main

import (
	"flag"
)

import (
	"github.com/apache/dubbo-getty"
	"github.com/dubbogo/gost/sync"
)

import (
	"github.com/apache/dubbo-getty/demo/hello"
	"github.com/apache/dubbo-getty/demo/hello/tcp"
	"github.com/apache/dubbo-getty/demo/util"
)

var (
	ip          = flag.String("ip", "127.0.0.1", "server IP")
	connections = flag.Int("conn", 1, "number of tcp connections")

	taskPoolMode        = flag.Bool("taskPool", false, "task pool mode")
	taskPoolQueueLength = flag.Int("task_queue_length", 100, "task queue length")
	taskPoolQueueNumber = flag.Int("task_queue_number", 4, "task queue number")
	taskPoolSize        = flag.Int("task_pool_size", 2000, "task poll size")
	pprofPort           = flag.Int("pprof_port", 65431, "pprof http port")
)

var (
	taskPool *gxsync.TaskPool
)

func main() {
	flag.Parse()

	util.SetLimit()

	util.Profiling(*pprofPort)

	if *taskPoolMode {
		taskPool = gxsync.NewTaskPool(
			gxsync.WithTaskPoolTaskQueueLength(*taskPoolQueueLength),
			gxsync.WithTaskPoolTaskQueueNumber(*taskPoolQueueNumber),
			gxsync.WithTaskPoolTaskPoolSize(*taskPoolSize),
		)
	}

	client := getty.NewTCPClient(
		getty.WithServerAddress(*ip+":8090"),
		getty.WithConnectionNumber(*connections),
	)

	client.RunEventLoop(NewHelloClientSession)

	go hello.ClientRequest()

	util.WaitCloseSignals(client)
	taskPool.Close()
}

// NewHelloClientSession use for init client session
func NewHelloClientSession(session getty.Session) (err error) {
	tcp.EventListener.SessionOnOpen = func(session getty.Session) {
		hello.Sessions = append(hello.Sessions, session)
	}
	err = tcp.InitialSession(session)
	if err != nil {
		return
	}
	return
}