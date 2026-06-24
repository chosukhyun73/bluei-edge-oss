#!/usr/bin/env bash
# scripts/check.sh — Phase 1 local CI check.
# Runs: gofmt lint, unit tests, smoke test.
# Usage: bash scripts/check.sh

set -euo pipefail

fail() { echo "FAIL: $*"; exit 1; }
pass() { echo "PASS: $*"; }

echo "=== bluei-edge check ==="
echo

# --- PATH ---
if ! command -v go &>/dev/null; then
  export PATH="$HOME/.local/go/bin:$PATH"
fi
command -v go &>/dev/null || fail "go not found; add Go to PATH (e.g. export PATH=\$HOME/.local/go/bin:\$PATH)"

echo "go: $(go version)"
echo

# --- gofmt ---
echo "-- gofmt"
mapfile -t go_files < <(find . \
  -path './.git' -prune -o \
  -path './bin' -prune -o \
  -path './var' -prune -o \
  -name '*.go' -type f -print | sort)

if [ "${#go_files[@]}" -eq 0 ]; then
  fail "no Go files found"
fi

unformatted=$(gofmt -l "${go_files[@]}")
if [ -n "$unformatted" ]; then
  echo "  Files need formatting:"
  echo "$unformatted" | sed 's/^/    /'
  fail "gofmt check failed; run: gofmt -w ./cmd ./internal"
fi
pass "gofmt clean"
echo

# --- go test ---
echo "-- go test ./..."
go test ./...
pass "go test passed"
echo

# --- smoke test ---
echo "-- scripts/smoke.sh"
bash scripts/smoke.sh
echo

echo "=== CHECK PASSED ==="
