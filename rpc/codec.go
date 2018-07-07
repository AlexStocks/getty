package rpc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"
	"unsafe"
)

import (
	proto "github.com/gogo/protobuf/proto"
	pb "github.com/golang/protobuf/proto"
	jerrors "github.com/juju/errors"
)

import (
	log "github.com/AlexStocks/log4go"
)

////////////////////////////////////////////
//  getty command
////////////////////////////////////////////

type gettyCodecType uint32

const (
	gettyCodecUnknown gettyCodecType = 0x00
	gettyJson                        = 0x01
	gettyProtobuf                    = 0x02
)

var gettyCodecTypeStrings = [...]string{
	"unknown",
	"json",
	"protobuf",
}

func (c gettyCodecType) String() string {
	if c == gettyJson || c == gettyProtobuf {
		return gettyCodecTypeStrings[c]
	}

	return gettyCodecTypeStrings[gettyCodecUnknown]
}

func String2CodecType(codecType string) gettyCodecType {
	switch codecType {
	case gettyCodecTypeStrings[gettyJson]:
		return gettyJson
	case gettyCodecTypeStrings[gettyProtobuf]:
		return gettyProtobuf
	}

	return gettyCodecUnknown
}

////////////////////////////////////////////
//  getty command
////////////////////////////////////////////

type gettyCommand uint32

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
//  getty call type
////////////////////////////////////////////

type gettyCallType uint32

const (
	gettyOneWay        gettyCallType = 0x01
	gettyTwoWay                      = 0x02
	gettyTwoWayNoReply               = 0x03
)

////////////////////////////////////////////
//  getty error code
////////////////////////////////////////////

type GettyErrorCode int32

const (
	GettyOK   GettyErrorCode = 0x00
	GettyFail                = 0x01
)

type SerializeType byte

const (
	JSON SerializeType = iota
	ProtoBuffer
)

var (
	Codecs = map[SerializeType]Codec{
		JSON:        &JSONCodec{},
		ProtoBuffer: &PBCodec{},
	}
)

// Codec defines the interface that decode/encode body.
type Codec interface {
	Encode(i interface{}) ([]byte, error)
	Decode(data []byte, i interface{}) error
}

// JSONCodec uses json marshaler and unmarshaler.
type JSONCodec struct{}

// Encode encodes an object into slice of bytes.
func (c JSONCodec) Encode(i interface{}) ([]byte, error) {
	return json.Marshal(i)
}

// Decode decodes an object from slice of bytes.
func (c JSONCodec) Decode(data []byte, i interface{}) error {
	return json.Unmarshal(data, i)
}

// PBCodec uses protobuf marshaler and unmarshaler.
type PBCodec struct{}

// Encode encodes an object into slice of bytes.
func (c PBCodec) Encode(i interface{}) ([]byte, error) {
	if m, ok := i.(proto.Marshaler); ok {
		return m.Marshal()
	}

	if m, ok := i.(pb.Message); ok {
		return pb.Marshal(m)
	}

	return nil, fmt.Errorf("%T is not a proto.Marshaler", i)
}

// Decode decodes an object from slice of bytes.
func (c PBCodec) Decode(data []byte, i interface{}) error {
	if m, ok := i.(proto.Unmarshaler); ok {
		return m.Unmarshal(data)
	}

	if m, ok := i.(pb.Message); ok {
		return pb.Unmarshal(data, m)
	}

	return fmt.Errorf("%T is not a proto.Unmarshaler", i)
}

////////////////////////////////////////////
// GettyPackageHandler
////////////////////////////////////////////

const (
	gettyPackageMagic        = 0x20160905
	maxPackageLen            = 1024 * 1024
	rpcPackagePlaceholderLen = 2
)

var (
	ErrNotEnoughStream         = jerrors.New("packet stream is not enough")
	ErrTooLargePackage         = jerrors.New("package length is exceed the getty package's legal maximum length.")
	ErrInvalidPackage          = jerrors.New("invalid rpc package")
	ErrNotFoundServiceOrMethod = jerrors.New("server invalid service or method")
	ErrIllegalMagic            = jerrors.New("package magic is not right.")
)

var (
	gettyPackageHeaderLen int
)

func init() {
	gettyPackageHeaderLen = (int)((uint)(unsafe.Sizeof(GettyPackageHeader{})))
}

type RPCPackage interface {
	Marshal(SerializeType, *bytes.Buffer) (int, error)
	// @buf length should be equal to GettyPkg.GettyPackageHeader.Len
	Unmarshal(sz SerializeType, buf *bytes.Buffer) error
	GetBody() []byte
	GetHeader() interface{}
}

type GettyPackageHeader struct {
	Magic    uint32 // magic number
	LogID    uint32 // log id
	Sequence uint64 // request/response sequence

	Command gettyCommand   // operation command code
	Code    GettyErrorCode // error code

	ServiceID uint32 // service id
	CodecType SerializeType
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
		err             error
		packLen, length int
		buf             *bytes.Buffer
	)

	packLen = gettyPackageHeaderLen
	if p.B != nil {
		buf = &bytes.Buffer{}
		length, err = p.B.Marshal(p.H.CodecType, buf)
		if err != nil {
			return nil, jerrors.Trace(err)
		}
		packLen = gettyPackageHeaderLen + length
	}
	buf0 := &bytes.Buffer{}
	err = binary.Write(buf0, binary.LittleEndian, uint16(packLen))
	if err != nil {
		return nil, jerrors.Trace(err)
	}
	err = binary.Write(buf0, binary.LittleEndian, p.H)
	if err != nil {
		return nil, jerrors.Trace(err)
	}
	if p.B != nil {
		if err = binary.Write(buf0, binary.LittleEndian, buf.Bytes()); err != nil {
			return nil, jerrors.Trace(err)
		}
	}
	return buf0, nil
}

func (p *GettyPackage) Unmarshal(buf *bytes.Buffer) (int, error) {
	var err error
	if buf.Len() < rpcPackagePlaceholderLen+gettyPackageHeaderLen {
		return 0, ErrNotEnoughStream
	}
	var packLen uint16
	err = binary.Read(buf, binary.LittleEndian, &packLen)
	if err != nil {
		return 0, jerrors.Trace(err)
	}
	if int(packLen) > maxPackageLen {
		return 0, ErrTooLargePackage
	}
	if int(packLen) < gettyPackageHeaderLen {
		return 0, ErrInvalidPackage
	}

	// header
	if err := binary.Read(buf, binary.LittleEndian, &(p.H)); err != nil {
		return 0, jerrors.Trace(err)
	}
	if p.H.Magic != gettyPackageMagic {
		log.Error("@p.H.Magic{%x}, right magic{%x}", p.H.Magic, gettyPackageMagic)
		return 0, ErrIllegalMagic
	}

	if int(packLen) > rpcPackagePlaceholderLen+gettyPackageHeaderLen {
		if err := p.B.Unmarshal(p.H.CodecType, bytes.NewBuffer(buf.Next(int(packLen)-gettyPackageHeaderLen))); err != nil {
			return 0, jerrors.Trace(err)
		}
	}

	return int(packLen), nil
}

////////////////////////////////////////////
// GettyRPCRequest
////////////////////////////////////////////

type GettyRPCHeaderLenType uint16

//easyjson:json
type GettyRPCRequestHeader struct {
	Service  string
	Method   string
	CallType gettyCallType
}

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

func (req *GettyRPCRequest) Marshal(sz SerializeType, buf *bytes.Buffer) (int, error) {
	codec := Codecs[sz]
	if codec == nil {
		return 0, jerrors.Errorf("can not find codec for %d", sz)
	}
	headerData, err := codec.Encode(req.header)
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

func (req *GettyRPCRequest) Unmarshal(sz SerializeType, buf *bytes.Buffer) error {

	var headerLen uint16
	err := binary.Read(buf, binary.LittleEndian, &headerLen)
	if err != nil {
		return jerrors.Trace(err)
	}

	header := make([]byte, headerLen)
	err = binary.Read(buf, binary.LittleEndian, header)
	if err != nil {
		return jerrors.Trace(err)
	}

	var bodyLen uint16
	err = binary.Read(buf, binary.LittleEndian, &bodyLen)
	if err != nil {
		return jerrors.Trace(err)
	}

	body := make([]byte, bodyLen)
	err = binary.Read(buf, binary.LittleEndian, body)
	if err != nil {
		return jerrors.Trace(err)
	}

	codec := Codecs[sz]
	if codec == nil {
		return jerrors.Errorf("can not find codec for %d", sz)
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

type GettyRPCResponseHeader struct {
	Error string
}

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

func (resp *GettyRPCResponse) Marshal(sz SerializeType, buf *bytes.Buffer) (int, error) {
	codec := Codecs[sz]
	if codec == nil {
		return 0, jerrors.Errorf("can not find codec for %d", sz)
	}
	headerData, err := codec.Encode(resp.header)
	if err != nil {
		return 0, jerrors.Trace(err)
	}

	bodyData, err := codec.Encode(resp.body)
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

func (resp *GettyRPCResponse) Unmarshal(sz SerializeType, buf *bytes.Buffer) error {

	var headerLen uint16
	err := binary.Read(buf, binary.LittleEndian, &headerLen)
	if err != nil {
		return jerrors.Trace(err)
	}

	header := make([]byte, headerLen)
	err = binary.Read(buf, binary.LittleEndian, header)
	if err != nil {
		return jerrors.Trace(err)
	}

	var bodyLen uint16
	err = binary.Read(buf, binary.LittleEndian, &bodyLen)
	if err != nil {
		return jerrors.Trace(err)
	}

	body := make([]byte, bodyLen)
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
	seq   uint64
	err   error
	reply interface{}
	done  chan struct{}
}

func NewPendingResponse() *PendingResponse {
	return &PendingResponse{done: make(chan struct{})}
}
