#!/usr/bin/env bash
# 대시보드를 "앱 창"으로 여는 바로가기 (운영자 2026-11-18: 브라우저 말고 앱 형태로).
#
# 주소창·탭 없는 WebKitGTK 창(deploy/dashboard-window.py)으로 연다. 작업 표시줄에 따로 뜬다.
# snap Chromium --app 은 쓰지 않는다: GX10 에서 AppArmor 가 127.0.0.1 접속까지 막아
# ERR_ACCESS_DENIED (2026-11-18 실측, processing-bluei 런처와 같은 문제).
# WebKitGTK(python3-gi, gir1.2-webkit2-4.1)가 없으면 기본 브라우저로 연다.
#
# 사용:  sudo /opt/bluei-edge/deploy/setup-dashboard-app.sh
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "sudo 로 실행하세요" >&2; exit 1; }

LIB=/usr/local/lib/bluei-edge
install -d -m 755 "$LIB"
install -m 755 "$(dirname "$0")/dashboard-window.py" "$LIB/dashboard-window.py"
cat > "$LIB/open-dashboard.sh" <<'LAUNCH'
#!/bin/sh
# bluei-edge 대시보드 앱 창 (deploy/setup-dashboard-app.sh 가 생성)
LIB=/usr/local/lib/bluei-edge
if python3 -c 'import gi; gi.require_version("WebKit2", "4.1")' 2>/dev/null; then
  exec python3 "$LIB/dashboard-window.py" "$@"
fi
exec xdg-open http://127.0.0.1:8080/
LAUNCH
chmod 755 "$LIB/open-dashboard.sh"

DESK=/usr/share/applications/bluei-edge-dashboard.desktop
cat > "$DESK" <<DESKTOP
[Desktop Entry]
Type=Application
Version=1.0
Name=Bluei Edge 대시보드
Comment=RAS·해상 양식장 통합 관제 (앱 창)
Exec=$LIB/open-dashboard.sh
Icon=bluei-edge
Terminal=false
StartupWMClass=bluei-edge
Categories=Network;Monitor;
Keywords=bluei;edge;ras;aquaculture;feeder;
DESKTOP
chmod 644 "$DESK"

U="${SUDO_USER:-}"
if [[ -n "$U" && "$U" != root ]]; then
  UD="$(sudo -u "$U" xdg-user-dir DESKTOP 2>/dev/null || echo "$(getent passwd "$U" | cut -d: -f6)/Desktop")"
  if [[ -d "$UD" ]]; then
    install -m 755 -o "$U" -g "$(id -gn "$U")" "$DESK" "$UD/"
    sudo -u "$U" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$(id -u "$U")/bus" \
      gio set "$UD/bluei-edge-dashboard.desktop" metadata::trusted true 2>/dev/null || true
    echo "  바탕화면: $UD/bluei-edge-dashboard.desktop"
  fi
fi
if python3 -c 'import gi; gi.require_version("WebKit2", "4.1")' 2>/dev/null; then
  echo "✓ 대시보드 바로가기 = 앱 창 (WebKitGTK)"
else
  echo "⚠ WebKitGTK 가 없어 기본 브라우저로 열립니다 — sudo apt install python3-gi gir1.2-webkit2-4.1 후 다시 실행하세요"
fi
