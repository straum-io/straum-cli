# Using straum-cli

The full behavior of each command, the Go package, configuration, exit
codes, security, development and releases. The generated flag reference
is in [commands/](commands/straum-cli.md).

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
  compatibility rules in [docs/status-json.md](status-json.md).
  Diagnostics go only to stderr.

| Flag | Default | |
|---|---|---|
| `--node host:port` | none | A node's metrics address. Repeat the flag or separate addresses with commas. |
| `--timeout` | `2s` | How long to wait for each node. |
| `-o, --output` | `table` | `table` or `json`. |

### `pub` and `sub`

Publish and subscribe over MQTT 5. These are ordinary client operations,
allowed by core D-141 as amended. The broker address is the node's
`STRAUM_CLUSTER_MQTT_LISTEN`. It comes from `--broker host:port`, or
else from `STRAUM_CLI_BROKER`; there is no default.

```console
$ straum-cli pub --broker 127.0.0.1:1883 -t sensors/a -m 21.5            # QoS 1 by default
$ straum-cli pub --broker 127.0.0.1:1883 -t sensors/a -m 21.5 -r         # set the retained message
$ straum-cli pub --broker 127.0.0.1:1883 -t sensors/a -m '' -r           # clear it
$ echo hello | straum-cli pub --broker 127.0.0.1:1883 -t greet --stdin
$ straum-cli sub --broker 127.0.0.1:1883 -t 'sensors/+' -t 'logs/#' -v
$ straum-cli sub --broker 127.0.0.1:1883 -t sensors/a -c 1 --wait 5s -o json
```

- **`pub` with QoS 1** (the default) succeeds only on a PUBACK. Straum
  sends the PUBACK once a majority of the cluster has committed the
  message.
- **`pub` with QoS 0** succeeds once the message is written to the
  connection, and nothing confirms it.
- **QoS 2 is refused:** straum doesn't support it (D-138).
- **Payload size:** at most 256 KiB, the core's packet limit.
- **`sub` output:** each message goes to stdout. Plain mode prints the
  payload (`-v` puts the topic first). `-o json` prints one
  `{"topic","qos","retain","payload_base64"}` object per line; fields
  are only ever added.
- **When `sub` stops:** on Ctrl-C, after `--count` messages, or after
  `--wait`. It then unsubscribes and disconnects.
- **MQTT 5 options:** `--no-local`, `--retain-as-published` and
  `--retain-handling 0|1|2`.
- **Sessions are always clean.** Persistent sessions, wills and
  expiry wait on the core's step 7c. Shared subscriptions and
  subscription identifiers are refused by the core.

### `version`

Prints the CLI's version, commit and Go version.

The full reference for every command and flag is generated in
[commands](commands/straum-cli.md).

## Go library

`status` is built on the package
`github.com/straum-io/straum-cli/metrics`, which other Go programs can
import. It reads the stable metrics and nothing else. It makes the same
single request per node, never retries and draws no health verdict.

```go
c := metrics.Client{Timeout: 2 * time.Second} // HTTP nil: no proxy, no redirects, no idle connections
r, err := c.Collect(ctx, []string{"127.0.0.1:9100", "127.0.0.1:9101"})
if err != nil { /* a bad address or a negative timeout; nothing was sent */ }
for _, n := range r.Nodes {
	if n.State == metrics.Unknown {
		// n.Err: a network error, *metrics.StatusError, *metrics.ContentError,
		// context.DeadlineExceeded (Timeout) or context.Canceled (ctx)
		continue
	}
	fmt.Println(n.Address, n.Metrics["straum_commit"])
}
```

- **`metrics.Stable()`** lists the metric names. Each is a `uint64`.
- **MQTT is not wrapped.** `pub` and `sub` use
  [paho.golang](https://github.com/eclipse/paho.golang) directly, and so
  can other programs.
- **Not provided:** an admin API, authentication and TLS, the same as for
  the CLI.
- **Before 1.0,** the package's API may change in a minor version.

## Configuration

There is no default cluster and no config file. Nodes come from `--node`.
If `--node` isn't given, they come from `STRAUM_CLI_NODES`
(comma-separated `host:port`). If neither is set, the command fails.
When both are set, the flag wins.

For `pub` and `sub` against a node that checks credentials
(`STRAUM_AUTH_PASSWORD_FILE`, core D-158 A) and speaks TLS
(`STRAUM_TLS_CERT_PATH`, core D-164 A):

```sh
STRAUM_CLI_PASSWORD=… straum-cli pub --broker node-a:8883 --tls-ca ca.pem \
  --username alice -t sensors/a -m 21.5
```

- **User name:** `--username`, or `STRAUM_CLI_USERNAME`.
- **Password:** `STRAUM_CLI_PASSWORD`, or the first line of
  `--password-file`, which wins when both are set and may not be empty.
  There is no password flag, so it never shows in a process list or the
  shell history. The node's password file takes hashes from
  `straum-server hash-password`, which reads the password on stdin.
- **TLS:** `--tls` checks the node's certificate against the system's CAs;
  `--tls-ca` trusts the CAs in a PEM file instead (and implies `--tls`);
  `--tls-server-name` sets the name to check, by default the `--broker`
  host (and also implies `--tls`). TLS 1.2 at least; the certificate is
  always checked.
- **No password in clear text:** a password to an address beyond loopback
  needs TLS, or the command stops before connecting, as the core lets
  its MQTT listener leave loopback only with TLS.
- A refusal names the reason: "bad user name or password", "not
  authorized" (a topic rule, or an anonymous client the node does not let
  in). Under a topic rule the node drops a refused QoS 0 publish without
  a refusal, so `pub` exits 0; use `-q 1` to learn the outcome.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Done: every node observed, the message published, or `sub` finished |
| 1 | Usage or configuration error |
| 2 | `status`: some nodes unknown. `sub`: `--wait` passed before `--count` messages arrived |
| 3 | `status`: every node unknown |
| 4 | `pub`/`sub`: refused by the node, or the connection failed before anything took effect |
| 5 | `pub`: sent, but the outcome is unknown (no PUBACK in time, or the connection broke). The message may or may not be committed; publishing it again can deliver it twice |

## Security

- **No credentials:** the metrics endpoint is plain HTTP without
  credentials (core D-112).
- **Loopback by default:** the core binds it to loopback unless the
  operator sets `STRAUM_CLUSTER_UNAUTHENTICATED_NETWORK=1` (D-107). Run
  the CLI on the nodes' host, or over a network you trust.
- **No proxy, no redirects:** the CLI never uses an HTTP proxy for these
  requests and doesn't follow redirects.
- **MQTT credentials and TLS** follow the core's (D-158 A, D-164 A); see
  Configuration. The metrics endpoint has neither.
- **Out of scope until the core supports them** (core D-110, D-141):
  client certificates and administrative operations.

## Development

| Target | |
|---|---|
| `make check` | gofmt, `go vet`, unit tests with `-race`, cross builds. Needs nothing else. |
| `make cover` | Statement coverage with `go test -coverprofile`; prints the total. Reported, not gated. |
| `make e2e` | Builds `straum-server` from `../straum` (override with `CORE=`) and runs the CLI against a real three-node cluster on loopback. Needs Rust. |
| `make drift-check` | Fails if a metric in `metrics/stable-metrics.txt` is not in the core's stable set (`docs/configuration.md`, "Stable metrics"). |
| `make coverage-check` | Fails if `docs/coverage.md` misses a core contract element, or has a row that is not done, verified, blocked or n/a. |
| `make golden` | Rewrites the golden output files after an intended change. |
| `make docs` | Regenerates the command reference in `docs/commands/`. `make test` fails while it is stale. |
| `make release-snapshot` | Validates `.goreleaser.yaml` and builds every release archive into `dist/`, without publishing anything. |

CI (`.github/workflows/ci.yml`) runs `make check` and reports statement
coverage (`make cover`). It also runs the three core checks against the
public core's `main` branch (`straum-io/straum`), and `pr-contract`
checks every PR body.

## Releases

- **Building:** pushing a `v*` tag on `main` runs
  `.github/workflows/release.yml`. It refuses a tag that is not on `main`,
  runs `make check`, then GoReleaser (the same version as
  `make release-snapshot`) builds archives for linux, darwin and windows,
  each on amd64 and arm64, plus checksums.
- **Provenance:** every archive and `checksums.txt` gets a signed build
  provenance: `gh attestation verify <archive> -R straum-io/straum-cli`.
- **Publishing:** the build creates a **draft** GitHub release. A
  maintainer reads it and publishes it.
- **Release notes:** they come from `CHANGELOG.md`, which is written by
  hand.
- **Withdrawing:** a bad release is unpublished (its tag stays, so the
  archives keep their provenance), and the fix ships as a new patch
  version with a `CHANGELOG.md` entry that says what was wrong.

When the core changes something listed in `docs/coverage.md`, update the
matrix in the same change.
