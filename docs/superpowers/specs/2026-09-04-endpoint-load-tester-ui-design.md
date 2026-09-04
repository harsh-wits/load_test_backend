# Endpoint Load Tester + UI — Design

**Date:** 2026-09-04
**Status:** Approved (design), pending implementation plan

## Context / Problem

The existing tool runs a **chained, callback-driven** ONDC buyer journey: one `rps` drives `select → (wait on_select) → init → (wait on_init) → confirm`, where each stage's payload is built from the *previous stage's real callback*. Two consequences make it unfit for the goal here:

1. The three stages cannot have **independent** QPS — every successful `select` yields exactly one `init`, then one `confirm`.
2. `init`/`confirm` cannot fire unless the async callbacks return to the tool. We verified the seller (`preprod.tipplr.in`) only delivers callbacks to the **registered** `bap_uri` and NACKs any other, so with the shared GCR identity the tool never receives them.

The user needs to **independently load-test each ONDC endpoint** at a target QPS and verify response latency stays under an SLO, while sellers supply their internal processing latency from their own logs separately.

### Load targets (SLO: **p95 < 350ms**)
| Stage | Target QPS |
|---|---|
| `select` / `on_select` | 300 |
| `init` / `on_init` | 100 |
| `confirm` / `on_confirm` | 20 |

## Goals
- A **new, isolated load-test mode** that blasts a single ONDC endpoint at a configured QPS for a configured duration, from a user-supplied payload template, and measures **synchronous** response latency + outcome.
- A **web UI** (served by the existing Go/Fiber server) to configure and launch per-stage load runs and watch live results with a **PASS/FAIL** verdict against **p95 < 350ms**.
- Reuse existing building blocks: concurrent dispatcher + pacing throttle, ONDC signing (`ondcauth`), latency percentiles.

## Non-Goals
- **Not** receiving or measuring async callbacks (`on_select`/`on_init`/`on_confirm`). Seller-side processing latency comes from the seller's logs, provided separately. The tool measures the **synchronous HTTP response** (ACK/NACK) latency only.
- **Not** the chained journey — this mode fires each endpoint independently.
- No auth/login on the UI (internal, locally-run tool).
- No changes to the existing session/preorder pipeline.

## Decisions (from brainstorming)
1. **Load model:** independent endpoint blasting (new mode).
2. **Payload source:** one user-supplied JSON template per stage, entered in the UI.
3. **SLO metric:** **p95 < 350ms** defines pass/fail (full distribution still displayed).
4. **UI stack:** single self-contained static HTML + vanilla JS + CSS, served by Fiber, embedded in the binary.
5. **Execution:** each stage is an independent run; UI can launch one or all three (as separate concurrent runs).
6. **Session coupling:** standalone — bpp details entered in the form; no session lifecycle.

## Architecture

New, isolated packages; no entanglement with the chained coordinator.

- **`internal/domain/loadtest/`** (new)
  - `Runner` — given a `Config`, clones the template to `qps × duration_sec` requests, refreshes IDs, signs, and fires at the target rate; records per-request latency + outcome; exposes live + final metrics.
  - Reuses `internal/domain/pipeline.DispatchConcurrentThrottled` + the pacing throttle for rate control. **Does not** use the per-session rate limiter (300 QPS deliberately exceeds the 150 per-session cap).
  - Reuses `internal/domain/latency` percentile math.
  - `Store` — tracks runs by `run_id`; in-memory default, Redis optional (reuse `shared/redis`). Holds live counters + latency samples (bounded by planned count).
- **`internal/handlers/loadtest/`** (new) — Fiber controller with routes below; wired in `internal/config/di/container.go` `RegisterRoutes`.
- **Static UI** — `internal/handlers/loadtest/ui/index.html` embedded via `//go:embed`, served at `GET /ui`. (Chosen over `SendFile` so it ships in the distroless image without extra COPY.)
- **Signing** — reuse `internal/shared/ondcauth.CreateAuthorisationHeader` with existing `BAP_PRIVATE_KEY` / `BAP_ID` / `BAP_UNIQUE_KEY_ID` env. To avoid the `keyId` vs `context.bap_id` mismatch we hit, when `sign=true` the runner **overrides** `context.bap_id` and `context.bap_uri` from config before signing.

## API

All under the root app (like callbacks), JSON envelopes consistent with `internal/shared/apierror`.

### `GET /loadtest/config`
UI bootstrap. Returns signing status + defaults so the form can prefill and warn.
```json
{ "signing_enabled": true, "bap_id": "pre-prod.gcr.ondc.org", "bap_uri": "https://pre-prod.gcr.ondc.org",
  "max_qps": 500, "max_duration_sec": 120, "request_timeout_ms": 5000,
  "slo": { "metric": "p95", "threshold_ms": 350 },
  "defaults": { "select": 300, "init": 100, "confirm": 20, "duration_sec": 30 } }
```

### `POST /loadtest`  — start a run
```json
{ "action": "select|init|confirm", "bpp_uri": "https://preprod.tipplr.in/ondc/v2",
  "qps": 300, "duration_sec": 30, "sign": true, "template": { /* ONDC envelope */ } }
```
→ `202` `{ "run_id": "...", "action": "select", "target_qps": 300, "planned_requests": 9000, "started_at": "..." }`

Validation (→ `400` via apierror): unknown `action`; `bpp_uri` empty; `qps` ≤ 0 or > `LOADTEST_MAX_QPS`; `duration_sec` ≤ 0 or > `LOADTEST_MAX_DURATION_SEC`; `template` not valid JSON with a `context` object; `sign=true` but `BAP_PRIVATE_KEY` empty.

### `GET /loadtest/:id` — poll live metrics
```json
{ "run_id": "...", "action": "select", "status": "running|completed|stopped|error",
  "target_qps": 300, "duration_sec": 30, "planned_requests": 9000,
  "elapsed_sec": 12.4,
  "metrics": { "sent": 3720, "ack": 3700, "nack": 15, "error": 5, "inflight": 8, "achieved_qps": 299.8,
    "latency_ms": { "avg":..,"p50":..,"p90":..,"p95":..,"p99":..,"min":..,"max":.. } },
  "slo": { "metric": "p95", "threshold_ms": 350, "pass": true } }
```

### `POST /loadtest/:id/stop` — abort; finalizes partial metrics.

## Load engine details
- `planned = qps × duration_sec`. Pacing throttle emits at `qps`; concurrency capped by `MAX_IN_FLIGHT` (existing) — at 300 QPS × ~0.35s ≈ ~105 concurrent, within the 256 default.
- **ID refresh per request:** set `context.transaction_id` = new UUID, `context.message_id` = new UUID, `context.timestamp` = now (RFC3339 ms), `ttl` default `PT30S` if absent. When `sign=true`, also set `context.bap_id`/`bap_uri` from config.
- **Sign:** BLAKE-512 digest + Ed25519 over the exact request bytes (`ondcauth`), `Authorization` header set.
- **Send:** `POST {trim(bpp_uri)}/{action}` with a per-request timeout `LOADTEST_REQUEST_TIMEOUT_MS` (default 5000).
- **Outcome classification:** `ack` = HTTP 2xx and `message.ack.status=="ACK"`; `nack` = 2xx but not ACK; `error` = non-2xx / transport / timeout.
- **Latency:** wall-clock send→response; percentiles over all completed HTTP responses (ack+nack); errors/timeouts counted but excluded from latency.
- **SLO:** `pass = p95 < threshold_ms` **and** at least one measured response. UI additionally warns when `nack`/`error` rate is high (e.g. > 5%) since that usually means bad identity/payload, not seller latency.

## UI (`GET /ui`)
Single page, three **stage cards** (Select / Init / Confirm):
- Fields: enable toggle, `bpp_uri`, **QPS** (prefilled 300 / 100 / 20), **duration_sec** (default 30), **payload template** (textarea, JSON), **sign** toggle, **Run** button.
- Header: signing-key status + `bap_id` (from `GET /loadtest/config`); SLO shown as **p95 < 350ms**; a **Run all** button (fires enabled stages as separate `POST /loadtest` calls).
- Results per card (polls `GET /loadtest/:id` ~1s while running): sent · ack · nack · error · achieved QPS · avg/p50/p90/p95/p99/max · **PASS/FAIL** badge (green if `p95 < 350`, red otherwise); high-NACK warning banner when applicable.
- Vanilla JS `fetch`; no framework, no build step.

## Config / env (new)
- `LOADTEST_MAX_QPS` (default 500) — per-run safety cap.
- `LOADTEST_MAX_DURATION_SEC` (default 120).
- `LOADTEST_REQUEST_TIMEOUT_MS` (default 5000).
- Signing reuses existing `BAP_PRIVATE_KEY` / `BAP_ID` / `BAP_UNIQUE_KEY_ID` / `BAP_URI`.
- Parsed in `internal/config/config.go` with defaults; documented in `.env.example`.

## Edge cases / safety
- Invalid template JSON, out-of-range qps/duration, missing bpp_uri, `sign` without a key → `400` apierror.
- QPS above cap is rejected (not silently clamped) to prevent accidental over-load of a seller.
- Stop cancels the run context; in-flight requests drain; metrics finalized as `stopped`.
- Unregistered `bap_uri` / invalid payload manifests as a high NACK count — surfaced in the UI, not hidden as a latency pass.
- Runner isolates panics per request so one bad response can't kill the run.

## Testing
- `loadtest` unit tests: ID refresh; ACK/NACK/error classification; pacing (achieved QPS ≈ target within tolerance against an in-process stub sink); SLO pass/fail computation.
- Reuse `internal/domain/latency` percentile tests.
- Handler tests: start/validate/poll/stop against a stub HTTP sink (follow `internal/handlers/testing/routes_test.go` patterns).
- Manual end-to-end: run select @ low QPS against `preprod.tipplr.in` with the faithful template, confirm ACKs + latency populate and PASS/FAIL renders.

## Out of scope (future)
- Async callback latency measurement (requires a BAP whose registered subscriber_url the user controls).
- Histogram/sparkline charts, CSV/xlsx export of load-run results, saved run history in Mongo.
- Combined mixed-load single run and sequential mode (only independent per-stage runs for now).
