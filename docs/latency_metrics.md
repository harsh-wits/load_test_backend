## Latency metrics

This project records latency for the preorder pipeline callback stages:

- `select` → `on_select`
- `init` → `on_init`
- `confirm` → `on_confirm`

Latency metrics are grouped into **three sets** (one per callback action) and exposed in the run response under:

- `run.metrics.on_select`
- `run.metrics.on_init`
- `run.metrics.on_confirm`

### Definitions

For a given stage \(e.g. `on_select`\) and a given `txn_id`:

- **sent_at**: the timestamp when the request (`select`) was sent
- **received_at**: the timestamp when the callback (`on_select`) was received
- **latency_ms**:

\[
latency\_ms = received\_at - sent\_at
\]

Only **successful** callback outcomes are used for latency percentiles:

- **avg_ms**: mean of successful `latency_ms`
- **p90_ms / p95_ms / p99_ms**: percentiles over successful `latency_ms` only

Counters:

- **sent**: total number of requests sent for the paired request stage (per `txn_id`)
- **success**: number of callbacks classified as success for the stage
- **failure**: number of callbacks classified as failure (invalid payload, verification/validation failures, etc.) and other non-timeout missing outcomes
- **timeout**: number of callbacks not received within the timeout threshold

### Timeout behavior

Timeout threshold is the same as the internal waiting threshold used during the run (currently 30 seconds per stage).

- If a callback arrives **after** the timeout threshold, it is treated as **timeout** for reporting and it does **not** overwrite earlier timeout classification.

### API usage

Poll a run and read latency metrics from the callback stages:

- `GET /sessions/{id}/runs/{run_id}`

Example fields to look at in the response:

```json
{
  "metrics": {
    "on_select": { "sent": 100, "success": 90, "failure": 0, "timeout": 10, "avg_ms": 120.5, "p90_ms": 210, "p95_ms": 260, "p99_ms": 400 },
    "on_init":   { "sent":  90, "success": 85, "failure": 0, "timeout":  5, "avg_ms": 140.2, "p90_ms": 240, "p95_ms": 290, "p99_ms": 430 },
    "on_confirm":{ "sent":  85, "success": 80, "failure": 0, "timeout":  5, "avg_ms": 160.9, "p90_ms": 260, "p95_ms": 310, "p99_ms": 470 }
  }
}
```

### MongoDB storage

Latency is persisted to MongoDB in two collections:

#### `run_latency_events`

One document per `(run_id, stage, txn_id)` where `stage` is a callback stage (`on_select`, `on_init`, `on_confirm`).

Common query patterns:

```js
// All raw events for a run + stage (useful for debugging)
db.run_latency_events.find({ run_id: "<RUN_ID>", stage: "on_select" })

// Only successful events (the population used for percentiles)
db.run_latency_events.find({ run_id: "<RUN_ID>", stage: "on_select", outcome: "success" })
```

#### `run_latency_summary`

One document per `(run_id, stage)` with aggregated counts and latency percentiles.

```js
// All 3 summaries for a run
db.run_latency_summary.find({ run_id: "<RUN_ID>" })

// One summary for a specific stage
db.run_latency_summary.findOne({ run_id: "<RUN_ID>", stage: "on_confirm" })
```

