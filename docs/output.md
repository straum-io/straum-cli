# JSON output

With `-o json` (or `output = json`), each command writes JSON lines to
stdout: one object per line. Errors stay on stderr as text; the exit code
says what happened (README.md). Before 1.0, fields may be added but are
not renamed or removed; a change that breaks this is noted in
CHANGELOG.md. Examples of every object are in
`testdata/output.golden.json`.

## Bytes

Keys, header names and values, and bodies are bytes. In JSON a byte field
is a string when the bytes are valid UTF-8, else `{"base64": "<standard
base64>"}`; `consume --bytes base64` writes every byte field in the second
form. A key is `null` when the message has none; a present, empty key is
`""`. `produce --input json` reads the same forms.

## Objects

| Command | Object |
|---|---|
| `ping` | first `{"type": "hello", "broker", "version", "cluster_id", "node_id", "max_frame_bytes", "max_message_bytes"}`, then per answered ping `{"type": "pong", "seq", "rtt_ms"}` |
| `topic create` | `{"topic", "created"}`; `created` is false when `--if-not-exists` found the topic |
| `topic list` | per topic `{"name", "partitions"}` |
| `topic describe` | `{"name", "replication_factor", "retention_ms", "partitions": [{"partition", "start_seq", "next_seq", "message_count", "index_bytes"}]}`; `retention_ms` 0 keeps everything |
| `broker stats`, `stats` | `{"node_id", "open_connections", "pending_bytes", "pending_bytes_peak", "max_pending_bytes", "fetch_waiters", "produce_requests", "groups_committed", "fsyncs", "messages_accepted", "messages_deduplicated", "body_bytes_received", "body_bytes_new", "wal_bytes", "segment_bytes", "index_bytes"}`, the broker's counters since it started |
| `produce` | per acknowledged Produce `{"topic", "partition", "base_seq", "count", "timestamp_ms"}`: the messages have seqs `base_seq` to `base_seq + count - 1` |
| `consume` | per message `{"topic", "partition", "seq", "timestamp_ms", "key", "headers": [{"name", "value"}], "body"}` |
| `positions list` | per saved position `{"cluster_id", "topic", "partition", "name", "next_seq", "saved_at"}`; `saved_at` is RFC 3339 UTC |
| `config show` | per setting `{"key", "value", "source"}`; source is `flag`, `env`, `file` or `default`; the first line is the profile |
| `config profiles` | per profile `{"profile", "current"}` |

Timestamps named `timestamp_ms` are milliseconds since the Unix epoch,
assigned by the broker.
