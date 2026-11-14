# scl — IEC 61850 SCL package and CLI tools

This document covers the `scl` Go package and the two command-line tools
built on top of it: **`sclparse`** and **`sclgen`**.

---

## Table of contents

- [Package overview](#package-overview)
- [Supported schema editions](#supported-schema-editions)
- [sclparse — inspect, validate, summarize SCL files](#sclparse)
- [sclgen — regenerate Go types from XSD (developer tool)](#sclgen)
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
IEC 61850 SCL files. Its subcommands, flags and exit codes are in
[cmd/sclparse/README.md](cmd/sclparse/README.md).

---

## sclgen

`sclgen` regenerates the internal Go type bindings from the official IEC 61850
XSD schemas bundled in `scl/specs/`. It is a **developer tool**: ordinary
users of the library do not need it. Its usage is in
[cmd/sclgen/README.md](cmd/sclgen/README.md).

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

#### Limits

Every parse has limits; a document beyond one is refused with an error
that wraps `scl.ErrLimitExceeded`, never read in part. Zero in an option
means its default.

| Limit | Default | Option |
|---|---|---|
| Document size (`ParseBytes`, `ParseFileOpts`, `Parse`, `ParseWithOptions`; a file's size is checked before it is read) | `DefaultMaxBytes`, 1 GiB | `ParseOptions.MaxBytes` |
| Element nesting, also of skipped content (unknown elements, `Private`) | `DefaultMaxDepth`, 1024 | `ParseOptions.MaxDepth` |
| Diagnostics returned; parsing and semantic validation stop adding them once the list is full | all | `ParseOptions.MaxDiagnostics` |
| Length of a name quoted in a diagnostic (cut at a character boundary, ends with "…") | `DefaultMaxDiagnosticName`, 256 bytes | `ParseOptions.MaxDiagnosticName` |
| Nodes of a type expansion (DO, SDO, DA, BDA at each place of the model) | `DefaultMaxExpandedNodes`, 33,554,432 | `FlattenOptions.MaxExpandedNodes`; for a server model `ServerModelOptions.MaxExpandedNodes`; for the GOOSE and SV control blocks of a document `goose.SCLOptions` and `sv.SCLOptions` |

`scl.DetectFile` reads a file up to its root element only.

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

What is checked (code, severity):

| Area | Checks |
|---|---|
| Index (`index.Build`) | XSD keys and uniques: duplicate IED, AccessPoint, LDevice, LN, DataSet (error), ReportControl (warning), GSEControl / SampledValueControl per LN, DO per LNodeType, DA/SDO per DOType, BDA per DAType, EnumVal `ord`, SubNetwork, GSE/SMV per ConnectedAP (`duplicate-name`, `duplicate-id`, … error); a DA name repeated with a different FC is a warning |
| Required (`scl.ValidateRequired`) | `missing-attribute` (error) for the required attributes of LN and LN0, DataSet, FCDA `fc`, ReportControl, GSEControl `name`/`appID`, SampledValueControl `name`/`smvID`, templates, communication |
| Templates | unresolved DO/SDO/DA/BDA types, `missing-type` (error) for a Struct or Enum without `type` |
| IEDs | unresolved `lnType`, `lnclass-mismatch` (error) when LN `lnClass` differs from its LNodeType |
| Datasets | unresolved LN, DO/SDO path, DA and BDA path of each FCDA (`unresolved-fcda`, warning); no attribute of the FCDA `fc` (`fcda-fc-mismatch`, warning; SG and SE are treated as equivalent) |
| Controls | dataset references, `invalid-attribute` (error) for SampledValueControl `smpRate` or `nofASDU` 0 |
| Addresses | P patterns (`address-format` warning, `invalid-address` error when unreadable, e.g. VLAN-PRIORITY 8), repeated P type, VLAN-PRIORITY without VLAN-ID, GOOSE APPID 0x0000-0x3FFF / 0x8000-0xBFFF and SV APPID 0x4000-0x7FFF (`appid-range`), multicast bit (`mac-not-multicast`), recommended 01-0C-CD-01/04 ranges (`mac-range`, info), MAC+APPID reused in a SubNetwork (`duplicate-address`) |
| Communication, topology | ConnectedAP, GSE/SMV and LNode references |

`scl.Flatten` keeps rows for branches it cannot expand, with `Status`
`unresolved-lntype`, `unresolved-dotype`, `unresolved-datype` or
`type-cycle`. It stops after `DefaultMaxExpandedNodes` nodes, the last
row with `Status` `expansion-limit`; `scl.FlattenWithOptions` sets
another limit and also returns an `expansion-limit` error diagnostic.

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
