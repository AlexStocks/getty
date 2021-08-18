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
