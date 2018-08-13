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
)

import (
	"github.com/AlexStocks/getty/micro"
	"github.com/AlexStocks/getty/rpc"
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
	config "github.com/koding/multiconfig"
)

const (
	APP_CONF_FILE     = "APP_CONF_FILE"
	APP_LOG_CONF_FILE = "APP_LOG_CONF_FILE"
)

type microConfig struct {
	rpc.ClientConfig
	Registry micro.RegistryConfig
}

var (
	conf *microConfig
)

func initConf() {
	// configure
	confFile := os.Getenv(APP_CONF_FILE)
	if confFile == "" {
		panic(fmt.Sprintf("application configure file name is nil"))
		return // I know it is of no usage. Just Err Protection.
	}
	if path.Ext(confFile) != ".toml" {
		panic(fmt.Sprintf("application configure file name{%v} suffix must be .toml", confFile))
		return
	}

	conf = new(rpc.ClientConfig)
	config.MustLoadWithPath(confFile, conf)
	if err := conf.CheckValidity(); err != nil {
		panic(jerrors.ErrorStack(err))
		return
	}

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
}
