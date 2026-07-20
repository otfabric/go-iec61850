# go-iec61850

[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Go Reference](https://pkg.go.dev/badge/github.com/otfabric/go-iec61850.svg)](https://pkg.go.dev/github.com/otfabric/go-iec61850)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/otfabric/go-iec61850/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/go-iec61850/actions/workflows/ci.yml)
[![Codecov](https://codecov.io/gh/otfabric/go-iec61850/graph/badge.svg)](https://codecov.io/gh/otfabric/go-iec61850)
[![Release](https://img.shields.io/github/v/release/otfabric/go-iec61850?label=release)](https://github.com/otfabric/go-iec61850/releases)

A pure-Go IEC 61850 MMS client and server library built on top of
[go-mms](https://github.com/otfabric/go-mms).

Logical devices, logical nodes, data objects, data attributes, functional
constraints, reports, datasets, controls, quality, timestamps, and SCL are
all first-class Go types. You work with IEC 61850 semantics, not raw MMS
domain names, item IDs, or alternate access selectors.

---

## Table of contents

- [Features](#features)
- [Installation](#installation)
- [Quick start](#quick-start)
  - [Client](#client)
  - [Server](#server)
  - [SCL parsing](#scl-parsing)
- [API overview](#api-overview)
  - [Connection](#connection)
  - [Browse & model discovery](#browse--model-discovery)
  - [Read & write](#read--write)
  - [Datasets](#datasets)
  - [Reports (URCB / BRCB)](#reports-urcb--brcb)
  - [Controls](#controls)
  - [Files](#files)
  - [Journals](#journals)
  - [Caching](#caching)
  - [SCL tooling](#scl-tooling-scl-package)
  - [Server](#server-1)
- [Command-line tools](#command-line-tools)
  - [sclparse](#sclparse)
  - [sclgen](#sclgen)
- [Object references](#object-references)
- [Functional constraints](#functional-constraints)
- [Architecture](#architecture)
- [Repository layout](#repository-layout)
- [Interoperability](#interoperability)
- [Documentation](#documentation)
- [License](#license)

---

## Features

- **Browse & discover** — list logical devices, nodes, data objects; build
  model trees; search by glob or regex
- **Read & write** — typed single/bulk reads and writes with IEC 61850 semantic
  values (quality, timestamps, enums, Dbpos, BCR)
- **Datasets** — list, inspect, read, create, and delete named variable lists
- **Reports** — full BRCB/URCB lifecycle: inspect, configure, subscribe, GI
  trigger, reserve/release, integrity period, segmented report reassembly,
  configurable overflow callback, BRCB replay on re-enable
- **Controls** — direct normal, SBO normal, and SBOw (enhanced security);
  server-side connection-scoped ownership enforcement, configurable select
  timeout, panic containment for application callbacks
- **Files** — list, read, download, delete, rename, obtain
- **Journals** — read by time range or entry ID, auto-pagination helpers
- **SCL tooling** — parse SCD/ICD/CID/IID files (schema editions v1.7 through
  2007C5), validate, flatten/export, generate XML, topology lookup helpers
- **CLI tools** — `sclparse` (inspect, validate, summarize SCL files) and
  `sclgen` (re-generate Go types from XSD)
- **Caching** — optional client-side model cache (none / explicit / lazy)
- **Server** — SCL-driven MMS server; runtime report engine (URCB/BRCB),
  control handlers (SBO/SBOw state machine), setting groups, journal services

---

## Installation

```bash
go get github.com/otfabric/go-iec61850
```

Requires Go 1.24 or later.

Install the CLI tools:

```bash
go install github.com/otfabric/go-iec61850/scl/cmd/sclparse@latest
go install github.com/otfabric/go-iec61850/scl/cmd/sclgen@latest
```

---

## Quick start

### Client

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/otfabric/go-iec61850"
)

func main() {
    ctx := context.Background()

    client, err := iec61850.Dial(ctx, "10.0.0.1:102", iec61850.DialOptions{})
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close(ctx)

    devices, err := client.ListLogicalDevices(ctx)
    if err != nil {
        log.Fatal(err)
    }
    for _, ld := range devices {
        fmt.Println(ld.Name)
    }

    // Read a data attribute by IEC 61850 reference.
    ref, _ := iec61850.ParseRef("PROT1LD/MMXU1.TotW.mag.f[MX]")
    val, err := client.Read(ctx, ref)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(val)
}
```

### Server

```go
package main

import (
    "context"
    "log"

    "github.com/otfabric/go-iec61850"
    "github.com/otfabric/go-iec61850/scl"
)

func main() {
    s, err := scl.ParseFile("station.icd")
    if err != nil {
        log.Fatal(err)
    }

    model, err := iec61850.NewServerModelFromSCL(s, "IED1", "")
    if err != nil {
        log.Fatal(err)
    }

    srv, err := iec61850.NewServer(model, iec61850.ServerOptions{})
    if err != nil {
        log.Fatal(err)
    }

    re := srv.EnableReports()
    defer re.Stop()

    // Register a direct-control handler for GGIO1.SPCSO1.
    srv.RegisterControl("InteropLD", "GGIO1.SPCSO1", iec61850.CtlModelDirectNormal,
        iec61850.ControlHandler{
            OnOperate: func(ctx context.Context, req iec61850.ControlRequest) error {
                log.Printf("operate: %s ctlVal=%v", req.Ref, req.CtlVal)
                return nil
            },
        })

    log.Fatal(srv.ListenAndServe(":102"))
}
```

### SCL parsing

```go
package main

import (
    "fmt"
    "log"

    "github.com/otfabric/go-iec61850/scl"
)

func main() {
    s, err := scl.ParseFile("station.scd")
    if err != nil {
        log.Fatal(err)
    }
    for _, ied := range s.IEDs {
        fmt.Printf("IED: %s (%s)\n", ied.Name, ied.Manufacturer)
    }

    findings := scl.Validate(s)
    for _, f := range findings {
        fmt.Println(f)
    }
}
```

---

## API overview

### Connection

| Function | Description |
|----------|-------------|
| `Dial` | Connect to an IEC 61850 server over MMS/TCP |
| `NewClient` | Wrap an existing `mms.Client` |
| `Client.Close` | Graceful close (MMS Conclude) |
| `Client.Abort` | Immediate abort |

### Browse & model discovery

| Method | Description |
|--------|-------------|
| `ListLogicalDevices` | List all logical devices |
| `ListLogicalNodes` | List logical nodes in an LD |
| `ListDataObjects` | List data objects in an LN |
| `ListChildren` | List direct children of a DO/DA |
| `Tree` / `TreeWithOptions` | Build the full model tree |
| `FindPaths` | Search by glob or regex pattern |
| `GetVariableType` | Get MMS type spec for a reference |

### Read & write

| Method | Description |
|--------|-------------|
| `Read` | Read a typed IEC 61850 value |
| `ReadRaw` | Read the raw MMS value |
| `ReadComponent` | Read a single structure component |
| `ReadMultiple` | Bulk read (single MMS PDU) |
| `Write` | Write a value |
| `WriteMultiple` | Bulk write (single MMS PDU) |

### Datasets

| Method | Description |
|--------|-------------|
| `ListDataSets` | List all datasets in an LD |
| `GetDataSet` | Get dataset definition and member list |
| `ReadDataSet` | Read all dataset member values |
| `CreateDataSet` | Create a dynamic named variable list (client only; server-side dynamic dataset creation is not yet implemented) |
| `DeleteDataSet` | Delete a dynamic named variable list (client only; server-side dynamic dataset deletion is not yet implemented) |

### Reports (URCB / BRCB)

| Method | Description |
|--------|-------------|
| `ListReports` | List all RCBs in an LD |
| `GetReportControlBlock` | Read RCB attributes |
| `SetReportControlBlock` | Write RCB attributes (mask-based) |
| `SubscribeReport` | Subscribe and manage the full RCB lifecycle |
| `TriggerGI` | Send a General Interrogation trigger |
| `ReserveURCB` / `ReleaseURCB` | Exclusive URCB ownership |

### Controls

| Method / Type | Description |
|---------------|-------------|
| `Client.Operate` | Direct-normal operate |
| `Client.Select` | SBO normal select (reads `SBO` attribute) |
| `Client.SelectWithValue` | SBOw enhanced-security select (writes `SBOw`) |
| `Client.Cancel` | Cancel an active SBO reservation |
| `Client.ReadCtlModel` | Read the `ctlModel` attribute |
| `Server.RegisterControl` | Install a server-side control handler |
| `ControlHandler` | Callbacks for `OnSelect`, `OnOperate`, `OnCancel` |
| `CtlModelDirectNormal` / `CtlModelSBONormal` / `CtlModelSBOEnhanced` | Control model constants |

### Files

| Method | Description |
|--------|-------------|
| `ListFiles` | List files on the server |
| `ReadFile` | Read file contents into memory |
| `DownloadFile` | Stream file to an `io.Writer` |
| `DeleteFile` | Delete a file |
| `RenameFile` | Rename a file |
| `ObtainFile` | Request server to copy a file from source to destination via its `FileProvider`; MMS segmented role-reversal not implemented |

### Journals

| Method | Description |
|--------|-------------|
| `ListJournals` | List journals in an LD |
| `ReadJournal` | Read entries by time range |
| `ReadJournalAfter` | Read entries after a known entry ID |
| `ReadJournalAll` | Auto-paginating time-range read |
| `ReadJournalAfterAll` | Auto-paginating after-entry read |

### Caching

| Method | Description |
|--------|-------------|
| `RefreshCache` | Refresh the full model cache |
| `RefreshLDCache` | Refresh cache for one logical device |
| `InvalidateCache` | Clear the full cache |
| `InvalidateLDCache` | Clear cache for one logical device |

### SCL tooling (`scl` package)

| Function | Description |
|----------|-------------|
| `Parse` / `ParseFile` | Parse SCL XML (auto-detects schema edition) |
| `Validate` | Semantic validation (types, dataset refs, topology) |
| `Flatten` / `WriteCSV` | Flatten model to tabular rows / CSV |
| `PrintTree` | Print SCL as a human-readable text tree |
| `ExportDataSets` / `ExportReports` | Export summaries |
| `Generate` | Write the SCL model back to XML |
| `FindIED`, `FindLDevice`, etc. | Fast lookup helpers |

See the [scl package README](scl/README.md) for full documentation including
the `sclparse` and `sclgen` command-line tools.

### Server

| Function / Method | Description |
|-------------------|-------------|
| `NewServer` | Create an IEC 61850 server from a data model |
| `NewServerModelFromSCL` | Build the server model from an SCL file |
| `Server.Serve` | Handle a single accepted connection |
| `Server.ListenAndServe` | Accept and serve connections on an address |
| `Server.EnableReports` | Start the runtime report engine (URCB/BRCB) |
| `Server.RegisterControl` | Install a control object handler |
| `Server.SetValue` | Write a value and trigger change-detection reports |
| `Server.Close` | Stop the server and all engines |

---

## Command-line tools

### sclparse

`sclparse` inspects, validates, and queries IEC 61850 SCL files (SCD, ICD,
CID, IID). It supports schema editions v1.7 through 2007C5 and auto-detects
the version.

```bash
# Install
go install github.com/otfabric/go-iec61850/scl/cmd/sclparse@latest

# Quick overview of a file
sclparse summary station.scd

# Semantic validation (types, dataset references, topology linkage)
sclparse validate station.scd

# List all IEDs with access-point and LD counts
sclparse list-ieds station.scd

# List all report control blocks
sclparse list-reports station.scd
```

**Available subcommands:**

| Subcommand | Description |
|------------|-------------|
| `summary` | Compact overview with element counts |
| `validate` | Semantic validation and diagnostics |
| `detect` | Identify schema edition and document kind |
| `inspect` | Show schema version, vendor namespaces, extensions |
| `dump-json` | Emit normalized model as JSON |
| `list-ieds` | List all IEDs with access-point and LD counts |
| `list-lns` | List all logical nodes with IED/LD location |
| `list-datasets` | List all datasets with member counts |
| `list-reports` | List all report control blocks |
| `list-goose` | List all GOOSE (GSE) control blocks |
| `list-smv` | List all Sampled Values control blocks |
| `list-connected-ap` | List all connected access points |
| `list-types` | List all data type templates |
| `version` | Print build version |

See [scl/README.md](scl/README.md) for detailed usage and examples.

### sclgen

`sclgen` regenerates the internal Go type bindings from the official IEC 61850
XSD schemas. It is a developer tool — ordinary users do not need it.

```bash
# Install
go install github.com/otfabric/go-iec61850/scl/cmd/sclgen@latest

# Regenerate Go types for all supported schema editions
sclgen generate --spec-root ./scl/specs --out ./scl/internal/raw

# Verify the generated output is up to date (used in CI)
sclgen check --spec-root ./scl/specs --out ./scl/internal/raw
```

**Available subcommands:**

| Subcommand | Description |
|------------|-------------|
| `generate` | Generate Go types from XSD schemas |
| `check` | Verify generated output is up to date |
| `version` | Print build version |

See [scl/README.md](scl/README.md) for full developer documentation.

---

## Object references

IEC 61850 object references use the format `LD/LN.DO.DA[FC]`:

```go
ref, _ := iec61850.ParseRef("IEDLD1/LLN0.Mod.stVal[ST]")
fmt.Println(ref.LD)   // "IEDLD1"
fmt.Println(ref.LN)   // "LLN0"
fmt.Println(ref.Path) // ["Mod", "stVal"]
fmt.Println(ref.FC)   // "ST"
```

The MMS wire format uses a different convention (domain = LD, item ID =
`LN$FC$DO$DA`). Conversion is handled by `Ref.ToMMS()` and `RefFromMMS()`.

The `DialOptions.IEDName` field allows working with full-IED-qualified servers:
when set, the client automatically prepends the IED name to outgoing domain
names and strips it from returned names, so application code always uses bare
LD instance names.

---

## Functional constraints

| FC | Description |
|----|-------------|
| ST | Status |
| MX | Measured values |
| SP | Setpoint |
| SV | Substitution |
| CF | Configuration |
| DC | Description |
| SG | Setting group |
| SE | Setting group editable |
| SR | Service response |
| OR | Operate received |
| BL | Blocking |
| EX | Extended definition |
| CO | Control |
| RP | Unbuffered reporting |
| BR | Buffered reporting |

---

## Architecture

```
┌─────────────────────────────────────┐
│  Application / sclparse CLI         │
├─────────────────────────────────────┤
│  go-iec61850 (this package)         │
│  IEC 61850 semantics, SCL, reports, │
│  controls, server model             │
├─────────────────────────────────────┤
│  go-mms                             │
│  Generic MMS protocol (ISO 9506)    │
├─────────────────────────────────────┤
│  go-cotp / go-tpkt                  │
│  ISO transport (COTP over TPKT)     │
└─────────────────────────────────────┘
```

---

## Repository layout

```
go-iec61850/
│
├── *.go                        Core library (client, server, browse, read,
│                               write, reports, controls, files, journals,
│                               datasets, caching, values, FC types, errors)
│
├── examples/
│   ├── basic-client/           Connect, list LDs, read a data attribute
│   ├── browse-tree/            Print the full model tree
│   ├── control/                Direct and SBO control operations
│   ├── files/                  File listing and download
│   └── reports/                URCB/BRCB subscription and delivery
│
├── interop/                    Interoperability tests (build tag: interop)
│   │                           Runs against live libiec61850 and iec61850bean
│   │                           adapters from mms-interop
│   ├── harness_test.go         Docker-based test harness
│   ├── libiec61850_*_test.go   Tests against libiec61850 adapters
│   ├── iec61850bean_*_test.go  Tests against iec61850bean adapters
│   ├── brcb_test.go            BRCB: EntryID, replay, purge, overflow
│   ├── cdc_test.go             CDC reads: DPS, BCR
│   ├── control_test.go         Direct, SBO, SBOw interop
│   ├── sbo_state_test.go       SBO/SBOw state machine
│   ├── urcb_*_test.go          URCB integrity, trigger, reservation
│   ├── multi_client_test.go    Concurrent multi-client scenarios
│   └── testdata/               ICD fixture and initial values
│
├── scl/                        SCL parsing, validation, code generation
│   ├── model.go                SCL Go type tree (SCD/ICD/CID/IID)
│   ├── parse.go                XML parser (auto-detects schema edition)
│   ├── validate.go             Semantic validation rules
│   ├── flatten.go              Tabular export and CSV writer
│   ├── generate.go             XML serialisation (round-trip)
│   ├── lookup.go               FindIED, FindLDevice, etc.
│   ├── index/                  Fast cross-reference index
│   ├── validate/               Extended validation rules
│   ├── specs/                  Official XSD schema bundles (v1.7–2007C5)
│   ├── internal/
│   │   ├── genir/              XSD-to-Go code generator (used by sclgen)
│   │   └── raw/                Generated XSD bindings (v17, 2007B, …)
│   └── cmd/
│       ├── sclparse/           CLI: inspect, validate, summarize SCL files
│       └── sclgen/             CLI: regenerate Go types from XSD schemas
│
├── internal/
│   ├── mapping/                IEC 61850 ↔ MMS name mapping utilities
│   ├── sclindex/               Lightweight SCL cross-reference index
│   └── servermodel/            Server-side MMS registration and value store
│
├── INTEROP.md                  IEC 61850 interoperability compatibility matrix
├── API.md                      Complete public API reference
├── ERRORS.md                   Error taxonomy, sentinel values, and usage patterns
├── KNOWN_LIMITATIONS.md        Known constraints and unsupported features
├── OBSERVABILITY.md            Logging and structured-log fields
└── RELEASE.md                  Changelog
```

---

## Interoperability

Client and server behaviour is tested bidirectionally against pinned
`libiec61850` and `iec61850bean` adapters through the independently versioned
[mms-interop](https://github.com/otfabric/mms-interop) infrastructure.

Tests live in `interop/` behind `-tags=interop` and cover:
basic operations, URCB/BRCB reporting, CDC reads (SPS, DPS, MV, BCR),
quality and timestamp semantics, datasets, direct/SBO/SBOw controls,
multi-client concurrency, and negative/error cases.

**Scope of interoperability evidence:** two independent software stacks have
been tested. Interoperability with physical IEDs (protection relays, meters,
bay controllers, RTUs) has not yet been formally established.

See [INTEROP.md](INTEROP.md) for the full compatibility matrix and
`make interop` to run the suite locally.

---

## Documentation

| Document | Description |
|----------|-------------|
| [scl/README.md](scl/README.md) | `scl` package, `sclparse`, and `sclgen` reference |
| [INTEROP.md](INTEROP.md) | Interoperability tests and compatibility matrix |
| [API.md](API.md) | Complete public API reference (types, methods, errors) |
| [ERRORS.md](ERRORS.md) | Error taxonomy, sentinel values, typed errors, usage patterns |
| [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md) | Known limitations and constraints |
| [OBSERVABILITY.md](OBSERVABILITY.md) | Logging and structured-log observability |

---

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
