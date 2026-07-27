## kesh development tasks.
## Tool versions are pinned in .mise.toml; run `mise install` to provision.

.PHONY: tools fmt lint vet test build ci check release release-dry-run

## tools: install editor-side helpers (golangci-lint itself comes via mise).
tools:
	go install golang.org/x/tools/cmd/goimports@latest

## fmt: apply gofmt + goimports (writes changes in place).
fmt:
	golangci-lint fmt

## lint: run all enabled analyzers.
lint:
	golangci-lint run ./...

## vet: go vet, kept as an explicit guarantee from AGENTS.md.
vet:
	go vet ./...

## test: race-enabled test suite.
test:
	go test -race ./...

## build: compile the CLI into ./bin.
build:
	go build -o ~/.local/bin/kesh ./cmd/kesh

## ci: the full local gate — lint, test, build.
ci: lint test build

## check: quick formatting sanity (non-zero exit if not formatted).
check:
	@out=$$(golangci-lint fmt --diff ./... 2>&1); \
	if [ -n "$$out" ]; then echo "$$out"; echo "run 'make fmt'"; exit 1; fi

## release: run a snapshot, then prompt before pushing a version tag.
release:
	@if [ -n "$(VERSION)" ] && [ -n "$(BUMP)" ]; then echo "set VERSION or BUMP, not both"; exit 1; fi
	@test -n "$(VERSION)$(BUMP)" || (echo "usage: make release VERSION=vX.Y.Z or BUMP=patch|minor|major [RELEASE_FLAGS=-y]"; exit 1)
	$(if $(BUMP),go run ./cmd/release $(RELEASE_FLAGS) --bump $(BUMP),go run ./cmd/release $(RELEASE_FLAGS) $(VERSION))

## release-dry-run: build release artifacts without prompting or publishing.
release-dry-run:
	@if [ -n "$(VERSION)" ] && [ -n "$(BUMP)" ]; then echo "set VERSION or BUMP, not both"; exit 1; fi
	@test -n "$(VERSION)$(BUMP)" || (echo "usage: make release-dry-run VERSION=vX.Y.Z or BUMP=patch|minor|major"; exit 1)
	$(if $(BUMP),go run ./cmd/release --dry-run --bump $(BUMP),go run ./cmd/release --dry-run $(VERSION))
