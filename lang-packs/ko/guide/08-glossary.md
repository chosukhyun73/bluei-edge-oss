---
slug: glossary
title: 용어집
order: 8
summary: BSF, GET, 밀도, RAS, WTG, 안전 게이트 등 대시보드에서 사용하는 용어.
applies_to: bluei-edge dashboard 0.1.0
last_updated: 2026-06-23
screenshots: []
---

# 용어집

bluei-edge 대시보드에서 사용되는 용어입니다.

| 용어 | 의미 |
|------|---------|
| **RAS** | 순환여과양식(Recirculating Aquaculture System) — 물을 여과해 재사용하는 육상 수조 양식. |
| **Cage / Marine** | 해상 가두리 양식. 두 가지 사이트 유형 중 하나(다른 하나는 **육상(RAS)**). |
| **Group** | 수조를 정리하고 비교하기 위한 수조 묶음(예: 하나의 RAS 라인). |
| **Cage / Tank** | 단일 양식 단위(RAS의 수조, 해상의 가두리). |
| **Stocking** | 수조에 입식된 어류 기록 — 어종, 마릿수, 평균 체중. |
| **Density** | **kg/m³** 단위의 입식 밀도. 바이오매스와 수조 용적으로 계산하며, 안전/급이 정책에 사용됩니다. |
| **Biomass** | 수조 내 어류의 총 생체중(마릿수 × 평균 체중). |
| **Operating mode** | **자동 (AI 운영)** 대 **수동 (운영자 제어)** 급이 제어. |
| **BSF policy** | 그룹의 급이 기조: **공격적 / 일반적 / 안정적**. |
| **GET₉₅ / GET₅₀** | 소화 배출 시간(Gut Evacuation Time) 목표 — 다음 급이 사이클의 간격 조절에 사용. |
| **Daily cycle cap** | 하루에 허용되는 최대 급이 사이클 수. |
| **Safety gate** | 안전하지 않은 수질 상태(용존산소(DO), 수온, 누락/오래된 센서)에서 급이 사이클을 차단할 수 있는 규칙 기반 점검. AI가 아닌 규칙 엔진이 소유합니다. |
| **Arbiter** | 제어 결정을 중재하는 시스템 구성요소. 실시간 동작을 소유하며, AI가 아닙니다. |
| **Inference** | 카메라 프레임에 대한 온디바이스 AI 분석(마릿수, 행동, 크기, 급이 반응). |
| **On-Site AI Training** | 자체 카메라 영상에서 어류 주위에 박스를 그려 AI를 학습시킨 뒤 모델을 훈련하는 현장 AI 학습. |
| **Controller (ESP32)** | 장비를 구동하는 현장 마이크로컨트롤러. USB 자동 등록이 가능합니다. |
| **Actuator** | 제어 가능한 장비(급이기, 펌프, 히터, UV 등). |
| **WTG** | 수처리 그룹(Water Treatment Group) — 사이트에 공급되는 공용 수처리 장치. |
| **DO** | 용존산소(Dissolved Oxygen, mg/L) — 안전에 직결되는 수질 지표. |
| **FCR** | 사료효율(Feed Conversion Ratio) — 성장 단위당 사용한 사료량. |
| **Edge / GX10** | bluei-edge를 실행하는 현장 장치. `http://<edge-ip>:8080/`에서 대시보드를 제공합니다. |
| **app.bluei.kr** | 엣지가 현장 이벤트를 동기화하는 클라우드 서비스. |

---

**탐색:** [← 문제 해결](07-troubleshooting.md) · [📖 목차](../index.md)
