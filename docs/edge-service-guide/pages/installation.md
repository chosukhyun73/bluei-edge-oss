---
slug: installation
title: Installation (self-install)
order: 1
summary: Install bluei-edge yourself on a farm-site device, with no on-site visit required.
applies_to: bluei-edge dashboard 0.1.0
last_updated: 2026-06-24
screenshots: []
---

# Installation (self-install)

bluei-edge is built so you can install and run it **yourself**, without an on-site
visit. This page walks you through installing it on a farm-site device from scratch.

> ℹ️ If your device already has bluei-edge installed and running, skip ahead to
> [Access & Login](01-access-and-login.md).

## What you need

- A **Linux device with systemd** at the farm — GX10, NVIDIA DGX Spark, or a plain
  amd64 server (Ubuntu 22.04+, Debian 12+, RHEL 9+, etc.).
- At least **32 GB of disk** (plus 50 GB+ if you will run the optional local LLM).
- **root (sudo)** access on that device.
- A computer on the **same local network (LAN)** to open the dashboard in a browser.

There are two ways to install — pick the one that fits your device. Both reach the
same result.

## Option A — release tarball + installer (no build tools)

Best for a normal farm device. Nothing to compile.

```bash
# 1) Get the latest bluei-edge-<version>.tar.gz from GitHub Releases.
# 2) From your computer, copy it to the device:
scp bluei-edge-<version>.tar.gz user@<edge-ip>:/tmp/

# 3) On the device, unpack and run the installer:
ssh user@<edge-ip>
cd /tmp && tar -xzf bluei-edge-<version>.tar.gz
sudo bluei-edge-<version>/deploy/install.sh -t /tmp/bluei-edge-<version>.tar.gz
```

The installer registers a **systemd service** and starts it automatically. It places
files here:

```
/opt/bluei-edge/                — program, dashboard, migrations
/etc/bluei-edge/edge.yaml       — your editable configuration
/var/lib/bluei-edge/edge.db     — local database (your data)
```

## Option B — build from source (git clone)

Best if you want to modify the source, or update later with `git pull`. This requires
**Go 1.21+ and Node/npm** installed on the device.

```bash
git clone https://github.com/chosukhyun73/bluei-edge-oss.git
cd bluei-edge-oss
make build && make dashboard
./bin/bluei-edge migrate --config configs/edge.example.yaml
./bin/bluei-edge run --config configs/edge.example.yaml
# To update later: git pull && make dashboard, then restart.
```

> To turn a source build into a systemd-managed service, run `make package` to produce a
> tarball, then install it with Option A's `install.sh`.

## Open the dashboard

On any browser on the same LAN, go to:

```
http://<edge-ip>:8080/
```

The dashboard loads directly from the device — no separate app to install. Then continue
with [Access & Login](01-access-and-login.md) and
[Initial Setup](02-initial-setup.md).

## Confirm it is running

```bash
sudo systemctl status bluei-edge
curl -sf http://<edge-ip>:8080/healthz
```

## Keeping it running

```bash
sudo systemctl restart bluei-edge        # restart after editing config
journalctl -u bluei-edge -f              # watch logs

# Upgrade later (your config and data are preserved)
sudo /opt/bluei-edge/deploy/install.sh -t /path/to/bluei-edge-<new-version>.tar.gz
```

> 🧠 **Optional — local AI.** bluei-edge can use a local LLM (Ollama) for AI assistance,
> but it is not required: core monitoring, feeding, and safety logic all work without it.
> For Ollama setup and full operational details, see the deployment guide that ships with
> the project (`docs/deployment-guide.md`).

---

**Navigation:** [← Overview](00-overview.md) · [📖 Contents](../index.md) · [Access & Login →](01-access-and-login.md)
