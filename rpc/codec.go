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
	"encoding/binary"
	"fmt"
	"reflect"
	"time"
	"unsafe"
)

import (
	log "github.com/AlexStocks/log4go"
	gxbytes "github.com/dubbogo/gost/bytes"
	"github.com/gogo/protobuf/proto"
	jsoniter "github.com/json-iterator/go"
	jerrors "github.com/juju/errors"
)

////////////////////////////////////////////
//  getty command
////////////////////////////////////////////

type gettyCommand int16

const (
	gettyDefaultCmd     gettyCommand = 0x00
	gettyCmdHbRequest                = 0x01
	gettyCmdHbResponse               = 0x02
	gettyCmdRPCRequest               = 0x03
	gettyCmdRPCResponse              = 0x04
)

var gettyCommandStrings = [...]string{
	"getty-default",
	"getty-heartbeat-request",
	"getty-heartbeat-response",
	"getty-request",
	"getty-response",
}

func (c gettyCommand) String() string {
	return gettyCommandStrings[c]
}

////////////////////////////////////////////
//  getty error code
////////////////////////////////////////////

type ErrorCode int16

const (
	GettyOK   ErrorCode = 0x00
	GettyFail           = 0x01
)

////////////////////////////////////////////
//  getty codec type
////////////////////////////////////////////

type CodecType int16

const (
	CodecUnknown  CodecType = 0x00
	CodecJson               = 0x01
	CodecProtobuf           = 0x02
)

var (
	gettyCodecStrings = [...]string{
		"unknown",
		"json",
		"protobuf",
	}

	Codecs = map[CodecType]Codec{
		CodecJson:     &JSONCodec{},
		CodecProtobuf: &PBCodec{},
	}
)

func (c CodecType) String() string {
	if c == CodecJson || c == CodecProtobuf {
		return gettyCodecStrings[c]
	}

	return gettyCodecStrings[CodecUnknown]
}

func (c CodecType) CheckValidity() bool {
	if c == CodecJson || c == CodecProtobuf {
		return true
	}

	return false
}

func GetCodecType(codecType string) CodecType {
	switch codecType {
	case gettyCodecStrings[CodecJson]:
		return CodecJson
	case gettyCodecStrings[CodecProtobuf]:
		return CodecProtobuf
	}

	return CodecUnknown
}

////////////////////////////////////////////
//  getty codec
////////////////////////////////////////////

type Codec interface {
	Encode(interface{}) ([]byte, error)
	Decode([]byte, interface{}) error
}

type JSONCodec struct{}

var (
	jsonstd = jsoniter.ConfigCompatibleWithStandardLibrary
)

func (c JSONCodec) Encode(i interface{}) ([]byte, error) {
	// return json.Marshal(i)
	return jsonstd.Marshal(i)
}

func (c JSONCodec) Decode(data []byte, i interface{}) error {
	// return json.Unmarshal(data, i)
	return jsonstd.Unmarshal(data, i)
}

type PBCodec struct{}

// Encode takes the protocol buffer
// and encodes it into the wire format, returning the data.
func (c PBCodec) Encode(msg interface{}) ([]byte, error) {
	// Can the object marshal itself?
	if pb, ok := msg.(proto.Marshaler); ok {
		pbBuf, err := pb.Marshal()
		return pbBuf, jerrors.Trace(err)
	}

	if pb, ok := msg.(proto.Message); ok {
		p := proto.NewBuffer(nil)
		err := p.Marshal(pb)
		return p.Bytes(), jerrors.Trace(err)
	}

	return nil, fmt.Errorf("protobuf can not marshal %T", msg)
}

// Decode parses the protocol buffer representation in buf and
// writes the decoded result to pb.  If the struct underlying pb does not match
// the data in buf, the results can be unpredictable.
//
// UnmarshalMerge merges into existing data in pb.
// Most code should use Unmarshal instead.
func (c PBCodec) Decode(buf []byte, msg interface{}) error {
	// If the object can unmarshal itself, let it.
	if u, ok := msg.(proto.Unmarshaler); ok {
		return jerrors.Trace(u.Unmarshal(buf))
	}

	if pb, ok := msg.(proto.Message); ok {
		return jerrors.Trace(proto.NewBuffer(nil).Unmarshal(pb))
	}

	return fmt.Errorf("protobuf can not unmarshal %T", msg)
}

////////////////////////////////////////////
// GettyPackageHandler
////////////////////////////////////////////

const (
	gettyPackageMagic = 0x20160905
	maxPackageLen     = 4 * 1024 * 1024
)

var (
	ErrNotEnoughStream = jerrors.New("packet stream is not enough")
	ErrTooLargePackage = jerrors.New("package length is exceed the getty package's legal maximum length.")
	ErrInvalidPackage  = jerrors.New("invalid rpc package")
	ErrIllegalMagic    = jerrors.New("package magic is not right.")
)

var (
	gettyPackageHeaderLen = (int)((uint)(unsafe.Sizeof(GettyPackageHeader{})))
)

type RPCPackage interface {
	Marshal(CodecType, *bytes.Buffer) (int, error)
	// @buf length should be equal to GettyPkg.GettyPackageHeader.Len
	Unmarshal(sz CodecType, buf *bytes.Buffer) error
	GetBody() []byte
	GetHeader() interface{}
}

type (
	MagicType     int32
	LogIDType     int64
	SequenceType  uint64
	ServiceIDType int16
	PkgLenType    int32
)

type GettyPackageHeader struct {
	Magic     MagicType     // magic number
	Command   gettyCommand  // operation command code
	ServiceID ServiceIDType // service id
	Sequence  SequenceType  // request/response sequence
	LogID     LogIDType     // log id

	Code      ErrorCode  // error code
	CodecType CodecType  // codec type
	PkgLen    PkgLenType // package body length
}

type GettyPackage struct {
	H GettyPackageHeader
	B RPCPackage
}

func (p GettyPackage) String() string {
	return fmt.Sprintf("log id:%d, sequence:%d, command:%s",
		p.H.LogID, p.H.Sequence, (gettyCommand(p.H.Command)).String())
}

func (p *GettyPackage) Marshal() (*bytes.Buffer, error) {
	var (
		err            error
		headerBuf, buf *bytes.Buffer
	)

	buf = bytes.NewBuffer(make([]byte, gettyPackageHeaderLen, gettyPackageHeaderLen<<2))
	//buf = gxbytes.GetBytesBuffer()
	//defer gxbytes.PutBytesBuffer(buf)

	// body
	if p.B != nil {
		length, err := p.B.Marshal(p.H.CodecType, buf)
		if err != nil {
			return nil, jerrors.Trace(err)
		}
		p.H.PkgLen = PkgLenType(length)
	}

	// header
	headerBuf = bytes.NewBuffer(nil)
	err = binary.Write(headerBuf, binary.LittleEndian, p.H)
	if err != nil {
		return nil, jerrors.Trace(err)
	}
	copy(buf.Bytes(), headerBuf.Bytes()[:gettyPackageHeaderLen])

	return buf, nil
}

func (p *GettyPackage) Unmarshal(buf *bytes.Buffer) (int, error) {
	bufLen := buf.Len()
	if bufLen < gettyPackageHeaderLen {
		return 0, ErrNotEnoughStream
	}

	// header
	if err := binary.Read(buf, binary.LittleEndian, &(p.H)); err != nil {
		return 0, jerrors.Trace(err)
	}

	if p.H.Magic != gettyPackageMagic {
		log.Error("@p.H.Magic{%x}, right magic{%x}", p.H.Magic, gettyPackageMagic)
		return 0, ErrIllegalMagic
	}

	totalLen := int(PkgLenType(gettyPackageHeaderLen) + p.H.PkgLen)
	if totalLen > maxPackageLen {
		return 0, ErrTooLargePackage
	}

	if bufLen >= totalLen && p.H.PkgLen != 0 {
		if err := p.B.Unmarshal(p.H.CodecType, bytes.NewBuffer(buf.Next(int(p.H.PkgLen)))); err != nil {
			return 0, jerrors.Trace(err)
		}
	}

	return totalLen, nil
}

////////////////////////////////////////////
// GettyRPCRequest
////////////////////////////////////////////

type GettyRPCHeaderLenType uint16

// // easyjson:json
// type GettyRPCRequestHeader struct {
// 	Service  string
// 	Method   string
// 	CallType gettyCallType
// }

type GettyRPCRequest struct {
	header GettyRPCRequestHeader
	body   interface{}
}

type GettyRPCRequestPackage struct {
	H          GettyPackageHeader
	header     GettyRPCRequestHeader
	service    *service
	methodType *methodType
	argv       reflect.Value
	replyv     reflect.Value
}

func NewGettyRPCRequest() RPCPackage {
	return &GettyRPCRequest{}
}

func (req *GettyRPCRequest) Marshal(sz CodecType, buf *bytes.Buffer) (int, error) {
	codec := Codecs[sz]
	if codec == nil {
		return 0, jerrors.Errorf("can not find codec for %s", sz)
	}
	headerData, err := codec.Encode(&req.header)
	if err != nil {
		return 0, jerrors.Trace(err)
	}
	bodyData, err := codec.Encode(req.body)
	if err != nil {
		return 0, jerrors.Trace(err)
	}
	err = binary.Write(buf, binary.LittleEndian, uint16(len(headerData)))
	if err != nil {
		return 0, jerrors.Trace(err)
	}
	err = binary.Write(buf, binary.LittleEndian, headerData)
	if err != nil {
		return 0, jerrors.Trace(err)
	}
	err = binary.Write(buf, binary.LittleEndian, uint16(len(bodyData)))
	if err != nil {
		return 0, jerrors.Trace(err)
	}
	err = binary.Write(buf, binary.LittleEndian, bodyData)
	if err != nil {
		return 0, jerrors.Trace(err)
	}

	return 2 + len(headerData) + 2 + len(bodyData), nil
}

func (req *GettyRPCRequest) Unmarshal(ct CodecType, buf *bytes.Buffer) error {
	var headerLen uint16
	err := binary.Read(buf, binary.LittleEndian, &headerLen)
	if err != nil {
		return jerrors.Trace(err)
	}

	var (
		headerp, bodyp *[]byte
	)
	// header := make([]byte, headerLen)
	headerp = gxbytes.GetBytes(int(headerLen))
	defer func() {
		gxbytes.PutBytes(headerp)
		gxbytes.PutBytes(bodyp)
	}()
	header := *headerp

	err = binary.Read(buf, binary.LittleEndian, header)
	if err != nil {
		return jerrors.Trace(err)
	}

	var bodyLen uint16
	err = binary.Read(buf, binary.LittleEndian, &bodyLen)
	if err != nil {
		return jerrors.Trace(err)
	}

	//body := make([]byte, bodyLen)
	bodyp = gxbytes.GetBytes(int(bodyLen))
	body := *bodyp

	err = binary.Read(buf, binary.LittleEndian, body)
	if err != nil {
		return jerrors.Trace(err)
	}

	codec := Codecs[ct]
	if codec == nil {
		return jerrors.Errorf("can not find codec for %d", ct)
	}

	err = codec.Decode(header, &req.header)
	if err != nil {
		return jerrors.Trace(err)
	}

	req.body = body
	return nil
}

func (req *GettyRPCRequest) GetBody() []byte {
	return req.body.([]byte)
}

func (req *GettyRPCRequest) GetHeader() interface{} {
	return req.header
}

////////////////////////////////////////////
// GettyRPCResponse
////////////////////////////////////////////

// type GettyRPCResponseHeader struct {
// 	Error string
// }

type GettyRPCResponse struct {
	header GettyRPCResponseHeader
	body   interface{}
}

type GettyRPCResponsePackage struct {
	H      GettyPackageHeader
	header GettyRPCResponseHeader
	body   []byte
}

func NewGettyRPCResponse() RPCPackage {
	return &GettyRPCResponse{}
}

func (resp *GettyRPCResponse) Marshal(sz CodecType, buf *bytes.Buffer) (int, error) {
	codec := Codecs[sz]
	if codec == nil {
		return 0, jerrors.Errorf("can not find codec for %d", sz)
	}

	var err error
	var headerData, bodyData []byte

	headerData, err = codec.Encode(&resp.header)
	if err != nil {
		return 0, jerrors.Trace(err)
	}

	if resp.body != nil {
		bodyData, err = codec.Encode(resp.body)
		if err != nil {
			return 0, jerrors.Trace(err)
		}
	}

	err = binary.Write(buf, binary.LittleEndian, uint16(len(headerData)))
	if err != nil {
		return 0, jerrors.Trace(err)
	}
	err = binary.Write(buf, binary.LittleEndian, headerData)
	if err != nil {
		return 0, jerrors.Trace(err)
	}
	err = binary.Write(buf, binary.LittleEndian, uint16(len(bodyData)))
	if err != nil {
		return 0, jerrors.Trace(err)
	}
	err = binary.Write(buf, binary.LittleEndian, bodyData)
	if err != nil {
		return 0, jerrors.Trace(err)
	}

	return 2 + len(headerData) + 2 + len(bodyData), nil
}

func (resp *GettyRPCResponse) Unmarshal(sz CodecType, buf *bytes.Buffer) error {
	var headerLen uint16
	err := binary.Read(buf, binary.LittleEndian, &headerLen)
	if err != nil {
		return jerrors.Trace(err)
	}

	var (
		headerp, bodyp *[]byte
	)
	// header := make([]byte, headerLen)
	headerp = gxbytes.GetBytes(int(headerLen))
	defer func() {
		gxbytes.PutBytes(headerp)
		gxbytes.PutBytes(bodyp)
	}()
	header := *headerp

	err = binary.Read(buf, binary.LittleEndian, header)
	if err != nil {
		return jerrors.Trace(err)
	}

	var bodyLen uint16
	err = binary.Read(buf, binary.LittleEndian, &bodyLen)
	if err != nil {
		return jerrors.Trace(err)
	}

	//body := make([]byte, bodyLen)
	bodyp = gxbytes.GetBytes(int(bodyLen))
	body := *bodyp

	err = binary.Read(buf, binary.LittleEndian, body)
	if err != nil {
		return jerrors.Trace(err)
	}

	codec := Codecs[sz]
	if codec == nil {
		return jerrors.Errorf("can not find codec for %d", sz)
	}

	err = codec.Decode(header, &resp.header)
	if err != nil {
		return jerrors.Trace(err)
	}

	resp.body = body
	return nil
}

func (resp *GettyRPCResponse) GetBody() []byte {
	return resp.body.([]byte)
}

func (resp *GettyRPCResponse) GetHeader() interface{} {
	return resp.header
}

////////////////////////////////////////////
// PendingResponse
////////////////////////////////////////////

type PendingResponse struct {
	seq       SequenceType
	err       error
	start     time.Time
	readStart time.Time
	callback  AsyncCallback
	reply     interface{}
	opts      CallOptions
	done      chan struct{}
}

func NewPendingResponse() *PendingResponse {
	return &PendingResponse{
		start: time.Now(),
		done:  make(chan struct{}),
	}
}

func (r PendingResponse) GetCallResponse() CallResponse {
	return CallResponse{
		Opts:      r.opts,
		Cause:     r.err,
		Start:     r.start,
		ReadStart: r.readStart,
		Reply:     r.reply,
	}
}
