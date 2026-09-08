SHELL := /bin/sh
export GOTOOLCHAIN := local
export CGO_ENABLED := 0

.PHONY: check toolchain fmt vet test race build package arch-source generate generated openapi manifests security browser systemd

check: toolchain fmt vet test race generated openapi manifests build security systemd

toolchain:
	@test "$$(go env GOVERSION)" = "go$$(tr -d '\n' < .go-version)" || { echo 'Use the exact Go version in .go-version'; exit 1; }

fmt:
	@test -z "$$(gofmt -l $$(rg --files -g '*.go'))" || { gofmt -l $$(rg --files -g '*.go'); exit 1; }

vet:
	go vet ./...

test:
	go test ./...

race:
	CGO_ENABLED=1 go test -race ./...

build:
	@mkdir -p bin
	go build -trimpath -buildvcs=false -o bin/bridged ./cmd/bridged
	go build -trimpath -buildvcs=false -o bin/bridgectl ./cmd/bridgectl
	go build -trimpath -buildvcs=false -o bin/bridge-hostd ./cmd/bridge-hostd
	go build -trimpath -buildvcs=false -o bin/bridge-worker ./cmd/bridge-worker

package: toolchain
	@mkdir -p dist/linux-amd64 dist/darwin-arm64 dist/darwin-amd64
	GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o dist/linux-amd64/bridged ./cmd/bridged
	GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o dist/linux-amd64/bridge-hostd ./cmd/bridge-hostd
	GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o dist/linux-amd64/bridge-worker ./cmd/bridge-worker
	GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o dist/linux-amd64/bridgectl ./cmd/bridgectl
	GOOS=darwin GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o dist/darwin-arm64/bridgectl ./cmd/bridgectl
	GOOS=darwin GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o dist/darwin-amd64/bridgectl ./cmd/bridgectl
	go run ./cmd/bridge-package

# arch-source creates source-only inputs for a reviewed local makepkg run. It
# never installs a package or starts a service. VERSION must be a stable tag.
arch-source: export BRIDGE_ARCH_VERSION := $(value VERSION)
arch-source: toolchain
	@test -n "$$BRIDGE_ARCH_VERSION" || { echo 'Set VERSION to vMAJOR.MINOR.PATCH'; exit 1; }
	go run ./cmd/bridge-arch-package --version "$$BRIDGE_ARCH_VERSION"

generate:
	go run ./cmd/bridge-apigen

generated:
	go run ./cmd/bridge-apigen --check
	go mod tidy -diff

openapi:
	go tool validate api/openapi.json

manifests:
	go run ./cmd/bridge-validate

security:
	go mod verify
	go tool govulncheck ./...

browser: build
	node scripts/browser-test.mjs

systemd:
	@if [ "$$(uname -s)" = Linux ] && command -v systemd-analyze >/dev/null 2>&1; then systemd-analyze verify deployment/systemd/*.service deployment/systemd/*.socket; else echo 'NOT RUN — compatible Linux systemd unavailable'; fi
