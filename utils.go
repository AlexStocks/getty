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
	"bytes"
	"encoding/binary"
	"net"
	"strconv"
)

// HostAddress composes a ip:port style address. Its opposite function is net.SplitHostPort.
func HostAddress(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

////////////////////////////////////////
// enc/dec
////////////////////////////////////////

func Int2Bytes(x int32) []byte {
	var buf = bytes.NewBuffer([]byte{})
	binary.Write(buf, binary.BigEndian, x)
	return buf.Bytes()
}

func Bytes2Int(b []byte) int32 {
	var (
		x   int32
		buf *bytes.Buffer
	)
	buf = bytes.NewBuffer(b)
	binary.Read(buf, binary.BigEndian, &x)
	return x
}
