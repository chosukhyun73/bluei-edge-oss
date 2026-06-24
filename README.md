# bluei-edge

양식장·수산 현장을 위한 오프라인 지원 엣지 런타임 — 한 대의 디바이스에서 단독으로 동작합니다.
*Offline-capable edge runtime for aquaculture & fisheries — runs standalone on a single device.*

---

## 이게 뭔가요? / What is this?

bluei-edge 는 양식장 현장 디바이스(GX10 / DGX Spark / 일반 amd64 서버)에 설치하는 단일 프로그램입니다.
*bluei-edge is a single program you install on a farm-site device (GX10 / DGX Spark / a plain amd64 server).*

수질 센서·장비 상태·카메라 상태를 수집하고, 모든 이벤트를 로컬에 먼저 기록하며, 급이·안전 로직을 **디바이스 위에서** 실행합니다.
*It collects water-quality sensors, equipment health, and camera status, records every event locally first, and runs feeding/safety logic **on the device.***

인터넷이 끊겨도 계속 동작하고, 네트워크가 복구되면 정규화된 이벤트를 app.bluei.kr 로 동기화합니다.
*It keeps working when the internet is down, and synchronizes normalized events to app.bluei.kr once connectivity returns.*

운영자는 같은 LAN 의 웹 브라우저로 `http://<edge-ip>:8080/` 에 접속해 대시보드를 사용합니다 — 별도 클라이언트 설치가 필요 없습니다.
*Operators use a web browser on the same LAN at `http://<edge-ip>:8080/` — no separate client install is needed.*

> 🌏 이 프로젝트는 현장에 직접 방문하지 않아도 **누구나 스스로 설치해 사용할 수 있도록** 공개되었습니다. 아래 설치 절차만 따라 하면 됩니다.
> *This project is open so that **anyone can install and run it themselves** without an on-site visit. Just follow the install steps below.*

### 안전 원칙 / Safety principle

실시간 안전 판단은 LLM·추론이 아니라 **규칙 엔진(rules engine)** 이 담당합니다. 추론은 권고일 뿐 최종 안전 결정자가 아닙니다.
*Realtime safety decisions belong to the **rules engine**, not to LLM/inference. Inference is advisory and is never the final safety decision-maker.*

---

## 라이선스 / License

**PolyForm Noncommercial License 1.0.0** 로 배포됩니다. 보기·사용·수정·재배포가 자유롭지만 **비상업적 목적에 한합니다.**
*Distributed under the **PolyForm Noncommercial License 1.0.0**. Free to view, use, modify, and redistribute — but **for noncommercial purposes only.***

상업적 이용은 별도 라이선스가 필요합니다. 전문은 [`LICENSE`](LICENSE) 를 참고하세요.
*Commercial use requires a separate license. See [`LICENSE`](LICENSE) for the full terms.*

---

## 시스템 요구사항 / Requirements

**설치할 디바이스 / Device to install on:**

- Linux + systemd (Ubuntu 22.04+, Debian 12+, RHEL 9+ 등 / etc.)
- 디스크 32GB 이상 (로컬 LLM 사용 시 모델용 50GB+ 추가) / 32GB+ disk (50GB+ more for the local LLM model, if used)
- root 권한(sudo) / root (sudo) access
- (옵션) 로컬 LLM 을 쓰려면 Ollama / (optional) Ollama, if you want the local LLM

**소스에서 직접 빌드할 때만 / Only if building from source:**

- Go 1.21+

---

## 설치 / Installation

설치 방법은 두 가지이며 결과는 같습니다. 디바이스 환경에 맞는 쪽을 고르세요.
*Two ways to install — both reach the same result. Pick the one that fits your device.*

### 방법 1 — 소스에서 빌드 (git clone) / Option 1 — build from source (git clone)

> 디바이스에 **Go 1.21+ 와 Node/npm** 가 필요합니다. 소스를 직접 수정하거나 GX10처럼 `git pull` 로 업데이트하려는 경우에 적합합니다.
> *Requires **Go 1.21+ and Node/npm** on the device. Best if you want to modify the source or update via `git pull` (the way the GX10 does).*

```bash
git clone https://github.com/chosukhyun73/bluei-edge-oss.git
cd bluei-edge-oss
make build && make dashboard
./bin/bluei-edge migrate --config configs/edge.example.yaml
./bin/bluei-edge run --config configs/edge.example.yaml
# 업데이트 / update: git pull && make dashboard  (그 후 재시작 / then restart)
```

> systemd 서비스로 자동 시작·재시작까지 원하면 `make package` 로 tarball 을 만들어 방법 2의 install.sh 로 설치하세요.
> *For systemd auto-start/restart, build a tarball with `make package` and install it via Option 2's install.sh.*

### 방법 2 — 릴리스 tarball + install.sh (빌드 불필요) / Option 2 — release tarball + install.sh (no build)

> 디바이스에 빌드 도구가 필요 없습니다. systemd 서비스로 자동 등록됩니다.
> *No build tools on the device. Registers as a systemd service automatically.*

```bash
# GitHub Releases 에서 tarball 다운로드 (또는 소스에서 `make package` 로 직접 생성)
# Download the tarball from GitHub Releases (or build your own with `make package`)
scp bluei-edge-<version>.tar.gz user@<edge-ip>:/tmp/
ssh user@<edge-ip>
cd /tmp && tar -xzf bluei-edge-<version>.tar.gz
sudo bluei-edge-<version>/deploy/install.sh -t /tmp/bluei-edge-<version>.tar.gz
```

install.sh 가 설치하는 위치 / What install.sh sets up:

```
/opt/bluei-edge/                — binary + dashboard + migrations
/etc/bluei-edge/edge.yaml       — 편집 가능한 config / editable config
/etc/bluei-edge/env             — operator API token (자동 생성 / auto-generated)
/var/lib/bluei-edge/edge.db     — SQLite 데이터 / data
/etc/systemd/system/bluei-edge.service
```

---

## 첫 실행 & 접속 / First run & access

install.sh 가 systemd 서비스를 등록·시작합니다. 같은 LAN 브라우저에서 접속하세요.
*install.sh registers and starts the systemd service. Open it from a browser on the same LAN.*

```
http://<edge-ip>:8080/
```

상태 확인 / Check status:

```bash
sudo systemctl status bluei-edge
curl -sf http://<edge-ip>:8080/healthz
```

---

## 운영 명령 / Operating commands

```bash
# 시작 / 중지 / 재시작 / start / stop / restart
sudo systemctl start bluei-edge
sudo systemctl stop bluei-edge
sudo systemctl restart bluei-edge

# 로그 / logs
journalctl -u bluei-edge -f

# config 수정 후 적용 / edit config, then apply
sudo vi /etc/bluei-edge/edge.yaml
sudo systemctl restart bluei-edge

# 업그레이드 (데이터·config 보존) / upgrade (data & config preserved)
sudo /opt/bluei-edge/deploy/install.sh -t /path/to/bluei-edge-<new-version>.tar.gz

# 제거 (데이터 보존) / uninstall (data preserved)
sudo /opt/bluei-edge/deploy/install.sh --uninstall
```

---

## (옵션) 로컬 LLM / Optional: local LLM

bluei-edge 는 AI 보조 기능에 로컬 LLM(Ollama)을 사용할 수 있습니다. 없어도 핵심 동작은 정상입니다.
*bluei-edge can use a local LLM (Ollama) for AI assistance. Core operation works fine without it.*

```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama pull gemma4:26b      # primary
ollama pull gemma4:e4b      # fallback
```

`/etc/bluei-edge/edge.yaml` 에서 활성화 / Enable in `/etc/bluei-edge/edge.yaml`:

```yaml
llm:
  enabled: true
  base_url: http://127.0.0.1:11435
  primary_model: gemma4:26b
  fallback_model: gemma4:e4b
```

---

## 더 알아보기 / Learn more

- **배포 상세** (systemd·업그레이드·LLM·트러블슈팅) / **Deployment details**: [`docs/deployment-guide.md`](docs/deployment-guide.md)
- **운영자 가이드** (로그인·초기설정·일일운영·AI·기록) / **Operator guide**: [`docs/edge-service-guide/`](docs/edge-service-guide/)

---

## 개발 / Development

소스를 직접 빌드·테스트하려는 기여자용 / For contributors building and testing from source:

```bash
# 전체 검사 (gofmt + 테스트 + smoke) / all checks
bash scripts/check.sh

# 테스트만 / tests only
go test ./...

# 로컬에서 직접 실행 / run locally
go build -o bin/bluei-edge ./cmd/bluei-edge/
./bin/bluei-edge check-config --config configs/edge.example.yaml
./bin/bluei-edge migrate --config configs/edge.example.yaml
./bin/bluei-edge run --config configs/edge.example.yaml
```

> Go 가 `$HOME/.local/go/bin` 에 설치돼 있으면 / If Go is under `$HOME/.local/go/bin`:
> `export PATH=$HOME/.local/go/bin:$PATH`

---

## 현재 상태 / Status

> **Phase 1 — 운영 준비 전(Not production-ready).** 런타임 기반(config·storage·로컬 API·mock worker·smoke test)은 마련돼 있으나, 운영용 하드웨어 제어·클라우드 sync 엔진·추론 기반 의사결정은 아직 구현되지 않았습니다.
> *Phase 1 — Not production-ready. The runtime foundation (config, storage, local API, mock workers, smoke test) is present; production hardware control, the cloud sync engine, and inference-based decisions are not implemented yet.*
