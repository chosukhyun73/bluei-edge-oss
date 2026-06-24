# bluei-edge — 배포 가이드 (Deployment Guide)

bluei-edge 를 양식장 현장 edge 디바이스 (GX10 / DGX Spark / amd64 서버) 에
프로덕션 솔루션으로 배포하는 절차.

> dev 환경 (`vite dev :5173 + backend :8080`) 실행 방법은 `README.md` 의 Quick Start 를 참조.

---

## 1. 솔루션 개요

bluei-edge 는 단일 디바이스 안에 모든 게 동작하는 edge appliance 형태로 배포된다.

- **외부 노출 port 1개** (기본 `:8080`) — operator 브라우저, 외부 통합 모두 이 port 하나만.
- **하나의 디렉토리** (`/opt/bluei-edge`) 에 binary + dashboard + migrations.
- **systemd unit** 으로 자동 시작 + 장애 시 자동 재시작.
- **로컬 LLM** (Ollama + gemma4:26b) 은 별도 컴포넌트 — bluei-edge 가 있는 같은 디바이스에 설치.

## 2. 사전 요구사항 (디바이스 측)

- Linux + systemd (Ubuntu 22.04+, Debian 12+, RHEL 9+ 등)
- 디스크: 32GB 이상 (LLM 모델 별도 50GB+)
- (옵션) Ollama 가 설치되어 있고 `gemma4:26b` (또는 `gemma4:e4b` 호환 모델) 다운로드 완료
- root 권한 (sudo)

## 3. 빌드 (개발자 환경에서 1회)

```bash
# 호스트 아키텍처
make package

# GX10 / DGX Spark (ARM64) 크로스 컴파일
make build-arm64
make dashboard
make package     # arm64 binary 가 bin/ 에 들어 있어야 함 — 필요 시 수동 교체
```

결과물:
```
dist/bluei-edge-<version>.tar.gz   ~5MB
```

## 4. 디바이스 측 설치 (1회)

```bash
# 1) tarball 을 디바이스에 복사
scp dist/bluei-edge-<version>.tar.gz user@<edge-ip>:/tmp/

# 2) install.sh 추출 (또는 디바이스에서 tarball 전체 풀고 deploy/install.sh 실행)
ssh user@<edge-ip>
cd /tmp
tar -xzf bluei-edge-<version>.tar.gz
sudo bluei-edge-<version>/deploy/install.sh -t /tmp/bluei-edge-<version>.tar.gz
```

설치되는 위치:
```
/opt/bluei-edge/                — binary + dashboard + migrations (불변)
/etc/bluei-edge/edge.yaml       — operator 가 편집할 config
/etc/bluei-edge/env             — operator API token (자동 생성)
/etc/bluei-edge/*.example.yaml  — farms/tanks/devices/... 시드
/var/lib/bluei-edge/edge.db     — SQLite 데이터
/etc/systemd/system/bluei-edge.service
```

## 5. 운영자 접속

```
http://<edge-ip>:8080/
```

같은 LAN 의 브라우저로 위 URL 접속 → dashboard 자동 로드.

## 6. 운영 명령

```bash
# 상태
sudo systemctl status bluei-edge

# 시작/중지/재시작
sudo systemctl start bluei-edge
sudo systemctl stop bluei-edge
sudo systemctl restart bluei-edge

# 로그
journalctl -u bluei-edge -f

# config 수정 후 적용
sudo vi /etc/bluei-edge/edge.yaml
sudo systemctl restart bluei-edge

# health check
curl -sf http://<edge-ip>:8080/healthz

# 업그레이드 (새 tarball 으로 재설치)
sudo /opt/bluei-edge/deploy/install.sh -t /path/to/bluei-edge-<new-version>.tar.gz
# → /etc/bluei-edge/edge.yaml 과 /var/lib/bluei-edge/ 데이터는 보존됨

# 제거 (데이터는 보존, 코드+systemd unit 만 삭제)
sudo /opt/bluei-edge/deploy/install.sh --uninstall
```

## 7. dev 환경과의 차이

| 항목 | Production | Dev |
|---|---|---|
| Dashboard 접속 | `http://<edge>:8080/` (backend 가 정적 서빙) | `http://localhost:5173/` (vite dev server, HMR) |
| API 호출 | 같은 origin `/v1/*` | vite proxy 가 `localhost:5173` → `localhost:8080` |
| Binary | `/opt/bluei-edge/bin/bluei-edge` | `./bin/bluei-edge` |
| Config | `/etc/bluei-edge/edge.yaml` (operator 편집) | `./configs/edge.example.yaml` |
| 데이터 | `/var/lib/bluei-edge/edge.db` | `./var/bluei-edge/edge.db` |
| 자동 시작 | systemd | nohup |
| 로그 | journald | `/tmp/bluei-edge.log` |

## 8. 로컬 LLM (Ollama) 연동

bluei-edge 는 `gemma4:26b` (primary) + `gemma4:e4b` (fallback) 모델을
Ollama 의 OpenAI-호환 API 로 호출한다.

```bash
# Ollama 설치 (디바이스)
curl -fsSL https://ollama.com/install.sh | sh

# 모델 다운로드
ollama pull gemma4:26b
ollama pull gemma4:e4b

# Ollama 가 11435 port 로 listen (또는 11434 default — bluei-edge config 의 base_url 일치 확인)
```

`/etc/bluei-edge/edge.yaml` 에서 LLM 설정:
```yaml
llm:
  enabled: true
  base_url: http://127.0.0.1:11435
  bearer_token: devtoken
  primary_model: gemma4:26b
  fallback_model: gemma4:e4b
```

## 9. 트러블슈팅

| 증상 | 점검 |
|---|---|
| `systemctl status bluei-edge` 가 failed | `journalctl -u bluei-edge -n 50` 으로 stderr 확인. 흔한 원인: `/etc/bluei-edge/edge.yaml` 잘못된 yaml, 다른 프로세스가 8080 점유, sqlite_path 디렉토리 없음 |
| `:8080` 접속 안 됨 | `sudo ss -tlnp \| grep :8080` 로 listen 확인. firewall (`ufw status`) 도 점검 |
| Dashboard 빈 화면 | `/opt/bluei-edge/web/dashboard/dist/index.html` 존재 여부 확인. 환경변수 `BLUEI_EDGE_DASHBOARD_DIR` 가 다른 경로 가리키는지 |
| API 401 unauthorized | `/etc/bluei-edge/env` 의 `BLUEI_EDGE_OPERATOR_TOKEN` 과 클라이언트 Bearer 일치 확인 |
| LLM 응답 없음 | `curl http://127.0.0.1:11435/api/tags` 로 Ollama 가동 확인. 모델 다운로드 여부 (`ollama list`) |
