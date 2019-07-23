/******************************************************
# MAINTAINER : wongoo
# LICENCE    : Apache License 2.0
# EMAIL      : gelnyang@163.com
# MOD        : 2019-06-11
******************************************************/

package main

import (
	"flag"
)

import (
	"github.com/dubbogo/getty"
	gxsync "github.com/dubbogo/gost/sync"
)

import (
	"github.com/dubbogo/getty/demo/hello/tcp"
	"github.com/dubbogo/getty/demo/util"
)

var (
	taskPollMode        = flag.Bool("taskPool", false, "task pool mode")
	taskPollQueueLength = flag.Int("task_queue_length", 100, "task queue length")
	taskPollQueueNumber = flag.Int("task_queue_number", 4, "task queue number")
	taskPollSize        = flag.Int("task_pool_size", 2000, "task poll size")
)

var (
	taskPoll *gxsync.TaskPool
)

func main() {
	flag.Parse()

	util.SetLimit()

	options := []getty.ServerOption{getty.WithLocalAddress(":8090")}

	if *taskPollMode {
		taskPoll = gxsync.NewTaskPool(
			gxsync.WithTaskPoolTaskQueueLength(*taskPollQueueLength),
			gxsync.WithTaskPoolTaskQueueNumber(*taskPollQueueNumber),
			gxsync.WithTaskPoolTaskPoolSize(*taskPollSize),
		)
	}

	server := getty.NewTCPServer(options...)

	go server.RunEventLoop(NewHelloServerSession)

	util.WaitCloseSignals(server)
}

func NewHelloServerSession(session getty.Session) (err error) {
	err = tcp.InitialSession(session)
	if err != nil {
		return
	}
	session.SetTaskPool(taskPoll)
	return
}
