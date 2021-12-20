/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"errors"
	"sync"
	"time"
)

import (
	getty "github.com/apache/dubbo-getty"
)

const (
	WritePkgTimeout = 1e8
)

var (
	errTooManySessions = errors.New("Too many echo sessions!")
	hbHandler          = &HeartbeatHandler{}
	msgHandler         = &MessageHandler{}
	echoMsgHandler     = newEchoMessageHandler()
)

type PackageHandler interface {
	Handle(getty.Session, *EchoPackage) error
}

////////////////////////////////////////////
// heartbeat handler
////////////////////////////////////////////

type HeartbeatHandler struct{}

func (h *HeartbeatHandler) Handle(session getty.Session, pkg *EchoPackage) error {
	log.Debugf("get echo heartbeat package{%s}", pkg)

	var rspPkg EchoPackage
	rspPkg.H = pkg.H
	rspPkg.B = echoHeartbeatResponseString
	rspPkg.H.Len = uint16(len(rspPkg.B) + 1)
	_, _, err := session.WritePkg(&rspPkg, WritePkgTimeout)
	return err
}

////////////////////////////////////////////
// message handler
////////////////////////////////////////////

type MessageHandler struct{}

func (h *MessageHandler) Handle(session getty.Session, pkg *EchoPackage) error {
	log.Debugf("get echo package{%s}", pkg)
	// write echo message handle logic here.
	_, _, err := session.WritePkg(pkg, WritePkgTimeout)
	if err != nil {
		log.Warnf("session.WritePkg(session{%s}, pkg{%s}) = error{%v}", session.Stat(), pkg, err)
		session.Close()
	}
	return err
}

////////////////////////////////////////////
// EchoMessageHandler
////////////////////////////////////////////

type clientEchoSession struct {
	session getty.Session
	reqNum  int32
}

type EchoMessageHandler struct {
	handlers map[uint32]PackageHandler

	rwlock     sync.RWMutex
	sessionMap map[getty.Session]*clientEchoSession
}

func newEchoMessageHandler() *EchoMessageHandler {
	handlers := make(map[uint32]PackageHandler)
	handlers[heartbeatCmd] = hbHandler
	handlers[echoCmd] = msgHandler

	return &EchoMessageHandler{sessionMap: make(map[getty.Session]*clientEchoSession), handlers: handlers}
}

func (h *EchoMessageHandler) OnOpen(session getty.Session) error {
	var err error

	h.rwlock.RLock()
	if conf.SessionNumber <= len(h.sessionMap) {
		err = errTooManySessions
	}
	h.rwlock.RUnlock()
	if err != nil {
		return err
	}

	log.Infof("got session:%s", session.Stat())
	h.rwlock.Lock()
	h.sessionMap[session] = &clientEchoSession{session: session}
	h.rwlock.Unlock()
	return nil
}

func (h *EchoMessageHandler) OnError(session getty.Session, err error) {
	log.Infof("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	h.rwlock.Lock()
	delete(h.sessionMap, session)
	h.rwlock.Unlock()
}

func (h *EchoMessageHandler) OnClose(session getty.Session) {
	log.Infof("session{%s} is closing......", session.Stat())
	h.rwlock.Lock()
	delete(h.sessionMap, session)
	h.rwlock.Unlock()
}

func (h *EchoMessageHandler) OnMessage(session getty.Session, pkg interface{}) {
	p, ok := pkg.(*EchoPackage)
	if !ok {
		log.Errorf("illegal packge{%#v}", pkg)
		return
	}

	handler, ok := h.handlers[p.H.Command]
	if !ok {
		log.Errorf("illegal command{%d}", p.H.Command)
		return
	}
	err := handler.Handle(session, p)
	if err != nil {
		h.rwlock.Lock()
		if _, ok := h.sessionMap[session]; ok {
			h.sessionMap[session].reqNum++
		}
		h.rwlock.Unlock()
	}
}

func (h *EchoMessageHandler) OnCron(session getty.Session) {
	var (
		flag   bool
		active time.Time
	)
	h.rwlock.RLock()
	if _, ok := h.sessionMap[session]; ok {
		active = session.GetActive()
		if conf.sessionTimeout.Nanoseconds() < time.Since(active).Nanoseconds() {
			flag = true
			log.Warnf("session{%s} timeout{%s}, reqNum{%d}",
				session.Stat(), time.Since(active).String(), h.sessionMap[session].reqNum)
		}
	}
	h.rwlock.RUnlock()
	if flag {
		h.rwlock.Lock()
		delete(h.sessionMap, session)
		h.rwlock.Unlock()
		session.Close()
	}
}
