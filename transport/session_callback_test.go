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
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSessionCallback(t *testing.T) {
	// Test basic add, remove and execute callback functionality
	t.Run("BasicCallback", func(t *testing.T) {
		s := &session{
			once:          &sync.Once{},
			done:          make(chan struct{}),
			closeCallback: callbacks{},
		}

		var callbackExecuted bool
		var callbackMutex sync.Mutex

		callback := func() {
			callbackMutex.Lock()
			callbackExecuted = true
			callbackMutex.Unlock()
		}

		// Add callback
		s.AddCloseCallback("testHandler", "testKey", callback)
		if s.closeCallback.Len() != 1 {
			t.Errorf("Expected callback count is 1, but got %d", s.closeCallback.Len())
		}

		// Test removing callback
		s.RemoveCloseCallback("testHandler", "testKey")
		if s.closeCallback.Len() != 0 {
			t.Errorf("Expected callback count is 0, but got %d", s.closeCallback.Len())
		}

		// Re-add callback
		s.AddCloseCallback("testHandler", "testKey", callback)

		// Test callback execution when closing
		go func() {
			time.Sleep(10 * time.Millisecond)
			s.stop()
		}()

		// Wait for callback execution
		time.Sleep(50 * time.Millisecond)

		callbackMutex.Lock()
		if !callbackExecuted {
			t.Error("Callback function was not executed")
		}
		callbackMutex.Unlock()
	})

	// Test adding, removing and executing multiple callbacks
	t.Run("MultipleCallbacks", func(t *testing.T) {
		s := &session{
			once:          &sync.Once{},
			done:          make(chan struct{}),
			closeCallback: callbacks{},
		}

		var callbackCount int
		var callbackMutex sync.Mutex

		// Add multiple callbacks
		totalCallbacks := 3
		for i := 0; i < totalCallbacks; i++ {
			index := i // Capture loop variable
			callback := func() {
				callbackMutex.Lock()
				callbackCount++
				callbackMutex.Unlock()
			}
			s.AddCloseCallback(fmt.Sprintf("handler%d", index), fmt.Sprintf("key%d", index), callback)
		}

		if s.closeCallback.Len() != totalCallbacks {
			t.Errorf("Expected callback count is %d, but got %d", totalCallbacks, s.closeCallback.Len())
		}

		// Remove one callback
		s.RemoveCloseCallback("handler0", "key0")
		expectedAfterRemove := totalCallbacks - 1
		if s.closeCallback.Len() != expectedAfterRemove {
			t.Errorf("Expected callback count is %d, but got %d", expectedAfterRemove, s.closeCallback.Len())
		}

		// Test execution of remaining callbacks when closing
		go func() {
			time.Sleep(10 * time.Millisecond)
			s.stop()
		}()

		time.Sleep(50 * time.Millisecond)

		callbackMutex.Lock()
		if callbackCount != expectedAfterRemove {
			t.Errorf("Expected executed callback count is %d, but got %d", expectedAfterRemove, callbackCount)
		}
		callbackMutex.Unlock()
	})

	// Test invokeCloseCallbacks functionality
	t.Run("InvokeCloseCallbacks", func(t *testing.T) {
		s := &session{
			once:          &sync.Once{},
			done:          make(chan struct{}),
			closeCallback: callbacks{},
		}

		var callbackResults []string
		var callbackMutex sync.Mutex

		// Add multiple different types of close callbacks
		callbacks := []struct {
			handler string
			key     string
			action  string
		}{
			{"cleanup", "resources", "Clean resources"},
			{"cleanup", "connections", "Close connections"},
			{"logging", "audit", "Log audit info"},
			{"metrics", "stats", "Update statistics"},
		}

		// Register all callbacks
		for _, cb := range callbacks {
			cbCopy := cb // Capture loop variable
			callback := func() {
				callbackMutex.Lock()
				callbackResults = append(callbackResults, cbCopy.action)
				callbackMutex.Unlock()
			}
			s.AddCloseCallback(cbCopy.handler, cbCopy.key, callback)
		}

		// Verify callback count
		expectedCount := len(callbacks)
		if s.closeCallback.Len() != expectedCount {
			t.Errorf("Expected callback count is %d, but got %d", expectedCount, s.closeCallback.Len())
		}

		// Manually invoke close callbacks (simulate invokeCloseCallbacks)
		callbackMutex.Lock()
		callbackResults = nil // Clear previous results
		callbackMutex.Unlock()

		// Execute all close callbacks
		s.closeCallback.Invoke()

		// Wait for callback execution to complete
		time.Sleep(10 * time.Millisecond)

		// Verify all callbacks were executed
		callbackMutex.Lock()
		if len(callbackResults) != expectedCount {
			t.Errorf("Expected to execute %d callbacks, but executed %d", expectedCount, len(callbackResults))
		}

		// Verify callback execution order (should execute in order of addition)
		expectedActions := []string{"Clean resources", "Close connections", "Log audit info", "Update statistics"}
		for i, result := range callbackResults {
			if i < len(expectedActions) && result != expectedActions[i] {
				t.Errorf("Position %d: Expected to execute '%s', but executed '%s'", i, expectedActions[i], result)
			}
		}
		callbackMutex.Unlock()

		// Test execution after removing a callback
		s.RemoveCloseCallback("cleanup", "resources")

		callbackMutex.Lock()
		callbackResults = nil
		callbackMutex.Unlock()

		// Execute callbacks again
		s.closeCallback.Invoke()
		time.Sleep(10 * time.Millisecond)

		// Verify execution results after removal
		callbackMutex.Lock()
		expectedAfterRemove := expectedCount - 1
		if len(callbackResults) != expectedAfterRemove {
			t.Errorf("Expected to execute %d callbacks after removal, but executed %d", expectedAfterRemove, len(callbackResults))
		}
		callbackMutex.Unlock()
	})

	// Test edge cases
	t.Run("EdgeCases", func(t *testing.T) {
		// Test empty callback list scenario
		s := &session{
			once:          &sync.Once{},
			done:          make(chan struct{}),
			closeCallback: callbacks{},
		}

		// Verify empty list
		if s.closeCallback.Len() != 0 {
			t.Errorf("Expected count for empty list is 0, but got %d", s.closeCallback.Len())
		}

		// Execute empty callback list (should not panic)
		s.closeCallback.Invoke()

		// Add a callback then remove it, execute again
		s.AddCloseCallback("test", "key", func() {})
		s.RemoveCloseCallback("test", "key")

		// Execute empty list after removal (should not panic)
		s.closeCallback.Invoke()

		if s.closeCallback.Len() != 0 {
			t.Errorf("Expected count after removal is 0, but got %d", s.closeCallback.Len())
		}
	})
}

func TestResetWaitsForCloseCallbacks(t *testing.T) {
	s := &session{
		once:          &sync.Once{},
		done:          make(chan struct{}),
		closeCallback: callbacks{},
	}

	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	s.AddCloseCallback("test", "blocking", func() {
		close(callbackStarted)
		<-releaseCallback
	})

	callbackDone := make(chan struct{})
	go func() {
		s.invokeCloseCallbacks()
		close(callbackDone)
	}()
	<-callbackStarted

	resetDone := make(chan struct{})
	go func() {
		s.Reset()
		close(resetDone)
	}()

	select {
	case <-resetDone:
		close(releaseCallback)
		<-callbackDone
		t.Fatal("Reset returned while a close callback was still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseCallback)
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("close callback did not finish")
	}
	select {
	case <-resetDone:
	case <-time.After(time.Second):
		t.Fatal("Reset did not finish after the close callback completed")
	}
	if got := s.closeCallback.Len(); got != 0 {
		t.Fatalf("callback count after Reset = %d, want 0", got)
	}
}
