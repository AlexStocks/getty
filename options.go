/******************************************************
# DESC    : getty client/server options
# AUTHOR  : Alex Stocks
# VERSION : 1.0
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2018-03-17 21:12
# FILE    : options.go
******************************************************/

package getty

import (
	"fmt"
)

/////////////////////////////////////////
// Server Options
/////////////////////////////////////////

type ServerOption func(*ServerOptions)

type ServerOptions struct {
	addr string

	// websocket
	path       string
	cert       string
	privateKey string
	caCert     string
}

// @addr server listen address.
func WithLocalAddress(addr string) ServerOption {
	return func(o *ServerOptions) {
		o.addr = addr
	}
}

// @path: websocket request url path
func WithWebsocketServerPath(path string) ServerOption {
	return func(o *ServerOptions) {
		o.path = path
	}
}

// @cert: server certificate file
func WithWebsocketServerCert(cert string) ServerOption {
	return func(o *ServerOptions) {
		o.cert = cert
	}
}

// @key: server private key(contains its public key)
func WithWebsocketServerPrivateKey(key string) ServerOption {
	return func(o *ServerOptions) {
		o.privateKey = key
	}
}

// @cert is the root certificate file to verify the legitimacy of server
func WithWebsocketServerRootCert(cert string) ServerOption {
	return func(o *ServerOptions) {
		o.caCert = cert
	}
}

/////////////////////////////////////////
// Client Options
/////////////////////////////////////////

type ClientOption func(*ClientOptions)

type ClientOptions struct {
	addr              string
	number            int
	reconnectInterval int // reConnect Interval

	// the cert file of wss server which may contain server domain, server ip, the starting effective date, effective
	// duration, the hash alg, the len of the private key.
	// wss client will use it.
	cert string
}

// @addr is server address.
func WithServerAddress(addr string) ClientOption {
	return func(o *ClientOptions) {
		o.addr = addr
	}
}

// @reconnectInterval is server address.
func WithReconnectInterval(reconnectInterval int) ClientOption {
	return func(o *ClientOptions) {
		o.reconnectInterval = reconnectInterval
	}
}

// @num is connection number.
func WithConnectionNumber(num int) ClientOption {
	return func(o *ClientOptions) {
		o.number = num
	}
}

// @cert is client certificate file. it can be empty.
func WithRootCertificateFile(cert string) ClientOption {
	return func(o *ClientOptions) {
		o.cert = cert
	}
}

/////////////////////////////////////////
// Task Pool Options
/////////////////////////////////////////

type TaskPoolOptions struct {
	tQLen      int // task queue length
	tQNumber   int // task queue number
	tQPoolSize int // task pool size
}

func (o *TaskPoolOptions) validate() {
	if o.tQPoolSize < 1 {
		panic(fmt.Sprintf("[getty][task_pool] illegal pool size %d", o.tQPoolSize))
	}

	if o.tQLen < 1 {
		o.tQLen = defaultTaskQLen
	}

	if o.tQNumber < 1 {
		o.tQNumber = defaultTaskQNumber
	}

	if o.tQNumber > o.tQPoolSize {
		o.tQNumber = o.tQPoolSize
	}
}

type TaskPoolOption func(*TaskPoolOptions)

// @size is the task queue pool size
func WithTaskPoolTaskPoolSize(size int) TaskPoolOption {
	return func(o *TaskPoolOptions) {
		o.tQPoolSize = size
	}
}

// @length is the task queue length
func WithTaskPoolTaskQueueLength(length int) TaskPoolOption {
	return func(o *TaskPoolOptions) {
		o.tQLen = length
	}
}

// @number is the task queue number
func WithTaskPoolTaskQueueNumber(number int) TaskPoolOption {
	return func(o *TaskPoolOptions) {
		o.tQNumber = number
	}
}
