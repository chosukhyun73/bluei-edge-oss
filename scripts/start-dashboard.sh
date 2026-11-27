#!/usr/bin/env bash
# 아이콘 클릭 진입점: 백엔드 ensure → 프론트(vite + Tauri) 기동.
#
# 동작:
#   0. 백엔드(:8080) healthz 확인
#        - 살아있으면 그대로 둔다 (절대 재시작 안 함 — 진행 중인 제어/상태 보존).
#        - 죽어있으면 기동 (CWD=프로젝트 루트, 상대 data_dir 해석).
#   1. 프로덕션 번들 빌드 → 성공 시에만 기존 vite/tauri/dashboard(프론트) process kill — 백엔드는 건드리지 않음.
#   2. vite preview (5173, 빌드된 번들 + /v1 프록시) 시작
#      ⚠ dev 서버(HMR)가 아니다 — 소스 수정 반영은 이 스크립트 재실행(재빌드)으로.
#      dev 모드 React 19 는 전체 재렌더마다 렌더러 메모리를 잔류시킨다(2026-09-15 격리 실측:
#      타이머 10배속 dev +269MB/h · prod +6MB/h, 주범 App.tsx 15초 목록 갱신). 워치독 일 1회 재기동의 원인.
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
BACKEND_CONFIG="${BLUEI_EDGE_CONFIG:-$ROOT/configs/edge.example.yaml}"
BACKEND_LOG=/tmp/bluei-edge.log

# 백엔드와 vite 프록시가 같은 operator token 을 써야 /v1/* 인증이 통과한다.
# (vite.config.ts 가 이 토큰을 Bearer 로 자동 주입.)
# 저장소에 토큰을 두지 않는다: 환경변수 > /etc/bluei-edge/env > 이 세션용 무작위 값.
if [ -z "${BLUEI_EDGE_OPERATOR_TOKEN:-}" ] && [ -r /etc/bluei-edge/env ]; then
  BLUEI_EDGE_OPERATOR_TOKEN="$(sed -n 's/^BLUEI_EDGE_OPERATOR_TOKEN=//p' /etc/bluei-edge/env | head -1)"
fi
: "${BLUEI_EDGE_OPERATOR_TOKEN:=$(openssl rand -hex 32)}"
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

# ── [1/4] 번들 빌드 후 기존 프론트 process 정리 (백엔드는 매칭 안 됨) ─────────
# 빌드를 먼저 한다 — 실패하면 기존 화면을 죽이지 않고 종료.
# tsc 는 생략(npm run dev 와 동일 수준). 로케일 생성은 dev/build 공통 선행 단계.
echo "[1/4] 대시보드 번들 빌드"
if ! (cd "$DASH" && node scripts/prepare-locales.mjs && npx vite build) > /tmp/vite-build.log 2>&1; then
  echo "[ERR] 빌드 실패 — 기존 프론트 유지. /tmp/vite-build.log 확인" >&2
  exit 5
fi
echo "      기존 프론트 process 정리"
# ⚠ 패턴을 좁게 (2026-09-17). 종전 `pkill -f "bluei-edge-dashboard"` 는 그 문자열이
# 명령줄에 들어간 무관한 프로세스까지 죽였다 (오늘 실제로 조회용 셸 2개가 같이 죽음).
# 앱이 아무 로그 없이 사라지는 사고(9-17 12:31)의 경로가 될 수 있어 정밀 매칭으로 바꾼다.
#   명령줄 전체가 바이너리 경로인 것만 (^…$) = 앱 본체. 그 문자열을 인자로 가진
#   셸·래퍼는 안 잡힌다. (-x 는 이름 15자 제한으로 매칭 자체가 안 되니 쓰지 말 것.)
pkill -f "^${TAURI_BIN}$" 2>/dev/null || true
pkill -f "$DASH/node_modules/.bin/vite" 2>/dev/null || true
pkill -f "vite preview --port 5173" 2>/dev/null || true
sleep 1

# 이전 실행 로그 보존 — truncate 로 사망 원인이 지워지던 것 방지 (2026-07-05).
# 직전 세션이 왜 죽었는지는 /tmp/tauri.log.1 · /tmp/vite.log.1 에서 확인.
# ⚠ 9-17 12:31 종료는 두 로그가 전부 비어 원인을 못 찾았다 — 종료 시각·종료코드와
#   그 순간의 스냅샷을 dashboard-exit-capture.sh 로 남긴다 (아래 [4/4] 래퍼).
#   ulimit -c unlimited 도 함께 건다 — 커널 core_pattern 이 파일 경로일 때만 실제로
#   덤프가 떨어진다(기본값은 apport 파이프라 안 떨어진다).
[ -s /tmp/tauri.log ] && mv -f /tmp/tauri.log /tmp/tauri.log.1
[ -s /tmp/vite.log ] && mv -f /tmp/vite.log /tmp/vite.log.1

echo "[2/4] vite preview 시작 (5173, 프로덕션 번들)"
cd "$DASH"
setsid nohup npx vite preview --port 5173 --strictPort > /tmp/vite.log 2>&1 < /dev/null &
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
# WEBKIT_DISABLE_MEMORY_PRESSURE_MONITOR — 렌더러가 RSS ~520MB 임계에서 +160MB/분
# runaway 후 16GB 자체 kill 하던 문제의 실험적 완화(2026-07-06). 압박 모니터의 병리적
# 폭주 가설 — 기저 누수(+1~3MB/분)만 남으면 확정. 6GB 워치독(cron)이 백스톱.
DISPLAY="$DISPLAY_VAL" \
  WEBKIT_DISABLE_DMABUF_RENDERER=1 \
  WEBKIT_DISABLE_COMPOSITING_MODE=1 \
  WEBKIT_DISABLE_MEMORY_PRESSURE_MONITOR=1 \
  setsid nohup bash -c 'ulimit -c unlimited 2>/dev/null; "$0"; rc=$?; "$1" "$rc"' \
    "$TAURI_BIN" "$ROOT/scripts/dashboard-exit-capture.sh" > /tmp/tauri.log 2>&1 < /dev/null &
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
