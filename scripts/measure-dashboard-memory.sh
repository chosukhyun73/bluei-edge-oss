#!/usr/bin/env bash
# Dashboard 메모리 측정 — vite + Tauri RSS 1분 주기 기록.
# 365일 무중단 정책 진단용. 24시간+ 데이터 누적 후 grow 패턴 분석.
#
# 출력: /tmp/dashboard-memory.log
# 형식: ISO8601-ts process pid=N rss_kb=N vsz_kb=N etime=hh:mm:ss
#
# 시작: setsid nohup bash scripts/measure-dashboard-memory.sh > /tmp/measure.log 2>&1 < /dev/null & disown
# 중지: pkill -f measure-dashboard-memory.sh

set -u

LOG="${MEASURE_LOG:-/tmp/dashboard-memory.log}"
INTERVAL="${MEASURE_INTERVAL:-60}"

echo "# measure start $(date -Iseconds) interval=${INTERVAL}s log=${LOG}" >> "$LOG"

while true; do
  ts="$(date -Iseconds)"

  # vite — node 프로세스 (sh -c vite 가 아니라 실제 node 가 메모리 사용)
  vite_pid=$(pgrep -f 'node.*\.bin/vite' | head -1)
  if [ -n "$vite_pid" ]; then
    line=$(ps -p "$vite_pid" -o rss=,vsz=,etime= 2>/dev/null)
    if [ -n "$line" ]; then
      rss=$(echo "$line" | awk '{print $1}')
      vsz=$(echo "$line" | awk '{print $2}')
      etime=$(echo "$line" | awk '{print $3}')
      echo "$ts vite pid=$vite_pid rss_kb=$rss vsz_kb=$vsz etime=$etime" >> "$LOG"
    fi
  fi

  # Tauri dashboard binary (dev = debug, prod = release). 둘 다 별도 라인으로 기록.
  for variant in debug release; do
    pid=$(pgrep -f "src-tauri/target/${variant}/bluei-edge-dashboard" | head -1)
    if [ -n "$pid" ]; then
      line=$(ps -p "$pid" -o rss=,vsz=,etime= 2>/dev/null)
      if [ -n "$line" ]; then
        rss=$(echo "$line" | awk '{print $1}')
        vsz=$(echo "$line" | awk '{print $2}')
        etime=$(echo "$line" | awk '{print $3}')
        echo "$ts tauri-${variant} pid=$pid rss_kb=$rss vsz_kb=$vsz etime=$etime" >> "$LOG"
      fi
    fi
  done

  # bluei-edge (백엔드) — leak 의심 시 비교용
  be_pid=$(pgrep -f 'bin/bluei-edge run' | head -1)
  if [ -n "$be_pid" ]; then
    line=$(ps -p "$be_pid" -o rss=,vsz=,etime= 2>/dev/null)
    if [ -n "$line" ]; then
      rss=$(echo "$line" | awk '{print $1}')
      vsz=$(echo "$line" | awk '{print $2}')
      etime=$(echo "$line" | awk '{print $3}')
      echo "$ts backend pid=$be_pid rss_kb=$rss vsz_kb=$vsz etime=$etime" >> "$LOG"
    fi
  fi

  # 프로세스 비어 있으면 (모두 죽음) 로그 한 줄 남김
  if [ -z "$vite_pid" ] && [ -z "$tauri_pid" ] && [ -z "$be_pid" ]; then
    echo "$ts NONE all_target_processes_missing" >> "$LOG"
  fi

  sleep "$INTERVAL"
done
