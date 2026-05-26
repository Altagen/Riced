# Riced -- theme-as-code engine for KDE Plasma.
# All Go work happens inside Podman so the host stays clean.

GO_IMG      := docker.io/library/golang:1.23-alpine
LINT_IMG    := docker.io/golangci/golangci-lint:v1.62-alpine
TAPLO_IMG   := docker.io/tamasfe/taplo:latest

# Named volumes survive between runs → fast incremental builds.
GO_BUILD_VOL := riced-go-build
GO_MOD_VOL   := riced-go-mod

PODMAN_GO := podman run --rm \
    -v $(CURDIR):/src \
    -v $(GO_BUILD_VOL):/root/.cache/go-build \
    -v $(GO_MOD_VOL):/go/pkg/mod \
    -w /src \
    -e CGO_ENABLED=0 \
    $(GO_IMG)

PODMAN_GO_TTY := podman run --rm -it \
    -v $(CURDIR):/src \
    -v $(GO_BUILD_VOL):/root/.cache/go-build \
    -v $(GO_MOD_VOL):/go/pkg/mod \
    -w /src \
    -e CGO_ENABLED=0 \
    $(GO_IMG)

# --- Version metadata injected at build time --------------------------------
VERSION ?= $(shell git -C $(CURDIR) describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git -C $(CURDIR) rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
    -X main.version=$(VERSION) \
    -X main.commit=$(COMMIT) \
    -X main.date=$(DATE)

# --- Targets ----------------------------------------------------------------
.PHONY: build test lint tidy fmt vet shell clean help fmt-toml lint-toml

help: ## Show this help
	@awk 'BEGIN{FS=":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the riced binary into ./bin/riced (static, linux/amd64)
	@mkdir -p bin
	$(PODMAN_GO) go build -trimpath -ldflags="$(LDFLAGS)" -o bin/riced ./cmd/riced
	@echo "→ bin/riced built ($(VERSION))"

test: ## Run unit tests (no -race locally -- needs CGO; CI runs the race detector)
	$(PODMAN_GO) go test -count=1 ./...

lint: ## Run golangci-lint
	podman run --rm -v $(CURDIR):/src -w /src $(LINT_IMG) golangci-lint run

fmt-toml: ## Reformat every TOML file in the repo
	podman run --rm -v $(CURDIR):/src -w /src $(TAPLO_IMG) format

lint-toml: ## Check TOML formatting AND validate every theme.toml against the JSON Schema
	podman run --rm -v $(CURDIR):/src -w /src $(TAPLO_IMG) format --check
	podman run --rm -v $(CURDIR):/src -w /src $(TAPLO_IMG) lint

tidy: ## Tidy go.mod / go.sum
	$(PODMAN_GO) go mod tidy

fmt: ## gofmt the codebase
	$(PODMAN_GO) gofmt -w .

vet: ## go vet
	$(PODMAN_GO) go vet ./...

shell: ## Drop into a shell inside the Go container (useful for ad-hoc commands)
	$(PODMAN_GO_TTY) sh

clean: ## Remove build output
	rm -rf bin/ dist/

clean-cache: ## Wipe Podman named volumes (forces full re-download next time)
	-podman volume rm $(GO_BUILD_VOL) $(GO_MOD_VOL)

release-snapshot: ## Build a local snapshot release (no publish) via goreleaser in Podman
	podman run --rm -v $(CURDIR):/src -w /src \
	  -v $(GO_BUILD_VOL):/root/.cache/go-build \
	  -v $(GO_MOD_VOL):/go/pkg/mod \
	  ghcr.io/goreleaser/goreleaser:latest \
	  release --snapshot --clean --skip=publish
