package rpc

import (
	"testing"
)

import (
	_ "github.com/gogo/protobuf/gogoproto"
	"github.com/stretchr/testify/assert"
)

func TestCallType(t *testing.T) {
	ct := CT_TwoWay
	assert.NotNil(t, ct.Enum())
	data, err := ct.MarshalJSON()
	assert.Nil(t, err)
	err = ct.UnmarshalJSON(data)
	assert.Nil(t, err)
	assert.True(t, len(ct.String()) > 0)
	_, arr := ct.EnumDescriptor()
	assert.True(t, len(arr) > 0)
}

func Test(t *testing.T) {

}

func TestGettyRPCRequestHeader(t *testing.T) {
	header := GettyRPCRequestHeader{
		Service: "hello",
	}
	header.Reset()
	assert.True(t, len(header.Service) == 0)
	header.ProtoMessage()
	_, arr := header.Descriptor()
	assert.True(t, len(arr) > 0)

	assert.True(t, header.Equal(&header))
	assert.Nil(t, header.VerboseEqual(header))

	assert.True(t, len(header.GoString()) > 0)
	assert.True(t, len(header.String()) > 0)
	data, err := header.Marshal()
	assert.Nil(t, err)
	assert.True(t, len(data) > 0)
	lg, err := header.MarshalTo(data)
	assert.Nil(t, err)
	assert.True(t, lg > 0)
	err = header.Unmarshal(data)
	assert.Nil(t, err)

	assert.True(t, header.Size() > 0)
}

func TestGettyRPCResponseHeader(t *testing.T) {
	header := GettyRPCResponseHeader{
		Error: "hello",
	}
	header.Reset()
	assert.True(t, len(header.Error) == 0)
	header.ProtoMessage()
	_, arr := header.Descriptor()
	assert.True(t, len(arr) > 0)

	assert.True(t, header.Equal(&header))
	assert.Nil(t, header.VerboseEqual(header))

	assert.True(t, len(header.GoString()) > 0)
	assert.True(t, len(header.String()) > 0)
	data, err := header.Marshal()
	assert.Nil(t, err)
	assert.True(t, len(data) > 0)
	lg, err := header.MarshalTo(data)
	assert.Nil(t, err)
	assert.True(t, lg > 0)
	err = header.Unmarshal(data)
	assert.Nil(t, err)

	assert.True(t, header.Size() > 0)
}
