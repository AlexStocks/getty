/******************************************************
# MAINTAINER : wongoo
# LICENCE    : Apache License 2.0
# EMAIL      : gelnyang@163.com
# MOD        : 2019-06-11
******************************************************/

package hello

import (
	"errors"
)

import (
	"github.com/dubbogo/getty"
)

type PackageHandler struct{}

func (h *PackageHandler) Read(ss getty.Session, data []byte) (interface{}, int, error) {
	s := string(data)
	return s, len(s), nil
}

func (h *PackageHandler) Write(ss getty.Session, pkg interface{}) ([]byte, error) {
	s, ok := pkg.(string)
	if !ok {
		log.Infof("illegal pkg:%+v", pkg)
		return nil, errors.New("invalid package")
	}

	return []byte(s), nil
}
