PROJECT_NAME := "tinyFaaS"
PKG := "github.com/OpenFogStack/$(PROJECT_NAME)"
GO_FILES := $(shell find . -name '*.go' | grep -v /vendor/ | grep -v /ext/ | grep -v _test.go)
TEST_DIR := ./test

.PHONY: all build start clean

all: build clean start

build: manager rproxy

manager rproxy: $(GO_FILES)
	@go build -o $@ -v $(PKG)/cmd/$@

start: manager rproxy
	./manager

test: build ${TEST_DIR}/test_all.py
	@python3 ${TEST_DIR}/test_all.py

clean: clean.sh
	@sh clean.sh

proto:
	@protoc -I mistify/registry/node mistify/registry/node/node.proto \
	--go_out=mistify/registry/node \
	--go-grpc_out=mistify/registry/node \
	--go_opt=paths=source_relative \
	--go-grpc_opt=paths=source_relative \
	node.proto