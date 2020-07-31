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
	"github.com/stretchr/testify/assert"
	"math/rand"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

func randKey(r *rand.Rand) string {
	b := make([]byte, r.Intn(4))
	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}
	return string(b)
}

func randValue() *rpcClientArray {
	return &rpcClientArray{}
}

func TestConcurrentRange(t *testing.T) {
	const mapSize = 1 << 10

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	m := RPCClientMap{}
	for n := int64(1); n <= mapSize; n++ {
		m.Store(randKey(r), randValue())
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	defer func() {
		close(done)
		wg.Wait()
	}()
	for g := int64(runtime.GOMAXPROCS(0)); g > 0; g-- {
		r := rand.New(rand.NewSource(g))
		wg.Add(1)
		go func(g int64) {
			defer wg.Done()
			for i := int64(0); ; i++ {
				select {
				case <-done:
					return
				default:
				}
				for n := int64(1); n < mapSize; n++ {
					nStr := strconv.Itoa(int(n))
					if r.Int63n(mapSize) == 0 {
						m.Store(nStr, randValue())
					} else {
						m.Load(nStr)
					}
				}
			}
		}(g)
	}

	iters := 1 << 10
	if testing.Short() {
		iters = 16
	}
	for n := iters; n > 0; n-- {
		seen := make(map[string]bool, mapSize)

		m.Range(func(ki string, vi *rpcClientArray) bool {
			//k, v := ki.(int64), vi.(int64)
			//if v%k != 0 {
			//	t.Fatalf("while Storing multiples of %v, Range saw value %v", k, v)
			//}
			if seen[ki] {
				t.Fatalf("Range visited key %v twice", ki)
			}
			seen[ki] = true
			return true
		})

		//if len(seen) != mapSize {
		//	t.Fatalf("Range visited %v elements of %v-element Map", len(seen), mapSize)
		//}
	}
}

func TestRPCClientMap_LoadOrStore(t *testing.T) {
	m := RPCClientMap{}
	for n := 1; n <= 1024; n++ {
		nStr := strconv.Itoa(n)
		_, loaded := m.LoadOrStore(nStr, randValue())
		assert.False(t, loaded)
	}
	for n := 1; n <= 1024; n++ {
		nStr := strconv.Itoa(n)
		_, loaded := m.LoadOrStore(nStr, randValue())
		assert.True(t, loaded)
		m.Delete(nStr)
	}
	for n := 1; n <= 10240; n++ {
		nStr := strconv.Itoa(n)
		_, loaded := m.LoadOrStore(nStr, randValue())
		assert.False(t, loaded)
	}
}
