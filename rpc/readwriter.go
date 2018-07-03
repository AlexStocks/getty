package rpc

import (
	"bytes"
)

import (
	"github.com/AlexStocks/getty"
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
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
	var (
		err    error
		length int
		pkg    GettyPackage
		buf    *bytes.Buffer
	)

	buf = bytes.NewBuffer(data)
	length, err = pkg.Unmarshal(buf)
	if err != nil {
		if jerrors.Cause(err) == ErrNotEnoughStream {
			return nil, 0, nil
		}
		return nil, 0, jerrors.Trace(err)
	}

	return &pkg, length, nil
}

func (p *RpcServerPackageHandler) Write(ss getty.Session, pkg interface{}) error {
	var (
		ok   bool
		err  error
		resp *GettyPackage
		buf  *bytes.Buffer
	)

	if resp, ok = pkg.(*GettyPackage); !ok {
		log.Error("illegal pkg:%+v\n", pkg)
		return jerrors.New("invalid rpc response")
	}

	buf, err = resp.Marshal()
	if err != nil {
		log.Warn("binary.Write(resp{%#v}) = err{%#v}", resp, err)
		return jerrors.Trace(err)
	}

	return jerrors.Trace(ss.WriteBytes(buf.Bytes()))
}

////////////////////////////////////////////
// RpcClientPackageHandler
////////////////////////////////////////////

type RpcClientPackageHandler struct {
}

func NewRpcClientPackageHandler() *RpcClientPackageHandler {
	return &RpcClientPackageHandler{}
}

func (p *RpcClientPackageHandler) Read(ss getty.Session, data []byte) (interface{}, int, error) {
	var (
		err    error
		length int
		pkg    GettyPackage
		buf    *bytes.Buffer
	)

	buf = bytes.NewBuffer(data)
	length, err = pkg.Unmarshal(buf)
	if err != nil {
		if err == ErrNotEnoughStream {
			return nil, 0, nil
		}
		return nil, 0, jerrors.Trace(err)
	}

	return &pkg, length, nil
}

func (p *RpcClientPackageHandler) Write(ss getty.Session, pkg interface{}) error {
	var (
		ok  bool
		err error
		req *GettyPackage
		buf *bytes.Buffer
	)

	if req, ok = pkg.(*GettyPackage); !ok {
		log.Error("illegal pkg:%+v\n", pkg)
		return jerrors.New("invalid rpc request")
	}

	buf, err = req.Marshal()
	if err != nil {
		log.Warn("binary.Write(req{%#v}) = err{%#v}", req, err)
		return jerrors.Trace(err)
	}

	return jerrors.Trace(ss.WriteBytes(buf.Bytes()))
}
