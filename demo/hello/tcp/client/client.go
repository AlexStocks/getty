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
)

import (
	"github.com/dubbogo/getty/demo/hello"
	"github.com/dubbogo/getty/demo/hello/tcp"
	"github.com/dubbogo/getty/demo/util"
)

var (
	ip          = flag.String("ip", "127.0.0.1", "server IP")
	connections = flag.Int("conn", 1, "number of tcp connections")
)

func main() {
	flag.Parse()
	util.SetLimit()

	client := getty.NewTCPClient(
		getty.WithServerAddress(*ip+":8090"),
		getty.WithConnectionNumber(*connections),
	)

	client.RunEventLoop(tcp.NewHelloClientSession)

	go hello.ClientRequest()

	util.WaitCloseSignals(client)
}
