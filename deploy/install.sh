#!/bin/bash
# bluei-edge installer — single-device edge appliance deployment
#
# Target: GX10 (Nvidia DGX Spark), amd64 servers, or any Linux with systemd.
# Idempotent: re-running upgrades the install in place.
#
# Layout after install:
#   /opt/bluei-edge/             — binary + dashboard + migrations (immutable artifacts)
#   /etc/bluei-edge/             — operator-editable config (edge.yaml + env)
#   /var/lib/bluei-edge/         — runtime data (sqlite DB, WAL, snapshots)
#   /etc/systemd/system/bluei-edge.service
#
# Usage:
#   sudo ./install.sh                       # install from sibling tarball
#   sudo ./install.sh -t /path/to/tar.gz    # install from explicit tarball
#   sudo ./install.sh --uninstall           # remove install (preserves /var/lib data)
#
# Requirements: root (sudo), systemd, tar.

set -euo pipefail

INSTALL_DIR="/opt/bluei-edge"
CONFIG_DIR="/etc/bluei-edge"
DATA_DIR="/var/lib/bluei-edge"
SERVICE_NAME="bluei-edge"
SVC_USER="bluei-edge"
SVC_GROUP="bluei-edge"

TARBALL=""
ACTION="install"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -t|--tarball) TARBALL="$2"; shift 2 ;;
    --uninstall)  ACTION="uninstall"; shift ;;
    -h|--help)
      sed -n '2,30p' "$0"; exit 0 ;;
    *)
      echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

require_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "this script must be run as root (try: sudo $0 $*)" >&2
    exit 1
  fi
}

require_root

# ── uninstall ─────────────────────────────────────────────────────────────────
if [[ "$ACTION" == "uninstall" ]]; then
  echo "→ stopping ${SERVICE_NAME}"
  systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
  systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
  systemctl daemon-reload || true
  rm -rf "${INSTALL_DIR}"
  echo "→ uninstall complete."
  echo "  Config preserved: ${CONFIG_DIR}/"
  echo "  Data preserved:   ${DATA_DIR}/"
  echo "  Remove manually if you want a clean slate."
  exit 0
fi

# ── locate tarball ────────────────────────────────────────────────────────────
if [[ -z "$TARBALL" ]]; then
  HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  for cand in "$HERE"/../bluei-edge-*.tar.gz "$HERE"/bluei-edge-*.tar.gz; do
    if [[ -f "$cand" ]]; then TARBALL="$cand"; break; fi
  done
fi
if [[ -z "$TARBALL" || ! -f "$TARBALL" ]]; then
  echo "no tarball found. Pass with -t /path/to/bluei-edge-VERSION.tar.gz" >&2
  exit 1
fi
echo "→ installing from: $TARBALL"

# ── user/group ───────────────────────────────────────────────────────────────
if ! id -u "$SVC_USER" >/dev/null 2>&1; then
  echo "→ creating service user: $SVC_USER"
  useradd --system --no-create-home --shell /usr/sbin/nologin --user-group "$SVC_USER"
fi

# ── directories ──────────────────────────────────────────────────────────────
mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$DATA_DIR"
chown -R "$SVC_USER:$SVC_GROUP" "$DATA_DIR"

# ── extract tarball ──────────────────────────────────────────────────────────
echo "→ extracting to $INSTALL_DIR"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT
tar -xzf "$TARBALL" -C "$TMPDIR"
SRC="$(ls -d "$TMPDIR"/bluei-edge-* | head -1)"
if [[ -z "$SRC" ]]; then
  echo "tarball did not contain a bluei-edge-* directory" >&2
  exit 1
fi

rm -rf "$INSTALL_DIR/bin" "$INSTALL_DIR/migrations" "$INSTALL_DIR/web"
cp -r "$SRC/bin"         "$INSTALL_DIR/"
cp -r "$SRC/migrations"  "$INSTALL_DIR/"
cp -r "$SRC/web"         "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/bin/"*

# ── config seed ──────────────────────────────────────────────────────────────
if [[ ! -f "$CONFIG_DIR/edge.yaml" ]]; then
  echo "→ seeding config: $CONFIG_DIR/edge.yaml"
  cp "$SRC/configs/edge.example.yaml" "$CONFIG_DIR/edge.yaml"
  sed -i -E 's|sqlite_path:\s*\./var/bluei-edge/edge\.db|sqlite_path: /var/lib/bluei-edge/edge.db|' \
    "$CONFIG_DIR/edge.yaml"
  for f in "$SRC"/configs/*.example.yaml; do
    base="$(basename "$f")"
    [[ "$base" == "edge.example.yaml" ]] && continue
    [[ -f "$CONFIG_DIR/$base" ]] || cp "$f" "$CONFIG_DIR/$base"
  done
else
  echo "→ existing config preserved: $CONFIG_DIR/edge.yaml"
fi

if [[ ! -f "$CONFIG_DIR/env" ]]; then
  TOKEN="$(openssl rand -hex 32 2>/dev/null || head -c 32 /dev/urandom | xxd -p)"
  cat > "$CONFIG_DIR/env" <<EOF
# bluei-edge operator API token. Keep this file secret.
BLUEI_EDGE_OPERATOR_TOKEN=${TOKEN}
EOF
  chmod 600 "$CONFIG_DIR/env"
  echo "→ generated operator token: $CONFIG_DIR/env"
fi

chown -R root:"$SVC_GROUP" "$CONFIG_DIR"
chmod 750 "$CONFIG_DIR"

# ── systemd unit ─────────────────────────────────────────────────────────────
echo "→ installing systemd unit"
cp "$SRC/deploy/systemd/${SERVICE_NAME}.service" "/etc/systemd/system/"
systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"

if systemctl is-active --quiet "${SERVICE_NAME}"; then
  echo "→ restarting ${SERVICE_NAME}"
  systemctl restart "${SERVICE_NAME}"
else
  echo "→ starting ${SERVICE_NAME}"
  systemctl start "${SERVICE_NAME}"
fi

sleep 1
if systemctl is-active --quiet "${SERVICE_NAME}"; then
  echo "✓ ${SERVICE_NAME} is running"
  PORT="$(grep -E '^\s*port:' "$CONFIG_DIR/edge.yaml" | awk '{print $2}' | head -1)"
  echo "  dashboard: http://$(hostname -I | awk '{print $1}'):${PORT:-8080}/"
  echo "  config:    $CONFIG_DIR/edge.yaml"
  echo "  data:      $DATA_DIR/"
  echo "  logs:      journalctl -u ${SERVICE_NAME} -f"
else
  echo "✗ ${SERVICE_NAME} failed to start" >&2
  journalctl -u "${SERVICE_NAME}" -n 30 --no-pager
  exit 1
fi
