/******************************************************
# DESC    : echo client app
# AUTHOR  : Alex Stocks
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2016-09-06 17:24
# FILE    : main.go
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
	"github.com/AlexStocks/getty-examples/rpc/proto"
	"github.com/AlexStocks/getty/rpc"
	"github.com/AlexStocks/goext/net"
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

const (
	pprofPath = "/debug/pprof/"
)

var (
	client *rpc.Client
)

////////////////////////////////////////////////////////////////////
// main
////////////////////////////////////////////////////////////////////

func main() {
	initConf()

	initProfiling()

	initClient()
	// gxlog.CInfo("%s starts successfull! its version=%s\n", conf.AppName, Version)
	log.Info("%s starts successfull! its version=%s\n", conf.AppName, Version)

	go test()

	initSignal()
}

func initProfiling() {
	var (
		addr string
	)

	addr = gxnet.HostAddress(conf.Host, conf.ProfilePort)
	log.Info("App Profiling startup on address{%v}", addr+pprofPath)
	go func() {
		log.Info(http.ListenAndServe(addr, nil))
	}()
}

func initClient() {
	var err error
	client, err = rpc.NewClient(conf)
	if err != nil {
		panic(jerrors.ErrorStack(err))
	}
}

func uninitClient() {
	client.Close()
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

			// 要么survialTimeout时间内执行完毕下面的逻辑然后程序退出，要么执行上面的超时函数程序强行退出
			uninitClient()
			// fmt.Println("app exit now...")
			log.Exit("app exit now...")
			log.Close()
			return
		}
	}
}

func testJSON() {
	ts := rpc_examples.TestService{}
	testReq := rpc_examples.TestReq{"aaa", "bbb", "ccc"}
	testRsp := rpc_examples.TestRsp{}
	err := client.Call(rpc.CodecJson, "127.0.0.1:20000", ts.Service(), "Test", &testReq, &testRsp)
	if err != nil {
		log.Error("client.Call(Json, TestService::Test) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Test(Json, param:%#v) = res:%s", testReq, testRsp)

	addReq := rpc_examples.AddReq{1, 10}
	addRsp := rpc_examples.AddRsp{}
	err = client.Call(rpc.CodecJson, "127.0.0.1:10000", ts.Service(), "Add", &addReq, &addRsp)
	if err != nil {
		log.Error("client.Call(Json, TestService::Add) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Add(Json, req:%#v) = res:%#v", addReq, addRsp)

	errReq := rpc_examples.ErrReq{1}
	errRsp := rpc_examples.ErrRsp{}
	err = client.Call(rpc.CodecJson, "127.0.0.1:20000", ts.Service(), "Err", &errReq, &errRsp)
	if err != nil {
		// error test case, this invocation should step into this branch.
		log.Error("client.Call(Json, TestService::Err) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Err(Json, req:%#v) = res:%s", errReq, errRsp)
}

func testProtobuf() {
	ts := rpc_examples.TestService{}
	testReq := rpc_examples.TestReq{"aaa", "bbb", "ccc"}
	testRsp := rpc_examples.TestRsp{}
	err := client.Call(rpc.CodecProtobuf, "127.0.0.1:20000", ts.Service(), "Test", &testReq, &testRsp)
	if err != nil {
		log.Error("client.Call(protobuf, TestService::Test) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Test(protobuf, param:%#v) = res:%s", testReq, testRsp)

	addReq := rpc_examples.AddReq{1, 10}
	addRsp := rpc_examples.AddRsp{}
	err = client.Call(rpc.CodecProtobuf, "127.0.0.1:10000", ts.Service(), "Add", &addReq, &addRsp)
	if err != nil {
		log.Error("client.Call(protobuf, TestService::Add) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Add(protobuf, req:%#v) = res:%#v", addReq, addRsp)

	errReq := rpc_examples.ErrReq{1}
	errRsp := rpc_examples.ErrRsp{}
	err = client.Call(rpc.CodecProtobuf, "127.0.0.1:20000", ts.Service(), "Err", &errReq, &errRsp)
	if err != nil {
		// error test case, this invocation should step into this branch.
		log.Error("client.Call(protobuf, TestService::Err) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Err(protobuf, req:%#v) = res:%#v", errReq, errRsp)
}

func test() {
	for i := 0; i < 5; i++ {
		testJSON()
		testProtobuf()
	}
}
