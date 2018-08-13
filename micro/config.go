package micro

import (
	"github.com/AlexStocks/getty/rpc"
	jerrors "github.com/juju/errors"
	"github.com/scylladb/go-set/strset"
)

const (
	ErrIllegalConf = "illegal conf "
)

var (
	registryArray = strset.New("zookeeper", "etcd")
)

// RegistryConfig provides configuration for registry
type RegistryConfig struct {
	Type             string `default:"etcd" yaml:"type" json:"type,omitempty"`
	Addr             string `default:"" yaml:"addr" json:"addr,omitempty"`
	KeepaliveTimeout int    `default:"5" yaml:"keepalive_time" json:"keepalive_timeout,omitempty"`
	Root             string `default:"getty" yaml:"root" json:"root,omitempty"`
	IDC              string `default:"idc-bj" yaml:"idc" json:"idc,omitempty"`
	NodeID           string `default:"node0" yaml:"node_id" json:"node_id,omitempty"`
	Codec            string `default:"json" yaml:"codec" json:"codec,omitempty"`
	codec            rpc.CodecType
}

// CheckValidity check parameter validity
func (c *RegistryConfig) CheckValidity() error {
	if !registryArray.Has(c.Type) {
		return jerrors.Errorf(ErrIllegalConf+"registry type %s", c.Type)
	}

	if len(c.Addr) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"registry addr %s", c.Addr)
	}

	if c.KeepaliveTimeout < 0 {
		return jerrors.Errorf(ErrIllegalConf+"keepalive timeout %d", c.KeepaliveTimeout)
	}

	if len(c.Root) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"root %s", c.Root)
	}

	if len(c.NodeID) == 0 {
		return jerrors.Errorf(ErrIllegalConf+"node id %s", c.NodeID)
	}

	if c.codec = rpc.GetCodecType(c.Codec); c.codec == rpc.CodecUnknown {
		return jerrors.Errorf(ErrIllegalConf+"codec type %s", c.Codec)
	}

	return nil
}
