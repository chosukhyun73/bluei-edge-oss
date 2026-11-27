#!/usr/bin/env bash
# 운영 예약 작업 + 대시보드 바로가기 — 새 GX10 에 1회 (재실행 = 갱신).
#
#   1) Ollama GPU 감시 (2분마다): 컨테이너가 GPU(NVML) 접근을 잃으면 재시작
#   2) 영상 NAS 이동 (매일 02:15): 7일 지난 클립을 CAPTURES_ARCHIVE_DIR 로 옮김
#      — /etc/bluei-edge/env 에 CAPTURES_ARCHIVE_DIR 가 없으면 아무것도 안 함
#   3) 대시보드 바로가기: 앱 메뉴 + 설치한 사용자의 바탕화면 (Chromium 앱 창 — setup-dashboard-app.sh)
#   4) GPU 클럭 상한 (부팅마다, systemd bluei-gpu-clock-cap): 과열 전원 차단·클럭 잠김 예방
#   5) GPU 과열·클럭 잠김 감시 (2분마다): gpu-health-watch.sh — 경고는 바탕화면 위젯에 표시
#   6) 바탕화면 우상단 "GX10 상태" 위젯 (conky, 설치한 사용자 로그인 시 자동 실행)
#
# 강릉 시절엔 레포 폴더 스크립트를 사용자 crontab 으로 돌렸다 — 레포를 지우면 함께
# 사라졌다(2026-11-15 재설치에서 확인). 그래서 /usr/local/lib/bluei-edge 에 복사해
# root cron 으로 돈다: 클립은 bluei-edge 소유, NAS cifs 는 uid=1000 소유라 둘 다
# 다룰 수 있는 계정이 root.
#
# 사용:  sudo /opt/bluei-edge/deploy/setup-ops.sh
set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "sudo 로 실행하세요" >&2; exit 1; }
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# 패키지: deploy/ops/ · 레포에서 직접: ../scripts/
if [[ -d "$HERE/ops" ]]; then OPS="$HERE/ops"; ICON_SRC="$OPS/bluei-edge.png"
else OPS="$HERE/../scripts"; ICON_SRC="$HERE/../web/dashboard/src-tauri/icons/128x128.png"; fi
LIB=/usr/local/lib/bluei-edge

echo "→ 스크립트 설치: $LIB"
install -d -m 755 "$LIB"
install -m 755 "$OPS/ollama-gpu-watch.sh" "$OPS/archive-captures-nas.sh" \
  "$OPS/gpu-health-watch.sh" "$OPS/gx10-temp-summary.sh" "$LIB/"

echo "→ GPU 클럭 상한: bluei-gpu-clock-cap.service"
install -m 644 "$HERE/systemd/bluei-gpu-clock-cap.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable bluei-gpu-clock-cap.service
systemctl restart bluei-gpu-clock-cap.service

echo "→ 예약 작업: /etc/cron.d/bluei-edge-ops"
cat > /etc/cron.d/bluei-edge-ops <<CRON
# bluei-edge 운영 예약 작업 (deploy/setup-ops.sh 가 생성)
SHELL=/bin/bash
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
*/2 * * * * root $LIB/ollama-gpu-watch.sh >> /var/log/bluei-ollama-gpu-watch.log 2>&1
*/2 * * * * root set -a; . /etc/bluei-edge/env 2>/dev/null; set +a; $LIB/gpu-health-watch.sh >> /var/log/bluei-gpu-health.log 2>&1
15 2 * * * root set -a; . /etc/bluei-edge/env; set +a; CAPTURES_SRC=/var/lib/bluei-edge/captures $LIB/archive-captures-nas.sh >> /var/log/bluei-captures-archive.log 2>&1
CRON
chmod 644 /etc/cron.d/bluei-edge-ops

echo "→ 대시보드 바로가기 (앱 창)"
[[ -f "$ICON_SRC" ]] && install -m 644 "$ICON_SRC" /usr/share/pixmaps/bluei-edge.png
"$HERE/setup-dashboard-app.sh"

echo "→ GX10 상태 위젯 (conky)"
# 예전엔 ~/.local/bin 스크립트를 불러, 2026-11-15 초기화 때 홈과 함께 지워져 빈 위젯만 남았다.
U="${SUDO_USER:-}"
if ! command -v conky >/dev/null; then
  echo "  ⚠ conky 없음 — sudo apt install conky 후 다시 실행하세요"
elif [[ -n "$U" && "$U" != root ]]; then
  UH="$(getent passwd "$U" | cut -d: -f6)"
  sudo -u "$U" mkdir -p "$UH/.config/conky" "$UH/.config/autostart"
  sudo -u "$U" tee "$UH/.config/conky/gx10-temp.conf" >/dev/null <<CONKY
conky.config = {
    alignment = 'top_right',
    gap_x = 40,
    gap_y = 90,
    minimum_width = 480,
    maximum_width = 480,
    update_interval = 2.0,
    double_buffer = true,
    own_window = true,
    own_window_type = 'desktop',
    own_window_transparent = false,
    own_window_argb_visual = true,
    own_window_argb_value = 150,
    own_window_colour = '111111',
    own_window_hints = 'undecorated,below,sticky,skip_taskbar,skip_pager',
    draw_shades = false,
    draw_outline = false,
    draw_borders = false,
    use_xft = true,
    font = 'Noto Sans CJK KR:size=20',
    default_color = 'EAEAEA',
    color1 = '7AD7FF',
};

conky.text = [[
\${color1}GX10 상태\${color}
\${execpi 2 $LIB/gx10-temp-summary.sh}
]];
CONKY
  sudo -u "$U" tee "$UH/.config/autostart/gx10-temp-widget.desktop" >/dev/null <<DESKTOP
[Desktop Entry]
Type=Application
Name=GX10 상태 위젯
Comment=GX10 GPU/기판/NVMe 온도·클럭·경고 (deploy/setup-ops.sh 가 생성)
Exec=env LANG=C.UTF-8 LC_ALL=C.UTF-8 conky -c $UH/.config/conky/gx10-temp.conf
Hidden=false
NoDisplay=false
X-GNOME-Autostart-enabled=true
DESKTOP
  echo "  위젯 설정: $UH/.config/conky/gx10-temp.conf (실행 중인 conky 는 설정 변경을 스스로 다시 읽음)"
fi

echo "→ 확인"
cat /etc/cron.d/bluei-edge-ops | grep -v '^#'
"$LIB/ollama-gpu-watch.sh" && echo "  GPU 감시 1회 실행 OK (출력 없음 = GPU 정상)"
systemctl is-active --quiet bluei-gpu-clock-cap.service && echo "  GPU 클럭 상한 적용 OK" \
  || echo "  ⚠ GPU 클럭 상한 실패 — journalctl -u bluei-gpu-clock-cap 확인"
(set -a; . /etc/bluei-edge/env 2>/dev/null; set +a; "$LIB/gpu-health-watch.sh") >> /var/log/bluei-gpu-health.log 2>&1
echo "  과열·클럭 감시 1회 실행 — 현재 경고: $(cat /run/bluei-edge-gpu-health 2>/dev/null | tr '\n' ' ')(비어 있으면 정상)"
grep -q '^CAPTURES_ARCHIVE_DIR=' /etc/bluei-edge/env 2>/dev/null \
  || echo "  ⚠ 영상 NAS 이동: /etc/bluei-edge/env 에 CAPTURES_ARCHIVE_DIR=/mnt/<nas>/<현장>/captures 를 넣기 전까지는 대기"
