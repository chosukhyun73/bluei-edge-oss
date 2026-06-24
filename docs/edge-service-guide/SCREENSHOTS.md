# Screenshot registry

Every image used by the guide. Reference images **by ID** in pages; never hardcode a
raw path in prose. Capture is **split**:

- **Owner: Maintainer** — structural shots (empty forms, modals, layout). Can be captured
  from a local dev dashboard with demo data.
- **Owner: Site (GX10)** — live-data shots (real tanks, sensor values, camera/inference,
  device detection). Must be captured on the GX10 with representative farm data.

## Conventions

- **File name**: `<ID>-<short-name>.png`, lowercase, in `images/` (e.g. `SS-01-login.png`).
- **Resolution**: capture at a consistent window width (target ~1440px) and the dark theme.
- **Privacy**: mask or use demo values for real business-registration numbers, license
  numbers, and account identifiers.
- **Callouts**: overlay numbered markers ①②③ matching the numbered steps in the page prose.
- **Language**: capture in **English** UI (toggle top-right). Add Korean captures only if a
  Korean guide is published later (`<ID>-...-ko.png`).
- **Status values**: `todo` · `captured` · `needs-reshoot` (UI changed).

## Registry

| ID | Page | Screen / what to show | How to reach | Required state | Owner | Status |
|----|------|-----------------------|--------------|----------------|-------|--------|
| <a id="ss-01"></a>SS-01 | 01 Access & Login | Login screen + phone-approval number | Open `http://<edge-ip>:8080/` when logged out | Logged out, approval step visible | Maintainer | captured |
| <a id="ss-02"></a>SS-02 | 01 Access & Login | Full header bar | After login | Annotate: system status, language, AI Assistant, Safe Shutdown | Maintainer | captured |
| <a id="ss-03"></a>SS-03 | 02 Initial Setup | Farm / Site selector row + **+ New** | Top of any tab | Annotate Farm vs Site dropdowns and the two **+ New** buttons | Maintainer | captured |
| <a id="ss-04"></a>SS-04 | 02 Initial Setup | Register New Farm modal | Header → Farm **+ New** | Empty form | Maintainer | captured |
| <a id="ss-05"></a>SS-05 | 02 Initial Setup | Register New Site modal | Header → Site **+ New** | Empty form, site-type toggle visible | Maintainer | captured |
| <a id="ss-06"></a>SS-06 | 02 Initial Setup | Register New Group modal | Group panel → **New Group** | Empty form, color picker | Maintainer | captured |
| <a id="ss-07"></a>SS-07 | 02 Initial Setup | Tank create + physical info (volume auto) | Tank settings → add tank | Form-factor + dimensions entered, auto volume shown | Maintainer | captured |
| <a id="ss-08"></a>SS-08 | 02 Initial Setup | Sensor / actuator / camera mapping | Tank settings tab | A few mappings present | Site (GX10) | todo |
| <a id="ss-09"></a>SS-09 | 02 Initial Setup | Controller USB auto-registration | AI Management → **Controllers** | ESP32 connected, auto-detect panel | Site (GX10) | todo |
| <a id="ss-10"></a>SS-10 | 02 Initial Setup | Stocking registration | Stocking·Shipping·Buyers → stocking | New stocking form | Site (GX10) | todo |
| <a id="ss-11"></a>SS-11 | 03 Daily Operations | Production Overview + tank cards | Site Tank Management | After stocking, cards with **Start** | Site (GX10) | todo |
| <a id="ss-12"></a>SS-12 | 03 Daily Operations | Feed cycle start form | Tank card → **Start** | Auto/Manual toggle, daily target, warnings | Site (GX10) | todo |
| <a id="ss-13"></a>SS-13 | 03 Daily Operations | Sensors / real-time trend | Site Tank Management / tank detail | Live sensor values | Site (GX10) | todo |
| <a id="ss-14"></a>SS-14 | 04 AI Management | Operating Policy (BSF · Density · GET) | AI Management → **Operating Policy** | Group selected, mode toggles | Site (GX10) | todo |
| <a id="ss-15"></a>SS-15 | 04 AI Management | Inference Monitor | AI Management → **Inference Monitor** | Camera active, range selector | Site (GX10) | todo |
| <a id="ss-16"></a>SS-16 | 04 AI Management | On-Site AI Training — draw boxes | AI Management → **On-Site AI Training** | Snapshot loaded, a box drawn | Site (GX10) | todo |
| <a id="ss-17"></a>SS-17 | 04 AI Management | Safety Learning — learned rules + dispute | AI Management → **Safety Learning** | At least one learned rule | Site (GX10) | todo |
| <a id="ss-18"></a>SS-18 | 05 Records & Trade | Production & Events registration | Production & Events | Mortality/treatment/transfer form open | Maintainer + Site | captured |
| <a id="ss-19"></a>SS-19 | 05 Records & Trade | Stocking / Shipping / document attach | Stocking·Shipping·Buyers | Records present, attach control | Site (GX10) | todo |
| <a id="ss-20"></a>SS-20 | 06 System Operations | AI Assistant panel | Header → **AI Assistant** | One Q&A turn, starter prompts | Maintainer | captured |
| <a id="ss-21"></a>SS-21 | 06 System Operations | Safe Shutdown dialog | Header → **Safe Shutdown** | Confirm step + done step | Maintainer | captured |

## Maintainer-capturable subset (no live farm data needed)

SS-01, SS-02, SS-03, SS-04, SS-05, SS-06, SS-07, SS-18 (form only), SS-20, SS-21.

## Site-only subset (needs real tanks / sensors / cameras / devices)

SS-08, SS-09, SS-10, SS-11, SS-12, SS-13, SS-14, SS-15, SS-16, SS-17, SS-19.
