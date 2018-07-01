package rpc

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"
)

import (
	"github.com/AlexStocks/getty"
	"github.com/AlexStocks/goext/net"
	jerrors "github.com/juju/errors"

	log "github.com/AlexStocks/log4go"
)

type Server struct {
	tcpServerList []getty.Server
	serviceMap    map[string]*service
	conf          *Config
}

func NewServer(conf *Config) *Server {
	s := &Server{
		serviceMap: make(map[string]*service),
		conf:       conf,
	}

	return s
}

func (s *Server) Run() {
	s.Init()
	log.Info("%s starts successfull! its version=%s, its listen ends=%s:%s\n",
		s.conf.AppName, getty.Version, s.conf.Host, s.conf.Ports)
	s.initSignal()
}

func (s *Server) Register(rcvr interface{}) error {
	svc := &service{
		typ:  reflect.TypeOf(rcvr),
		rcvr: reflect.ValueOf(rcvr),
		name: reflect.Indirect(reflect.ValueOf(rcvr)).Type().Name(),
		// Install the methods
		method: prepareMethods(reflect.TypeOf(rcvr)),
	}
	if svc.name == "" {
		s := "rpc.Register: no service name for type " + svc.typ.String()
		log.Error(s)
		return jerrors.New(s)
	}
	if !isExported(svc.name) {
		s := "rpc.Register: type " + svc.name + " is not exported"
		log.Error(s)
		return jerrors.New(s)
	}
	if _, present := s.serviceMap[svc.name]; present {
		return jerrors.New("rpc: service already defined: " + svc.name)
	}

	if len(svc.method) == 0 {
		// To help the user, see if a pointer receiver would work.
		method := prepareMethods(reflect.PtrTo(svc.typ))
		str := "rpc.Register: type " + svc.name + " has no exported methods of suitable type"
		if len(method) != 0 {
			str = "rpc.Register: type " + svc.name + " has no exported methods of suitable type (" +
				"hint: pass a pointer to value of that type)"
		}
		log.Error(str)

		return jerrors.New(str)
	}

	s.serviceMap[svc.name] = svc

	return nil
}

func (s *Server) newSession(session getty.Session) error {
	var (
		ok      bool
		tcpConn *net.TCPConn
	)

	if s.conf.GettySessionParam.CompressEncoding {
		session.SetCompressType(getty.CompressZip)
	}

	if tcpConn, ok = session.Conn().(*net.TCPConn); !ok {
		panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp connection\n", session.Stat(), session.Conn()))
	}

	tcpConn.SetNoDelay(s.conf.GettySessionParam.TcpNoDelay)
	tcpConn.SetKeepAlive(s.conf.GettySessionParam.TcpKeepAlive)
	if s.conf.GettySessionParam.TcpKeepAlive {
		tcpConn.SetKeepAlivePeriod(s.conf.GettySessionParam.keepAlivePeriod)
	}
	tcpConn.SetReadBuffer(s.conf.GettySessionParam.TcpRBufSize)
	tcpConn.SetWriteBuffer(s.conf.GettySessionParam.TcpWBufSize)

	session.SetName(s.conf.GettySessionParam.SessionName)
	session.SetMaxMsgLen(s.conf.GettySessionParam.MaxMsgLen)
	session.SetPkgHandler(NewRpcServerPacketHandler(s)) //
	session.SetEventListener(NewRpcServerHandler())     //
	session.SetRQLen(s.conf.GettySessionParam.PkgRQSize)
	session.SetWQLen(s.conf.GettySessionParam.PkgWQSize)
	session.SetReadTimeout(s.conf.GettySessionParam.tcpReadTimeout)
	session.SetWriteTimeout(s.conf.GettySessionParam.tcpWriteTimeout)
	session.SetCronPeriod((int)(s.conf.sessionTimeout.Nanoseconds() / 1e6))
	session.SetWaitTime(s.conf.GettySessionParam.waitTimeout)
	log.Debug("app accepts new session:%s\n", session.Stat())

	return nil
}

func (s *Server) Init() {
	var (
		addr      string
		portList  []string
		tcpServer getty.Server
	)

	portList = s.conf.Ports
	if len(portList) == 0 {
		panic("portList is nil")
	}
	for _, port := range portList {
		addr = gxnet.HostAddress2(s.conf.Host, port)
		tcpServer = getty.NewTCPServer(
			getty.WithLocalAddress(addr),
		)
		// run s
		tcpServer.RunEventLoop(s.newSession)
		log.Debug("s bind addr{%s} ok!", addr)
		s.tcpServerList = append(s.tcpServerList, tcpServer)
	}
}

func (s *Server) Stop() {
	for _, tcpServer := range s.tcpServerList {
		tcpServer.Close()
	}
}

func (s *Server) initSignal() {
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
			go time.AfterFunc(s.conf.failFastTimeout, func() {
				// log.Warn("app exit now by force...")
				// os.Exit(1)
				log.Exit("app exit now by force...")
				log.Close()
			})

			// 要么survialTimeout时间内执行完毕下面的逻辑然后程序退出，要么执行上面的超时函数程序强行退出
			s.Stop()
			// fmt.Println("app exit now...")
			log.Exit("app exit now...")
			log.Close()
			return
		}
	}
}
