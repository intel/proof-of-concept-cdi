# Copyright 2017 The Kubernetes Authors.
# Copyright 2020 Intel Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

GO_BINARY=go
GO=GOOS=linux CGO_ENABLED=0 GO111MODULE=on $(GO_BINARY)
IMPORT_PATH=github.com/intel/cdi
CMDS=cdi-driver cdi-runc
TEST_CMDS=$(addsuffix -test,$(CMDS))
SHELL=bash
export PWD=$(shell pwd)
OUTPUT_DIR=_output
.DELETE_ON_ERROR:

ifeq ($(VERSION), )
VERSION=$(shell git describe --long --dirty --tags --match='v*')
endif

IMAGE_VERSION?=canary
IMAGE_TAG=cdi-driver$*:$(IMAGE_VERSION)

BUILD_ARGS:=--build-arg VERSION=${VERSION}

# An alias for "make build" and the default target.
all: build

# Build all binaries, including tests.
build: $(CMDS) $(TEST_CMDS) check-go-version-$(GO_BINARY)

# "make test" runs a variety of fast tests.
test:
	@rm -rf $$(pwd)/_work/evil-ca
	@rm -rf $$(pwd)/_work/cdi-ca
	@WORKDIR=$$(pwd)/_work/evil-ca ./deploy/setup-ca.sh
	@WORKDIR=$$(pwd)/_work/cdi-ca ./deploy/setup-ca.sh
	@cp _work/evil-ca/cdi-node-controller.pem _work/cdi-ca/wrong-node-controller.pem
	@cp _work/evil-ca/cdi-node-controller-key.pem _work/cdi-ca/wrong-node-controller-key.pem
	TEST_WORK=$$(pwd)/_work $(GO) test ./pkg/...

# Build production binaries.
$(CMDS): check-go-version-$(GO_BINARY)
	$(GO) build -ldflags '-X github.com/intel/cdi/pkg/$@.version=${VERSION} -s -w' -a -o ${OUTPUT_DIR}/$@ ./cmd/$@

# Build a test binary that can be used instead of the normal one with
# additional "-run" parameters. In contrast to the normal it then also
# supports -test.coverprofile.
$(TEST_CMDS): %-test: check-go-version-$(GO_BINARY)
	$(GO) test --cover -covermode=atomic -c -coverpkg=./pkg/... -ldflags '-X github.com/intel/cdi/pkg/$*.version=${VERSION}' -o ${OUTPUT_DIR}/$@ ./cmd/$*

# Build images
image:
	# This ensures that all sources are available in the "vendor" directory for use
	# inside "docker build".
	go mod tidy
	go mod vendor
	docker build --build-arg GOFLAGS=-mod=vendor $(BUILD_ARGS) -t $(IMAGE_TAG) -f ./Dockerfile . --label revision=$(VERSION)

clean:
	$(GO) clean -r -x ./cmd/...
	-rm -rf $(OUTPUT_DIR) vendor

.PHONY: all build test clean $(CMDS) $(TEST_CMDS)

# Kustomize latest release version
KUSTOMIZE_VERSION=v3.8.0
_work/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz:
	@mkdir _work
	curl -L https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize/${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz -o $(abspath $@)

_work/kustomize: _work/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz
	tar xzf $< -C _work
	touch $@

# We generate deployment files with kustomize and include the output
# in the git repo because not all users will have kustomize or it
# might be an unsuitable version. When any file changes, update the
# output.
KUSTOMIZE_INPUT := $(shell [ ! -d deploy/kustomize ] || find deploy/kustomize -type f)

# Output files and their corresponding kustomize target.
# The "testing" flavor of the generated files contains both
# the loglevel changes and enables coverage data collection.
KUSTOMIZE_OUTPUT :=
KUSTOMIZE_OUTPUT += deploy/kubernetes-1.18/cdi.yaml
KUSTOMIZATION_deploy/kubernetes-1.18/cdi.yaml = deploy/kustomize/kubernetes-1.18

kustomize: _work/kustomize clean_kustomize_output $(KUSTOMIZE_OUTPUT)

$(KUSTOMIZE_OUTPUT): _work/kustomize $(KUSTOMIZE_INPUT)
	$< build --load_restrictor none $(KUSTOMIZATION_$@) >$@

clean_kustomize_output:
	rm -f $(KUSTOMIZE_OUTPUT)

# Always re-generate the output files because "git rebase" might have
# left us with an inconsistent state.
.PHONY: kustomize $(KUSTOMIZE_OUTPUT)

.PHONY: clean-kustomize
clean: clean-kustomize
clean-kustomize:
	rm -f _work/kustomize-*
	rm -f _work/kustomize

.PHONY: test-kustomize $(addprefix test-kustomize-,$(KUSTOMIZE_OUTPUT))
test: test-kustomize
test-kustomize: $(addprefix test-kustomize-,$(KUSTOMIZE_OUTPUT))
$(addprefix test-kustomize-,$(KUSTOMIZE_OUTPUT)): test-kustomize-%: _work/kustomize
	@ if ! diff <($< build --load_restrictor none $(KUSTOMIZATION_$*)) $*; then echo "$* was modified manually" && false; fi

# Targets in the makefile can depend on check-go-version-<path to go binary>
# to trigger a warning if the x.y version of that binary does not match
# what the project uses. Make ensures that this is only checked once per
# invocation.
.PHONY: check-go-version-%
check-go-version-%:
	@ hack/verify-go-version.sh "$*"
