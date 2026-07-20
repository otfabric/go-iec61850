# go-iec61850 — Interoperability Completion Roadmap

This file is the master tracking document for the OTFabric IEC 61850 MMS stack.
It supersedes all prior PLAN.md content.

---

## End goal

A production-grade Go IEC 61850 MMS stack that can claim:

> The OTFabric IEC 61850 MMS client and server have been exercised bidirectionally
> against two independently implemented IEC 61850 stacks, including normal operation,
> reporting, control, lifecycle, malformed input, concurrency, recovery, and
> sustained-load scenarios.

This is an interoperability claim, not a formal UCA conformance-certification claim.

---

## Current baseline (v1.0.0 — as of 2026-07-20)

The six interop directions are established:

| # | Direction |
|---|-----------|
| 1 | Go MMS client → libiec61850 MMS server |
| 2 | libiec61850 MMS client → Go MMS server |
| 3 | Go IEC 61850 client → libiec61850 IED server |
| 4 | libiec61850 IED client → Go IEC 61850 server |
| 5 | Go IEC 61850 client → iec61850bean IED server |
| 6 | iec61850bean IED client → Go IEC 61850 server |

**Already done (see INTEROP.md for the full matrix):**

- ✅ Association lifecycle (all 4 IEC directions).
- ✅ LD/LN/DO discovery.
- ✅ ST, MX, CF, DC reads and writes.
- ✅ Dataset listing, member discovery, mixed-FC read, multi-member read.
- ✅ URCB: reserve, enable, dchg, GI, multi-member dchg, disable, reconnect.
- ✅ Direct control (all 4 directions).
- ✅ SBO normal security (all 4 directions).
- ✅ SBO enhanced / SBOw (3 of 4 directions; iec61850bean server gap documented).
- ✅ Compact negative suite (unknown LD/LN/DO, read-only write, invalid dataset, double URCB).
- ✅ CI workflow (`interop.yml`) with digest-pinned adapter images.

**Current adapter images (pinned):**

```
ghcr.io/otfabric/mms-interop-libiec61850@sha256:78bad3f28b6cc7174fe1b0f5b28b22a991f7d908ac234020654918238f3ec977
ghcr.io/otfabric/mms-interop-iec61850bean@sha256:5c88248173264e30540e5f2767c6c22aa83b271f267d1509f1593b6c70c0709f
```

---

## Implementation order

| Milestone | Work | Target |
|-----------|------|--------|
| **M1** (in `go-mms`) | Transport, association, MMS semantics | go-mms v1.1.0 |
| **M2** | Evidence reconciliation + discovery/data-model parity | go-iec61850 v1.1.0-rc.1 |
| **M3** | URCB reporting completion | v1.1.0-rc.2 or v1.2.0-rc.1 |
| **M4** | Controls completion + concurrency | v1.2.0-rc.2 |
| **M5** | Robustness qualification + final release | v1.2.0 |

Phases below map to milestones M2–M5 for this repo.

---

## First mandatory action — Reconcile evidence

**Status:** ⬜ In progress

There is at least one known inconsistency between `mms-interop/COVERAGE.md` and
`go-iec61850/INTEROP.md`. Before implementing new capabilities:

- [ ] Audit `mms-interop/COVERAGE.md` against the v0.1.1 adapter command set.
- [ ] Correct `COVERAGE.md` for `iec61850bean-ied-reporter` (it IS available in v0.1.1).
- [ ] Add harness checks in `interop/harness_test.go` that fail immediately when:
  - Adapter `adapterVersion` is unexpected.
  - A required command is absent from `--capabilities`.
  - Fixture revision is incompatible.
  - A test would skip because of missing infrastructure (no silent skips without a registered limitation).
- [ ] Update `interop.yml` to pin v0.1.1 digests (done: 78bad3f / 5c88248).
- [ ] Update any examples still referencing v0.1.0 digests.

**Exit criterion:** One authoritative capability manifest; no markdown file may contradict it.

---

## Phase C — Discovery and data-model parity (Milestone M2)

### C1. Server-side negative parity ✅ (mostly complete)

Implemented across:
- `libiec61850_negative_test.go` — LibIEC→Go directions
- `iec61850bean_negative_test.go` — Bean→Go directions
- `libiec61850_negative_test.go:TestGoServer_Neg_*` — go server direct tests

Coverage:
- [x] Bean→Go: unknown LD error (`TestBeanClient_Neg_UnknownLD`)
- [x] Bean→Go: unknown LN error (`TestBeanClient_Neg_UnknownLN`)
- [x] Bean→Go: unknown DO error (`TestBeanClient_Neg_UnknownDO`)
- [x] Bean→Go: read-only write rejection (`TestBeanClient_Neg_WriteReadOnly`)
- [x] Bean→Go: invalid dataset reference (`TestBeanClient_Neg_InvalidDataSet`)
- [ ] Bean→Go: double URCB reservation error — **SKIP** (bean client API limitation; skip registered in `COVERAGE.md`)
- [x] LibIEC→Go: unknown LD/LN/DO error
- [x] LibIEC→Go: write read-only attribute
- [x] LibIEC→Go: invalid dataset reference
- [x] LibIEC→Go: double URCB reservation error (`TestLibIECClient_Neg_URCBDoubleReserve`)
- [x] LibIEC→Go: exact wrong-type error (`TestGoServer_Neg_WriteWrongType`)
- [x] Go→Go: all six `TestGoServer_Neg_*` tests

### C2. Functional constraint coverage ⬜

Expand fixture and tests beyond ST/MX/CF/DC:

- [ ] SP (Service Parameter).
- [ ] SV (Substitution Value).
- [ ] CO (Control Object) — already partially covered via controls.
- [ ] RP (Unbuffered Report) — already partially covered via URCB.
- [ ] BR (Buffered Report) — dependent on Phase F decision.
- [ ] EX where represented.
- [ ] SG/SE only if setting-group services are claimed.

Validate per FC: discovery, MMS naming, read/write permissions, attribute omission
from wrong FC, multi-FC objects.

### C3. Structured data / Common Data Classes ✅

Added to fixture (`interop/testdata/interop.icd` and `mms-interop/fixtures/iec61850/interop.icd`):
- [x] SPS — quality, timestamp, stVal (GGIO1.SPS1 — was already present)
- [x] DPS — double-point status (GGIO1.DPS1, `Dbpos` 2-bit BitString) — added
- [x] MV — magnitude, quality, timestamp (MMXU1.TotW — was already present)
- [x] INS — integer status (LLN0.Beh — was already present)
- [x] INC — integer controllable (LLN0.Mod — was already present)
- [x] SPC — already covered (controls)
- [x] BCR — binary counter reading (MMTR1.TotVAh, INT32U) — added
- [ ] DPC — double-point controllable (deferred to v1.1.0)
- [ ] ACT/ACD — action (deferred to v1.1.0)

Tests in `cdc_test.go` (`TestGoServer_CDC_DPS_Read`, `TestGoServer_CDC_BCR_Read`, etc.).
Adapter-direction tests skip until `mms-interop` images are rebuilt with new fixture.

### C4. Quality and timestamp semantics ✅

All tests in `quality_timestamp_test.go`:

- [x] Quality validity=good (default, all bits zero) — `TestGoServer_Quality_Good`
- [x] Quality validity=invalid — `TestGoServer_Quality_Invalid`
- [x] Quality validity=questionable — `TestGoServer_Quality_Questionable`
- [x] Quality detail bits (source=substituted, test, operatorBlocked) — `TestGoServer_Quality_DetailBits`
- [x] UTC timestamp round-trip with millisecond precision — `TestGoServer_Timestamp_Round_Trip`
- [x] UTC timestamp quality flags (LeapSecondsKnown, ClockNotSynchronized) — `TestGoServer_Timestamp_QualityFlags`
- [x] UTC timestamp fractional-second accuracy across 0/100/250/999 ms — `TestGoServer_Timestamp_Accuracy`

### C5. SCL/model agreement ⬜

- [ ] SCL-generated server model matches runtime MMS discovery (all 4 directions).
- [ ] Canonical object references map to correct MMS names.
- [ ] Dataset member order is stable across restarts and reconnects.
- [ ] FCDA references resolved correctly.
- [ ] Missing/invalid SCL references fail at model-load time (not at runtime).
- [ ] Duplicate LDs, LNs, datasets, RCBs, controls rejected at load time.
- [ ] Runtime values type-checked against SCL model before server starts.

**Phase C exit criterion:** The same canonical model is discovered consistently by
Go client, libiec61850 client, and Bean client, with equivalent object references,
types, permissions, and dataset ordering.

---

## Phase D — Dataset interoperability completion (Milestone M2)

### D1. Static dataset parity ✅ (substantially covered)

Coverage across `dataset_depth_test.go`, `*_client_test.go`, `*_server_test.go`:

- [x] Dataset listing (`TestBeanClient_ListDataSets`, `TestLibIECClient_ListDataSets`)
- [x] Ordered member discovery (`TestLibIECClient_DS_MemberDiscovery`, `TestBeanClient_DS_MemberDiscovery`)
- [x] Mixed FC members (`TestLibIECClient_DS_ReadMixedFC`, `TestBeanClient_DS_ReadMixedFC`)
- [x] Multi-member reads — all 4 directions (`TestBeanClient_ReadDataSet`, `TestLibIECClient_ReadDataSet`, `TestBeanServer_ReadDataSet`, `TestLibIECServer_ReadDataSet`)
- [x] Multi-member writes (`TestLibIECServer_DS_MultiWrite`)
- [x] GI report with dataset members (`TestLibIECServer_DS_GIReport`)

Remaining:
- [x] Partial failure in a multi-member operation — `TestGoServer_DS_WritePartialFail`
- [x] Nested structured / array members — `TestGoServer_DS_NestedDORead`
- [ ] Bean→Go: multi-member write (requires adapter write-dataset capability; deferred to v1.1.0)

### D2. Dataset error behavior ⬜

- [ ] Unknown dataset.
- [ ] Dataset belonging to another LD.
- [ ] Dataset containing an invalid member.
- [ ] Read with one inaccessible member.
- [ ] Write with too few / too many values.
- [ ] Write with wrong member type.
- [ ] Write with read-only member.
- [ ] Document and test atomicity policy for multi-member write.

### D3. Dynamic datasets ⬜

**Decision required:** Include or explicitly exclude from v1.1.0 profile.

If included:
- [ ] Client: define NVL, read it, delete it (against libiec61850 and bean).
- [ ] Server: association-specific dynamic datasets with cleanup on close.
- [ ] Server: name collision handling.
- [ ] Server: configurable maximum number and members.
- [ ] Server: access-control hooks.
- [ ] Server: no persistence across restart unless configured.

If excluded:
- [ ] Unsupported requests receive a protocol-correct `ServiceError`.
- [ ] README states explicitly that dynamic datasets are not supported.

**Phase D exit criterion:** Static datasets have complete four-direction coverage.
Dynamic dataset support is either completed or excluded with protocol-correct rejection.

---

## Phase E — URCB reporting completion (Milestone M3 — highest priority)

### E1. Integrity reporting ✅ (engine done, interop tests in progress)

**Engine implemented** in `report_engine.go`:
- `IntgPd` millisecond timer via `integrityLoop` goroutine.
- All dataset members included with `ReasonIntegrity`.
- Stops on disable and server shutdown.
- Coexists with dchg triggers.

**Interop tests** (being added to `interop/urcb_integrity_test.go`):
- [x] `TestGoServer_URCB_IntegrityPeriod` (go→go)
- [x] `TestGoServer_URCB_IntegrityCoexistsWithDchg` (go→go)
- [x] `TestGoServer_URCB_IntegrityDisableStopsTimer` (go→go)
- [x] `TestLibIECServer_URCB_IntegrityPeriod` (go→libiec)

Remaining:
- [ ] `TestBeanServer_URCB_IntegrityPeriod` if bean server supports IntgPd.

### E2. Multi-member report parity ⬜

- [x] One changed member (all 4 directions ✅).
- [x] Multiple changed members in one report (libIEC→Go ✅).
- [ ] Multiple writes producing separate reports.
- [ ] Multiple writes coalesced by BufTm.
- [ ] Correct inclusion-bit ordering (4 directions).
- [ ] Correct reason-for-inclusion per member (4 directions).
- [ ] Mixed reason values in one report.

### E3. Trigger options ✅ (engine done, unit tests being added)

**Engine implemented** — `TrgOpDataChanged`, `TrgOpQualityChanged`, `TrgOpDataUpdate`, `TrgOpIntegrity`, `TrgOpGI` all handled in `report_engine.go`.

**Tests being added** to `interop/urcb_trigger_test.go`:
- [x] dchg — covered by existing tests.
- [x] `TestGoServer_URCB_TrgOp_DataUpdate` (dupd on every write).
- [x] `TestGoServer_URCB_TrgOp_QualityChange` (qchg on quality change).
- [x] `TestGoServer_URCB_TrgOps_NoDchgIfSameValue` (negative: same value no trigger).
- [x] GI — covered by existing tests.

### E4. Optional fields ⬜

Exercise report optional fields individually and in representative combinations:
- [x] SeqNum — ✅ covered.
- [x] Report timestamp — ✅ covered.
- [ ] Dataset name in report.
- [ ] Data reference field.
- [ ] Buffer overflow flag.
- [ ] Entry ID.
- [ ] Configuration revision.
- [ ] Segmentation fields.

Decoder must not depend on a fixed optional-field layout.

### E5. Reservation and ownership ⬜

- [ ] Reserve — ✅ covered.
- [ ] Double reserve by same association.
- [ ] Reserve by second association rejected.
- [ ] Enable without ownership rejected.
- [ ] Disable by non-owner rejected.
- [ ] Disconnect releases reservation.
- [ ] Abort releases reservation.
- [ ] Server restart resets reservation.
- [ ] Reconnect and re-reserve — ✅ covered.
- [ ] Multiple URCBs reserved by one client.
- [ ] Multiple clients reserving different URCBs.
- [ ] Configurable maximum active subscriptions.

### E6. Report backpressure ✅ (implemented and documented)

**Already implemented** in `SubscribeReportOptions`:
- `QueueSize int` — buffer size for the report channel (default 64).
- `OverflowPolicy` — `OverflowDropNewest`, `OverflowDropOldest`, `OverflowBlock`, `OverflowCallback`.
- `OnOverflow func(*ReportIndication)` — called on drop (panic-safe since 2026-07).
- `TestSubscribeReport_OverflowDropOldest`, `TestInfoReportHandlerPanicDoesNotKillClient`.

Remaining:
- [ ] Slow-consumer test: handler blocks 1s, verify confirmed requests still work.
- [ ] Client-closes-inside-callback test.

### E7. Report parser robustness ⬜

Inject:
- [ ] Unknown dataset reference.
- [ ] Dataset member-count mismatch.
- [ ] Inclusion-bit count mismatch.
- [ ] Missing optional fields.
- [ ] Unknown optional fields.
- [ ] Wrong report value type.
- [ ] Truncated access result.
- [ ] Malformed reason-for-inclusion.
- [ ] Unknown RptID.
- [ ] Sequence discontinuity.
- [ ] Duplicate sequence number.

Malformed reports must fail the subscription, not crash the association.

**Phase E exit criterion:** URCB reporting works under normal, concurrent, slow-consumer,
disconnect, malformed-report, GI, integrity, and multi-member conditions, with complete
state cleanup.

---

## Phase F — BRCB scope decision (Milestone M3)

**Decision: Implement BRCB (Option A) — included in v1.0.0.**

**v1.0.0 BRCB scope — COMPLETE:**
- [x] Discovery (BR FC group — registered).
- [x] Server-side buffer (`bufferedEntry`, `entryID`, `bufMax=1000`).
- [x] `PurgeBuf` handling.
- [x] `brcb01` added to interop fixture ICD.
- [x] `TestGoServer_BRCB_EntryID`: GI triggers non-empty EntryID.
- [x] `TestGoServer_BRCB_Replay`: disable BRCB, re-enable → buffered entries replayed.
- [x] `TestGoServer_BRCB_Overflow`: fill past `bufMax=2` → `BufOvfl` flag set.
- [x] `TestGoServer_BRCB_Purge`: PurgeBuf clears queue → no replay on re-enable.

**v1.1.0 (deferred):**
- `TestLibIECServer_BRCB_BasicReplay`: adapter test (requires mms-interop rebuild).
- Persistent storage interface.
- Per-BRCB configurable limits.
- Clock-change behavior.

---

## Phase G — Control state-machine completion (Milestone M4)

### G1. Direct control ✅ (basic coverage)

- [x] Direct normal security (`TestLibIECServer_Control_DirectOperate`, `TestBeanServer_Control_DirectOperate`)
- [x] Operate success — `stVal` written on operate
- [ ] Direct enhanced security, if supported
- [ ] AddCause propagation on rejection
- [ ] Interlock-check / synchronism-check flags

### G2. SBO normal security ✅ (complete)

All tests in `sbo_state_test.go`:

- [x] SBO select and operate
- [x] Select timeout (configurable via `ControlHandler.SelectTimeout`) → `TestGoServer_SBO_SelectTimeout`
- [x] Cancel by owner → `TestGoServer_SBO_CancelByOwner`
- [x] Cancel by non-owner rejected → `TestGoServer_SBO_CancelByNonOwner`
- [x] Second-client contention rejected → `TestGoServer_SBO_SecondClientContention`
- [x] Disconnect releases selection → `TestGoServer_SBO_DisconnectReleasesSelection`
- [x] Repeated select by same client (idempotent) → `TestGoServer_SBO_RepeatedSelectSameClient`
- [x] Operate without select rejected → `TestGoServer_SBO_OperateWithoutSelect`

### G3. SBOw enhanced security ✅ (complete)

All tests in `sbo_state_test.go` and `sbow_test.go`:

- [x] SelectWithValue + Operate
- [x] Operate-without-select rejected → `TestGoServer_SBOw_OperateWithoutSelect`
- [x] Cancel clears selection → `TestGoServer_SBOw_CancelClearsSelect`
- [x] SelectWithValue value must match Operate value → `TestGoServer_SBOw_ValueMustMatchOperate`
- [x] ctlNum must match across Select/Operate
- [x] Contention (second client) rejected → `TestGoServer_SBOw_SecondClientContention`
- [x] Disconnect/re-select after disconnect → `TestGoServer_SBOw_DisconnectReleasesSelection`

**Implementation notes:**
- `controlRegistration` tracks `selectCtlVal *mms.Value` for SBOw ctlVal matching
- `selectConn *mms.ServerConn` used for connection-scoped contention (both SBO and SBOw)
- `isConnActive` checks if selection owner is still connected (lazy cleanup on select/operate)

### G4. Upstream limitation register ✅

Current known limitations (registered in `mms-interop/COVERAGE.md`):
- iec61850bean server, go-client direction, SBOw `SelectWithValue` — documented and skipped.

---

## Phase H — Multi-client and concurrency validation (Milestone M4)

### H1. Server concurrency ✅ (covered)

All tests in `multi_client_test.go`:

- [x] Concurrent reads (5 clients) → `TestGoServer_MultiClient_ConcurrentReads`
- [x] Concurrent writes to independent values → `TestGoServer_MultiClient_ConcurrentWrites_IndependentVars`
- [x] Invoke-ID isolation per connection → `TestGoServer_MultiClient_InvokeIDIsolation`
- [x] Disconnect one client leaves others unaffected → `TestGoServer_MultiClient_DisconnectOneKeepsOthers`

Remaining (deferred to v1.1.0):
- [ ] 10, 50 simultaneous associations stress test
- [ ] Concurrent writes to the same value (last-writer-wins policy test)
- [ ] Concurrent report subscriptions.
- [ ] Concurrent controls on independent objects.
- [ ] Contention on the same control.
- [ ] One malformed client alongside healthy clients.
- [ ] One slow client alongside healthy clients.
- [ ] One client disconnecting repeatedly.

### H2. Isolation ⬜

- [ ] Association-specific dynamic datasets are isolated.
- [ ] URCB ownership is isolated per connection.
- [ ] Control selection is isolated per connection.
- [ ] Invoke IDs are isolated.
- [ ] Decode errors on one connection do not affect another.
- [ ] Report backpressure on one connection does not block another.
- [ ] Closing one client does not mutate another client's state.

### H3. Thread safety ⬜

- [ ] `go test -race ./...` clean.
- [ ] `go test -race -tags=interop ./interop/...` clean.
- [ ] Interop tests include repeated and parallel modes where adapter permits.

**Phase H exit criterion:** No races, cross-association contamination, global locks causing
pathological blocking, or server-wide failure caused by one client.

---

## Phase I — Malformed traffic and protocol hardening (Milestone M5)

### I1. Corpus creation ⬜

- [ ] Record representative PCAPs for: association, discovery, read/write, dataset, URCB, control.
- [ ] Turn them into a decoder regression corpus and mutation corpus.

### I2. Fuzzing ✅ (comprehensive coverage already in place)

**Already implemented** — 25+ `FuzzXxx` targets across `go-mms` and `go-iec61850`:

`go-mms`:
- `FuzzACSEParse`, `FuzzDecodeTLV`, `FuzzDecodeLength`, `FuzzDecodeInteger`, `FuzzDecodeUnsigned`
- `FuzzDecodeTypeSpec`, `FuzzDecodeObjectName`, `FuzzUnmarshalDataElement`, `FuzzUnmarshalAccessResults`
- `FuzzDecodePdu`, `FuzzDecodeConfirmedError`, `FuzzDecodeRejectPDU`, `FuzzDecodeConfirmedResponse`
- `FuzzUnmarshalReadResponse`, `FuzzUnmarshalWriteResponse`, `FuzzUnmarshalGetNameListResponse`
- `FuzzUnmarshalInformationReport`, `FuzzPresentationParse`, `FuzzSessionParse`, and more.

`go-iec61850`:
- `FuzzParseRef`, `FuzzParseFC`, `FuzzDecodeQuality`, `FuzzNewValue`, `FuzzParse` (SCL).

Remaining:
- [ ] COTP/TPKT decoder fuzz (`go-cotp`).
- [ ] Report parser fuzz (`FuzzDecodeReport` in go-iec61850).
- [ ] Stateful mutation proxy (Phase I3) — mms-interop infrastructure.
### I2. Fuzzing ⬜

Fuzz targets:
- [ ] TPKT decoder.
- [ ] COTP decoder.
- [ ] MMS BER decoder.
- [ ] Object-reference parser.
- [ ] Report parser.
- [ ] SCL parser.
- [ ] Value conversion.

Assertions: no panic, no unbounded allocation, no infinite loop, no goroutine leak.

### I3. Stateful mutation proxy ⬜

Use `mms-interop-proxy` (see `mms-interop/PLAN.md` M3.1) with live sessions:
- [ ] Packet splitting and joining.
- [ ] Truncation and duplication.
- [ ] Delay and connection close at defined state transitions.
- [ ] BER tag/length mutation.
- [ ] Selective response dropping.

### I4. Resource limits ⬜

Enforce configurable limits for:
- [ ] PDU size.
- [ ] BER nesting depth.
- [ ] Structure and array element count.
- [ ] String and octet-string length.
- [ ] Dataset members.
- [ ] Outstanding requests.
- [ ] Active associations.
- [ ] Active subscriptions.
- [ ] Pending reports.
- [ ] Control selections.

**Phase I exit criterion:** Malformed or excessive input produces bounded, deterministic
errors without panic, memory explosion, deadlock, or corruption of subsequent sessions.

---

## Phase J — Recovery and endurance (Milestone M5)

### J1. Reconnection matrix ⬜

For each major operation, disconnect:
- [ ] Before request write.
- [ ] Mid-request.
- [ ] After request write but before response.
- [ ] Mid-response.
- [ ] During report delivery.
- [ ] During selection.
- [ ] During operate.
- [ ] During server shutdown.

Verify: deterministic error to caller, no blocked request, resources released, new
association succeeds, no stale subscription/reservation/selection.

### J2. Long-running tests ⬜

- [ ] 24-hour baseline (repeated association, reads, writes, reports, controls, GI).
- [ ] 72-hour release-candidate test.
- [ ] Track: memory, goroutines, file descriptors, CPU, latency percentiles, reconnect success.

### J3. Network impairment ⬜

Test with:
- [ ] Latency and jitter.
- [ ] Packet loss simulated below TCP.
- [ ] Very small TCP buffers.
- [ ] Slow reader / slow writer.
- [ ] Half-open connection behavior (NAT-like idle timeout).

**Phase J exit criterion:** No monotonic resource growth, unrecoverable state, silent
data loss, or degradation under repeated reconnect cycles.

---

## Phase K — API production hardening (Milestone M5)

### K1. Context and deadlines ⬜

- [ ] Every blocking client operation respects `context.Context`.
- [ ] Cancellation propagates without corrupting the association.
- [ ] Document per-operation timeout policy.

### K2. Error API ✅ (mostly done)

`go-iec61850` error taxonomy:
- `ErrInvalidArgument`, `ErrSelectFailed`, `ErrOperateFailed`, `ErrNotControllable`
- `ControlError` with `Ref`, `Operation`, `AddCause`, `Wrapped` fields
- `DataAccessError` with `Ref`, `ErrorCode`, `Operation`
- `ReportError` with `RCBRef`, `Message`

`go-mms` error taxonomy: See `go-mms/PLAN.md` Phase B4.

Remaining:
- [ ] Verify `errors.Is` / `errors.As` chain is consistent across all error types.

### K3. Observability ⬜

Optional hooks for:
- [ ] Association opened/closed.
- [ ] Request latency.
- [ ] Report received/dropped.
- [ ] Control operation result.
- [ ] Active subscriptions and associations (counters).

No forced logging framework — prefer interfaces or callbacks.

### K4. Server application interfaces ✅ (panic containment done)

- [x] `ControlHandler.OnOperate`, `OnSelect`, `OnCancel` — panic recovery via `callControlHandler`.
- [x] `OnOverflow` callback — panic recovery in `deliver()`.
- [x] Configurable `SelectTimeout` per `ControlHandler`.
- [ ] `ReadFunc` / `WriteFunc` — panic recovery not yet added.
- [ ] Document cancellation propagation policy.

### K5. Compatibility commitment ⬜

- [ ] Freeze public API.
- [ ] Document semantic-versioning policy.
- [ ] Mark experimental APIs explicitly.
- [ ] Add migration documentation from v1.0.1.
- [ ] Verify all examples compile.
- [ ] Add complete client and server examples.
- [ ] Document supported and unsupported IEC 61850 services.

**Phase K exit criterion:** Applications can safely integrate without relying on
undocumented timing, internal errors, global state, or fragile callback behavior.

---

## Phase L — CI and release evidence (Milestone M5)

### L1. CI tiers

| Tier | Trigger | Tests | Status |
|------|---------|-------|--------|
| 1 | Every PR | Unit tests (3 Go versions), race detector, vet, staticcheck, golangci-lint | ✅ (`ci.yml` → `go-ci.yml@v2`) |
| 2 | Every PR | Full 4-direction IEC 61850 interop matrix against digest-pinned images | ✅ (`interop.yml` → `make interop`) |
| 3 | Nightly | Extended fuzzing, concurrency stress, leak checks | ⬜ (planned) |
| 4 | Release candidate | Full matrix, endurance, platform matrix, clean-room reproduction | ⬜ (planned) |

Image digests pinned in `interop.yml`:
- `mms-interop-libiec61850@sha256:78bad3f...`
- `mms-interop-iec61850bean@sha256:5c88248...`

### L2. Platform coverage

- [x] Linux amd64 (CI runs ubuntu-latest).
- [ ] Linux arm64.
- [ ] macOS amd64/arm64 (unit tests, client functionality).
- [ ] Windows amd64 (unit tests, client; server Linux-only is acceptable if documented).

### L3. Reproducibility ✅ (partially done)

- [x] Adapter image digests pinned in `interop.yml`.
- [x] Go toolchain version pinned (`go-versions: ["1.24", "1.25", "1.26"]`).
- [x] Fixture revision (`iec61850-v1` / `iec61850-v2`) validated in interop harness.
- [ ] Archive per release: test result JSON, adapter logs, capability manifest.
- [ ] PCAP capture for failed tests.

### L4. Flake policy ✅ (partially enforced)

- [x] Failed interop test is not retried silently.
- [x] Test timeouts are explicit (`timeout-minutes: 45` in `interop.yml`).
- [ ] Timing margins centralized.
- [ ] All sleeps replaced by readiness/state events.

**Phase L status:** Tiers 1 and 2 complete. Tiers 3 and 4, arm64/macOS/Windows, and
archive artefacts deferred to v1.1.0.

---

## Final production release gates

### Functional gate

- All claimed services have positive tests.
- All meaningful service errors have negative tests.
- Every claimed client capability tested against at least one independent server.
- Every claimed server capability tested against at least one independent client.
- Core capabilities tested against both libiec61850 and iec61850bean wherever their APIs support it.
- Every unsupported external direction has a documented upstream limitation.

### Lifecycle gate

- Clean close, abort, timeout, remote reset, reconnect, server restart, adapter restart.
- Pending-request cancellation.
- Subscription, reservation, and control-selection cleanup.

### Concurrency gate

- Multiple requests, multiple clients, multiple subscriptions, contending controls.
- Race detector clean.
- No cross-association state contamination.

### Robustness gate

- Mutation/fuzz corpus clean.
- Resource limits enforced.
- No known panic from network input.
- No known deadlock.
- No goroutine, socket, timer, or memory leak.
- Sustained operation completed successfully.

### Evidence gate

- Adapter images digest-pinned.
- Third-party versions and checksums recorded.
- Fixture revision recorded.
- Full matrix generated from test results.
- Release test artifacts retained.
- CI reproduces result from release tags.
- No unexplained skips.
- No flaky release-blocking tests.

### Documentation gate

- Supported service profile stated.
- Unsupported services stated.
- Known upstream limitations listed.
- Timeout and concurrency semantics documented.
- Reporting backpressure policy documented.
- BRCB persistence policy documented (or "unbuffered only" named).
- Dynamic dataset lifecycle documented.
- Server callback guarantees documented.
- Error taxonomy documented.
- Security considerations stated.
- Production deployment example present.
- Upgrade notes from v1.0.1.

---

## Final release recommendation

When all gates pass, cut coordinated releases:

| Package | Release |
|---------|---------|
| `mms-interop` | v1.0.0 |
| `go-cotp` | v1.x |
| `go-mms` | v1.x |
| `go-iec61850` | v1.x |

The final `go-iec61850` release notes must state precisely:
1. Which IEC 61850 services are supported.
2. Which services were verified against libiec61850 and iec61850bean.
3. Which directions were tested and which upstream limitations prevented a direction.
4. Whether BRCB is included.
5. Whether dynamic datasets are included.
6. Which editions/profile assumptions apply.
7. That formal UCA conformance certification is separate from this interoperability testing.

---

## Definition of "interop roadmap closed"

The roadmap is closed only when:

- The six primary directions remain passing.
- All claimed reporting and control state machines are exercised beyond happy paths.
- All server-side ownership and multi-client behavior is proven.
- Recovery is proven at every protocol boundary.
- External stacks cannot crash, hang, leak, or poison the Go stack through malformed traffic.
- The exact test evidence is reproducible from public releases.
- Remaining exclusions are conscious product-scope decisions, not unimplemented ambiguity.
