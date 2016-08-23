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
	"time"
)

func HostAddress(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func dial(addr string) (net.Conn, error) {
	return net.Dial("tcp", addr)
}

func dialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, timeout)
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

type GettyHeader struct {
	Magic     int32
	Len       int32 // body length
	LogId     int32 // log id
	ServiceId int32 // service id
	Code      int32 // error code
	Extra     int32 // reserved
}

func (this *GettyHeader) Bytes() []byte {
	var buf = bytes.NewBuffer(make([]byte, 24))
	binary.Write(buf, binary.BigEndian, this.Magic)
	binary.Write(buf, binary.BigEndian, this.Len)
	binary.Write(buf, binary.BigEndian, this.LogId)
	binary.Write(buf, binary.BigEndian, this.ServiceId)
	binary.Write(buf, binary.BigEndian, this.Code)
	binary.Write(buf, binary.BigEndian, this.Extra)
	return buf.Bytes()
}

type WrappedPackage struct {
	H       GettyHeader
	Payload []byte
}

func (this *WrappedPackage) PackMessage() []byte {
	return append(this.H.Bytes(), this.Payload...)
}
