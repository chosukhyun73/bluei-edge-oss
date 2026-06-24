---
slug: daily-operations
title: Daily Operations
order: 3
summary: Monitor tanks, run feeding cycles (auto/manual), and read alerts and the safety gate.
applies_to: bluei-edge dashboard 0.1.0
last_updated: 2026-06-23
screenshots: [SS-11, SS-12, SS-13]
---

# Daily Operations

This is your everyday screen. Open the **Site Tank Management** tab to see status at a
glance, start or stop feeding, and watch water quality.

## Read the Production Overview

The top of **Site Tank Management** shows a summary bar for the selected farm/site:

- **Active Groups** — groups with at least one active tank, out of the total.
- **Total Fish** — summed fish count across tanks.
- **Avg FCR** — average feed conversion ratio (shows `—` until there is data).
- **Harvest Imminent** — tanks approaching harvest.
- **Vacant** — empty tanks, out of the total.

Below it, each tank is a **card** showing its name, status (for example **Vacant** or an
active cycle), the current policy, and a **Start** action.

> 📸 **SS-11** · Production Overview + tank cards · _capture pending_ — see [registry](../SCREENSHOTS.md#ss-11)

> ℹ️ Group and tank **names** are shown exactly as you entered them (your data), in both
> languages. Only system labels translate when you switch language.

## Run a feeding cycle

1. On a tank card, select **Start**.
2. Choose the mode:
   - **Auto (AI-operated)** — the system times and sizes feeding within policy and safety.
   - **Manual (operator-controlled)** — you drive the pulses yourself.
3. Confirm the **daily target**. The form warns you if:
   - the tank already has an **active cycle** (stop it first), or
   - the cycle would **exceed the daily target**.
4. Start the cycle. To end early, **stop / interrupt** the active cycle from the same card
   or the **Feed Cycle** monitor.

> 📸 **SS-12** · Feed cycle start form (auto/manual, daily target, warnings) · _capture pending_ — see [registry](../SCREENSHOTS.md#ss-12)

## Understand the safety gate

Before a cycle begins, a rule-based **safety gate** checks water conditions and can
**block** the cycle. Typical block reasons, shown in plain language:

- **Temperature** below/above the critical threshold.
- **Dissolved oxygen (DO)** below the critical threshold.
- A sensor reading that is **missing** or **stale** (not updated for several minutes).

When blocked, resolve the water condition first, then retry.

> ⚠️ The safety gate is owned by the **rules engine**, not the AI — it applies regardless
> of any AI recommendation. See [Overview & Safety Principles](00-overview.md).

## Monitor water quality

- The **sensor matrix** shows current values per metric (water temperature, DO, pH,
  salinity, ammonia, and more) with units.
- **Real-time trend** charts plot recent history for a selected metric.
- A **stale** reading means the sensor has not reported recently — check the sensor and the
  collector (see [Troubleshooting](07-troubleshooting.md)).

> 📸 **SS-13** · Sensors / real-time trend · _capture pending_ — see [registry](../SCREENSHOTS.md#ss-13)

## Read alerts

Alerts surface conditions that need attention. Severity is shown as:

- **Critical** — act now (e.g. a safety-critical water condition).
- **Warning** — investigate soon.
- **Info** — for awareness.

---

**Navigation:** [← Initial Setup](02-initial-setup.md) · [📖 Contents](../index.md) · [AI Management →](04-ai-management.md)
