package getty

import (
	"testing"
	"time"
)

import (
	"github.com/stretchr/testify/assert"
)

func TestTCPServer(t *testing.T) {
	var (
		server           *server
		serverMsgHandler MessageHandler
	)
	addr := "127.0.0.1:0"
	func() {
		server = newServer(
			TCP_SERVER,
			WithLocalAddress(addr),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		server.RunEventLoop(newServerSession)
		assert.True(t, server.ID() > 0)
		assert.True(t, server.EndPointType() == TCP_SERVER)
	}()
	time.Sleep(500e6)

	addr = server.streamListener.Addr().String()
	t.Logf("server addr: %v", addr)
	clt := newClient(TCP_CLIENT,
		WithServerAddress(addr),
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

	server.Close()
	assert.True(t, server.IsClosed())
}

func TestUDPServer(t *testing.T) {
	var (
		server           *server
		serverMsgHandler MessageHandler
	)
	addr := "127.0.0.1:0"
	func() {
		server = newServer(
			UDP_ENDPOINT,
			WithLocalAddress(addr),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		server.RunEventLoop(newServerSession)
		assert.True(t, server.ID() > 0)
		assert.True(t, server.EndPointType() == UDP_ENDPOINT)
	}()
	time.Sleep(500e6)

	//addr = server.streamListener.Addr().String()
	//t.Logf("server addr: %v", addr)
	//clt := newClient(TCP_CLIENT,
	//	WithServerAddress(addr),
	//	WithReconnectInterval(5e8),
	//	WithConnectionNumber(1),
	//)
	//assert.NotNil(t, clt)
	//assert.True(t, clt.ID() > 0)
	//assert.Equal(t, clt.endPointType, TCP_CLIENT)
	//
	//var (
	//	msgHandler MessageHandler
	//)
	//cb := func(session Session) error {
	//	return newSessionCallback(session, &msgHandler)
	//}
	//
	//clt.RunEventLoop(cb)
	//time.Sleep(1e9)
	//
	//assert.Equal(t, 1, msgHandler.SessionNumber())
	//clt.Close()
	//assert.True(t, clt.IsClosed())
	//
	//server.Close()
	//assert.True(t, server.IsClosed())
}
