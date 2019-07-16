/******************************************************
# MAINTAINER : wongoo
# LICENCE    : Apache License 2.0
# EMAIL      : gelnyang@163.com
# MOD        : 2019-06-11
******************************************************/

package hello

import (
	"github.com/dubbogo/getty"
	"time"
)

const (
	CronPeriod      = 20 * time.Second
	WritePkgTimeout = 1e8
)

var (
	log = getty.GetLogger()
)
