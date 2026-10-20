# SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

include ../../../build/makefile/runai-cluster.mk
include ../../../build/makefile/index.mk
DOCKER_WORK_DIR=${DOCKER_WORK_DIR_CLUSTER}

# Change for every new service
SERVICE_NAME=goofys
##@ Basic Commands

test: GO_TEST_PACKAGES=./mount-check
test: test-go
.PHONY: test

lint: lint-go ## Run linter.
.PHONY: lint

validate:
  #no-op
.PHONY: validate

build: build-go ## Build the service and startup probe.
.PHONY: build

build-go-amd: build-mount-check-amd
build-go-arm: build-mount-check-arm

build-mount-check-amd: git-config-command format-go gocache
	${DOCKER_GO_COMMAND} env GOFIPS140=v1.0.0 go build -trimpath -ldflags='-s -w' -o ${BUILD_DIR}/s3-mount-check-amd64 ./mount-check

build-mount-check-arm: git-config-command format-go gocache
	${DOCKER_GO_COMMAND_ARM} env GOFIPS140=v1.0.0 go build -trimpath -ldflags='-s -w' -o ${BUILD_DIR}/s3-mount-check-arm64 ./mount-check
.PHONY: build-mount-check-amd build-mount-check-arm
