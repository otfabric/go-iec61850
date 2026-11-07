# sclgen

`sclgen` regenerates the internal Go type bindings from the official IEC 61850
XSD schemas bundled in `scl/specs/`. It is a **developer tool** — ordinary
users of the library do not need it. The generated files are committed to the
repository under `scl/internal/raw/`.

## Installation

See [Install sclparse and sclgen](../../../README.md#install-sclparse-and-sclgen)
in the repository README. Contributors build it with `make build` in a checkout.
`sclgen` is a nested module.

## Subcommands

### generate

Parse the XSD bundles and write Go source files.

```bash
sclgen generate \
  --spec-root ./scl/specs \
  --out ./scl/internal/raw
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--spec-root` | Root directory containing XSD bundles (required) |
| `--out` | Output root for generated packages (required) |
| `--versions` | Comma-separated edition list or `all` (default: `all`) |
| `-v`, `--verbose` | Verbose output |

### check

Verify that the generated files match what `generate` would produce. Used in
CI to catch stale generated code.

```bash
sclgen check \
  --spec-root ./scl/specs \
  --out ./scl/internal/raw
```

Logs `CHECK FAILED` with the first difference for every edition whose
generated files are out of date and exits with code 1 (findings).

### version

Print the version, release tag, commit, build date, Go version and platform
on one line; `--json` prints the same as one line of JSON.

```console
$ sclgen version
sclgen 1.9.0 (tag v1.9.0, commit 1802d0f9afe8642d1e278120b76fcdfac967cb9e, built 2026-11-07T15:41:49Z, go1.26.3, linux/amd64)
$ sclgen version --json
{"name":"sclgen","version":"1.9.0","tag":"v1.9.0","commit":"1802d0f9afe8642d1e278120b76fcdfac967cb9e","built":"2026-11-07T15:41:49Z","go":"go1.26.3","platform":"linux/amd64"}
```

The JSON keys are always present, in this order. The `tag …,` part (and a
non-empty `tag`) appears only for a release build. A build from source
without the release ldflags reports `dev` (or the module version of a
`go install …@vX.Y.Z`), the commit from the Go build information with
`-dirty` for a modified tree, and `built unknown`.

`sclgen completion bash|zsh|fish|powershell` generates the shell completion
script; installing it is described in the
[repository README](../../../README.md#shell-completion).

## Exit codes

See [docs/exitcodes.md](docs/exitcodes.md).
