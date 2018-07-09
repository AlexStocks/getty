package rpc

import (
	"reflect"
	"sync"
	"time"
)

import (
	"github.com/AlexStocks/getty"
	jerrors "github.com/juju/errors"

	log "github.com/AlexStocks/log4go"
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

	req, ok := pkg.(GettyRPCRequestPackage)
	if !ok {
		log.Error("illegal packge{%#v}", pkg)
		return
	}
	// heartbeat
	if req.H.Command == gettyCmdHbRequest {
		h.replyCmd(session, req, gettyCmdHbResponse, "")
		return
	}
	if req.header.CallType == gettyTwoWayNoReply {
		h.replyCmd(session, req, gettyCmdRPCResponse, "")
		function := req.methodType.method.Func
		function.Call([]reflect.Value{req.service.rcvr, req.argv, req.replyv})
		return
	}
	h.callService(session, req, req.service, req.methodType, req.argv, req.replyv)
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
	service *service, methodType *methodType, argv, replyv reflect.Value) {

	function := methodType.method.Func
	returnValues := function.Call([]reflect.Value{service.rcvr, argv, replyv})
	errInter := returnValues[0].Interface()
	if errInter != nil {
		h.replyCmd(session, req, gettyCmdRPCResponse, errInter.(error).Error())
		return
	}

	resp := GettyPackage{
		H: req.H,
	}
	resp.H.Code = GettyOK
	resp.H.Command = gettyCmdRPCResponse
	resp.B = &GettyRPCResponse{
		body: replyv.Interface(),
	}

	session.WritePkg(resp, 5*time.Second)
}

////////////////////////////////////////////
// RpcClientHandler
////////////////////////////////////////////

type RpcClientHandler struct {
	conn *gettyRPCClientConn
}

func NewRpcClientHandler(client *gettyRPCClientConn) *RpcClientHandler {
	return &RpcClientHandler{conn: client}
}

func (h *RpcClientHandler) OnOpen(session getty.Session) error {
	h.conn.addSession(session)
	return nil
}

func (h *RpcClientHandler) OnError(session getty.Session, err error) {
	log.Info("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	h.conn.removeSession(session)
}

func (h *RpcClientHandler) OnClose(session getty.Session) {
	log.Info("session{%s} is closing......", session.Stat())
	h.conn.removeSession(session)
}

func (h *RpcClientHandler) OnMessage(session getty.Session, pkg interface{}) {
	p, ok := pkg.(*GettyRPCResponsePackage)
	if !ok {
		log.Error("illegal packge{%#v}", pkg)
		return
	}
	log.Debug("get rpc response{%s}", p)
	h.conn.updateSession(session)

	pendingResponse := h.conn.pool.rpcClient.RemovePendingResponse(p.H.Sequence)
	if pendingResponse == nil {
		return
	}
	if p.H.Command == gettyCmdHbResponse {
		return
	}
	if p.H.Code == GettyFail && len(p.header.Error) > 0 {
		pendingResponse.err = jerrors.New(p.header.Error)
		pendingResponse.done <- struct{}{}
		return
	}
	codec := Codecs[p.H.CodecType]
	if codec == nil {
		pendingResponse.err = jerrors.Errorf("can not find codec for %d", p.H.CodecType)
		pendingResponse.done <- struct{}{}
		return
	}
	err := codec.Decode(p.body, pendingResponse.reply)
	if err != nil {
		pendingResponse.err = err
		pendingResponse.done <- struct{}{}
		return
	}
	pendingResponse.done <- struct{}{}
}

func (h *RpcClientHandler) OnCron(session getty.Session) {
	rpcSession, err := h.conn.getClientRpcSession(session)
	if err != nil {
		log.Error("client.getClientSession(session{%s}) = error{%#v}", session.Stat(), err)
		return
	}
	if h.conn.pool.rpcClient.conf.sessionTimeout.Nanoseconds() < time.Since(session.GetActive()).Nanoseconds() {
		log.Warn("session{%s} timeout{%s}, reqNum{%d}",
			session.Stat(), time.Since(session.GetActive()).String(), rpcSession.reqNum)
		h.conn.removeSession(session) // -> h.conn.close() -> h.conn.pool.remove(h.conn)
		return
	}

	h.conn.pool.rpcClient.heartbeat(session)
}
