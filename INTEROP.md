# go-iec61850 Interoperability

`go-iec61850` decides which capabilities need verification. Tests in `interop/` consume the adapter images published by [mms-interop](https://github.com/otfabric/mms-interop) and assert `go-iec61850` behaviour against live, independent implementations.

New adapter commands or fixture capabilities are requested from `mms-interop` only when the required external operation does not yet exist there.

## Architecture

```
mms-interop
  libiec61850 adapter images
  iec61850bean adapter images
  dscsystems adapter images
  iec61850-rust adapter images
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
      goose_test.go                  TestGOOSE_* (libiec61850, dscsystems, iec61850-rust)
      sv_test.go                     TestSV_* (libiec61850, dscsystems, iec61850-rust),
                                     TestLibIECClient_SVCB_Read (MSVCB over MMS)
```

The `TestLibIEC*` tests run twice in CI: against libiec61850 and, with
`IEC61850_PEER_STACK=dscsystems`, against the
[dscsystems](https://github.com/dscsystems/go-iec61850) adapters, which
implement the same commands and JSON Lines contract (`dscsystems-` prefix),
and with `IEC61850_PEER_STACK=iec61850-rust` against the iec61850-rust
adapters. See the per-stack matrix in [COMPATIBILITY.md](COMPATIBILITY.md#per-stack-matrix).

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
LIBIEC61850_IMAGE=ghcr.io/otfabric/mms-interop-libiec61850:v0.4.1 \
IEC61850BEAN_IMAGE=ghcr.io/otfabric/mms-interop-iec61850bean:v0.4.1 \
make interop

# The same suites against another stack
IEC61850_PEER_STACK=dscsystems \
DSCSYSTEMS_IMAGE=ghcr.io/otfabric/mms-interop-dscsystems:v0.4.1 make interop
IEC61850_PEER_STACK=iec61850-rust \
IEC61850RUST_IMAGE=ghcr.io/otfabric/mms-interop-iec61850-rust:v0.4.1 make interop
```

CI pins every image by digest in `.github/workflows/interop.yml`. GOOSE
and SV need a raw interface on a Docker network and root; see
[interop/README.md](interop/README.md) and `make interop-goose`.

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LIBIEC61850_IMAGE` | libiec61850 adapter image | `mms-interop-libiec61850:local` |
| `IEC61850BEAN_IMAGE` | iec61850bean adapter image | `mms-interop-iec61850bean:local` |
| `DSCSYSTEMS_IMAGE` | dscsystems adapter image | `mms-interop-dscsystems:local` |
| `IEC61850RUST_IMAGE` | iec61850-rust adapter image | `mms-interop-iec61850-rust:local` |
| `IEC61850_PEER_STACK` | stack behind `TestLibIEC*`: `libiec61850`, `dscsystems` or `iec61850-rust` | `libiec61850` |
| `IEC61850_SKIP_BEAN` | do not start the shared iec61850bean server | — |
| `GOOSE_IFACE` | raw interface on the GOOSE Docker network (`goose0`); GOOSE tests skip when unset | — |
| `GOOSE_NETWORK` | Docker network of the GOOSE adapters | `mms-interop-goose` |
| `IEC61850_FIXTURE_DIR` | Path to `fixtures/iec61850/` from mms-interop (`goose/` and `sv/` hold `fixtures/goose/` and `fixtures/sv/`) | `testdata/` |
| `IEC61850_SV_FIXTURE` | SV fixture revision the images implement (`sv-v1`); SV tests skip unless it matches | — |

## Test naming

| Prefix | Go role | Adapter counterpart |
|--------|---------|---------------------|
| `TestLibIECClient_` | IEC 61850 client | `libiec61850-ied-server` |
| `TestLibIECServer_` | IEC 61850 server | `libiec61850-ied-client` |
| `TestBeanClient_` | IEC 61850 client | `iec61850bean-ied-server` |
| `TestBeanServer_` | IEC 61850 server | `iec61850bean-ied-client` |
| `TestGoServer_` | IEC 61850 server | client of the peer stack |
| `TestGOOSE_` | GOOSE publisher / subscriber / server | `<stack>-goose-subscriber` / `-goose-publisher` |
| `TestSV_` | SV publisher / subscriber / server | `<stack>-sv-subscriber` / `-sv-publisher` |

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
| SBOw — CommandTermination+ after operate | n/a | ✓² | n/a | — |
| Cancel | ✓ | ✓ | — | — |

**Notes:**
- `some` indicates the error is caught but the specific error category is not yet asserted for all directions.
- BRCB is implemented and covered by Go client/server integration tests (`interop/brcb_*.go`). External BRCB interoperability against libiec61850 and IEC61850bean is not yet part of the adapter matrix.
- `TestBeanClient_*` (go→bean) tests use `client.Abort` for teardown because iec61850bean's server does not respond to MMS Conclude requests.
- ¹ `TestBeanClient_Control_SBOwOperate` is skipped: `iec61850bean` server does not expose `SBOw[CO]` as a writable MMS attribute; direct SBOw select is a known gap in that stack.
- ² The dscsystems controller waits for CommandTermination after an
  enhanced-security operate; the Go server sends it since v1.1.0.
- The same libiec61850 columns also pass against dscsystems
  (`IEC61850_PEER_STACK=dscsystems`).

### GOOSE

Fixture `goose-v1` (mms-interop `fixtures/goose/`, copied to
`interop/testdata/goose/`): `gcbAll` with eight members (boolean,
integer, unsigned, float32, quality, UtcTime, Dbpos, nested structure)
and `gcbBool`. Frames cross a Docker bridge (`goose0`) that the test opens
with the `goose` raw transport.

Each row holds for libiec61850, dscsystems and iec61850-rust.

| Capability | Go → stack | stack → Go |
|-----------|:---:|:---:|
| Header: gocbRef, datSet, goID, APPID, dst, confRev, flags | ✓ | ✓ |
| allData values of all member types (initial state) | ✓ | ✓ |
| State change (stNum+1, sqNum 0, new values) | ✓ | ✓ |
| Retransmission (sqNum increments) | ✓ | ✓ |
| 802.1Q tag (priority 4, VID 0) | — | ✓ |
| Subscriber validation from SCL (datSet, goID, confRev, member types) | n/a | ✓ |
| Supervision timeout after publisher stops | ✓ | ✓ |
| IED server GoCB: data change → new state | ✓ | n/a |

The publisher-side VLAN tag is not checked by the adapter subscribers:
Linux strips it before an `AF_PACKET` socket sees the frame and neither
adapter asks for auxiliary data; the Go subscriber does.

### Sampled Values

Fixture `sv-v1` (mms-interop `fixtures/sv/`, copied to
`interop/testdata/sv/`) with the v0.4.1 images: `MSVCB01` (9-2LE, 4000
samples/s, one ASDU per frame) and `MSVCB02` (generic dataset, 1000
samples/s, four ASDUs per frame, every optional field). Each row holds
for libiec61850, dscsystems and iec61850-rust unless noted.

| Capability | Go → stack | stack → Go |
|-----------|:---:|:---:|
| Header: svID, APPID, dst, confRev, smpSynch, noASDU | ✓ | ✓ |
| Optional fields: datSet, refrTm, smpRate, smpMod | ✓¹ | ✓ |
| 9-2LE values (8 × INT32 + quality) and generic dataset values | ✓ | ✓ |
| Value update reaches the other side | ✓ | ✓ |
| Loss, duplicates, reordering (stats, ≤ 1 %) | ✓ | ✓ |
| Subscriber validation from SCL, supervision timeout | n/a | ✓ |
| IED server MSVCB: SvEna, data change in the stream | ✓ | n/a |
| MSVCB read over MMS (Go client → IED server) | libiec61850, dscsystems² | n/a |

¹ libiec61850's subscriber drops ASDUs with a two-octet smpMod, so
`MSVCB02` (with smpMod) is skipped for that subscriber. The Go
subscriber accepts one- and two-octet smpMod.
² iec61850-rust does not model SV control blocks over MMS. libiec61850
encodes the MSVCB's ConfRev, SmpRate, SmpMod and noASDU as MMS
`Integer`; the client accepts non-negative Integer wherever an
unsigned control-block attribute is expected. The MS `OptFlds` is 5
bits from libiec61850 and 10 bits from dscsystems; the client reads the
first five.

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
| CreateDataSet | ✓ | — | server dynamic dataset creation deferred |
| DeleteDataSet | ✓ | — | server dynamic dataset deletion deferred |
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
| DeleteFile | ✓ | ✓ | |
| GetFileAttributeValues | ✓ | ✓ | |
| SendGOOSEMessage | ✓ | ✓ | [GOOSE.md](GOOSE.md); `goose` package and `Server.EnableGOOSE` |
| GetGoCBValues / SetGoCBValues | ✓ | ✓ | `Client.GetGoCB` / `SetGoCB`; GoEna, GoID, DatSet writable |
| SendGSSEMessage | — | — | GSSE not supported |
| SendMSVMessage | ✓ | ✓ | [SV.md](SV.md); `sv` package and `Server.EnableSV` |
| GetMSVCBValues / SetMSVCBValues | ✓ | ✓ | `Client.GetMSVCB` / `SetMSVCB`; SvEna, MsvID, DatSet, SmpRate, OptFlds, SmpMod writable |
| SendUSVMessage, Get/SetUSVCBValues | — | — | unicast SV not supported |
| Select | ✓ | ✓ | SBO normal |
| SelectWithValue | ✓ | ✓ | SBOw enhanced |
| Cancel | ✓ | ✓ | |
| Operate | ✓ | ✓ | direct and SBO/SBOw |
| CommandTermination | — | ✓ | positive termination after enhanced-security operate |
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
| GOOSE security (IEC 62351-6), fixed-offset encoding | not implemented | see [GOOSE.md](GOOSE.md#limitations) |
| SV security (IEC 62351), unicast SV (USVCB) | not implemented | see [SV.md](SV.md#limitations) |
| Dynamic dataset creation | not implemented | deferred |
| CommandTermination− (negative), client-side termination handling | not implemented | deferred |
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

## Fixtures

`interop/testdata/` holds synchronized copies of the canonical mms-interop
fixtures, matching the pinned adapter images (mms-interop v0.4.1):

| Path | Fixture revision | Content |
|---|---|---|
| `interop.icd`, `values.json` | `iec61850-v3` | IED model and initial values |
| `goose/` | `goose-v1` | `interop-goose.cid`, `goose.json` (GoCBs `gcbAll`, `gcbBool`) |
| `sv/` | `sv-v1` | `interop-sv.cid`, `sv.json` (MSVCB01 9-2LE, MSVCB02 generic) |

Update them together with the image digests in
`.github/workflows/interop.yml` when the fixture contract changes.
