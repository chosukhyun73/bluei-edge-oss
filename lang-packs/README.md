# Language packs

This directory holds **language packs** for bluei-edge. The repository is structured so the
**core (code + English base) can be open-sourced**, while additional languages live here as
**separate, gate-able packs**.

## Model: base + packs

- **Base language = `en`**, kept in the core locations (always present, always public):
  - Dashboard UI strings → `web/dashboard/src/locales/en.json`
  - Operator guide → `docs/edge-service-guide/pages/*.md`
- **Each additional language = one pack** under `lang-packs/<code>/`:

```
lang-packs/
└── ko/
    ├── pack.yaml          # metadata: code, name, version, covers
    ├── dashboard.json     # dashboard UI strings for this language
    └── guide/             # operator-guide pages for this language (00–08 + index)
```

`en` is **never** a pack — it is the core base.

## How a pack reaches the running product (build-time assembly)

The dashboard is **bundled** (no runtime download). Before the dashboard builds, a prepare
step assembles the present packs into the build:

- `web/dashboard/scripts/prepare-locales.mjs` copies each
  `lang-packs/<code>/dashboard.json` → `web/dashboard/src/locales/<code>.json`.
- `web/dashboard/src/lib/i18n.ts` then **globs** `../locales/*.json` and auto-registers
  every locale present. So a pack being present/absent = a language being enabled/gated.
- `npm run dev` and `npm run build` (and therefore `make dashboard`) run the prepare step
  automatically. The generated `src/locales/<code>.json` files are git-ignored; only
  `en.json` (the base) is committed.

The operator guide follows the same base+pack split: the **English base** is in
`docs/edge-service-guide/`; the **Korean pages** are this pack's `guide/`. This repo is the
**canonical source** for all languages; the bluei.kr web guide is a published mirror synced
from here.

## Add a new language

1. `mkdir lang-packs/<code>` and add `pack.yaml`, `dashboard.json`, and `guide/`.
2. Translate `dashboard.json` from `web/dashboard/src/locales/en.json` (keep the **same
   keys**; `tr()` falls back to `en` for any missing key).
3. Translate the guide pages from `docs/edge-service-guide/pages/` into `guide/`.
4. Rebuild — the language appears automatically (label comes from `i18n.ts` `KNOWN_LABELS`,
   or the code itself if unknown; add a label there for a nicer name).

> Keys must match the base `en.json`. A missing key safely falls back to English; an extra
> key is simply unused.

## Open-source gating

- The **core builds and runs en-only** with no packs present — safe for a public mirror.
- To open-source: publish the core plus whichever packs you choose. Keep partner/private or
  in-progress languages out of the public tree (e.g. a separate overlay, or
  `.gitattributes export-ignore`).
- **Product builds (e.g. the GX10 for Korean farms) must include the `ko` pack** so Korean
  stays the default — `DEFAULT_LANGUAGE` is `ko` when the pack is present, otherwise `en`.
