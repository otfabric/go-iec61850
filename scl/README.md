# scl — IEC 61850 SCL package and CLI tools

This document covers the `scl` Go package and the two command-line tools
built on top of it: **`sclparse`** and **`sclgen`**.

---

## Table of contents

- [Package overview](#package-overview)
- [Supported schema editions](#supported-schema-editions)
- [sclparse — inspect, validate, summarize SCL files](#sclparse)
  - [Installation](#installation)
  - [Global usage](#global-usage)
  - [Subcommands](#subcommands)
    - [summary](#summary)
    - [validate](#validate)
    - [detect](#detect)
    - [inspect](#inspect)
    - [dump-json](#dump-json)
    - [list-ieds](#list-ieds)
    - [list-lns](#list-lns)
    - [list-datasets](#list-datasets)
    - [list-reports](#list-reports)
    - [list-goose](#list-goose)
    - [list-smv](#list-smv)
    - [list-connected-ap](#list-connected-ap)
    - [list-types](#list-types)
  - [Exit codes](#exit-codes)
  - [JSON output](#json-output)
- [sclgen — regenerate Go types from XSD (developer tool)](#sclgen)
  - [Installation](#installation-1)
  - [Subcommands](#subcommands-1)
- [scl Go package API](#scl-go-package-api)
  - [Parsing](#parsing)
  - [Validation](#validation)
  - [Inspection and export](#inspection-and-export)
  - [Code generation](#code-generation)
  - [Lookup helpers](#lookup-helpers)

---

## Package overview

The `scl` package provides:

- A complete Go type tree for all IEC 61850 SCL elements (SCD, ICD, CID, IID)
- An XML parser that auto-detects the schema edition (v1.7 through 2007C5)
- Semantic validation (type resolution, dataset references, topology linkage,
  GOOSE/SMV `cbName` cross-checks)
- Flatten/export to tabular rows and CSV
- XML generation (structure-preserving; not byte-for-byte, GSE/SMV control
  blocks are not serialized)
- Fast lookup helpers: `FindIED`, `FindLDevice`, `FindLN`, `FindLNodeType`, etc.
- A cross-reference index for topology and type resolution

---

## Supported schema editions

| Label | Standard | Notes |
|-------|----------|-------|
| v1.7 | IEC 61850-6:2003 | Early edition |
| 2007B | IEC 61850-6:2009 | Widely deployed |
| 2007B4 | IEC 61850-6:2018 | Current production standard |
| 2007C5 | IEC 61850-6:2025 | Latest edition |

The parser detects the edition automatically from the XML namespace. All four
editions share the same Go type tree; edition-specific elements are
normalised during parsing.

---

## sclparse

`sclparse` is a command-line tool for inspecting, validating, and summarizing
IEC 61850 SCL files. It is the fastest way to query a substation
configuration file without writing code.

### Installation

Download a binary from the [releases](https://github.com/otfabric/go-iec61850/releases), or build from a checkout. `sclparse` is a nested module:

```bash
cd scl/cmd/sclparse
go build -o sclparse .
```

### Global usage

```
sclparse <subcommand> [flags] <file>
```

Most subcommands accept a `--json` flag to emit machine-readable JSON instead
of the default human-readable text output.

### Subcommands

#### summary

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

#### validate

Run semantic validation and print diagnostics. Exits with code 3 if any
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
| `--strict` | Treat warnings as errors (exit code 3) |
| `--max-errors N` | Stop after N errors (0 = unlimited) |
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

#### detect

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

#### inspect

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

#### dump-json

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

#### list-ieds

List all IEDs with their access-point and logical-device counts.

```bash
sclparse list-ieds station.scd
sclparse list-ieds --json station.scd
```

---

#### list-lns

List all logical nodes across all IEDs and LDs.

```bash
sclparse list-lns station.scd
sclparse list-lns --json station.scd
```

---

#### list-datasets

List all datasets with member counts.

```bash
sclparse list-datasets station.scd
sclparse list-datasets --json station.scd
```

---

#### list-reports

List all report control blocks (URCB and BRCB) with their dataset, trigger
options, and optional fields configuration.

```bash
sclparse list-reports station.scd
sclparse list-reports --json station.scd
```

---

#### list-goose

List all GOOSE (GSE) control blocks.

```bash
sclparse list-goose station.scd
sclparse list-goose --json station.scd
```

---

#### list-smv

List all Sampled Values (SMV) control blocks.

```bash
sclparse list-smv station.scd
sclparse list-smv --json station.scd
```

---

#### list-connected-ap

List all connected access points with their IED and address details.

```bash
sclparse list-connected-ap station.scd
sclparse list-connected-ap --json station.scd
```

---

#### list-types

List all data type templates: `LNodeType`, `DOType`, `DAType`, and `EnumType`.

```bash
sclparse list-types station.scd
sclparse list-types --json station.scd
```

---

#### version

Print the build version.

```bash
sclparse version
```

---

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Usage error (bad arguments) |
| 2 | File parse failure |
| 3 | Validation errors found (validate subcommand only) |

---

### JSON output

All subcommands that accept `--json` emit a self-describing JSON object.
The schema is stable within a major version. Use `dump-json` for the most
complete machine-readable representation of the full model.

---

## sclgen

`sclgen` regenerates the internal Go type bindings from the official IEC 61850
XSD schemas bundled in `scl/specs/`. It is a **developer tool** — ordinary
users of the library do not need it. The generated files are committed to the
repository under `scl/internal/raw/`.

### Installation

Download a binary from the [releases](https://github.com/otfabric/go-iec61850/releases), or run `make build` in a checkout. `sclgen` is a nested module.

### Subcommands

#### generate

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
| `-v / --verbose` | Verbose output |

#### check

Verify that the generated files match what `generate` would produce. Used in
CI to catch stale generated code.

```bash
sclgen check \
  --spec-root ./scl/specs \
  --out ./scl/internal/raw
```

Exits with a non-zero code and prints a diff if any file is out of date.

---

## scl Go package API

### Parsing

```go
import "github.com/otfabric/go-iec61850/scl"

// Parse from file (auto-detects schema edition).
s, err := scl.ParseFile("station.scd")

// Parse from an io.Reader.
s, err := scl.Parse(r)

// Parse with options; the Result carries version info, diagnostics and kind.
res, err := scl.ParseFileOpts("station.scd", scl.ParseOptions{})

// Detect the schema edition without a full parse.
info, err := scl.DetectFile("station.scd") // or scl.DetectVersion(data)

// Infer the document kind (SCD/ICD/CID/...) from a parsed model.
kind := scl.DetectKind(s)
```

### Validation

```go
import (
    "github.com/otfabric/go-iec61850/scl/index"
    "github.com/otfabric/go-iec61850/scl/validate"
)

// Pass-based validation with a shared cross-reference index.
idx, idxDiags := index.Build(s)
for _, d := range validate.All(s, idx, idxDiags) {
    fmt.Println(d.Severity, d.Code, d.Path, d.Message)
}
```

`scl.Validate(s)` is still available but deprecated in favour of
`validate.All`.

### Inspection and export

```go
// Print SCL as a human-readable text tree.
err := scl.PrintTree(os.Stdout, s)

// Flatten all data attributes to tabular rows.
rows := scl.Flatten(s)

// Export the flattened rows to CSV.
err = scl.WriteCSV(os.Stdout, rows)

// Dataset / report / GOOSE / SMV / connected-AP summaries (row slices).
dsRows := scl.ExportDataSets(s)
rptRows := scl.ExportReports(s)
gseRows := scl.ExportGSEControls(s)
smvRows := scl.ExportSMVControls(s)
apRows := scl.ExportConnectedAPs(s)
```

### Code generation

```go
// Write the SCL model back to XML. Structure is preserved, but formatting,
// namespace prefixes and ordering may differ, and GSE/SMV control blocks
// are not serialized (see KNOWN_LIMITATIONS.md).
if err := scl.Generate(outFile, s); err != nil {
    log.Fatal(err)
}
```

### Lookup helpers

```go
ied := s.FindIED("IED1")
ld  := ied.FindLDevice("LD1")
ln  := ld.FindLN("", "GGIO", "1")
lnType := s.FindLNodeType("LGGIO")

// Cross-reference index for fast bulk lookups.
import "github.com/otfabric/go-iec61850/scl/index"
idx, diags := index.Build(s)
lnType = idx.FindLNodeType("LGGIO")
doType := idx.FindDOType("SPC_Type")
```
