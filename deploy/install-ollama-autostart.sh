#!/usr/bin/env bash
# Ollama(로컬 LLM) 자동 기동 설정 — GX10.
#
# 문제(2026-08-17): 재부팅 때 Docker 가 NVIDIA GPU 장치 정의(CDI, /var/run/cdi/nvidia.yaml)보다
# 먼저 떠서 bluei-ollama 컨테이너 시작이 "unresolvable CDI devices" 로 실패했고, Docker 는 다시
# 시도하지 않아 두 달간 멈춰 있었다.
#
# 조치:
#   1) 재부팅해도 남는 고정 GPU 장치 정의를 /etc/cdi 에 만든다 (Docker 가 부팅 직후부터 찾을 수 있게).
#      드라이버 업데이트 뒤에는 nvidia-cdi-refresh 가 만드는 /var/run/cdi 본이 우선한다.
#   2) Docker 자체 재시작 정책을 끄고, systemd 서비스(bluei-ollama.service)가 관리한다
#      — GPU 정의가 생길 때까지 기다렸다가 시작, 죽으면 15초 뒤 재시작.
#
# 사용: sudo bash deploy/install-ollama-autostart.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
UNIT_SRC="$ROOT/deploy/systemd/bluei-ollama.service"
UNIT_DST="/etc/systemd/system/bluei-ollama.service"

[[ $EUID -eq 0 ]] || { echo "sudo 로 실행하세요"; exit 1; }
docker inspect bluei-ollama >/dev/null 2>&1 || { echo "bluei-ollama 컨테이너가 없습니다 (local-ai/compose.yaml 로 먼저 생성)"; exit 1; }

echo "[1/4] 고정 GPU 장치 정의 생성 → /etc/cdi/nvidia.yaml"
mkdir -p /etc/cdi
nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
nvidia-ctk cdi list

echo "[2/4] Docker 자체 재시작 정책 끄기 (systemd 가 관리)"
docker update --restart=no bluei-ollama

echo "[3/4] systemd 서비스 설치"
install -m 0644 "$UNIT_SRC" "$UNIT_DST"
systemctl daemon-reload
systemctl enable bluei-ollama.service

echo "[4/4] 서비스로 다시 기동"
docker stop -t 30 bluei-ollama >/dev/null 2>&1 || true
systemctl restart bluei-ollama.service
sleep 10
systemctl --no-pager --lines=5 status bluei-ollama.service || true
docker ps --filter name=bluei-ollama --format '{{.Names}} {{.Status}}'
echo "완료. 확인: curl -s http://127.0.0.1:11435/api/tags"
