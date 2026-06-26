---
slug: ai-management
title: AI Management
order: 5
summary: Operating policy, inference monitor, on-site AI training, and learned safety rules.
applies_to: bluei-edge dashboard 0.1.0
last_updated: 2026-06-23
screenshots: [SS-14, SS-15, SS-16, SS-17]
---

# AI Management

The **AI Management** tab groups the advanced, AI-related controls. It has these sub-tabs:
**Operating Policy**, **Inference Monitor**, **Feed Cycle**, **Safety Learning**,
**Model Management**, **Learning Data**, **On-Site AI Training**, **Controllers**, **Admin**.

> ⚠️ AI here is **advisory and analytical**. It does not override the safety gate. Real-time
> safety stays with the rules engine — see [Overview & Safety Principles](00-overview.md).

## Operating Policy (BSF · Density · GET)

Sets how feeding behaves for a group and shows the resulting per-tank numbers.

- **Group BSF policy** — **Aggressive / Standard / Conservative** feeding stance.
- **Operating mode** — **Auto (AI-operated)** or **Manual (operator-controlled)**.
- **Daily cycle cap** — the maximum feeding cycles per day.
- Each tank card shows: **growth stage**, **BSF**, **daily feed**, **density (kg/m³)**,
  **GET₉₅ / GET₅₀** (gut-evacuation-time targets that pace the next feeding), **daily
  cycles**, and **target per cycle**. A per-tank **Policy** override is available.

> 📸 **SS-14** · Operating Policy (BSF · Density · GET) · _capture pending_ — see [registry](../SCREENSHOTS.md#ss-14)

> ℹ️ Density needs tank **volume** and **stocking** to be set — see
> [Initial Setup](02-initial-setup.md). Missing inputs show a warning such as
> "insufficient stocking info".

## Inference Monitor

Shows live AI analysis of camera frames per tank, with a time-range selector
(**1 hour … since stocking**). Use it to watch counts and feeding response over time.

> 📸 **SS-15** · Inference Monitor · _capture pending_ — see [registry](../SCREENSHOTS.md#ss-15)

## Feed Cycle (monitor)

Follows an active feeding cycle in detail — scheduled vs executed pulses, amounts, and
weight feedback from the feeder. This is the detailed counterpart to the **Start** action
in [Daily Operations](03-daily-operations.md).

## On-Site AI Training

This is where the AI is taught — and where the algorithms that run the whole system are
set. There are two kinds of learning, and all of it happens **on the GX10 (NVIDIA DGX
Spark) at your farm**:

- **Models you teach** (vision + behavior): the **detector (YOLO)** and the **behavior model (LRCN)**.
- **Models that learn by themselves** (per tank, no labeling): the **anomaly baseline**
  and the **water-quality forecast**.

> ✅ Training is safe to run anytime — the **current AI keeps working** during training, and
> a newly trained model goes live **only after you deploy it**. You can roll back at any time.

### 1 · Teach the detector (YOLO) — draw boxes

The **detector (YOLO)** finds **where the fish are** in the camera image (count and position). It
needs your labels to start.

1. Open the **Bootstrap** tab and load a camera **snapshot**.
2. **Draw boxes** by dragging around fish (and, where shown, feed / exclude zones). You do
   not need to mark every fish — just the clearly visible ones in each scene.
3. **Save** and move to a new scene; vary time of day, angle, and fish position.
4. The on-screen counters show progress toward the **minimum boxes and frames** — more and
   more varied examples improve accuracy.

> 📸 **SS-16** · On-Site AI Training — drawing boxes · _capture pending_ — see [registry](../SCREENSHOTS.md#ss-16)

### 2 · Teach the behavior model — score clips ★ main model

The **behavior model (LRCN)** is the **main operating model**. It watches short video
clips and scores **how actively the fish are feeding (0–1)**. That score is what drives
feeding decisions — always together with the safety rules.

1. The camera **continuously captures ~7-second clips** into two pools: **Feeding response**
   (during a feeding cycle) and **Stability** (outside feeding). This requires continuous
   capture to have been running for a few days.
2. Open the **Dispute** tab, **fetch a clip**, watch it, and **score it 0–1** for the shown
   phase; mark it **right / wrong / unsure** and add an optional note.
3. If a clip is unusable, **quarantine** it (occlusion, low visibility, other) so it is
   excluded from training.
4. Keep scoring until the on-screen gate is met.

> ℹ️ Until the behavior model has been trained the first time, feeding decisions fall back
> to the **safety rules** — so the farm stays safe while the AI is still learning.

### 3 · Run training, review, and deploy

1. Press **Start AI Training**. It trains the detector and behavior model together, usually
   in **5–30 minutes**, in the background.
2. When it finishes, review the **Test Results** — three signal lights: **accuracy**,
   **response speed (latency)**, and **image clarity**. Green means safe to use.
3. Press **Deploy to field** to switch all decisions to the new model, or **roll back** to
   the previous one. Nothing changes in production until you deploy.

### 4 · Per-tank models that learn by themselves (no labeling)

These run per Cage/Tank and need **no operator labeling** — only enough operating history.
They raise the AI's **confidence** for that tank, which is what unlocks more autonomy.

- **Anomaly baseline** — learns a tank's **normal pattern** from **7+ days** of operation
  (an autoencoder). Pick the tank and **Start training now**. Afterward the system
  periodically computes an **anomaly score** and raises a dashboard alert when the tank
  drifts from normal.
- **Short-term water-quality forecast** — predicts **dissolved oxygen (DO)** at
  **t+10 / 30 / 60 / 120 minutes** from the last hour of DO / temperature / pH. Pick the tank
  and **Train Now**. Predictions then appear in the state vector under **Water → Predictions**.

### 5 · Training data & disk

Clips are kept in a **training pool**; unusable clips are moved to **training-excluded**
(low visibility / occlusion / other) so they never affect learning. The **captures Disk
Usage** card shows pool size and free space, and old data is cleaned up automatically as
the disk fills.

### How it fits together

**Detector (YOLO)** finds the fish → the **behavior model (LRCN)** scores feeding activity → the
decision router combines that score with the tank's **confidence**, the **operating mode**,
and the **safety gate / rules** to choose the action. The **anomaly baseline** and **water
forecast** raise confidence and add early warnings. Real-time safety always stays with the
rules engine — see [Overview & Safety Principles](00-overview.md).

## Model Management & Learning Data

- **Model Management** — the model library: candidate vs verified models, with
  promotion/rollback and timestamps.
- **Learning Data** — the captured/training data pools and disk usage.

## Safety Learning

Shows **learned safety rules** mined from operation, and lets you **dispute** a decision
when the system got it wrong — categorized as **wrong condition**, **wrong action**, or
**wrong timing**. Disputes feed back into rule mining.

> 📸 **SS-17** · Safety Learning — learned rules + dispute · _capture pending_ — see [registry](../SCREENSHOTS.md#ss-17)

## Controllers & Admin

- **Controllers** — list controllers, run a self-test, and register new ones (including
  ESP32 **USB auto-registration**; see [Initial Setup §5](02-initial-setup.md)).
- **Admin** — registries for water-treatment groups (**WTG**), species, and models. These
  are commissioning/advanced tools, not part of daily operation.

---

**Navigation:** [← Daily Operations](03-daily-operations.md) · [📖 Contents](../index.md) · [Production Records & Trade →](05-records-and-trade.md)
