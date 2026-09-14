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

// quietLogger swallows records, so the test does not fill its output with what
// the concurrent writers produce.
type quietLogger struct{}

func (quietLogger) Debug(args ...any)         {}
func (quietLogger) Debugf(_ string, _ ...any) {}
func (quietLogger) Info(args ...any)          {}
func (quietLogger) Infof(_ string, _ ...any)  {}
func (quietLogger) Warn(args ...any)          {}
func (quietLogger) Warnf(_ string, _ ...any)  {}
func (quietLogger) Error(args ...any)         {}
func (quietLogger) Errorf(_ string, _ ...any) {}

// TestSetLoggerIsSafeWhileOtherGoroutinesLog: SetLogger assigned the package
// level interface value that the helpers - Debugf and friends - read directly,
// so any session logging while a management goroutine called SetLogger raced
// with that assignment and could even observe a half written interface value.
// The logger now sits behind an atomic pointer: the swap is one store and the
// value a reader loads is immutable.
//
// The race detector is the point of this test, exactly like the level test
// above: it is green on the broken code unless something drives a swap and a log
// call from two goroutines at once.
func TestSetLoggerIsSafeWhileOtherGoroutinesLog(t *testing.T) {
	const iterations = 2000

	previous := GetLogger()
	t.Cleanup(func() { SetLogger(previous) })

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			SetLogger(quietLogger{})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			Debugf("probe %d", i)
			Infof("probe %d", i)
		}
	}()
	wg.Wait()
}
