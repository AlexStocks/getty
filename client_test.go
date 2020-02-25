package getty

import (
	"bytes"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

import (
	"github.com/stretchr/testify/assert"
)

type PackageHandler struct{}

func (h *PackageHandler) Read(ss Session, data []byte) (interface{}, int, error) {
	return nil, 0, nil
}

func (h *PackageHandler) Write(ss Session, pkg interface{}) ([]byte, error) {
	return nil, nil
}

type MessageHandler struct {
	lock  sync.Mutex
	array []Session
}

func newMessageHandler() *MessageHandler {
	return &MessageHandler{}
}

func (h *MessageHandler) SessionNumber() int {
	h.lock.Lock()
	connNum := len(h.array)
	h.lock.Unlock()

	return connNum
}

func (h *MessageHandler) OnOpen(session Session) error {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.array = append(h.array, session)

	return nil
}
func (h *MessageHandler) OnError(session Session, err error)         {}
func (h *MessageHandler) OnClose(session Session)                    {}
func (h *MessageHandler) OnMessage(session Session, pkg interface{}) {}
func (h *MessageHandler) OnCron(session Session)                     {}

type Package struct{}

func (p Package) String() string {
	return ""
}
func (p Package) Marshal() (*bytes.Buffer, error)           { return nil, nil }
func (p *Package) Unmarshal(buf *bytes.Buffer) (int, error) { return 0, nil }

var (
	pkg        Package
	pkgHandler PackageHandler
)

func newSessionCallback(session Session, handler *MessageHandler) error {
	session.SetName("hello-client-session")
	session.SetMaxMsgLen(1024)
	session.SetPkgHandler(&pkgHandler)
	session.SetEventListener(handler)
	session.SetRQLen(4)
	session.SetWQLen(32)
	session.SetReadTimeout(3e9)
	session.SetWriteTimeout(3e9)
	session.SetCronPeriod((int)(30e9 / 1e6))
	session.SetWaitTime(3e9)
	session.SetTaskPool(nil)

	return nil
}

func TestTCPClient(t *testing.T) {
	listenLocalServer := func() (net.Listener, error) {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			return nil, err
		}

		go http.Serve(listener, nil)
		return listener, nil
	}

	listener, err := listenLocalServer()
	assert.Nil(t, err)
	assert.NotNil(t, listener)

	addr := listener.Addr().(*net.TCPAddr)
	t.Logf("server addr: %v", addr)
	clt := newClient(TCP_CLIENT,
		WithServerAddress(addr.String()),
		WithReconnectInterval(5e8),
		WithConnectionNumber(1),
	)
	assert.NotNil(t, clt)
	assert.True(t, clt.ID() > 0)
	assert.Equal(t, clt.endPointType, TCP_CLIENT)

	var (
		msgHandler MessageHandler
	)
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	clt.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	clt.Close()
	assert.True(t, clt.IsClosed())
}

func TestUDPClient(t *testing.T) {
	var (
		err  error
		conn *net.UDPConn
	)
	func() {
		srcAddr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
		conn, err = net.ListenUDP("udp", srcAddr)
		assert.Nil(t, err)
		assert.NotNil(t, conn)
	}()
	defer conn.Close()

	addr := conn.LocalAddr()
	t.Logf("server addr: %v", addr)
	clt := NewUDPClient(
		WithServerAddress(addr.String()),
		WithReconnectInterval(5e8),
		WithConnectionNumber(1),
	)
	assert.NotNil(t, clt)
	assert.True(t, clt.ID() > 0)
	//assert.Equal(t, clt.endPointType, UDP_CLIENT)

	var (
		msgHandler MessageHandler
	)
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	clt.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	clt.Close()
	assert.True(t, clt.IsClosed())
}

func TestNewWSClient(t *testing.T) {
	var (
		server           Server
		serverMsgHandler MessageHandler
	)
	addr := "127.0.0.1:65000"
	path := "/hello"
	func() {
		server = NewWSServer(
			WithLocalAddress(addr),
			WithWebsocketServerPath(path),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		go server.RunEventLoop(newServerSession)
	}()
	time.Sleep(1e9)

	client := NewWSClient(
		WithServerAddress("ws://"+addr+path),
		WithConnectionNumber(1),
	)

	var (
		msgHandler MessageHandler
	)
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	client.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	client.Close()
	assert.True(t, client.IsClosed())
	server.Close()
	assert.True(t, server.IsClosed())
}

const (
	WssProfilePath = "./examples/profiles/wss/"
	WssServerCRT   = "server_cert/server.crt"
	WssServerKEY   = "server_cert/server.key"
	WssClientCRT   = "client_cert/client.crt"
)

func TestNewWSSClient(t *testing.T) {
	var (
		server           Server
		serverMsgHandler MessageHandler
	)
	addr := "127.0.0.1:63450"
	path := "/hello"
	func() {
		server = NewWSSServer(
			WithLocalAddress(addr),
			WithWebsocketServerPath(path),
			WithWebsocketServerCert(WssProfilePath+WssServerCRT),
			WithWebsocketServerPrivateKey(WssProfilePath+WssServerKEY),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		go server.RunEventLoop(newServerSession)
	}()
	time.Sleep(1e9)

	client := NewWSSClient(
		WithServerAddress("wss://"+addr+path),
		WithConnectionNumber(1),
		WithRootCertificateFile(WssProfilePath+WssClientCRT),
	)

	var (
		msgHandler MessageHandler
	)
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	client.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	client.Close()
	assert.True(t, client.IsClosed())
	assert.False(t, server.IsClosed())
	//server.Close()
	//assert.True(t, server.IsClosed())
}
