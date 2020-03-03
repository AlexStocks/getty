package rpc

import (
    "testing"
)

import (
    "github.com/stretchr/testify/assert"
)

func TestClientConfig_CheckValidity(t *testing.T) {
    var err error
    initConf()
    err = clientConf.CheckValidity()
    assert.Nil(t, err)
}

func TestServerConfig_CheckValidity(t *testing.T) {
    var err error
    initConf()
    err = serverConf.CheckValidity()
    assert.Nil(t, err)
}

