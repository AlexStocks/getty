# Licensed to the Apache Software Foundation (ASF) under one or more
# contributor license agreements.  See the NOTICE file distributed with
# this work for additional information regarding copyright ownership.
# The ASF licenses this file to You under the Apache License, Version 2.0
# (the "License"); you may not use this file except in compliance with
# the License.  You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

SHELL := bash
.DELETE_ON_ERROR:
.DEFAULT_GOAL := help
.SHELLFLAGS := -eu -o pipefail -c
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules
MAKEFLAGS += --no-print-directory

.PHONY: help test test-race fmt check-fmt clean lint install-golangci-lint install-imports-formatter

help:
	@echo "Available commands:"
	@echo "  test       - Run unit tests with coverage"
	@echo "  test-race  - Run transport race tests"
	@echo "  fmt        - Format code"
	@echo "  check-fmt  - Verify code formatting"
	@echo "  lint       - Run golangci-lint"
	@echo "  clean      - Clean test generate files"

# Run unit tests
test: clean
	GOTOOLCHAIN=go1.25.0+auto go test ./... -count=1 -coverprofile=coverage.txt -covermode=atomic

test-race:
	GOTOOLCHAIN=go1.25.0+auto go test -race ./transport -count=1

fmt: install-imports-formatter
	go fmt ./... && GOROOT=$(shell go env GOROOT) imports-formatter

check-fmt: fmt
	@git diff --exit-code -- . ':!coverage.txt' || { \
		echo "Formatting changes are required. Run 'make fmt'."; \
		exit 1; \
	}

# Clean test generate files
clean:
	rm -rf coverage.txt

# Run golangci-lint
lint: install-golangci-lint
	go vet ./...
	golangci-lint run ./... --timeout=10m

install-golangci-lint:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.4.0

install-imports-formatter:
	go install github.com/dubbogo/tools/cmd/imports-formatter@v1.0.10
