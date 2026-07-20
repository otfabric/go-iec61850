# go-iec61850 Interoperability

`go-iec61850` decides which capabilities need verification. Tests in `interop/` consume the adapter images published by [mms-interop](https://github.com/otfabric/mms-interop) and assert `go-iec61850` behaviour against live, independent implementations.

New adapter commands or fixture capabilities are requested from `mms-interop` only when the required external operation does not yet exist there.

## Architecture

```
mms-interop
  libiec61850 adapter images
  iec61850bean adapter images
         |
    go-iec61850/interop/
      harness_test.go                lifecycle helpers, fixture loading
      libiec61850_client_test.go     TestLibIECClient_*
      libiec61850_server_test.go     TestLibIECServer_*
      iec61850bean_client_test.go    TestBeanClient_*
      iec61850bean_server_test.go    TestBeanServer_*
      libiec61850_urcb_client_test.go
      libiec61850_urcb_server_test.go
      libiec61850_negative_test.go   TestLibIECClient_Neg_* / TestGoServer_Neg_*
      iec61850bean_negative_test.go  TestBeanClient_Neg_*
      dataset_depth_test.go          TestLibIECClient_DS_* / TestBeanClient_DS_* / TestLibIECServer_DS_*
      control_test.go                TestLibIECClient_Control_* etc.
      sbo_test.go                    TestLibIEC*_Control_SBO* / TestBean*_Control_SBO* / TestGoServer_SBO_*
      sbow_test.go                   TestLibIEC*_Control_SBOw* / TestBean*_Control_SBOw* / TestGoServer_SBOw_*
      report_semantics_test.go       TestLibIEC*_URCB_GI* / *MultiMember*
```

Each test:
1. Starts the adapter container with `docker run`.
2. Waits for the readiness event (`{"event":"ready",...}`) on stdout.
3. Exercises the `go-iec61850` API under test.
4. Asserts results.
5. Tears the container down.

No pre-running containers. No manual steps. Tests are gated behind `-tags=interop`.

## Running

```bash
# Build adapter images locally (in mms-interop)
cd ../mms-interop && make build

# Run against local images
LIBIEC61850_IMAGE=mms-interop-libiec61850:local \
IEC61850BEAN_IMAGE=mms-interop-iec61850bean:local \
make interop

# Run against published images (version tags)
LIBIEC61850_IMAGE=ghcr.io/otfabric/mms-interop-libiec61850:v0.1.0 \
IEC61850BEAN_IMAGE=ghcr.io/otfabric/mms-interop-iec61850bean:v0.1.0 \
make interop

# CI — digest pinned
LIBIEC61850_IMAGE=ghcr.io/otfabric/mms-interop-libiec61850@sha256:<digest> \
IEC61850BEAN_IMAGE=ghcr.io/otfabric/mms-interop-iec61850bean@sha256:<digest> \
make interop
```

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LIBIEC61850_IMAGE` | libiec61850 adapter image | `mms-interop-libiec61850:local` |
| `IEC61850BEAN_IMAGE` | iec61850bean adapter image | `mms-interop-iec61850bean:local` |
| `IEC61850_FIXTURE_DIR` | Path to `fixtures/iec61850/` from mms-interop | `testdata/` |

## Test naming

| Prefix | Go role | Adapter counterpart |
|--------|---------|---------------------|
| `TestLibIECClient_` | IEC 61850 client | `libiec61850-ied-server` |
| `TestLibIECServer_` | IEC 61850 server | `libiec61850-ied-client` |
| `TestBeanClient_` | IEC 61850 client | `iec61850bean-ied-server` |
| `TestBeanServer_` | IEC 61850 server | `iec61850bean-ied-client` |

Sub-topics are appended after a second underscore: `TestLibIECClient_URCB_DataChange`.

---

## IEC 61850 compatibility matrix

**Key:** ✓ covered · some partial · — not yet tested · n/a not applicable

### Basic operations

| Capability | Go→libIEC | libIEC→Go | Go→Bean | Bean→Go |
|-----------|:---:|:---:|:---:|:---:|
| Associate / conclude | ✓ | ✓ | ✓ | ✓ |
| Reconnect after close | ✓ | ✓ | ✓ | ✓ |
| Logical device discovery | ✓ | ✓ | ✓ | ✓ |
| Logical node discovery | ✓ | ✓ | ✓ | ✓ |
| Data-object discovery | ✓ | ✓ | ✓ | ✓ |
| ST read | ✓ | ✓ | ✓ | ✓ |
| MX read | ✓ | ✓ | ✓ | ✓ |
| CF read | ✓ | ✓ | ✓ | ✓ |
| DC read | ✓ | ✓ | ✓ | ✓ |
| Basic write / read-back | ✓ | ✓ | ✓ | ✓ |
| Dataset listing | ✓ | ✓ | ✓ | ✓ |
| Dataset read | ✓ | ✓ | ✓ | ✓ |
| Wrong-type rejection | ✓ | some | ✓ | some |
| Unknown LD error | ✓ | ✓ | ✓ | — |
| Unknown LN error | ✓ | ✓ | ✓ | — |
| Unknown DO error | ✓ | — | ✓ | — |
| Write read-only error | ✓ | ✓ | ✓ | — |
| Invalid dataset reference | ✓ | ✓ | ✓ | — |
| Double-reserve URCB error | ✓ | — | ✓ | — |

### Dataset depth

| Capability | Go→libIEC | libIEC→Go | Go→Bean | Bean→Go |
|-----------|:---:|:---:|:---:|:---:|
| Member discovery (ordered refs) | ✓ | — | ✓ | — |
| Mixed FC members (ST + MX) | ✓ | — | ✓ | — |
| Multi-member dataset read | ✓ | ✓ | ✓ | — |
| GI report (all members, ordered) | ✓ | ✓ | ✓ | — |
| Multi-write → multi-member dchg | — | ✓ | — | — |

### URCB reporting

| Capability | Go→libIEC | libIEC→Go | Go→Bean | Bean→Go |
|-----------|:---:|:---:|:---:|:---:|
| URCB discovery | ✓ | ✓ | ✓ | ✓ |
| RCB attribute read | ✓ | ✓ | ✓ | ✓ |
| Reserve URCB | ✓ | ✓ | ✓ | ✓ |
| Enable reports | ✓ | ✓ | ✓ | ✓ |
| dchg report triggered | ✓ | ✓ | ✓ | ✓ |
| Report RptID | ✓ | ✓ | ✓ | ✓ |
| Report SeqNum | ✓ | ✓ | ✓ | ✓ |
| Report inclusion bits | ✓ | ✓ | ✓ | ✓ |
| Report value | ✓ | ✓ | ✓ | ✓ |
| Reason-for-inclusion (DataChanged) | ✓ | ✓ | ✓ | ✓ |
| Disable reports | ✓ | ✓ | ✓ | ✓ |
| Reconnect and re-reserve | ✓ | ✓ | ✓ | — |
| Same-value write → no report | ✓ | ✓ | — | — |
| Rejected write → no spurious report | ✓ | ✓ | — | — |
| General interrogation | ✓ | ✓ | ✓ | — |
| Integrity period | — | — | — | — |
| Multi-member report | — | ✓ | — | — |
| Buffered reporting (BRCB) | — | — | — | — |

### Controls

| Capability | Go→libIEC | libIEC→Go | Go→Bean | Bean→Go |
|-----------|:---:|:---:|:---:|:---:|
| Direct control (Oper) | ✓ | ✓ | ✓ | ✓ |
| Select-before-operate (SBO normal) | ✓ | ✓ | ✓ | ✓ |
| SBO — operate without select rejected | n/a | n/a | n/a | ✓ |
| SBO enhanced (SBOw) | ✓ | ✓ | ✓ | n/a¹ |
| SBOw — operate without select rejected | n/a | n/a | n/a | ✓ |
| SBOw — cancel clears select | n/a | n/a | n/a | ✓ |
| Cancel | ✓ | ✓ | — | — |

**Notes:**
- `some` indicates the error is caught but the specific error category is not yet asserted for all directions.
- BRCB, GOOSE, and Sampled Values are out of scope for the near term.
- `TestBeanClient_*` (go→bean) tests use `client.Abort` for teardown because iec61850bean's server does not respond to MMS Conclude requests.
- ¹ `TestBeanClient_Control_SBOwOperate` is skipped: `iec61850bean` server does not expose `SBOw[CO]` as a writable MMS attribute; direct SBOw select is a known gap in that stack.

---

## IEC 61850 edition service profile

`go-iec61850` targets **IEC 61850 Edition 2** (2007B) as its primary conformance baseline. Edition 2.1 (2007B4) features are not guaranteed unless listed below.

### Protocol profile

| Layer | Implementation | Status |
|-------|---------------|--------|
| MMS (ISO 9506) | `go-mms` | ✓ |
| COTP / ISO 8073 Class 0 | `go-cotp` | ✓ |
| TPKT (RFC 1006) | `go-tpkt` | ✓ |
| TCP | `net` stdlib | ✓ |
| TLS (optional) | `crypto/tls` | ✓ |

### IEC 61850-7-2 services

| Service | Client | Server | Notes |
|---------|:------:|:------:|-------|
| Associate | ✓ | ✓ | ACSE/Presentation/Session layers |
| Abort | ✓ | ✓ | |
| Conclude | ✓ | ✓ | |
| GetServerDirectory | ✓ | ✓ | |
| GetLogicalDeviceDirectory | ✓ | ✓ | |
| GetLogicalNodeDirectory | ✓ | ✓ | |
| GetDataDirectory | ✓ | ✓ | |
| GetDataDefinition | ✓ | ✓ | `GetVariableAccessAttributes` |
| GetDataValues | ✓ | ✓ | single + multi-variable |
| SetDataValues | ✓ | ✓ | single + multi-variable |
| GetDataSetValues | ✓ | ✓ | |
| GetDataSetDirectory | ✓ | ✓ | |
| CreateDataSet | — | — | dynamic datasets deferred |
| DeleteDataSet | — | — | |
| GetRCBValues | ✓ | ✓ | URCB and BRCB |
| SetRCBValues | ✓ | ✓ | URCB and BRCB |
| GetSGCBValues | ✓ | ✓ | |
| SetSGValues | ✓ | ✓ | |
| ConfirmEditSGValues | ✓ | ✓ | |
| GetEditSGValues | ✓ | ✓ | |
| QueryLogByTime | ✓ | ✓ | |
| QueryLogAfter | ✓ | ✓ | |
| GetLogStatusValues | ✓ | — | |
| GetFile | ✓ | ✓ | |
| SetFile | — | ✓ | |
| DeleteFile | — | — | |
| GetFileAttributeValues | ✓ | ✓ | |
| SendGSSEMessage | — | — | GOOSE out of scope |
| SendMSVMessage | — | — | Sampled Values out of scope |
| Select | ✓ | ✓ | SBO normal |
| SelectWithValue | ✓ | ✓ | SBOw enhanced |
| Cancel | ✓ | ✓ | |
| Operate | ✓ | ✓ | direct and SBO/SBOw |
| CommandTermination | — | — | |
| TimeActivatedOperate | — | — | |

### Common Data Classes (IEC 61850-7-3)

| CDC | Description | Client-readable | Server-pub | Interop tested |
|-----|-------------|:---------------:|:----------:|:--------------:|
| SPS | Single-point status | ✓ | ✓ | ✓ |
| DPS | Double-point status | ✓ | ✓ | ✓ |
| INS | Integer status | ✓ | ✓ | ✓ |
| ACT | Protection activation | ✓ | ✓ | ✓ |
| SPC | Single-point controllable | ✓ | ✓ | ✓ |
| DPC | Double-point controllable | ✓ | ✓ | ✓ |
| MV  | Measured value | ✓ | ✓ | ✓ |
| CMV | Complex measured value | ✓ | ✓ | — |
| ENS | Enumerated status | ✓ | ✓ | — |
| BCR | Binary counter reading | ✓ | ✓ | ✓ |
| BSC | Binary step controllable | ✓ | ✓ | — |

### Report features

| Feature | URCB | BRCB | Notes |
|---------|:----:|:----:|-------|
| Data-change trigger (dchg) | ✓ | ✓ | |
| Quality-change trigger (qchg) | ✓ | ✓ | |
| Data-update trigger (dupd) | ✓ | ✓ | |
| Integrity scan | ✓ | ✓ | configurable period |
| General interrogation | ✓ | ✓ | |
| OptFlds: SeqNum | ✓ | ✓ | |
| OptFlds: TimeStamp | ✓ | ✓ | |
| OptFlds: ReasonCode | ✓ | ✓ | |
| OptFlds: DataRef | ✓ | ✓ | |
| OptFlds: DataSet | ✓ | ✓ | |
| OptFlds: EntryID | n/a | ✓ | |
| OptFlds: ConfRev | ✓ | ✓ | |
| OptFlds: BufOvfl | n/a | ✓ | |
| EntryID-based resume | n/a | ✓ | client writes EntryID before enable |
| Purge buffer | n/a | ✓ | |
| Replay on re-enable | n/a | ✓ | |
| Overflow policy (DropNewest) | ✓ | ✓ | configurable |
| Connection-scoped reservation | ✓ | n/a | disconnect releases Resv |
| Multi-segment reports | ✓ | ✓ | reassembled automatically |

### Known limitations / deferred features

| Feature | Status | Notes |
|---------|--------|-------|
| GOOSE | not implemented | out of scope |
| Sampled Values | not implemented | out of scope |
| Dynamic dataset creation | not implemented | deferred |
| CommandTermination | not implemented | deferred |
| TimeActivatedOperate | not implemented | deferred |
| iec61850bean SBOw | known gap (upstream) | iec61850bean does not expose `SBOw[CO]` |
| Edition 2.1 extensions | partial | not all 2007B4 features validated |

---

## BRCB EntryID resume — implementation contract

This section documents the precise semantics of EntryID-based resume as implemented. Users relying on this feature should treat this as the authoritative specification rather than assuming universal IEC 61850 behavior.

### What EntryID is

- An EntryID is an 8-byte big-endian `OctetString` that uniquely identifies a buffered report entry on the server.
- The server assigns monotonically increasing 64-bit counters, starting at 1 for the first entry after `EnableReports()` is called.
- ID 0 (`0x0000000000000000`) is reserved as the neutral sentinel meaning "no resume point."

### Exclusive boundary

The supplied EntryID is treated as **exclusive**. A client that writes EntryID `N` before enabling the BRCB receives only entries with ID **strictly greater than** `N` — i.e., entries produced *after* the entry the client last received.

This matches the IEC 61850-7-2 description where the client stores the EntryID of the last received entry.

### Special cases

| Client writes | Server behavior |
|--------------|----------------|
| All-zero bytes (`0x00…00`) | Full buffer replay — no filtering |
| Valid EntryID present in buffer | Replay of entries after that ID |
| EntryID beyond the highest buffered ID | Empty replay — client has already seen everything |
| EntryID from before a `PurgeBuf` | Empty replay — IDs are invalidated by purge |
| Partial write (< 8 bytes) | Treated as zero (no filtering) |

### Buffer overflow interaction

When the server-side BRCB buffer (`bufMax`, default 1000 entries) overflows, the oldest entry is dropped. If a client's resume point was before the dropped entries, the client will miss those intermediate entries. The first replayed or newly delivered report will have `BufOvfl=true` to signal the gap.

### Stability

- EntryIDs are **not persistent** across server process restarts. They reset to 1 on each `EnableReports()` call.
- Clients must not cache EntryIDs across reconnects to a different server process or after a server restart.
- `PurgeBuf=true` invalidates all previously issued EntryIDs. A subsequent enable with any non-zero EntryID will find nothing to replay.

### Byte format

```
Byte 0 (MSB)  Byte 7 (LSB)
[uint64, big-endian]
```

Example: EntryID 1 is `0x0000000000000001`.

---

## Report backpressure and loss policy

This section defines what happens when the application cannot consume reports fast enough.

### Client-side queue (SubscribeReportOptions.QueueSize)

Each subscription has an in-process Go channel with a configurable capacity (default: 64). The `OverflowPolicy` field controls what happens when this channel is full:

| Policy | Behavior | Notes |
|--------|----------|-------|
| `OverflowDropNewest` (default) | Incoming report is discarded | SeqNum gaps indicate loss |
| `OverflowDropOldest` | Oldest buffered report is discarded to make room | Provides fresher data |
| `OverflowBlock` | Dispatcher blocks until channel has space | Zero application-layer loss; may stall all report delivery |
| `OverflowCallback` | `OnOverflow` callback is invoked instead of delivery | Custom loss handling |

**Important:** The server is unaware of client-side queue overflow. No error is returned to the server; no reconnect occurs. The application is responsible for consuming reports promptly or choosing an appropriate policy.

### URCB loss (unbuffered reports)

URCB provides best-effort delivery. Application queue overflow silently discards reports. This is the expected and specified behavior for unbuffered reporting — no gap indicator is sent because the server does not know the client missed a report.

### BRCB server-side buffer overflow

The server-side BRCB buffer (default: 1000 entries, configurable via `SetRCBBufMax`) is separate from the client-side application queue. When the server buffer fills:

1. The oldest entry is dropped (drop-oldest policy).
2. `bufOvfl` is set to `true`.
3. The next delivered report will have `BufOvfl=true` set (when `OptFldBufOvfl` is enabled).
4. The client can detect the gap from `BufOvfl` and decide whether to re-read the dataset via GI.

### Confirmed services vs. report delivery

Report delivery (InformationReports) and confirmed services (Read, Write, SetRCBValues) use the same MMS connection but are dispatched independently. A full application-side report queue does not block confirmed services unless `OverflowBlock` is in use.

---

## Roadmap

Phases are driven by `go-iec61850` implementation priorities. Adapter infrastructure is requested from `mms-interop` only when a required command or fixture capability is absent.

### Phase 2D — URCB parity with iec61850bean ✅

Prove that reporting works across both independent IEC 61850 stacks.

**Direction 1:** go-iec61850 client → iec61850bean IED server ✅

- Subscribe to `urcb01` (ReserveURCB, AutoEnable).
- Write `GGIO1.SPS1.stVal` to trigger dchg.
- Receive and validate: RptID, SeqNum, Inclusion[0]=true/[1]=false, Value, ReasonDataChanged.
- Close subscription; verify RptEna=false.

**Direction 2:** iec61850bean IED client → go-iec61850 server ✅

- iec61850bean client enables URCB, writes to trigger dchg, receives report via Java listener.
- Adapter emits report fields as JSON Lines.
- Go test validates.

**Completed.** Both directions work. Key fixes required:
1. `registerLNHierarchy` now includes RP/BR FC groups in LN TypeSpec so
   `iec61850bean.retrieveModel()` discovers URCBs via `GetVariableAccessAttributes`.
2. `buildRCBFields` now stores DatSet as fully-qualified `domain/LNName$dsName`
   so `iec61850bean.processReport` can look up the DataSet from its `ServerModel`.
3. SCL `ctlModel` DAI values use enum name strings (`direct-with-normal-security`)
   rather than ordinals for compatibility with `iec61850bean` SCL parser.

Scope: one URCB, one dataset, dchg, one changed member.

**Adapter:** `iec61850bean-ied-reporter` command implemented in mms-interop.

Test names: `TestBeanClient_URCB_*`, `TestBeanServer_URCB_*`.

---

### Phase 2E — direct control ✅

The ICD already contains `GGIO1.SPCSO1` (direct-control, `ctlModel=1`). No fixture change required.

Four directions, all passing:

| Test | Direction |
|------|-----------|
| `TestLibIECServer_Control_DirectOperate` | libiec61850 client → go server |
| `TestLibIECClient_Control_DirectOperate` | go client → libiec61850 server |
| `TestBeanServer_Control_DirectOperate`   | iec61850bean client → go server |
| `TestBeanClient_Control_DirectOperate`   | go client → iec61850bean server |

---

### Phase 2F — compact negative suite ✅

One focused negative test per error category. Tests for Go→libIEC and Go→Bean directions use the shared adapter servers; Go-server-side (libIEC→Go, bean→Go) tests use `TestGoServer_Neg_*`.

| Scenario | Test(s) |
|----------|---------|
| Unknown logical device | `TestLibIECClient_Neg_UnknownLD`, `TestBeanClient_Neg_UnknownLD`, `TestGoServer_Neg_UnknownLD` |
| Unknown logical node | `TestLibIECClient_Neg_UnknownLN`, `TestBeanClient_Neg_UnknownLN`, `TestGoServer_Neg_UnknownLN` |
| Unknown data object | `TestLibIECClient_Neg_UnknownDO`, `TestBeanClient_Neg_UnknownDO` |
| Write read-only attribute | `TestLibIECClient_Neg_WriteReadOnly`, `TestBeanClient_Neg_WriteReadOnly`, `TestGoServer_Neg_WriteReadOnly` |
| Invalid dataset reference | `TestLibIECClient_Neg_InvalidDataSet`, `TestBeanClient_Neg_InvalidDataSet`, `TestGoServer_Neg_InvalidDataSet` |
| Double-reserve URCB | `TestLibIECClient_Neg_URCBDoubleReserve`, `TestBeanClient_Neg_URCBDoubleReserve` |

---

### Phase 2G — dataset depth ✅

| Scenario | Test(s) |
|----------|---------|
| Member discovery (ordered refs) | `TestLibIECClient_DS_MemberDiscovery`, `TestBeanClient_DS_MemberDiscovery` |
| Mixed ST + MX members in one read | `TestLibIECClient_DS_ReadMixedFC`, `TestBeanClient_DS_ReadMixedFC` |
| Multi-value dataset read | `TestLibIECClient_DS_ReadAllMembers`, `TestBeanClient_DS_ReadAllMembers` |
| GI → full dataset in report | `TestLibIECServer_DS_GIReport` |
| Two simultaneous writes → multi-member dchg | `TestLibIECServer_DS_MultiWrite` |

Dynamic dataset creation deferred (ownership and lifecycle complexity).

---

### Phase 2H — report semantics expansion ✅ (partial)

**2H-a General interrogation** ✅ — `TestLibIECClient_URCB_GIReport`, `TestLibIECServer_URCB_GIReport`, `TestBeanServer_URCB_GIReport`.

**2H-b Integrity** — configure short integrity period; receive periodic complete report; assert `ReasonIntegrity`. Not yet started.

**2H-c Multiple data changes** ✅ — `TestLibIECServer_URCB_MultiMemberDchg`.

**2H-d Buffered reporting** — separate phase; requires decisions on entry IDs, buffer time, reconnect, purge-buffer, overflow and replay-after-disconnect.

---

### Phase 2I — SBO controls ✅

Five tests covering SBO normal security (`ctlModel=2`, `GGIO1.SPCSO2`) in all four directions plus a negative case:

| Test | Direction |
|------|-----------|
| `TestLibIECServer_Control_SBOOperate`   | libiec61850 client → go server |
| `TestLibIECClient_Control_SBOOperate`   | go client → libiec61850 server |
| `TestBeanServer_Control_SBOOperate`     | iec61850bean client → go server |
| `TestBeanClient_Control_SBOOperate`     | go client → iec61850bean server |
| `TestGoServer_SBO_OperateWithoutSelect` | operate rejected when no prior select |

**Remaining (not yet tested):** SBO enhanced (SBOw / `ctlModel=4`), cancel, select timeout, wrong ctlNum, second-client contention.

---

### Phase 2J — SBOw (enhanced security) controls ✅

Select-before-operate with enhanced security (`ctlModel=4`, `GGIO1.SPCSO3`, `SelectWithValue`):

| Test | Direction |
|------|-----------|
| `TestLibIECServer_Control_SBOwOperate`   | libiec61850 client → go server |
| `TestLibIECClient_Control_SBOwOperate`   | go client → libiec61850 server |
| `TestBeanServer_Control_SBOwOperate`     | iec61850bean client → go server |
| `TestBeanClient_Control_SBOwOperate`     | _(skipped — known iec61850bean gap)_ |
| `TestGoServer_SBOw_OperateWithoutSelect` | operate rejected when no prior SBOw select |
| `TestGoServer_SBOw_CancelClearsSelect`   | select, cancel, then operate rejected |

Key findings:
- `ctlNum` must be identical across `SelectWithValue` and `Operate`; both the `libiec61850` and `go-iec61850` servers enforce this.
- `libiec61850-ied-controller` performs select+operate atomically on the same `ControlObjectClient` instance to guarantee `ctlNum` consistency.
- `iec61850bean` server does not expose `SBOw[CO]` as a writable MMS object; go→bean SBOw is a known gap.

---

## Fixtures

`interop/testdata/interop.icd` and `interop/testdata/values.json` are synchronized copies of the canonical fixtures from mms-interop. They must be updated alongside the pinned adapter image version when the fixture contract changes.
