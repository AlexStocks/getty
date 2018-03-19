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

func (this *EchoClient) isAvailable() bool {
	if this.selectSession() == nil {
		return false
	}

	return true
}

func (this *EchoClient) close() {
	this.lock.Lock()
	if this.gettyClient != nil {
		for _, s := range this.sessions {
			log.Info("close client session{%s, last active:%s, request number:%d}",
				s.session.Stat(), s.session.GetActive().String(), s.reqNum)
			s.session.Close()
		}
		this.gettyClient.Close()
		this.gettyClient = nil
		this.sessions = this.sessions[:0]
	}
	this.lock.Unlock()
}

func (this *EchoClient) selectSession() getty.Session {
	// get route server session
	this.lock.RLock()
	defer this.lock.RUnlock()
	count := len(this.sessions)
	if count == 0 {
		log.Info("client session array is nil...")
		return nil
	}

	return this.sessions[rand.Int31n(int32(count))].session
}

func (this *EchoClient) addSession(session getty.Session) {
	log.Debug("add session{%s}", session.Stat())
	if session == nil {
		return
	}

	this.lock.Lock()
	this.sessions = append(this.sessions, &clientEchoSession{session: session})
	log.Debug("after add session{%s}, session number:%d", session.Stat(), len(this.sessions))
	this.lock.Unlock()
}

func (this *EchoClient) removeSession(session getty.Session) {
	if session == nil {
		return
	}

	this.lock.Lock()

	for i, s := range this.sessions {
		if s.session == session {
			this.sessions = append(this.sessions[:i], this.sessions[i+1:]...)
			log.Debug("delete session{%s}, its index{%d}", session.Stat(), i)
			break
		}
	}
	log.Info("after remove session{%s}, left session number:%d", session.Stat(), len(this.sessions))

	this.lock.Unlock()
}

func (this *EchoClient) updateSession(session getty.Session) {
	if session == nil {
		return
	}

	this.lock.Lock()

	for i, s := range this.sessions {
		if s.session == session {
			this.sessions[i].reqNum++
			break
		}
	}

	this.lock.Unlock()
}

func (this *EchoClient) getClientEchoSession(session getty.Session) (clientEchoSession, error) {
	var (
		err         error
		echoSession clientEchoSession
	)

	this.lock.Lock()

	err = errSessionNotExist
	for _, s := range this.sessions {
		if s.session == session {
			echoSession = *s
			err = nil
			break
		}
	}

	this.lock.Unlock()

	return echoSession, err
}

func (this *EchoClient) heartbeat(session getty.Session) {
	var (
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
	ctx.PeerAddr = &(this.serverAddr)

	if err := session.WritePkg(ctx, WritePkgTimeout); err != nil {
		log.Warn("session.WritePkg(session{%s}, context{%#v}) = error{%v}", session.Stat(), ctx, err)
		session.Close()

		this.removeSession(session)
	}
	log.Debug("session.WritePkg(session{%s}, context{%#v})", session.Stat(), ctx)
}
