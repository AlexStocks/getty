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

package service

import (
	log "github.com/AlexStocks/log4go"
)

import (
	jerrors "github.com/juju/errors"
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

func (r *TestService) Test(req *TestReq, rsp *TestRsp) error {
	rsp.A = req.A + ", " + req.B + ", " + req.C
	return nil
}

func (r *TestService) Add(req *AddReq, rsp *AddRsp) error {
	rsp.Sum = req.A + req.B
	return nil
}

func (r *TestService) Err(req *ErrReq, rsp *ErrRsp) error {
	return jerrors.New("this is a error test")
}

func (r *TestService) Event(req *EventReq) error {
	log.Info("got event %s", req.A)
	return nil
}
