package rpc

import (
	"encoding/json"
	"reflect"
	"sync"
	"time"
)

import (
	"github.com/AlexStocks/getty"
	jerrors "github.com/juju/errors"

	log "github.com/AlexStocks/log4go"
)

const (
	CmdTypePing = "ping"
	CmdTypeErr  = "err"
	CmdTypeAck  = "ack"
)

var (
	errTooManySessions = jerrors.New("too many echo sessions")
)

type rpcSession struct {
	session getty.Session
	active  time.Time
	reqNum  int32
}

type RpcServerHandler struct {
	maxSessionNum  int
	sessionTimeout time.Duration
	sessionMap     map[getty.Session]*rpcSession
	rwlock         sync.RWMutex

	sendLock sync.Mutex
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
	if h.maxSessionNum < len(h.sessionMap) {
		err = errTooManySessions
	}
	h.rwlock.RUnlock()
	if err != nil {
		return err
	}

	log.Info("got session:%s", session.Stat())
	h.rwlock.Lock()
	h.sessionMap[session] = &rpcSession{session: session}
	h.rwlock.Unlock()
	return nil
}

func (h *RpcServerHandler) OnError(session getty.Session, err error) {
	log.Info("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	h.rwlock.Lock()
	delete(h.sessionMap, session)
	h.rwlock.Unlock()
}

func (h *RpcServerHandler) OnClose(session getty.Session) {
	log.Info("session{%s} is closing......", session.Stat())
	h.rwlock.Lock()
	delete(h.sessionMap, session)
	h.rwlock.Unlock()
}

func (h *RpcServerHandler) OnMessage(session getty.Session, pkg interface{}) {
	p, ok := pkg.(*RpcRequest)
	if !ok {
		log.Error("illegal packge{%#v}", pkg)
		return
	}

	if p.header.IsPing() {
		h.replyCmd(session, p.header.Seq, "", CmdTypePing)
		return
	}

	if p.header.CallType == RequestSendOnly {
		h.asyncCallService(session, p.header.Seq, p.service, p.methodType, p.argv, p.replyv)
		return
	}
	h.callService(session, p.header.Seq, p.service, p.methodType, p.argv, p.replyv)
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

func (h *RpcServerHandler) replyCmd(session getty.Session, seq uint64, err string, cmd string) {
	resp := NewRpcResponse()
	resp.header.Seq = seq
	switch cmd {
	case CmdTypePing:
		resp.header.ReplyType = ReplyTypePong
	case CmdTypeAck:
		resp.header.ReplyType = ReplyTypeAck
	case CmdTypeErr:
		resp.header.ReplyType = ReplyTypeAck
		resp.header.Error = err
	}
	h.sendLock.Lock()
	defer h.sendLock.Unlock()
	session.WritePkg(resp, 0)
}

func (h *RpcServerHandler) asyncCallService(session getty.Session, seq uint64, service *service, methodType *methodType, argv, replyv reflect.Value) {
	h.replyCmd(session, seq, "", CmdTypeAck)
	function := methodType.method.Func
	function.Call([]reflect.Value{service.rcvr, argv, replyv})
	return
}

func (h *RpcServerHandler) callService(session getty.Session, seq uint64, service *service, methodType *methodType, argv, replyv reflect.Value) {
	function := methodType.method.Func
	returnValues := function.Call([]reflect.Value{service.rcvr, argv, replyv})
	errInter := returnValues[0].Interface()

	resp := NewRpcResponse()
	resp.header.ReplyType = ReplyTypeData
	resp.header.Seq = seq
	if errInter != nil {
		h.replyCmd(session, seq, errInter.(error).Error(), CmdTypeErr)
		return
	}
	resp.body = replyv.Interface()
	h.sendLock.Lock()
	defer h.sendLock.Unlock()
	session.WritePkg(resp, 0)
}

type RpcClientHandler struct {
	client *Client
}

func NewRpcClientHandler(client *Client) *RpcClientHandler {
	h := &RpcClientHandler{
		client: client,
	}
	return h
}

func (h *RpcClientHandler) OnOpen(session getty.Session) error {
	h.client.addSession(session)
	return nil
}

func (h *RpcClientHandler) OnError(session getty.Session, err error) {
	log.Info("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	h.client.removeSession(session)
}

func (h *RpcClientHandler) OnClose(session getty.Session) {
	log.Info("session{%s} is closing......", session.Stat())
	h.client.removeSession(session)
}

func (h *RpcClientHandler) OnMessage(session getty.Session, pkg interface{}) {
	p, ok := pkg.(*RpcResponse)
	if !ok {
		log.Error("illegal packge{%#v}", pkg)
		return
	}
	log.Debug("get rpc response{%s}", p)
	h.client.updateSession(session)

	pendingResponse := h.client.RemovePendingResponse(p.header.Seq)
	if p.header.ReplyType == ReplyTypePong {
		return
	}
	if len(p.header.Error) > 0 {
		pendingResponse.err = jerrors.New(p.header.Error)
	}
	err := json.Unmarshal(p.body.([]byte), pendingResponse.reply)
	if err != nil {
		pendingResponse.err = err
	}
	pendingResponse.done <- struct{}{}
}

func (h *RpcClientHandler) OnCron(session getty.Session) {
	rpcSession, err := h.client.getClientRpcSession(session)
	if err != nil {
		log.Error("client.getClientSession(session{%s}) = error{%#v}", session.Stat(), err)
		return
	}
	if conf.sessionTimeout.Nanoseconds() < time.Since(session.GetActive()).Nanoseconds() {
		log.Warn("session{%s} timeout{%s}, reqNum{%d}",
			session.Stat(), time.Since(session.GetActive()).String(), rpcSession.reqNum)
		h.client.removeSession(session)
		return
	}

	h.client.ping(session)
}
