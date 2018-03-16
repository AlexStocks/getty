/******************************************************
# DESC       : codec interface
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2016-08-17 11:20
# FILE       : codec.go
******************************************************/

package getty

// NewSessionCallback will be invoked when server accepts a new client connection or client connects to server successfully.
// If there are too many client connections or u do not want to connect a server again, u can return non-nil error. And
// then getty will close the new session.
type NewSessionCallback func(Session) error

// Reader is used to unmarshal a complete pkg from buffer
type Reader interface {
	// Parse tcp/udp/websocket pkg from buffer and if possible return a complete pkg
	// If length of buf is not long enough, u should return {nil,0, nil}
	// The second return value is the length of the pkg.
	Read(Session, []byte) (interface{}, int, error)
}

// Writer is used to marshal pkg and write to session
type Writer interface {
	Write(Session, interface{}) error
}

// tcp package handler interface
type ReadWriter interface {
	Reader
	Writer
}

// EventListener is used to process pkg that received from remote session
type EventListener interface {
	// invoked when session opened
	// If the return error is not nil, @Session will be closed.
	OnOpen(Session) error

	// invoked when session closed.
	OnClose(Session)

	// invoked when got error.
	OnError(Session, error)

	// invoked periodically, its period can be set by (Session)SetCronPeriod
	OnCron(Session)

	// invoked when receive packge. Pls attention that do not handle long time logic processing in this func.
	// You'd better set the package's maximum length. If the message's length is greater than it, u should
	// should return err in Reader{Read} and getty will close this connection soon.
	//
	// If this is a udp event listener, the second parameter type is UDPContext.
	OnMessage(Session, interface{})
}
