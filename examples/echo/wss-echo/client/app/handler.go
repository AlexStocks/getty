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
	"time"
)

import (
	getty "github.com/apache/dubbo-getty"
)

var (
	errSessionNotExist = errors.New("session not exist!")
	echoMsgHandler     = newEchoMessageHandler()
)

////////////////////////////////////////////
// EchoMessageHandler
////////////////////////////////////////////

type clientEchoSession struct {
	session getty.Session
	reqNum  int32
}

type EchoMessageHandler struct{}

func newEchoMessageHandler() *EchoMessageHandler {
	return &EchoMessageHandler{}
}

func (h *EchoMessageHandler) OnOpen(session getty.Session) error {
	client.addSession(session)

	return nil
}

func (h *EchoMessageHandler) OnError(session getty.Session, err error) {
	log.Infof("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	client.removeSession(session)
}

func (h *EchoMessageHandler) OnClose(session getty.Session) {
	log.Infof("session{%s} is closing......", session.Stat())
	client.removeSession(session)
}

func (h *EchoMessageHandler) OnMessage(session getty.Session, pkg interface{}) {
	p, ok := pkg.(*EchoPackage)
	if !ok {
		log.Errorf("illegal packge{%#v}", pkg)
		return
	}

	log.Debugf("get echo package{%s}", p)
	client.updateSession(session)
}

func (h *EchoMessageHandler) OnCron(session getty.Session) {
	clientEchoSession, err := client.getClientEchoSession(session)
	if err != nil {
		log.Errorf("client.getClientSession(session{%s}) = error{%#v}", session.Stat(), err)
		return
	}
	if conf.sessionTimeout.Nanoseconds() < time.Since(session.GetActive()).Nanoseconds() {
		log.Warnf("session{%s} timeout{%s}, reqNum{%d}",
			session.Stat(), time.Since(session.GetActive()).String(), clientEchoSession.reqNum)
		client.removeSession(session)
		return
	}
}
