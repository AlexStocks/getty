/******************************************************
# MAINTAINER : wongoo
# LICENCE    : Apache License 2.0
# EMAIL      : gelnyang@163.com
# MOD        : 2019-06-11
******************************************************/

package hello

import (
	"github.com/dubbogo/getty"
)

const (
	CronPeriod      = 20e9
	WritePkgTimeout = 1e8
)

var (
	log = getty.GetLogger()
)
