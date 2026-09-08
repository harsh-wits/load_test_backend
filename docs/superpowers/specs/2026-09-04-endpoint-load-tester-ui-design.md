# Endpoint Load Tester + Dashboard — Design

**Date:** 2026-09-04 (updated 2026-09-08)
**Status:** Approved (design), pending implementation plan

## Context / Problem

The existing tool runs a **chained, callback-driven** ONDC buyer journey: one `rps` drives `select → (wait on_select) → init → (wait on_init) → confirm`, each stage built from the *previous stage's real callback*. That's unfit here because (a) the three stages can't have **independent** volumes/QPS, and (b) `init`/`confirm` can't fire without receiving async callbacks — which only return to the **registered** `bap_uri` (the seller NACKs any other), so with the shared GCR identity they never arrive.

The user needs a **local load generator**: fire a defined number of requests at any ONDC endpoint, at a controlled rate, with **randomized** payloads generated from a single `on_search` catalog. **Latency is reconciled from the sellers' own logs** (matched by `transaction_id`), so the tool's job is to *deliver controlled, varied load* and *emit a request ledger* — not to be the authoritative latency measurer.

### Load targets (informational SLO: p95 < 350ms, client-side cross-check only)
| Stage | Target |
|---|---|
| `select` / `on_select` | 300 QPS |
| `init` / `on_init` | 100 QPS |
| `confirm` / `on_confirm` | 20 QPS |

## Goals
- A **new, isolated `loadtest` mode** that blasts a single ONDC endpoint at a configured **count** or **QPS × duration**, from payloads **generated out of one `on_search` catalog**, with configurable randomization.
- A **reconciliation ledger** export (`transaction_id` + timestamps + ACK/NACK) so sellers can join their log-measured processing time to each request.
- A **simple local dashboard** (static page served by Fiber) to configure, launch, watch, and export runs.
- Reuse existing building blocks: concurrent dispatcher + pacing throttle, ONDC signing, on_search→select generation, latency percentiles.

## Non-Goals
- **Not** the authoritative latency measurement — sellers reconcile from their logs. The tool records **client-observed** latency only, as a sanity cross-check.
- **Not** receiving/measuring async callbacks (`on_select`/`on_init`/…).
- **Not** the chained journey; **no** changes to the existing session/preorder pipeline.
- No UI auth, no charts/graphs, no multi-user — single local operator.

## Decisions (from brainstorming)
1. **Load model:** independent per-endpoint blasting (new mode).
2. **Payload source:** one `on_search` catalog → tool **generates** select/init/confirm (init/confirm quotes **synthesized from catalog item prices**; buyer fields enriched). (`search` optionally supported via the existing search-payload generator.)
3. **Latency:** seller-authoritative (from their logs); tool shows client-side latency + an informational **p95 < 350ms** indicator.
4. **UI stack:** single self-contained static HTML + vanilla JS, served by Fiber, embedded; deliberately **simple** (local use).
5. **Execution:** each endpoint is an independent run; launch one or all.
6. **Session coupling:** standalone — seller details entered in the form; no session lifecycle.
7. **Randomization:** configurable knobs for items, quantity, provider, location, delivery zone (extends today's item-only randomization).

## Architecture

New, isolated packages; no entanglement with the chained coordinator.

- **`internal/domain/loadtest/`** (new)
  - `Generator` — given an `on_search` catalog + `RandomizeConfig` + target `action`, produces a payload. Builds on the existing `buildDistinctOrdersFromOnSearch` logic for select; for **init/confirm** it takes a generated order, **synthesizes a `quote` from catalog `item.price.value × quantity`**, and reuses buyer-side enrichment (billing/fulfillment/payment) analogous to `enrichInit`/`enrichConfirm` in `init_confirm_from_callbacks.go` — but sourced from the catalog, not a callback.
  - `Runner` — clones/streams `count` (or `qps×duration`) generated payloads (fresh `transaction_id`/`message_id`/`timestamp` each), signs each (`ondcauth`), fires at target rate via `pipeline.DispatchConcurrentThrottled` + pacing throttle (its **own** rate control, **not** the 150 per-session cap). Records per-request ledger rows + latency samples.
  - `Store` — runs keyed by `run_id`; in-memory default, Redis optional. Holds live counters, latency samples, and the **ledger** (bounded by count).
- **`internal/handlers/loadtest/`** (new) — Fiber controller; routes below; wired in `container.go`.
- **Static UI** — `internal/handlers/loadtest/ui/index.html` embedded via `//go:embed`, served at `GET /ui`.
- **Signing** — reuse `ondcauth` + existing `BAP_*` env; when `sign=true`, override `context.bap_id`/`bap_uri` from config before signing (avoids the keyId↔bap_id mismatch we hit).

## Randomization (`RandomizeConfig`)
Extends today's item-only behavior (`select_batch.go:449-520`). All optional; sensible defaults preserve current behavior.

| Knob | Options | Default | Today |
|---|---|---|---|
| `item_count` | `{min,max}` items per order | `{1,5}` | random 1–5 ✓ |
| `quantity` | `{fixed:n}` or `{min,max}` | `{fixed:2}` | hardcoded 2 |
| `provider` | `first` \| `random` across `bpp/providers[]` | `first` | first only |
| `location` | `first` \| `random` across `provider.locations[]` | `first` | first only |
| `delivery` | list of `{gps,area_code}` to cycle/randomize | one (from input) | fixed from template |
| `seed` | int for reproducible runs | time-seeded | time-seeded (non-reproducible) |

## API

Root-mounted, JSON envelopes consistent with `internal/shared/apierror`.

### `GET /loadtest/config`
Bootstrap: `{ signing_enabled, bap_id, bap_uri, max_qps, max_duration_sec, max_requests, request_timeout_ms, slo:{metric:"p95",threshold_ms:350}, defaults:{select:300,init:100,confirm:20,duration_sec:30} }`

### `POST /loadtest` — start a run
```json
{ "action": "select|init|confirm|search",
  "bpp_id": "preprod.tipplr.in", "bpp_uri": "https://preprod.tipplr.in/ondc/v2",
  "count": 9000,                       // OR qps+duration_sec
  "qps": 300, "duration_sec": 30,
  "sign": true,
  "on_search": { /* catalog; required for select/init/confirm */ },
  "randomize": { "item_count":{"min":1,"max":5}, "quantity":{"fixed":2},
                 "provider":"random", "location":"random",
                 "delivery":[{"gps":"12.91..,77.65..","area_code":"560103"}], "seed":123 } }
```
→ `202 { run_id, action, target_qps|count, planned_requests, started_at }`
Validation → `400` apierror: unknown `action`; missing `bpp_uri`; `on_search` missing/unparseable for select/init/confirm; `qps`/`count`/`duration` out of range vs caps; `sign=true` with empty `BAP_PRIVATE_KEY`.

### `GET /loadtest/:id` — live metrics
```json
{ run_id, action, status:"running|completed|stopped|error", planned_requests, elapsed_sec,
  metrics:{ sent, ack, nack, error, inflight, achieved_qps,
            client_latency_ms:{avg,p50,p90,p95,p99,min,max} },
  slo:{ metric:"p95", threshold_ms:350, pass:true, note:"client-side cross-check" },
  warnings:[ "high NACK rate (…) — check bap_uri/payload" ] }
```

### `GET /loadtest/:id/ledger?format=csv|json` — **reconciliation export**
One row per request: `transaction_id, message_id, action, sent_at_utc_ms, http_status, ack_status(ACK|NACK|none), client_latency_ms, error`. This is the artifact the seller joins against their logs by `transaction_id`.

### `POST /loadtest/:id/stop` — abort; finalize partial metrics + ledger.

## Load engine details
- `planned = count` or `qps × duration_sec`. Pacing throttle emits at target rate; concurrency capped by `MAX_IN_FLIGHT`.
- Per request: generate payload from catalog+randomize → refresh ids/timestamp → (override bap_id/uri if signing) → sign → `POST {trim(bpp_uri)}/{action}` with timeout `LOADTEST_REQUEST_TIMEOUT_MS` (default 5000) → classify + record ledger row.
- **Classification:** `ack` = 2xx & `message.ack.status=="ACK"`; `nack` = 2xx but not ACK; `error` = non-2xx/transport/timeout.
- Latency percentiles over completed HTTP responses (ack+nack); errors excluded from latency, counted separately.

## UI (`GET /ui`) — simple, local
- **Header:** `bpp_id`/`bpp_uri` inputs; signing status (from `/loadtest/config`); one **`on_search` upload/paste** box (shared by all stages).
- **Per-endpoint rows** (Select / Init / Confirm [+ Search]): enable; **count** or **QPS + duration**; compact **randomize** toggles (provider/location random, quantity, delivery list); **Run** button. Plus **Run all**.
- **Live results** per row (poll `GET /loadtest/:id` ~1s): sent · ack · nack · error · achieved QPS; small client-latency readout + green/red **p95<350** dot (informational).
- **Download ledger** (CSV/JSON) button per run.
- Vanilla JS `fetch`; minimal CSS; no framework/build.

## Config / env (new)
- `LOADTEST_MAX_QPS` (default 500), `LOADTEST_MAX_DURATION_SEC` (120), `LOADTEST_MAX_REQUESTS` (default 100000), `LOADTEST_REQUEST_TIMEOUT_MS` (5000).
- Signing reuses `BAP_PRIVATE_KEY`/`BAP_ID`/`BAP_UNIQUE_KEY_ID`/`BAP_URI`. Parsed in `internal/config/config.go`; documented in `.env.example`.

## Edge cases / safety
- Invalid/absent `on_search` for select/init/confirm, out-of-range volume, missing `bpp_uri`, `sign` without key → `400`.
- Volume above caps is **rejected** (not clamped) to avoid accidentally over-loading a seller.
- Synthesized init/confirm quotes: seller usually validates quotes **async** → request ACKs (intake latency is real); if it validates synchronously it NACKs — surfaced via NACK count + warning, never hidden.
- Unregistered `bap_uri` ⇒ wall of NACKs ⇒ warning banner.
- Stop cancels run context; in-flight drains; metrics + ledger finalized as `stopped`.
- Per-request panic isolation so one bad response can't kill the run.

## Testing
- `loadtest` unit tests: generation from `on_search` (select + synthesized init/confirm quote from catalog prices); each randomization knob (provider/location/quantity/item_count/delivery, seeded reproducibility); id refresh; ACK/NACK/error classification; pacing (achieved ≈ target); ledger row correctness.
- Reuse `internal/domain/latency` percentile tests.
- Handler tests: start/validate/poll/stop/ledger against a stub HTTP sink (follow `internal/handlers/testing/routes_test.go`).
- Manual e2e: select @ low QPS vs `preprod.tipplr.in` with a real `on_search`; confirm ACKs, ledger populates, CSV downloads.

## Out of scope (future)
- Async callback latency (needs a BAP whose registered subscriber_url the user controls).
- Charts/history-in-Mongo, combined mixed-load single run, sequential mode.
- Endpoints beyond search/select/init/confirm (status/track/cancel/update).
