#!/usr/bin/env bash
#
# build-proce-macos-app.sh — processing-bluei 를 macOS 앱 묶음(.app)으로 만든다.
#
# 앱을 열면 이 Mac 에서 processing-bluei 서버가 뜨고 화면이 열린다. 기록은
# ~/Library/Application Support/processing-bluei 에 보관된다(앱을 지워도 남는다).
# 서명·공증을 하지 않는 로컬용 묶음이다 — 만든 Mac 에서 쓰는 용도.
#
# 사용법:
#   bash scripts/build-proce-macos-app.sh            # dist/processing-bluei.app
#   bash scripts/build-proce-macos-app.sh ~/Desktop  # 만든 뒤 그 폴더에 복사

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="$REPO/dist/processing-bluei.app"
RES="$APP/Contents/Resources"
PORT=8791
cd "$REPO"

echo "→ 서버 빌드"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$RES/migrations" "$RES/web/processing"
go build -ldflags='-s -w' -o "$RES/processing-bluei" ./cmd/processing-bluei

echo "→ 화면 빌드"
(cd web/processing && npm install --no-audit --no-fund >/dev/null && npm run build >/dev/null)
cp -R web/processing/dist "$RES/web/processing/dist"
cp migrations/*.sql "$RES/migrations/"

echo "→ 아이콘"
ICONSET="$(mktemp -d)/processing-bluei.iconset"
mkdir -p "$ICONSET"
python3 scripts/proce-icon.py "$ICONSET/icon_512x512@2x.png"
for size in 16 32 128 256 512; do
  sips -z $size $size "$ICONSET/icon_512x512@2x.png" --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  sips -z $((size*2)) $((size*2)) "$ICONSET/icon_512x512@2x.png" --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$RES/processing-bluei.icns"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>processing-bluei</string>
  <key>CFBundleDisplayName</key><string>processing-bluei</string>
  <key>CFBundleIdentifier</key><string>kr.bluei.proce</string>
  <key>CFBundleExecutable</key><string>processing-bluei-launcher</string>
  <key>CFBundleIconFile</key><string>processing-bluei</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>LSUIElement</key><true/>
</dict>
</plist>
PLIST

# 실행기: 서버가 이미 떠 있으면 화면만 열고, 아니면 서버를 띄운 뒤 화면을 연다.
cat > "$APP/Contents/MacOS/processing-bluei-launcher" <<LAUNCH
#!/bin/bash
RES="\$(cd "\$(dirname "\$0")/../Resources" && pwd)"
DATA="\$HOME/Library/Application Support/processing-bluei"
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
  if [ -d "/Applications/Google Chrome.app" ]; then
    open -na "Google Chrome" --args --app="\$URL"
  else
    open "\$URL"
  fi
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
chmod +x "$APP/Contents/MacOS/processing-bluei-launcher"

echo "→ $APP"
if [ "${1:-}" != "" ]; then
  rm -rf "$1/processing-bluei.app"
  cp -R "$APP" "$1/"
  echo "→ $1/processing-bluei.app"
fi
