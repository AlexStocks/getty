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

package micro

import (
	"github.com/AlexStocks/goext/container/set/strset"
	jerrors "github.com/juju/errors"
)

import (
	"github.com/AlexStocks/getty/rpc"
)

const (
	ErrIllegalConf = "illegal conf "
)

var (
	registryArray = strset.New("zookeeper", "etcd")
)

type ServiceConfig struct {
	LocalHost string `default:"127.0.0.1" yaml:"local_host" json:"local_host, omitempty"`
	LocalPort int    `default:"10001" yaml:"local_port" json:"local_port, omitempty"`
	Group     string `default:"idc-bj" yaml:"group" json:"group,omitempty"`
	NodeID    string `default:"node0" yaml:"node_id" json:"node_id,omitempty"`
	Protocol  string `default:"json" yaml:"protocol" json:"protocol,omitempty"`
	Service   string `default:"test" yaml:"service" json:"service,omitempty"`
	Version   string `default:"v1" yaml:"version" json:"version,omitempty"`
	Meta      string `default:"default-meta" yaml:"meta" json:"meta,omitempty"`
}

// CheckValidity check parameter validity
func (c *ServiceConfig) CheckValidity() error {
	if len(c.LocalHost) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"local host %s", c.LocalHost)
	}

	if c.LocalPort <= 0 || 65535 < c.LocalPort {
		return jerrors.Errorf(ErrIllegalConf+"local port %d", c.LocalPort)
	}

	if len(c.Group) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"group %s", c.Group)
	}

	if len(c.NodeID) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"node id %s", c.NodeID)
	}

	if codec := rpc.GetCodecType(c.Protocol); codec == rpc.CodecUnknown {
		return jerrors.Errorf(ErrIllegalConf+"protocol type %s", c.Protocol)
	}

	if len(c.Service) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"service %s", c.Service)
	}

	if len(c.Version) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"service version %s", c.Version)
	}

	return nil
}

// RegistryConfig provides configuration for registry
type RegistryConfig struct {
	Type             string `default:"etcd" yaml:"type" json:"type,omitempty"`
	RegAddr          string `default:"127.0.0.1:2181" yaml:"reg_addr" json:"reg_addr,omitempty"`
	KeepaliveTimeout int    `default:"5" yaml:"keepalive_timeout" json:"keepalive_timeout,omitempty"`
	Root             string `default:"/getty" yaml:"root" json:"root,omitempty"`
}

// CheckValidity check parameter validity
func (c *RegistryConfig) CheckValidity() error {
	if !registryArray.Has(c.Type) {
		return jerrors.Errorf(ErrIllegalConf+"registry type %s", c.Type)
	}
	if len(c.RegAddr) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"registry addr %s", c.RegAddr)
	}

	if c.KeepaliveTimeout < 0 {
		return jerrors.Errorf(ErrIllegalConf+"keepalive timeout %d", c.KeepaliveTimeout)
	}

	if len(c.Root) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"root %s", c.Root)
	}

	return nil
}

// ProviderRegistryConfig provides provider configuration for registry
type ProviderRegistryConfig struct {
	RegistryConfig `yaml:"basic" json:"basic,omitempty"`
	ServiceArray   []ServiceConfig `default:"" yaml:"service_array" json:"service_array,omitempty"`
}

// CheckValidity check parameter validity
func (c *ProviderRegistryConfig) CheckValidity() error {
	if err := c.RegistryConfig.CheckValidity(); err != nil {
		return jerrors.Trace(err)
	}

	for idx := 0; idx < len(c.ServiceArray); idx++ {
		if err := c.ServiceArray[idx].CheckValidity(); err != nil {
			return jerrors.Errorf(ErrIllegalConf+"service reference config, idx:%d, err:%s",
				idx, jerrors.ErrorStack(err))
		}
	}

	return nil
}

// ConsumerRegistryConfig provides consumer configuration for registry
type ConsumerRegistryConfig struct {
	RegistryConfig `yaml:"basic" json:"basic,omitempty"`
	Group          string `default:"idc-bj" yaml:"group" json:"group,omitempty"`
}

// CheckValidity check parameter validity
func (c *ConsumerRegistryConfig) CheckValidity() error {
	if err := c.RegistryConfig.CheckValidity(); err != nil {
		return jerrors.Trace(err)
	}

	if len(c.Group) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"group %s", c.Group)
	}

	return nil
}
