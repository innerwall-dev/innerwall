# ADR-0008: UI is a static Vite + React SPA embedded in the control-plane binary

**Status:** Accepted

## Context

A server-rendered frontend (or one with server actions) adds a second runtime, a second deployment artifact, and a private backchannel between UI and data that bypasses the public API. For an infrastructure tool, the UI is a client of the platform, not a tier of it — and single-artifact deployment is a real operability feature.

## Decision

- The UI is a **static single-page application** built with **Vite + React**, compiled to assets and embedded into the control-plane binary via `go:embed`.
- No server-side rendering, no server actions, no separate frontend service or Node runtime in production.
- The SPA communicates exclusively through the public REST façade (ADR-0007).
- The flow map renders with **ReactFlow**, aggregated by label group with drill-down — per-host hairballs are explicitly not the visualization model.

## Consequences

- Deployment remains one binary; UI and API version together atomically.
- The public API stays honest — it is the only path the UI has.
- SEO/SSR benefits are irrelevant here and knowingly forgone.
- Binary size grows by the asset bundle; accepted.
