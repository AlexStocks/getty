package rpc

import (
    "fmt"
    "net"
    "reflect"
    //"sync"
    "time"
)

import (
    log "github.com/AlexStocks/log4go"
    jerrors "github.com/juju/errors"
)

import (
    "github.com/AlexStocks/getty/transport"
)

/////////////////////////////////////////////////
// receiver
/////////////////////////////////////////////////

type receiver struct {
    maxSessionNum  int                      // 需要删除 TODO
    sessionTimeout time.Duration            // 需要删除 TODO
    //rwlock         sync.RWMutex             // protect follows
    addr           string                   // 映射到 rpcSession pool
    conf           ClientConfig
    //sessionMap     map[string]*rpcSession   // addr + protocol TODO protocol 复用 addr 确定一个 session
}

// TODO opts client
func newReceiver(addr string) *receiver {
    return &receiver{
        maxSessionNum:  12,
        sessionTimeout: 10,
        addr: addr,
        //sessionMap:     make(map[string]*rpcSession),
    }
}

func (c receiver) newSession(session getty.Session) error {
    var (
        ok      bool
        tcpConn *net.TCPConn
        conf    ClientConfig
    )

    conf = c.conf
    if conf.GettySessionParam.CompressEncoding {
        session.SetCompressType(getty.CompressZip)
    }

    if tcpConn, ok = session.Conn().(*net.TCPConn); !ok {
        panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp connection\n", session.Stat(), session.Conn()))
    }

    tcpConn.SetNoDelay(conf.GettySessionParam.TcpNoDelay)
    tcpConn.SetKeepAlive(conf.GettySessionParam.TcpKeepAlive)
    if conf.GettySessionParam.TcpKeepAlive {
        tcpConn.SetKeepAlivePeriod(conf.GettySessionParam.keepAlivePeriod)
    }
    tcpConn.SetReadBuffer(conf.GettySessionParam.TcpRBufSize)
    tcpConn.SetWriteBuffer(conf.GettySessionParam.TcpWBufSize)

    session.SetName(conf.GettySessionParam.SessionName)
    session.SetMaxMsgLen(conf.GettySessionParam.MaxMsgLen)
    session.SetPkgHandler(rpcClientPackageHandler)
    session.SetEventListener(NewRpcClientHandler(c))
    session.SetEventListener(newReceiver(c))
    // session.SetRQLen(conf.GettySessionParam.PkgRQSize)
    session.SetWQLen(conf.GettySessionParam.PkgWQSize)
    session.SetReadTimeout(conf.GettySessionParam.tcpReadTimeout)
    session.SetWriteTimeout(conf.GettySessionParam.tcpWriteTimeout)
    session.SetCronPeriod((int)(conf.heartbeatPeriod.Nanoseconds() / 1e6))
    session.SetWaitTime(conf.GettySessionParam.waitTimeout)
    log.Debug("client new session:%s\n", session.Stat())

    return nil
}

func (h *receiver) OnOpen(session getty.Session) error {
    //var err error
    /*
    h.rwlock.RLock()
    if h.maxSessionNum <= len(h.sessionMap) {
        err = errTooManySessions
    }
    h.rwlock.RUnlock()
    if err != nil {
        return jerrors.Trace(err)
    }
    */

    log.Debug("got session:%s", session.Stat())
    pool.lock.Lock()
    // TODO 全局 session 如何维护？如果已经存在，判断失活？
    pool.connMap[session.RemoteAddr()] = &rpcSession{session: session}
    //h.sessionMap[session] = &rpcSession{session: session}
    pool.lock.Unlock()
    return nil
}

func (*receiver) OnError(session getty.Session, err error) {
    log.Debug("session{%s} got error{%v}, will be closed.", session.Stat(), err)
    pool.lock.Lock()
    // TODO 如何找到全局的 session 然后删除？
    delete(pool.connMap, session.RemoteAddr())
    pool.lock.Unlock()
}

func (*receiver) OnClose(session getty.Session) {
    log.Debug("session{%s} is closing......", session.Stat())
    pool.lock.Lock()
    // TODO 如何找到全局的 session 然后删除？
    delete(pool.connMap, session.RemoteAddr())
    pool.lock.Unlock()
}

func (h *receiver) OnMessage(session getty.Session, pkg interface{}) {
    var rs *rpcSession
    // TODO 如何找到全局的 session
    if session != nil {
        func() {
            pool.lock.RLock()
            defer pool.lock.RUnlock()

            if _, ok := pool.connMap[session.RemoteAddr()]; ok {
                rs = pool.connMap[session.RemoteAddr()]
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

func (h *receiver) OnCron(session getty.Session) {
    var (
        flag   bool
        active time.Time
    )

    pool.lock.RLock()
    if _, ok := pool.connMap[session.RemoteAddr()]; ok {
        active = session.GetActive()
        if h.sessionTimeout.Nanoseconds() < time.Since(active).Nanoseconds() {
            flag = true
            log.Warn("session{%s} timeout{%s}, reqNum{%d}",
                session.Stat(), time.Since(active).String(), pool.connMap[session.RemoteAddr()].GetReqNum())
        }
    }
    pool.lock.RUnlock()

    if flag {
        pool.lock.Lock()
        delete(pool.connMap, session.RemoteAddr())
        pool.lock.Unlock()
        session.Close()
    }
}

func (h *receiver) replyCmd(session getty.Session, req GettyRPCRequestPackage, cmd gettyCommand, err string) {
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
    _ = session.WritePkg(resp, 5*time.Second)
}

func (h *receiver) callService(session getty.Session, req GettyRPCRequestPackage,
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
