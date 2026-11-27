#!/bin/bash
# bluei-lrcn setup — LRCN 비전 추론 서버(8081) 신규 현장 프로비저닝 (docs/51 §5.2)
#
# install.sh 이후 선택 실행. venv(수 GB)는 tarball 에 없으므로 여기서 생성한다.
# 인터넷 필요. GPU(CUDA) 권장 — 없으면 --cpu.
#
# Usage:
#   sudo ./setup-lrcn.sh              # venv 생성 + 의존성 + systemd 유닛 설치/기동 (cuda)
#   sudo ./setup-lrcn.sh --cpu        # GPU 없는 장비 (device=cpu)
#
# torch 주의: pip 기본 wheel 은 CUDA 동봉 x86_64 기준. GX10(ARM64/GB10) 등 플랫폼별
# cu 빌드가 필요하면 먼저 venv 에 맞는 torch 를 수동 설치한 뒤 이 스크립트를 재실행
# (이미 설치된 torch 는 건드리지 않는다).

set -euo pipefail

INSTALL_DIR="/opt/bluei-edge"
VENV="$INSTALL_DIR/local-ai/venv"
UNIT_DST="/etc/systemd/system/bluei-lrcn.service"
DEVICE="cuda"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cpu) DEVICE="cpu"; shift ;;
    -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

if [[ "$(id -u)" -ne 0 ]]; then
  echo "this script must be run as root (try: sudo $0)" >&2
  exit 1
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UNIT_SRC="$HERE/systemd/bluei-lrcn-prod.service"

if [[ ! -d "$INSTALL_DIR/services/lrcn" ]]; then
  echo "✗ $INSTALL_DIR/services/lrcn 없음 — install.sh 를 먼저 실행하세요 (구버전 tarball 이면 재패키징 필요)" >&2
  exit 1
fi
if [[ ! -f "$UNIT_SRC" ]]; then
  echo "✗ unit template 없음: $UNIT_SRC" >&2
  exit 1
fi

# ── venv + 의존성 ─────────────────────────────────────────────────────────────
if [[ ! -x "$VENV/bin/python" ]]; then
  echo "→ creating venv: $VENV"
  python3 -m venv "$VENV"
fi
echo "→ installing dependencies (torch 포함 — 수 GB, 수 분 소요)"
"$VENV/bin/pip" install --upgrade pip >/dev/null
"$VENV/bin/pip" install -r "$INSTALL_DIR/services/lrcn/requirements.txt"

# ── 씨앗 가중치 확인 (없어도 서버는 뜨나 씨앗 추론 불가 — 경고만) ──────────────
SEED="$INSTALL_DIR/local-ai/models/blueocean-baseline/bootstrap-2026-05-21-blueocean-final.pt"
if [[ ! -f "$SEED" ]]; then
  echo "⚠ 학습 씨앗 없음: $SEED — 현장 학습 promote 전까지 LRCN 추론 불가"
fi

# ── systemd 유닛 ─────────────────────────────────────────────────────────────
echo "→ installing systemd unit ($UNIT_DST, device=$DEVICE)"
sed "s/--device cuda/--device $DEVICE/" "$UNIT_SRC" > "$UNIT_DST"
systemctl daemon-reload
systemctl enable bluei-lrcn
systemctl restart bluei-lrcn

sleep 2
if systemctl is-active --quiet bluei-lrcn; then
  echo "✓ bluei-lrcn is running"
  echo "  health: curl -s http://127.0.0.1:8081/healthz"
  echo "  logs:   journalctl -u bluei-lrcn -f"
  echo "  다음: /etc/bluei-edge/edge.yaml 의 inference.lrcn.enabled: true 후 bluei-edge 재시작"
else
  echo "✗ bluei-lrcn failed to start" >&2
  journalctl -u bluei-lrcn -n 30 --no-pager
  exit 1
fi
