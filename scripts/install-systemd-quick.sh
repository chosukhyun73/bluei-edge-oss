#!/usr/bin/env bash
# bluei-edge systemd 빠른 등록 (현 위치 in-place). ★ 루트로 실행:
#     sudo bash scripts/install-systemd-quick.sh
#
# 현재 프로젝트 위치/바이너리/config(edge.empty.yaml) 를 그대로 구동하는 system 서비스를 만든다.
#   - 부팅 자동기동 (WantedBy=multi-user.target)
#   - 크래시/비정상 종료 시 자동 재시작 (Restart=on-failure, 5s)
#   - 종료 시 SIGTERM + 20s 유예 → 기존 graceful 종료 경로
# ★ Restart=on-failure 인 이유: 대시보드 "안전 종료" 버튼은 graceful 종료(exit 0)다.
#   always 면 systemd 가 즉시 다시 띄워 버튼이 무력화된다. on-failure 면
#   운영자 의도 종료(exit 0)는 존중하고, 크래시(비정상 exit/SIGKILL)만 자동 복구한다.
# 데이터(/home/bluei/.../var)가 홈에 있으므로 ProtectHome 류 하드닝은 쓰지 않는다.
#
# 제거:  sudo systemctl disable --now bluei-edge && sudo rm /etc/systemd/system/bluei-edge.service && sudo systemctl daemon-reload
set -euo pipefail

ROOT=/home/bluei/projects/bluei-edge
RUN_USER=bluei
TOKEN="${BLUEI_EDGE_OPERATOR_TOKEN:-qrbqWR8mEJgYTJmwwBFVa55HMImJG1TwAdJn1S1uFl4}"
UNIT=/etc/systemd/system/bluei-edge.service
ENV_DIR=/etc/bluei-edge
ENV_FILE="$ENV_DIR/env"

if [ "$(id -u)" -ne 0 ]; then
  echo "루트 권한 필요 — 이렇게 실행하세요:  sudo bash $0" >&2
  exit 1
fi
if [ ! -x "$ROOT/bin/bluei-edge" ]; then
  echo "바이너리 없음: $ROOT/bin/bluei-edge — 먼저 빌드(go build -o bin/bluei-edge ./cmd/bluei-edge)" >&2
  exit 2
fi

echo "[1/6] 운영자 토큰 env 파일 작성: $ENV_FILE (600)"
install -d -m 755 "$ENV_DIR"
cat > "$ENV_FILE" <<EOF
BLUEI_EDGE_OPERATOR_TOKEN=$TOKEN
EOF
chmod 600 "$ENV_FILE"

echo "[2/6] systemd unit 작성: $UNIT"
cat > "$UNIT" <<EOF
[Unit]
Description=bluei-edge — RAS aquaculture edge runtime (in-place quick install)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$RUN_USER
WorkingDirectory=$ROOT
EnvironmentFile=$ENV_FILE
ExecStart=$ROOT/bin/bluei-edge run -config configs/edge.empty.yaml
Restart=on-failure
RestartSec=5s
TimeoutStopSec=20s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

echo "[3/6] 수동 백엔드(:8080) 정지 — 포트 충돌 방지 (graceful)"
PID=$(ss -tlnpH 'sport = :8080' 2>/dev/null | grep -oE 'pid=[0-9]+' | head -1 | cut -d= -f2 || true)
if [ -n "${PID:-}" ]; then
  echo "      manual pid=$PID 종료"
  kill -TERM "$PID" 2>/dev/null || true
  sleep 3
fi

echo "[4/6] daemon-reload + enable --now"
systemctl daemon-reload
systemctl enable --now bluei-edge

echo "[5/6] 가동 대기 (healthz)"
ok=0
for i in $(seq 1 20); do
  sleep 1
  if curl -sf --max-time 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then ok=1; break; fi
done

echo "[6/6] 상태"
echo "  enabled: $(systemctl is-enabled bluei-edge 2>&1)"
echo "  active : $(systemctl is-active bluei-edge 2>&1)"
echo "  healthz: $(curl -s --max-time 3 http://127.0.0.1:8080/healthz 2>&1)"
if [ "$ok" = "1" ]; then
  echo
  echo "✅ 완료. 이제 GX10 재부팅/전원 ON 시 백엔드가 자동 기동되고, 크래시해도 5초 후 자동 재시작됩니다."
  echo "   확인:  systemctl status bluei-edge   |   로그:  journalctl -u bluei-edge -f"
else
  echo
  echo "⚠ healthz 미응답 — journalctl -u bluei-edge -e 로 원인 확인" >&2
  exit 3
fi
