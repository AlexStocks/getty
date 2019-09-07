/******************************************************
# MAINTAINER : wongoo
# LICENCE    : Apache License 2.0
# EMAIL      : gelnyang@163.com
# MOD        : 2019-06-11
******************************************************/

package tcp

import (
	"fmt"
	"net"
	"time"
)

import (
	"github.com/dubbogo/getty"
	"github.com/dubbogo/gost/sync"
)

import (
	"github.com/dubbogo/getty/demo/hello"
)

var (
	pkgHandler    = &hello.PackageHandler{}
	eventListener = &hello.MessageHandler{}
)

func NewHelloClientSession(session getty.Session, taskPool *gxsync.TaskPool) (err error) {
	eventListener.SessionOnOpen = func(session getty.Session) {
		hello.Sessions = append(hello.Sessions, session)
	}
	err = InitialSession(session)
	if err != nil {
		return
	}
	session.SetTaskPool(taskPool)
	return
}

func InitialSession(session getty.Session) (err error) {
	session.SetCompressType(getty.CompressZip)

	tcpConn, ok := session.Conn().(*net.TCPConn)
	if !ok {
		panic(fmt.Sprintf("newSession: %s, session.conn{%#v} is not tcp connection", session.Stat(), session.Conn()))
	}

	if err = tcpConn.SetNoDelay(true); err != nil {
		return err
	}
	if err = tcpConn.SetKeepAlive(true); err != nil {
		return err
	}
	if err = tcpConn.SetKeepAlivePeriod(10 * time.Second); err != nil {
		return err
	}
	if err = tcpConn.SetReadBuffer(262144); err != nil {
		return err
	}
	if err = tcpConn.SetWriteBuffer(524288); err != nil {
		return err
	}

	session.SetName("hello")
	session.SetMaxMsgLen(128)
	session.SetRQLen(1024)
	session.SetWQLen(512)
	session.SetReadTimeout(time.Second)
	session.SetWriteTimeout(5 * time.Second)
	session.SetCronPeriod(int(hello.CronPeriod / 1e6))
	session.SetWaitTime(time.Second)

	session.SetPkgHandler(pkgHandler)
	session.SetEventListener(eventListener)
	return nil
}
