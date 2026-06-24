# bluei-edge Dashboard

RAS 양식장 통합 관제 대시보드 — Tauri 2.x + React 19 + Vite 6 + Tailwind v4

## 요구사항

- Node.js 20+
- Rust 1.75+ (Tauri 네이티브 빌드 시)
- Go backend가 `:8080`에서 실행 중이어야 함

## 의존성 설치

```bash
cd web/dashboard
npm install
```

## 개발 (웹 브라우저)

```bash
npm run dev
# http://localhost:5173 에서 확인
# Go backend (bluei-edge)가 :8080에서 실행 중이어야 그룹 데이터가 로드됨
```

## 개발 (Tauri 네이티브 데스크탑 창)

```bash
npm run tauri dev
# 네이티브 창에서 띄움 — Rust 툴체인 필요
```

## 빌드

```bash
# 정적 dist 빌드
npm run build

# 네이티브 인스톨러 (Rust 필요)
npm run tauri build
```

## 환경 변수

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `VITE_BLUEI_API` | `http://127.0.0.1:8080` | Go backend URL |

`.env.local` 파일에 설정하거나 셸 환경변수로 지정 가능.

## 구조

- `src/lib/api.ts` — Go backend fetch wrapper
- `src/lib/types.ts` — Group, Tank 타입 (백엔드 응답 기준)
- `src/components/` — 컴포넌트 (Phase 2 shell: Header, Footer, GroupSelector, placeholders)
- `src/components/ui/` — shadcn 프리미티브 (Card, Button, Badge)
- `src-tauri/` — Tauri 2.x 설정 및 Rust entry point

## Phase 로드맵

- **Phase 2 (현재)**: App shell + Group 사이드바 + placeholder 메인 패널
- **Phase 3**: TankDetail 위젯, Confidence gauge, Decision routing UI, water-quality chart, sampling form
