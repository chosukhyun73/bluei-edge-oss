#!/usr/bin/env bash
# setup-ollama.sh — 신규 현장 GX10 에 로컬 LLM(Ollama + Gemma) 설치 · 자동 기동.
#
# 강릉 GX10 에서 검증한 방식(2026-10-19): Docker 컨테이너 bluei-ollama + GPU(CDI) + systemd 관리.
#   - 컨테이너 포트 127.0.0.1:11435 (edge.yaml 의 inference.ollama.endpoint / llm.endpoint 기본값과 같음)
#   - 재부팅 때 Docker 가 GPU 장치 정의(CDI)보다 먼저 떠 시작이 실패하던 문제(2026-08-17) 대비:
#     /etc/cdi 고정 정의 + bluei-ollama.service (정의 대기 → 시작, 죽으면 재시작)
#
# 전제: DGX OS(Docker · NVIDIA Container Toolkit 기본 포함), 인터넷(모델 다운로드), install.sh 완료.
# 사용:
#   sudo /opt/bluei-edge/deploy/setup-ollama.sh                # 컨테이너 + 자동 기동 + 기본 모델
#   sudo /opt/bluei-edge/deploy/setup-ollama.sh --no-models    # 모델 다운로드 생략(나중에)
# 기본 모델: gemma4:26b(운영자 어시스턴트 바탕, ~18GB) + gemma4:e4b(경량, ~10GB)
#           + bluei-edge-assistant(deploy/ollama/bluei-edge-assistant.Modelfile 로 생성)
set -euo pipefail

IMAGE="${OLLAMA_IMAGE:-ollama/ollama:0.40.16}"  # GX10 재설치(2026-11-15) 검증. 0.22.1 은 모델 pull 이 412 로 거부됨
NAME="bluei-ollama"
PORT="${OLLAMA_PORT:-11435}"
MODELS=(gemma4:26b gemma4:e4b)
PULL=1
[[ "${1:-}" == "--no-models" ]] && PULL=0

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UNIT_SRC="$HERE/systemd/bluei-ollama.service"
MODELFILE="$HERE/ollama/bluei-edge-assistant.Modelfile"

[[ $EUID -eq 0 ]] || { echo "sudo 로 실행하세요" >&2; exit 1; }
command -v docker >/dev/null || { echo "✗ docker 없음 — DGX OS 기본 패키지를 확인하세요" >&2; exit 1; }
command -v nvidia-ctk >/dev/null || { echo "✗ nvidia-ctk 없음 — NVIDIA Container Toolkit 을 설치하세요" >&2; exit 1; }

echo "[1/5] GPU 장치 정의(CDI) 고정본 → /etc/cdi/nvidia.yaml"
mkdir -p /etc/cdi
nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml >/dev/null
nvidia-ctk cdi list | grep -q "nvidia.com/gpu=all" || { echo "✗ GPU 장치 정의 생성 실패" >&2; exit 1; }

echo "[2/5] 컨테이너 $NAME ($IMAGE, 127.0.0.1:$PORT)"
if ! docker inspect "$NAME" >/dev/null 2>&1; then
  docker pull "$IMAGE"
  docker create --name "$NAME" --restart=no \
    -p "127.0.0.1:${PORT}:11434" -v ollama-data:/root/.ollama \
    --device nvidia.com/gpu=all -e OLLAMA_HOST=0.0.0.0:11434 "$IMAGE" >/dev/null
else
  echo "  이미 있음 — 재사용 (재시작 정책만 systemd 로 이관)"
  docker update --restart=no "$NAME" >/dev/null
fi

echo "[3/5] systemd 서비스 bluei-ollama.service"
install -m 0644 "$UNIT_SRC" /etc/systemd/system/bluei-ollama.service
systemctl daemon-reload
systemctl enable bluei-ollama.service >/dev/null
docker stop -t 30 "$NAME" >/dev/null 2>&1 || true
systemctl restart bluei-ollama.service
for i in $(seq 1 30); do curl -sf "http://127.0.0.1:${PORT}/api/version" >/dev/null && break; sleep 2; done
curl -sf "http://127.0.0.1:${PORT}/api/version" >/dev/null || { echo "✗ Ollama 응답 없음 — journalctl -u bluei-ollama" >&2; exit 1; }
docker logs "$NAME" 2>&1 | grep -m1 "inference compute" || echo "⚠ GPU 인식 로그가 아직 없음 — docker logs $NAME 확인"

if [[ $PULL -eq 1 ]]; then
  echo "[4/5] 모델 다운로드 (수십 GB, 15~60분)"
  for m in "${MODELS[@]}"; do docker exec "$NAME" ollama pull "$m"; done
  echo "[5/5] 운영자 어시스턴트 모델 생성 (bluei-edge-assistant)"
  docker cp "$MODELFILE" "$NAME":/tmp/assistant.Modelfile
  docker exec "$NAME" ollama create bluei-edge-assistant -f /tmp/assistant.Modelfile
else
  echo "[4-5/5] 모델 다운로드 생략 (--no-models). 나중에: docker exec $NAME ollama pull gemma4:26b"
fi

docker exec "$NAME" ollama list || true
echo "✓ 완료. 다음: /etc/bluei-edge/edge.yaml 의 llm.enabled: true (endpoint http://localhost:${PORT}) 후 bluei-edge 재시작"
