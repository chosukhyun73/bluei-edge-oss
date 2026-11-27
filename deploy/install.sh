#!/bin/bash
# bluei-edge installer — single-device edge appliance deployment
#
# Target: GX10 (Nvidia DGX Spark), amd64 servers, or any Linux with systemd.
# Idempotent: re-running upgrades the install in place.
#
# Layout after install:
#   /opt/bluei-edge/             — binary + dashboard + migrations (immutable artifacts)
#   /etc/bluei-edge/             — operator-editable config (edge.yaml + env)
#   /var/lib/bluei-edge/         — runtime data (sqlite DB, WAL, snapshots)
#   /etc/systemd/system/bluei-edge.service
#
# Usage:
#   sudo ./install.sh                       # install from sibling tarball
#   sudo ./install.sh -t /path/to/tar.gz    # install from explicit tarball
#   sudo ./install.sh --uninstall           # remove install (preserves /var/lib data)
#
# Requirements: root (sudo), systemd, tar.

set -euo pipefail

INSTALL_DIR="/opt/bluei-edge"
CONFIG_DIR="/etc/bluei-edge"
DATA_DIR="/var/lib/bluei-edge"
SERVICE_NAME="bluei-edge"
SVC_USER="bluei-edge"
SVC_GROUP="bluei-edge"

TARBALL=""
ACTION="install"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -t|--tarball) TARBALL="$2"; shift 2 ;;
    --uninstall)  ACTION="uninstall"; shift ;;
    -h|--help)
      sed -n '2,30p' "$0"; exit 0 ;;
    *)
      echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

require_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "this script must be run as root (try: sudo $0 $*)" >&2
    exit 1
  fi
}

require_root

# ── uninstall ─────────────────────────────────────────────────────────────────
if [[ "$ACTION" == "uninstall" ]]; then
  echo "→ stopping ${SERVICE_NAME}"
  systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
  systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
  systemctl daemon-reload || true
  rm -rf "${INSTALL_DIR}"
  echo "→ uninstall complete."
  echo "  Config preserved: ${CONFIG_DIR}/"
  echo "  Data preserved:   ${DATA_DIR}/"
  echo "  Remove manually if you want a clean slate."
  exit 0
fi

# ── locate tarball ────────────────────────────────────────────────────────────
if [[ -z "$TARBALL" ]]; then
  HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  for cand in "$HERE"/../bluei-edge-*.tar.gz "$HERE"/bluei-edge-*.tar.gz; do
    if [[ -f "$cand" ]]; then TARBALL="$cand"; break; fi
  done
fi
if [[ -z "$TARBALL" || ! -f "$TARBALL" ]]; then
  echo "no tarball found. Pass with -t /path/to/bluei-edge-VERSION.tar.gz" >&2
  exit 1
fi
echo "→ installing from: $TARBALL"

# ── user/group ───────────────────────────────────────────────────────────────
if ! id -u "$SVC_USER" >/dev/null 2>&1; then
  echo "→ creating service user: $SVC_USER"
  useradd --system --no-create-home --shell /usr/sbin/nologin --user-group "$SVC_USER"
fi

# ── directories ──────────────────────────────────────────────────────────────
mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$DATA_DIR"
chown -R "$SVC_USER:$SVC_GROUP" "$DATA_DIR"

# ── extract tarball ──────────────────────────────────────────────────────────
echo "→ extracting to $INSTALL_DIR"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT
tar -xzf "$TARBALL" -C "$TMPDIR"
SRC="$(ls -d "$TMPDIR"/bluei-edge-* | head -1)"
if [[ -z "$SRC" ]]; then
  echo "tarball did not contain a bluei-edge-* directory" >&2
  exit 1
fi

rm -rf "$INSTALL_DIR/bin" "$INSTALL_DIR/migrations" "$INSTALL_DIR/web" "$INSTALL_DIR/configs"
cp -r "$SRC/bin"         "$INSTALL_DIR/"
cp -r "$SRC/migrations"  "$INSTALL_DIR/"
cp -r "$SRC/web"         "$INSTALL_DIR/"
# configs/ 예시 라이브러리(읽기전용 틀 자산) — 런타임이 WorkingDirectory 상대로
# 읽는다(예: configs/vision-algorithms.example.yaml, 학습·모델관리 탭). 운영자
# 편집용 config 는 /etc/bluei-edge/edge.yaml 로 별도 (아래 config seed). 이게 없으면
# 학습 데이터 탭이 파일 부재로 에러 — 신규 현장 학습 워크플로 진입점이 막힌다.
cp -r "$SRC/configs"     "$INSTALL_DIR/"
# services/ — LRCN 추론 서버 소스 (틀 자산 → 업그레이드 시 교체). venv 는 별도
# (deploy/setup-lrcn.sh 가 생성 — local-ai/venv 라 이 rm 대상이 아니어서 보존).
if [[ -d "$SRC/services" ]]; then
  rm -rf "$INSTALL_DIR/services"
  cp -r "$SRC/services" "$INSTALL_DIR/"
fi
# deploy/ — setup-ollama.sh·setup-lrcn.sh 등 (docs/51 이 /opt/bluei-edge/deploy/ 경로로 안내).
# docs/operator-guide — 어시스턴트 도움말이 읽는 가이드북 (없으면 "가이드북에서 확인되지 않습니다").
for d in deploy docs; do
  if [[ -d "$SRC/$d" ]]; then
    rm -rf "$INSTALL_DIR/$d"
    cp -r "$SRC/$d" "$INSTALL_DIR/"
  fi
done
chmod +x "$INSTALL_DIR/bin/"*

# ── AI 자산 경계 (틀만 이식, 지능은 현장에서 자란다) ───────────────────────────
# 학습 씨앗(baseline)은 틀의 일부 → 매 업그레이드 시 갱신.
# 현장 학습 산출물(models/active 매니페스트 · training/runs)은 현장 소유 →
# 절대 삭제하지 않는다 (위 rm -rf 대상은 bin/migrations/web 뿐). 신규 현장은
# active 매니페스트가 없어 룰 기반으로 콜드스타트하고, 운영자 라벨링→학습→promote
# 로 스스로 채운다.
if [[ -d "$SRC/local-ai/models/blueocean-baseline" ]]; then
  echo "→ seeding training baseline (immutable 틀 asset)"
  mkdir -p "$INSTALL_DIR/local-ai/models/blueocean-baseline"
  cp -rf "$SRC/local-ai/models/blueocean-baseline/." \
    "$INSTALL_DIR/local-ai/models/blueocean-baseline/"
fi
# 현장 학습 디렉토리 보장 (없을 때만 생성 — 기존 학습물 보존).
mkdir -p "$INSTALL_DIR/local-ai/models/active" "$INSTALL_DIR/local-ai/training/runs"

# ── config seed ──────────────────────────────────────────────────────────────
if [[ ! -f "$CONFIG_DIR/edge.yaml" ]]; then
  # 신규 현장 템플릿을 seed — 채우기 전엔 관찰/dry-run 모드로 안전 가동
  # (실장치 제어·실급이·동기화는 운영자가 값 채우고 명시적으로 켤 때까지 OFF).
  echo "→ seeding config: $CONFIG_DIR/edge.yaml (신규 현장 템플릿 — 편집 필요)"
  cp "$SRC/configs/edge.new-site.example.yaml" "$CONFIG_DIR/edge.yaml"
  for f in "$SRC"/configs/*.example.yaml; do
    base="$(basename "$f")"
    # edge 마스터 config 예시(edge.example / edge.new-site)는 /etc 에 seed 안 함
    # — 위에서 edge.yaml 로 이미 seed 했다.
    case "$base" in edge.example.yaml|edge.new-site.example.yaml) continue ;; esac
    [[ -f "$CONFIG_DIR/$base" ]] || cp "$f" "$CONFIG_DIR/$base"
  done
else
  echo "→ existing config preserved: $CONFIG_DIR/edge.yaml"
fi

if [[ ! -f "$CONFIG_DIR/env" ]]; then
  TOKEN="$(openssl rand -hex 32 2>/dev/null || head -c 32 /dev/urandom | xxd -p)"
  cat > "$CONFIG_DIR/env" <<EOF
# bluei-edge operator API token. Keep this file secret.
BLUEI_EDGE_OPERATOR_TOKEN=${TOKEN}
EOF
  chmod 600 "$CONFIG_DIR/env"
  echo "→ generated operator token: $CONFIG_DIR/env"
fi

chown -R root:"$SVC_GROUP" "$CONFIG_DIR"
chmod 750 "$CONFIG_DIR"

# ── systemd unit ─────────────────────────────────────────────────────────────
echo "→ installing systemd unit"
cp "$SRC/deploy/systemd/${SERVICE_NAME}.service" "/etc/systemd/system/"
systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"

if systemctl is-active --quiet "${SERVICE_NAME}"; then
  echo "→ restarting ${SERVICE_NAME}"
  systemctl restart "${SERVICE_NAME}"
else
  echo "→ starting ${SERVICE_NAME}"
  systemctl start "${SERVICE_NAME}"
fi

sleep 1
if systemctl is-active --quiet "${SERVICE_NAME}"; then
  echo "✓ ${SERVICE_NAME} is running"
  PORT="$(grep -E '^\s*port:' "$CONFIG_DIR/edge.yaml" | awk '{print $2}' | head -1)"
  echo "  dashboard: http://$(hostname -I | awk '{print $1}'):${PORT:-8080}/"
  echo "  config:    $CONFIG_DIR/edge.yaml"
  echo "  data:      $DATA_DIR/"
  echo "  logs:      journalctl -u ${SERVICE_NAME} -f"
else
  echo "✗ ${SERVICE_NAME} failed to start" >&2
  journalctl -u "${SERVICE_NAME}" -n 30 --no-pager
  exit 1
fi
