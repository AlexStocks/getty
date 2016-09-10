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

/////////////////////////////////////////
// network utility
/////////////////////////////////////////

// HostAddress composes a ip:port style address. Its opposite function is net.SplitHostPort.
func HostAddress(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func HostAddress2(host string, port string) string {
	return net.JoinHostPort(host, port)
}

func HostPort(addr string) (string, string, error) {
	return net.SplitHostPort(addr)
}

/////////////////////////////////////////
// count watch
/////////////////////////////////////////

type CountWatch struct {
	start time.Time
}

func (w *CountWatch) Start() {
	var t time.Time
	if t.Equal(w.start) {
		w.start = time.Now()
	}
}

func (w *CountWatch) Reset() {
	w.start = time.Now()
}

func (w *CountWatch) Count() int64 {
	return time.Since(w.start).Nanoseconds()
}
