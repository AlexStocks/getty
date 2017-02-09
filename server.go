/******************************************************
# DESC       : getty server
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2016-08-17 11:21
# FILE       : server.go
******************************************************/

package getty

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sync"
	"time"
)

import (
	"github.com/AlexStocks/goext/log"
	"github.com/AlexStocks/goext/net"
	"github.com/AlexStocks/goext/sync"
	"github.com/AlexStocks/goext/time"
	log "github.com/AlexStocks/log4go"
	"github.com/gorilla/websocket"
)

var (
	errSelfConnect        = errors.New("connect self!")
	serverFastFailTimeout = gxtime.TimeSecondDuration(1)
)

type Server struct {
	// net
	addr     string
	listener net.Listener
	lock     sync.Mutex   // for server
	server   *http.Server // for ws or wss server

	sync.Once
	done chan gxsync.Empty
	wg   sync.WaitGroup
}

func NewServer() *Server {
	return &Server{done: make(chan gxsync.Empty)}
}

func (s *Server) stop() {
	var (
		err error
		ctx context.Context
	)
	select {
	case <-s.done:
		return
	default:
		s.Once.Do(func() {
			close(s.done)
			s.lock.Lock()
			if s.server != nil {
				ctx, _ = context.WithTimeout(context.Background(), serverFastFailTimeout)
				if err = s.server.Shutdown(ctx); err != nil {
					// 如果下面内容输出为：server shutdown ctx: context deadline exceeded，
					// 则说明有未处理完的active connections。
					log.Error("server shutdown ctx:%#v", err)
				}
			}
			s.lock.Unlock()
			// 把listener.Close放在这里，既能防止多次关闭调用，
			// 又能及时让Server因accept返回错误而从RunEventloop退出
			s.listener.Close()
		})
	}
}

func (s *Server) IsClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// (Server)Bind's functionality is equal to (Server)Listen.
func (s *Server) Bind(network string, host string, port int) error {
	if port <= 0 {
		return errors.New("port<=0 illegal")
	}

	return s.Listen(network, gxnet.HostAddress(host, port))
}

// net.ipv4.tcp_max_syn_backlog
// net.ipv4.tcp_timestamps
// net.ipv4.tcp_tw_recycle
func (s *Server) Listen(network string, addr string) error {
	listener, err := net.Listen(network, addr)
	if err != nil {
		return err
	}
	s.addr = addr
	s.listener = listener

	return nil
}

func (s *Server) RunEventloop(newSession NewSessionCallback) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		var (
			err    error
			client Session
			delay  time.Duration
		)
		for {
			if s.IsClosed() {
				log.Warn("Server{%s} stop acceptting client connect request.", s.addr)
				return
			}
			if delay != 0 {
				time.Sleep(delay)
			}
			client, err = s.accept(newSession)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
					if delay == 0 {
						delay = 5 * time.Millisecond
					} else {
						delay *= 2
					}
					if max := 1 * time.Second; delay > max {
						delay = max
					}
					continue
				}
				log.Warn("Server{%s}.Accept() = err {%#v}", s.addr, err)
				continue
			}
			delay = 0
			// client.RunEventLoop()
			client.(*session).run()
		}
	}()
}

type wsHandler struct {
	http.ServeMux
	server     *Server
	newSession NewSessionCallback
	upgrader   websocket.Upgrader
}

func newWSHandler(server *Server, newSession NewSessionCallback) *wsHandler {
	return &wsHandler{
		server:     server,
		newSession: newSession,
		upgrader: websocket.Upgrader{
			// in default, ReadBufferSize & WriteBufferSize is 4k
			// HandshakeTimeout: server.HTTPTimeout,
			CheckOrigin:       func(_ *http.Request) bool { return true }, // allow connections from any origin
			EnableCompression: true,
		},
	}
}

func (s *wsHandler) serveWSRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		// w.WriteHeader(http.StatusMethodNotAllowed)
		http.Error(w, "Method not allowed", 405)
		return
	}

	if s.server.IsClosed() {
		http.Error(w, "HTTP server is closed(code:500-11).", 500)
		log.Warn("Server{%s} stop acceptting client connect request.", s.server.addr)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Warn("upgrader.Upgrader(http.Request{%#v}) = error{%#v}", r, err)
		return
	}
	if conn.RemoteAddr().String() == conn.LocalAddr().String() {
		log.Warn("conn.localAddr{%s} == conn.RemoteAddr", conn.LocalAddr().String(), conn.RemoteAddr().String())
		return
	}
	// conn.SetReadLimit(int64(handler.maxMsgLen))
	ss := NewWSSession(conn)
	err = s.newSession(ss)
	if err != nil {
		conn.Close()
		log.Warn("Server{%s}.newSession(ss{%#v}) = err {%#v}", s.server.addr, ss, err)
		return
	}
	if ss.(*session).maxMsgLen > 0 {
		conn.SetReadLimit(int64(ss.(*session).maxMsgLen))
	}
	// ss.RunEventLoop()
	ss.(*session).run()
}

// RunWSEventLoop serve websocket client request
// @newSession: new websocket connection callback
// @path: websocket request url path
func (s *Server) RunWSEventLoop(newSession NewSessionCallback, path string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		var (
			err     error
			handler *wsHandler
			server  *http.Server
		)
		handler = newWSHandler(s, newSession)
		handler.HandleFunc(path, handler.serveWSRequest)
		server = &http.Server{
			Addr:    s.addr,
			Handler: handler,
			// ReadTimeout:    server.HTTPTimeout,
			// WriteTimeout:   server.HTTPTimeout,
		}
		s.lock.Lock()
		s.server = server
		s.lock.Unlock()
		err = server.Serve(s.listener)
		if err != nil {
			log.Error("http.Server.Serve(addr{%s}) = err{%#v}", s.addr, err)
			// panic(err)
		}
	}()
}

// serve websocket client request
// RunWSSEventLoop serve websocket client request
// @newSession: new websocket connection callback
// @path: websocket request url path
// @cert: server certificate file
// @privateKey: server private key(contains its public key)
// @caCert: root certificate file. to verify the legitimacy of client. it can be nil.
func (s *Server) RunWSSEventLoop(
	newSession NewSessionCallback,
	path string,
	cert string,
	privateKey string,
	caCert string) {

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		var (
			err      error
			certPem  []byte
			certPool *x509.CertPool
			config   *tls.Config
			handler  *wsHandler
			server   *http.Server
		)

		config = &tls.Config{InsecureSkipVerify: true}
		config.Certificates = make([]tls.Certificate, 1)
		gxlog.CInfo("server cert:%s, key:%s, caCert:%s", cert, privateKey, caCert)
		if config.Certificates[0], err = tls.LoadX509KeyPair(cert, privateKey); err != nil {
			log.Error("tls.LoadX509KeyPair(cert{%s}, privateKey{%s}) = err{%#v}", cert, privateKey, err)
			return
		}

		if caCert != "" {
			certPem, err = ioutil.ReadFile(caCert)
			if err != nil {
				panic(fmt.Errorf("ioutil.ReadFile(certFile{%s}) = err{%#v}", caCert, err))
			}
			certPool = x509.NewCertPool()
			if ok := certPool.AppendCertsFromPEM(certPem); !ok {
				panic("failed to parse root certificate file")
			}
			config.ClientCAs = certPool
			config.ClientAuth = tls.RequireAndVerifyClientCert
			config.InsecureSkipVerify = false
		}

		handler = newWSHandler(s, newSession)
		handler.HandleFunc(path, handler.serveWSRequest)
		server = &http.Server{
			Addr:    s.addr,
			Handler: handler,
			// ReadTimeout:    server.HTTPTimeout,
			// WriteTimeout:   server.HTTPTimeout,
		}
		server.SetKeepAlivesEnabled(true)
		s.lock.Lock()
		s.server = server
		s.lock.Unlock()
		err = server.Serve(tls.NewListener(s.listener, config))
		if err != nil {
			log.Error("http.Server.Serve(addr{%s}) = err{%#v}", s.addr, err)
			panic(err)
		}
	}()
}

func (s *Server) Listener() net.Listener {
	return s.listener
}

func (s *Server) accept(newSession NewSessionCallback) (Session, error) {
	conn, err := s.listener.Accept()
	if err != nil {
		return nil, err
	}
	if conn.RemoteAddr().String() == conn.LocalAddr().String() {
		log.Warn("conn.localAddr{%s} == conn.RemoteAddr", conn.LocalAddr().String(), conn.RemoteAddr().String())
		return nil, errSelfConnect
	}

	ss := NewTCPSession(conn)
	err = newSession(ss)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return ss, nil
}

func (s *Server) Close() {
	s.stop()
	s.wg.Wait()
}
