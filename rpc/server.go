package rpc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	"github.com/AlexStocks/getty"
	"github.com/AlexStocks/goext/log"
	"github.com/AlexStocks/goext/net"

	log "github.com/AlexStocks/log4go"
)

type Server struct {
	tcpServerList []getty.Server
	serviceMap    map[string]*service
}

func NewServer() *Server {
	s := &Server{
		serviceMap: make(map[string]*service),
	}

	return s
}

func (server *Server) Run() {
	initConf(defaultServerConfFile)
	initLog(defaultServerLogConfFile)
	initProfiling()
	server.Init()
	gxlog.CInfo("%s starts successfull! its version=%s, its listen ends=%s:%s\n",
		conf.AppName, Version, conf.Host, conf.Ports)
	log.Info("%s starts successfull! its version=%s, its listen ends=%s:%s\n",
		conf.AppName, Version, conf.Host, conf.Ports)
	server.initSignal()
}

func (server *Server) Register(rcvr interface{}) error {
	s := new(service)
	s.typ = reflect.TypeOf(rcvr)
	s.rcvr = reflect.ValueOf(rcvr)
	sname := reflect.Indirect(s.rcvr).Type().Name()
	if sname == "" {
		s := "rpc.Register: no service name for type " + s.typ.String()
		log.Error(s)
		return errors.New(s)
	}
	if !isExported(sname) {
		s := "rpc.Register: type " + sname + " is not exported"
		log.Error(s)
		return errors.New(s)
	}
	if _, present := server.serviceMap[sname]; present {
		return errors.New("rpc: service already defined: " + sname)
	}
	s.name = sname
	// Install the methods
	s.method = suitableMethods(s.typ, true)
	if len(s.method) == 0 {
		str := ""
		// To help the user, see if a pointer receiver would work.
		method := suitableMethods(reflect.PtrTo(s.typ), false)
		if len(method) != 0 {
			str = "rpc.Register: type " + sname + " has no exported methods of suitable type (hint: pass a pointer to value of that type)"
		} else {
			str = "rpc.Register: type " + sname + " has no exported methods of suitable type"
		}
		log.Error(s)
		return errors.New(str)
	}
	server.serviceMap[s.name] = s
	return nil
}

func (server *Server) newSession(session getty.Session) error {
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
	if conf.GettySessionParam.TcpKeepAlive {
		tcpConn.SetKeepAlivePeriod(conf.GettySessionParam.keepAlivePeriod)
	}
	tcpConn.SetReadBuffer(conf.GettySessionParam.TcpRBufSize)
	tcpConn.SetWriteBuffer(conf.GettySessionParam.TcpWBufSize)

	session.SetName(conf.GettySessionParam.SessionName)
	session.SetMaxMsgLen(conf.GettySessionParam.MaxMsgLen)
	session.SetPkgHandler(NewRpcServerPacketHandler(server)) //
	session.SetEventListener(NewRpcServerHandler())          //
	session.SetRQLen(conf.GettySessionParam.PkgRQSize)
	session.SetWQLen(conf.GettySessionParam.PkgWQSize)
	session.SetReadTimeout(conf.GettySessionParam.tcpReadTimeout)
	session.SetWriteTimeout(conf.GettySessionParam.tcpWriteTimeout)
	session.SetCronPeriod((int)(conf.sessionTimeout.Nanoseconds() / 1e6))
	session.SetWaitTime(conf.GettySessionParam.waitTimeout)
	log.Debug("app accepts new session:%s\n", session.Stat())

	return nil
}

func (server *Server) Init() {
	var (
		addr      string
		portList  []string
		tcpServer getty.Server
	)

	portList = conf.Ports
	if len(portList) == 0 {
		panic("portList is nil")
	}
	for _, port := range portList {
		addr = gxnet.HostAddress2(conf.Host, port)
		tcpServer = getty.NewTCPServer(
			getty.WithLocalAddress(addr),
		)
		// run server
		tcpServer.RunEventLoop(server.newSession)
		log.Debug("server bind addr{%s} ok!", addr)
		server.tcpServerList = append(server.tcpServerList, tcpServer)
	}
}

func (server *Server) Stop() {
	for _, tcpServer := range server.tcpServerList {
		tcpServer.Close()
	}
}

func (server *Server) initSignal() {
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

			// 要么survialTimeout时间内执行完毕下面的逻辑然后程序退出，要么执行上面的超时函数程序强行退出
			server.Stop()
			// fmt.Println("app exit now...")
			log.Exit("app exit now...")
			log.Close()
			return
		}
	}
}
