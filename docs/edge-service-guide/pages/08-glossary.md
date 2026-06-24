---
slug: glossary
title: Glossary
order: 8
summary: BSF, GET, density, RAS, WTG, safety gate, and other terms used in the dashboard.
applies_to: bluei-edge dashboard 0.1.0
last_updated: 2026-06-23
screenshots: []
---

# Glossary

Terms as they are used in the bluei-edge dashboard.

| Term | Meaning |
|------|---------|
| **RAS** | Recirculating Aquaculture System — land-based tank farming that filters and reuses water. |
| **Cage / Marine** | Sea-based cage culture; one of the two site types (the other is **Land (RAS)**). |
| **Group** | A collection of tanks (for example, one RAS line) used to organize and compare tanks. |
| **Cage / Tank** | A single culture unit (a tank in RAS, a cage in marine). |
| **Stocking** | The record of fish placed into a tank — species, count, and average weight. |
| **Density** | Stocking density in **kg/m³**, computed from biomass and tank volume; used by the safety/feeding policy. |
| **Biomass** | Total live weight of fish in a tank (fish count × average weight). |
| **Operating mode** | **Auto (AI-operated)** vs **Manual (operator-controlled)** feeding control. |
| **BSF policy** | Feeding stance for a group: **Aggressive / Standard / Conservative**. |
| **GET₉₅ / GET₅₀** | Gut Evacuation Time targets — used to pace the next feeding cycle. |
| **Daily cycle cap** | Maximum number of feeding cycles allowed per day. |
| **Safety gate** | A rule-based check that can block a feeding cycle on unsafe water conditions (DO, temperature, missing/stale sensor). Owned by the rules engine, not the AI. |
| **Arbiter** | The system component that arbitrates control decisions; owns real-time action, not the AI. |
| **Inference** | On-device AI analysis of camera frames (count, behavior, size, feeding response). |
| **On-Site AI Training** | Teaching the AI on your own cameras by drawing boxes around fish, then training a model. |
| **Controller (ESP32)** | The on-site microcontroller that drives equipment; can be USB auto-registered. |
| **Actuator** | A controllable piece of equipment (feeder, pump, heater, UV, etc.). |
| **WTG** | Water Treatment Group — a shared water-treatment unit serving a site. |
| **DO** | Dissolved Oxygen (mg/L) — a safety-critical water-quality metric. |
| **FCR** | Feed Conversion Ratio — feed used per unit of growth. |
| **Edge / GX10** | The on-site appliance running bluei-edge; serves the dashboard at `http://<edge-ip>:8080/`. |
| **app.bluei.kr** | The cloud service the edge synchronizes field events to. |

---

**Navigation:** [← Troubleshooting](07-troubleshooting.md) · [📖 Contents](../index.md)
