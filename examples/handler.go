package main

import (
	"errors"
	"sync"
	"time"
)

import (
	"github.com/AlexStocks/getty"
	log "github.com/AlexStocks/log4go"
)

const (
	WritePkgTimeout = 1e8
)

var (
	errTooManySessions = errors.New("Too many echo sessions!")
)

type PackageHandler interface {
	Handle(getty.Session, *EchoPackage) error
}

////////////////////////////////////////////
// heartbeat handler
////////////////////////////////////////////

type HeartbeatHandler struct{}

func (this *HeartbeatHandler) Handle(session getty.Session, pkg *EchoPackage) error {
	log.Debug("get echo heartbeat package{%s}", pkg)

	var rspPkg EchoPackage
	rspPkg.H = pkg.H
	rspPkg.B = echoHeartbeatResponseString
	rspPkg.H.Len = uint16(len(rspPkg.B) + 1)

	return session.WritePkg(&rspPkg, WritePkgTimeout)
}

////////////////////////////////////////////
// message handler
////////////////////////////////////////////

type MessageHandler struct{}

func (this *MessageHandler) Handle(session getty.Session, pkg *EchoPackage) error {
	log.Debug("get echo package{%s}", pkg)
	// write echo message handle logic here.
	return session.WritePkg(pkg, WritePkgTimeout)
}

////////////////////////////////////////////
// EchoMessageHandler
////////////////////////////////////////////

type clientEchoSession struct {
	session getty.Session
	active  time.Time
	reqNum  int32
}

type EchoMessageHandler struct {
	handlers map[uint32]PackageHandler

	rwlock     sync.RWMutex
	sessionMap map[getty.Session]*clientEchoSession
}

func newEchoMessageHandler() *EchoMessageHandler {
	handlers := make(map[uint32]PackageHandler)
	handlers[heartbeatCmd] = &HeartbeatHandler{}
	handlers[echoCmd] = &MessageHandler{}

	return &EchoMessageHandler{sessionMap: make(map[getty.Session]*clientEchoSession), handlers: handlers}
}

func (this *EchoMessageHandler) OnOpen(session getty.Session) error {
	var (
		err error
	)

	this.rwlock.RLock()
	if 100 < len(this.sessionMap) {
		err = errTooManySessions
	}
	this.rwlock.RUnlock()
	if err != nil {
		return err
	}

	log.Info("got session:%s", session.Stat())
	this.rwlock.Lock()
	this.sessionMap[session] = &clientEchoSession{session: session}
	this.rwlock.Unlock()
	return nil
}

func (this *EchoMessageHandler) OnError(session getty.Session, err error) {
	log.Info("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	this.rwlock.Lock()
	delete(this.sessionMap, session)
	this.rwlock.Unlock()
}

func (this *EchoMessageHandler) OnClose(session getty.Session) {
	log.Info("session{%s} is closing......", session.Stat())
	this.rwlock.Lock()
	delete(this.sessionMap, session)
	this.rwlock.Unlock()
}

func (this *EchoMessageHandler) OnMessage(session getty.Session, pkg interface{}) {
	p, ok := pkg.(*EchoPackage)
	if !ok {
		log.Error("illegal packge{%#v}", pkg)
		return
	}

	handler, ok := this.handlers[p.H.Command]
	if !ok {
		log.Error("illegal command{%d}", p.H.Command)
		return
	}
	err := handler.Handle(session, p)
	if err != nil {
		this.rwlock.Lock()
		if _, ok := this.sessionMap[session]; ok {
			this.sessionMap[session].active = time.Now()
			this.sessionMap[session].reqNum++
		}
		this.rwlock.Unlock()
	}
}

func (this *EchoMessageHandler) OnCron(session getty.Session) {
	var (
		flag   bool
		active time.Time
	)
	this.rwlock.RLock()
	if _, ok := this.sessionMap[session]; ok {
		active = session.GetActive()
		if 5*(int64(time.Nanosecond)) < time.Since(active).Nanoseconds() {
			flag = true
			log.Warn("session{%s} timeout{%s}, reqNum{%d}",
				session.Stat(), time.Since(active).String(), this.sessionMap[session].reqNum)
		}
	}
	this.rwlock.RUnlock()
	if flag {
		this.rwlock.Lock()
		delete(this.sessionMap, session)
		this.rwlock.Unlock()
		session.Close()
	}
}
