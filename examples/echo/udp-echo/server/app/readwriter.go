/******************************************************
# DESC    : echo stream parser
# AUTHOR  : Alex Stocks
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2016-09-04 13:08
# FILE    : readwriter.go
******************************************************/

package main

import (
	"bytes"
	"errors"
	"fmt"
	"time"
)

import (
	"github.com/AlexStocks/getty"
	log "github.com/AlexStocks/log4go"
)

var (
	echoPkgHandler = NewEchoPackageHandler()
)

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
		log.Error("illegal UDPContext{%#v}", udpCtx)
		return nil, fmt.Errorf("illegal @udpCtx{%#v}", udpCtx)
	}

	startTime = time.Now()
	if echoPkg, ok = ctx.Pkg.(*EchoPackage); !ok {
		log.Error("illegal pkg:%+v, addr:%s\n", ctx.Pkg, ctx.PeerAddr)
		return nil, errors.New("invalid echo package!")
	}

	buf, err = echoPkg.Marshal()
	if err != nil {
		log.Warn("binary.Write(echoPkg{%#v}) = err{%#v}", echoPkg, err)
		return nil, err
	}

	log.Debug("WriteEchoPkgTimeMs = %s", time.Since(startTime).String())

	return buf.Bytes(), nil
}
