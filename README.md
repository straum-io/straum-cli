# straum-cli

The command-line tool for Straum is being rebuilt from scratch, together
with Straum itself. This repository holds no code right now, only the
build and release setup the new code will use:

- `.github/`: CI (`ci.yml`), the release pipeline (`release.yml`), the PR
  body check (`pr-contract.yml`), the Issue and PR templates.
- `go.mod`, `Makefile`, `.goreleaser.yaml`: the module, the build and
  check targets, the release archives for six platforms.

While there is no Go code, CI skips its steps.

The previous code is kept under the tag
[`archive/main-2026-10-23`](https://github.com/straum-io/straum-cli/tree/archive/main-2026-10-23).

Apache License 2.0.
