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

var (
	Sessions []getty.Session
)

func ClientRequest() {
	for _, session := range Sessions {
		go func() {
			echoTimes := 10
			for i := 0; i < echoTimes; i++ {
				err := session.WritePkg("hello", WritePkgTimeout)
				if err != nil {
					log.Infof("session.WritePkg(session{%s}, error{%v}", session.Stat(), err)
					session.Close()
				}
			}
			log.Infof("after loop %d times", echoTimes)
		}()
	}

}
