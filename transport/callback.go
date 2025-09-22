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
	"reflect"
)

import (
	perrors "github.com/pkg/errors"
)

import (
	log "github.com/AlexStocks/getty/util"
)

// callbackNode represents a node in the callback linked list
// Each node contains handler identifier, key, callback function and pointer to next node
type callbackNode struct {
	handler any           // Handler identifier, used to identify the source or type of callback
	key     any           // Unique identifier key for callback, used in combination with handler
	call    func()        // Actual callback function to be executed
	next    *callbackNode // Pointer to next node, forming linked list structure
}

// callbacks is a singly linked list structure for managing multiple callback functions
// Supports dynamic addition, removal and execution of callbacks
type callbacks struct {
	first *callbackNode // Pointer to the first node of the linked list
	last  *callbackNode // Pointer to the last node of the linked list, used for quick addition of new nodes
	cbNum int           // Number of callback functions in the linked list
}

// isComparable checks if a value is comparable using Go's == operator
// Returns true if the value can be safely compared, false otherwise
func isComparable(v any) bool {
	if v == nil {
		return true
	}
	return reflect.TypeOf(v).Comparable()
}

// Add adds a new callback function to the callback linked list
// Parameters:
//   - handler: Handler identifier, can be any type
//   - key: Unique identifier key for callback, used in combination with handler
//   - callback: Callback function to be executed, ignored if nil
//
// Note: If a callback with the same handler and key already exists, it will be replaced
func (t *callbacks) Add(handler, key any, callback func()) {
	// Prevent adding empty callback function
	if callback == nil {
		return
	}

	// Guard: avoid runtime panic on non-comparable types
	if !isComparable(handler) || !isComparable(key) {
		_ = log.Error(perrors.New(fmt.Sprintf("callbacks.Add: non-comparable handler/key: %T, %T; ignored", handler, key)))
		return
	}

	// Check if a callback with the same handler and key already exists
	for cb := t.first; cb != nil; cb = cb.next {
		if cb.handler == handler && cb.key == key {
			// Replace existing callback
			cb.call = callback
			return
		}
	}

	// Create new callback node
	newItem := &callbackNode{handler, key, callback, nil}

	if t.first == nil {
		// If linked list is empty, new node becomes the first node
		t.first = newItem
	} else {
		// Otherwise add new node to the end of linked list
		t.last.next = newItem
	}
	// Update pointer to last node
	t.last = newItem
	// Increment callback count
	t.cbNum++
}

// Remove removes the specified callback function from the callback linked list
// Parameters:
//   - handler: Handler identifier of the callback to be removed
//   - key: Unique identifier key of the callback to be removed
//
// Note: If no matching callback is found, this method has no effect
func (t *callbacks) Remove(handler, key any) {
	// Guard: avoid runtime panic on non-comparable types
	if !isComparable(handler) || !isComparable(key) {
		_ = log.Error(perrors.New(fmt.Sprintf("callbacks.Remove: non-comparable handler/key: %T, %T; ignored", handler, key)))
		return
	}

	var prev *callbackNode

	// Traverse linked list to find the node to be removed
	for callback := t.first; callback != nil; prev, callback = callback, callback.next {
		// Found matching node
		if callback.handler == handler && callback.key == key {
			if t.first == callback {
				// If it's the first node, update first pointer
				t.first = callback.next
			} else if prev != nil {
				// If it's a middle node, update the next pointer of the previous node
				prev.next = callback.next
			}

			if t.last == callback {
				// If it's the last node, update last pointer
				t.last = prev
			}

			// Decrement callback count
			t.cbNum--

			// Return immediately after finding and removing
			return
		}
	}
}

// Invoke executes all registered callback functions in the linked list
// Executes each callback in the order they were added
// Note: If a callback function is nil, it will be skipped
// If a callback panics, it will be handled by the outer caller's panic recovery
func (t *callbacks) Invoke() {
	// Traverse the entire linked list starting from the head node
	for callback := t.first; callback != nil; callback = callback.next {
		// Ensure callback function is not nil before executing
		if callback.call != nil {
			callback.call()
		}
	}
}

// Len returns the number of callback functions in the linked list
// Return value: Total number of currently registered callback functions
func (t *callbacks) Len() int {
	return t.cbNum
}
