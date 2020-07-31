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
	// "flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"

	"sync/atomic"
	"syscall"
	"time"
)

import (
	"github.com/AlexStocks/getty/transport"
	gxlog "github.com/AlexStocks/goext/log"
	gxnet "github.com/AlexStocks/goext/net"
	gxtime "github.com/AlexStocks/goext/time"
	log "github.com/AlexStocks/log4go"
)

const (
	pprofPath = "/debug/pprof/"
)

const (
	WritePkgTimeout = 1e9
	WritePkgASAP    = 0e9
)

var (
	connectedClient   EchoClient
	unconnectedClient EchoClient
)

////////////////////////////////////////////////////////////////////
// main
////////////////////////////////////////////////////////////////////

func main() {
	initConf()

	initProfiling()

	initClient()
	log.Info("%s starts successfull! its version=%s\n", conf.AppName, getty.Version)
	gxlog.CInfo("%s starts successfull! its version=%s\n", conf.AppName, getty.Version)

	go test()

	initSignal()
}

func initProfiling() {
	var (
		addr string
	)

	addr = gxnet.HostAddress(conf.LocalHost, conf.ProfilePort)
	log.Info("App Profiling startup on address{%v}", addr+pprofPath)
	go func() {
		log.Info(http.ListenAndServe(addr, nil))
	}()
}

func newSession(session getty.Session) error {
	var (
		ok          bool
		udpConn     *net.UDPConn
		gettyClient getty.Client
		client      *EchoClient
		sessionName string
	)

	if gettyClient, ok = session.EndPoint().(getty.Client); !ok {
		panic(fmt.Sprintf("the endpoint type of session{%#v} is not getty.Client", session))
	}

	switch gettyClient {
	case connectedClient.gettyClient:
		client = &connectedClient
		sessionName = "connected-" + conf.GettySessionParam.SessionName

	case unconnectedClient.gettyClient:
		client = &unconnectedClient
		sessionName = "unconnected-" + conf.GettySessionParam.SessionName

	default:
		panic(fmt.Sprintf("illegal session{%#v} endpoint", session))
	}

	if conf.GettySessionParam.CompressEncoding {
		session.SetCompressType(getty.CompressZip)
	}

	if udpConn, ok = session.Conn().(*net.UDPConn); !ok {
		panic(fmt.Sprintf("%s, session.conn{%#v} is not udp connection\n", session.Stat(), session.Conn()))
	}

	udpConn.SetReadBuffer(conf.GettySessionParam.UdpRBufSize)
	udpConn.SetWriteBuffer(conf.GettySessionParam.UdpWBufSize)

	session.SetName(sessionName)
	session.SetMaxMsgLen(conf.GettySessionParam.MaxMsgLen)
	session.SetPkgHandler(echoPkgHandler)
	session.SetEventListener(newEchoMessageHandler(client))
	session.SetRQLen(conf.GettySessionParam.PkgRQSize)
	session.SetWQLen(conf.GettySessionParam.PkgWQSize)
	session.SetReadTimeout(conf.GettySessionParam.udpReadTimeout)
	session.SetWriteTimeout(conf.GettySessionParam.udpWriteTimeout)
	session.SetCronPeriod((int)(conf.heartbeatPeriod.Nanoseconds() / 1e6))
	session.SetWaitTime(conf.GettySessionParam.waitTimeout)
	log.Debug("client new session:%s\n", session.Stat())
	gxlog.CDebug("client new session:%s\n", session.Stat())

	return nil
}

func initClient() {
	unconnectedClient.gettyClient = getty.NewUDPPEndPoint(
		getty.WithLocalAddress(gxnet.HostAddress(net.IPv4zero.String(), 0)),
	)
	unconnectedClient.gettyClient.RunEventLoop(newSession)
	unconnectedClient.serverAddr = net.UDPAddr{IP: net.ParseIP(conf.ServerHost), Port: conf.ServerPort}

	connectedClient.gettyClient = getty.NewUDPClient(
		getty.WithServerAddress(gxnet.HostAddress(conf.ServerHost, conf.ServerPort)),
		getty.WithConnectionNumber((int)(conf.ConnectionNum)),
	)
	connectedClient.gettyClient.RunEventLoop(newSession)
}

func uninitClient() {
	connectedClient.close()
	unconnectedClient.close()
}

func initSignal() {
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
			go time.AfterFunc(conf.failFastTimeout, func() {
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

func echo(client *EchoClient) {
	var (
		err error
		pkg EchoPackage
		ctx getty.UDPContext
	)

	pkg.H.Magic = echoPkgMagic
	pkg.H.LogID = (uint32)(src.Int63())
	pkg.H.Sequence = atomic.AddUint32(&reqID, 1)
	// pkg.H.ServiceID = 0
	pkg.H.Command = echoCmd
	pkg.B = conf.EchoString
	pkg.H.Len = (uint16)(len(pkg.B)) + 1

	ctx.Pkg = &pkg
	ctx.PeerAddr = &(client.serverAddr)

	if session := client.selectSession(); session != nil {
		// err := session.WritePkg(ctx, WritePkgTimeout)
		err = session.WritePkg(ctx, WritePkgASAP)
		if err != nil {
			log.Warn("session.WritePkg(session{%s}, UDPContext{%#v}) = error{%v}", session.Stat(), ctx, err)
			session.Close()
			client.removeSession(session)
		}
	}
}

func testEchoClient(client *EchoClient) {
	var (
		cost    int64
		counter gxtime.CountWatch
	)

	for {
		if client.isAvailable() {
			break
		}
		time.Sleep(3e9)
	}

	counter.Start()
	for i := 0; i < conf.EchoTimes; i++ {
		echo(client)
	}
	cost = counter.Count()
	log.Info("after loop %d times, echo cost %d ms", conf.EchoTimes, cost/1e6)
}

func test() {
	testEchoClient(&unconnectedClient)
	testEchoClient(&connectedClient)
}
