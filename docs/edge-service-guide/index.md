---
slug: home
title: bluei-edge Operator Guide
order: -1
summary: Wiki home and table of contents for the bluei-edge operator guide.
applies_to: bluei-edge dashboard 0.1.0
last_updated: 2026-06-23
is_home: true
---

# bluei-edge Operator Guide

The operator guide for **bluei-edge** — the on-site control system that runs on the
**GX10** appliance at your farm. You use it from a browser on the same network:
**`http://<edge-ip>:8080/`**.

This is the home page. Pick a section below, or read top to bottom for a full walkthrough
from first login to daily operation.

> ℹ️ The dashboard is bilingual — switch **English / 한국어** from the top-right header at
> any time. This guide is written for the English UI.

## Table of contents

| # | Page | What it covers |
|---|------|----------------|
| 0 | [Overview & Safety Principles](pages/00-overview.md) | What the system does, how it stays safe, and who owns each decision. |
| 1 | [Installation (self-install)](pages/installation.md) | Install bluei-edge yourself on a farm-site device, with no on-site visit required. |
| 2 | [Access & Login](pages/01-access-and-login.md) | Reach the dashboard, sign in via phone approval, and read the screen layout. |
| 3 | [Initial Setup (one-time)](pages/02-initial-setup.md) | Register farm, site, groups, tanks, sensors, devices, cameras, controllers, and stocking. |
| 4 | [Daily Operations](pages/03-daily-operations.md) | Monitor tanks, run feeding cycles (auto/manual), read alerts and the safety gate. |
| 5 | [AI Management](pages/04-ai-management.md) | Operating policy, inference, on-site AI training, and learned safety. |
| 6 | [Production Records & Trade](pages/05-records-and-trade.md) | Log mortality/treatment/transfer and manage stocking, shipping, and documents. |
| 7 | [System Operations](pages/06-system-operations.md) | AI assistant, safe shutdown, offline behavior, and sync. |
| 8 | [Troubleshooting](pages/07-troubleshooting.md) | Common symptoms and fixes. |
| 9 | [Glossary](pages/08-glossary.md) | BSF, GET, density, RAS, WTG, safety gate, and other terms. |

## New here? Start with these

1. **[Access & Login](pages/01-access-and-login.md)** — get into the dashboard.
2. **[Initial Setup](pages/02-initial-setup.md)** — register your farm and first tanks.
3. **[Daily Operations](pages/03-daily-operations.md)** — run your first feeding cycle.

## Safety in one line

> ⚠️ Real-time safety is owned by the **rules engine / safety gate**, not the AI. The AI is
> advisory; the device stays safe even when AI or the cloud is unavailable. See
> [Overview & Safety Principles](pages/00-overview.md).

---

_Applies to bluei-edge dashboard **0.1.0** · last updated **2026-06-23**. Page order and
titles are defined in [`manifest.yaml`](manifest.yaml); maintenance conventions are in
[`README.md`](README.md)._
