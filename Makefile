# bluei-edge — single-binary edge appliance build
#
# Targets:
#   make build         — local dev build (host arch)
#   make build-arm64   — cross-compile for GX10 / DGX Spark (linux/arm64)
#   make build-amd64   — cross-compile for amd64 servers
#   make dashboard     — build the React dashboard (web/dashboard/dist)
#   make package       — produce a deployable tarball in dist/ (host arch)
#   make package ARCH=arm64 — GX10 / DGX Spark 용 tarball (build-arm64 자동 선행)
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
# 학습 씨앗 — 신규 현장 전이학습 출발점(backbone). 유일하게 이식하는 가중치.
# 현장 학습 산출물(local-ai/models/active·training/runs)은 틀에 포함하지 않는다.
BASELINE_SEED  := local-ai/models/blueocean-baseline/bootstrap-2026-05-21-blueocean-final.pt

# package arch 선택 — `make package ARCH=arm64` 로 GX10 용 tarball 을 한 방에.
# 기본(ARCH 미지정)은 host arch, tarball 이름도 기존과 동일.
ifeq ($(ARCH),arm64)
PKG_BUILD := build-arm64
PKG_BIN   := $(BIN_DIR)/$(BIN_NAME)-linux-arm64
PKG_NAME  := bluei-edge-$(VERSION)-arm64
else ifeq ($(ARCH),amd64)
PKG_BUILD := build-amd64
PKG_BIN   := $(BIN_DIR)/$(BIN_NAME)-linux-amd64
PKG_NAME  := bluei-edge-$(VERSION)-amd64
else
PKG_BUILD := build
PKG_BIN   := $(BIN_DIR)/$(BIN_NAME)
PKG_NAME  := bluei-edge-$(VERSION)
endif

.PHONY: all build build-arm64 build-amd64 dashboard package test clean run check proce proce-build proce-app proce-run

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

# package — 신규 현장용 "틀" tarball. 담는 것: 바이너리·대시보드·migrations·config
# 예시·deploy 스크립트 + 학습 씨앗(baseline) 하나. 담지 않는 것: 강릉 학습 산출물
# (local-ai/models/active 매니페스트·training/runs). active 매니페스트를 넣지 않으므로
# 신규 현장은 룰 기반으로 콜드스타트하고, 지능은 현장에서 학습해 채운다.
package: $(PKG_BUILD) dashboard
	@rm -rf $(DIST_DIR)
	@mkdir -p $(DIST_DIR)/$(PKG_NAME)/{bin,migrations,configs,web/dashboard,deploy,docs/operator-guide,local-ai/models/blueocean-baseline,services/lrcn}
	@cp $(PKG_BIN)                        $(DIST_DIR)/$(PKG_NAME)/bin/$(BIN_NAME)
	@cp -r migrations/*.sql               $(DIST_DIR)/$(PKG_NAME)/migrations/
	@cp configs/*.example.yaml            $(DIST_DIR)/$(PKG_NAME)/configs/
	@cp -r $(DASHBOARD_DIR)/dist          $(DIST_DIR)/$(PKG_NAME)/web/dashboard/
	@# 운영자 가이드북 — 어시스턴트 search_guide 가 WorkingDirectory 상대 docs/operator-guide 를 읽는다.
	@cp docs/operator-guide/*.md         $(DIST_DIR)/$(PKG_NAME)/docs/operator-guide/
	@# LRCN 추론 서버 소스 — 신규 현장이 deploy/setup-lrcn.sh 로 8081 서버를 띄우는 데 필요.
	@cp services/lrcn/*.py services/lrcn/requirements.txt $(DIST_DIR)/$(PKG_NAME)/services/lrcn/
	@if [ -f $(BASELINE_SEED) ]; then \
		cp $(BASELINE_SEED) $(DIST_DIR)/$(PKG_NAME)/local-ai/models/blueocean-baseline/; \
		echo "→ 학습 씨앗 포함: $(notdir $(BASELINE_SEED))"; \
	else \
		echo "⚠ 학습 씨앗 없음($(BASELINE_SEED)) — 씨앗 없이 패키징 (현장 학습은 random init 로)"; \
	fi
	@if [ -d deploy ]; then cp -r deploy/* $(DIST_DIR)/$(PKG_NAME)/deploy/ 2>/dev/null || true; fi
	@# 운영 예약 작업 스크립트 — deploy/setup-ops.sh 가 cron 으로 등록.
	@mkdir -p $(DIST_DIR)/$(PKG_NAME)/deploy/ops
	@cp scripts/ollama-gpu-watch.sh scripts/archive-captures-nas.sh scripts/gpu-health-watch.sh scripts/gx10-temp-summary.sh $(DIST_DIR)/$(PKG_NAME)/deploy/ops/
	@cp web/dashboard/src-tauri/icons/128x128.png $(DIST_DIR)/$(PKG_NAME)/deploy/ops/bluei-edge.png
	tar -czf $(DIST_DIR)/$(PKG_NAME).tar.gz -C $(DIST_DIR) $(PKG_NAME)
	@echo "→ $(DIST_DIR)/$(PKG_NAME).tar.gz"

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

# ── processing-bluei — 가공공장 관리 서비스 (별도 실행 파일 + 별도 앱) ─────────────
# 양식 기능과 장비 제어는 들어가지 않는다. 저장소·이벤트 기록 코드는 bluei-edge 와 같이 쓴다.
PROCE_PKG      := ./cmd/processing-bluei
PROCE_BIN      := processing-bluei
PROCE_APP_DIR  := web/processing

proce: proce-build proce-app

proce-build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/$(PROCE_BIN) $(PROCE_PKG)
	@echo "→ $(BIN_DIR)/$(PROCE_BIN) (host)"

proce-app:
	cd $(PROCE_APP_DIR) && $(NPM) install --no-audit --no-fund && $(NPM) run build
	@echo "→ $(PROCE_APP_DIR)/dist/ (static)"

proce-run: proce-build
	$(BIN_DIR)/$(PROCE_BIN) run -config configs/proce.example.yaml
