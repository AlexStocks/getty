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

package rpc

import (
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

import (
	"github.com/AlexStocks/getty/transport"
)

var (
	errTooManySessions = jerrors.New("too many sessions")
)

type rpcSession struct {
	session getty.Session
	reqNum  int32
}

func (s *rpcSession) AddReqNum(num int32) {
	atomic.AddInt32(&s.reqNum, num)
}

func (s *rpcSession) GetReqNum() int32 {
	return atomic.LoadInt32(&s.reqNum)
}

////////////////////////////////////////////
// RpcServerHandler
////////////////////////////////////////////

type RpcServerHandler struct {
	maxSessionNum  int
	sessionTimeout time.Duration
	sessionMap     map[getty.Session]*rpcSession
	rwlock         sync.RWMutex
}

func NewRpcServerHandler(maxSessionNum int, sessionTimeout time.Duration) *RpcServerHandler {
	return &RpcServerHandler{
		maxSessionNum:  maxSessionNum,
		sessionTimeout: sessionTimeout,
		sessionMap:     make(map[getty.Session]*rpcSession),
	}
}

func (h *RpcServerHandler) OnOpen(session getty.Session) error {
	var err error
	h.rwlock.RLock()
	if h.maxSessionNum <= len(h.sessionMap) {
		err = errTooManySessions
	}
	h.rwlock.RUnlock()
	if err != nil {
		return jerrors.Trace(err)
	}

	log.Debug("got session:%s", session.Stat())
	h.rwlock.Lock()
	h.sessionMap[session] = &rpcSession{session: session}
	h.rwlock.Unlock()
	return nil
}

func (h *RpcServerHandler) OnError(session getty.Session, err error) {
	log.Debug("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	h.rwlock.Lock()
	delete(h.sessionMap, session)
	h.rwlock.Unlock()
}

func (h *RpcServerHandler) OnClose(session getty.Session) {
	log.Debug("session{%s} is closing......", session.Stat())
	h.rwlock.Lock()
	delete(h.sessionMap, session)
	h.rwlock.Unlock()
}

func (h *RpcServerHandler) OnMessage(session getty.Session, pkg interface{}) {
	var rs *rpcSession
	if session != nil {
		func() {
			h.rwlock.RLock()
			defer h.rwlock.RUnlock()

			if _, ok := h.sessionMap[session]; ok {
				rs = h.sessionMap[session]
			}
		}()
		if rs != nil {
			rs.AddReqNum(1)
		}
	}

	req, ok := pkg.(GettyRPCRequestPackage)
	if !ok {
		log.Error("illegal package{%#v}", pkg)
		return
	}
	// heartbeat
	if req.H.Command == gettyCmdHbRequest {
		h.replyCmd(session, req, gettyCmdHbResponse, "")
		return
	}
	if req.header.CallType == CT_OneWay {
		function := req.methodType.method.Func
		function.Call([]reflect.Value{req.service.rcvr, req.argv})
		return
	}
	if req.header.CallType == CT_TwoWayNoReply {
		h.replyCmd(session, req, gettyCmdRPCResponse, "")
		function := req.methodType.method.Func
		function.Call([]reflect.Value{req.service.rcvr, req.argv, req.replyv})
		return
	}
	err := h.callService(session, req, req.service, req.methodType, req.argv, req.replyv)
	if err != nil {
		log.Error("h.callService(session:%#v, req:%#v) = %s", session, req, jerrors.ErrorStack(err))
	}
}

func (h *RpcServerHandler) OnCron(session getty.Session) {
	var (
		flag   bool
		active time.Time
	)

	h.rwlock.RLock()
	if _, ok := h.sessionMap[session]; ok {
		active = session.GetActive()
		if h.sessionTimeout.Nanoseconds() < time.Since(active).Nanoseconds() {
			flag = true
			log.Warn("session{%s} timeout{%s}, reqNum{%d}",
				session.Stat(), time.Since(active).String(), h.sessionMap[session].GetReqNum())
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

func (h *RpcServerHandler) replyCmd(session getty.Session, req GettyRPCRequestPackage, cmd gettyCommand, err string) {
	resp := GettyPackage{
		H: req.H,
	}
	resp.H.Command = cmd
	if len(err) != 0 {
		resp.H.Code = GettyFail
		resp.B = &GettyRPCResponse{
			header: GettyRPCResponseHeader{
				Error: err,
			},
		}
	}

	session.WritePkg(resp, 5*time.Second)
}

func (h *RpcServerHandler) callService(session getty.Session, req GettyRPCRequestPackage,
	service *service, methodType *methodType, argv, replyv reflect.Value) error {

	function := methodType.method.Func
	returnValues := function.Call([]reflect.Value{service.rcvr, argv, replyv})
	errInter := returnValues[0].Interface()
	if errInter != nil {
		h.replyCmd(session, req, gettyCmdRPCResponse, errInter.(error).Error())
		return nil
	}

	resp := GettyPackage{
		H: req.H,
	}
	resp.H.Code = GettyOK
	resp.H.Command = gettyCmdRPCResponse
	resp.B = &GettyRPCResponse{
		body: replyv.Interface(),
	}

	return jerrors.Trace(session.WritePkg(resp, 5*time.Second))
}

////////////////////////////////////////////
// RpcClientHandler
////////////////////////////////////////////

type RpcClientHandler struct {
	conn *gettyRPCClient
}

func NewRpcClientHandler(client *gettyRPCClient) *RpcClientHandler {
	return &RpcClientHandler{conn: client}
}

func (h *RpcClientHandler) OnOpen(session getty.Session) error {
	h.conn.addSession(session)
	return nil
}

func (h *RpcClientHandler) OnError(session getty.Session, err error) {
	log.Error("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	h.conn.removeSession(session)
}

func (h *RpcClientHandler) OnClose(session getty.Session) {
	log.Info("session{%s} is closing......", session.Stat())
	h.conn.removeSession(session)
}

func (h *RpcClientHandler) OnMessage(session getty.Session, pkg interface{}) {
	p, ok := pkg.(*GettyRPCResponsePackage)
	if !ok {
		log.Error("illegal package{%#v}", pkg)
		return
	}
	// log.Debug("get rpc response{%#v}", p)
	h.conn.updateSession(session)

	pendingResponse := h.conn.pool.rpcClient.removePendingResponse(p.H.Sequence)
	if pendingResponse == nil {
		log.Error("failed to get pending response context for response package %s", *p)
		return
	}
	if p.H.Command == gettyCmdHbResponse {
		return
	}
	if p.H.Code == GettyFail && len(p.header.Error) > 0 {
		pendingResponse.err = jerrors.New(p.header.Error)
		if pendingResponse.callback == nil {
			pendingResponse.done <- struct{}{}
		} else {
			pendingResponse.callback(pendingResponse.GetCallResponse())
		}
		return
	}
	codec := Codecs[p.H.CodecType]
	if codec == nil {
		pendingResponse.err = jerrors.Errorf("can not find codec for %d", p.H.CodecType)
		pendingResponse.done <- struct{}{}
		return
	}
	err := codec.Decode(p.body, pendingResponse.reply)
	pendingResponse.err = err
	if pendingResponse.callback == nil {
		pendingResponse.done <- struct{}{}
	} else {
		pendingResponse.callback(pendingResponse.GetCallResponse())
	}
}

func (h *RpcClientHandler) OnCron(session getty.Session) {
	rpcSession, err := h.conn.getClientRpcSession(session)
	if err != nil {
		log.Error("client.getClientSession(session{%s}) = error{%s}",
			session.Stat(), jerrors.ErrorStack(err))
		return
	}
	if h.conn.pool.rpcClient.conf.sessionTimeout.Nanoseconds() < time.Since(session.GetActive()).Nanoseconds() {
		log.Warn("session{%s} timeout{%s}, reqNum{%d}",
			session.Stat(), time.Since(session.GetActive()).String(), rpcSession.GetReqNum())
		h.conn.removeSession(session) // -> h.conn.close() -> h.conn.pool.remove(h.conn)
		return
	}

	codecType := GetCodecType(h.conn.protocol)

	h.conn.pool.rpcClient.heartbeat(session, codecType)
}
