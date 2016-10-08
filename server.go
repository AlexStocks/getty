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
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

import (
	"github.com/AlexStocks/goext/net"
	log "github.com/AlexStocks/log4go"
	"github.com/gorilla/websocket"
)

var (
	errSelfConnect = errors.New("connect self!")
)

type Server struct {
	// net
	addr     string
	listener net.Listener

	sync.Once
	done chan empty
	wg   sync.WaitGroup
}

func NewServer() *Server {
	return &Server{done: make(chan empty)}
}

func (this *Server) stop() {
	select {
	case <-this.done:
		return
	default:
		this.Once.Do(func() {
			close(this.done)
			// 把listener.Close放在这里，既能防止多次关闭调用，
			// 又能及时让Server因accept返回错误而从RunEventloop退出
			this.listener.Close()
		})
	}
}

func (this *Server) IsClosed() bool {
	select {
	case <-this.done:
		return true
	default:
		return false
	}
}

// (Server)Bind's functionality is equal to (Server)Listen.
func (this *Server) Bind(network string, host string, port int) error {
	if port <= 0 {
		return errors.New("port<=0 illegal")
	}

	return this.Listen(network, gxnet.HostAddress(host, port))
}

// net.ipv4.tcp_max_syn_backlog
// net.ipv4.tcp_timestamps
// net.ipv4.tcp_tw_recycle
func (this *Server) Listen(network string, addr string) error {
	listener, err := net.Listen(network, addr)
	if err != nil {
		return err
	}
	this.addr = addr
	this.listener = listener

	return nil
}

func (this *Server) RunEventloop(newSession NewSessionCallback) {
	this.wg.Add(1)
	go func() {
		defer this.wg.Done()
		var (
			err    error
			client *Session
			delay  time.Duration
		)
		for {
			if this.IsClosed() {
				log.Warn("Server{%s} stop acceptting client connect request.", this.addr)
				return
			}
			if delay != 0 {
				time.Sleep(delay)
			}
			client, err = this.accept(newSession)
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
				log.Warn("Server{%s}.Accept() = err {%#v}", this.addr, err)
				continue
			}
			delay = 0
			client.RunEventLoop()
		}
	}()
}

type WSHandler struct {
	server     *Server
	newSession NewSessionCallback
	upgrader   websocket.Upgrader
}

func NewWSHandler(server *Server) *WSHandler {
	return &WSHandler{
		server: server,
		upgrader: websocket.Upgrader{
			// HandshakeTimeout: server.HTTPTimeout,
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
	}
}

func (this *WSHandler) ServeHTTPServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	if this.server.IsClosed() {
		http.Error(w, "HTTP server is closed(code:500-11).", 500)
		log.Warn("Server{%s} stop acceptting client connect request.", this.server.addr)
		return
	}

	conn, err := this.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Warn("upgrader.Upgrader(http.Request{%#v}) = error{%#v}", r, err)
		return
	}
	if conn.RemoteAddr().String() == conn.LocalAddr().String() {
		return nil, errSelfConnect
		log.Warn("conn.localAddr{%s} == conn.RemoteAddr", conn.LocalAddr().String(), conn.RemoteAddr().String())
		return
	}
	// conn.SetReadLimit(int64(handler.maxMsgLen))
	session := NewWSSession(conn)
	err = this.newSession(session)
	if err != nil {
		conn.Close()
		log.Warn("Server{%s}.newSession(session{%#v}) = err {%#v}", this.server.addr, session, err)
		return nil
	}
	session.RunEventLoop()
}

func (this *Server) RunWSEventLoop(newSession NewSessionCallback) {
	this.wg.Add(1)
	go func() {
		defer this.wg.Done()
		http.Server{
			Addr:    this.addr,
			Handler: NewWSHandler(this),
			// ReadTimeout:    server.HTTPTimeout,
			// WriteTimeout:   server.HTTPTimeout,
		}.Serve(this.listener)
	}()
}

func (this *Server) Listener() net.Listener {
	return this.listener
}

func (this *Server) accept(newSession NewSessionCallback) (*Session, error) {
	conn, err := this.listener.Accept()
	if err != nil {
		return nil, err
	}
	if conn.RemoteAddr().String() == conn.LocalAddr().String() {
		log.Warn("conn.localAddr{%s} == conn.RemoteAddr", conn.LocalAddr().String(), conn.RemoteAddr().String())
		return nil, errSelfConnect
	}

	session := NewTCPSession(conn)
	err = newSession(session)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return session, nil
}

func (this *Server) Close() {
	this.stop()
	this.wg.Wait()
}
