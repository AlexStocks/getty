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
	"io/ioutil"
)

import (
	"github.com/AlexStocks/getty/rpc"
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
	"gopkg.in/yaml.v2"
)

const (
	APP_CONF_FILE     = "APP_CONF_FILE"
	APP_LOG_CONF_FILE = "APP_LOG_CONF_FILE"
)

type Config struct {
	ServerHost string `default:"127.0.0.1" yaml:"server_host" json:"server_host,omitempty"`
	ServerPort int `default:"1234" yaml:"server_port" json:"server_port,omitempty"`
	rpc.ClientConfig `yaml:"core" json:"core, omitempty"`
}

var (
	conf *Config
)

func initConf() {
	// configure
	confFile := os.Getenv(APP_CONF_FILE)
	if confFile == "" {
		panic(fmt.Sprintf("application configure file name is nil"))
		return // I know it is of no usage. Just Err Protection.
	}
	if path.Ext(confFile) != ".yml" {
		panic(fmt.Sprintf("application configure file name{%v} suffix must be .yml", confFile))
		return
	}

	conf = &Config{}
	
	confFileStream, err := ioutil.ReadFile(confFile)
	if err != nil {
		panic(fmt.Sprintf("ioutil.ReadFile(file:%s) = error:%s", confFile, jerrors.ErrorStack(err)))
		return
	}
	err = yaml.Unmarshal(confFileStream, conf)
	if err != nil {
		panic(fmt.Sprintf("yaml.Unmarshal() = error:%s", jerrors.ErrorStack(err)))
		return
	}
	fmt.Printf("conf:%#v\n", conf)
	if err = conf.ClientConfig.CheckValidity(); err != nil {
		panic(jerrors.ErrorStack(err))
		return
	}
	if conf.ServerPort < 0  {
		panic("Config.ServerPort < 0")
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
