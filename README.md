# straum-cli

[![CI](https://github.com/straum-io/straum-cli/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/straum-io/straum-cli/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
![Status: experimental](https://img.shields.io/badge/status-experimental-orange)

A terminal client for [Straum](https://github.com/straum-io/straum): publish,
subscribe and see what each node of the cluster reports, from one binary.

**Docs:** [straum-io.github.io/ecosystem/straum-cli](https://straum-io.github.io/ecosystem/straum-cli/)

Straum is experimental and not production-ready, and so is this CLI. It
uses only what the core exposes today: MQTT and the read-only metrics
endpoint.

## Why

Any MQTT client can publish to Straum, but none of them tells you what a
PUBACK from Straum means, or what state each node is in. straum-cli knows
the core's contract: `pub` succeeds only on a PUBACK, which Straum sends
once a majority of the cluster has committed the message, and `status`
reads the metrics the core declares stable, never guessing at the rest.

## What it does

- **`status`** asks every node's metrics endpoint once and shows commit,
  applied and log positions, lag and outbox per node. A node that does not
  answer is `unknown`; the CLI never calls a cluster healthy on its own.
- **`pub` and `sub`** over MQTT 5: QoS 0 and 1, retained messages, `+` and
  `#` filters, no-local, retain-as-published and retain-handling.
- **Scriptable:** `-o json` with a versioned schema, and distinct exit codes,
  including one for "sent, outcome unknown".
- **A Go package**, `github.com/straum-io/straum-cli/metrics`, for programs
  that want the same status read.
- Builds for Linux, macOS and Windows on amd64 and arm64.

## Quick start

```sh
go install github.com/straum-io/straum-cli@latest   # Go 1.26 or newer
make build    # from a checkout: bin/straum-cli; `make cross` builds all six targets
```

With a Straum cluster running (see its
[quick start](https://github.com/straum-io/straum#quick-start)):

```console
$ straum-cli sub --broker 127.0.0.1:1884 -t 'sensors/+' -v &
$ straum-cli pub --broker 127.0.0.1:1883 -t sensors/a -m 21.5
sensors/a 21.5

$ straum-cli status --node 127.0.0.1:9100,127.0.0.1:9101,127.0.0.1:9102
ADDRESS         STATE     NODE  SLOT  LEADING  SERVING  COMMIT  APPLIED  ...
127.0.0.1:9100  observed  0     1     1        1        20      20       ...
```

`STRAUM_CLI_BROKER` and `STRAUM_CLI_NODES` save you the flags.

## Compared with others

General MQTT clients such as `mosquitto_pub`, `mosquitto_sub` or MQTTX speak
the same protocol and suit plain publish and subscribe. Use straum-cli when
you need the cluster view, or an exit code that tells "the cluster
committed it" apart from "sent, but no PUBACK came back".

## More

- [docs/usage.md](docs/usage.md): every command's behavior, the Go package,
  exit codes, security, development and releases.
- [docs/commands](docs/commands/straum-cli.md): the generated flag
  reference.
- [docs/coverage.md](docs/coverage.md): what the core can do and which
  command covers it.
- [CHANGELOG.md](CHANGELOG.md) and
  [Issues](https://github.com/straum-io/straum-cli/issues).

Apache License 2.0.
