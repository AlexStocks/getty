package rpc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"reflect"
)

import (
	jerrors "github.com/juju/errors"
)

const (
	MaxPacketLen          = 16 * 1024
	RequestSendOnly int16 = 1

	ReplyTypeData = 0x01
	ReplyTypePong = 0x10
	ReplyTypeAck  = 0x100
)

var (
	ErrNotEnoughStream         = jerrors.New("packet stream is not enough")
	ErrTooLargePackage         = jerrors.New("package length is exceed the echo package's legal maximum length.")
	ErrNotFoundServiceOrMethod = jerrors.New("server invalid service or method")
)

type RequestHeader struct {
	Seq      uint64
	Service  string
	Method   string
	CallType int16
}

func NewRequestHeader() *RequestHeader {
	return &RequestHeader{}
}

func (reqHeader *RequestHeader) IsPing() bool {
	if reqHeader.Service == "go" && reqHeader.Method == "ping" {
		return true
	}
	return false
}

type RpcRequest struct {
	server     *Server
	header     *RequestHeader
	body       interface{}
	service    *service
	methodType *methodType
	argv       reflect.Value
	replyv     reflect.Value
}

func NewRpcRequest(server *Server) *RpcRequest {
	return &RpcRequest{
		server: server,
		header: NewRequestHeader(),
	}
}

func (req *RpcRequest) Marshal() (*bytes.Buffer, error) {
	var err error
	var buf *bytes.Buffer
	buf = &bytes.Buffer{}

	headerData, err := json.Marshal(req.header)
	if err != nil {
		return nil, err
	}

	bodyData, err := json.Marshal(req.body)
	if err != nil {
		return nil, err
	}

	//前2字节总长度，header长度2字节+header数据，body长度2字节+body数据
	packLen := 2 + 2 + len(headerData) + 2 + len(bodyData)
	err = binary.Write(buf, binary.LittleEndian, uint16(packLen))
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, uint16(len(headerData)))
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, headerData)
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, uint16(len(bodyData)))
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, bodyData)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func (req *RpcRequest) Unmarshal(buf *bytes.Buffer) (int, error) {
	var err error
	if buf.Len() < 7 {
		return 0, ErrNotEnoughStream
	}
	var packLen uint16
	err = binary.Read(buf, binary.LittleEndian, &packLen)
	if err != nil {
		return 0, err
	}
	if packLen > MaxPacketLen {
		return 0, ErrTooLargePackage
	}
	var headerLen uint16
	err = binary.Read(buf, binary.LittleEndian, &headerLen)
	if err != nil {
		return 0, err
	}
	header := make([]byte, headerLen)
	err = binary.Read(buf, binary.LittleEndian, header)
	if err != nil {
		return 0, err
	}
	var bodyLen uint16
	err = binary.Read(buf, binary.LittleEndian, &bodyLen)
	if err != nil {
		return 0, err
	}
	body := make([]byte, bodyLen)
	err = binary.Read(buf, binary.LittleEndian, body)
	if err != nil {
		return 0, err
	}
	err = json.Unmarshal(header, req.header)
	if err != nil {
		return 0, err
	}

	if req.header.IsPing() {
		return int(packLen), nil
	}
	req.service = req.server.serviceMap[req.header.Service]
	if req.service != nil {
		req.methodType = req.service.method[req.header.Method]
	}
	if req.service == nil || req.methodType == nil {
		return 0, ErrNotFoundServiceOrMethod
	}

	argIsValue := false
	if req.methodType.ArgType.Kind() == reflect.Ptr {
		req.argv = reflect.New(req.methodType.ArgType.Elem())
	} else {
		req.argv = reflect.New(req.methodType.ArgType)
		argIsValue = true
	}
	err = json.Unmarshal(body, req.argv.Interface())
	if err != nil {
		return 0, err
	}
	if argIsValue {
		req.argv = req.argv.Elem()
	}
	req.replyv = reflect.New(req.methodType.ReplyType.Elem())

	return int(packLen), nil
}

type ResponseHeader struct {
	Seq       uint64
	ReplyType int16
	Error     string
}

func NewResponseHeader() *ResponseHeader {
	return &ResponseHeader{}
}

type RpcResponse struct {
	header *ResponseHeader
	body   interface{}
}

func NewRpcResponse() *RpcResponse {
	r := &RpcResponse{
		header: NewResponseHeader(),
	}
	return r
}

func (resp *RpcResponse) Marshal() (*bytes.Buffer, error) {
	var err error
	var buf *bytes.Buffer
	buf = &bytes.Buffer{}

	headerData, err := json.Marshal(resp.header)
	if err != nil {
		return nil, err
	}

	bodyData, err := json.Marshal(resp.body)
	if err != nil {
		return nil, err
	}

	//前2字节总长度，header长度2字节+header数据，body长度2字节+body数据
	packLen := 2 + 2 + len(headerData) + 2 + len(bodyData)
	err = binary.Write(buf, binary.LittleEndian, uint16(packLen))
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, uint16(len(headerData)))
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, headerData)
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, uint16(len(bodyData)))
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, bodyData)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func (resp *RpcResponse) Unmarshal(buf *bytes.Buffer) (int, error) {
	var err error
	if buf.Len() < 7 {
		return 0, ErrNotEnoughStream
	}
	var packLen uint16
	err = binary.Read(buf, binary.LittleEndian, &packLen)
	if err != nil {
		return 0, err
	}
	if packLen > MaxPacketLen {
		return 0, ErrTooLargePackage
	}
	var headerLen uint16
	err = binary.Read(buf, binary.LittleEndian, &headerLen)
	if err != nil {
		return 0, err
	}
	header := make([]byte, headerLen)
	err = binary.Read(buf, binary.LittleEndian, header)
	if err != nil {
		return 0, err
	}
	var bodyLen uint16
	err = binary.Read(buf, binary.LittleEndian, &bodyLen)
	if err != nil {
		return 0, err
	}
	body := make([]byte, bodyLen)
	err = binary.Read(buf, binary.LittleEndian, body)
	if err != nil {
		return 0, err
	}
	resp.body = body
	err = json.Unmarshal(header, resp.header)
	if err != nil {
		return 0, err
	}
	// err = json.Unmarshal(body, resp.body)
	// if err != nil {
	// 	return 0, err
	// }
	return int(packLen), nil
}

type PendingResponse struct {
	seq   uint64
	err   error
	reply interface{}
	done  chan struct{}
}

func NewPendingResponse() *PendingResponse {
	return &PendingResponse{done: make(chan struct{})}
}
