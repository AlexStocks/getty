// +build linux

/******************************************************
# MAINTAINER : wongoo
# LICENCE    : Apache License 2.0
# EMAIL      : gelnyang@163.com
# MOD        : 2019-06-11
******************************************************/

package util

import (
	"syscall"
)

func SetLimit() {
	var rLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		panic(err)
	}
	rLimit.Cur = rLimit.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		panic(err)
	}
}
