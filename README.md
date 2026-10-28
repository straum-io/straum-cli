# straum-cli

A terminal client for a [Straum](https://github.com/straum-io/straum)
broker: create and inspect topics, publish and read messages, check a
broker. It speaks Straum protocol version 0 through
[straum-go-sdk](https://github.com/straum-io/straum-go-sdk) and is meant
for operators, developers and scripts: results go to stdout as a table or
as JSON lines, diagnostics to stderr, and every outcome has its own exit
code.

Straum is experimental and not production-ready, and so is this CLI. No
release has been published yet.

## Quick start

Build the broker from a Straum checkout and start it on loopback (version 0
listens on loopback only; it has no authentication yet):

```bash
git clone https://github.com/straum-io/straum && cd straum
cargo run --release -p straum-broker -- \
    --data-dir /tmp/straum-data --cluster-id dev --node-id 1 --listen 127.0.0.1:7171
```

In another terminal, install the CLI (Go 1.26.5 or later) and use it. The
default broker address is `127.0.0.1:7171`.

```bash
go install github.com/straum-io/straum-cli@main

straum-cli ping
straum-cli topic create orders
straum-cli produce orders 'first order' 'second order' --key customer-7
printf 'line one\nline two\n' | straum-cli produce orders --stdin
straum-cli consume orders
straum-cli consume orders --follow -o json     # Ctrl-C to stop
```

`consume --name billing` remembers where it stopped, so the next
`consume orders --name billing` reads only new messages.

## Commands

| Command | |
|---|---|
| `ping` | the broker's Hello answer (version, cluster id, node id, limits) and Ping round trips |
| `topic create`, `topic list`, `topic describe` | topics; `list` follows the broker's page cursor |
| `produce` | messages from arguments, stdin or files, with keys and headers, in batches; acknowledgements are durable |
| `consume` | from a seq, `earliest`, `latest` or a named consumer's saved position; `--follow`; `--count`, `--until-seq`, `--duration` |
| `broker stats` (or `stats`) | the broker's counters |
| `config` | `show`, `path`, `profiles`, `use`, `set` |
| `positions` | `list`, `reset`, `set` for named consumers |
| `version` | CLI, commit, Go and SDK versions |

Every flag is in the generated reference,
[docs/commands/straum-cli.md](docs/commands/straum-cli.md); the JSON output
is described in [docs/output.md](docs/output.md).

## Configuration

Settings come from, highest first: flags, environment variables, the
selected profile of the configuration file, built-in defaults.
`straum-cli config show` prints each setting with its source.

The file is `--config`, else `$STRAUM_CLI_CONFIG`, else
`<user config dir>/straum-cli/config` (`straum-cli config path`). A missing
file is not an error. It holds `key = value` lines with `#` comments:

```text
current = local

[profile local]
broker = 127.0.0.1:7171
timeout = 10s

[profile staging]
broker = 10.0.0.5:7171, 10.0.0.6:7171
```

| Key | Flag | Environment | Default |
|---|---|---|---|
| `broker` | `--broker` | `STRAUM_CLI_BROKER` | `127.0.0.1:7171` |
| `timeout` (per request) | `--timeout` | `STRAUM_CLI_TIMEOUT` | `10s` |
| `connect_timeout` | `--connect-timeout` | | `10s` |
| `output` (`table`, `json`) | `-o`, `--output` | `STRAUM_CLI_OUTPUT` | `table` |
| `client_name` | | | `straum-cli` |
| `max_in_flight_bytes` | | | `8388608` |

The profile is `--profile`, else `$STRAUM_CLI_PROFILE`, else the file's
`current`, else `default`. `straum-cli config set <key> <value>` writes to
the selected profile and `straum-cli config use <profile>` sets `current`.
An unknown key is an error naming its line. The keys and flags `tls`,
`tls_ca`, `tls_server_name`, `auth_mechanism`, `username` and
`password_file` are reserved for a broker version that serves TLS and
authentication and are refused until then.

## Saved positions

The broker stores no consumer positions. `consume --name N` keeps N's next
seq per broker cluster id, topic and partition in
`<state dir>/straum-cli/<profile>/positions.json`, where the state
directory is `$XDG_STATE_HOME`, else `~/.local/state` (Linux), else the
user configuration directory (macOS, Windows). A position is saved after
the messages of each fetch are written to stdout, so delivery to stdout is
at least once. One process at a time may use a name. An unnamed `consume`
saves nothing. `straum-cli positions list|reset|set` inspect and change
them; a saved position beyond the partition's end (a broker re-created
with the same cluster id) is refused until `positions reset`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | done |
| 1 | usage, configuration, local state or input error; nothing was sent, except that `produce` prints the acknowledgements of what it sent before a bad input line |
| 2 | stopped by a limit before the requested result: `consume --count` not reached within `--duration`; some `ping --count` pings failed |
| 3 | could not connect or complete Hello, or a request that changes nothing lost its connection or timed out |
| 4 | refused by the broker; the request had no effect (`UnknownTopic`, `TopicExists`, `MessageTooLarge`, `Forbidden`, ...) |
| 5 | `produce` or `topic create` sent, outcome unknown: the connection broke, the timeout passed or Ctrl-C came before the answer, so the messages or the topic may or may not exist |
| 6 | the broker's storage failed (`StorageError`); it refuses writes until restarted |
| 7 | retries exhausted (`Overloaded`, `NotLeader`, `FencedLeaderEpoch`); the request had no effect |
| 130 | interrupted (Ctrl-C) with no request in flight that changes anything; `consume --follow` exits 0 |

`produce` and `topic create` do not resend a request by default: protocol
version 0 has no idempotent produce, so a resent request may be stored
twice. `--retries N` allows N resends.

## Development

```bash
scripts/validate.sh quick                   # gofmt, vet, staticcheck, unit, golden and docs tests
scripts/validate.sh push <straum-checkout>  # + govulncheck, -race, live tests
scripts/validate.sh pr <straum-checkout>    # + six-platform build, goreleaser check
make golden                                 # rewrite testdata/*.golden.* after an intended output change
make docs                                   # rewrite docs/commands after a command or flag change
```

Dependencies: cobra for the commands, straum-go-sdk for the protocol,
`golang.org/x/sys` for file locks on Windows.

The live tests run the CLI against `straum-broker` and
`straum-fake-server` built from the Straum commit in `CONTRACT`, the one
straum-go-sdk's version in `go.mod` pins.

The previous CLI is under the tag
[`archive/main-2026-10-23`](https://github.com/straum-io/straum-cli/tree/archive/main-2026-10-23).

Apache License 2.0.
