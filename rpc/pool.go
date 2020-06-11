package rpc

import (
	"fmt"
	"math/rand"
	//"net"
	"strings"
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

// 连接池
var pool *gettyRPCPool

// 原 gettyRPCClient
type gettyRPCConn struct {
	once     sync.Once
	protocol string
	addr     string
	active   int64 // 为0，则说明没有被创建或者被销毁了

	lock        sync.RWMutex

	endpoint    getty.EndPoint
	sessions    []*rpcSession
}

var (
	errClientPoolClosed = jerrors.New("client pool closed")
)

// Client Example
func newRPCClient(protocol, addr string) {
	client := getty.NewTCPClient(getty.WithServerAddress(addr))
	conn, err := newGettyRPCConn(protocol, addr, client)
	if err != nil {
		session := conn.selectSession()
		session.WritePkg(nil, 0)
	}
}

// Server Example
func newRPCServer(protocol, addr string) {
	server := getty.NewTCPServer()
	conn, err := newGettyRPCConn(protocol, addr, server)
	if err != nil {
		session := conn.selectSession()
		session.WritePkg(nil, 0)
	}
}

func newGettyRPCConn(protocol, addr string, endpoint getty.EndPoint) (*gettyRPCConn, error) {
	c := &gettyRPCConn{
		protocol: protocol,
		addr:     addr,
		endpoint: endpoint,
	}
	// 拆分 handler 注册
	rc := newReceiver(addr)
	go endpoint.RunEventLoop(rc.newSession)
	// TODO 判断 session 有效
	idx := 1
	for {
		idx++
		if c.isAvailable() {
			break
		}

		if idx > 2000 {
			c.endpoint.Close()
			return nil, jerrors.New(fmt.Sprintf("failed to create endpoint connection to %s in 3 seconds", addr))
		}
		time.Sleep(1e6)
	}
	log.Info("endpoint init ok")
	c.updateActive(time.Now().Unix())
	return c, nil
}

func (c *gettyRPCConn) updateActive(active int64) {
	atomic.StoreInt64(&c.active, active)
}

func (c *gettyRPCConn) getActive() int64 {
	return atomic.LoadInt64(&c.active)
}

// TODO 多协议可以共地址端口
func (c *gettyRPCConn) selectSession() getty.Session {
	c.lock.RLock()
	defer c.lock.RUnlock()

	count := len(c.sessions)
	if count == 0 {
		return nil
	}
	return c.sessions[rand.Int31n(int32(count))].session
}

func (c *gettyRPCConn) addSession(session getty.Session) {
	log.Debug("add session{%s}", session.Stat())
	if session == nil {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.sessions = append(c.sessions, &rpcSession{session: session})
}

// 需要指定地址？？
func (c *gettyRPCConn) removeSession(session getty.Session) {
	if session == nil {
		return
	}

	var removeFlag bool
	func() {
		c.lock.Lock()
		defer c.lock.Unlock()
		if len(c.sessions) == 0 {
			return
		}

		for i, s := range c.sessions {
			if s.session == session {
				c.sessions = append(c.sessions[:i], c.sessions[i+1:]...)
				log.Debug("delete session{%s}, its index{%d}", session.Stat(), i)
				break
			}
		}
		log.Info("after remove session{%s}, left session number:%d", session.Stat(), len(c.sessions))
		if len(c.sessions) == 0 {
			removeFlag = true
		}
	}()
	if removeFlag {
		c.close() // -> pool.remove(c)
	}
}

func (c *gettyRPCConn) updateSession(session getty.Session) {
	if session == nil {
		return
	}

	var rs *rpcSession
	func() {
		c.lock.RLock()
		defer c.lock.RUnlock()
		if c.sessions == nil {
			return
		}

		for i, s := range c.sessions {
			if s.session == session {
				rs = c.sessions[i]
				break
			}
		}
	}()
	if rs != nil {
		rs.AddReqNum(1)
	}
}

func (c *gettyRPCConn) getRpcSession(session getty.Session) (rpcSession, error) {
	var (
		err        error
		rpcSession rpcSession
	)
	c.lock.Lock()
	defer c.lock.Unlock()
	if len(c.sessions) == 0 {
		return rpcSession, errClientClosed
	}

	err = errSessionNotExist
	for _, s := range c.sessions {
		if s.session == session {
			rpcSession = *s
			err = nil
			break
		}
	}

	return rpcSession, jerrors.Trace(err)
}

func (c *gettyRPCConn) isAvailable() bool {
	if c.selectSession() == nil {
		return false
	}

	return true
}

func (c *gettyRPCConn) close() error {
	err := jerrors.Errorf("close gettyRPCConn{%#v} again", c)
	c.once.Do(func() {
		// delete @c from client pool
		// TODO 全局 pool
		pool.remove(c)

		var (
			endpoint getty.EndPoint
			sessions    []*rpcSession
		)
		func() {
			c.lock.Lock()
			defer c.lock.Unlock()

			// 节点
			endpoint = c.endpoint
			c.endpoint = nil

			sessions = make([]*rpcSession, 0, len(c.sessions))
			for _, s := range c.sessions {
				sessions = append(sessions, s)
			}
			c.sessions = c.sessions[:0]
		}()

		c.updateActive(0)

		go func() {
			if endpoint != nil {
				endpoint.Close()
			}
			for _, s := range sessions {
				log.Info("close endpoint session{%s, last active:%s, request number:%d}",
					s.session.Stat(), s.session.GetActive().String(), s.GetReqNum())
				s.session.Close()
			}
		}()

		err = nil
	})

	return err
}

type gettyRPCPool struct {
	rpcClient *Client  // TODO 抽象出 config
	//size      int   // []*gettyRPCClient数组的size
	ttl       int64 // 每个gettyRPCConn的有效期时间. pool对象会在getConn时执行ttl检查

	lock  sync.RWMutex
	//connMap RPCClientMap
	connMap map[string]*gettyRPCConn // key: addr+protocol val: gettyRPCConn  TODO protocol 公用连接
	//connMap sync.Map
}

// TODO opts
func newgettyRPCPool(rpcClient *Client, ttl time.Duration) *gettyRPCPool {
	return &gettyRPCPool{
		rpcClient: rpcClient,
		ttl:       int64(ttl.Seconds()),
	}
}

func (p *gettyRPCPool) close() {
	p.lock.Lock()
	defer p.lock.Unlock()
	for _, val := range p.connMap {
		val.close()
	}
}

func (p *gettyRPCPool) get(protocol, addr string) (*gettyRPCConn, error) {
	var builder strings.Builder

	builder.WriteString(addr)
	builder.WriteString("@")
	builder.WriteString(protocol)

	key := builder.String()
	conn, ok := p.connMap[key]
	if ok {
		return conn, nil
	}

	// create new conn
	// TODO 如何创建？
	conn, err := newGettyRPCConn(protocol, addr, nil)
	return conn, jerrors.Trace(err)
}

func (p *gettyRPCPool) put(conn *gettyRPCConn) {
	if conn == nil || conn.getActive() == 0 {
		return
	}

	var builder strings.Builder

	builder.WriteString(conn.addr)
	builder.WriteString("@")
	builder.WriteString(conn.protocol)

	key := builder.String()
	p.lock.Lock()
	defer p.lock.Unlock()
	// TODO 检测
	p.connMap[key] = conn
}

// TODO delete
func (p *gettyRPCPool) remove(conn *gettyRPCConn) {
	if conn == nil || conn.getActive() == 0 {
		return
	}

	var builder strings.Builder

	builder.WriteString(conn.addr)
	builder.WriteString("@")
	builder.WriteString(conn.protocol)

	key := builder.String()
	p.lock.Lock()
	defer p.lock.Unlock()
	conn, ok := p.connMap[key]
	if !ok {
		return
	}
	delete(p.connMap, key)
}
