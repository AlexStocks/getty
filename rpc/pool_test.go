package rpc

import (
	"testing"
)

import (
	"github.com/stretchr/testify/assert"
)

func TestRpcClientArray(t *testing.T) {
	arr := newRpcClientArray()
	assert.NotNil(t, arr)

	client := &gettyRPCClient{}
	arr.Put(client)
	assert.Equal(t, 1, len(arr.array))

	arr.Close()
	assert.Equal(t, 0, len(arr.array))
}
