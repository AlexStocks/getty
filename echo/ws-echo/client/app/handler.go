/******************************************************
# DESC    : echo package handler
# AUTHOR  : Alex Stocks
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2016-09-04 13:08
# FILE    : handler.go
******************************************************/

package main

import (
	"errors"
	"time"
)

import (
	"github.com/AlexStocks/getty"
	log "github.com/AlexStocks/log4go"
)

var (
	errSessionNotExist = errors.New("session not exist!")
)

////////////////////////////////////////////
// EchoMessageHandler
////////////////////////////////////////////

type clientEchoSession struct {
	session *getty.Session
	reqNum  int32
}

type EchoMessageHandler struct {
}

func newEchoMessageHandler() *EchoMessageHandler {
	return &EchoMessageHandler{}
}

func (this *EchoMessageHandler) OnOpen(session *getty.Session) error {
	client.addSession(session)

	return nil
}

func (this *EchoMessageHandler) OnError(session *getty.Session, err error) {
	log.Info("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	client.removeSession(session)
}

func (this *EchoMessageHandler) OnClose(session *getty.Session) {
	log.Info("session{%s} is closing......", session.Stat())
	client.removeSession(session)
}

func (this *EchoMessageHandler) OnMessage(session *getty.Session, pkg interface{}) {
	p, ok := pkg.(*EchoPackage)
	if !ok {
		log.Error("illegal packge{%#v}", pkg)
		return
	}

	log.Debug("get echo package{%s}", p)
	client.updateSession(session)
}

func (this *EchoMessageHandler) OnCron(session *getty.Session) {
	clientEchoSession, err := client.getClientEchoSession(session)
	if err != nil {
		log.Error("client.getClientSession(session{%s}) = error{%#v}", session.Stat(), err)
		return
	}
	if conf.sessionTimeout.Nanoseconds() < time.Since(session.GetActive()).Nanoseconds() {
		log.Warn("session{%s} timeout{%s}, reqNum{%d}",
			session.Stat(), time.Since(session.GetActive()).String(), clientEchoSession.reqNum)
		client.removeSession(session)
		return
	}
}
