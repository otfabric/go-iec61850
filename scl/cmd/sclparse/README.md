# sclparse

`sclparse` is a command-line tool for inspecting, validating, and summarizing
IEC 61850 SCL files, built on the [`scl` package](../../README.md) of
go-iec61850. It is the fastest way to query a substation
configuration file without writing code.

## Installation

See [Install sclparse and sclgen](../../../README.md#install-sclparse-and-sclgen)
in the repository README. Contributors build it with `make build` in a checkout.
`sclparse` is a nested module.

## Global usage

```text
sclparse <subcommand> [flags] <file>
```

Most subcommands accept a `--json` flag to emit machine-readable JSON instead
of the default human-readable text output.

The SCL schema requires the IEC 61850-6 namespace on the root element, so a
file whose `<SCL>` declares no namespace is refused (exit code 3). The global
flag `--assume-namespace` reads such a file as if it declared the namespace,
with a `namespace-assumed` warning; without version attributes it is read as
schema 1.7. It applies to every subcommand that parses the file (not
`detect`, which reports what the file declares).

```bash
sclparse validate --assume-namespace demo.icd
```

## Subcommands

### summary

Print a compact overview of element counts.

```bash
sclparse summary station.scd
sclparse summary --json station.scd
sclparse summary --counts-only station.scd
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--json` | Emit JSON |
| `--pretty` | Pretty-print JSON |
| `--counts-only` | Show only counts, no names |

---

### validate

Run semantic validation and print diagnostics. Exits with code 1 (findings) if any
validation errors are found.

```bash
sclparse validate station.scd
sclparse validate --strict station.scd        # treat warnings as errors
sclparse validate --max-errors 10 station.scd # stop after 10 errors
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--json` | Emit diagnostics as JSON |
| `--strict` | Treat warnings as errors (exit code 1) |
| `--max-errors` N | Stop after N errors (0 = unlimited) |
| `--warnings-as-errors` | Promote warnings to errors |
| `--no-warnings` | Suppress warning output |

Validation checks include:

- LNodeType, DOType, DAType, EnumType resolution
- Dataset member reference validity
- Report control block dataset linkage
- GOOSE and SMV `cbName` cross-references
- Topology `LNode` resolution (`Substation`, `VoltageLevel`, `Bay`)
- Connected access point IED linkage

---

### detect

Identify the schema edition and document kind.

```bash
sclparse detect station.scd
sclparse detect --json station.scd
sclparse detect --quiet station.scd  # only print the schema version
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--json` | Emit JSON |
| `--quiet` | Print only the schema version string |

---

### inspect

Show schema version, vendor extension namespaces, and `Private` element
usage.

```bash
sclparse inspect station.scd
sclparse inspect --json station.scd
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--json` | Emit JSON |

---

### dump-json

Emit the full normalized SCL model as JSON. Useful for scripting or feeding
into external tools.

```bash
sclparse dump-json station.scd
sclparse dump-json --pretty station.scd
sclparse dump-json --output model.json station.scd
sclparse dump-json --include-metadata station.scd
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--pretty` | Pretty-print with indentation |
| `-o / --output` | Write to a file instead of stdout |
| `--include-metadata` | Wrap output in a metadata envelope (version, kind) |

---

### list-ieds

List all IEDs with their access-point and logical-device counts.

```bash
sclparse list-ieds station.scd
sclparse list-ieds --json station.scd
```

---

### list-lns

List all logical nodes across all IEDs and LDs.

```bash
sclparse list-lns station.scd
sclparse list-lns --json station.scd
```

---

### list-datasets

List all datasets with member counts.

```bash
sclparse list-datasets station.scd
sclparse list-datasets --json station.scd
```

---

### list-reports

List all report control blocks (URCB and BRCB) with their dataset, trigger
options, and optional fields configuration.

```bash
sclparse list-reports station.scd
sclparse list-reports --json station.scd
```

---

### list-goose

List all GOOSE (GSE) control blocks.

```bash
sclparse list-goose station.scd
sclparse list-goose --json station.scd
```

---

### list-smv

List all Sampled Values (SMV) control blocks.

```bash
sclparse list-smv station.scd
sclparse list-smv --json station.scd
```

---

### list-connected-ap

List all connected access points with their IED and address details.

```bash
sclparse list-connected-ap station.scd
sclparse list-connected-ap --json station.scd
```

---

### list-types

List all data type templates: `LNodeType`, `DOType`, `DAType`, and `EnumType`.

```bash
sclparse list-types station.scd
sclparse list-types --json station.scd
```

---

### version

Print the version, release tag, commit, build date, Go version and platform
on one line; `--json` prints the same as one line of JSON.

```console
$ sclparse version
sclparse 1.9.0 (tag v1.9.0, commit 1802d0f9afe8642d1e278120b76fcdfac967cb9e, built 2026-11-07T15:41:49Z, go1.26.3, linux/amd64)
$ sclparse version --json
{"name":"sclparse","version":"1.9.0","tag":"v1.9.0","commit":"1802d0f9afe8642d1e278120b76fcdfac967cb9e","built":"2026-11-07T15:41:49Z","libraries":[],"go":"go1.26.3","platform":"linux/amd64"}
```

The JSON keys are always present, in this order. The `tag …,` part (and a
non-empty `tag`) appears only for a release build. A build from source
without the release ldflags reports `dev` (a `go install …@vX.Y.Z` reports that version; a
pseudo-version or a `+dirty` version is `dev` too), the commit from the Go build information with
`-dirty` for a modified tree, and `built unknown`.
Other OT Fabric `go-*` libraries the binary is built on (not go-iec61850
itself) appear after `built …` as `go-x 1.2.3, ` and in the JSON
`libraries` array (`[]` when there are none, as today).

`sclparse completion bash|zsh|fish|powershell` generates the shell completion
script; installing it is described in the
[repository README](../../../README.md#shell-completion).

---

## Exit codes

See [docs/exitcodes.md](docs/exitcodes.md).

---

## JSON output

All subcommands that accept `--json` emit a self-describing JSON object.
The schema is stable within a major version. Use `dump-json` for the most
complete machine-readable representation of the full model.
