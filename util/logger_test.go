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

package getty

import (
	"sync"
	"testing"
)

// TestLoggerLevelAccessorsAreRaceFree drives SetLoggerLevel against the level
// getters from two goroutines. SetLoggerLevel used to assign a fresh
// zap.AtomicLevel to zapLoggerConfig.Level while IsDebugEnabled/GetLoggerLevel
// read that field - a data race on the field itself, reachable from any
// management thread because IsDebugEnabled sits on the per-connection paths.
// AtomicLevel is now updated in place, so both sides go through atomics.
//
// The race detector is the point of this test: it is green on the broken code
// unless something drives both directions at once, which is why the race job
// needs to cover this package.
func TestLoggerLevelAccessorsAreRaceFree(t *testing.T) {
	const iterations = 2000

	previous := GetLoggerLevel()
	t.Cleanup(func() {
		if err := SetLoggerLevel(previous); err != nil {
			t.Error(err)
		}
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := SetLoggerLevel(LoggerLevelInfo); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = IsDebugEnabled()
			_ = GetLoggerLevel()
		}
	}()
	wg.Wait()
}
