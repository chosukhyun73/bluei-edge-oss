#!/usr/bin/env bash
#
# fetch-knowledge.sh — GX10 가 게이팅 허브에서 AI 지식팩(rag-index)을 받아 설치한다.
#
# 5단계(소비, GX10 측). 게이팅: Authorization: Bearer <TOKEN>(허브가 검증).
# 버전이 같으면 다운로드 skip(idempotent). 설치 후 bluei-edge 재시작 필요(부팅 시 인덱스 로드).
#
# 환경변수:
#   BLUEI_KB_URL    게이팅 허브 베이스 URL (예: https://api.bluei.kr/gx10/knowledge)
#   BLUEI_KB_TOKEN  구매자/노드 토큰 (게이팅)
#   BLUEI_KB_DIR    설치 경로 (기본 /var/lib/bluei-edge/knowledge)
#
# 사용:
#   BLUEI_KB_URL=https://… BLUEI_KB_TOKEN=… sudo -E bash deploy/fetch-knowledge.sh
#   (이후) sudo systemctl restart bluei-edge

set -euo pipefail

BASE_URL="${BLUEI_KB_URL:-}"
TOKEN="${BLUEI_KB_TOKEN:-}"
TARGET="${BLUEI_KB_DIR:-/var/lib/bluei-edge/knowledge}"

[ -n "$BASE_URL" ] || { echo "[err] BLUEI_KB_URL 미설정" >&2; exit 1; }
BASE_URL="${BASE_URL%/}"

auth=()
[ -n "$TOKEN" ] && auth=(-H "Authorization: Bearer $TOKEN")

sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }
jget() { python3 -c "import json,sys;print(json.load(open(sys.argv[1]))[sys.argv[2]])" "$1" "$2"; }

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

echo "· 버전 확인: $BASE_URL/latest.json"
curl -fsSL ${auth[@]+"${auth[@]}"} "$BASE_URL/latest.json" -o "$tmp/latest.json"
ver="$(jget "$tmp/latest.json" version)"
file="$(jget "$tmp/latest.json" file)"
want="$(jget "$tmp/latest.json" sha256)"

cur="$(cat "$TARGET/.version" 2>/dev/null || echo none)"
if [ "$ver" = "$cur" ]; then
  echo "✓ 이미 최신 지식팩 (v$ver) — 설치 생략."
  exit 0
fi

echo "· 새 버전 $ver (현재 $cur) 다운로드…"
curl -fsSL ${auth[@]+"${auth[@]}"} "$BASE_URL/$file" -o "$tmp/pack.tar.gz"
got="$(sha256 "$tmp/pack.tar.gz")"
[ "$got" = "$want" ] || { echo "[err] sha256 불일치 (want=$want got=$got)" >&2; exit 1; }

tar -xzf "$tmp/pack.tar.gz" -C "$tmp"
src="$(find "$tmp" -name rag-index.jsonl | head -1)"
[ -f "$src" ] || { echo "[err] 패키지에 rag-index.jsonl 없음" >&2; exit 1; }

mkdir -p "$TARGET"
cp "$src" "$TARGET/rag-index.jsonl"
man="$(dirname "$src")/rag-index.manifest.json"
[ -f "$man" ] && cp "$man" "$TARGET/"
echo "$ver" > "$TARGET/.version"

echo "✓ 설치 완료: $TARGET/rag-index.jsonl (v$ver)"
echo "  적용하려면: sudo systemctl restart bluei-edge"
echo "  (config knowledge.enabled=true, index_path=$TARGET/rag-index.jsonl 확인)"
