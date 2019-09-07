/******************************************************
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2019-07-25
******************************************************/

package util

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
)

func Profiling(port int) {
	go func() {
		http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	}()
}
