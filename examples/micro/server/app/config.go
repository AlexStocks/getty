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
)

import (
	"github.com/AlexStocks/getty/micro"
	"github.com/AlexStocks/getty/rpc"
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
	"gopkg.in/yaml.v2"
)

const (
	APP_CONF_FILE     = "APP_CONF_FILE"
	APP_LOG_CONF_FILE = "APP_LOG_CONF_FILE"
)

type microConfig struct {
	rpc.ServerConfig `yaml:"core" json:"core, omitempty"`
	Registry         micro.ProviderRegistryConfig `yaml:"registry" json:"registry, omitempty"`
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
	if path.Ext(confFile) != ".yml" {
		panic(fmt.Sprintf("application configure file name{%v} suffix must be .yml", confFile))
		return
	}
	conf = &microConfig{}

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

	if err := conf.ServerConfig.CheckValidity(); err != nil {
		panic(jerrors.ErrorStack(err))
		return
	}
	if err := conf.Registry.CheckValidity(); err != nil {
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

	return
}
