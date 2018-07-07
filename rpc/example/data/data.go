package data

import (
	"errors"

	log "github.com/AlexStocks/log4go"
)

type TestABC struct {
	A, B, C string
}

type TestRpc struct {
	i int
}

func (r *TestRpc) Service() string {
	return "TestRpc"
}

func (r *TestRpc) Version() string {
	return "v1.0"
}

func (r *TestRpc) Test(arg TestABC, res *string) error {
	log.Debug("arg:%+v", arg)
	*res = "this is a test"
	return nil
}

func (r *TestRpc) Add(n int, res *int) error {
	r.i += n
	*res = r.i + 100
	return nil
}

func (r *TestRpc) Err(n int, res *int) error {
	return errors.New("this is a error test")
}
