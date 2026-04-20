SHELL := /bin/bash
.DEFAULT_GOAL := help

LISTEN ?= :18080
DATA_ROOT ?= ../http-mock-data
ROUTES ?= routes.yaml

.PHONY: help fmt test build run validate

help: ## 显示常用命令
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt: ## Go 代码格式化
	go fmt ./...

test: ## 运行测试
	go test ./...

build: ## 编译检查
	go build ./cmd/http-mock

run: ## 启动 http-mock 服务
	go run ./cmd/http-mock serve --routes "$(ROUTES)" --data-root "$(DATA_ROOT)" --listen "$(LISTEN)"

validate: ## 校验 routes.yaml 和 responses/*
	go run ./cmd/http-mock validate --routes "$(ROUTES)" --data-root "$(DATA_ROOT)"
