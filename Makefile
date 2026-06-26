# bluei-edge — single-binary edge appliance build
#
# Targets:
#   make build         — local dev build (host arch)
#   make build-arm64   — cross-compile for GX10 / DGX Spark (linux/arm64)
#   make build-amd64   — cross-compile for amd64 servers
#   make dashboard     — build the React dashboard (web/dashboard/dist)
#   make package       — produce a deployable tarball in dist/
#   make test          — go test ./... + tsc + vite build
#   make clean         — remove build artifacts
#   make run           — local foreground run (uses configs/edge.example.yaml)
#
# Production layout (after `make build && make dashboard`):
#   bin/bluei-edge                       — Go binary
#   web/dashboard/dist/                  — dashboard static files (served by bluei-edge :8080)
#   migrations/*.sql                     — SQL migrations (loaded at startup)
#   configs/edge.example.yaml            — seed config
#
# Single-binary embed of dist/ + migrations is planned in Phase 3.

SHELL          := /bin/bash
# Go가 user-local 설치(~/.local/go/bin)에 있는 경우도 자동 인식.
export PATH    := $(HOME)/.local/go/bin:$(PATH)
GO             ?= go
NPM            ?= npm
BIN_DIR        := bin
DIST_DIR       := dist
GO_PKG         := ./cmd/bluei-edge
BIN_NAME       := bluei-edge
DASHBOARD_DIR  := web/dashboard
VERSION        := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS        := -s -w -X main.version=$(VERSION)

.PHONY: all build build-arm64 build-amd64 dashboard package test clean run check

all: build dashboard

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/$(BIN_NAME) $(GO_PKG)
	@echo "→ $(BIN_DIR)/$(BIN_NAME) (host)"

# modernc.org/sqlite is pure Go → CGO_ENABLED=0 cross-compiles with no C toolchain.
build-arm64:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
		$(GO) build -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/$(BIN_NAME)-linux-arm64 $(GO_PKG)
	@echo "→ $(BIN_DIR)/$(BIN_NAME)-linux-arm64 (GX10 / DGX Spark target)"

build-amd64:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		$(GO) build -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/$(BIN_NAME)-linux-amd64 $(GO_PKG)
	@echo "→ $(BIN_DIR)/$(BIN_NAME)-linux-amd64"

dashboard:
	cd $(DASHBOARD_DIR) && $(NPM) install --no-audit --no-fund && $(NPM) run build
	@echo "→ $(DASHBOARD_DIR)/dist/ (static)"

package: build dashboard
	@rm -rf $(DIST_DIR)
	@mkdir -p $(DIST_DIR)/bluei-edge-$(VERSION)/{bin,migrations,configs,web/dashboard,deploy}
	@cp $(BIN_DIR)/$(BIN_NAME)            $(DIST_DIR)/bluei-edge-$(VERSION)/bin/
	@cp -r migrations/*.sql               $(DIST_DIR)/bluei-edge-$(VERSION)/migrations/
	@cp configs/*.example.yaml            $(DIST_DIR)/bluei-edge-$(VERSION)/configs/
	@cp -r $(DASHBOARD_DIR)/dist          $(DIST_DIR)/bluei-edge-$(VERSION)/web/dashboard/
	@if [ -d deploy ]; then cp -r deploy/* $(DIST_DIR)/bluei-edge-$(VERSION)/deploy/ 2>/dev/null || true; fi
	tar -czf $(DIST_DIR)/bluei-edge-$(VERSION).tar.gz -C $(DIST_DIR) bluei-edge-$(VERSION)
	@echo "→ $(DIST_DIR)/bluei-edge-$(VERSION).tar.gz"

test:
	$(GO) test -count=1 ./...
	cd $(DASHBOARD_DIR) && $(NPM) run build

check: test
	@gofmt -l internal/ cmd/ | tee /tmp/gofmt.out
	@test ! -s /tmp/gofmt.out

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR) $(DASHBOARD_DIR)/dist

run: build
	BLUEI_EDGE_OPERATOR_TOKEN=$${BLUEI_EDGE_OPERATOR_TOKEN:-devtoken} \
		$(BIN_DIR)/$(BIN_NAME) run -config configs/edge.example.yaml
