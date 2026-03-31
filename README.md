## Seller App Load Tester

Multi-tenant, session-based ONDC seller app load tester acting as a BAP simulator to verify seller latency claims across the full buyer journey.

### Architecture

- **Session-based isolation**: each BPP team creates a session with their BPP details. Sessions are independent -- one team's load test doesn't interfere with another's.
- **Redis for active state**: sessions, run metrics, txnID routing, and rate limit counters live in Redis for sub-millisecond access.
- **MongoDB for persistence**: completed runs, session history, and catalog snapshots are persisted to MongoDB.
- **Two-tier rate limiting**: a Redis Lua token-bucket enforces both a global RPS ceiling and a per-session cap.
- **Concurrent dispatch**: each pipeline stage fans out payloads via goroutines, capped by a semaphore (`MAX_IN_FLIGHT`) and the rate limiter.
- **Callback-driven payload generation**: `init` is built from the actual `on_select` response, `confirm` from `on_init`.
- **ONDC auth headers**: outbound calls are signed with BLAKE2b-512 + Ed25519; inbound callbacks can be verified.
- **Structured errors**: all API errors follow a consistent envelope (`{success, error: {code, message}, timestamp}`).

### Running locally

#### Minimal app only (external Redis/Mongo)

```bash
cp .env.example .env
# edit .env as needed (point REDIS_URL / MONGO_URI to your infra)
docker compose up --build
```

The app will be available on `http://localhost:8080`.

#### Full local stack (app + Redis + Mongo)

```bash
cp .env.example .env
# uses defaults: REDIS_URL=seller-load-tester-redis:6379, MONGO_URI=mongodb://seller-load-tester-mongo:27017
docker compose -f docker-compose.local.yml up --build
```

This starts Redis and Mongo alongside the app for a fully self-contained local environment.

### Workflow

1. **Create a session** -- `POST /sessions` with your BPP details.
2. **Run discovery** -- `POST /sessions/:id/discovery` sends `/search` and waits for `on_search`.
3. **Start preorder load test** -- `POST /sessions/:id/preorder` with `rps` and `duration_sec`.
4. **Poll run progress** -- `GET /sessions/:id/runs/:run_id` returns real-time per-action metrics.
5. **Stop a run** -- `POST /sessions/:id/runs/:run_id/stop` to abort mid-flight.
6. **Clean up** -- `DELETE /sessions/:id` when done.

### API endpoints

#### Sessions

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/sessions?bpp_id=` | List past sessions for a BPP |
| POST | `/sessions` | Create a session (`bpp_id`, `bpp_uri`) |
| GET | `/sessions/:id` | Get session state, catalog status, preorder status |
| DELETE | `/sessions/:id` | Soft-delete session and clean up state |
| PUT | `/sessions/:id/error_injection` | Enable/disable schema-error injection for preorder |

#### Pipelines

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/sessions/:id/discovery/payload` | Generate default search payload template |
| POST | `/sessions/:id/discovery` | Run synchronous discovery: sends `/search`, waits for `on_search` |
| PUT | `/sessions/:id/catalog` | Upload raw on_search payload as catalog |
| POST | `/sessions/:id/preorder` | Start preorder load test: select -> init -> confirm |

#### Runs & Reports

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/sessions/:id/runs` | List all runs for a session |
| GET | `/sessions/:id/runs/:run_id` | Poll run progress and per-action metrics |
| POST | `/sessions/:id/runs/:run_id/stop` | Force stop a running test |
| GET | `/sessions/:id/report` | Download aggregated CSV report across all runs |

#### Callbacks

| Method | Endpoint |
|--------|----------|
| POST | `/on_search` |
| POST | `/on_select` |
| POST | `/on_init` |
| POST | `/on_confirm` |

### Preorder pipeline flow

1. **Select**: batch of select payloads generated from the session's catalog, dispatched concurrently.
2. **Wait for `on_select`**: callbacks recorded per transaction, metrics incremented.
3. **Init**: for each successful `on_select`, an `init` payload is built from the response.
4. **Wait for `on_init`**: callbacks recorded.
5. **Confirm**: for each successful `on_init`, a `confirm` payload is built from the response.
6. **Wait for `on_confirm`**: callbacks recorded.
7. **Metrics**: run metrics are available in real time via `GET /sessions/:id/runs/:run_id`.

### Latency metrics

See [`docs/latency_metrics.md`](docs/latency_metrics.md) for how latency is computed, where it is exposed in the API, and how to query raw/summarized latency data in MongoDB.

### Configuration

| Key | Description |
|-----|-------------|
| `BAP_ID` | BAP subscriber ID |
| `BAP_URI` | BAP callback URL |
| `CORE_VERSION` | Default ONDC core version (can be overridden per session via `core_version` in `POST /sessions`) |
| `BAP_PRIVATE_KEY` | Base64 Ed25519 private key for signing (empty = no signing) |
| `BAP_PUBLIC_KEY` | Base64 Ed25519 public key for verification |
| `BAP_UNIQUE_KEY_ID` | Key ID for the `Signature keyId` field |
| `MONGO_URI` | MongoDB connection string |
| `MONGO_DB` | MongoDB database name |
| `REDIS_URL` | Redis connection URL |
| `REDIS_DB` | Redis database number |
| `GLOBAL_RPS_LIMIT` | Global rate limit across all sessions (default 2000) |
| `PER_SESSION_RPS_LIMIT` | Per-session rate limit (default 150) |
| `SESSION_TTL_SECONDS` | Session expiry TTL (default 3600) |
| `DEFAULT_RPS` | Default RPS if not specified in request |
| `DEFAULT_DURATION` | Default duration if not specified |
| `MAX_IN_FLIGHT` | Max concurrent HTTP requests per stage |
| `PIPELINE_STAGE_GAP_SECONDS` | Delay between stages |
| `RUN_STORE_BACKEND` | `memory` (default) or `redis` for run payloads |

### Error responses

All errors follow a structured format:

```json
{
  "success": false,
  "error": {
    "code": "SESSION_4001",
    "message": "Session not found"
  },
  "timestamp": "2026-03-12T12:00:00Z"
}
```

Error codes are namespaced: `SESSION_*` for session errors, `PIPELINE_*` for pipeline/upstream errors, `REQUEST_*` for request validation, `HTTP_*` for standard HTTP errors.

### ONDC authorization headers

When `BAP_PRIVATE_KEY` is set, every outbound call includes an `Authorization` header with a BLAKE2b-512 digest signed with Ed25519.

Inbound callback `Authorization` verification is controlled per-session using `verification_enabled`.

### Preorder error injection

Schema-error injection is controlled per session using `error_injection_enabled` via `PUT /sessions/:id/error_injection` with body:

```json
{
  "enabled": true
}
```

Rules:
- Enabled by default (disable explicitly if needed).
- If `rps <= 1`, injection is disabled.
- If `2 <= rps <= 9`, exactly 1 payload per stage-second is corrupted.
- If `rps >= 10`, `floor(rps * 0.10)` payloads per stage-second are corrupted.
- Corrupted payloads always use fixed 2-field mutations:
  - `select`: wrong `item.id` and wrong `provider.id`
  - `init`: wrong `item.id` and wrong `fulfillment.id`
  - `confirm`: wrong `fulfillment.id` and invalid random `order.state`

To toggle this for a specific session via `curl` (assuming backend on `localhost:8080`):

```bash
SESSION_ID=<your-session-id>

curl -X PUT "http://localhost:8080/sessions/${SESSION_ID}/error_injection" \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'

# To disable again:
curl -X PUT "http://localhost:8080/sessions/${SESSION_ID}/error_injection" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

### Mock BPP

A FastAPI mock BPP is provided at `.idea/scripts/mock_bpp.py` for local load testing. Set the session's `bpp_uri` to point at it.

```bash
cd .idea/scripts
pip install -r requirements.txt
python mock_bpp.py
```

### Swagger

When `SWAGGER_ENABLE=true`, Swagger UI is at `GET /swagger` and the spec at `GET /swagger/openapi.yaml`.