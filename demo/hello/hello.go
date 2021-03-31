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

package hello

import (
	"github.com/apache/dubbo-getty"
)

var Sessions []getty.Session

func ClientRequest() {
	for _, session := range Sessions {
		ss := session
		go func() {
			echoTimes := 10
			for i := 0; i < echoTimes; i++ {
				_, _, err := ss.WritePkg("hello", WritePkgTimeout)
				if err != nil {
					log.Infof("session.WritePkg(session{%s}, error{%v}", ss.Stat(), err)
					ss.Close()
				}
			}
			log.Infof("after loop %d times", echoTimes)
		}()
	}
}
