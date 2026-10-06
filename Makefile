export GOTOOLCHAIN := local

PWD := $(shell pwd)
GOPATH := $(shell go env GOPATH)
LDFLAGS := $(shell go run buildscripts/gen-ldflags.go)

GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

BUILD_LDFLAGS := '$(LDFLAGS)'

VERSION ?= $(shell git describe --tags)
TAG ?= "soulteary/oc:$(VERSION)"

all: build

checks:
	@echo "Checking dependencies"
	@(env bash $(PWD)/buildscripts/checkdeps.sh)

getdeps:
	@mkdir -p ${GOPATH}/bin
	@which golangci-lint 1>/dev/null || (echo "Installing golangci-lint" && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2)
	@which stringer 1>/dev/null || (echo "Installing stringer" && go install golang.org/x/tools/cmd/stringer@latest)

crosscompile:
	@(env bash $(PWD)/buildscripts/cross-compile.sh)

verifiers: getdeps vet fmt lint

docker: build
	@docker build -t $(TAG) . -f Dockerfile.dev

vet:
	@echo "Running $@"
	@GO111MODULE=on go vet github.com/soulteary/mc/...

fmt:
	@echo "Running $@"
	@GO111MODULE=on gofmt -d cmd/
	@GO111MODULE=on gofmt -d pkg/

lint:
	@echo "Running $@ check"
	@GO111MODULE=on ${GOPATH}/bin/golangci-lint run --timeout=5m --config ./.golangci.yml

# Builds OC, runs the verifiers then runs the tests.
check: test
test: verifiers build
	@echo "Running unit tests"
	@GO111MODULE=on CGO_ENABLED=0 go test -tags kqueue ./... 1>/dev/null
	@echo "Running functional tests"
	@(env bash $(PWD)/functional-tests.sh)

test-race: verifiers build
	@echo "Running unit tests under -race"
	@GO111MODULE=on go test -race -v --timeout 20m ./...

# Verify OC binary
verify:
	@echo "Verifying build with race"
	@GO111MODULE=on CGO_ENABLED=1 go build -race -tags kqueue -trimpath --ldflags "$(LDFLAGS)" -o $(PWD)/oc 1>/dev/null
	@echo "Running functional tests"
	@(env bash $(PWD)/functional-tests.sh)

# Builds OC locally.
build: checks
	@echo "Building OC binary to './oc'"
	@GO111MODULE=on CGO_ENABLED=0 go build -trimpath -tags kqueue --ldflags $(BUILD_LDFLAGS) -o $(PWD)/oc

# Builds OC and installs it to $GOPATH/bin.
install: build
	@echo "Installing OC binary to '$(GOPATH)/bin/oc'"
	@mkdir -p $(GOPATH)/bin && cp -f $(PWD)/oc $(GOPATH)/bin/oc
	@echo "Installation successful. To learn more, try \"oc --help\"."

clean:
	@echo "Cleaning up all the generated files"
	@find . -name '*.test' | xargs rm -fv
	@find . -name '*~' | xargs rm -fv
	@rm -rvf oc
	@rm -rvf build
	@rm -rvf release
