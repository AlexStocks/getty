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
	"bytes"
	"reflect"
)

import (
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

import (
	"github.com/AlexStocks/getty/transport"
)

////////////////////////////////////////////
// RpcServerPackageHandler
////////////////////////////////////////////

type RpcServerPackageHandler struct {
	server *Server
}

func NewRpcServerPackageHandler(server *Server) *RpcServerPackageHandler {
	return &RpcServerPackageHandler{
		server: server,
	}
}

func (p *RpcServerPackageHandler) Read(ss getty.Session, data []byte) (interface{}, int, error) {
	pkg := &GettyPackage{
		B: NewGettyRPCRequest(),
	}

	buf := bytes.NewBuffer(data)
	length, err := pkg.Unmarshal(buf)
	if err != nil {
		if jerrors.Cause(err) == ErrNotEnoughStream {
			return nil, 0, nil
		}
		return nil, 0, jerrors.Trace(err)
	}

	req := GettyRPCRequestPackage{H: pkg.H}
	if pkg.H.Command == gettyCmdHbRequest {
		return req, length, nil
	}
	req.header = pkg.B.GetHeader().(GettyRPCRequestHeader)

	// get service & method
	req.service = p.server.serviceMap[req.header.Service]
	if req.service != nil {
		req.methodType = req.service.method[req.header.Method]
	}
	if req.service == nil {
		return nil, 0, jerrors.Errorf("request service is nil")
	}
	if req.methodType == nil {
		return nil, 0, jerrors.Errorf("request method is nil")
	}
	// get args
	argIsValue := false
	if req.methodType.ArgType.Kind() == reflect.Ptr {
		req.argv = reflect.New(req.methodType.ArgType.Elem())
	} else {
		req.argv = reflect.New(req.methodType.ArgType)
		argIsValue = true
	}
	codec := Codecs[req.H.CodecType]
	if codec == nil {
		return nil, 0, jerrors.Errorf("can not find codec for %d", req.H.CodecType)
	}
	err = codec.Decode(pkg.B.GetBody(), req.argv.Interface())
	if err != nil {
		return nil, 0, jerrors.Trace(err)
	}
	if argIsValue {
		req.argv = req.argv.Elem()
	}
	// get reply
	if req.methodType.ReplyType != nil {
		req.replyv = reflect.New(req.methodType.ReplyType.Elem())
	}

	return req, length, nil
}

func (p *RpcServerPackageHandler) Write(ss getty.Session, pkg interface{}) ([]byte, error) {
	resp, ok := pkg.(GettyPackage)
	if !ok {
		log.Error("illegal pkg:%+v\n", pkg)
		return nil, jerrors.New("invalid rpc response")
	}

	buf, err := resp.Marshal()
	if err != nil {
		log.Warn("binary.Write(resp{%#v}) = err{%#v}", resp, err)
		return nil, jerrors.Trace(err)
	}

	return buf.Bytes(), nil
}

////////////////////////////////////////////
// RpcClientPackageHandler
////////////////////////////////////////////

var (
	rpcClientPackageHandler = &RpcClientPackageHandler{}
)

type RpcClientPackageHandler struct{}

func (p *RpcClientPackageHandler) Read(ss getty.Session, data []byte) (interface{}, int, error) {
	pkg := &GettyPackage{
		B: NewGettyRPCResponse(),
	}

	buf := bytes.NewBuffer(data)
	length, err := pkg.Unmarshal(buf)
	if err != nil {
		if err == ErrNotEnoughStream {
			return nil, 0, nil
		}
		return nil, 0, jerrors.Trace(err)
	}

	resp := &GettyRPCResponsePackage{
		H:      pkg.H,
		header: pkg.B.GetHeader().(GettyRPCResponseHeader),
	}
	if pkg.H.Command != gettyCmdHbResponse {
		resp.body = pkg.B.GetBody()
	}
	return resp, length, nil
}

func (p *RpcClientPackageHandler) Write(ss getty.Session, pkg interface{}) ([]byte, error) {
	req, ok := pkg.(GettyPackage)
	if !ok {
		log.Error("illegal pkg:%+v\n", pkg)
		return nil, jerrors.New("invalid rpc request")
	}

	buf, err := req.Marshal()
	if err != nil {
		log.Warn("binary.Write(req{%#v}) = err{%#v}", req, jerrors.ErrorStack(err))
		return nil, jerrors.Trace(err)
	}

	return buf.Bytes(), nil
}
