#!/usr/bin/env bash
# Check a Hikvision RTSP camera connection without storing credentials in files.
# Required env:
#   HIKVISION_HOST=192.168.0.94
#   HIKVISION_USER=admin
#   HIKVISION_PASSWORD='...'
# Optional env:
#   HIKVISION_CHANNEL=101
#   HIKVISION_RTSP_PORT=554
#   HIKVISION_TIMEOUT_SEC=8

set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }

host="${HIKVISION_HOST:-}"
user="${HIKVISION_USER:-admin}"
pass="${HIKVISION_PASSWORD:-}"
channel="${HIKVISION_CHANNEL:-101}"
port="${HIKVISION_RTSP_PORT:-554}"
timeout_sec="${HIKVISION_TIMEOUT_SEC:-8}"

[ -n "$host" ] || fail "HIKVISION_HOST is required"
[ -n "$pass" ] || fail "HIKVISION_PASSWORD is required"
command -v gst-launch-1.0 >/dev/null || fail "gst-launch-1.0 not found; install GStreamer tools"

redact() {
  sed -E 's#rtsp://[^ @]*@#rtsp://[redacted]@#g; s#user-pw=[^ ]+#user-pw=[redacted]#g; s#password=[^ ]+#password=[redacted]#g'
}

echo "== Hikvision RTSP check =="
echo "host=$host port=$port channel=$channel user=$user"

if timeout 2 bash -c "</dev/tcp/$host/$port" 2>/dev/null; then
  echo "PASS: TCP $port open"
else
  fail "TCP $port closed or filtered"
fi

uri="rtsp://$host:$port/Streaming/Channels/$channel"
out="${TMPDIR:-/tmp}/bluei-hikvision-rtsp-check.out"
set +e
timeout "$timeout_sec" gst-launch-1.0 -q \
  rtspsrc location="$uri" user-id="$user" user-pw="$pass" protocols=tcp latency=200 \
  ! fakesink sync=false >"$out" 2>&1
rc=$?
set -e

case "$rc" in
  124)
    echo "PASS: RTSP stream opened and stayed alive for ${timeout_sec}s"
    ;;
  0)
    echo "PASS: RTSP pipeline exited cleanly"
    ;;
  *)
    echo "FAIL: RTSP pipeline failed rc=$rc" >&2
    redact <"$out" | head -80 >&2 || true
    exit "$rc"
    ;;
esac
