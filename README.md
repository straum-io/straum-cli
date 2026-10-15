# straum-cli

A terminal client for [Straum](https://github.com/rubtsov-stan/straum), a
replicated MQTT broker. It is for operators, developers and scripts.

The Straum core is **experimental and not production-ready**, and so is
this CLI. It uses only the surfaces the core exposes today. What it
covers, and what waits on the core, is in [docs/coverage.md](docs/coverage.md).

## Install

```sh
go install github.com/rubtsov-stan/straum-cli@latest
# or, from a checkout:
make build     # bin/straum-cli
make cross     # dist/: linux, darwin and windows, each amd64 and arm64
```

Go 1.26 or newer.

## Commands

### `status`

Asks each node's metrics endpoint once and shows the core's stable metrics
from each. The endpoint is whatever the node's
`STRAUM_CLUSTER_METRICS_LISTEN` is set to.

```console
$ straum-cli status --node 127.0.0.1:9100,127.0.0.1:9101,127.0.0.1:9102
ADDRESS         STATE     NODE  SLOT  LEADING  SERVING  COMMIT  APPLIED  LOG_END  LOG_DURABLE  LAG  OUTBOX_ENTRIES  OUTBOX_CAPACITY  AGE
127.0.0.1:9100  observed  0     1     1        1        10      10       10       10           0    0               341              1ms
127.0.0.1:9101  observed  1     1     0        1        10      10       10       10           0    0               341              1ms
127.0.0.1:9102  unknown   -     -     -        -        -       -        -        -            -    -               -                -
straum-cli: 127.0.0.1:9102: dial tcp 127.0.0.1:9102: connect: connection refused
```

- **Values are shown as the node reported them.** Each row names its
  source address and the age of the observation.
- **No health verdict.** A node that doesn't answer, answers with an
  error, or omits a stable metric is `unknown`. It is never assumed
  healthy, and the CLI draws no verdict from the numbers.
- **One request per node.** Requests run concurrently, each bounded by
  `--timeout`. There are no retries: run the command again to ask again.
- **`-o json`** writes a versioned document, described with its
  compatibility rules in [docs/status-json.md](docs/status-json.md).
  Diagnostics go only to stderr.

| Flag | Default | |
|---|---|---|
| `--node host:port` | none | A node's metrics address. Repeat the flag or separate addresses with commas. |
| `--timeout` | `2s` | How long to wait for each node. |
| `-o, --output` | `table` | `table` or `json`. |

### `version`

Prints the CLI's version, commit and Go version.

## Configuration

There is no default cluster and no config file. Nodes come from `--node`.
If `--node` isn't given, they come from `STRAUM_CLI_NODES`
(comma-separated `host:port`). If neither is set, the command fails.
When both are set, the flag wins.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Every node observed |
| 1 | Usage or configuration error |
| 2 | Some nodes unknown |
| 3 | Every node unknown |

## Security

- **No credentials:** the metrics endpoint is plain HTTP without
  credentials (core D-112).
- **Loopback by default:** the core binds it to loopback unless the
  operator sets `STRAUM_CLUSTER_UNAUTHENTICATED_NETWORK=1` (D-107). Run
  the CLI on the nodes' host, or over a network you trust.
- **No proxy, no redirects:** the CLI never uses an HTTP proxy for these
  requests and doesn't follow redirects.
- **Out of scope until the core supports them** (core D-110, D-141):
  authentication, TLS and administrative operations.

## Development

| Target | |
|---|---|
| `make check` | gofmt, `go vet`, unit tests with `-race`, cross builds. Needs nothing else. |
| `make e2e` | Builds `straum-server` from `../straum` (override with `CORE=`) and runs the CLI against a real three-node cluster on loopback. Needs Rust. |
| `make drift-check` | Fails if a metric in `stable-metrics.txt` is not in the core's stable set (`docs/configuration.md`, "Stable metrics"). |
| `make coverage-check` | Fails if `docs/coverage.md` misses a core contract element, or has a row that is not done, verified, blocked or n/a. |
| `make golden` | Rewrites the golden output files after an intended change. |

CI (`.github/workflows/ci.yml`) runs `make check`. It also runs the three
core checks against the core's `main` branch. That job needs the
`STRAUM_CORE_TOKEN` secret: a read-only token for the private core
repository.

When the core changes something listed in `docs/coverage.md`, update the
matrix in the same change.
