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
	// "flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	// "strings"
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
	sessionName        = "echo-server"
)

const (
	survivalTimeout = 3e9
	pprofPath       = "/debug/pprof/"
)

var (
// host  = flag.String("host", "127.0.0.1", "local host address that server app will use")
// ports = flag.String("ports", "12345,12346,12347", "local host port list that the server app will bind")
)

var (
	serverList []*getty.Server
)

func main() {
	// flag.Parse()
	// if *host == "" || *ports == "" {
	// 	panic(fmt.Sprintf("Please intput local host ip or port lists"))
	// }

	initConf()

	initProfiling()

	initServer()

	initSignal()
}

func initProfiling() {
	var (
		addr string
	)

	// addr = *host + ":" + "10000"
	addr = getty.HostAddress(conf.Host, conf.ProfilePort)
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
	session.SetWaitTime(waitTimeout)
	log.Debug("app accepts new session:%s\n", session.Stat())

	return nil
}

func initServer() {
	var (
		err      error
		addr     string
		portList []string
		server   *getty.Server
	)

	// if *host == "" {
	// 	panic("host can not be nil")
	// }
	// if *ports == "" {
	// 	panic("ports can not be nil")
	// }

	// portList = strings.Split(*ports, ",")

	portList = conf.Ports
	if len(portList) == 0 {
		panic("portList is nil")
	}
	for _, port := range portList {
		server = getty.NewServer()
		// addr = *host + ":" + port
		// addr = conf.Host + ":" + port
		addr = getty.HostAddress2(conf.Host, port)
		err = server.Listen("tcp", addr)
		if err != nil {
			panic(fmt.Sprintf("server.Listen(tcp, addr:%s) = error{%#v}", addr, err))
		}

		// run server
		server.RunEventloop(newSession)
		log.Debug("server bind addr{%s} ok!", addr)
		serverList = append(serverList, server)
	}
}

func uninitServer() {
	for _, server := range serverList {
		server.Close()
	}
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
			uninitServer()
			fmt.Println("app exit now...")
			return
		}
	}
}
