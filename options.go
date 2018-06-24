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

	// handler
	reader         Reader // @reader should be nil when @conn is a gettyWSConn object.
	writer         Writer
	eventListener  EventListener
	sessionHandler SessionHandler
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

// @reader bytes to pkgmessage
func WithServerPkgReader(reader Reader) ServerOption {
	return func(o *ServerOptions) {
		o.reader = reader
	}
}

// @writer pkgmessage to bytes
func WithServerPkgWriter(writer Writer) ServerOption {
	return func(o *ServerOptions) {
		o.writer = writer
	}
}

// @listener eventlistener of server
func WithServerEventListener(listener EventListener) ServerOption {
	return func(o *ServerOptions) {
		o.eventListener = listener
	}
}

// @hander hander'init will be called when session init
func WithServerSessionHander(handler SessionHandler) ServerOption {
	return func(o *ServerOptions) {
		o.sessionHandler = handler
	}
}

/////////////////////////////////////////
// Client Options
/////////////////////////////////////////

type ClientOption func(*ClientOptions)

type ClientOptions struct {
	addr   string
	number int

	// the cert file of wss server which may contain server domain, server ip, the starting effective date, effective
	// duration, the hash alg, the len of the private key.
	// wss client will use it.
	cert string

	// handler
	reader         Reader // @reader should be nil when @conn is a gettyWSConn object.
	writer         Writer
	eventListener  EventListener
	sessionHandler SessionHandler
}

// @addr is server address.
func WithServerAddress(addr string) ClientOption {
	return func(o *ClientOptions) {
		o.addr = addr
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

// @reader bytes to pkgmessage
func WithClientPkgReader(reader Reader) ClientOption {
	return func(o *ClientOptions) {
		o.reader = reader
	}
}

// @writer pkgmessage to bytes
func WithClientPkgWriter(writer Writer) ClientOption {
	return func(o *ClientOptions) {
		o.writer = writer
	}
}

// @listener eventlistener of server
func WithClientEventListener(listener EventListener) ClientOption {
	return func(o *ClientOptions) {
		o.eventListener = listener
	}
}

// @hander hander'init will be called when session init
func WithClientSessionHander(handler SessionHandler) ClientOption {
	return func(o *ClientOptions) {
		o.sessionHandler = handler
	}
}
