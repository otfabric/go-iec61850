# go-iec61850 Interoperability

`go-iec61850` decides which capabilities need verification. Tests in `interop/` consume the adapter images published by [iec61850-interop](https://github.com/otfabric/iec61850-interop) and assert `go-iec61850` behaviour against live, independent implementations.

New adapter commands or fixture capabilities are requested from `iec61850-interop` only when the required external operation does not yet exist there.

## Architecture

```
iec61850-interop
  libiec61850 adapter images
  iec61850bean adapter images
  dscsystems adapter images
  iec61850-rust adapter images
  cbsigma adapter images
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
      goose_test.go                  TestGOOSE_* (libiec61850, dscsystems, iec61850-rust, cbsigma)
      sv_test.go                     TestSV_* (libiec61850, dscsystems, iec61850-rust),
                                     TestLibIECClient_SVCB_Read (MSVCB over MMS)
```

The `TestLibIEC*` tests run once per stack in CI: against libiec61850 and, with
`IEC61850_PEER_STACK=dscsystems`, against the
[dscsystems](https://github.com/dscsystems/go-iec61850) adapters, which
implement the same commands and JSON Lines contract (`dscsystems-` prefix),
with `IEC61850_PEER_STACK=iec61850-rust` against the iec61850-rust
adapters, and with `IEC61850_PEER_STACK=cbsigma` against the cB-Sigma
adapters ([iec61850-sim](https://github.com/cB-Sigma/iec61850-sim) serves
the IED, [cB-Sigma/iec61850](https://github.com/cB-Sigma/iec61850) is the
client, controller and reporter). See the per-stack matrix in [COMPATIBILITY.md](COMPATIBILITY.md#per-stack-matrix).

Each test:
1. Starts the adapter container with `docker run`.
2. Waits for the readiness event (`{"event":"ready",...}`) on stdout.
3. Exercises the `go-iec61850` API under test.
4. Asserts results.
5. Tears the container down.

No pre-running containers. No manual steps. Tests are gated behind `-tags=interop`.

## Running

```bash
# Build adapter images locally (in iec61850-interop)
cd ../iec61850-interop && make build

# Run against local images
LIBIEC61850_IMAGE=iec61850-interop-libiec61850:local \
IEC61850BEAN_IMAGE=iec61850-interop-iec61850bean:local \
make interop

# Run against published images (version tags)
LIBIEC61850_IMAGE=ghcr.io/otfabric/iec61850-interop-libiec61850:v1.0.1 \
IEC61850BEAN_IMAGE=ghcr.io/otfabric/iec61850-interop-iec61850bean:v1.0.1 \
make interop

# The same suites against another stack
IEC61850_PEER_STACK=dscsystems \
DSCSYSTEMS_IMAGE=ghcr.io/otfabric/iec61850-interop-dscsystems:v1.0.1 make interop
IEC61850_PEER_STACK=iec61850-rust \
IEC61850RUST_IMAGE=ghcr.io/otfabric/iec61850-interop-iec61850-rust:v1.0.1 make interop
IEC61850_PEER_STACK=cbsigma \
CBSIGMA_IMAGE=ghcr.io/otfabric/iec61850-interop-cbsigma:v1.4.0 make interop
```

### Against go-iec61850 itself (Self-interop)

`IEC61850_PEER_STACK=otfabric IEC61850_INTEROP_OTFABRIC_IMAGE=ghcr.io/otfabric/iec61850-interop-otfabric:latest`
runs the same suites against the latest published go-iec61850 (and the go-mms
under it) behind the same contract: an image of
[iec61850-interop](https://github.com/otfabric/iec61850-interop/tree/main/own/go-iec61850),
built by hand from the latest releases. With that stack the GOOSE and SV tests
run against it alone. The manual **Self-interop** workflow does this from any
commit and logs the image digest; it finds a wire change that breaks an earlier
release, which the e2e tests (both sides from one commit) cannot. It is not
independent evidence, never a required check and not part of the Interop
workflow, and nothing pins the image (ADR-0012), so it cannot ask for a release.
Nothing in this file's matrices comes from it.

CI pins every image by digest in `.github/workflows/interop.yml`. GOOSE
and SV need a raw interface on a Docker network and root; see
[interop/README.md](interop/README.md) and `make interop-goose`.

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LIBIEC61850_IMAGE` | libiec61850 adapter image | `iec61850-interop-libiec61850:local` |
| `IEC61850BEAN_IMAGE` | iec61850bean adapter image | `iec61850-interop-iec61850bean:local` |
| `DSCSYSTEMS_IMAGE` | dscsystems adapter image | `iec61850-interop-dscsystems:local` |
| `IEC61850RUST_IMAGE` | iec61850-rust adapter image | `iec61850-interop-iec61850-rust:local` |
| `CBSIGMA_IMAGE` | cbsigma adapter image | `iec61850-interop-cbsigma:local` |
| `IEC61850_PEER_STACK` | stack behind `TestLibIEC*`: `libiec61850`, `dscsystems`, `iec61850-rust`, `cbsigma`, or `otfabric` (with `IEC61850_INTEROP_OTFABRIC_IMAGE`) | `libiec61850` |
| `IEC61850_INTEROP_OTFABRIC_IMAGE` | image of go-iec61850's own adapter (`ghcr.io/otfabric/iec61850-interop-otfabric`), the `otfabric` stack: not independent, no default, runs only when set | — |
| `IEC61850_SKIP_BEAN` | do not start the shared iec61850bean server | — |
| `GOOSE_IFACE` | raw interface on the GOOSE Docker network (`goose0`); GOOSE tests skip when unset | — |
| `GOOSE_NETWORK` | Docker network of the GOOSE adapters | `iec61850-interop-goose` |
| `IEC61850_FIXTURE_DIR` | Path to `fixtures/iec61850/` from iec61850-interop (`goose/` and `sv/` hold `fixtures/goose/` and `fixtures/sv/`) | `testdata/` |
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
| SBOw — CommandTermination+ awaited (`OperateWait`) | ✓³ | n/a | n/a | n/a |
| Refused operate — LastApplError (object-not-selected, ctlNum) | ✓³ | n/a | n/a | n/a |
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
- ³ `interop/termination_test.go`, against libiec61850, dscsystems,
  iec61850-rust and cbsigma: all four send the positive CommandTermination.
  libiec61850, dscsystems and cbsigma send LastApplError in the five-member
  layout (CntrlObj, Error, Origin, ctlNum, AddCause) with AddCause 18
  (object-not-selected); iec61850-rust refuses the write without a
  LastApplError (skipped).

### Control model choices

Where IEC 61850-7-2/-8-1 leave a choice, the server follows libiec61850
(sources at the pinned revision, `src/iec61850/server/mms_mapping/control.c`;
read, not an interop run). The client side tests above still pass.

| Situation | Response | Spec |
|-----------|----------|------|
| Select (Read of SBO) granted | `LD/LN$CO$DO` (libiec61850; Table 84 writes `…$SBO`) | 8-1 20.5.4 |
| Select refused | `""` | 8-1 20.5.5 |
| Oper while not selected (SBO) | access-denied, AddCause 18 | 7-2 Table 47, 8-1 Table 87 |
| Oper/Cancel while another client holds the selection | temporarily-unavailable, AddCause 27 | 7-2 Table 47 |
| Select/SBOw while selected (also by the holder) | temporarily-unavailable, AddCause 19 | 7-2 Table 47 |
| Any service during execution or before operTm | temporarily-unavailable, AddCause 12 (Cancel by the holder before operTm succeeds) | 7-2 Table 47 |
| Oper/Cancel parameters differ from SBOw (ctlVal, ctlNum, origin, Test, Check) | access-denied, AddCause 26; the selection is released | 7-2 20.5.2 |
| Invalid origin (orCat outside 0..8) | access-denied, AddCause 26 (Oper) or 3 (SBOw) | 7-3 orCat |
| SBOw on a model without enhanced SBO | access-denied, AddCause 1 | 7-2 Table 47 |
| Malformed Oper/SBOw/Cancel | type-inconsistent, no LastApplError | 8-1 8.1.3.4.4.2 |
| Control write without `RegisterControl` | access-denied | 8-1 Table 87 |
| Ownership | the MMS association, not the origin | 7-2 20.2 |

The client discovers whether the Oper type has operTm and reuses the
SelectWithValue ctlNum for the Operate, as libiec61850's client does.

### GOOSE

Fixture `goose-v1` (iec61850-interop `fixtures/goose/`, copied to
`interop/testdata/goose/`): `gcbAll` with eight members (boolean,
integer, unsigned, float32, quality, UtcTime, Dbpos, nested structure)
and `gcbBool`. Frames cross a Docker bridge (`goose0`) that the test opens
with the `goose` raw transport.

Each row holds for libiec61850, dscsystems, iec61850-rust and cbsigma.

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

Fixture `sv-v1` (iec61850-interop `fixtures/sv/`, copied to
`interop/testdata/sv/`) with the v1.0.1 images: `MSVCB01` (9-2LE, 4000
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

COTP transport selectors (`interop/tsap_test.go`, otfabric/go-iec61850#28):

| Direction | Scenario | Result |
|---|---|---|
| go client → strict gate → peer IED server | default client (T-selector `0001`/`0001`, TPDU size 1024) | associates, reads through the gate |
| go client → strict gate | called T-selector omitted (the v1.5.0 default) | refused, DR reason 3 (address unknown) |
| go client → strict gate requiring `00000001` | default `0001` / configured `00000001` | refused / associates |
| peer client → go server requiring its T-selector | stack defaults; `01`; `00000001` via `--local-tsel`/`--remote-tsel` | associates (dscsystems: flags unsupported, skipped) |
| peer client → go server requiring `0002` | called `0001` | association refused |

The gate is a test-side proxy in front of the peer server that refuses a CR
without the required called T-selector or without a TPDU size, as some IEDs do.
It is our code, not an independent stack, so it lives in this suite and not
in iec61850-interop.

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
| CreateDataSet | ✓ | ✓ | persistent and `@name` (association); server: go-mms, in memory |
| DeleteDataSet | ✓ | ✓ | |
| SetDataSetValues | ✓ | ✓ | client: `WriteDataSet`; own tests only |
| GetRCBValues | ✓ | ✓ | URCB and BRCB |
| SetRCBValues | ✓ | ✓ | URCB and BRCB |
| GetSGCBValues | ✓ | ✓ | |
| SetSGValues | ✓ | ✓ | server: one value bank per group (v1.18.0); own tests only |
| ConfirmEditSGValues | ✓ | ✓ | own tests only |
| GetEditSGValues | ✓ | ✓ | |
| GetLCBValues / SetLCBValues | ✓ | ✓ | MMS Read/Write of `LN$LG$<lcb>` (8-1 Table 68); own tests only |
| QueryLogByTime | ✓ | ✓ | open-ended range: zero start/stop on the client |
| QueryLogAfter | ✓ | ✓ | |
| GetLogStatusValues | ✓ | ✓ | OldEntrTm, NewEntrTm, OldEnt, NewEnt of the LCB; own tests only |
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
| CommandTermination | ✓ | ✓ | client: `OperateWait`; server: positive, negative via `ControlHandler.OnExecute` |
| LastApplError | ✓ | ✓ | client: in `ControlError`; server: sent before a refused control write |
| TimeActivatedOperate | ✓ | ✓ | operTm from the Oper type; own tests only |

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
| Purge buffer | n/a | ✓ | PurgeBuf, and implicitly on a configuration change |
| Buffering while disabled | n/a | ✓ | from `EnableReports` on (7-2 §17.2.3.2.3.6) |
| Replay on re-enable | n/a | ✓ | |
| BufTm aggregation | ✓ | ✓ | |
| Overflow policy (DropNewest) | ✓ | ✓ | configurable |
| Association-scoped ownership | ✓ | ✓ | owner-only delivery; association loss disables and releases; own tests only |
| Multi-segment reports | ✓ | ✓ | server segments at its MaxPDUSize, client reassembles; own tests only |
| Indexed RCB instances | ✓ | ✓ | SCL `indexed` / `RptEnabled max` → `<name>01..` |

The rows marked "own tests only" are covered by go-iec61850's spec tests
(`TestSpec_*` in `report_spec_server_test.go`, `journal_lcb_spec_test.go`)
and not yet by a run against the iec61850-interop images.

### Reporting and logging: implementation choices

Where IEC 61850-7-2 and -8-1 leave a choice, or where the two readings of a
clause differ, the server does the following:

| Topic | Behaviour | Basis |
|-------|-----------|-------|
| BufOvfl after re-enable | TRUE in the first report after a disabled→enabled transition only when entries that were never sent were discarded (since start-up or the last purge), and in the first report after such a discard while enabled. Discarding an entry already sent does not set it. | 7-2 §17.2.3.2.2.8 and Figure 30: BufOvfl signals a possible loss |
| Enable without EntryID resync | Sends all buffered entries from the oldest. | 7-2 Figure 24 |
| GI entries | Get an EntryID and keep the time order, are sent once in the enabling session, are not replayed and are dropped when unsent at disable. | 7-2 §17.2.2.13 Figure 28 (no GI outside the enabled state) |
| Segment size | The server's MMS `MaxPDUSize`, minus the InformationReport framing. | go-mms does not expose the negotiated size per connection |
| SqNum on a segmented report | Same SqNum for all segments, SubSqNum 0..k−1, MoreSegmentsFollow on all but the last; each segment carries the full-width inclusion bit string. | 7-2 §17.2.3.2.2.5, 8-1 §17.2.1 |
| URCB OptFlds | BufOvfl, EntryID and segmentation are cleared when written and never reported. | 8-1 Table 63, 7-2 §17.2.5.2 |
| Indexed RCB naming | `indexed` absent or true: `<name>01..<name>NN` (NN = RptEnabled max, default 1), as libiec61850 does; `indexed="false"`: `<name>`. Edition 1 files are always indexed. | 8-1 §17.1.2/§17.1.3.2, IEC 61850-6 §9.3.8 |
| Journal variable tags | Per member: the ObjectReference `LD/LN$FC$DO$DA`, then `ReasonCode` (7-bit). One journal entry holds all members of one notification. | 8-1 §17.3.3.4.3/4 (libiec61850 uses the same `ReasonCode` tag; the `…$V|reasonCode` form in the 8-1 example is not used) |
| LCB write errors | LogRef/DatSet/TrgOps/IntgPd while LogEna=TRUE: temporarily-unavailable; LogRef or DatSet naming nothing: object-value-invalid. | 8-1 §17.3.4.3, §8.1.3.4.4.2 |

### Known limitations / deferred features

| Feature | Status | Notes |
|---------|--------|-------|
| GOOSE security (IEC 62351-6), fixed-offset encoding | not implemented | see [GOOSE.md](GOOSE.md#limitations) |
| SV security (IEC 62351), unicast SV (USVCB) | not implemented | see [SV.md](SV.md#limitations) |
| CommandTermination− (negative) | own tests only | no peer in the matrix sends one on demand |
| TimeActivatedOperate | own tests only | no peer in the matrix exercises it |
| iec61850bean SBOw | known gap (upstream) | iec61850bean does not expose `SBOw[CO]` |
| Edition 2.1 extensions | partial | not all 2007B4 features validated |

---

## BRCB EntryID resume — implementation contract

This section documents EntryID-based resume as implemented, following
IEC 61850-7-2 §17.2.2.15 and Figure 24.

### What EntryID is

- An EntryID is an 8-byte big-endian `OctetString` that identifies a buffered report entry on the server (IEC 61850-8-1 §8.1.3.5).
- The server assigns increasing 64-bit counters, starting at 1 for the first entry after `EnableReports()`. IDs continue after a purge; they are never reused while the server runs.
- ID 0 (`0x0000000000000000`) is never assigned: writing it ends a pending resynchronisation.
- While the BRCB is disabled, EntryID reads the newest buffered entry; while it is enabled, the entry of the last report sent.

### Resynchronisation

The client writes the EntryID of the last entry it received while the BRCB is disabled, then enables it. The server sends the entries **after** that entry, then the live ones, with SqNum continuing without a gap.

| Client writes (RptEna=FALSE) | Server behavior |
|--------------|----------------|
| All-zero bytes (`0x00…00`) | No resync: the next enable sends the whole buffer |
| EntryID of a buffered entry | Accepted; the next enable sends the entries after it |
| EntryID not (or no longer) in the buffer | Refused with object-value-invalid (parameter-value-inappropriate) |
| Not 8 octets | Refused with type-inconsistent |
| Any EntryID while RptEna=TRUE | Refused with temporarily-unavailable |

If the resync entry is discarded between the write and the enable, the enable sends the whole buffer and the first report has `BufOvfl=true`.

### Buffer overflow interaction

When the server-side BRCB buffer (`bufMax`, default 1000 entries) overflows, the oldest entry is dropped. If that entry had not been sent, the first report sent afterwards (after the next enable, when disabled) has `BufOvfl=true`; the following reports have `BufOvfl=false`.

### Stability

- EntryIDs are **not persistent** across server process restarts. They restart at 1 on each `EnableReports()` call.
- Clients must not cache EntryIDs across reconnects to a different server process or after a server restart; a stale EntryID is refused, not silently ignored.
- `PurgeBuf=true` (only while disabled) and a change of RptID, DatSet, BufTm, TrgOps or IntgPd empty the buffer (IEC 61850-8-1 §17.1.2). An EntryID from before the purge is refused.

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
2. If it had not been sent, the next delivered report has `BufOvfl=true` (when `OptFldBufOvfl` is enabled); later reports have `BufOvfl=false`. Dropping an entry that was already sent sets nothing.
3. The client can detect the gap from `BufOvfl` and decide whether to re-read the dataset via GI.

### Confirmed services vs. report delivery

Report delivery (InformationReports) and confirmed services (Read, Write, SetRCBValues) use the same MMS connection but are dispatched independently. A full application-side report queue does not block confirmed services unless `OverflowBlock` is in use.

---

## Fixtures

`interop/testdata/` holds synchronized copies of the canonical iec61850-interop
fixtures, matching the pinned adapter images (iec61850-interop v1.4.0):

| Path | Fixture revision | Content |
|---|---|---|
| `interop.icd`, `values.json` | `iec61850-v3` | IED model and initial values |
| `goose/` | `goose-v1` | `interop-goose.cid`, `goose.json` (GoCBs `gcbAll`, `gcbBool`) |
| `sv/` | `sv-v1` | `interop-sv.cid`, `sv.json` (MSVCB01 9-2LE, MSVCB02 generic) |

Update them together with the image digests in
`.github/workflows/interop.yml` when the fixture contract changes.
