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
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

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

////////////////////////////////////////////
// GettyPackageHandler
////////////////////////////////////////////

const (
	gettyPackageMagic = 0x20160905
	maxPackageLen     = 1024 * 1024
)

var (
	ErrNotEnoughStream         = jerrors.New("packet stream is not enough")
	ErrTooLargePackage         = jerrors.New("package length is exceed the getty package's legal maximum length.")
	ErrNotFoundServiceOrMethod = jerrors.New("server invalid service or method")
	ErrIllegalMagic            = jerrors.New("package magic is not right.")
)

var (
	gettyPackageHeaderLen  int
	gettyRPCRequestMinLen  int
	gettyRPCResponseMinLen int
)

func init() {
	gettyPackageHeaderLen = (int)((uint)(unsafe.Sizeof(GettyPackageHeader{})))
	gettyRPCRequestMinLen = (int)((uint)(unsafe.Sizeof(GettyRPCRequestHeader{}))) + 2
	gettyRPCResponseMinLen = (int)((uint)(unsafe.Sizeof(GettyRPCResponseHeader{}))) + 2
}

type RPCPackage interface {
	Marshal(*bytes.Buffer) error
	// @buf length should be equal to GettyPkg.GettyPackageHeader.Len
	Unmarshal(buf *bytes.Buffer) error
}

type GettyPackageHeader struct {
	Magic    uint32 // magic number
	LogID    uint32 // log id
	Sequence uint64 // request/response sequence

	Command gettyCommand   // operation command code
	Code    GettyErrorCode // error code

	ServiceID uint32 // service id
	Len       uint32 // body length
}

type GettyPackage struct {
	H GettyPackageHeader
	B RPCPackage
}

func (p GettyPackage) String() string {
	return fmt.Sprintf("log id:%d, sequence:%d, command:%s",
		p.H.LogID, p.H.Sequence, (gettyCommand(p.H.Command)).String())
}

func (p GettyPackage) Marshal() (*bytes.Buffer, error) {
	var (
		err          error
		length, size int
		buf, buf0    *bytes.Buffer
	)

	buf = &bytes.Buffer{}
	err = binary.Write(buf, binary.LittleEndian, p.H)
	if err != nil {
		return nil, jerrors.Trace(err)
	}
	if p.B != nil {
		if err = p.B.Marshal(buf); err != nil {
			return nil, jerrors.Trace(err)
		}

		// body length
		length = buf.Len() - gettyPackageHeaderLen
		size = (int)((uint)(unsafe.Sizeof(p.H.Len)))
		buf0 = bytes.NewBuffer(buf.Bytes()[gettyPackageHeaderLen-size : size])
		binary.Write(buf0, binary.LittleEndian, length)
	}

	return buf, nil
}

func (p *GettyPackage) Unmarshal(buf *bytes.Buffer) (int, error) {
	if buf.Len() < gettyPackageHeaderLen {
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
	if buf.Len() < (int)(p.H.Len) {
		return 0, ErrNotEnoughStream
	}
	if maxPackageLen < p.H.Len {
		return 0, ErrTooLargePackage
	}

	if p.H.Len != 0 {
		if err := p.B.Unmarshal(bytes.NewBuffer(buf.Next(int(p.H.Len)))); err != nil {
			return 0, jerrors.Trace(err)
		}
	}

	return (int)(p.H.Len) + gettyPackageHeaderLen, nil
}

////////////////////////////////////////////
// GettyRPCRequest
////////////////////////////////////////////

type GettyRPCHeaderLenType uint16

//easyjson:json
type GettyRPCRequestHeader struct {
	Service  string        `json:"service,omitempty"`
	Method   string        `json:"method,omitempty"`
	CallType gettyCallType `json:"call_type,omitempty"`
}

type GettyRPCRequest struct {
	server     *Server
	header     GettyRPCRequestHeader
	body       interface{}
	service    *service
	methodType *methodType
	argv       reflect.Value
	replyv     reflect.Value
}

// json rpc stream format
// |-- 2B (GettyRPCRequestHeader length) --|-- GettyRPCRequestHeader --|-- rpc body --|

func NewGettyRPCRequest(server *Server) *GettyRPCRequest {
	return &GettyRPCRequest{
		server: server,
	}
}

func (req *GettyRPCRequest) Marshal(buf *bytes.Buffer) error {
	headerData, err := req.header.MarshalJSON()
	if err != nil {
		return jerrors.Trace(err)
	}

	bodyData, err := json.Marshal(req.body)
	if err != nil {
		return jerrors.Trace(err)
	}

	err = binary.Write(buf, binary.LittleEndian, uint16(len(headerData)))
	if err != nil {
		return jerrors.Trace(err)
	}
	err = binary.Write(buf, binary.LittleEndian, headerData)
	if err != nil {
		return jerrors.Trace(err)
	}
	err = binary.Write(buf, binary.LittleEndian, bodyData)
	if err != nil {
		return jerrors.Trace(err)
	}

	return nil
}

// @buf length should be equal to GettyPkg.GettyPackageHeader.Len
func (req *GettyRPCRequest) Unmarshal(buf *bytes.Buffer) error {
	if buf.Len() < gettyRPCRequestMinLen {
		return ErrNotEnoughStream
	}

	var headerLen uint16
	err := binary.Read(buf, binary.LittleEndian, &headerLen)
	if err != nil {
		return jerrors.Trace(err)
	}

	header := buf.Next(int(headerLen))
	body := buf.Next(buf.Len())
	err = (&req.header).UnmarshalJSON(header)
	if err != nil {
		return jerrors.Trace(err)
	}

	// get service & method
	req.service = req.server.serviceMap[req.header.Service]
	if req.service != nil {
		req.methodType = req.service.method[req.header.Method]
	}
	if req.service == nil || req.methodType == nil {
		return ErrNotFoundServiceOrMethod
	}

	// get args
	argIsValue := false
	if req.methodType.ArgType.Kind() == reflect.Ptr {
		req.argv = reflect.New(req.methodType.ArgType.Elem())
	} else {
		req.argv = reflect.New(req.methodType.ArgType)
		argIsValue = true
	}
	err = json.Unmarshal(body, req.argv.Interface())
	if err != nil {
		return jerrors.Trace(err)
	}
	if argIsValue {
		req.argv = req.argv.Elem()
	}
	// get reply
	req.replyv = reflect.New(req.methodType.ReplyType.Elem())

	return nil
}

////////////////////////////////////////////
// GettyRPCResponse
////////////////////////////////////////////

type GettyRPCResponseHeader struct {
	Error string `json:"error,omitempty"` // error string
}

type GettyRPCResponse struct {
	header GettyRPCResponseHeader `json:"header,omitempty"`
	body   interface{}            `json:"body,omitempty"`
}

func (resp *GettyRPCResponse) Marshal(buf *bytes.Buffer) error {
	headerData, err := json.Marshal(resp.header)
	if err != nil {
		return jerrors.Trace(err)
	}

	bodyData, err := json.Marshal(resp.body)
	if err != nil {
		return jerrors.Trace(err)
	}

	err = binary.Write(buf, binary.LittleEndian, (GettyRPCHeaderLenType)(len(headerData)))
	if err != nil {
		return jerrors.Trace(err)
	}
	if _, err = buf.Write(headerData); err != nil {
		return jerrors.Trace(err)
	}
	if _, err = buf.Write(bodyData); err != nil {
		return jerrors.Trace(err)
	}

	return nil
}

// @buf length should be equal to GettyPkg.GettyPackageHeader.Len
func (resp *GettyRPCResponse) Unmarshal(buf *bytes.Buffer) error {
	if buf.Len() < gettyRPCResponseMinLen {
		return ErrNotEnoughStream
	}

	var headerLen GettyRPCHeaderLenType
	err := binary.Read(buf, binary.LittleEndian, &headerLen)
	if err != nil {
		return jerrors.Trace(err)
	}

	header := buf.Next(int(headerLen))
	if len(header) != int(headerLen) {
		return ErrNotEnoughStream
	}
	resp.body = buf.Next(int(buf.Len()))
	err = json.Unmarshal(header, resp.header)
	if err != nil {
		return jerrors.Trace(err)
	}

	return nil
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
