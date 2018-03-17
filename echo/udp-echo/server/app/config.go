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
		CompressEncoding bool   `default:"false"`
		UdpRBufSize      int    `default:"262144"`
		UdpWBufSize      int    `default:"65536"`
		PkgRQSize        int    `default:"1024"`
		PkgWQSize        int    `default:"1024"`
		UdpReadTimeout   string `default:"1s"`
		udpReadTimeout   time.Duration
		UdpWriteTimeout  string `default:"5s"`
		udpWriteTimeout  time.Duration
		WaitTimeout      string `default:"7s"`
		waitTimeout      time.Duration
		MaxMsgLen        int    `default:"1024"`
		SessionName      string `default:"echo-client"`
	}

	// Config holds supported types by the multiconfig package
	Config struct {
		// local address
		AppName     string   `default:"echo-server"`
		Host        string   `default:"127.0.0.1"`
		Ports       []string `default:["10000"]`
		ProfilePort int      `default:"10086"`

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

	conf.GettySessionParam.udpReadTimeout, err = time.ParseDuration(conf.GettySessionParam.UdpReadTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(UdpReadTimeout{%#v}) = error{%v}", conf.GettySessionParam.UdpReadTimeout, err))
		return
	}
	conf.GettySessionParam.udpWriteTimeout, err = time.ParseDuration(conf.GettySessionParam.UdpWriteTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(UdpWriteTimeout{%#v}) = error{%v}", conf.GettySessionParam.UdpWriteTimeout, err))
		return
	}
	conf.GettySessionParam.waitTimeout, err = time.ParseDuration(conf.GettySessionParam.WaitTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(WaitTimeout{%#v}) = error{%v}", conf.GettySessionParam.WaitTimeout, err))
		return
	}
	// gxlog.CInfo("config{%#v}\n", conf)

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
	log.Info("config{%#v}", conf)

	return
}
