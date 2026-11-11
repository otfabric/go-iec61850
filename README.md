# go-iec61850

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Go Reference](https://pkg.go.dev/badge/github.com/otfabric/go-iec61850.svg)](https://pkg.go.dev/github.com/otfabric/go-iec61850)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/otfabric/go-iec61850/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/go-iec61850/actions/workflows/ci.yml)
[![Codecov](https://codecov.io/gh/otfabric/go-iec61850/graph/badge.svg)](https://codecov.io/gh/otfabric/go-iec61850)
[![Release](https://img.shields.io/github/v/release/otfabric/go-iec61850?label=release)](https://github.com/otfabric/go-iec61850/releases)

A pure-Go IEC 61850 stack: **MMS client and server**, **GOOSE** and
**Sampled Values** publishers and subscribers, and **SCL** parsing and
validation. No cgo, Go 1.26+.

Logical devices, logical nodes, data objects and attributes, functional
constraints, datasets, report, GOOSE and SV control blocks, quality and
timestamps are first-class Go types. You work with IEC 61850 semantics, not
with raw MMS domain names, item IDs, BER or Ethernet frames.

| Protocol | Standard | Client / subscriber | Server / publisher |
|---|---|---|---|
| MMS (ACSI over ISO/TCP) | IEC 61850-8-1, ISO 9506 | `iec61850.Client` | `iec61850.Server` |
| GOOSE | IEC 61850-8-1 | `goose.Subscriber`, `Client.GetGoCB` | `goose.Publisher`, `Server.EnableGOOSE` |
| Sampled Values | IEC 61850-9-2, IEC 61869-9, UCA 9-2LE | `sv.Subscriber`, `Client.GetMSVCB` | `sv.Publisher`, `Server.EnableSV` |
| R-GOOSE, R-SV (UDP/IP, HMAC) | IEC TR 61850-90-5 session layer | `goose`/`sv` subscriber over `rsession.Transport` | `goose`/`sv` publisher over `rsession.Transport` |
| SCL | IEC 61850-6 (1.7 – 2007C5) | `scl`, `scl/index`, `scl/validate` | `NewServerModelFromSCL`, `goose`/`sv` `ControlBlockFromSCL` |

---

## Status

- **Stable API** — v1 follows semantic versioning for every non-internal
  package, client and server alike ([COMPATIBILITY.md](COMPATIBILITY.md)).
- **Interoperability** — tested in CI against pinned builds of five
  independent stacks (libiec61850, iec61850bean, dscsystems,
  iec61850-rust, cbsigma) in both directions, GOOSE (and SV, all but
  iec61850bean and cbsigma) on a Docker bridge included ([per-stack matrix](COMPATIBILITY.md#per-stack-matrix),
  [INTEROP.md](INTEROP.md)).
- **Quality gates** — ≥ 90 % statement coverage per package, race
  detector on Go 1.26–1.27, fuzz targets for the MMS value, GOOSE, SV and
  SCL decoders, chaos tests, golangci-lint.
- **Not certified** — no UCA conformance certification and no formal
  validation against physical IEDs yet; see
  [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md) and
  [SECURITY.md](SECURITY.md) before deploying.

---

## Table of contents

- [Status](#status)
- [Features](#features)
- [Installation](#installation)
- [Quick start](#quick-start)
  - [MMS client](#mms-client)
  - [MMS server](#mms-server)
  - [GOOSE](#goose)
  - [Sampled Values](#sampled-values)
  - [SCL parsing](#scl-parsing)
- [API overview](#api-overview)
- [Command-line tools](#command-line-tools)
  - [Install sclparse and sclgen](#install-sclparse-and-sclgen)
- [Object references](#object-references)
- [Functional constraints](#functional-constraints)
- [Architecture](#architecture)
- [Repository layout](#repository-layout)
- [Interoperability](#interoperability)
- [Development](#development)
- [Documentation](#documentation)
- [License](#license)

---

## Features

**MMS client**

- **Browse & discover** — logical devices, nodes, data objects; model
  trees; search by glob or regex; optional model cache
- **Read & write** — typed single and bulk reads and writes with IEC 61850
  values (quality, timestamps, enums, Dbpos, BCR)
- **Datasets** — list, inspect, read, create and delete
- **Reports** — full BRCB/URCB lifecycle: configure, subscribe, GI,
  reserve/release, integrity period, segmented report reassembly,
  overflow callback, BRCB resume by EntryID (BRCB is tested against this
  library's own server only, see [Interoperability](#interoperability))
- **Controls** — direct, SBO and SBOw (normal and enhanced security)
- **Setting groups, journals, files**
- **Resilience** — connection retry with back-off, `slog` logging, metrics
  hooks (go-mms)

**MMS server**

- SCL-driven model; reports (URCB/BRCB), controls (SBO/SBOw state machine,
  CommandTermination, LastApplError), setting groups, journals, file services, Identify
  and Status; connection hooks, authentication hook, graceful `Shutdown`
- One `SetValue` call drives reports, journals, GOOSE and SV

**GOOSE** ([GOOSE.md](GOOSE.md))

- Frame codec, publisher with stNum/sqNum and MinTime→MaxTime
  retransmission, supervised subscriber (timeouts, gaps, restarts,
  validation against SCL), typed events
- Server GoCBs published on data change and exposed over MMS; client GoCB
  read/write

**Sampled Values** ([SV.md](SV.md))

- Frame codec, dataset layouts and 9-2LE scaling, drift-free pacing with
  `noASDU` batching, loss/duplicate/reorder accounting and supervision
  without allocations per frame
- Server MSVCBs published at `smpRate` and exposed over MMS; client MSVCB
  read/write

**Raw Ethernet** (`l2`, shared by GOOSE and SV) — Linux `AF_PACKET`,
macOS/BSD BPF, kernel EtherType filters, an in-memory hub with fault
injection for tests

**SCL** ([scl/README.md](scl/README.md)) — parse SCD/ICD/CID/IID (schema
editions 1.7 to 2007C5), validate, flatten/export, generate XML, lookup
helpers; CLI tools `sclparse` and `sclgen`

---

## Installation

```bash
go get github.com/otfabric/go-iec61850
```

Requires Go 1.26 or later.

The command-line tools `sclparse` and `sclgen` have their own installer: see
[Install sclparse and sclgen](#install-sclparse-and-sclgen). Each tool is a
nested module, so the library does not require cobra.

---

## Quick start

The snippets below are compiled and run by `go test`
([readme_test.go](readme_test.go)) against the demo IED of the examples
([demo.icd](examples/internal/demoied/demo.icd)): an IED `DemoIED` with
logical device `LD1` (served as MMS domain `DemoIEDLD1`, the IED name
followed by the instance), a GOOSE control block `gcb01` and a 9-2LE Sampled
Values control block `MSVCB01`. Complete programs, each with a README
and a smoke test, are in [examples/](examples/).

### MMS client

```go
import (
    "context"
    "errors"
    "fmt"
    "log/slog"

    iec61850 "github.com/otfabric/go-iec61850"
    "github.com/otfabric/go-mms"
)

func readMeasurement(ctx context.Context, addr string) error {
    client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{
        Retry:  mms.DefaultRetryPolicy(), // retry the connection with backoff
        Logger: slog.Default(),
    })
    if err != nil {
        return err
    }
    defer func() { _ = client.Close(ctx) }()

    devices, err := client.ListLogicalDevices(ctx)
    if err != nil {
        return err
    }
    for _, ld := range devices {
        fmt.Println("LD", ld.Name)
    }

    ref, err := iec61850.ParseRef("DemoIEDLD1/MMXU1.PhV.phsA.cVal.mag.f[MX]")
    if err != nil {
        return err
    }
    val, err := client.Read(ctx, ref)
    if errors.Is(err, iec61850.ErrDataAccess) {
        return fmt.Errorf("the server refused %s: %w", ref, err)
    } else if err != nil {
        return err
    }
    fmt.Println(ref, "=", val)
    return nil
}
```

`Dial` sends the ISO addressing IEDs expect (T-selector `0001`, TPDU size
1024, S-selector `0001`, P-selector `00000001`, AP-titles `1.1.1.999` /
`1.1.1.999.1`, AE-qualifier 12). For an IED configured otherwise set
`MMS.Transport.RemoteTSelector` and `MMS.ISO.Remote*`, or take them from its
SCL ConnectedAP with `ApplySCLAddress` ([API.md](API.md#iso-addressing)).
`DialOptions` also takes `Metrics` (an `mms.Metrics`), a model `Cache`,
the `IEDName` prefix of Edition 1 servers, and a `Dialer` that opens the TCP
connection, for a destination allowlist checked against the resolved IP, a
proxy or an in-memory transport ([SECURITY.md](SECURITY.md#deployment-guidance)). Examples:
[basic-client](examples/basic-client), [ied-client](examples/ied-client)
(reports), [resilient-client](examples/resilient-client) (reconnects,
slog, metrics), [datasets](examples/datasets),
[setting-groups](examples/setting-groups), [journal](examples/journal),
[control](examples/control), [files](examples/files).

### MMS server

```go
func serve(ctx context.Context, doc *scl.SCL, ln *iso.Listener) error {
    model, err := iec61850.NewServerModelFromSCL(doc, "DemoIED", "")
    if err != nil {
        return err
    }
    srv, err := iec61850.NewServer(model, iec61850.ServerOptions{Logger: slog.Default()})
    if err != nil {
        return err
    }
    defer srv.Close()
    srv.EnableReports()

    // Store keys are "<LD>/<MMS item ID>".
    err = srv.RegisterControl("DemoIEDLD1", "GGIO1.SPCSO1", iec61850.CtlModelDirectNormal, iec61850.ControlHandler{
        OnOperate: func(ctx context.Context, req iec61850.ControlRequest) error {
            on, _ := req.CtlVal.Bool()
            srv.SetValue(ctx, "DemoIEDLD1/GGIO1$ST$SPCSO1$stVal", mms.NewBoolean(on))
            return nil
        },
    })
    if err != nil {
        return err
    }
    // SetValue updates the model and drives reports, logs, GOOSE and SV.
    srv.SetValue(ctx, "DemoIEDLD1/MMXU1$MX$PhV$phsA$cVal$mag$f", mms.NewFloat(230.1))

    return srv.ListenAndServe(ctx, ln) // until ctx is done
}
```

with `doc, err := scl.ParseFile("station.icd")` and
`ln, err := iso.Listen(":102")`. `EnableJournals`,
`EnableSettingGroups`, `EnableGOOSE` and `EnableSV` add the other
services; see [examples/ied-server](examples/ied-server).

### GOOSE

```go
func publishGOOSE(ctx context.Context, tr goose.Transport, doc *scl.SCL) error {
    cb, err := goose.ControlBlockFromSCL(doc, "DemoIED", "LD1", "gcb01")
    if err != nil {
        return err
    }
    cfg, err := cb.PublisherConfig() // address, references, times, initial values
    if err != nil {
        return err
    }
    cfg.Src = tr.HardwareAddr()
    pub, err := goose.NewPublisher(tr, cfg)
    if err != nil {
        return err
    }
    defer func() { _ = pub.Close() }()
    go func() { _ = pub.Run(ctx) }() // sends, then retransmits from MinTime to MaxTime

    values := cfg.Values
    values[0] = mms.NewBoolean(true)            // GGIO1.Ind1.stVal
    if err := pub.Publish(values); err != nil { // a new state (stNum)
        return err
    }
    <-ctx.Done()
    return nil
}
```

```go
func subscribeGOOSE(ctx context.Context, tr goose.Transport, doc *scl.SCL) error {
    cb, err := goose.ControlBlockFromSCL(doc, "DemoIED", "LD1", "gcb01")
    if err != nil {
        return err
    }
    sub, err := goose.NewSubscriber(tr, goose.SubscriberConfig{})
    if err != nil {
        return err
    }
    defer func() { _ = sub.Close() }()
    cfg := cb.SubscriptionConfig() // filters and validation from SCL
    cfg.OnlyStateChanges = true
    s, err := sub.Subscribe(cfg)
    if err != nil {
        return err
    }
    go func() { _ = sub.Run(ctx) }()

    for {
        select {
        case <-ctx.Done():
            return nil
        case ev := <-s.Events():
            switch ev.Kind {
            case goose.EventMessage:
                fmt.Println("stNum", ev.Frame.PDU.StNum, ev.Frame.PDU.AllData)
            case goose.EventTimeout:
                fmt.Println("timeAllowedToLive expired")
            case goose.EventInvalid:
                fmt.Println("invalid:", ev.Err, errors.Is(ev.Err, goose.ErrConfRevMismatch))
            default: // restarts, sequence gaps, ...
            }
        }
    }
}
```

`tr` is a raw Ethernet transport, `goose.Open("eth0",
goose.TransportOptions{})` (Linux: `CAP_NET_RAW`; macOS: `/dev/bpf`
access), an in-memory `goose.NewHub().NewPort(goose.MAC{})` in tests,
or `rsession.Open(...)` for authenticated R-GOOSE over UDP/IP.
See [GOOSE.md](GOOSE.md) and the examples
[goose-publisher](examples/goose-publisher),
[goose-subscriber](examples/goose-subscriber),
[goose-server](examples/goose-server) and
[goose-client](examples/goose-client).

### Sampled Values

```go
func publishSV(ctx context.Context, tr sv.Transport, doc *scl.SCL) error {
    cb, err := sv.ControlBlockFromSCL(doc, "DemoIED", "LD1", "MSVCB01") // 9-2LE, 4000 samples/s
    if err != nil {
        return err
    }
    cfg, err := cb.PublisherConfig()
    if err != nil {
        return err
    }
    cfg.Src = tr.HardwareAddr()
    cfg.Source = func(info sv.SampleInfo, dst []byte) []byte { // called for every sample
        wt := 2 * math.Pi * 50 * float64(info.Index) / 4000
        var s sv.LESample
        s.Current[0].Value = sv.AmpsCount(141.4 * math.Sin(wt))
        s.Voltage[0].Value = sv.VoltsCount(89800 * math.Sin(wt))
        return s.Append(dst)
    }
    pub, err := sv.NewPublisher(tr, cfg)
    if err != nil {
        return err
    }
    defer func() { _ = pub.Close() }()
    return pub.Run(ctx) // paced on an absolute schedule until ctx is done
}
```

```go
func subscribeSV(ctx context.Context, tr sv.Transport, doc *scl.SCL) error {
    cb, err := sv.ControlBlockFromSCL(doc, "DemoIED", "LD1", "MSVCB01")
    if err != nil {
        return err
    }
    s, err := sv.NewSubscriber(tr, sv.SubscriberConfig{})
    if err != nil {
        return err
    }
    defer func() { _ = s.Close() }()
    cfg := cb.SubscriptionConfig()      // filters and validation from SCL
    cfg.OnASDU = func(smp *sv.Sample) { // on the receive goroutine, without allocation
        var le sv.LESample
        if smp.ASDU.SmpCnt == 0 && sv.DecodeLE(smp.ASDU.Sample, &le) == nil {
            fmt.Printf("IA %.1f A, UA %.0f V\n", sv.Amps(le.Current[0].Value), sv.Volts(le.Voltage[0].Value))
        }
    }
    sub, err := s.Subscribe(cfg)
    if err != nil {
        return err
    }
    go func() {
        for ev := range sub.Events() { // start, gaps, timeouts, synch changes, invalid ASDUs
            fmt.Println(ev.Kind, ev.Stream)
        }
    }()

    err = s.Run(ctx)
    for _, st := range sub.Stats() {
        fmt.Printf("%s: %d ASDUs, %d lost, %.0f samples/s\n", st.Stream, st.ASDUs, st.Lost, st.Rate)
    }
    return err
}
```

`tr` is `sv.Open("eth0", sv.TransportOptions{})` or
`sv.NewHub().NewPort(sv.MAC{})`. See [SV.md](SV.md) and the examples
[sv-publisher](examples/sv-publisher),
[sv-subscriber](examples/sv-subscriber) and
[svcb-client](examples/svcb-client).

### SCL parsing

```go
package main

import (
    "fmt"
    "log"

    "github.com/otfabric/go-iec61850/scl"
    "github.com/otfabric/go-iec61850/scl/index"
    "github.com/otfabric/go-iec61850/scl/validate"
)

func main() {
    s, err := scl.ParseFile("station.scd")
    if err != nil {
        log.Fatal(err)
    }
    for _, ied := range s.IEDs {
        fmt.Printf("IED: %s (%s)\n", ied.Name, ied.Manufacturer)
    }

    idx, idxDiags := index.Build(s)
    for _, d := range validate.All(s, idx, idxDiags) {
        fmt.Println(d.Severity, d.Path, d.Message)
    }
}
```

---

## API overview

### Connection

| Function | Description |
|----------|-------------|
| `Dial` | Connect to an IEC 61850 server over MMS/TCP (`DialOptions.Retry` for back-off, `DialOptions.Dialer` for a custom TCP dialer) |
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

### GOOSE and SV control blocks (client)

| Method | Description |
|--------|-------------|
| `GetGoCB` / `SetGoCB` | Read a GoCB; write GoEna, GoID, DatSet (`GoCBUpdate`) |
| `GetMSVCB` / `SetMSVCB` | Read an MSVCB; write SvEna, MsvID, DatSet, SmpRate, OptFlds, SmpMod (`MSVCBUpdate`) |

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
| `validate.All` | Semantic validation (types, dataset refs, topology); `scl.Validate` is deprecated |
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
| `Server.ListenAndServe` | Accept and serve connections on an `mms.TransportListener` (e.g. from `iso.Listen`) |
| `Server.EnableReports` | Start the runtime report engine (URCB/BRCB) |
| `Server.RegisterControl` | Install a control object handler |
| `Server.SetValue` | Write a value and trigger change-detection reports, GOOSE and SV samples |
| `Server.EnableGOOSE` | Publish the model's GoCBs on a `goose.Transport` |
| `Server.EnableSV` | Publish the model's MSVCBs on an `sv.Transport` |
| `Server.EnableSettingGroups` / `EnableJournals` | Start the setting-group and journal engines |
| `ServerOptions` | Identity, `Authenticate`, `FileProvider`, `OnConnect`/`OnDisconnect`, `Logger`, `Metrics` |
| `Server.Shutdown` | Stop accepting associations and drain the active ones |
| `Server.Close` | Stop the server and all engines immediately |

### GOOSE (`goose` package) and Sampled Values (`sv` package)

| Function / Type | Description |
|-----------------|-------------|
| `Open` | Raw Ethernet transport on an interface (`AF_PACKET` / BPF) |
| `NewHub` | In-memory transport for tests and simulation |
| `ControlBlockFromSCL` / `ControlBlocksFromSCL` | Control block, address and dataset layout from SCL |
| `NewPublisher` | GOOSE: stNum/sqNum and retransmission; SV: paced samples with `noASDU` batching |
| `NewSubscriber` → `Subscribe` | Filtered, validated subscriptions with events and supervision (GOOSE) or stats (SV) |
| `Encode` / `Decode` | Frame codecs with bounded decoding (`Limits`) |
| `sv.LESample`, `sv.DecodeLE` | 9-2LE sample layout and scaling |

See [GOOSE.md](GOOSE.md), [SV.md](SV.md) and [API.md](API.md#goose-package)
for the full API.

---

## Command-line tools

### Install sclparse and sclgen

```bash
curl -fsSL https://get.otfabric.io/sclparse | sh
```

```powershell
irm https://get.otfabric.io/sclparse.ps1 | iex
```

```bash
curl -fsSL https://get.otfabric.io/sclgen | sh
```

```powershell
irm https://get.otfabric.io/sclgen.ps1 | iex
```

Linux (amd64, arm64, armv7), macOS (Intel, Apple silicon) and Windows (amd64, arm64). The installer verifies the release's SHA-256 checksum, installs per user without `sudo` (`~/.local/bin`, Windows `%LOCALAPPDATA%\OTFabric\bin`), and upgrades in place when run again. Pin a version with `OTF_VERSION=x.y.z`; for CI, containers and device images use `OTF_UNMANAGED_INSTALL=<dir>`; all options: [otfabric/install](https://github.com/otfabric/install#options). `OTF_VERSION` is the version of go-iec61850, which ships `sclparse` and `sclgen`. For offline machines, the archives and checksums are on [Releases](https://github.com/otfabric/go-iec61850/releases).

### Shell completion

`sclparse` completes SCL file names (`.scd`, `.cid`, `.icd`, `.ssd`, `.iid`, `.sed`, `.xml`); `sclgen` completes the `--versions` editions and directories.

```bash
# bash (needs the bash-completion package)
mkdir -p ~/.local/share/bash-completion/completions
sclparse completion bash > ~/.local/share/bash-completion/completions/sclparse
sclgen completion bash > ~/.local/share/bash-completion/completions/sclgen

# zsh: a directory on your fpath, then restart the shell
mkdir -p ~/.zsh/completions && sclparse completion zsh > ~/.zsh/completions/_sclparse
mkdir -p ~/.zsh/completions && sclgen completion zsh > ~/.zsh/completions/_sclgen
# and once in ~/.zshrc:  fpath=(~/.zsh/completions $fpath); autoload -U compinit && compinit

# fish
mkdir -p ~/.config/fish/completions && sclparse completion fish > ~/.config/fish/completions/sclparse.fish
mkdir -p ~/.config/fish/completions && sclgen completion fish > ~/.config/fish/completions/sclgen.fish
```

```powershell
# PowerShell: add this line to your profile ($PROFILE)
sclparse completion powershell | Out-String | Invoke-Expression
sclgen completion powershell | Out-String | Invoke-Expression
```

### sclparse

`sclparse` inspects, validates, and queries IEC 61850 SCL files (SCD, ICD,
CID, IID). It supports schema editions v1.7 through 2007C5 and auto-detects
the version.

```bash
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
| `version` | Print version information (`--json`: one line of JSON) |

See [scl/README.md](scl/README.md) for detailed usage and examples.

### sclgen

`sclgen` regenerates the internal Go type bindings from the official IEC 61850
XSD schemas. It is a developer tool — ordinary users do not need it.

```bash
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
| `version` | Print version information (`--json`: one line of JSON) |

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
LD instance names. A client that does not know the IED name takes the logical
devices from `ListLogicalDevices` (GetServerDirectory).

A server built with `NewServerModelFromSCL` serves each logical device under
its IEC 61850-8-1 name, as the other IEC 61850 stacks do: LDevice@ldName when
the SCL gives one, otherwise the IED name followed by the instance
(`InteropIEDInteropLD`). `NewServerModelFromSCLWithOptions` with
`ServerModelOptions{InstanceDomainNames: true}` keeps the bare instance
(`InteropLD`) of go-iec61850 before v1.7.0; setting `ldName` in the SCL is the
standard way to give a device a short name.

---

## Functional constraints

| FC | Description |
|----|-------------|
| ST | Status |
| MX | Measured values |
| SP | Setpoint |
| SV | Substitution (not Sampled Values) |
| CF | Configuration |
| DC | Description |
| SG | Setting group |
| SE | Setting group editable |
| SR | Service response |
| OR | Operate received |
| BL | Blocking |
| EX | Extended definition |
| CO | Control |
| US | Unicast SV control |
| MS | Multicast SV control |
| RP | Unbuffered reporting |
| BR | Buffered reporting |
| LG | Logging (log control) |
| GO | GOOSE control |

---

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│  Application, examples, sclparse                              │
├───────────────────────────────┬───────────────────────────────┤
│  iec61850: Client, Server     │  goose          sv            │
│  reports, controls, datasets, │  codec, pub/sub, SCL binding  │
│  setting groups, journals,    │                               │
│  files, GoCB/MSVCB engines ───┼──► publishes via goose / sv   │
├───────────────────────────────┼───────────────────────────────┤
│  go-mms  (ISO 9506 MMS)       │  l2: AF_PACKET / BPF / Hub    │
├───────────────────────────────┤  EtherType 0x88B8 / 0x88BA    │
│  go-cotp / go-tpkt  (ISO/TCP) │                               │
├───────────────────────────────┴───────────────────────────────┤
│  scl, scl/index, scl/validate — model for server, goose, sv   │
└───────────────────────────────────────────────────────────────┘
```

MMS runs over TCP (port 102); GOOSE and SV are layer-2 multicast on the
process or station bus and need raw-socket privileges.

---

## Repository layout

```
go-iec61850/
│
├── *.go                        Core library (client, server, browse, read,
│                               write, reports, controls, files, journals,
│                               datasets, caching, values, FC types, errors)
│
├── examples/                   Runnable programs, each with a README and
│   │                           an in-process smoke test (see examples/README.md)
│   ├── basic-client/ browse-tree/ ied-client/ resilient-client/
│   │                           MMS client: discovery, reads, reconnects
│   ├── reports/ datasets/ setting-groups/ journal/ control/ files/
│   │                           MMS client services
│   ├── ied-server/             Server with every service (demo IED)
│   ├── goose-publisher/ goose-subscriber/ goose-server/ goose-client/
│   ├── sv-publisher/ sv-subscriber/ svcb-client/
│   └── internal/demoied/       The demo IED (demo.icd) and test helpers
│
├── interop/                    Interoperability tests (build tag: interop)
│   │                           against iec61850-interop adapter images of
│   │                           libiec61850, iec61850bean, dscsystems,
│   │                           iec61850-rust and cbsigma (see interop/README.md)
│   ├── harness_test.go         Docker-based harness, IEC61850_PEER_STACK
│   ├── libiec61850_*_test.go   Stack-parametrized MMS suites (TestLibIEC*)
│   ├── iec61850bean_*_test.go  iec61850bean suites (TestBean*)
│   ├── brcb_*, urcb_*, sbo*,   Reporting, control, CDC, dataset and
│   │   cdc_, dataset_*, …      multi-client scenarios
│   ├── goose_test.go           GOOSE publisher/subscriber/server
│   ├── sv_test.go              SV publisher/subscriber/server, MSVCB over MMS
│   └── testdata/               IED, GOOSE and SV fixtures (iec61850-interop)
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
│       ├── sclparse/           CLI: inspect, validate, summarize SCL files (nested module)
│       └── sclgen/             CLI: regenerate Go types from XSD schemas (nested module)
│
├── goose/                      GOOSE codec, publisher, subscriber, SCL binding
├── sv/                         Sampled Values codec, 9-2LE, publisher,
│                               subscriber, SCL binding
├── l2/                         Raw Ethernet (AF_PACKET, BPF), EtherType
│                               demux and in-memory hub shared by goose and sv
│
├── internal/
│   ├── mapping/                IEC 61850 ↔ MMS name mapping utilities
│   └── servermodel/            Server-side MMS registration and value store
│
├── scripts/covergate.go         Per-package coverage gate used by CI
│
├── API.md                      Complete public API reference
├── ARCHITECTURE.md             Layers, goroutines, ownership, backends
├── COMPATIBILITY.md            Versioning policy, platforms, per-stack matrix
├── ERRORS.md                   Error taxonomy and usage patterns
├── GOOSE.md                    GOOSE guide
├── INTEROP.md                  Interop architecture and service matrix
├── KNOWN_LIMITATIONS.md        Unsupported features and constraints
├── OBSERVABILITY.md            Logging fields and metrics
├── SECURITY.md                 Vulnerability reporting, deployment guidance
├── SV.md                       Sampled Values guide
├── TESTING.md                  Test tiers, coverage policy, fuzz regressions
├── docs/FEATURE-STATUS.md      Test and independent evidence per feature
└── RELEASE.md                  Changelog
```

---

## Interoperability

Every pull request runs the MMS client and server in both directions
against pinned [iec61850-interop](https://github.com/otfabric/iec61850-interop)
adapter images of five independent stacks — libiec61850 (C),
iec61850bean (Java), dscsystems go-iec61850 (Go), iec61850-rust and
cB-Sigma iec61850/iec61850-sim (Go, `cbsigma`) — GOOSE against the
libiec61850, dscsystems, iec61850-rust and cbsigma publishers and
subscribers, and Sampled Values against the libiec61850, dscsystems and
iec61850-rust ones, on a Docker bridge.

The suites in `interop/` (build tag `interop`) cover browsing, reads and
writes, datasets, URCB reporting, CDC reads, quality and timestamps,
direct/SBO/SBOw controls, files, multi-client concurrency, negative cases,
GOOSE header, values, retransmission and supervision, and SV headers,
optional fields, values, loss accounting and MSVCB access over MMS.

BRCB reporting (EntryID, replay, purge, overflow, EntryID resume) has no
independent peer yet: `interop/brcb_*_test.go` runs this library's client
against this library's server, and the adapter matrix does not include BRCB
([INTEROP.md](INTEROP.md)). The Go server's URCB reservation suite
(`interop/urcb_reservation_test.go`) is the same kind of test.

The same suites also run, by hand and never as a required check, against the
latest published go-iec61850 itself: the **Self-interop** workflow uses
`ghcr.io/otfabric/iec61850-interop-otfabric:latest` (`IEC61850_PEER_STACK=otfabric`),
which finds a wire change that breaks an earlier release. It is not
independent evidence ([INTEROP.md](INTEROP.md#against-go-iec61850-itself-self-interop)).

| Where | What |
|---|---|
| [COMPATIBILITY.md](COMPATIBILITY.md#per-stack-matrix) | Per-stack result matrix and stack deviations |
| [INTEROP.md](INTEROP.md) | Test architecture, service matrix, running locally |
| [interop/README.md](interop/README.md) | Environment variables, GOOSE/SV network setup |

Interoperability with physical IEDs (protection relays, meters, bay
controllers, merging units) has not yet been formally established.

---

## Development

```bash
make build          # sclparse and sclgen into bin/
make test           # unit tests
make test-race      # with the race detector
make e2e            # client against server on loopback TCP, every example
make lint-ci        # golangci-lint
make coverage-pkg-check  # >= 90 % statement coverage per package
make fuzz           # every fuzz target, FUZZTIME each (default 15s)
make interop        # MMS interop against the adapter images (Docker)
make interop-goose  # GOOSE and SV interop (Linux, root, Docker network)
make check          # the pre-commit set
```

The raw-socket tests of `l2`, `goose` and `sv` skip without privileges;
CI runs them as root. Releases are cut by the `release.yml` workflow;
[RELEASE.md](RELEASE.md) is the changelog.

---

## Documentation

| Document | Description |
|----------|-------------|
| [pkg.go.dev](https://pkg.go.dev/github.com/otfabric/go-iec61850) | Go reference for every package |
| [API.md](API.md) | Public API reference (client, server, goose, sv, l2, scl) |
| [GOOSE.md](GOOSE.md) | GOOSE publisher, subscriber, transports, SCL binding, server GoCBs |
| [SV.md](SV.md) | Sampled Values publisher, subscriber, 9-2LE, SCL binding, server MSVCBs |
| [scl/README.md](scl/README.md) | `scl` package, `sclparse` and `sclgen` |
| [examples/README.md](examples/README.md) | Runnable examples for MMS, GOOSE and SV |
| [ERRORS.md](ERRORS.md) | Sentinel and typed errors, usage patterns |
| [OBSERVABILITY.md](OBSERVABILITY.md) | Logging fields and metrics hooks |
| [COMPATIBILITY.md](COMPATIBILITY.md) | Versioning policy, supported platforms, per-stack matrix |
| [INTEROP.md](INTEROP.md) | Interoperability tests and service matrix |
| [docs/FEATURE-STATUS.md](docs/FEATURE-STATUS.md) | Each feature: how it is tested, which independent stacks exchanged it |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Package layers, goroutines and ownership, platform backends |
| [TESTING.md](TESTING.md) | Test tiers, prerequisites, coverage policy, fuzz regressions |
| [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md) | Unsupported features and constraints |
| [SECURITY.md](SECURITY.md) | Vulnerability reporting and deployment guidance |
| [RELEASE.md](RELEASE.md) | Release notes |

---

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
