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

	// "strings"
	"crypto/tls"
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

var (
	client EchoClient
)

////////////////////////////////////////////////////////////////////
// main
////////////////////////////////////////////////////////////////////

func main() {
	initConf()

	initProfiling()

	initClient()
	gxlog.CInfo("%s starts successfull! its version=%s\n", conf.AppName, getty.Version)
	log.Info("%s starts successfull! its version=%s\n", conf.AppName, getty.Version)

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
		flag1, flag2 bool
		tcpConn      *net.TCPConn
	)

	_, flag1 = session.Conn().(*tls.Conn)
	tcpConn, flag2 = session.Conn().(*net.TCPConn)
	if !flag1 && !flag2 {
		panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp/tls connection\n", session.Stat(), session.Conn()))
	}

	if conf.GettySessionParam.CompressEncoding {
		session.SetCompressType(getty.CompressZip)
	}
	// else {
	//	session.SetCompressType(getty.CompressNone)
	//}

	if flag2 {
		tcpConn.SetNoDelay(conf.GettySessionParam.TcpNoDelay)
		tcpConn.SetKeepAlive(conf.GettySessionParam.TcpKeepAlive)
		tcpConn.SetReadBuffer(conf.GettySessionParam.TcpRBufSize)
		tcpConn.SetWriteBuffer(conf.GettySessionParam.TcpWBufSize)
	}

	session.SetName(conf.GettySessionParam.SessionName)
	session.SetMaxMsgLen(conf.GettySessionParam.MaxMsgLen)
	session.SetPkgHandler(echoPkgHandler)
	session.SetEventListener(echoMsgHandler)
	session.SetRQLen(conf.GettySessionParam.PkgRQSize)
	session.SetWQLen(conf.GettySessionParam.PkgWQSize)
	session.SetReadTimeout(conf.GettySessionParam.tcpReadTimeout)
	session.SetWriteTimeout(conf.GettySessionParam.tcpWriteTimeout)
	session.SetCronPeriod((int)(conf.heartbeatPeriod.Nanoseconds() / 1e6))
	session.SetWaitTime(conf.GettySessionParam.waitTimeout)
	log.Debug("client new session:%s\n", session.Stat())

	return nil
}

func initClient() {
	client.gettyClient = getty.NewWSClient(
		getty.WithServerAddress(gxnet.WSHostAddress(conf.ServerHost, conf.ServerPort, conf.ServerPath)),
		getty.WithConnectionNumber((int)(conf.ConnectionNum)),
	)

	client.gettyClient.RunEventLoop(newSession)
}

func uninitClient() {
	client.close()
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

func echo() {
	var pkg EchoPackage
	pkg.H.Magic = echoPkgMagic
	pkg.H.LogID = (uint32)(src.Int63())
	pkg.H.Sequence = atomic.AddUint32(&reqID, 1)
	// pkg.H.ServiceID = 0
	pkg.H.Command = echoCmd
	pkg.B = conf.EchoString
	pkg.H.Len = (uint16)(len(pkg.B)) + 1

	if session := client.selectSession(); session != nil {
		err := session.WritePkg(&pkg, conf.GettySessionParam.waitTimeout)
		if err != nil {
			log.Warn("session.WritePkg(session{%s}, pkg{%s}, timeout{%d}) = error{%v}",
				session.Stat(), pkg, conf.GettySessionParam.waitTimeout, err)
			session.Close()
			client.removeSession(session)
		}
	}
}

func test() {
	for {
		if client.isAvailable() {
			break
		}
		time.Sleep(1e6)
	}

	var (
		cost    int64
		counter gxtime.CountWatch
	)
	counter.Start()
	for i := 0; i < conf.EchoTimes; i++ {
		echo()
	}
	cost = counter.Count()
	log.Info("after loop %d times, echo cost %d ms", conf.EchoTimes, cost/1e6)
	gxlog.CInfo("after loop %d times, echo cost %d ms", conf.EchoTimes, cost/1e6)
}
