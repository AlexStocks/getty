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
	"github.com/AlexStocks/getty/transport"
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

func (h *EchoMessageHandler) OnOpen(session getty.Session) error {
	h.client.addSession(session)

	return nil
}

func (h *EchoMessageHandler) OnError(session getty.Session, err error) {
	log.Info("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	h.client.removeSession(session)
}

func (h *EchoMessageHandler) OnClose(session getty.Session) {
	log.Info("session{%s} is closing......", session.Stat())
	h.client.removeSession(session)
}

func (h *EchoMessageHandler) OnMessage(session getty.Session, udpCtx interface{}) {
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

	h.client.updateSession(session)
}

func (h *EchoMessageHandler) OnCron(session getty.Session) {
	clientEchoSession, err := h.client.getClientEchoSession(session)
	if err != nil {
		log.Error("client.getClientSession(session{%s}) = error{%#v}", session.Stat(), err)
		return
	}
	if conf.sessionTimeout.Nanoseconds() < time.Since(session.GetActive()).Nanoseconds() {
		log.Warn("session{%s} timeout{%s}, reqNum{%d}",
			session.Stat(), time.Since(session.GetActive()).String(), clientEchoSession.reqNum)
		// UDP_ENDPOINT session should be long live.
		if h.client != &unconnectedClient {
			h.client.removeSession(session)
		}
		return
	}

	h.client.heartbeat(session)
}
