# bluei-edge — Edge Service Guide (source)

This folder is the **single source of truth** for the operator-facing guide that is
published as the **bluei.kr edge service guide page**. It is written to be edited and
re-published continuously as the dashboard evolves.

> Audience of the guide: **farm-site operators** using the bluei-edge dashboard
> (`http://<edge-ip>:8080/`). Language: **English-first** (Korean translation may be
> added later as a parallel `*.ko.md` set).

---

## Folder layout

```
docs/edge-service-guide/
├── README.md            ← (this file) conventions + how to publish/maintain
├── index.md             ← wiki HOME: title + table of contents linking every page
├── manifest.yaml        ← ordered page list + metadata that drives the web nav
├── SCREENSHOTS.md       ← screenshot registry: every image, who captures it, status
├── pages/               ← one Markdown file per guide page (the actual content)
│   ├── 00-overview.md
│   ├── 01-access-and-login.md
│   ├── 02-initial-setup.md
│   ├── 03-daily-operations.md
│   ├── 04-ai-management.md
│   ├── 05-records-and-trade.md
│   ├── 06-system-operations.md
│   ├── 07-troubleshooting.md
│   └── 08-glossary.md
└── images/              ← screenshots, named by ID (e.g. SS-01-login.png)
```

One file per page keeps edits **local**: changing a feature touches one page, not a
monolith. `manifest.yaml` is the only place that defines page **order and nav titles**,
so reordering/adding a page is a one-line change there.

## Wiki structure (TOC + pages)

The guide is published as a **wiki**: `index.md` is the home page and holds the **table of
contents** that links to every page; each page links back. Navigation comes from two
places that must stay in sync:

- **`index.md`** — the human-readable TOC (what a reader lands on).
- **`manifest.yaml`** — the machine-readable order/titles/slugs (what the web nav is built
  from).

Every page ends with a **navigation footer**:

```markdown
**Navigation:** [← Prev Title](NN-prev.md) · [📖 Contents](../index.md) · [Next Title →](NN-next.md)
```

Links are **relative `.md` paths** so they work on GitHub and in most static-site
generators; the bluei.kr publisher rewrites them to slug URLs. When you add/reorder/rename
a page, update **all three**: `manifest.yaml`, the `index.md` TOC row, and the prev/next
footers of the neighbouring pages.

## Page format

Every page begins with YAML front-matter consumed by the web publisher:

```yaml
---
slug: access-and-login        # URL segment on bluei.kr (stable — avoid changing)
title: Access & Login         # nav + page title
order: 1                      # sort order within the guide
summary: One-line description shown in nav/cards.
applies_to: bluei-edge dashboard 0.1.0   # dashboard version this page reflects
last_updated: 2026-06-23
screenshots: [SS-01, SS-02]   # IDs used on this page (must exist in SCREENSHOTS.md)
---
```

After the front-matter: standard Markdown. Conventions:

- **One H1 per page** matching `title`. Use `##`/`###` for sections (these become the
  in-page table of contents on the web).
- **Numbered callouts** in prose reference numbered overlays on the screenshot, e.g.
  "Click **① + New**". Keep numbers consistent with the image annotation.
- **UI labels** in **bold** exactly as they appear in the English UI (e.g. **Safe Shutdown**,
  **On-Site AI Training**). Data examples in `code` (e.g. `ras_tank_01`).
- **Safety notes** use a blockquote with a leading emoji: `> ⚠️ ...` (warning),
  `> ℹ️ ...` (info), `> ✅ ...` (good practice).

## Screenshots

Images are referenced by **ID**, not by raw path, so an image can be swapped without
touching prose. While an image is not yet captured, leave a **placeholder**:

```markdown
> 📸 **SS-01** · Login screen · _capture pending_ — see [registry](../SCREENSHOTS.md#ss-01)
```

When captured, replace with:

```markdown
![Login screen with phone-approval flow](../images/SS-01-login.png)
```

All images, their capture state, the exact screen + state needed, and **who owns the
capture** live in [`SCREENSHOTS.md`](./SCREENSHOTS.md). Update an image → bump
`last_updated` on the page and the registry row.

## Maintenance workflow (per change)

1. Edit the relevant `pages/NN-*.md` (and `manifest.yaml` if the page set changes).
2. If UI changed, update or flag affected screenshots in `SCREENSHOTS.md`.
3. Bump `last_updated` (and `applies_to` if it tracks a new dashboard version).
4. Commit; the bluei.kr publisher re-renders from `manifest.yaml` + `pages/`.

## Status

Content drafted for **all pages** (`00`–`08`) plus the `index.md` wiki home and full
navigation. The **10 maintainer/structure screenshots are captured** (English UI, raw —
numbered ①②③ callout overlays are still to be added by an editor/publisher). Remaining
work:

- **11 live-data screenshots** (`SS-08`–`SS-17`, `SS-19`) — captured on the GX10 with real
  farm data (see registry for the per-shot screen + state).
- **Callout overlays** on the captured images.
- **Domain review** of glossary terms marked _domain review pending_ (BSF, GET, WTG).
