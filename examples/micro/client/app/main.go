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
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

import (
	"github.com/AlexStocks/goext/database/filter"
	"github.com/AlexStocks/goext/database/registry"
	"github.com/AlexStocks/goext/net"

	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

import (
	"github.com/AlexStocks/getty/micro"
	"github.com/AlexStocks/getty/rpc"
	"github.com/AlexStocks/getty/transport"
)

const (
	pprofPath = "/debug/pprof/"
)

var (
	client *micro.Client
	seq    int64
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

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

type NodePartitionReplica struct {
	GroupID int `json:"group_id,omitempty"`
	NodeID  int `json:"node_id,omitempty"`
}

func LoadBalance(ctx context.Context, sa *gxfilter.ServiceArray) (*gxregistry.Service, error) {
	var (
		ok      bool
		seq     int64
		groupID int
	)

	if ctx == nil {
		seq = int64(rand.Int())
		groupID = 1
	} else {
		if seq, ok = ctx.Value("seq").(int64); !ok {
			return nil, jerrors.Errorf("illegal seq %#v", ctx.Value("seq"))
		}
		if groupID, ok = ctx.Value("group_id").(int); !ok {
			return nil, jerrors.Errorf("illegal group_id %#v", ctx.Value("group_id"))
		}
	}

	var (
		npr          NodePartitionReplica
		serviceArray = make([]*gxregistry.Service, 0)
	)
	for idx := range sa.Arr {
		meta := micro.GetServiceNodeMetadata(sa.Arr[idx])
		// log.Error("xxxxxxxxxxxxx idx:%d: %s, meta:%s", idx, sa.Arr[idx], meta)
		if len(meta) != 0 {
			err := json.Unmarshal([]byte(meta), &npr)
			// log.Error("meta:%s, npr:%#v", meta, npr)
			if err != nil {
				log.Warn("illegal node meta:%s", meta)
				continue
			}
			if npr.GroupID == groupID {
				serviceArray = append(serviceArray, sa.Arr[idx])
			}
		}
	}

	arrLen := len(serviceArray)
	if arrLen == 0 {
		return nil, jerrors.Errorf("can not get service whose group is %d from @arr %#v", groupID, sa)
	}

	service := serviceArray[int(seq)%arrLen]
	// log.Error("seq %d, service %#v", seq, service)
	return service, nil
	// return sa.Arr[seq%arrLen], nil
	// context.WithValue(context.Background(), key, value).Value(key)
}

func initClient() {
	var err error
	client, err = micro.NewClient(&conf.ClientConfig, &conf.Registry, micro.WithServiceHash(LoadBalance))
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

	ctx := context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 2)
	eventReq := EventReq{A: "hello"}
	err := client.CallOneway(ctx, rpc.CodecJson, ts.Service(), ts.Version(), "Event", &eventReq,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		log.Error("client.CallOneway(Json, TestService::Event) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Event(Json, req:%#v)", eventReq)

	ctx = context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 1)
	testReq := TestReq{"aaa", "bbb", "ccc"}
	testRsp := TestRsp{}
	err = client.Call(ctx, rpc.CodecJson, ts.Service(), ts.Version(), "Test", &testReq, &testRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		log.Error("client.Call(Json, TestService::Test) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Test(Json, param:%#v) = res:%s", testReq, testRsp)

	ctx = context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 2)
	addReq := AddReq{1, 10}
	addRsp := AddRsp{}
	err = client.Call(ctx, rpc.CodecJson, ts.Service(), ts.Version(), "Add", &addReq, &addRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		log.Error("client.Call(Json, TestService::Add) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Add(Json, req:%#v) = res:%#v", addReq, addRsp)

	ctx = context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 1)
	errReq := ErrReq{1}
	errRsp := ErrRsp{}
	err = client.Call(ctx, rpc.CodecJson, ts.Service(), ts.Version(), "Err", &errReq, &errRsp,
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

	ctx := context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 1)
	testReq := TestReq{"aaa", "bbb", "ccc"}
	testRsp := TestRsp{}
	err := client.AsyncCall(ctx, rpc.CodecJson,
		ts.Service(), ts.Version(), "Test", &testReq, Callback, &testRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Test::Json"))
	if err != nil {
		log.Error("client.AsyncCall(Json, TestService::Test) = error:%s", jerrors.ErrorStack(err))
		return
	}

	ctx = context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 2)
	addReq := AddReq{1, 10}
	addRsp := AddRsp{}
	err = client.AsyncCall(ctx, rpc.CodecJson, ts.Service(),
		ts.Version(), "Add", &addReq, Callback, &addRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Add::Json"))
	if err != nil {
		log.Error("client.AsyncCall(Json, TestService::Add) = error:%s", jerrors.ErrorStack(err))
		return
	}

	ctx = context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 1)
	errReq := ErrReq{1}
	errRsp := ErrRsp{}
	err = client.AsyncCall(ctx, rpc.CodecJson, ts.Service(),
		ts.Version(), "Err", &errReq, Callback, &errRsp,
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

	ctx := context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 1)
	eventReq := EventReq{A: "hello"}
	err := client.CallOneway(ctx, rpc.CodecJson, ts.Service(), ts.Version(), "Event", &eventReq,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		log.Error("client.AsyncCall(Protobuf, TestService::Event) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Event(Protobuf, req:%#v)", eventReq)

	ctx = context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 2)
	testReq := TestReq{"aaa", "bbb", "ccc"}
	testRsp := TestRsp{}
	err = client.Call(ctx, rpc.CodecProtobuf, ts.Service(), ts.Version(), "Test", &testReq,
		&testRsp, rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		log.Error("client.Call(protobuf, TestService::Test) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Test(protobuf, param:%#v) = res:%s", testReq, testRsp)

	ctx = context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 1)
	addReq := AddReq{1, 10}
	addRsp := AddRsp{}
	err = client.Call(ctx, rpc.CodecProtobuf, ts.Service(), ts.Version(), "Add", &addReq,
		&addRsp, rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		log.Error("client.Call(protobuf, TestService::Add) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Add(protobuf, req:%#v) = res:%#v", addReq, addRsp)

	ctx = context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 2)
	errReq := ErrReq{1}
	errRsp := ErrRsp{}
	err = client.Call(ctx, rpc.CodecProtobuf, ts.Service(), ts.Version(), "Err", &errReq,
		&errRsp, rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6))
	if err != nil {
		// error test case, this invocation should step into this branch.
		log.Error("client.Call(protobuf, TestService::Err) = error:%s", jerrors.ErrorStack(err))
		return
	}
	log.Info("TestService::Err(protobuf, req:%#v) = res:%#v", errReq, errRsp)
}

func testAsyncProtobuf() {
	ts := TestService{}

	ctx := context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 1)
	testReq := TestReq{"aaa", "bbb", "ccc"}
	testRsp := TestRsp{}
	err := client.AsyncCall(ctx, rpc.CodecProtobuf,
		ts.Service(), ts.Version(), "Test", &testReq, Callback, &testRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Test::Protobuf"))
	if err != nil {
		log.Error("client.AsyncCall(protobuf, TestService::Test) = error:%s", jerrors.ErrorStack(err))
		return
	}

	ctx = context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 2)
	addReq := AddReq{1, 10}
	addRsp := AddRsp{}
	err = client.AsyncCall(ctx, rpc.CodecProtobuf,
		ts.Service(), ts.Version(), "Add", &addReq, Callback, &addRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Add::Protobuf"))
	if err != nil {
		log.Error("client.AsyncCall(protobuf, TestService::Add) = error:%s", jerrors.ErrorStack(err))
		return
	}

	ctx = context.WithValue(context.Background(), "seq", atomic.AddInt64(&seq, 1))
	ctx = context.WithValue(ctx, "group_id", 1)
	errReq := ErrReq{1}
	errRsp := ErrRsp{}
	err = client.AsyncCall(ctx, rpc.CodecProtobuf,
		ts.Service(), ts.Version(), "Err", &errReq, Callback, &errRsp,
		rpc.WithCallRequestTimeout(100e6), rpc.WithCallResponseTimeout(100e6),
		rpc.WithCallMeta("hello", "Service::Err::protobuf"))
	if err != nil {
		// error test case, this invocation should step into this branch.
		log.Error("client.Call(protobuf, TestService::Err) = error:%s", jerrors.ErrorStack(err))
		return
	}
}

func test() {
	for i := 0; i < 1; i++ {
		log.Debug("start to test json:")
		testJSON()
		log.Debug("start to test async json:")
		testAsyncJSON()
		log.Debug("start to test pb:")
		testProtobuf()
		log.Debug("start to test async pb:")
		testAsyncProtobuf()
	}
}
