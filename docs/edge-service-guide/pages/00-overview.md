---
slug: overview
title: Overview & Safety Principles
order: 0
summary: What the edge system does, how it stays safe, and who owns each decision.
applies_to: bluei-edge dashboard 0.1.0
last_updated: 2026-06-23
screenshots: []
---

# Overview & Safety Principles

bluei-edge analyzes farm conditions with AI and is built to enable tailored growth
through continuous learning. It is an on-site control system running on the **GX10**
appliance, powered by NVIDIA DGX Spark. It collects sensor and camera data, keeps a local record of everything, applies
safety and feeding logic **on the device**, and synchronizes normalized events up to
`app.bluei.kr` when a network is available.

You operate it from a web browser on the same network:
**`http://<edge-ip>:8080/`**. The dashboard and the device speak to each other directly,
so it keeps working even when the internet is down.

## What the edge system does

- **Collects** water-quality sensor readings, equipment health, and camera status from
  each tank.
- **Records** every event locally first, so nothing is lost when offline.
- **Decides** feeding and safety actions using on-device rules and AI assistance.
- **Synchronizes** field events to `app.bluei.kr` once connectivity returns.

## Who owns each decision (read this first)

> ⚠️ **Real-time safety is owned by the rules engine, not the AI.**
> The AI provides recommendations and analysis. It is **not** the final decision-maker
> for safety. A built-in **safety gate** can block a feeding cycle when water conditions
> are unsafe (for example, dissolved oxygen below threshold), regardless of any AI output.

| Concern | Owner |
|---------|-------|
| Real-time safety (block unsafe actions) | Rules engine / safety gate |
| Feeding recommendations & analysis | AI (advisory) |
| Final operational choices | You, the operator |
| Cloud orchestration & sync | `app.bluei.kr` (not the local runtime) |

This separation is intentional: the device stays safe and predictable even if AI or the
cloud is unavailable.

## How this guide is organized

The guide follows the order you actually work in:

1. **Access & Login** — reach the dashboard and sign in.
2. **Initial Setup** — one-time registration of farm, site, groups, tanks, sensors,
   devices, cameras, controllers, and stocking.
3. **Daily Operations** — monitoring, feeding cycles, alerts.
4. **AI Management** — operating policy, inference, on-site training, learned safety.
5. **Production Records & Trade** — events, stocking, shipping, documents.
6. **System Operations** — AI assistant, safe shutdown, offline & sync.
7. **Troubleshooting** and **Glossary**.

> ℹ️ The dashboard is bilingual. Switch between **English** and **한국어** with the
> language selector in the top-right header at any time. Your choice is remembered on
> that browser.

---

**Navigation:** [📖 Contents](../index.md) · [Access & Login →](01-access-and-login.md)
