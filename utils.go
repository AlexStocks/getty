/******************************************************
# DESC    : getty utility
# AUTHOR  : Alex Stocks
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2016-08-22 17:44
# FILE    : utils.go
******************************************************/

package getty

import (
	"net"
	"strconv"
	"time"
)

// HostAddress composes a ip:port style address. Its opposite function is net.SplitHostPort.
func HostAddress(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

type CountWatch struct {
	start time.Time
}

func (w *CountWatch) Start() {
	w.start = time.Now()
}

func (w *CountWatch) Reset() {
	w.start = time.Now()
}

func (w *CountWatch) Count() int64 {
	return time.Since(w.start).Nanoseconds()
}
