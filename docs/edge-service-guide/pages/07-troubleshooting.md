---
slug: troubleshooting
title: Troubleshooting
order: 8
summary: Blank screen, 401, sensors not updating, camera connection failures, and more.
applies_to: bluei-edge dashboard 0.1.0
last_updated: 2026-06-23
screenshots: []
---

# Troubleshooting

Common symptoms and what to check. For installation/deployment-level problems, use the
internal deployment guide rather than this operator guide.

| Symptom | Likely cause | What to do |
|---------|--------------|------------|
| Dashboard is blank / won't load | Runtime not running, or wrong address | Confirm you are at `http://<edge-ip>:8080/` and that the GX10 / runtime is up. |
| `401 Unauthorized` in the browser console | Operator authentication mismatch | Re-open the dashboard from the supported entry point; if it persists, the backend auth/token needs attention. |
| A sensor shows a stale value or `—` | Sensor offline past the threshold, wiring, or the collector is down | Check the sensor connection and the collector status; the reading clears when fresh data arrives. |
| Camera won't connect | Wrong RTSP address or password | Re-check the RTSP details for that camera and re-save its password. |
| A feeding cycle is blocked | Safety gate (DO / temperature / sensor) | Read the gate's reason, resolve the water condition, then retry. See [Daily Operations → safety gate](03-daily-operations.md). |
| "Already an active cycle" when starting | The tank already has a running cycle | Stop / interrupt the active cycle first, then start a new one. |
| Controller not detected | USB / ESP32 not enumerated | Reconnect the device and retry auto-registration in **AI Management → Controllers**. |
| Density shows `0.0` / growth stage "Undetermined" | Tank volume or stocking not set | Complete tank physical info and stocking — see [Initial Setup](02-initial-setup.md). |
| UI text appears in the wrong language | Language toggle | Switch **English / 한국어** in the header. Operator-entered names never translate (they are data). |

> ℹ️ If a problem looks like a hardware or network fault at the device level (power,
> cabling, the GX10 itself), escalate to site support — those are outside the dashboard.

---

**Navigation:** [← System Operations](06-system-operations.md) · [📖 Contents](../index.md) · [Glossary →](08-glossary.md)
