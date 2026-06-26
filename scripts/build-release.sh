#!/usr/bin/env bash
#
# build-release.sh — 공개 릴리스용 멀티아치 tarball 생성 (linux/arm64 + linux/amd64)
#
# modernc.org/sqlite(순수 Go)라 CGO 없이 크로스컴파일된다 → C 툴체인 불필요.
# 결과물: dist/bluei-edge-<VERSION>-linux-<arch>.tar.gz
#   각 tarball 내부 구조는 deploy/install.sh 가 기대하는 형태:
#     bluei-edge-<VERSION>/{bin/bluei-edge, migrations, configs, web/dashboard/dist, deploy}
#
# 사용법:
#   scripts/build-release.sh v0.1.0     # 버전 지정
#   scripts/build-release.sh            # git describe 자동
#
# 설치(디바이스): tar -xzf <tarball> && sudo bluei-edge-<VERSION>/deploy/install.sh -t <tarball>

set -euo pipefail
export PATH="$HOME/.local/go/bin:$PATH"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
BIN_NAME="bluei-edge"
DIST="dist"
LDFLAGS="-s -w -X main.version=${VERSION}"

echo "▶ 릴리스 빌드: ${VERSION}"

# 1) 대시보드 정적 빌드 (호스트 무관, 1회)
echo "· dashboard 빌드"
( cd web/dashboard && npm install --no-audit --no-fund && npm run build )

rm -rf "$DIST"
for ARCH in arm64 amd64; do
  echo "· linux/${ARCH} 빌드·패키지"
  PKG="${BIN_NAME}-${VERSION}"
  STAGE="$DIST/_stage-${ARCH}/${PKG}"
  mkdir -p "$STAGE"/{bin,migrations,configs,web/dashboard,deploy}
  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" \
    go build -ldflags="$LDFLAGS" -o "$STAGE/bin/${BIN_NAME}" ./cmd/bluei-edge
  cp migrations/*.sql        "$STAGE/migrations/"
  cp configs/*.example.yaml  "$STAGE/configs/"
  cp -r web/dashboard/dist   "$STAGE/web/dashboard/"
  cp -r deploy/*             "$STAGE/deploy/"
  tar -czf "$DIST/${PKG}-linux-${ARCH}.tar.gz" -C "$DIST/_stage-${ARCH}" "$PKG"
  rm -rf "$DIST/_stage-${ARCH}"
  echo "  → $DIST/${PKG}-linux-${ARCH}.tar.gz"
done

echo "✓ 완료:"
ls -lh "$DIST"/*.tar.gz
