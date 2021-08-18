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
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"
)

import (
	"github.com/AlexStocks/goext/net"
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

import (
	rpcservice "github.com/AlexStocks/getty/examples/rpc/service"
	"github.com/AlexStocks/getty/rpc"
	"github.com/AlexStocks/getty/transport"
)

const (
	pprofPath = "/debug/pprof/"
)

var (
	server *rpc.Server
)

func main() {
	initConf()

	initProfiling()

	initServer()
	log.Info("%s starts successfull! its version=%s, its listen ends=%s:%s\n",
		conf.AppName, getty.Version, conf.Host, conf.Ports)

	initSignal()
}

func initProfiling() {
	var (
		addr string
	)

	// addr = *host + ":" + "10000"
	addr = gxnet.HostAddress(conf.Host, conf.ProfilePort)
	log.Info("App Profiling startup on address{%v}", addr+pprofPath)
	go func() {
		log.Info(http.ListenAndServe(addr, nil))
	}()
}

func initServer() {
	var err error
	server, err = rpc.NewServer(conf)
	if err != nil {
		panic(jerrors.ErrorStack(err))
		return
	}
	err = server.Register(&rpcservice.TestService{})
	if err != nil {
		panic(jerrors.ErrorStack(err))
		return
	}
	server.Start()
}

func uninitServer() {
	server.Stop()
}

func initSignal() {
	timeout, _ := time.ParseDuration(conf.FailFastTimeout)

	// signal.Notify的ch信道是阻塞的(signal.Notify不会阻塞发送信号), 需要设置缓冲
	signals := make(chan os.Signal, 1)
	// It is not possible to block SIGKILL or syscall.SIGSTOP
	signal.Notify(signals, os.Interrupt, os.Kill, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	for {
		sig := <-signals
		log.Info("get signal %s", sig.String())
		switch sig {
		case syscall.SIGHUP:
		// reload()
		default:
			go time.AfterFunc(timeout, func() {
				// log.Warn("app exit now by force...")
				// os.Exit(1)
				log.Exit("app exit now by force...")
				log.Close()
			})

			// 要么fastFailTimeout时间内执行完毕下面的逻辑然后程序退出，要么执行上面的超时函数程序强行退出
			uninitServer()
			// fmt.Println("app exit now...")
			log.Exit("app exit now...")
			log.Close()
			return
		}
	}
}
