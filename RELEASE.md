# go-iec61850 Releases

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
