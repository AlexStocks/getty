/******************************************************
# DESC    : echo server
# AUTHOR  : Alex Stocks
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2016-09-04 15:49
# FILE    : server.go
******************************************************/

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
	"github.com/AlexStocks/getty"
	"github.com/AlexStocks/getty/micro"
)

const (
	pprofPath = "/debug/pprof/"
)

var (
	server *micro.Server
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
	server, err = micro.NewServer(&conf.ServerConfig, &conf.Registry)
	if err != nil {
		panic(jerrors.ErrorStack(err))
		return
	}
	err = server.Register(&TestService{})
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
