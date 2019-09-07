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
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
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
	"github.com/AlexStocks/getty/rpc"
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
	log.Info("%s starts successfull! its version=%s\n", conf.AppName, getty.Version)

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
	client, err = rpc.NewClient(&conf.ClientConfig)
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

			// 要么fastFailTimeout时间内执行完毕下面的逻辑然后程序退出，要么执行上面的超时函数程序强行退出
			uninitClient()
			// fmt.Println("app exit now...")
			log.Exit("app exit now...")
			log.Close()
			return
		}
	}
}

func testJSON() {
	ts := TestService{}
	testReq := TestReq{"aaa", "bbb", "ccc"}
	testRsp := TestRsp{}
	addr := net.JoinHostPort(conf.ServerHost, strconv.Itoa(conf.ServerPort))

	eventReq := EventReq{A: "hello"}
	err := client.CallOneway(rpc.CodecJson, addr, ts.Service(), "Event", &eventReq,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		log.Error("client.CallOneway(Json, TestService::Event) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Event(Json, req:%#v)", eventReq)

	err = client.Call(rpc.CodecJson, addr, ts.Service(), "Test", &testReq, &testRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		log.Error("client.Call(Json, TestService::Test) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Test(Json, param:%#v) = res:%s", testReq, testRsp)

	addReq := AddReq{1, 10}
	addRsp := AddRsp{}
	err = client.Call(rpc.CodecJson, addr, ts.Service(), "Add", &addReq, &addRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		log.Error("client.Call(Json, TestService::Add) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Add(Json, req:%#v) = res:%#v", addReq, addRsp)

	errReq := ErrReq{1}
	errRsp := ErrRsp{}
	err = client.Call(rpc.CodecJson, addr, ts.Service(), "Err", &errReq, &errRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		// error test case, this invocation should step into this branch.
		log.Error("client.Call(Json, TestService::Err) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Err(Json, req:%#v) = res:%s", errReq, errRsp)
}

func Callback(rsp rpc.CallResponse) {
	log.Info("method:%s, cost time span:%s, error:%s, reply:%#v",
		rsp.Opts.Meta["hello"].(string),
		time.Since(rsp.Start),
		jerrors.ErrorStack(rsp.Cause),
		rsp.Reply)
}

func testAsyncJSON() {
	ts := TestService{}
	testReq := TestReq{"aaa", "bbb", "ccc"}
	testRsp := TestRsp{}
	addr := net.JoinHostPort(conf.ServerHost, strconv.Itoa(conf.ServerPort))

	err := client.AsyncCall(rpc.CodecJson, addr,
		ts.Service(), "Test", &testReq, Callback, &testRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Test::Json"))
	if err != nil {
		log.Error("client.AsyncCall(Json, TestService::Test) = error:%s", jerrors.ErrorStack(err))
		return
	}

	addReq := AddReq{1, 10}
	addRsp := AddRsp{}
	err = client.AsyncCall(rpc.CodecJson, addr,
		ts.Service(), "Add", &addReq, Callback, &addRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Add::Json"))
	if err != nil {
		log.Error("client.AsyncCall(Json, TestService::Add) = error:%s", jerrors.ErrorStack(err))
		return
	}

	errReq := ErrReq{1}
	errRsp := ErrRsp{}
	err = client.AsyncCall(rpc.CodecJson, addr,
		ts.Service(), "Err", &errReq, Callback, &errRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Err::Json"))
	if err != nil {
		// error test case, this invocation should step into this branch.
		log.Error("client.Call(Json, TestService::Err) = error:%s", jerrors.ErrorStack(err))
		return
	}
}

func testProtobuf() {
	ts := TestService{}
	testReq := TestReq{"aaa", "bbb", "ccc"}
	testRsp := TestRsp{}
	addr := net.JoinHostPort(conf.ServerHost, strconv.Itoa(conf.ServerPort))

	eventReq := EventReq{A: "hello"}
	err := client.CallOneway(rpc.CodecProtobuf, addr, ts.Service(), "Event", &eventReq,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		log.Error("client.CallOneway(Protobuf, TestService::Event) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Event(Protobuf, req:%#v)", eventReq)

	err = client.Call(rpc.CodecProtobuf, addr, ts.Service(), "Test", &testReq, &testRsp,
		rpc.WithCallRequestTimeout(500e6), rpc.WithCallResponseTimeout(500e6))
	if err != nil {
		log.Error("client.Call(protobuf, TestService::Test) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Test(protobuf, param:%#v) = res:%s", testReq, testRsp)

	addReq := AddReq{1, 10}
	addRsp := AddRsp{}
	err = client.Call(rpc.CodecProtobuf, addr, ts.Service(), "Add", &addReq, &addRsp,
		rpc.WithCallRequestTimeout(500e6), rpc.WithCallResponseTimeout(500e6))
	if err != nil {
		log.Error("client.Call(protobuf, TestService::Add) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Add(protobuf, req:%#v) = res:%#v", addReq, addRsp)

	errReq := ErrReq{1}
	errRsp := ErrRsp{}
	err = client.Call(rpc.CodecProtobuf, addr, ts.Service(), "Err", &errReq, &errRsp,
		rpc.WithCallRequestTimeout(500e6), rpc.WithCallResponseTimeout(500e6))
	if err != nil {
		// error test case, this invocation should step into this branch.
		log.Error("client.Call(protobuf, TestService::Err) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Err(protobuf, req:%#v) = res:%#v", errReq, errRsp)
}

func testAsyncProtobuf() {
	ts := TestService{}
	testReq := TestReq{"aaa", "bbb", "ccc"}
	testRsp := TestRsp{}
	addr := net.JoinHostPort(conf.ServerHost, strconv.Itoa(conf.ServerPort))

	err := client.AsyncCall(rpc.CodecProtobuf, addr,
		ts.Service(), "Test", &testReq, Callback, &testRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Test::Protobuf"))
	if err != nil {
		log.Error("client.AsyncCall(protobuf, TestService::Test) = error:%s", jerrors.ErrorStack(err))
		return
	}

	addReq := AddReq{1, 10}
	addRsp := AddRsp{}
	err = client.AsyncCall(rpc.CodecProtobuf, addr,
		ts.Service(), "Add", &addReq, Callback, &addRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Add::Protobuf"))
	if err != nil {
		log.Error("client.AsyncCall(protobuf, TestService::Add) = error:%s", jerrors.ErrorStack(err))
		return
	}

	errReq := ErrReq{1}
	errRsp := ErrRsp{}
	err = client.AsyncCall(rpc.CodecProtobuf, addr,
		ts.Service(), "Err", &errReq, Callback, &errRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Err::protobuf"))
	if err != nil {
		// error test case, this invocation should step into this branch.
		log.Error("client.Call(protobuf, TestService::Err) = error:%s", jerrors.ErrorStack(err))
		return
	}
}

func test() {
	// for i := 0; i < 1; i++ {
	testJSON()
	testAsyncJSON()
	testAsyncProtobuf()
	testProtobuf()
	// }
}
