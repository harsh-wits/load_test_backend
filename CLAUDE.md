# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A multi-tenant, session-based **ONDC seller-app load tester**. It acts as a BAP (buyer app) simulator that drives the full ONDC buyer journey (`search → select → init → confirm`) against a seller's BPP endpoints to measure their latency and success-rate claims. Written in Go with the Fiber web framework.

## Commands

```bash
# Build
go build ./cmd/server              # main HTTP server -> ./server binary
go build ./cmd/export_run_metrics  # Excel report exporter

# Test
go test ./...                                        # all tests
go test ./internal/domain/session/                   # one package
go test -run TestIsValidDomain ./internal/domain/session/   # single test

# Run locally (app only; point REDIS_URL / MONGO_URI at external infra)
cp .env.example .env
docker compose up --build            # -> http://localhost:8080

# Run full self-contained stack (app + Redis + Mongo with auth)
docker compose -f docker-compose.local.yml up --build

# Export a run's metrics to a stakeholder .xlsx
go run ./cmd/export_run_metrics -input run_export_input.json -output stakeholder_run_report.xlsx
```

Config is entirely env-driven (`.env` loaded via godotenv). See `.env.example` for the full set; `internal/config/config.go` is the single source of truth for defaults and parsing.

## Architecture

The layering is `handlers → domain → ports → shared`, wired together in one DI container.

- **`internal/config/di/container.go`** — the composition root. `BuildContainer()` constructs Redis, Mongo, the run-payload store, session manager, and rate limiter; `RegisterRoutes()` builds the pipeline coordinator and both HTTP controllers and mounts them. Start here to understand how everything connects.
- **`internal/handlers/testing/routes.go`** — the primary API surface (sessions, discovery, catalog, preorder load tests, runs, reports). This is the largest and most important handler file (~1400 lines). Routes are grouped under `/sessions`.
- **`internal/handlers/callbacks/`** — inbound ONDC callback endpoints: `POST /on_search`, `/on_select`, `/on_init`, `/on_confirm`. These receive the seller's async responses and record latency.
- **`internal/domain/pipeline/`** — the load-test engine. `BCoordinator` (`coordinator.go`) orchestrates the `select → init → confirm` stages; `dispatcher.go` fans out payloads concurrently (semaphore capped by `MAX_IN_FLIGHT`, gated by the rate limiter `Throttle`).
- **`internal/domain/session/`** — session lifecycle (`manager.go`), the Redis **dual token-bucket rate limiter** (`rate_limiter.go`, implemented as a Lua script enforcing both a global RPS ceiling and a per-session cap in one atomic call), and validation.
- **`internal/ports/`** — outbound adapters: `seller/` (signed calls to the BPP under test), `registry/` (ONDC registry `/lookup` for inbound signature verification), `session/` (Redis + Mongo session stores).
- **`internal/shared/`** — cross-cutting: `redis/`, `mongo/`, `crypto/` + `ondcauth/` (BLAKE2b-512 + Ed25519 request signing/verification), `runlog/` (run-payload store, pluggable memory vs. redis backend), `apierror/` (the structured error envelope + Fiber error handler).

### Key data-flow concepts

- **Session isolation**: each BPP team creates a session carrying their BPP details. Sessions are independent — one team's test cannot interfere with another's. Active state (sessions, run metrics, txnID routing, rate-limit counters) lives in **Redis**; completed runs and history are persisted to **MongoDB**.
- **Callback-driven payload generation**: payloads are built from the *previous stage's actual callback response* — `init` is generated from the real `on_select` body, `confirm` from `on_init`. The pipeline is not replaying static fixtures; it chains real responses. See `internal/domain/pipeline/init_confirm_from_callbacks.go`.
- **txnID routing**: outbound requests carry a transaction ID; inbound callbacks are matched back to their run via a txnID→run link (`TxnLinker`) so async latency can be attributed correctly.
- **Error injection**: preorder runs can inject schema errors into a configurable fraction of payloads (`PUT /sessions/:id/error_injection`) to test the seller's validation behavior; `coordinator_test.go` covers the corruption logic.
- **Run payload persistence** is separate from the run-metrics store: `RUN_PERSISTENCE=FS` writes raw payloads to `RUNS_FS_ROOT`, `DB` writes to a Mongo `run_payloads` collection.

### Typical workflow (API)

1. `POST /sessions` — create a session with BPP details.
2. `POST /sessions/:id/discovery` — synchronous `search`, waits for `on_search`.
3. `PUT /sessions/:id/catalog` (optional) — upload a raw `on_search` as the catalog.
4. `POST /sessions/:id/preorder` — start the `select → init → confirm` load test at a given `rps` / `duration_sec`.
5. `GET /sessions/:id/runs/:run_id` — poll real-time per-action metrics; `POST .../stop` to abort.
6. `GET /sessions/:id/report` — aggregated CSV; or use the Excel exporter for stakeholder reports.

## Conventions

- **API errors** always use the envelope `{success, error: {code, message}, timestamp}`. Add new error cases to `internal/shared/apierror/` (see `session.go`, `pipeline.go`, `generic.go`) rather than returning ad-hoc Fiber errors.
- **`examples/payloads/`** holds the base ONDC payload templates the coordinator loads (e.g. `select/select.json`); **`fixtures/`** holds sample `on_search` data. Payload-shaping code references these paths relative to the working directory, and the Dockerfile copies `docs/`, `examples/`, and `fixtures/` into the image — keep them in sync when changing payload logic.
- **API reference** lives in `docs/openapi.yaml`; latency-metric semantics and the Mongo queries backing them are documented in `docs/latency_metrics.md` and `docs/latency_mongo_queries.md`.
