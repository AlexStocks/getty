package rpc_examples

import (
	"errors"
)

type TestService struct {
	i int
}

func (r *TestService) Service() string {
	return "TestService"
}

func (r *TestService) Version() string {
	return "v1.0"
}

func (r *TestService) Test(arg TestReq, rsp *TestRsp) error {
	rsp.A = arg.A + ", " + arg.B + ", " + arg.C
	return nil
}

func (r *TestService) Add(n int, res *int) error {
	r.i += n
	*res = r.i + 100
	return nil
}

func (r *TestService) Err(n int, res *int) error {
	return errors.New("this is a error test")
}
