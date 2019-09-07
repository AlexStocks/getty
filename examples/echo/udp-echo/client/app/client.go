/******************************************************
# DESC    : echo client
# AUTHOR  : Alex Stocks
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2016-09-06 17:24
# FILE    : client.go
******************************************************/

package main

import (
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

import (
	"github.com/AlexStocks/getty"
	log "github.com/AlexStocks/log4go"
)

var (
	reqID uint32
	src   = rand.NewSource(time.Now().UnixNano())
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

////////////////////////////////////////////////////////////////////
// echo client
////////////////////////////////////////////////////////////////////

type EchoClient struct {
	lock        sync.RWMutex
	sessions    []*clientEchoSession
	gettyClient getty.Client
	serverAddr  net.UDPAddr
}

func (c *EchoClient) isAvailable() bool {
	if c.selectSession() == nil {
		return false
	}

	return true
}

func (c *EchoClient) close() {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.gettyClient != nil {
		c.gettyClient.Close()
		c.gettyClient = nil
		for _, s := range c.sessions {
			log.Info("close client session{%s, last active:%s, request number:%d}",
				s.session.Stat(), s.session.GetActive().String(), s.reqNum)
			s.session.Close()
		}
		c.sessions = c.sessions[:0]
	}
}

func (c *EchoClient) selectSession() getty.Session {
	// get route server session
	c.lock.RLock()
	defer c.lock.RUnlock()
	count := len(c.sessions)
	if count == 0 {
		//log.Warn("client session array is nil...")
		return nil
	}

	return c.sessions[rand.Int31n(int32(count))].session
}

func (c *EchoClient) addSession(session getty.Session) {
	log.Debug("add session{%s}", session.Stat())
	if session == nil {
		return
	}

	c.lock.Lock()
	c.sessions = append(c.sessions, &clientEchoSession{session: session})
	log.Debug("after add session{%s}, session number:%d", session.Stat(), len(c.sessions))
	c.lock.Unlock()
}

func (c *EchoClient) removeSession(session getty.Session) {
	if session == nil {
		return
	}

	c.lock.Lock()

	for i, s := range c.sessions {
		if s.session == session {
			c.sessions = append(c.sessions[:i], c.sessions[i+1:]...)
			log.Debug("delete session{%s}, its index{%d}", session.Stat(), i)
			break
		}
	}
	log.Info("after remove session{%s}, left session number:%d", session.Stat(), len(c.sessions))

	c.lock.Unlock()
}

func (c *EchoClient) updateSession(session getty.Session) {
	if session == nil {
		return
	}

	c.lock.Lock()

	for i, s := range c.sessions {
		if s.session == session {
			c.sessions[i].reqNum++
			break
		}
	}

	c.lock.Unlock()
}

func (c *EchoClient) getClientEchoSession(session getty.Session) (clientEchoSession, error) {
	var (
		err         error
		echoSession clientEchoSession
	)

	c.lock.Lock()

	err = errSessionNotExist
	for _, s := range c.sessions {
		if s.session == session {
			echoSession = *s
			err = nil
			break
		}
	}

	c.lock.Unlock()

	return echoSession, err
}

func (c *EchoClient) heartbeat(session getty.Session) {
	var (
		err error
		pkg EchoPackage
		ctx getty.UDPContext
	)

	pkg.H.Magic = echoPkgMagic
	pkg.H.LogID = (uint32)(src.Int63())
	pkg.H.Sequence = atomic.AddUint32(&reqID, 1)
	// pkg.H.ServiceID = 0
	pkg.H.Command = heartbeatCmd
	pkg.B = echoHeartbeatRequestString
	pkg.H.Len = (uint16)(len(pkg.B) + 1)

	ctx.Pkg = &pkg
	ctx.PeerAddr = &(c.serverAddr)

	//if err := session.WritePkg(ctx, WritePkgTimeout); err != nil {
	if err = session.WritePkg(ctx, WritePkgASAP); err != nil {
		log.Warn("session.WritePkg(session{%s}, context{%#v}) = error{%v}", session.Stat(), ctx, err)
		session.Close()

		c.removeSession(session)
	}
	log.Debug("session.WritePkg(session{%s}, context{%#v})", session.Stat(), ctx)
}
