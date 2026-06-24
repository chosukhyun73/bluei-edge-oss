#!/usr/bin/env bash
# 아이콘 클릭 진입점: 백엔드 ensure → 프론트(vite + Tauri) 기동.
#
# 동작:
#   0. 백엔드(:8080) healthz 확인
#        - 살아있으면 그대로 둔다 (절대 재시작 안 함 — 진행 중인 제어/상태 보존).
#        - 죽어있으면 기동 (CWD=프로젝트 루트, 상대 data_dir 해석).
#   1. 기존 vite/tauri/dashboard(프론트) process kill — 백엔드는 건드리지 않음.
#   2. vite dev server (5173) 시작
#   3. 5173 listen 확인 후 tauri binary 실행 (X11)
#   4. exit 후에도 전부 살아있음 (setsid + nohup + disown)
#
# 프론트만 죽었을 때: 이 스크립트를 다시 실행(아이콘 재클릭)하면 백엔드는 그대로 두고 프론트만 재시작된다.
# 로그: 백엔드 /tmp/bluei-edge.log, vite /tmp/vite.log, tauri /tmp/tauri.log
set -u

ROOT="${BLUEI_EDGE_ROOT:-/home/bluei/projects/bluei-edge}"
DASH="$ROOT/web/dashboard"
TAURI_BIN="$DASH/src-tauri/target/debug/bluei-edge-dashboard"
DISPLAY_VAL="${DISPLAY:-:1}"

BACKEND_PORT=8080
BACKEND_BIN="$ROOT/bin/bluei-edge"
BACKEND_CONFIG="${BLUEI_EDGE_CONFIG:-$ROOT/configs/edge.empty.yaml}"
BACKEND_LOG=/tmp/bluei-edge.log

# 백엔드와 vite 프록시가 같은 operator token 을 써야 /v1/* 인증이 통과한다.
# (vite.config.ts 가 이 토큰을 Bearer 로 자동 주입.)
: "${BLUEI_EDGE_OPERATOR_TOKEN:=qrbqWR8mEJgYTJmwwBFVa55HMImJG1TwAdJn1S1uFl4}"
export BLUEI_EDGE_OPERATOR_TOKEN

if [ ! -x "$TAURI_BIN" ]; then
  echo "[ERR] tauri binary not found at $TAURI_BIN" >&2
  echo "      먼저: cd $DASH && npm run tauri build (또는 tauri dev 한 번 실행)" >&2
  exit 1
fi

# ── [0/4] 백엔드 ensure (살아있으면 재시작 안 함) ─────────────────────────────
echo "[0/4] 백엔드 :$BACKEND_PORT 확인"
if curl -sf --max-time 3 "http://127.0.0.1:$BACKEND_PORT/healthz" >/dev/null 2>&1; then
  echo "      백엔드 이미 가동중 — 그대로 둠 (재시작 안 함)"
else
  if systemctl cat bluei-edge >/dev/null 2>&1; then
    # systemd 가 관리(Restart=always) → 수동 기동하면 포트 충돌. 곧 올라오니 대기만.
    echo "      백엔드 미가동 — systemd(bluei-edge)가 관리중 → 기동 대기 (수동 시작 안 함)"
  else
    echo "      백엔드 미가동 → 수동 기동 (systemd 미설치)"
    if [ ! -x "$BACKEND_BIN" ]; then
      echo "[ERR] 백엔드 바이너리 없음: $BACKEND_BIN" >&2
      echo "      먼저: cd $ROOT && PATH=\$HOME/.local/go/bin:\$PATH go build -o bin/bluei-edge ./cmd/bluei-edge" >&2
      exit 3
    fi
    # 상대 data_dir(./var/bluei-edge) 해석을 위해 CWD=ROOT 로 기동. setsid+nohup 으로 분리.
    ( cd "$ROOT" && setsid nohup "$BACKEND_BIN" run -config "$BACKEND_CONFIG" > "$BACKEND_LOG" 2>&1 < /dev/null & )
  fi
  for i in $(seq 1 20); do
    sleep 1
    if curl -sf --max-time 2 "http://127.0.0.1:$BACKEND_PORT/healthz" >/dev/null 2>&1; then
      echo "      백엔드 가동 확인 (t=${i}s)"
      break
    fi
    if [ "$i" = "20" ]; then
      if systemctl cat bluei-edge >/dev/null 2>&1; then
        echo "[ERR] 백엔드 미응답 — systemd 관리 서비스가 정지 상태일 수 있음(운영자 안전종료 후)." >&2
        echo "      기동:  sudo systemctl start bluei-edge   또는 재부팅. 로그: journalctl -u bluei-edge -e" >&2
      else
        echo "[ERR] 백엔드 healthz 타임아웃 — $BACKEND_LOG 확인" >&2
      fi
      exit 4
    fi
  done
fi

# ── [1/4] 기존 프론트 process 정리 (백엔드는 매칭 안 됨) ──────────────────────
echo "[1/4] 기존 프론트 process 정리"
pkill -f "vite.*--config" 2>/dev/null || true
pkill -f "node.*vite" 2>/dev/null || true
pkill -f "bluei-edge-dashboard" 2>/dev/null || true
sleep 1

echo "[2/4] vite dev server 시작 (5173)"
cd "$DASH"
setsid nohup npm run dev > /tmp/vite.log 2>&1 < /dev/null &
VITE_PID=$!
disown $VITE_PID 2>/dev/null || true

echo "[3/4] vite 5173 listen 대기"
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
  sleep 1
  if ss -tln 2>/dev/null | grep -q ":5173 "; then
    echo "      5173 listening (t=${i}s)"
    break
  fi
  if [ "$i" = "15" ]; then
    echo "[ERR] vite 5173 timeout — /tmp/vite.log 확인" >&2
    exit 2
  fi
done

echo "[4/4] tauri dashboard binary 실행 (DISPLAY=$DISPLAY_VAL)"
DISPLAY="$DISPLAY_VAL" \
  WEBKIT_DISABLE_DMABUF_RENDERER=1 \
  WEBKIT_DISABLE_COMPOSITING_MODE=1 \
  setsid nohup "$TAURI_BIN" > /tmp/tauri.log 2>&1 < /dev/null &
TAURI_PID=$!
disown $TAURI_PID 2>/dev/null || true

sleep 2
echo
echo "=== status ==="
curl -sf --max-time 2 "http://127.0.0.1:$BACKEND_PORT/healthz" >/dev/null 2>&1 && echo "  backend  :$BACKEND_PORT alive" || echo "  backend  DOWN — $BACKEND_LOG 확인"
ps -p $VITE_PID  > /dev/null 2>&1 && echo "  vite     PID=$VITE_PID  alive" || echo "  vite     DEAD"
ps -p $TAURI_PID > /dev/null 2>&1 && echo "  tauri    PID=$TAURI_PID  alive" || echo "  tauri    DEAD — /tmp/tauri.log 확인"
echo
echo "Dashboard 창이 떠야 합니다. 안 뜨면 /tmp/tauri.log 확인."
