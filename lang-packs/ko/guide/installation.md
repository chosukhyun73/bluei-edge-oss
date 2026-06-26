---
slug: installation
title: 설치 (직접 설치)
order: 1
summary: 현장 방문 없이, 양식장 현장 디바이스에 bluei-edge를 직접 설치합니다.
applies_to: bluei-edge dashboard 0.1.0
last_updated: 2026-06-24
screenshots: []
---

# 설치 (직접 설치)

bluei-edge는 현장 방문 없이 **직접 설치해 운영**할 수 있도록 만들어졌습니다. 이 페이지는
양식장 현장 디바이스에 처음부터 설치하는 과정을 안내합니다.

> ℹ️ 디바이스에 이미 bluei-edge가 설치·실행 중이라면 [접속 및 로그인](01-access-and-login.md)으로
> 건너뛰세요.

## 준비물

- 현장의 **systemd 기반 Linux 디바이스** — GX10, NVIDIA DGX Spark, 또는 일반 amd64 서버
  (Ubuntu 22.04+, Debian 12+, RHEL 9+ 등).
- **디스크 32GB 이상** (옵션 로컬 LLM 사용 시 50GB+ 추가).
- 해당 디바이스의 **root(sudo) 권한**.
- 대시보드를 열 **같은 LAN의 컴퓨터**(브라우저).

설치 방법은 두 가지이며 결과는 같습니다. 디바이스 환경에 맞는 쪽을 고르세요.

## 방법 A — 릴리스 tarball + 설치 스크립트 (빌드 도구 불필요)

일반 현장 디바이스에 적합합니다. 컴파일할 것이 없습니다.

```bash
# 1) GitHub Releases 에서 최신 bluei-edge-<version>.tar.gz 를 받습니다.
# 2) 내 컴퓨터에서 디바이스로 복사:
scp bluei-edge-<version>.tar.gz user@<edge-ip>:/tmp/

# 3) 디바이스에서 압축을 풀고 설치 스크립트 실행:
ssh user@<edge-ip>
cd /tmp && tar -xzf bluei-edge-<version>.tar.gz
sudo bluei-edge-<version>/deploy/install.sh -t /tmp/bluei-edge-<version>.tar.gz
```

설치 스크립트가 **systemd 서비스**를 등록하고 자동으로 시작합니다. 설치 위치:

```
/opt/bluei-edge/                — 프로그램, 대시보드, 마이그레이션
/etc/bluei-edge/edge.yaml       — 편집 가능한 설정
/var/lib/bluei-edge/edge.db     — 로컬 데이터베이스(내 데이터)
```

## 방법 B — 소스에서 빌드 (git clone)

소스를 직접 수정하거나 이후 `git pull` 로 업데이트하려는 경우에 적합합니다. 디바이스에
**Go 1.21+ 와 Node/npm** 가 설치돼 있어야 합니다.

```bash
git clone https://github.com/chosukhyun73/bluei-edge-oss.git
cd bluei-edge-oss
make build && make dashboard
./bin/bluei-edge migrate --config configs/edge.example.yaml
./bin/bluei-edge run --config configs/edge.example.yaml
# 이후 업데이트: git pull && make dashboard, 그 후 재시작.
```

> 소스 빌드를 systemd 서비스로 운영하려면 `make package` 로 tarball 을 만든 뒤 방법 A의
> `install.sh` 로 설치하세요.

## 대시보드 접속

같은 LAN의 브라우저에서 다음 주소로 접속합니다.

```
http://<edge-ip>:8080/
```

대시보드는 디바이스에서 바로 로드됩니다 — 별도 앱 설치가 필요 없습니다. 이어서
[접속 및 로그인](01-access-and-login.md), [초기 설정](02-initial-setup.md)으로 진행하세요.

## 동작 확인

```bash
sudo systemctl status bluei-edge
curl -sf http://<edge-ip>:8080/healthz
```

## 운영 유지

```bash
sudo systemctl restart bluei-edge        # 설정 수정 후 재시작
journalctl -u bluei-edge -f              # 로그 확인

# 이후 업그레이드 (설정·데이터는 보존됨)
sudo /opt/bluei-edge/deploy/install.sh -t /path/to/bluei-edge-<new-version>.tar.gz
```

> 🧠 **옵션 — 로컬 AI.** bluei-edge는 AI 보조에 로컬 LLM(Ollama)을 사용할 수 있지만 필수는
> 아닙니다. 핵심 모니터링·급이·안전 로직은 LLM 없이도 모두 동작합니다. Ollama 설정과 운영
> 상세는 프로젝트에 포함된 배포 가이드(`docs/deployment-guide.md`)를 참고하세요.

---

**탐색:** [← 개요](00-overview.md) · [📖 목차](../index.md) · [접속 및 로그인 →](01-access-and-login.md)
