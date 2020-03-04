package rpc

import (
    "testing"
)

import (
    "github.com/stretchr/testify/assert"
)

func TestClientConfig_CheckValidity(t *testing.T) {
    var err error
    err = buildClientConf().CheckValidity()
    assert.Nil(t, err)
}

func TestServerConfig_CheckValidity(t *testing.T) {
    var err error
    err = buildServerConf().CheckValidity()
    assert.Nil(t, err)
}

