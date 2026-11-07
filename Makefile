PACKAGE_NAME          := github.com/vale-cli/vale/v3
GOLANG_CROSS_VERSION  ?= v0.2.0

SYSROOT_DIR     ?= sysroots
SYSROOT_ARCHIVE ?= sysroots.tar.bz2

LAST_TAG := $(shell git describe --abbrev=0 --tags)
CURR_SHA := $(shell git rev-parse --verify HEAD)

LDFLAGS := -ldflags "-s -w -X main.version=$(LAST_TAG)"

EXE := $(if $(filter Windows_NT,$(OS)),.exe,)

DOCKER_BUILD_TARGETS := linux/arm64,linux/amd64
DOCKER_USER  ?= jdkato

.PHONY: data test lint install rules bench compare release choco-cross grammars

all: build

# make release tag=v0.4.3
release:
	git tag $(tag)
	git push origin $(tag)

# If os and/or arch are not set, default values are used which are set by the system used for building
# make build os=darwin
# make build os=windows
# make build os=linux
# make build os=darwin arch=arm64
# make build os=windows arch=amd64
# make build os=linux arch=amd64
# make build os=linux arch=arm64
build:
	GOOS=$(os) GOARCH=$(arch) go build ${LDFLAGS} -o bin/$(exe) ./cmd/vale

# The browser build: no CGo, so no tree-sitter, and no external converters.
# `wasm_exec.js` is the loader that ships with the Go toolchain that built it.
wasm:
	GOOS=js GOARCH=wasm go build ${LDFLAGS} -o bin/vale.wasm ./cmd/valew
	install -m 0644 "$$(go env GOROOT)/lib/wasm/wasm_exec.js" bin/wasm_exec.js

test-wasm: wasm
	node testdata/wasm/smoke.mjs bin/wasm_exec.js bin/vale.wasm

bench:
	go test -bench=. -benchmem ./internal/core ./internal/lint ./internal/check

profile:
	go test -benchmem -run=^$$ -bench ^BenchmarkLintMD$$ github.com/vale-cli/vale/v3/internal/lint -cpuprofile=bin/cpu.out -memprofile=bin/mem.out -trace=bin/trace.out
	mv lint.test bin

# go install github.com/aclements/go-misc/benchmany@latest
# go install golang.org/x/tools/cmd/benchcmp@latest
# go install golang.org/x/perf/cmd/benchstat@latest
compare:
	cd internal/lint && \
	benchmany -n 10 -o new.txt ${CURR_SHA} && \
	benchmany -n 10 -o old.txt ${LAST_TAG} && \
	benchstat old.txt new.txt

# `go test` throws away a passing package's output, which would take the
# end-to-end progress display with it -- so build that suite and run it
# directly, and leave the rest to `go test`.
test:
	go test `go list ./... | grep -v /internal/e2e`
	go test -c -o bin/e2e.test$(EXE) ./internal/e2e
	./bin/e2e.test$(EXE) $(run)

docker:
	@echo ${DOCKER_PASS} | docker login -u ${DOCKER_USER} --password-stdin

	# Ignore command failure
	-docker buildx create \
		--name container-builder \
		--driver docker-container \
		--use --bootstrap

	docker buildx build \
		--build-arg ltag=${LAST_TAG} \
		--platform=${DOCKER_BUILD_TARGETS} \
		--file Dockerfile \
		--tag ${DOCKER_USER}/vale:${LAST_TAG} \
		--tag ${DOCKER_USER}/vale:latest \
		--push \
		.

	docker buildx rm container-builder #Tidy up

choco-cross:
	@docker run \
		--rm \
		-e CGO_ENABLED=1 \
		--env-file .release-env \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/$(PACKAGE_NAME) \
		-v `pwd`/sysroot:/sysroot \
		-w /go/src/$(PACKAGE_NAME) \
		jdkato/choco-cross:${GOLANG_CROSS_VERSION} \
		release --clean

# Compile every tree-sitter grammar to a WebAssembly module (needs zig). The
# tests read them from grammars/dist.
grammars:
	grammars/build.sh
