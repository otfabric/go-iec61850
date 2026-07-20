# go-iec61850

[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Go Reference](https://pkg.go.dev/badge/github.com/otfabric/go-iec61850.svg)](https://pkg.go.dev/github.com/otfabric/go-iec61850)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/otfabric/go-iec61850/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/go-iec61850/actions/workflows/ci.yml)
[![Codecov](https://codecov.io/gh/otfabric/go-iec61850/graph/badge.svg)](https://codecov.io/gh/otfabric/go-iec61850)
[![Release](https://img.shields.io/github/v/release/otfabric/go-iec61850?label=release)](https://github.com/otfabric/go-iec61850/releases)

A pure-Go IEC 61850 MMS client and server library built on top of
[go-mms](https://github.com/otfabric/go-mms).

This library exposes IEC 61850 concepts — logical devices, logical nodes, data
objects, data attributes, functional constraints, reports, datasets, quality,
timestamps, SCL — as first-class Go types. Users work with IEC 61850 semantics,
not raw MMS domains, item IDs, or alternate access selectors.

## Features

- **Browse & discover** — list logical devices, nodes, data objects; build model
  trees; search by glob or regex
- **Read & write** — typed single/bulk reads and writes with IEC 61850 semantic
  values (quality, timestamps, enums)
- **Datasets** — list, inspect, read, create, and delete named variable lists
- **Reports** — full BRCB/URCB lifecycle: inspect, configure, subscribe,
  GI trigger, reserve/release, segmented report reassembly, configurable overflow
- **Files** — list, read, download, delete, rename, obtain
- **Journals** — read by time range or entry ID, auto-pagination helpers
- **SCL tooling** — parse SCD/ICD/CID/IID files, validate, flatten/export,
  generate XML, lookup helpers for types and topology
- **Caching** — optional client-side model cache (none / explicit / lazy)
- **Server (experimental)** — SCL-driven server model, MMS registration,
  config generation

## Installation

```bash
go get github.com/otfabric/go-iec61850
```

Requires Go 1.24 or later.

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

## API overview

### Connection

| Function | Description |
|----------|-------------|
| `Dial` | Connect to an IEC 61850 server |
| `NewClient` | Wrap an existing `mms.Client` |
| `Client.Close` | Graceful close |
| `Client.Abort` | Immediate abort |

### Browse & model discovery

| Method | Description |
|--------|-------------|
| `ListLogicalDevices` | List all LDs |
| `ListLogicalNodes` | List LNs in an LD |
| `ListDataObjects` | List DOs in an LN |
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
| `ReadMultiple` | Bulk read |
| `Write` | Write a value |
| `WriteMultiple` | Bulk write |

### Datasets

| Method | Description |
|--------|-------------|
| `ListDataSets` | List datasets in an LD |
| `GetDataSet` | Get dataset definition |
| `ReadDataSet` | Read all dataset member values |
| `CreateDataSet` | Create a dynamic dataset |
| `DeleteDataSet` | Delete a dynamic dataset |

### Reports

| Method | Description |
|--------|-------------|
| `ListReports` | List RCBs in an LD |
| `GetReportControlBlock` | Read RCB attributes |
| `SetReportControlBlock` | Write RCB attributes (mask-based) |
| `SubscribeReport` | Subscribe with lifecycle management |
| `TriggerGI` | Trigger general interrogation |
| `ReserveURCB` / `ReleaseURCB` | URCB ownership |

### Files

| Method | Description |
|--------|-------------|
| `ListFiles` | List files on the server |
| `ReadFile` | Read file into memory |
| `DownloadFile` | Stream file to an `io.Writer` |
| `DeleteFile` | Delete a file |
| `RenameFile` | Rename a file |
| `ObtainFile` | Server-to-server file transfer |

### Journals

| Method | Description |
|--------|-------------|
| `ListJournals` | List journals in an LD |
| `ReadJournal` | Read entries by time range |
| `ReadJournalAfter` | Read entries after an entry ID |
| `ReadJournalAll` | Auto-paginating time-range read |
| `ReadJournalAfterAll` | Auto-paginating after-entry read |

### Caching

| Method | Description |
|--------|-------------|
| `RefreshCache` | Refresh full model cache |
| `RefreshLDCache` | Refresh cache for one LD |
| `InvalidateCache` | Clear full cache |
| `InvalidateLDCache` | Clear cache for one LD |

### SCL tooling (`scl` package)

| Function | Description |
|----------|-------------|
| `Parse` / `ParseFile` | Parse SCL XML |
| `Validate` | Semantic validation |
| `Flatten` | Flatten to tabular rows |
| `WriteCSV` | Export flat rows as CSV |
| `PrintTree` | Print SCL as text tree |
| `ExportDataSets` | Export dataset summaries |
| `ExportReports` | Export report summaries |
| `Generate` | Write SCL model back to XML |
| `FindIED`, `FindLDevice`, etc. | Lookup helpers |

### Server (experimental)

| Function / Method | Description |
|-------------------|-------------|
| `NewServer` | Create server from data model |
| `NewServerModelFromSCL` | Build server model from SCL |
| `Server.Serve` | Handle a single connection |
| `Server.ListenAndServe` | Accept and serve connections |

## Architecture

```
┌─────────────────────────────────────┐
│  Application / iec61850ctl CLI      │
├─────────────────────────────────────┤
│  go-iec61850 (this package)         │
│  IEC 61850 semantics, SCL, reports  │
├─────────────────────────────────────┤
│  go-mms                             │
│  Generic MMS protocol               │
├─────────────────────────────────────┤
│  go-cotp / go-tpkt                  │
│  ISO transport (COTP over TPKT)     │
└─────────────────────────────────────┘
```

## Object references

IEC 61850 object references use the format `LD/LN.DO.DA[FC]`:

```go
ref, _ := iec61850.ParseRef("IEDLD1/LLN0.Mod.stVal[ST]")
fmt.Println(ref.LD)   // "IEDLD1"
fmt.Println(ref.LN)   // "LLN0"
fmt.Println(ref.Path) // "Mod.stVal"
fmt.Println(ref.FC)   // "ST"
```

The MMS wire format uses a different convention (domain = LD, item ID =
`LN$FC$DO$DA`). Conversion is handled by `Ref.ToMMS()` and `RefFromMMS()`.

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

## Interoperability

Client and server behaviour is tested bidirectionally against pinned libiec61850 and iec61850bean adapters through the independently versioned [mms-interop](https://github.com/otfabric/mms-interop) infrastructure. Tests in `interop/` run behind `-tags=interop`.

See [INTEROP.md](INTEROP.md) for the compatibility matrix and `make interop` for how to run.

## Documentation

- [INTEROP.md](INTEROP.md) — interoperability tests and IEC 61850 compatibility matrix
- [API.md](API.md) — complete public API reference (types, methods, errors)
- [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md) — known limitations and constraints
- [OBSERVABILITY.md](OBSERVABILITY.md) — logging and observability

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
