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

package main

import (
	"bytes"
	"errors"
	"fmt"
	"time"
)

import (
	getty "github.com/apache/dubbo-getty"
)

var echoPkgHandler = NewEchoPackageHandler()

type EchoPackageHandler struct{}

func NewEchoPackageHandler() *EchoPackageHandler {
	return &EchoPackageHandler{}
}

func (h *EchoPackageHandler) Read(ss getty.Session, data []byte) (interface{}, int, error) {
	var (
		err error
		len int
		pkg EchoPackage
		buf *bytes.Buffer
	)

	buf = bytes.NewBuffer(data)
	len, err = pkg.Unmarshal(buf)
	if err != nil {
		if err == ErrNotEnoughStream {
			return nil, 0, nil
		}

		return nil, 0, err
	}

	return &pkg, len, nil
}

func (h *EchoPackageHandler) Write(ss getty.Session, udpCtx interface{}) ([]byte, error) {
	var (
		ok        bool
		err       error
		startTime time.Time
		echoPkg   *EchoPackage
		buf       *bytes.Buffer
		ctx       getty.UDPContext
	)

	ctx, ok = udpCtx.(getty.UDPContext)
	if !ok {
		log.Errorf("illegal UDPContext{%#v}", udpCtx)
		return nil, fmt.Errorf("illegal @udpCtx{%#v}", udpCtx)
	}

	startTime = time.Now()
	if echoPkg, ok = ctx.Pkg.(*EchoPackage); !ok {
		log.Errorf("illegal pkg:%+v, addr:%s", ctx.Pkg, ctx.PeerAddr)
		return nil, errors.New("invalid echo package!")
	}

	buf, err = echoPkg.Marshal()
	if err != nil {
		log.Warnf("binary.Write(echoPkg{%#v}) = err{%#v}", echoPkg, err)
		return nil, err
	}

	log.Debugf("WriteEchoPkgTimeMs = %s", time.Since(startTime).String())

	return buf.Bytes(), nil
}
