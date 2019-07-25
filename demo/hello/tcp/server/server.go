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
	taskPoolMode        = flag.Bool("taskPool", false, "task pool mode")
	taskPoolQueueLength = flag.Int("task_queue_length", 100, "task queue length")
	taskPoolQueueNumber = flag.Int("task_queue_number", 4, "task queue number")
	taskPoolSize        = flag.Int("task_pool_size", 2000, "task poll size")
	pprofPort           = flag.Int("pprof_port", 65432, "pprof http port")
)

var (
	taskPool *gxsync.TaskPool
)

func main() {
	flag.Parse()

	util.SetLimit()

    util.Profiling(*pprofPort)

	options := []getty.ServerOption{getty.WithLocalAddress(":8090")}

	if *taskPoolMode {
		taskPool = gxsync.NewTaskPool(
			gxsync.WithTaskPoolTaskQueueLength(*taskPoolQueueLength),
			gxsync.WithTaskPoolTaskQueueNumber(*taskPoolQueueNumber),
			gxsync.WithTaskPoolTaskPoolSize(*taskPoolSize),
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
	session.SetTaskPool(taskPool)
	return
}
