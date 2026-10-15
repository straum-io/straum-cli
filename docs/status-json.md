# `status -o json`: the output schema

`straum-cli status -o json` writes one JSON document to stdout. Any
diagnostics go to stderr, so stdout parses as JSON even when some nodes
are unknown.

## Compatibility

`schema_version` is 1.

These changes keep the version:
- adding a field;
- adding a metric to `metrics`, after the core declares it stable.

These changes bump the version:
- removing or renaming a field;
- changing a field's type or meaning;
- removing a metric.

A consumer should ignore fields it doesn't know.

Each name in `metrics` is a series the core declares stable, in
`docs/configuration.md` of the straum repository, "Stable metrics" (D-141
A). The CLI never derives values. Each value is the node's own number, as
the node rendered it.

## Document

```json
{
  "schema_version": 1,
  "generated_at": "2026-10-16T12:00:00Z",
  "nodes": [
    {
      "address": "127.0.0.1:9100",
      "state": "observed",
      "observed_at": "2026-10-16T11:59:59.985Z",
      "age_ms": 15,
      "latency_ms": 3,
      "metrics": {
        "straum_applied": 10,
        "straum_commit": 10,
        "straum_lag": 0,
        "straum_leading": 1,
        "straum_log_durable": 10,
        "straum_log_end": 10,
        "straum_node": 0,
        "straum_outbox_capacity": 341,
        "straum_outbox_entries": 0,
        "straum_serving": 1,
        "straum_slot": 1
      }
    },
    {
      "address": "127.0.0.1:9101",
      "state": "unknown",
      "latency_ms": 2000,
      "error": "no answer within 2s"
    }
  ]
}
```

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | integer | This document's version. |
| `generated_at` | RFC 3339 UTC | When the CLI finished asking, by the CLI's clock. |
| `nodes` | array | One entry per configured node, in the order given on the command line or in `STRAUM_CLI_NODES`. |
| `nodes[].address` | string | The `host:port` the values came from, as configured. |
| `nodes[].state` | `"observed"` \| `"unknown"` | `observed`: the node answered HTTP 200 with every stable metric as a non-negative integer. `unknown`: anything else. Neither is a health verdict. |
| `nodes[].observed_at` | RFC 3339 UTC | Only when observed. When the response was read, by the CLI's clock. The core does not timestamp its metrics. |
| `nodes[].age_ms` | integer | Only when observed. `generated_at − observed_at`: how old the values were when the document was written. |
| `nodes[].latency_ms` | integer | How long the request took, whether it succeeded or not. |
| `nodes[].error` | string | Only when unknown. Why, for a person to read. The text is not a stable interface. |
| `nodes[].metrics` | object | Only when observed. Stable metric name → unsigned 64-bit integer. Keys are sorted. |

`straum_node` is the id the node reports for itself. The CLI doesn't
check it against anything: a wrong address shows up as an unexpected id,
and the CLI reports it rather than correcting it.

The exit code summarises the document: 0 when every node is observed, 2
when some are unknown, 3 when all are unknown.
