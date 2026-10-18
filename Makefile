# The straum core checkout, for the drift, coverage and e2e checks.
CORE ?= ../straum
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: build test cover lint check cross e2e drift-check coverage-check golden clean docs release-snapshot

build:
	CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o bin/straum-cli .

test:
	go test -race ./...

# Statement coverage (go test -coverprofile), reported, not gated (ECO-D-7).
cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo 'gofmt: files above need formatting' >&2; exit 1; }
	go vet ./...
	go vet -tags e2e ./...

# Everything that needs no core checkout.
check: lint test cross

cross:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "build $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags '$(LDFLAGS)' -o dist/straum-cli-$$os-$$arch$$ext . || exit 1; \
	done

# A real three-node cluster on loopback, built from $(CORE).
e2e:
	cd $(CORE) && cargo build -p server --bin straum-server
	STRAUM_SERVER_BIN=$(abspath $(CORE))/target/debug/straum-server go test -tags e2e -race -count=1 -run E2E ./...

drift-check:
	scripts/check-drift.sh $(CORE)

coverage-check:
	scripts/check-coverage.sh $(CORE)

# Rewrite testdata/*.golden.* after an intended output change.
golden:
	go test -run TestGolden -update .

clean:
	rm -rf bin dist

# Rewrite docs/commands after a change to commands, flags or help text.
docs:
	go test -run TestCommandDocs -update .

GORELEASER := go run github.com/goreleaser/goreleaser/v2@v2.18.3

# Check .goreleaser.yaml and build every release archive locally, without
# publishing anything.
release-snapshot:
	$(GORELEASER) check
	$(GORELEASER) release --snapshot --clean
