/******************************************************
# DESC    : echo stream parser
# AUTHOR  : Alex Stocks
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2016-09-04 13:08
# FILE    : readwriter.go
******************************************************/

package getty

import (
	"bytes"
	"errors"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
)

var (
	ErrNotEnoughStream = errors.New("packet stream is not enough")
)

type EchoPackageHandler struct {
	pkg ProtoPackage
}

func NewEchoPackageHandler(pkg ProtoPackage) *EchoPackageHandler {
	return &EchoPackageHandler{pkg}
}

func (h *EchoPackageHandler) Read(ss Session, data []byte) (ProtoPackage, int, error) {
	var (
		err error
		len int
		buf *bytes.Buffer
	)

	buf = bytes.NewBuffer(data)
	len, err = h.pkg.Unmarshal(buf)
	if err != nil {
		if err == ErrNotEnoughStream {
			return nil, 0, nil
		}

		return nil, 0, err
	}

	return h.pkg, len, nil
}

func (h *EchoPackageHandler) Write(ss Session, pkg ProtoPackage) error {
	var (
		err       error
		startTime time.Time
		buf       *bytes.Buffer
	)

	startTime = time.Now()

	buf, err = pkg.Marshal()
	if err != nil {
		log.Warn("binary.Write(echoPkg{%#v}) = err{%#v}", pkg, err)
		return err
	}

	err = ss.WriteBytes(buf.Bytes())
	log.Info("WriteEchoPkgTimeMs = %s", time.Since(startTime).String())

	return err
}
