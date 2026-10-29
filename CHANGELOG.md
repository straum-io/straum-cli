# Changelog

Every notable change to straum-cli. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [Semantic Versioning](https://semver.org/). Before 1.0, a minor
version may change commands, flags, exit codes and the JSON output.

## [Unreleased]

### Added

- A new CLI for Straum protocol version 0 on straum-go-sdk v0.1.0; the
  previous one is under the tag `archive/main-2026-10-23`.
- `ping`, `topic create|list|describe`, `produce`, `consume`,
  `broker stats` (also `stats`), `config show|path|profiles|use|set`,
  `positions list|reset|set`, `version`.
- Table and JSON-lines output (`docs/output.md`), exit codes 0 to 7 and
  130 (README.md).
- `produce` stopped by an input error reports how many messages were
  acknowledged before it (stderr; a `summary` line with `-o json`).
- A `key = value` configuration file with profiles; saved positions for
  named consumers.
