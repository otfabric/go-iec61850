# sclgen

`sclgen` regenerates the internal Go type bindings from the official IEC 61850
XSD schemas bundled in `scl/specs/`. It is a **developer tool** — ordinary
users of the library do not need it. The generated files are committed to the
repository under `scl/internal/raw/`.

## Installation

Download a binary from the [releases](https://github.com/otfabric/go-iec61850/releases), or run `make build` in a checkout. `sclgen` is a nested module.

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

Exits with code 1 and logs `CHECK FAILED` with the first difference for every
edition whose generated files are out of date; see
[docs/exitcodes.md](docs/exitcodes.md).

### version

Print the build version.

```bash
sclgen version
```

`sclgen completion bash|zsh|fish|powershell` generates the shell completion
script.
