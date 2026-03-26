# MongoDB Latency Queries (for provided run_ids)

## Run IDs

Use the run IDs below in the pipelines.

```txt
5d6f439d-50f3-423e-9583-0f2701053416
2eb2722c-5b70-4fbc-9fc2-ec0223e615b6
a61bffc2-40c2-4116-83ab-6d6cb9a4d50f
2aff2d45-ccb7-4b25-877d-2abfc14a51fe
dc57df27-0922-4475-830e-d42638d63e92
900b7c2e-af5c-429c-b4c2-d7face7776c8
bce58da1-726a-4fd4-92e5-8bc954ba64ee
```

## Collection

Run these against `run_latency_summary`.

## Query 1: Average per stage and per run (includes percentiles)

```json
[
  { "$match": { "run_id": { "$in": [
    "5d6f439d-50f3-423e-9583-0f2701053416",
    "2eb2722c-5b70-4fbc-9fc2-ec0223e615b6",
    "a61bffc2-40c2-4116-83ab-6d6cb9a4d50f",
    "2aff2d45-ccb7-4b25-877d-2abfc14a51fe",
    "dc57df27-0922-4475-830e-d42638d63e92",
    "900b7c2e-af5c-429c-b4c2-d7face7776c8",
    "bce58da1-726a-4fd4-92e5-8bc954ba64ee"
  ] } } },
  { "$project": {
    "_id": 0,
    "run_id": 1,
    "stage": 1,
    "sent": 1,
    "success": 1,
    "failure": 1,
    "timeout": 1,
    "avg_ms": 1,
    "p90_ms": 1,
    "p95_ms": 1,
    "p99_ms": 1
  } },
  { "$sort": { "run_id": 1, "stage": 1 } }
]
```

## Query 2: Average per stage across all selected runs (weighted by `success`)

```json
[
  { "$match": {
    "run_id": { "$in": [
      "5d6f439d-50f3-423e-9583-0f2701053416",
      "2eb2722c-5b70-4fbc-9fc2-ec0223e615b6",
      "a61bffc2-40c2-4116-83ab-6d6cb9a4d50f",
      "2aff2d45-ccb7-4b25-877d-2abfc14a51fe",
      "dc57df27-0922-4475-830e-d42638d63e92",
      "900b7c2e-af5c-429c-b4c2-d7face7776c8",
      "bce58da1-726a-4fd4-92e5-8bc954ba64ee"
    ] },
    "stage": { "$in": ["on_select", "on_init", "on_confirm"] }
  } },
  { "$group": {
    "_id": "$stage",
    "sent": { "$sum": "$sent" },
    "success": { "$sum": "$success" },
    "failure": { "$sum": "$failure" },
    "timeout": { "$sum": "$timeout" },
    "weightedAvgNumerator": { "$sum": { "$multiply": ["$avg_ms", "$success"] } }
  } },
  { "$project": {
    "_id": 0,
    "stage": "$_id",
    "sent": 1,
    "success": 1,
    "failure": 1,
    "timeout": 1,
    "avg_ms": {
      "$cond": [
        { "$eq": ["$success", 0] },
        null,
        { "$divide": ["$weightedAvgNumerator", "$success"] }
      ]
    }
  } },
  { "$sort": { "stage": 1 } }
]
```

## Query 3: Average per run across stages (weighted by `success`)

```json
[
  { "$match": {
    "run_id": { "$in": [
      "5d6f439d-50f3-423e-9583-0f2701053416",
      "2eb2722c-5b70-4fbc-9fc2-ec0223e615b6",
      "a61bffc2-40c2-4116-83ab-6d6cb9a4d50f",
      "2aff2d45-ccb7-4b25-877d-2abfc14a51fe",
      "dc57df27-0922-4475-830e-d42638d63e92",
      "900b7c2e-af5c-429c-b4c2-d7face7776c8",
      "bce58da1-726a-4fd4-92e5-8bc954ba64ee"
    ] },
    "stage": { "$in": ["on_select", "on_init", "on_confirm"] }
  } },
  { "$group": {
    "_id": "$run_id",
    "sent": { "$sum": "$sent" },
    "success": { "$sum": "$success" },
    "failure": { "$sum": "$failure" },
    "timeout": { "$sum": "$timeout" },
    "weightedAvgNumerator": { "$sum": { "$multiply": ["$avg_ms", "$success"] } }
  } },
  { "$project": {
    "_id": 0,
    "run_id": "$_id",
    "sent": 1,
    "success": 1,
    "failure": 1,
    "timeout": 1,
    "avg_ms": {
      "$cond": [
        { "$eq": ["$success", 0] },
        null,
        { "$divide": ["$weightedAvgNumerator", "$success"] }
      ]
    }
  } },
  { "$sort": { "run_id": 1 } }
]
```

