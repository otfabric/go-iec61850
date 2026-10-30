# go-iec61850 Releases

## Unreleased

### Changed

- **`scl/cmd/sclgen` and `scl/cmd/sclparse` are now nested modules.** The
  library's `go.mod` no longer requires cobra, so consumers stop inheriting it.
  Library import paths and the exported API are unchanged.
  - Release binaries are published as before.
  - Build from a checkout with `make build`, or `cd scl/cmd/<tool> && go build .`.
    `go build ./scl/cmd/<tool>` from the root also works, through `go.work`.
  - `go install github.com/otfabric/go-iec61850/scl/cmd/<tool>@<version>` works
    up to v1.2.2 and not after it, while the nested modules' `replace` is in place.

---

## v1.2.2

**Date:** 2026-10-28
**Previous release:** v1.2.1

### Summary

Interop suite moved to the renamed
[iec61850-interop](https://github.com/otfabric/iec61850-interop) adapter
images (v1.0.1); go-mms v1.2.2.

### Changed

- Dependency: `github.com/otfabric/go-mms` v1.2.2.
- Interop: the adapter repository is renamed to
  `otfabric/iec61850-interop`. CI pins the
  `ghcr.io/otfabric/iec61850-interop-<stack>` v1.0.1 images; the local
  default tags are `iec61850-interop-<stack>:local` and the GOOSE network
  is `iec61850-interop-goose`. Environment variables and adapter command
  names are unchanged.
- Interop: the DPS (`GGIO1.DPS1.stVal`) and BCR (`MMTR1.TotVAh.actVal`)
  reads assert the fixture values in every direction on all four stacks
  (libiec61850, iec61850bean, dscsystems, iec61850-rust).

---

## v1.2.1

**Date:** 2026-10-28
**Previous release:** v1.2.0

### Summary

Two client and server fixes found while writing the v1.2.0 examples.

### Fixed

- Client: `GetDataSet` on a data set that does not exist now wraps
  `ErrNotFound`, so `errors.Is(err, iec61850.ErrNotFound)` works. Both
  server answers for an unknown named variable list are recognised
  (MMS access/object-non-existent and definition/object-undefined); the
  `*mms.ServiceError` stays in the error chain. `examples/datasets` uses
  the sentinel.
- Server reports: a member that had no value when the RCB was enabled
  (e.g. an MX float) carried a spurious `quality-change` reason next to
  `data-change` on its first update. A missing previous value now counts
  as a quality change only when the new value carries a quality.

---

## v1.2.0

**Date:** 2026-10-28
**Previous release:** v1.1.0

### Summary

IEC 61850 Sampled Values (new `sv` package and server/client
integration) and a shared raw Ethernet layer (new `l2` package) carrying
GOOSE and SV.

### Added

- **`sv` package** ([SV.md](SV.md)) — pure Go, no cgo, clean-room from
  IEC 61850-9-2, IEC 61869-9 and the UCA 9-2LE guideline:
  - frame codec (EtherType `0x88BA`, APDU header incl. simulation bit,
    `savPdu` with `noASDU`, opaque `security`, ASDUs with svID, datSet,
    smpCnt, confRev, refrTm, smpSynch, smpRate, sample, smpMod,
    gmIdentity) with strict, bounded decoding; zero-allocation
    `DecodeInto` and `AppendFrame`;
  - datasets: `Layout` of fixed-size basic types ↔ go-mms values,
    `Quality`, and 9-2LE helpers (`LESample`, 1 mA / 10 mV scaling);
  - `Publisher`: absolute-schedule pacing at `smpRate` with burst
    catch-up and skip accounting, `noASDU` batching, `smpCnt` wrap,
    `AlignSecond`, per-sample `Source` or thread-safe `Update`, run-time
    `SetEnabled`/`SetSmpSynch`/`SetSimulation`, context shutdown;
  - `Subscriber`: filters (dst, src, APPID, svID), validation (datSet,
    confRev, smpRate/smpMod, sample size), per-stream loss/duplicate/
    out-of-order accounting with learned or derived `smpCnt` wrap, rate
    estimation, synch and confRev tracking, timeout supervision, callback
    or channel delivery, no allocation per frame;
  - SCL binding: `ControlBlocksFromSCL` / `ControlBlockFromSCL`
    (SampledValueControl + SMV address + dataset layout);
  - `Metrics` hooks, sentinel and typed errors; benchmarks for 32
    interleaved 4 kHz streams.
- **`l2` package** — the AF_PACKET/BPF/in-memory transport moved out of
  `goose`: one socket can receive several EtherTypes (kernel filter),
  `Demux` splits them, `MAC.IsSVMulticast`, `VLAN.TCI`.
- **Server SV** — `Server.EnableSV` publishes the model's MSVCBs at
  `smpRate` from the dataset values; MSVCB attributes
  (`LLN0$MS$<name>`: SvEna, MsvID, DatSet, ConfRev, SmpRate, OptFlds,
  SmpMod, DstAddress, noASDU) are readable over MMS, SvEna/MsvID/DatSet/
  SmpRate/OptFlds/SmpMod writable; `Capabilities().SV`.
- **Client MSVCB API** — `Client.GetMSVCB`, `Client.SetMSVCB`.
- **SCL** — `SMVControl` gains `SmpMod`, `SecurityEnable` and `SmvOpts`;
  `SMVControlRow` gains `SmpMod`.
- **Examples** ([examples/README.md](examples/README.md)) — new
  `goose-publisher`, `goose-subscriber`, `goose-server`, `goose-client`,
  `sv-publisher`, `sv-subscriber`, `svcb-client`, `datasets`,
  `setting-groups`, `journal` and `resilient-client`; the existing ones
  reworked (flags, contexts, `errors.Is`, graceful shutdown) and
  `ied-server` now serves a demo IED with every service. Each has a
  README and an in-process smoke test; CI builds them.
- **README** quick start for MMS client and server, GOOSE and SV; the
  snippets are compiled and run by `readme_test.go`.

### Fixed

- Server setting groups: `SelectActiveSG`, `SelectEditSG` and
  `ConfirmEditSG` from the go-iec61850 client (component writes to the
  SGCB) bypassed the setting-group engine, so the handlers never ran and
  no edit session started. The SGCB now routes component writes through
  the engine; its read-only attributes ignore stale values.
- Server journals: a `ReadJournalAfter` cursor never matched its entry
  (stored with sub-millisecond time, sent back as a millisecond
  TimeOfDay), so polling clients received the last entries again.
- Server model: a DA name repeated under another FC (ASG `setMag` in SG
  and SE) was rejected as a duplicate.
- SV decoder accepts a one-octet `smpMod` (sent by iec61850-rust).

### Changed

- `goose.MAC`, `Transport`, `TransportOptions`, `TransportError`, `Hub`,
  `HubPort`, `Faults` and `HubStats` are now aliases of the `l2` types,
  and the transport sentinels are shared with `l2`. Code using them by
  name compiles unchanged (verified with iec61850ctl v0.3.0).

---

## v1.1.0

**Date:** 2026-10-28
**Previous release:** v1.0.9

### Summary

GOOSE support (new `goose` package and server/client integration), a
quality pass (per-package coverage gate, fuzz targets, chaos tests,
dial retry) and two more interop stacks (dscsystems, iec61850-rust).

### Added

- **`goose` package** ([GOOSE.md](GOOSE.md)) — pure Go, no cgo:
  - frame codec (Ethernet, 802.1Q, APDU header incl. simulation bit, BER
    `goosePdu`, `allData` as go-mms values) with strict, bounded decoding;
  - `Publisher`: stNum/sqNum, MinTime→MaxTime retransmission,
    timeAllowedToLive, test/simulation/ndsCom, GoEna, context shutdown;
  - `Subscriber`: filters (dst, APPID, gocbRef, src), validation (datSet,
    goID, confRev, member types), per-stream supervision (timeout, gaps,
    duplicates, out-of-order, restarts) as typed events, LGOS-style
    `Supervision` snapshots, channel or callback delivery;
  - transports: Linux `AF_PACKET`, macOS/BSD BPF, in-memory `Hub` with
    fault injection; `ErrUnsupportedPlatform` elsewhere;
  - SCL binding: `ControlBlocksFromSCL` / `ControlBlockFromSCL`
    (GSEControl + GSE address + dataset layout);
  - `Metrics` hooks, `slog` logging, sentinel and typed errors.
- **Server GOOSE** — `Server.EnableGOOSE` publishes the model's GoCBs on
  data change; GoCB attributes (`LLN0$GO$<name>`) are readable over MMS,
  GoEna/GoID/DatSet writable per IEC 61850-7-2/8-1.
- **Client GoCB API** — `Client.GetGoCB`, `Client.SetGoCB`.
- **CommandTermination** — the server sends a positive CommandTermination
  after an enhanced-security operate (found by the dscsystems
  controller, which waits for it).
- **`DialOptions.Retry`** (`mms.RetryPolicy`, via `iso.WithRetry`) —
  connection retries with exponential back-off and jitter; each retry is
  logged at Warn.
- **Metrics** — `DialOptions.Metrics` and `ServerOptions.Metrics` pass an
  `mms.Metrics` to the MMS client and server.
- **`Server.Shutdown`** — graceful stop: closes listeners (also on the
  hook-routing accept loop), concludes associations, stops the engines.
- `ServiceCapabilities.GOOSE`, `ErrGOOSE`.

### Dependencies

- **go-mms** — `v1.1.0` → **[v1.2.1](https://github.com/otfabric/go-mms/releases/tag/v1.2.1)**
  (go-cotp v1.1.0 with per-direction deadlines: a context that expires
  before any octet moved returns `ctx.Err()` and leaves the connection
  usable; v1.2.0 added sentinels incl. `ErrServerClosed`/`ErrInvalidArgument`, `RejectError`,
  `ServiceError.CodeName()`, metrics, retry, `Client.Done`/`Err`,
  `RequestTimeout`, `Server.Shutdown`, `AssociationTimeout`).
  `ServiceError.Error()` now ends with the code name; match errors with
  `errors.Is` / `errors.As`, not their text.

### Fixed

- Server report engine: equal array values compared as different, so an
  unchanged array member triggered a data-change report.

### Quality

- CI coverage gate: every package ≥ 90 % statement coverage
  (`scripts/covergate.go`, raw-socket tests run as root on `lo`).
- Fuzz targets for the GOOSE decoder, GoCB mapping and GSE address
  parsing (nightly matrix now per package).
- Chaos tests: fault-injecting MMS transport (corrupt, truncate, drop,
  reset) on client and server; GOOSE loss/duplication/reorder/corruption.

### Interop

- dscsystems and iec61850-rust adapters as further stacks:
  `IEC61850_PEER_STACK` runs the client/server suites against libiec61850,
  dscsystems or iec61850-rust; unsupported steps skip with a reason
  (per-stack matrix in COMPATIBILITY.md).
- GOOSE interop against libiec61850, dscsystems and iec61850-rust, both
  directions, plus Go IED server → each subscriber (fixture `goose-v1`).

---

## v1.0.9

**Date:** 2026-10-28
**Previous release:** v1.0.8

### Summary

Bump go-mms to [v1.1.0](https://github.com/otfabric/go-mms/releases/tag/v1.1.0)
(FileDirectory / FileDelete wire fixes, optional `lastModified`, BinaryTime
range). No IEC 61850 API is added or removed.

### Dependencies

- **go-mms** — `v1.0.7` → **v1.1.0**

### Changed

- **Empty BRCB `TimeOfEntry` / SCL `EntryTime`** — initialise to the MMS
  TimeOfDay epoch (1984-01-01) instead of Unix 0. go-mms v1.1.0 rejects
  BinaryTime values outside 1984-01-01…2163-06-05, so a read of an unused
  BRCB would fail with a service error.
- **Unknown object DataAccessError** — go-mms now reports
  `object-non-existent` (10) for an unknown domain or variable;
  `object-undefined` (4) remains for alternate-access mismatches. The
  IEC 61850 wrappers still pass the MMS code through
  [`DataAccessError.ErrorCode`](ERRORS.md).
- **FileEntry.LastModified** — zero means the server omitted the optional
  field (already true on the wire; the godoc now says so).

---

## v1.0.8

**Date:** 2026-07-30
**Previous release:** v1.0.7

### Summary

Patch release: bump go-mms to v1.0.7 (pulls go-cotp v1.0.4 / go-tpkt v1.0.1).
No public API surface or behavioural changes. Go floor remains **1.24**.

### Dependencies

- **go-mms** — `v1.0.6` → **v1.0.7**
- **go-cotp** (indirect) — `v1.0.3` → **v1.0.4**
- **go-tpkt** (indirect) — `v1.0.0` → **v1.0.1**

### Unchanged

- No API, wire, or behavioural changes. Import path remains `github.com/otfabric/go-iec61850`.

---

## v1.0.7

**Date:** 2026-07-30
**Previous release:** v1.0.6

### Summary

Dial now propagates `DialOptions.Logger` into the underlying MMS client when
`MMS.Logger` is unset (matching existing Server behaviour). Dependency bump to
go-mms v1.0.6 / go-cotp v1.0.3. Docs and Makefile tooling aligned with sibling
OT Fabric libraries, including a real `make fuzz` target. Broad unit-test
coverage lift (non-example packages ~**90–100%**). Go floor remains **1.24**.

### Fixed

- **Dial logger → MMS** — when `DialOptions.Logger` is set and
  `DialOptions.MMS.Logger` is nil, the IEC logger is passed through to
  `iso.Dial` / `mms.Client` (Server already did this).

### Dependencies

- **go-mms** — `v1.0.5` → **v1.0.6**
- **go-cotp** (indirect) — `v1.0.2` → **v1.0.3**

### Changed

- **OBSERVABILITY.md / options godoc / doc.go** — clarify IEC→MMS logger
  inheritance and that `iso.WithLogger` is independent.
- **API.md** — document `IEDName` on DialOptions/ClientOptions; note logger
  propagation.

### Build / tooling

- **Makefile** — exports `GOWORK=off`; `make fuzz` runs the eight root fuzz
  targets plus `FuzzParse` in `./scl` (replaces the stale “planned for M6”
  stub).

### Tests

Coverage pass across client, server, SCL, and `internal/servermodel`
(approximate statement coverage: root ~**90%**, `scl` ~**92%**,
`scl/validate` **100%**, `scl/index` ~**93%**, `scl/internal/genir` ~**93%**,
`internal/servermodel` ~**87%**). Notable additions:

- **Values / Write / client helpers** — type-mismatch and nil guards on
  accessors; `Write` validation edges; `ldDomain` / `refToMMS` /
  `stripIEDPrefix`.
- **Server** — `installWriteInterceptor` (RCB / SGCB / CO / DA notify),
  `parseCOStoreKey` / `parseRCBStoreKey`.
- **Control / reports** — Operate / Select / SelectWithValue / Cancel /
  ReadCtlModel / ReadLastApplError edges; `buildCancel`; `OrCat` /
  `AddCause` `String`; `ListReportsVerified` / `verifyReportCandidates`.
- **Setting groups (client)** — `GetSettingGroupInfo`, `SelectActiveSG` /
  `SelectEditSG` / `ConfirmEditSG`, `GetEditSGValue` / `SetEditSGValue` /
  `GetActiveSGValue`, `decodeSGCB` member-type edges; server
  `GetEditSettingGroup`.
- **Data sets** — List / Get / Read / Create / Delete validation and
  loopback paths; `memberIdentity`; lazy-cache `ReadDataSet`.
- **SCL** — `Parse` / `ParseFile` / `ParseFileOpts` / `ParseWithOptions` /
  `ParseBytes` (strict, MaxDiagnostics, read failures); `DetectKind` /
  `KindFromPath`; flatten GSE/SMV/ConnectedAP exports; index
  `FindGSEControl` / `FindSMVControl`; validate package edge matrix;
  v1.7 convert Private / SMV / SettingControl / LogControl; genir
  `Emit` / enums / types / SCL root / type-resolve helpers; sclparse
  `list-smv` / completion / version string.
- **servermodel** — `registerSGCB`, compound `registerDA` /
  `registerDAWithFC`, RCB register Read/Write via MMS loopback,
  `makeStructRead` / `buildAttrElemAndRead`.

No public API surface changes. No breaking changes.

Import path remains `github.com/otfabric/go-iec61850`.

---

## v1.0.6

### Fixed

- **Report subscription overflow tests** — `TestSubscribeReport_QueueOverflow` and
  `TestSubscribeReport_OverflowDropOldest` no longer use a fixed sleep plus non-blocking
  drain. They wait for the first delivered report (timeout) before draining the channel,
  which removes a flake under `-race` on busy CI (observed on Go 1.26).
- **Report engine logging** — removed accidental `stderr` debug prints from the URCB
  delayed-send path (`[DBG] send OK` / `send failed`). Failures remain on the structured
  logger.

No API changes. No breaking changes.

Import path remains `github.com/otfabric/go-iec61850`.

---

## v1.0.5

### Dependencies

- Bumped `github.com/otfabric/go-mms` from **v1.0.4 → v1.0.5** (fix 32-bit / linux armv7 compile overflow in file PDU error formatting).

No API changes. No breaking changes.

Import path remains `github.com/otfabric/go-iec61850`.

---

## v1.0.4

Documentation, lint hardening, and code-quality improvements. No API changes, no behavior changes.

### Dependencies

- Bumped `github.com/otfabric/go-mms` from **v1.0.3 → v1.0.4** (documentation and lint hardening; no API or behavior changes).
- Bumped `github.com/otfabric/go-cotp` (indirect) from **v1.0.1 → v1.0.2**.

### Documentation — error taxonomy covering all 20 sentinel values, 7 typed error structs
  (`ReferenceError`, `DecodeError`, `ModelError`, `ReportError`, `SCLParseError`,
  `DataAccessError`, `ControlError`), underlying MMS error passthrough, and `errors.Is` /
  `errors.As` usage patterns.
- Added link to `ERRORS.md` in `README.md` documentation table and repository file-tree section.

### Linting and code quality

- **`golangci-lint` exclusion-free** (except two documented gocritic disables):
  - Removed blanket `errcheck` suppression for test files; all issues fixed in code.
  - Removed dead `staticcheck SA2002` test-file exclusion (SA2002 does not occur in this repo).
  - `ifElseChain` disabled: three-branch `if/else if/else` patterns are preferred over `switch`
    for exclusive error-then-nil-then-value decisions.
  - `exitAfterDefer` disabled: `Example_*` documentation functions intentionally show
    `defer client.Close` followed by `log.Fatal` to model correct cleanup for users.
- Fixed unchecked error returns across 10 test files:
  - `c.Abort`, `sub.Close`, `defer sub.Close` → `_ = ...` or `defer func() { _ = ... }()`
  - `srv.MMS().SetVariableRead(...)` → wrapped in `if err := ...; err != nil { t.Fatalf(...) }`
  - `srv.RegisterControl(...)` in `example_test.go` → `_ = srv.RegisterControl(...)`
- Converted `if/else if/else` chains in `bulk.go` and `dataset.go` to `switch` statements.
- Fixed `exitAfterDefer` in all example programs (`basic-client`, `control`, `files`, `reports`,
  `ied-client`, `ied-server`) and `scl/cmd/sclgen`: moved `defer cancel()` / `defer stop()` to
  after the last potential fatal exit; explicit cleanup called before each `log.Fatal`.
- Removed unused test helper return values:
  - `setupRCBLoopback` dropped `*mms.Server` return (never used by any caller).
  - `setupWritableLoopback` dropped `*writableVars` return (never used by any caller).
  - `executeCmd` in `scl/cmd/sclparse` dropped `string` return (never used by any caller).
- Added `unparam` and `gocritic` linters to `.golangci.yml`.
- Added `vuln` target (`govulncheck`) to `Makefile` and wired it into `make check`.

No API changes. No breaking changes.

Import path remains `github.com/otfabric/go-iec61850`.

---

## v1.0.3

**Added**: Recovery, concurrency, resource-pressure, and decoder-robustness hardening.

### Recovery & lifecycle (`reconnect_test.go`)
- `TestClientReconnect_AfterServerRestart` — reconnect after server shutdown; new-port server model.
- `TestClientReconnect_MultipleSequential` — 5 sequential connect/close cycles without resource leak.
- `TestClientReconnect_URCBResubscription` — URCB re-established after reconnect on same server.
- `TestClient_NoGoroutineLeakOnConnectDisconnect` — 10 cycles; goroutine delta ≤ 3.
- `TestServer_Close_StopsReportEngine` — post-close connections refused immediately.
- `TestClientReconnect_ReadAfterServerClose` — reads return errors (not hang) after server gone.

### BRCB EntryID resume (`interop/brcb_resume_test.go`, `report_engine.go`)
- Implemented `decodeEntryID` and `filterEntriesAfter` helpers.
- `enableRCB` reads the client's pre-written EntryID and filters buffer replay to entries strictly after that point.
- Policy: exclusive boundary — ID N means replay entries > N; zero means full replay; future ID means no replay.
- EntryIDs are 8-byte big-endian, monotonically increasing from 1, non-persistent across server restarts.
- `PurgeBuf=true` invalidates all previously issued EntryIDs.
- Three interop tests: `Resume`, `ZeroResume` (full buffer), `ResumeFromFuture` (no replay).

### Bug fix: BRCB replay skipped after unconditional re-enable (`report_engine.go`)
- **Root cause**: when a BRCB generated a buffered entry it wrote `EntryID` to the value store. On re-enable, `enableRCB` read that server-written value as the resume filter, excluding the only buffered entry — so no replay was delivered.
- **Fix**: `EntryID` in the store is reset to all-zero bytes when `RptEna=false` is written. A subsequent re-enable with no client-written EntryID decodes to `resumeID=0`, which means full replay. Tests that explicitly write an EntryID before re-enabling are unaffected — they overwrite the cleared value before `RptEna=true`.
- Covered by `TestGoServer_BRCB_Replay` (was timing out, now passes).

### Concurrent request hardening (`concurrent_test.go`)
- Context cancellation releases all in-flight confirmed requests.
- Asynchronous report delivery does not stall or corrupt concurrent read responses.
- Aborting the client while 20 writes are in flight — all goroutines exit cleanly.
- Timed-out requests do not accumulate goroutines (delta ≤ 3 after 10 timeouts).

### Resource pressure tests (`resource_exhaust_test.go`)
- 50 sequential connections and 20 concurrent connections without descriptor leak.
- Report flood (30 GIs, queue-size=2) — reads succeed throughout; `OverflowDropNewest` drops silently.
- 10×200 concurrent `SetValue` with 100 simultaneous client reads — zero read errors.

### SCL corpus expansion (`scl_corpus_test.go`)
- Multi-LN model (LLN0 + 3×GGIO + MMXU with AnalogueValue DAType/BDA).
- Multi-LD model (4 logical devices).
- SDO chain (DPC with Oper sub-object).
- Enum DAs (ENS with custom enum type).
- Multi-LN URCB report with cross-LN dataset members.

### Report decoder robustness (additional fuzz targets in `fuzz_test.go`)
- `FuzzDecodeReportIndication` — arbitrary value arrays (4 seeds: minimal, empty, all-zeros, all-ones).
- `FuzzDecodeOptFlds` — arbitrary bit-string bytes for the OptFlds decoder.
- `FuzzDecodeTrgOps` — arbitrary bit-string bytes for the TrgOps decoder.
- `FuzzDecodeReportIndication_NilValues` — selective nil entries via fuzz-driven bit masks.

### API documentation (`example_test.go`)
- Added `Example*` functions for `Dial`, `SubscribeReport`, `Write`, `GetReportControlBlock`, `NewServer`, `SetValue`, `RegisterControl`, and `ParseRef`.

### Service profile (`INTEROP.md`)
- IEC 61850-7-2 service table (client + server columns).
- CDC conformance table with interop-tested column.
- Full report features table (URCB/BRCB, all OptFlds).
- New `BRCB EntryID resume` section with explicit exclusive-boundary contract, byte format, stability guarantees, overflow interaction, and purge invalidation.
- New `Report backpressure and loss policy` section documenting URCB vs. BRCB queue loss semantics.
- Known limitations and deferred features table.

### Scheduled fuzz workflow (`.github/workflows/fuzz.yml`)
- Nightly 5-minute fuzz run per target, with `workflow_dispatch` override.
- Failing corpus entries uploaded as artifacts (30-day retention).

**Race detector**: `go test -race ./...` passes cleanly in both `go-iec61850` and `go-mms`.

---

## v1.0.2

**Fixed**: SBO/SBOw control server — connection-scoped ownership and ctlVal matching.

- `controlRegistration` now tracks `selectConn *mms.ServerConn` (SBO normal and SBOw contention) and `selectCtlVal *mms.Value` (SBOw value matching).
- `executeSelect` denies a second client's select when an active selection by a different connection exists; stale selections (owner disconnected or timed out) are cleared automatically.
- `executeOperate` (SBOw) rejects operates where the operating connection does not match the selecting connection (`AddCauseSelectFailed`) or where the `ctlVal` differs from the one provided at select time (`AddCauseParameterChange`).
- `executeCancel` enforces ownership for both SBO normal (connection identity) and SBOw (connection + origin identity).
- `handleSBOSelectRead` lazily clears a stale selection when the previous owner's connection is no longer active, allowing immediate re-selection by a new client.
- `isConnActive` helper queries the live `mms.Server.Connections()` list to determine whether a stored `*ServerConn` is still open.
- `mmsValuesEqual` helper compares two `*mms.Value` instances by type and byte content.

**Added**: `ControlHandler.SelectTimeout` — configurable per-control select reservation timeout.

When `SelectTimeout > 0` in a `ControlHandler`, that duration overrides `DefaultSelectTimeout` (30 s) for that specific control object. Allows tests and applications to use short timeouts without changing the global default.

**Fixed**: Panic containment for `ControlHandler` callbacks.

`callControlHandler` wraps every invocation of `OnSelect` and `OnOperate` in a `defer recover()`. A panic in application callback code is caught and returned as a `*ControlError` instead of crashing the server goroutine.

**Fixed**: Panic containment for `OnOverflow` report callback.

`deliver` in `report.go` wraps the `OverflowCallback` invocation in a `defer recover()`, preventing an application panic from terminating the report delivery goroutine.

**Fixed**: RCB write interceptor value-restore on rejection.

`internal/servermodel.registerRCB` now saves the previous store value before calling `vs.Set`, and restores it if the write interceptor (e.g. `ReportEngine.HandleRCBWrite`) returns an error. This prevents a rejected URCB `Resv` or `RptEna` write from leaving the value store in an inconsistent state.

**Fixed**: URCB reservation and enable ownership enforcement.

`ReportEngine.HandleRCBWrite` now enforces connection-scoped ownership for `Resv` and `RptEna` writes:
- `Resv=true` is rejected with `DataAccessErrorObjectAccessDenied` if another active connection already holds the reservation.
- `Resv=false` is rejected if the clearing client does not own the reservation.
- `RptEna=true` is rejected for URCBs if the enabling client is not the reservation owner.
- Stale reservations (owner disconnected) are released automatically so a new client can reserve immediately.

**Added**: BRCB replay on re-enable.

`ReportEngine.enableRCB` now replays all buffered entries to the enabling connection when a BRCB is re-enabled with a non-empty buffer. Replay is delivered asynchronously via `replayBRCBEntries` after a 150 ms delay (to ensure the `RptEna` write response reaches the client before the first replayed report). `ReportEngine.SetRCBBufMax` allows test code to shrink the buffer for overflow testing without changing the default.

**Added**: DPS and BCR CDCs to the interop fixture.

`interop/testdata/interop.icd` (and the matching `mms-interop` fixture) now includes:
- `GGIO1.DPS1` — double-point status (`Dbpos` 2-bit BitString, cdc=DPS).
- `MMTR1.TotVAh` — binary counter reading (`INT32U`, cdc=BCR).
- `brcb01` buffered report control block with `entryID=true` and `bufOvfl=true` in `OptFields`.

**Added**: Interop test coverage for new phases.

New test files gated behind `-tags=interop`:

- `interop/brcb_test.go` — `TestGoServer_BRCB_EntryID`, `_Replay`, `_Purge`, `_Overflow`.
- `interop/cdc_test.go` — DPS and BCR read tests in all four directions (adapter directions skip pending `mms-interop` image rebuild).
- `interop/quality_timestamp_test.go` — Quality validity states (good/invalid/questionable/detail-bits), UTC timestamp round-trip, quality flags (`LeapSecondsKnown`, `ClockNotSynchronized`), and fractional-second accuracy sub-tests.
- `interop/sbo_state_test.go` — Full SBO/SBOw state-machine suite: cancel by non-owner, configurable select timeout, repeated select, second-client contention, disconnect releases selection, SBOw `ctlVal` mismatch, SBOw second-client contention.
- `interop/urcb_reservation_test.go` — Double-reserve same/different connection, enable without ownership, disconnect releases reservation.
- `interop/urcb_integrity_test.go` — Integrity-period delivery, coexistence with dchg, disable stops timer.
- `interop/urcb_trigger_test.go` — Data-change, quality-change, data-update, no-dchg-on-same-value, multiple-trigger coalescing.
- `interop/multi_client_test.go` — Concurrent reads (5 clients), concurrent writes to independent variables, invoke-ID isolation (3 clients × 10 reads), disconnect-one-keeps-others.
- `interop/dataset_depth_test.go` additions — `TestGoServer_DS_WritePartialFail` (type-mismatch partial failure in `WriteMultiple`), `TestGoServer_DS_NestedDORead` (structured MX read of `TotW.mag`).

---

## v1.0.1

**Added**: MIT `LICENSE` with copyright `Copyright (c) 2026 OT Fabric`.

**Added**: `// SPDX-License-Identifier: MIT` headers on first-party Go sources (generated `scl/internal/raw` code left unchanged).

**Changed**: README license section and badge block for public release — added pkg.go.dev, removed Go Report Card, and switched Codecov to a tokenless public badge URL.

---

## v1.0.0

**Changed**: Bump dependencies to stable releases (`go-mms v1.0.0`, `go-cotp v1.0.1`).

**Added**: `DialOptions.IEDName` — when set, the client prepends the IED identifier to every MMS domain name it sends and strips it from domain names returned by the server. Callers can always use bare LD instance names regardless of whether the target is a stand-alone Go server (no prefix) or a full IEC 61850 server with a vendor IED name.

**Added**: SBO normal (select-before-operate, `ctlModel=2`) — `Client.Select` reads `SBO[CO]` and grants/denies ownership per connection; `Server.RegisterControl` with `CtlModelSBONormal` installs a read-interceptor that enforces single-owner semantics. `executeOperate` checks the owning connection identity for SBO normal and the originator identity for SBO enhanced, so both paths share a single control-execution function.

**Added**: SBO enhanced (select-before-operate with enhanced security, `ctlModel=4`) — `Client.SelectWithValue` sends an `SBOw[CO]` write containing a full Oper structure; `Server.RegisterControl` with `CtlModelSBOEnhanced` validates that the `ctlNum` in the subsequent `Operate` matches the one used at select time. Cancel clears the select reservation for both SBO models.

**Added**: `Client.Cancel` sends an MMS write to `Cancel[CO]`, releasing any SBO reservation held by the caller.

**Fixed**: `OperateParams` / `CancelParams` Oper structure encoding — `operTm` is now omitted from the encoded structure when not set, instead of sending a zero-time seventh element. Several IEC 61850 servers (including `libiec61850` and `iec61850bean`) define `OperSPC_Type` without `operTm` and reject a seven-element structure with `TypeInconsistent`.

**Fixed**: `Server.RegisterControl` now correctly handles the `CtlModelSBOEnhanced` (`ctlModel=4`) path: `executeOperate` validates ctlNum consistency between `SelectWithValue` and `Operate`, and `executeCancel` clears both `selectOwner` and `selectCtlNum`.

**Added**: General interrogation — `Client.TriggerGI` sends an `RptEna`+`GI` write sequence; the report engine emits a GI report carrying all dataset members with `ReasonGeneralInterrogation` set in the inclusion bits.

**Fixed**: Report engine multi-member dchg — writes to multiple dataset members in a single MMS Write now correctly produce one report containing all changed members rather than emitting separate per-member reports or silently dropping later changes.

**Added**: Server `SetVariableRead(domain, itemID, fn)` — overrides the MMS read function for an already-registered variable at runtime. Used internally by `RegisterControl` for SBO normal; also available to applications that need custom read-time behaviour.

**Changed**: `internal/servermodel.Register` now includes RP/BR functional-constraint groups in the LN `TypeSpec` so that external tools (e.g. `iec61850bean`) can discover URCBs via `GetVariableAccessAttributes`.

**Fixed**: `buildRCBFields` stores `DatSet` as a fully-qualified `domain/LNName$dsName` reference so that `iec61850bean`'s `processReport` can resolve the dataset from its `ServerModel`.

**Fixed**: SCL `ctlModel` DAI values are now emitted as enum name strings (`direct-with-normal-security`, `sbo-with-normal-security`, etc.) rather than ordinals, for compatibility with `iec61850bean`'s SCL parser.

**Changed**: Examples directory renamed from `_examples/` to `examples/`.

**Added**: `interop/` test suite — consumed via `-tags=interop`; exercises `go-iec61850` against live `libiec61850` and `iec61850bean` adapters from `mms-interop`. Covers: basic operations, URCB reporting (dchg, GI, multi-member), dataset depth, negative/error cases, direct control, SBO normal, and SBO enhanced (SBOw). See `INTEROP.md` for the full compatibility matrix.

**Added**: `.github/workflows/interop.yml` — CI workflow that runs the full interop suite against pinned `mms-interop` adapter images.

---

## v0.1.3

**Changed**: CI/release pipeline, SCL tooling, and documentation overhaul.

- Add GitHub Actions CI (`ci.yml`) and release (`release.yml`) workflows for package and binary releases.
- `sclgen` and `sclparse` binaries now embed full build metadata via ldflags (`version`, `tag`, `commit`, `buildDate`) and expose it through a dedicated `version` subcommand.
- Makefile build targets derive version from git tags instead of `version.txt` files.
- SCL model: add topology `LNode` references on `Substation`, `VoltageLevel`, and `Bay`; lossless `Private.InnerXML` capture.
- SCL validation: add topology LNode resolution, GOOSE/SMV `cbName` linkage checks; deprecate `scl.Validate()` in favour of `scl/validate.All()`.
- SCL CLI: add `list-goose`, `list-smv`, `list-connected-ap`, `list-types`, and `inspect` commands; deprecate `ParseWithOptions`/`ParseFileWithOptions`.
- Add content-based `DetectKind` for document classification.
- New `API.md` (merged from `ERRORS.md`); updated `KNOWN_LIMITATIONS.md`, `OBSERVABILITY.md`, `interop/README.md`.

---

## v0.1.2

**Changed**: Increase go-mms dependency to v0.1.4 and upstep ci and release flows (both package and binary)

---

## v0.1.1

**Fixed**: Pump go-mms v0.1.3 (Race condition in Client.Close / conclude handshake)

- When the reader loop received a ConcludeResponse, it signaled concludeCh and then returned (closing readerDone). Because Go's select picks randomly among ready cases, conclude() could non-deterministically hit the readerDone case and return a spurious "connection closed before conclude response" error. The fix drains concludeCh when readerDone fires before reporting failure.

---

## v0.1.0

**Changed**: N/A

- Initial release.

---
