package rpc

import (
	"bytes"
)

import (
	"github.com/AlexStocks/getty"
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

type RpcServerPacketHandler struct {
	server *Server
}

func NewRpcServerPacketHandler(server *Server) *RpcServerPacketHandler {
	return &RpcServerPacketHandler{
		server: server,
	}
}

func (p *RpcServerPacketHandler) Read(ss getty.Session, data []byte) (interface{}, int, error) {
	var (
		err error
		len int
		buf *bytes.Buffer
	)

	buf = bytes.NewBuffer(data)
	req := NewRpcRequest(p.server)
	len, err = req.Unmarshal(buf)
	if err != nil {
		if err == ErrNotEnoughStream {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	return req, len, nil
}

func (p *RpcServerPacketHandler) Write(ss getty.Session, pkg interface{}) error {
	var (
		ok   bool
		err  error
		resp *RpcResponse
		buf  *bytes.Buffer
	)

	if resp, ok = pkg.(*RpcResponse); !ok {
		log.Error("illegal pkg:%+v\n", pkg)
		return jerrors.New("invalid rpc response")
	}

	buf, err = resp.Marshal()
	if err != nil {
		log.Warn("binary.Write(resp{%#v}) = err{%#v}", resp, err)
		return err
	}

	err = ss.WriteBytes(buf.Bytes())

	return err
}

type RpcClientPacketHandler struct {
}

func NewRpcClientPacketHandler() *RpcClientPacketHandler {
	return &RpcClientPacketHandler{}
}

func (p *RpcClientPacketHandler) Read(ss getty.Session, data []byte) (interface{}, int, error) {
	var (
		err error
		len int
		buf *bytes.Buffer
	)

	buf = bytes.NewBuffer(data)
	resp := NewRpcResponse()
	len, err = resp.Unmarshal(buf)
	if err != nil {
		if err == ErrNotEnoughStream {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	return resp, len, nil
}

func (p *RpcClientPacketHandler) Write(ss getty.Session, pkg interface{}) error {
	var (
		ok  bool
		err error
		req *RpcRequest
		buf *bytes.Buffer
	)

	if req, ok = pkg.(*RpcRequest); !ok {
		log.Error("illegal pkg:%+v\n", pkg)
		return jerrors.New("invalid rpc request")
	}

	buf, err = req.Marshal()
	if err != nil {
		log.Warn("binary.Write(req{%#v}) = err{%#v}", req, err)
		return err
	}

	err = ss.WriteBytes(buf.Bytes())

	return err
}
