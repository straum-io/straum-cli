# A Straum checkout at the commit in CONTRACT, for the live tests.
STRAUM ?= ../straum
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: build test cover lint check cross live golden docs clean release-check release-snapshot

build:
	CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o bin/straum-cli .

test:
	go test -race ./...

# Statement coverage (go test -coverprofile), reported, not gated (ECO-D-7).
cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

# gofmt, go vet and staticcheck; the tiers are in scripts/validate.sh.
lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo 'gofmt: files above need formatting' >&2; exit 1; }
	go vet ./...
	go vet -tags live ./...
	GOOS=windows go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 -tags live ./...

# Everything that needs no Straum checkout.
check: lint test cross

cross:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "build $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags '$(LDFLAGS)' -o dist/straum-cli-$$os-$$arch$$ext . || exit 1; \
	done

# The CLI against straum-broker and straum-fake-server built from $(STRAUM).
live:
	scripts/live.sh $(STRAUM) -race

# Rewrite testdata/*.golden* after an intended output change.
golden:
	go test . -run 'TestGolden|TestConfigNoFileDefaults' -update

# Rewrite docs/commands after a change to commands, flags or help text.
docs:
	go test -run TestCommandDocs -update .

clean:
	rm -rf bin dist

GORELEASER := go run github.com/goreleaser/goreleaser/v2@v2.18.3

# Check .goreleaser.yaml.
release-check:
	$(GORELEASER) check

# Build every release archive locally, without publishing anything.
release-snapshot: release-check
	$(GORELEASER) release --snapshot --clean
