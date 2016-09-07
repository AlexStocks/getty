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
	log "github.com/AlexStocks/log4go"
)

const (
	tcpRBufSize        = 256 * 1024
	tcpWBufSize        = 64 * 1024
	pkgRQSize          = 1024
	pkgWQSize          = 64
	tcpReadTimeout     = 1e9
	tcpWriteTimeout    = 5e9
	waitTimeout        = 5e9 // 5s
	echoSessionTimeout = 5e9
	maxSessionNum      = 100
	sessionName        = "echo-client"
)

const (
	survivalTimeout = 3e9
	pprofPath       = "/debug/pprof/"
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

	go test()

	initSignal()
}

func initProfiling() {
	var (
		addr string
	)

	addr = getty.HostAddress(conf.LocalHost, conf.ProfilePort)
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

	if tcpConn, ok = session.Conn().(*net.TCPConn); !ok {
		panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp connection\n", session.Stat(), session.Conn()))
	}

	tcpConn.SetNoDelay(true)
	tcpConn.SetReadBuffer(tcpRBufSize)
	tcpConn.SetWriteBuffer(tcpWBufSize)
	tcpConn.SetKeepAlive(true)

	session.SetName(sessionName)
	session.SetPkgHandler(NewEchoPackageHandler())
	session.SetEventListener(newEchoMessageHandler())
	session.SetRQLen(pkgRQSize)
	session.SetWQLen(pkgWQSize)
	session.SetReadDeadline(tcpReadTimeout)
	session.SetWriteDeadline(tcpWriteTimeout)
	session.SetCronPeriod((int)(conf.heartbeatPeriod.Nanoseconds() / 1e6))
	session.SetWaitTime(waitTimeout)
	log.Debug("client new session:%s\n", session.Stat())

	return nil
}

func initClient() {
	client.gettyClient = getty.NewClient(
		(int)(conf.ConnectionNum),
		conf.connectInterval,
		getty.HostAddress(conf.ServerHost, conf.ServerPort),
	)
	client.gettyClient.RunEventLoop(newSession)
}

func uninitClient() {
	client.close()
}

func initSignal() {
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
			go time.AfterFunc(survivalTimeout, func() {
				log.Warn("app exit now by force...")
				os.Exit(1)
			})

			// 要么survialTimeout时间内执行完毕下面的逻辑然后程序退出，要么执行上面的超时函数程序强行退出
			uninitClient()
			fmt.Println("app exit now...")
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
	pkg.B = echoMessage
	pkg.H.Len = (uint16)(len(pkg.B))

	if session := client.selectSession(); session != nil {
		err := session.WritePkg(&pkg)
		if err != nil {
			log.Warn("session.WritePkg(session{%s}, pkg{%s}) = error{%v}", session.Stat(), pkg, err)
			session.Close()
		}
	}
}

func test() {
	for {
		if client.isAvailable() {
			break
		}
		time.Sleep(1e9)
	}

	for i := 0; i < conf.EchoTimes; i++ {
		echo()
	}
}
