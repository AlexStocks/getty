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
	session getty.Session
	reqNum  int32
}

type EchoMessageHandler struct {
	client *EchoClient
}

func newEchoMessageHandler(client *EchoClient) *EchoMessageHandler {
	return &EchoMessageHandler{client: client}
}

func (this *EchoMessageHandler) OnOpen(session getty.Session) error {
	this.client.addSession(session)

	return nil
}

func (this *EchoMessageHandler) OnError(session getty.Session, err error) {
	log.Info("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	this.client.removeSession(session)
}

func (this *EchoMessageHandler) OnClose(session getty.Session) {
	log.Info("session{%s} is closing......", session.Stat())
	this.client.removeSession(session)
}

func (this *EchoMessageHandler) OnMessage(session getty.Session, udpCtx interface{}) {
	ctx, ok := udpCtx.(getty.UDPContext)
	if !ok {
		log.Error("illegal UDPContext{%#v}", udpCtx)
		return
	}
	p, ok := ctx.Pkg.(*EchoPackage)
	if !ok {
		log.Error("illegal packge{%#v}", ctx.Pkg)
		return
	}

	log.Debug("get echo package{%s}", p)

	this.client.updateSession(session)
}

func (this *EchoMessageHandler) OnCron(session getty.Session) {
	clientEchoSession, err := this.client.getClientEchoSession(session)
	if err != nil {
		log.Error("client.getClientSession(session{%s}) = error{%#v}", session.Stat(), err)
		return
	}
	if conf.sessionTimeout.Nanoseconds() < time.Since(session.GetActive()).Nanoseconds() {
		log.Warn("session{%s} timeout{%s}, reqNum{%d}",
			session.Stat(), time.Since(session.GetActive()).String(), clientEchoSession.reqNum)
		this.client.removeSession(session)
		return
	}

	this.client.heartbeat(session)
}
