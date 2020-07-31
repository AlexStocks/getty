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
	"fmt"
	"net"
	"reflect"
)

import (
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

import (
	"github.com/AlexStocks/getty/transport"
)

type Server struct {
	conf          ServerConfig
	serviceMap    map[string]*service
	tcpServerList []getty.Server
	rpcHandler    *RpcServerHandler
	pkgHandler    *RpcServerPackageHandler
}

func NewServer(conf *ServerConfig) (*Server, error) {
	if err := conf.CheckValidity(); err != nil {
		return nil, jerrors.Trace(err)
	}

	s := &Server{
		serviceMap: make(map[string]*service),
		conf:       *conf,
	}
	s.rpcHandler = NewRpcServerHandler(s.conf.SessionNumber, s.conf.sessionTimeout)
	s.pkgHandler = NewRpcServerPackageHandler(s)

	return s, nil
}

func (s *Server) Register(rcvr GettyRPCService) error {
	svc := &service{
		typ:  reflect.TypeOf(rcvr),
		rcvr: reflect.ValueOf(rcvr),
		name: reflect.Indirect(reflect.ValueOf(rcvr)).Type().Name(),
		// Install the methods
		method: suitableMethods(reflect.TypeOf(rcvr)),
	}
	if svc.name == "" {
		s := "rpc.Register: no service name for type " + svc.typ.String()
		log.Error(s)
		return jerrors.New(s)
	}
	if !isExported(svc.name) {
		s := "rpc.Register: type " + svc.name + " is not exported"
		log.Error(s)
		return jerrors.New(s)
	}
	if _, present := s.serviceMap[svc.name]; present {
		return jerrors.New("rpc: service already defined: " + svc.name)
	}

	if len(svc.method) == 0 {
		// To help the user, see if a pointer receiver would work.
		method := suitableMethods(reflect.PtrTo(svc.typ))
		str := "rpc.Register: type " + svc.name + " has no exported methods of suitable type"
		if len(method) != 0 {
			str = "rpc.Register: type " + svc.name + " has no exported methods of suitable type (" +
				"hint: pass a pointer to value of that type)"
		}
		log.Error(str)

		return jerrors.New(str)
	}

	s.serviceMap[svc.name] = svc

	return nil
}

func (s *Server) newSession(session getty.Session) error {
	var (
		ok      bool
		tcpConn *net.TCPConn
	)

	if s.conf.GettySessionParam.CompressEncoding {
		session.SetCompressType(getty.CompressZip)
	}

	if tcpConn, ok = session.Conn().(*net.TCPConn); !ok {
		panic(fmt.Sprintf("%s, session.conn{%#v} is not tcp connection\n", session.Stat(), session.Conn()))
	}

	tcpConn.SetNoDelay(s.conf.GettySessionParam.TcpNoDelay)
	tcpConn.SetKeepAlive(s.conf.GettySessionParam.TcpKeepAlive)
	if s.conf.GettySessionParam.TcpKeepAlive {
		tcpConn.SetKeepAlivePeriod(s.conf.GettySessionParam.keepAlivePeriod)
	}
	tcpConn.SetReadBuffer(s.conf.GettySessionParam.TcpRBufSize)
	tcpConn.SetWriteBuffer(s.conf.GettySessionParam.TcpWBufSize)

	session.SetName(s.conf.GettySessionParam.SessionName)
	session.SetMaxMsgLen(s.conf.GettySessionParam.MaxMsgLen)
	session.SetPkgHandler(s.pkgHandler)
	session.SetEventListener(s.rpcHandler)
	session.SetRQLen(s.conf.GettySessionParam.PkgRQSize)
	session.SetWQLen(s.conf.GettySessionParam.PkgWQSize)
	session.SetReadTimeout(s.conf.GettySessionParam.tcpReadTimeout)
	session.SetWriteTimeout(s.conf.GettySessionParam.tcpWriteTimeout)
	session.SetCronPeriod((int)(s.conf.sessionTimeout.Nanoseconds() / 1e6))
	session.SetWaitTime(s.conf.GettySessionParam.waitTimeout)
	log.Debug("app accepts new session:%s\n", session.Stat())

	return nil
}

func (s *Server) Start() {
	var (
		addr      string
		portList  []string
		tcpServer getty.Server
	)

	portList = s.conf.Ports
	if len(portList) == 0 {
		panic("portList is nil")
	}
	for _, port := range portList {
		addr = net.JoinHostPort(s.conf.Host, port)
		tcpServer = getty.NewTCPServer(
			getty.WithLocalAddress(addr),
		)
		tcpServer.RunEventLoop(s.newSession)
		log.Debug("s bind addr{%s} ok!", addr)
		s.tcpServerList = append(s.tcpServerList, tcpServer)
	}
}

func (s *Server) Stop() {
	list := s.tcpServerList
	s.tcpServerList = nil
	if list != nil {
		for _, tcpServer := range list {
			tcpServer.Close()
		}
	}
}
