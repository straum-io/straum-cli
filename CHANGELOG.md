# Changelog

Every notable change to straum-cli. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [Semantic Versioning](https://semver.org/). Before 1.0, a minor
version may change commands, flags and exit codes. The `status -o json`
schema has its own version (docs/status-json.md).

## [Unreleased]

### Added

- `pub` and `sub` connect with a user name and password (`--username`,
  `--password-file` or `STRAUM_CLI_PASSWORD`) and over TLS (`--tls`,
  `--tls-ca`, `--tls-server-name`), as the core accepts them (D-158 A,
  D-164 A). A password is never sent in clear text beyond loopback.
  Refusals name their reason ("bad user name or password", "not
  authorized").

- `status`: each configured node's stable metrics (core D-141 A).
  - Each value carries its source address and the age of the
    observation.
  - A node that doesn't answer is `unknown`; no health verdict is drawn.
  - Output is a table, or JSON with schema version 1.
  - Exit codes 0/1/2/3.
- `version`.
- `pub`: publish one message over MQTT 5 (core D-141, amended).
  - QoS 0 or 1, and retained messages; an empty payload clears the
    retained message.
  - Exit 4 when the node refused or nothing was sent.
  - Exit 5 when the message was sent but the outcome is unknown.
- `sub`: subscribe over MQTT 5.
  - `+` and `#` filters.
  - The MQTT 5 options no-local, retain-as-published and retain-handling.
  - Stops with `--count` or `--wait`.
  - Output is plain text or JSON lines.
- `docs/coverage.md`: the broker coverage matrix, checked against the
  core in CI.
- `docs/commands/`: the command reference, generated from the command
  tree and checked in CI.
- Release builds with GoReleaser for linux, darwin and windows, each on
  amd64 and arm64.
- The Go package `github.com/straum-io/straum-cli/metrics`: what
  `status` reads, for other Go programs. It returns typed results
  (`Report`, `Node`, `State`) and typed errors (`StatusError`,
  `ContentError`). `status` now uses it; its output is unchanged.
- The exported API of `metrics` is listed in
  `testdata/metrics-api.golden`, and `make check` fails when the API
  differs from it; `make golden` rewrites it after an intended change.
- `pub` and `sub` report the reason when the node disconnects them (for
  example 0x8e, session taken over) and exit 4.

### Changed

- `metrics` parses each node's response as it arrives instead of reading
  it whole first, and skips comment lines without allocating: a 5-node
  `Collect` makes about half the allocations (straum-cli#17). The API and
  the results are unchanged.
- Releases are cut only from tags on `main`, with GoReleaser pinned to the
  version `make release-snapshot` uses, and carry a signed build
  provenance for every archive and `checksums.txt`.

- The coverage and drift checks read the core's declared client contract:
  the stable-settings, stable-kinds, reserved-kinds, stable-reasons and
  stable-metrics blocks of its `docs/configuration.md`, each pinned by a
  test in the core. They no longer scan the core's Rust source.
