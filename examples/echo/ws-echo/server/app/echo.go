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
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"
)

import (
	log "github.com/AlexStocks/log4go"
)

////////////////////////////////////////////
//  echo command
////////////////////////////////////////////

type echoCommand uint32

const (
	echoCmd = iota
)

var echoCommandStrings = [...]string{
	"echo",
}

func (c echoCommand) String() string {
	return echoCommandStrings[c]
}

////////////////////////////////////////////
// EchoPkgHandler
////////////////////////////////////////////

const (
	echoPkgMagic     = 0x20160905
	maxEchoStringLen = 0xff
)

var (
	ErrNotEnoughStream = errors.New("packet stream is not enough")
	ErrTooLargePackage = errors.New("package length is exceed the echo package's legal maximum length.")
	ErrIllegalMagic    = errors.New("package magic is not right.")
)

var (
	echoPkgHeaderLen int
)

func init() {
	echoPkgHeaderLen = (int)((uint)(unsafe.Sizeof(EchoPkgHeader{})))
}

type EchoPkgHeader struct {
	Magic uint32
	LogID uint32 // log id

	Sequence  uint32 // request/response sequence
	ServiceID uint32 // service id

	Command uint32 // operation command code
	Code    int32  // error code

	Len uint16 // body length
	_   uint16
	_   int32 // reserved, maybe used as package md5 checksum
}

type EchoPackage struct {
	H EchoPkgHeader
	B string
}

func (p EchoPackage) String() string {
	return fmt.Sprintf("log id:%d, sequence:%d, command:%s, echo string:%s",
		p.H.LogID, p.H.Sequence, (echoCommand(p.H.Command)).String(), p.B)
}

func (p EchoPackage) Marshal() (*bytes.Buffer, error) {
	var (
		err error
		buf *bytes.Buffer
	)

	buf = &bytes.Buffer{}
	err = binary.Write(buf, binary.LittleEndian, p.H)
	if err != nil {
		return nil, err
	}
	buf.WriteByte((byte)(len(p.B)))
	buf.WriteString(p.B)

	return buf, nil
}

func (p *EchoPackage) Unmarshal(buf *bytes.Buffer) (int, error) {
	var (
		err error
		len byte
	)

	if buf.Len() < echoPkgHeaderLen {
		return 0, ErrNotEnoughStream
	}

	// header
	err = binary.Read(buf, binary.LittleEndian, &(p.H))
	if err != nil {
		return 0, err
	}
	if p.H.Magic != echoPkgMagic {
		log.Error("@p.H.Magic{%x}, right magic{%x}", p.H.Magic, echoPkgMagic)
		return 0, ErrIllegalMagic
	}
	if buf.Len() < (int)(p.H.Len) {
		return 0, ErrNotEnoughStream
	}
	// 防止恶意客户端把这个字段设置过大导致服务端死等或者服务端在准备对应的缓冲区时内存崩溃
	if maxEchoStringLen < p.H.Len-1 {
		return 0, ErrTooLargePackage
	}

	len, err = buf.ReadByte()
	if err != nil {
		return 0, nil
	}
	p.B = (string)(buf.Next((int)(len)))

	return (int)(p.H.Len) + echoPkgHeaderLen, nil
}
