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
	// "flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	// "strings"
	"sync/atomic"
	"syscall"
	"time"
)

import (
	"github.com/AlexStocks/getty"
	"github.com/AlexStocks/gocolor"
	"github.com/AlexStocks/goext/net"
	"github.com/AlexStocks/goext/time"
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
	gocolor.Info("%s starts successfull! its version=%s\n", conf.AppName, Version)
	log.Info("%s starts successfull! its version=%s\n", conf.AppName, Version)

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

func newSession(session *getty.Session) error {
	var (
		ok      bool
		tcpConn *net.TCPConn
	)

	if conf.GettySessionParam.CompressEncoding {
		session.SetCompressType(getty.CompressZip)
	}

	if tcpConn, ok = session.Conn().(*net.TCPConn); !ok {
		panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp connection\n", session.Stat(), session.Conn()))
	}

	tcpConn.SetNoDelay(conf.GettySessionParam.TcpNoDelay)
	tcpConn.SetKeepAlive(conf.GettySessionParam.TcpKeepAlive)
	tcpConn.SetReadBuffer(conf.GettySessionParam.TcpRBufSize)
	tcpConn.SetWriteBuffer(conf.GettySessionParam.TcpWBufSize)

	session.SetName(conf.GettySessionParam.SessionName)
	session.SetPkgHandler(NewEchoPackageHandler())
	session.SetEventListener(newEchoMessageHandler())
	session.SetRQLen(conf.GettySessionParam.PkgRQSize)
	session.SetWQLen(conf.GettySessionParam.PkgWQSize)
	session.SetReadDeadline(conf.GettySessionParam.tcpReadTimeout)
	session.SetWriteDeadline(conf.GettySessionParam.tcpWriteTimeout)
	session.SetCronPeriod((int)(conf.heartbeatPeriod.Nanoseconds() / 1e6))
	session.SetWaitTime(conf.GettySessionParam.waitTimeout)
	log.Debug("client new session:%s\n", session.Stat())

	return nil
}

func initClient() {
	client.gettyClient = getty.NewClient(
		(int)(conf.ConnectionNum),
		conf.connectInterval,
		gxnet.HostAddress(conf.ServerHost, conf.ServerPort),
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
			})

			// 要么survialTimeout时间内执行完毕下面的逻辑然后程序退出，要么执行上面的超时函数程序强行退出
			uninitClient()
			// fmt.Println("app exit now...")
			log.Exit("app exit now...")
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
		err := session.WritePkg(&pkg)
		if err != nil {
			log.Warn("session.WritePkg(session{%s}, pkg{%s}) = error{%v}", session.Stat(), pkg, err)
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
	gocolor.Info("after loop %d times, echo cost %d ms", conf.EchoTimes, cost/1e6)
}
