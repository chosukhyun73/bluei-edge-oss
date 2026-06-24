---
slug: home
title: bluei-edge 운영자 가이드
order: -1
summary: bluei-edge 운영자 가이드의 위키 홈 및 목차입니다.
applies_to: bluei-edge dashboard 0.1.0
last_updated: 2026-06-23
is_home: true
---

# bluei-edge 운영자 가이드

**bluei-edge** 운영자 가이드입니다. bluei-edge는 농장의 **GX10** 장비에서 동작하는
현장 제어 시스템입니다. 같은 네트워크에 있는 브라우저에서 다음 주소로 접속해 사용합니다.
**`http://<edge-ip>:8080/`**.

이 페이지는 홈 화면입니다. 아래에서 섹션을 선택하거나, 위에서 아래로 차례대로 읽으면
첫 로그인부터 일상 운영까지 전체 과정을 따라갈 수 있습니다.

> ℹ️ 대시보드는 이중 언어를 지원합니다. 우측 상단 헤더에서 언제든지 **English / 한국어**를
> 전환할 수 있습니다. 이 가이드는 영어 UI를 기준으로 작성되었습니다.

## 목차

| # | 페이지 | 내용 |
|---|------|----------------|
| 0 | [개요 및 안전 원칙](pages/00-overview.md) | 시스템이 하는 일, 안전을 유지하는 방식, 각 의사결정의 주체. |
| 1 | [접속 및 로그인](pages/01-access-and-login.md) | 대시보드 접속, 휴대폰 승인 로그인, 화면 구성 이해. |
| 2 | [초기 설정 (최초 1회)](pages/02-initial-setup.md) | 농장, 사이트, 그룹, 수조, 센서, 장비, 카메라, 컨트롤러, 입식 등록. |
| 3 | [일상 운영](pages/03-daily-operations.md) | 수조 모니터링, 사료 사이클 실행(자동/수동), 알림 및 안전 게이트 확인. |
| 4 | [인공지능 관리 (AI)](pages/04-ai-management.md) | 운영 정책, 추론, 현장 AI 학습, 학습 기반 안전. |
| 5 | [생산 기록 및 거래](pages/05-records-and-trade.md) | 폐사/처치/이동 기록 및 입식, 출하, 서류 관리. |
| 6 | [시스템 운영](pages/06-system-operations.md) | AI 운영 도우미, 안전 종료, 오프라인 동작, 동기화. |
| 7 | [문제 해결](pages/07-troubleshooting.md) | 일반적인 증상과 해결 방법. |
| 8 | [용어집](pages/08-glossary.md) | BSF, GET, 밀도, RAS, WTG, 안전 게이트 등의 용어. |

## 처음이신가요? 여기서 시작하세요

1. **[접속 및 로그인](pages/01-access-and-login.md)** — 대시보드에 접속합니다.
2. **[초기 설정](pages/02-initial-setup.md)** — 농장과 첫 수조를 등록합니다.
3. **[일상 운영](pages/03-daily-operations.md)** — 첫 사료 사이클을 실행합니다.

## 한 줄로 보는 안전

> ⚠️ 실시간 안전은 AI가 아니라 **규칙 엔진 / 안전 게이트**가 책임집니다. AI는
> 보조적 역할이며, AI나 클라우드를 사용할 수 없는 상황에서도 장비는 안전하게 유지됩니다.
> [개요 및 안전 원칙](pages/00-overview.md)을 참고하세요.

---

_bluei-edge dashboard **0.1.0** 기준 · 최종 업데이트 **2026-06-23**. 페이지 순서와
제목은 [`manifest.yaml`](manifest.yaml)에 정의되어 있으며, 유지보수 규칙은
[`README.md`](README.md)에 있습니다._
