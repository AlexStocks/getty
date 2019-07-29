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
	"github.com/dubbogo/gost/sync"
)

import (
	"github.com/dubbogo/getty/demo/hello"
	"github.com/dubbogo/getty/demo/hello/tcp"
	"github.com/dubbogo/getty/demo/util"
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

	client.RunEventLoop(tcp.NewHelloClientSession)

	go hello.ClientRequest()

	util.WaitCloseSignals(client)
}

