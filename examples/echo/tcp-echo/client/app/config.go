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
	"io/ioutil"
	"os"
	"path"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
	yaml "gopkg.in/yaml.v2"
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
		CompressEncoding bool   `default:"false" yaml:"compress_encoding" json:"compress_encoding,omitempty"`
		TcpNoDelay       bool   `default:"true" yaml:"tcp_no_delay" json:"tcp_no_delay,omitempty"`
		TcpKeepAlive     bool   `default:"true" yaml:"tcp_keep_alive" json:"tcp_keep_alive,omitempty"`
		KeepAlivePeriod  string `default:"180s" yaml:"keep_alive_period" json:"keep_alive_period,omitempty"`
		keepAlivePeriod  time.Duration
		TcpRBufSize      int    `default:"262144" yaml:"tcp_r_buf_size" json:"tcp_r_buf_size,omitempty"`
		TcpWBufSize      int    `default:"65536" yaml:"tcp_w_buf_size" json:"tcp_w_buf_size,omitempty"`
		PkgRQSize        int    `default:"1024" yaml:"pkg_rq_size" json:"pkg_rq_size,omitempty"`
		PkgWQSize        int    `default:"1024" yaml:"pkg_wq_size" json:"pkg_wq_size,omitempty"`
		TcpReadTimeout   string `default:"1s" yaml:"tcp_read_timeout" json:"tcp_read_timeout,omitempty"`
		tcpReadTimeout   time.Duration
		TcpWriteTimeout  string `default:"5s" yaml:"tcp_write_timeout" json:"tcp_write_timeout,omitempty"`
		tcpWriteTimeout  time.Duration
		WaitTimeout      string `default:"7s" yaml:"wait_timeout" json:"wait_timeout,omitempty"`
		waitTimeout      time.Duration
		MaxMsgLen        int    `default:"1024" yaml:"max_msg_len" json:"max_msg_len,omitempty"`
		SessionName      string `default:"echo-client" yaml:"session_name" json:"session_name,omitempty"`
	}

	// Config holds supported types by the multiconfig package
	Config struct {
		// local
		AppName   string `default:"echo-client" yaml:"app_name" json:"app_name,omitempty"`
		LocalHost string `default:"127.0.0.1" yaml:"local_host" json:"local_host,omitempty"`

		// server
		ServerHost  string `default:"127.0.0.1"  yaml:"server_host" json:"server_host,omitempty"`
		ServerPort  int    `default:"10000"  yaml:"server_port" json:"server_port,omitempty"`
		ProfilePort int    `default:"10086"  yaml:"profile_port" json:"profile_port,omitempty"`

		// session pool
		ConnectionNum int `default:"16" yaml:"connection_number" json:"connection_number,omitempty"`

		// heartbeat
		HeartbeatPeriod string `default:"15s" yaml:"heartbeat_period" json:"heartbeat_period,omitempty"`
		heartbeatPeriod time.Duration

		// session
		SessionTimeout string `default:"60s" yaml:"session_timeout" json:"session_timeout,omitempty"`
		sessionTimeout time.Duration

		// echo
		EchoString string `default:"hello"  yaml:"echo_string" json:"echo_string,omitempty"`
		EchoTimes  int    `default:"10"  yaml:"echo_times" json:"echo_times,omitempty"`

		// app
		FailFastTimeout string `default:"5s" yaml:"fail_fast_timeout" json:"fail_fast_timeout,omitempty"`
		failFastTimeout time.Duration

		// session tcp parameters
		GettySessionParam GettySessionParam `required:"true" yaml:"getty_session_param" json:"getty_session_param,omitempty"`

		// task pool
		TaskQueueLength int `default:"1024" yaml:"task_queue_length" json:"task_queue_length,omitempty"`
		TaskQueueNumber int `default:"1024" yaml:"task_queue_number" json:"task_queue_number,omitempty"`
		TaskPoolSize    int `default:"1024" yaml:"task_pool_size" json:"task_pool_size,omitempty"`
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
	if path.Ext(confFile) != ".yml" {
		panic(fmt.Sprintf("application configure file name{%v} suffix must be .yml", confFile))
		return
	}

	conf = &Config{}
	confFileStream, err := ioutil.ReadFile(confFile)
	if err != nil {
		panic(fmt.Sprintf("ioutil.ReadFile(file:%s) = error:%s", confFile, err))
		return
	}
	err = yaml.Unmarshal(confFileStream, conf)
	if err != nil {
		panic(fmt.Sprintf("yaml.Unmarshal() = error:%s", err))
		return
	}

	conf.heartbeatPeriod, err = time.ParseDuration(conf.HeartbeatPeriod)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(HeartbeatPeriod{%#v}) = error{%v}", conf.HeartbeatPeriod, err))
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
	conf.GettySessionParam.keepAlivePeriod, err = time.ParseDuration(conf.GettySessionParam.KeepAlivePeriod)
	if err != nil {
		panic(fmt.Sprintf("time.ParseDuration(KeepAlivePeriod{%#v}) = error{%v}", conf.GettySessionParam.KeepAlivePeriod, err))
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
