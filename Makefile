GO ?= go
BINARY ?= kabby
SRC := ./cmd/kabby

.PHONY: build test dev clean vet prepublishOnly cross release tidy bench

build: tidy
	$(GO) build -o $(BINARY) $(SRC)

test: tidy
	$(GO) test ./...

vet: tidy
	$(GO) vet ./...

dev: tidy
	$(GO) run $(SRC)

tidy:
	$(GO) mod tidy

clean:
	$(GO) clean
	rm -f $(BINARY)
	rm -rf dist

prepublishOnly: vet test
	@echo "prepublishOnly: vet and test passed"

# Cross-compile static release binaries (no CGO, no runtime deps)
RELEASE_TARGETS := linux/amd64 darwin/amd64 darwin/arm64 windows/amd64
cross: tidy
	@for target in $(RELEASE_TARGETS); do \
		goos=$${target%/*}; goarch=$${target#*/}; \
		ext=""; [ "$$goos" = "windows" ] && ext=".exe"; \
		GOOS=$$goos GOARCH=$$goarch CGO_ENABLED=0 $(GO) build -o dist/kabby-$$goos-$$goarch$$ext $(SRC); \
	done
	@echo "cross: static binaries in dist/"

release: clean vet test cross
	@echo "release: binaries in dist/"

bench:
	@echo "bench: see bench/run.sh for scenarios"
