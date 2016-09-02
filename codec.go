/******************************************************
# DESC       : codec interface
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2016-08-17 11:20
# FILE       : codec.go
******************************************************/

package getty

import (
	"bytes"
)

// Reader is used to unmarshal a complete pkg from buffer
type Reader interface {
	// Parse pkg from buffer and if possible return a complete pkg
	Read(*Session, *bytes.Buffer) (interface{}, error)
}

// Writer is used to marshal pkg and write to session
type Writer interface {
	Write(*Session, interface{}) error
}

// packet handler interface
type ReadWriter interface {
	Reader
	Writer
}

// EventListener is used to process pkg that recved from remote session
type EventListener interface {
	// invoked when session opened
	OnOpen(*Session)

	// invoked when session closed
	OnClose(*Session)

	// invoked when got error
	OnError(*Session, error)

	// invoked periodically, its period can be set by (Session)SetCronPeriod
	OnCron(*Session)

	// invoked when receive packge. Pls attention that do not handle long time logic processing in this func.
	// Y'd better set the package's maximum length. If the message's length is greater than it, u should
	// should return err and getty will close this connection soon.
	OnMessage(*Session, interface{})
}
