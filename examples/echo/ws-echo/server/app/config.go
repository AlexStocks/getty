/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"
	"os"
	"path"
	"time"
)

import (
	// "github.com/AlexStocks/goext/log"
	log "github.com/AlexStocks/log4go"
	config "github.com/koding/multiconfig"
)

const (
	APP_CONF_FILE     = "APP_CONF_FILE"
	APP_LOG_CONF_FILE = "APP_LOG_CONF_FILE"
)

var (
	conf *Config
)

type (
	GettySessionParam struct {
		CompressEncoding bool   `default:"false"` // Accept-Encoding: gzip, deflate, sdch
		TcpNoDelay       bool   `default:"true"`
		TcpKeepAlive     bool   `default:"true"`
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
		SessionName      string `default:"echo-server"`
	}

	// Config holds supported types by the multiconfig package
	Config struct {
		// local address
		AppName     string   `default:"echo-server"`
		Host        string   `default:"127.0.0.1"`
		Ports       []string `default:["10000"]`
		Paths       []string `default:["/echo"]`
		ProfilePort int      `default:"10086"`

		// session
		HeartbeatPeriod string `default:"30s"`
		heartbeatPeriod time.Duration
		SessionTimeout  string `default:"60s"`
		sessionTimeout  time.Duration
		SessionNumber   int `default:"1000"`

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
	conf.heartbeatPeriod, err = time.ParseDuration(conf.HeartbeatPeriod)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(HeartbeatPeriod{%#v}) = error{%v}", conf.heartbeatPeriod, err))
		return
	}
	conf.sessionTimeout, err = time.ParseDuration(conf.SessionTimeout)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(SessionTimeout{%#v}) = error{%v}", conf.SessionTimeout, err))
		return
	}
	if conf.sessionTimeout <= conf.heartbeatPeriod {
		panic(fmt.Sprintf("SessionTimeout{%#v} <= HeartbeatPeriod{%#v}", conf.SessionTimeout, conf.HeartbeatPeriod))
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
