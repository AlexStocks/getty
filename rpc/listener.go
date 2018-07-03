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
	CmdTypeErr = "err"
	CmdTypeAck = "ack"
)

var (
	errTooManySessions = jerrors.New("too many echo sessions")
)

type rpcSession struct {
	session getty.Session
	reqNum  int32
}

////////////////////////////////////////////
// RpcServerHandler
////////////////////////////////////////////

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
		return jerrors.Trace(err)
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
	h.rwlock.Lock()
	if _, ok := h.sessionMap[session]; ok {
		h.sessionMap[session].reqNum++
	}
	h.rwlock.Unlock()

	p, ok := pkg.(*GettyPackage)
	if !ok {
		log.Error("illegal packge{%#v}", pkg)
		return
	}
	req, ok := p.B.(*GettyRPCRequest)
	if !ok {
		log.Error("illegal request{%#v}", p.B)
		return
	}

	if p.H.Command == gettyCmdHbRequest {
		h.replyCmd(session, p, gettyCmdHbResponse, "")
		return
	}

	if req.header.CallType == gettyTwoWayNoReply {
		h.replyCmd(session, p, gettyCmdRPCResponse, "")
		function := req.methodType.method.Func
		function.Call([]reflect.Value{req.service.rcvr, req.argv, req.replyv})
		return
	}
	h.callService(session, p, req.service, req.methodType, req.argv, req.replyv)
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

func (h *RpcServerHandler) replyCmd(session getty.Session, reqPkg *GettyPackage, cmd gettyCommand, err string) {
	rspPkg := *reqPkg
	rspPkg.H.Code = 0
	rspPkg.H.Command = gettyCmdRPCResponse
	if len(err) != 0 {
		rspPkg.H.Code = GettyFail
		rspPkg.B = &GettyRPCResponse{
			header: GettyRPCResponseHeader{
				Error: err,
			},
		}
	}

	h.sendLock.Lock()
	defer h.sendLock.Unlock()
	session.WritePkg(&rspPkg, 0)
}

func (h *RpcServerHandler) callService(session getty.Session, reqPkg *GettyPackage, service *service,
	methodType *methodType, argv, replyv reflect.Value) {

	function := methodType.method.Func
	returnValues := function.Call([]reflect.Value{service.rcvr, argv, replyv})
	errInter := returnValues[0].Interface()

	if errInter != nil {
		h.replyCmd(session, reqPkg, gettyCmdRPCResponse, errInter.(error).Error())
		return
	}

	rspPkg := *reqPkg
	rspPkg.H.Code = 0
	rspPkg.H.Command = gettyCmdRPCResponse
	rspPkg.B = &GettyRPCResponse{
		body: replyv.Interface(),
	}
	h.sendLock.Lock()
	defer h.sendLock.Unlock()
	session.WritePkg(&rspPkg, 0)
}

////////////////////////////////////////////
// RpcClientHandler
////////////////////////////////////////////

type RpcClientHandler struct {
	client *Client
}

func NewRpcClientHandler(client *Client) *RpcClientHandler {
	return &RpcClientHandler{client: client}
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
	p, ok := pkg.(*GettyPackage)
	if !ok {
		log.Error("illegal packge{%#v}", pkg)
		return
	}
	log.Debug("get rpc response{%s}", p)
	h.client.updateSession(session)

	pendingResponse := h.client.RemovePendingResponse(p.H.Sequence)
	if p.H.Command == gettyCmdHbResponse {
		return
	}
	if p.B == nil {
		log.Error("response:{%#v} body is nil", p)
		return
	}
	rsp, ok := p.B.(*GettyRPCResponse)
	if !ok {
		log.Error("response body:{%#v} type is not *GettyRPCResponse", p.B)
		return
	}
	if p.H.Code == GettyFail && len(rsp.header.Error) > 0 {
		pendingResponse.err = jerrors.New(rsp.header.Error)
	}
	err := json.Unmarshal(rsp.body.([]byte), pendingResponse.reply)
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
	if h.client.conf.sessionTimeout.Nanoseconds() < time.Since(session.GetActive()).Nanoseconds() {
		log.Warn("session{%s} timeout{%s}, reqNum{%d}",
			session.Stat(), time.Since(session.GetActive()).String(), rpcSession.reqNum)
		h.client.removeSession(session)
		return
	}

	h.client.ping(session)
}
