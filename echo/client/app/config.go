/******************************************************
# DESC       : env var & configure
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2016-09-06 16:53
# FILE       : config.go
******************************************************/

package main

import (
	"fmt"
	"os"
	"path"
	"time"
)

import (
	"github.com/AlexStocks/gocolor"
	log "github.com/AlexStocks/log4go"
	config "github.com/koding/multiconfig"
)

const (
	APP_CONF_FILE     string = "APP_CONF_FILE"
	APP_LOG_CONF_FILE string = "APP_LOG_CONF_FILE"
)

var (
	conf *Config
)

type (
	GettySessionParam struct {
		TcpNoDelay      bool   `default:"true"`
		TcpKeepAlive    bool   `default:"true"`
		TcpRBufSize     int    `default:"262144"`
		TcpWBufSize     int    `default:"65536"`
		PkgRQSize       int    `default:"1024"`
		PkgWQSize       int    `default:"1024"`
		TcpReadTimeout  string `default:"1s"`
		tcpReadTimeout  time.Duration
		TcpWriteTimeout string `default:"5s"`
		tcpWriteTimeout time.Duration
		WaitTimeout     string `default:"7s"`
		waitTimeout     time.Duration
		SessionName     string `default:"echo-client"`
	}

	// Config holds supported types by the multiconfig package
	Config struct {
		LocalHost string `default:"127.0.0.1"`

		// server
		ServerHost  string `default:"127.0.0.1"`
		ServerPort  int    `default:"10000"`
		ProfilePort int    `default:"10086"`

		// session pool
		ConnectionNum   int    `default:"16"`
		ConnectInterval string `default:"5s"`
		connectInterval time.Duration

		// heartbeat
		HeartbeatPeriod string `default:"15s"`
		heartbeatPeriod time.Duration

		// session
		SessionTimeout string `default:"60s"`
		sessionTimeout time.Duration

		// echo
		EchoString string `default:"hello"`
		EchoTimes  int    `default:"10"`

		// app
		FailFastTimeout string `default:"5s"`
		failFastTimeout time.Duration

		// session tcp parameters
		GettySessionParam GettySessionParam `required:"true"`
	}
)

func initConf() {
	var (
		err      error
		confFile string
	)

	// configure
	confFile = os.Getenv(APP_CONF_FILE)
	if confFile == "" {
		panic(fmt.Sprintf("application configure file name is nil"))
		return // I know it is of no usage. Just Err Protection.
	}
	if path.Ext(confFile) != ".toml" {
		panic(fmt.Sprintf("application configure file name{%v} suffix must be .toml", confFile))
		return
	}
	conf = new(Config)
	config.MustLoadWithPath(confFile, conf)
	conf.connectInterval, err = time.ParseDuration(conf.ConnectInterval)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(ConnectionInterval{%#v}) = error{%v}", conf.ConnectInterval, err))
		return
	}
	conf.heartbeatPeriod, err = time.ParseDuration(conf.HeartbeatPeriod)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(HeartbeatPeroid{%#v}) = error{%v}", conf.HeartbeatPeriod, err))
		return
	}
	conf.sessionTimeout, err = time.ParseDuration(conf.SessionTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(SessionTimeout{%#v}) = error{%v}", conf.SessionTimeout, err))
		return
	}
	conf.failFastTimeout, err = time.ParseDuration(conf.FailFastTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(FailFastTimeout{%#v}) = error{%v}", conf.FailFastTimeout, err))
		return
	}
	conf.GettySessionParam.tcpReadTimeout, err = time.ParseDuration(conf.GettySessionParam.TcpReadTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(TcpReadTimeout{%#v}) = error{%v}", conf.GettySessionParam.TcpReadTimeout, err))
		return
	}
	conf.GettySessionParam.tcpWriteTimeout, err = time.ParseDuration(conf.GettySessionParam.TcpWriteTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(TcpWriteTimeout{%#v}) = error{%v}", conf.GettySessionParam.TcpWriteTimeout, err))
		return
	}
	conf.GettySessionParam.waitTimeout, err = time.ParseDuration(conf.GettySessionParam.WaitTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(WaitTimeout{%#v}) = error{%v}", conf.GettySessionParam.WaitTimeout, err))
		return
	}

	gocolor.Info("config{%#v}\n", conf)

	// log
	confFile = os.Getenv(APP_LOG_CONF_FILE)
	if confFile == "" {
		panic(fmt.Sprintf("log configure file name is nil"))
		return
	}
	if path.Ext(confFile) != ".xml" {
		panic(fmt.Sprintf("log configure file name{%v} suffix must be .xml", confFile))
		return
	}
	log.LoadConfiguration(confFile)

	return
}
