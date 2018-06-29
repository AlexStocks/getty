package rpc

import (
	"fmt"
	"path"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
	config "github.com/koding/multiconfig"
)

const (
	defaultClientConfFile    string = "client_config.toml"
	defaultClientLogConfFile string = "client_log.xml"
	defaultServerConfFile    string = "server_config.toml"
	defaultServerLogConfFile string = "server_log.xml"
)

var (
	conf *Config
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

func initConf(confFile string) {
	var err error
	if path.Ext(confFile) != ".toml" {
		panic(fmt.Sprintf("application configure file name{%v} suffix must be .toml", confFile))
	}
	conf = new(Config)
	config.MustLoadWithPath(confFile, conf)

	conf.heartbeatPeriod, err = time.ParseDuration(conf.HeartbeatPeriod)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(HeartbeatPeroid{%#v}) = error{%v}", conf.HeartbeatPeriod, err))
	}

	conf.sessionTimeout, err = time.ParseDuration(conf.SessionTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(SessionTimeout{%#v}) = error{%v}", conf.SessionTimeout, err))
	}
	conf.failFastTimeout, err = time.ParseDuration(conf.FailFastTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(FailFastTimeout{%#v}) = error{%v}", conf.FailFastTimeout, err))
	}
	conf.GettySessionParam.keepAlivePeriod, err = time.ParseDuration(conf.GettySessionParam.KeepAlivePeriod)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(KeepAlivePeriod{%#v}) = error{%v}", conf.GettySessionParam.KeepAlivePeriod, err))
	}
	conf.GettySessionParam.tcpReadTimeout, err = time.ParseDuration(conf.GettySessionParam.TcpReadTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(TcpReadTimeout{%#v}) = error{%v}", conf.GettySessionParam.TcpReadTimeout, err))
	}
	conf.GettySessionParam.tcpWriteTimeout, err = time.ParseDuration(conf.GettySessionParam.TcpWriteTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(TcpWriteTimeout{%#v}) = error{%v}", conf.GettySessionParam.TcpWriteTimeout, err))
	}
	conf.GettySessionParam.waitTimeout, err = time.ParseDuration(conf.GettySessionParam.WaitTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(WaitTimeout{%#v}) = error{%v}", conf.GettySessionParam.WaitTimeout, err))
	}
	return
}

func initLog(logFile string) {
	if path.Ext(logFile) != ".xml" {
		panic(fmt.Sprintf("log configure file name{%v} suffix must be .xml", logFile))
	}
	log.LoadConfiguration(logFile)
	log.Info("config{%#v}", conf)
}
