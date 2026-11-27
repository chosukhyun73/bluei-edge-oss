#!/usr/bin/env bash
#
# build-proce-linux-app.sh — processing-bluei 를 GX10(Ubuntu, arm64) 바탕화면 앱 묶음으로 만든다.
#
# 묶음을 GX10 사용자 홈에 풀고 install-desktop.sh 를 돌리면 바탕화면에 아이콘이
# 생긴다. sudo 없이 사용자 홈에만 설치하는 확인용 형태다(/opt + systemd 설치는 별도).
# 기록은 ~/.local/share/processing-bluei 에 보관된다(묶음을 지워도 남는다).
#
# 사용법:
#   bash scripts/build-proce-linux-app.sh          # dist/processing-bluei-linux-arm64.tar.gz
#   bash scripts/build-proce-linux-app.sh gx10     # 만든 뒤 ssh 호스트에 올리고 설치

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAME=processing-bluei-linux-arm64
OUT="$REPO/dist/$NAME"
PORT=8791
cd "$REPO"

echo "→ 서버 빌드 (linux/arm64)"
rm -rf "$OUT"
mkdir -p "$OUT/migrations" "$OUT/web/processing"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags='-s -w' -o "$OUT/processing-bluei" ./cmd/processing-bluei

echo "→ 화면 빌드"
(cd web/processing && npm install --no-audit --no-fund >/dev/null && npm run build >/dev/null)
cp -R web/processing/dist "$OUT/web/processing/dist"
cp migrations/*.sql "$OUT/migrations/"

echo "→ 아이콘"
python3 scripts/proce-icon.py "$OUT/processing-bluei.png"

# 실행기: 서버가 이미 떠 있으면 화면만 열고, 아니면 서버를 띄운 뒤 화면을 연다.
cat > "$OUT/processing-bluei-launcher.sh" <<LAUNCH
#!/bin/bash
RES="\$(cd "\$(dirname "\$0")" && pwd)"
DATA="\${XDG_DATA_HOME:-\$HOME/.local/share}/processing-bluei"
URL="http://127.0.0.1:$PORT/"
mkdir -p "\$DATA"
if [ ! -f "\$DATA/proce.yaml" ]; then
  cat > "\$DATA/proce.yaml" <<YAML
site:
  site_id: plant_local
  name: 가공공장
  site_type: processing
  timezone: Asia/Seoul
edge:
  edge_id: proce_local
  mode: development
  data_dir: "\$DATA"
runtime:
  startup_timeout_sec: 30
  shutdown_timeout_sec: 10
api:
  bind_host: 127.0.0.1
  port: $PORT
  request_timeout_sec: 30
  auth:
    enabled: false
    operator_token_env: PROCESSING_BLUEI_OPERATOR_TOKEN
storage:
  driver: sqlite
  sqlite_path: "\$DATA/proce.db"
  migration_mode: auto
YAML
fi
open_ui() {
  # GX10(Ubuntu 24.04, nvidia 커널)에서는 snap Chromium 의 네트워크가 AppArmor 에
  # 막혀 127.0.0.1 도 ERR_ACCESS_DENIED 가 된다. Firefox 가 있으면 그쪽으로 연다.
  # 사용자의 Firefox 프로필(프로필 선택 창 포함)을 건드리지 않도록 전용 프로필을 쓴다.
  if command -v firefox >/dev/null 2>&1; then
    PROF="\$HOME/snap/firefox/common/processing-bluei-profile"
    [ -d "\$HOME/snap/firefox" ] || PROF="\$DATA/firefox-profile"
    if [ ! -f "\$PROF/user.js" ]; then
      mkdir -p "\$PROF"
      cat > "\$PROF/user.js" <<JS
user_pref("browser.aboutwelcome.enabled", false);
user_pref("browser.startup.homepage_override.mstone", "ignore");
user_pref("startup.homepage_welcome_url", "");
user_pref("datareporting.policy.dataSubmissionPolicyBypassNotification", true);
user_pref("browser.shell.checkDefaultBrowser", false);
JS
    fi
    firefox --no-remote --profile "\$PROF" --new-window "\$URL" >/dev/null 2>&1 &
    return
  fi
  for b in /snap/bin/chromium chromium chromium-browser google-chrome; do
    if command -v "\$b" >/dev/null 2>&1; then
      "\$b" --app="\$URL" --class=processing-bluei --window-size=1500,1000 >/dev/null 2>&1 &
      return
    fi
  done
  xdg-open "\$URL"
}
if curl -fs "\${URL}healthz" >/dev/null 2>&1; then
  open_ui
  exit 0
fi
cd "\$RES"
export PROCESSING_BLUEI_APP_DIR="\$RES/web/processing/dist"
"\$RES/processing-bluei" run -config "\$DATA/proce.yaml" >> "\$DATA/proce.log" 2>&1 &
SRV=\$!
trap 'kill \$SRV 2>/dev/null' TERM INT
for i in \$(seq 1 50); do curl -fs "\${URL}healthz" >/dev/null 2>&1 && break; sleep 0.2; done
open_ui
wait \$SRV
LAUNCH

# 바탕화면 아이콘 등록 (사용자 홈에만 쓴다).
cat > "$OUT/install-desktop.sh" <<'INSTALL'
#!/bin/bash
set -euo pipefail
RES="$(cd "$(dirname "$0")" && pwd)"
DESKTOP="$(xdg-user-dir DESKTOP 2>/dev/null || echo "$HOME/Desktop")"
ICONS="$HOME/.local/share/icons/hicolor/512x512/apps"
mkdir -p "$ICONS" "$HOME/.local/share/applications" "$DESKTOP"
cp "$RES/processing-bluei.png" "$ICONS/processing-bluei.png"
cat > "$DESKTOP/processing-bluei.desktop" <<EOF
[Desktop Entry]
Type=Application
Version=1.0
Name=processing-bluei
Comment=가공공장 HACCP·추적 기록 (앱 창)
Exec=$RES/processing-bluei-launcher.sh
Icon=$ICONS/processing-bluei.png
Terminal=false
StartupWMClass=processing-bluei
Categories=Office;
EOF
chmod +x "$DESKTOP/processing-bluei.desktop"
cp "$DESKTOP/processing-bluei.desktop" "$HOME/.local/share/applications/"
gio set "$DESKTOP/processing-bluei.desktop" metadata::trusted true 2>/dev/null || true
echo "→ $DESKTOP/processing-bluei.desktop"
INSTALL
chmod +x "$OUT/processing-bluei-launcher.sh" "$OUT/install-desktop.sh"

tar -C "$REPO/dist" -czf "$REPO/dist/$NAME.tar.gz" "$NAME"
echo "→ dist/$NAME.tar.gz"

if [ "${1:-}" != "" ]; then
  scp -q "$REPO/dist/$NAME.tar.gz" "$1:"
  ssh "$1" "rm -rf ~/processing-bluei && mkdir ~/processing-bluei && tar -xzf ~/$NAME.tar.gz -C ~/processing-bluei --strip-components=1 && rm ~/$NAME.tar.gz && DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/\$(id -u)/bus ~/processing-bluei/install-desktop.sh"
fi
