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
	"time"
)

import (
	"github.com/AlexStocks/getty"
	log "github.com/AlexStocks/log4go"
)

type EchoPackageHandler struct {
}

func NewEchoPackageHandler() *EchoPackageHandler {
	return &EchoPackageHandler{}
}

func (this *EchoPackageHandler) Read(ss getty.Session, data []byte) (interface{}, int, error) {
	var (
		err error
		len int
		pkg EchoPackage
		buf *bytes.Buffer
	)

	buf = bytes.NewBuffer(data)
	len, err = pkg.Unmarshal(buf)
	if err != nil {
		if err == ErrNotEnoughSteam {
			return nil, 0, nil
		}

		return nil, 0, err
	}

	return &pkg, len, nil
}

func (this *EchoPackageHandler) Write(ss getty.Session, pkg interface{}) error {
	var (
		ok        bool
		err       error
		startTime time.Time
		echoPkg   *EchoPackage
		buf       *bytes.Buffer
	)

	startTime = time.Now()
	if echoPkg, ok = pkg.(*EchoPackage); !ok {
		log.Error("illegal pkg:%+v\n", pkg)
		return errors.New("invalid echo package!")
	}

	buf, err = echoPkg.Marshal()
	if err != nil {
		log.Warn("binary.Write(echoPkg{%#v}) = err{%#v}", echoPkg, err)
		return err
	}

	err = ss.WriteBytes(buf.Bytes())
	log.Info("WriteEchoPkgTimeMs = %s", time.Since(startTime).String())

	return err
}
