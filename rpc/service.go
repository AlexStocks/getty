package rpc

import (
	"reflect"
	"sync"
)

type methodType struct {
	sync.Mutex
	method    reflect.Method
	ArgType   reflect.Type
	ReplyType reflect.Type
	numCalls  uint
}
type service struct {
	name   string
	rcvr   reflect.Value
	typ    reflect.Type
	method map[string]*methodType
}
