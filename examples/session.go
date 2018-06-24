package main

import (
	"github.com/AlexStocks/getty"
	"net"
	"fmt"
	"time"
	log "github.com/AlexStocks/log4go"
)

type EchoSessionHandler struct {
}

func (this *EchoSessionHandler) Initialize(session getty.Session) error {
	var (
		ok      bool
		tcpConn *net.TCPConn
	)

	session.SetCompressType(getty.CompressZip)

	if tcpConn, ok = session.Conn().(*net.TCPConn); !ok {
		panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp connection\n", session.Stat(), session.Conn()))
	}

	tcpConn.SetNoDelay(true)
	tcpConn.SetKeepAlive(true)
	tcpConn.SetKeepAlivePeriod(4)

	tcpConn.SetReadBuffer(1024)
	tcpConn.SetWriteBuffer(1024)

	session.SetName("session-examples")
	session.SetMaxMsgLen(10000000)
	session.SetRQLen(2)
	session.SetWQLen(2)
	session.SetReadTimeout(1 * time.Second)
	session.SetWriteTimeout(1 * time.Second)
	session.SetCronPeriod((int)(60 * time.Second / 1e6))
	session.SetWaitTime(1 * time.Second)
	log.Debug("app accepts new session:%s\n", session.Stat())
	return nil
}
