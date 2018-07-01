package rpc

import (
	"time"
)

type (
	GettySessionParam struct {
		CompressEncoding bool   `default:"false"`
		TcpNoDelay       bool   `default:"true"`
		TcpKeepAlive     bool   `default:"true"`
		KeepAlivePeriod  string `default:"180s"`
		keepAlivePeriod  time.Duration
		TcpRBufSize      int    `default:"262144"`
		TcpWBufSize      int    `default:"65536"`
		PkgRQSize        int    `default:"1024"`
		PkgWQSize        int    `default:"1024"`
		TcpReadTimeout   string `default:"1s"`
		tcpReadTimeout   time.Duration
		TcpWriteTimeout  string `default:"5s"`
		tcpWriteTimeout  time.Duration
		WaitTimeout      string `default:"7s"`
		waitTimeout      time.Duration
		MaxMsgLen        int    `default:"1024"`
		SessionName      string `default:"echo-client"`
	}

	// Config holds supported types by the multiconfig package
	Config struct {
		// local address
		AppName string   `default:"echo-server"`
		Host    string   `default:"127.0.0.1"`
		Ports   []string `default:["10000"]`

		// server
		ServerHost  string `default:"127.0.0.1"`
		ServerPort  int    `default:"10000"`
		ProfilePort int    `default:"10086"`

		// session pool
		ConnectionNum int `default:"16"`

		// heartbeat
		HeartbeatPeriod string `default:"15s"`
		heartbeatPeriod time.Duration

		// session
		SessionTimeout string `default:"60s"`
		sessionTimeout time.Duration
		SessionNumber  int `default:"1000"`

		// app
		FailFastTimeout string `default:"5s"`
		failFastTimeout time.Duration

		// session tcp parameters
		GettySessionParam GettySessionParam `required:"true"`
	}
)
