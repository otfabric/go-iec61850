# go-iec61850 Releases

## v1.18.0

**Date:** 2026-11-11
**Previous release:** v1.17.1

Six features in one release: R-GOOSE and R-SV over UDP/IP with authenticated SPDUs (IEC TR 61850-90-5); setting group value banks (IEC 61850-7-2 §16); BufTm and a pluggable BRCB buffer for the report engine (§17.2.2.9, §17.2.3.2.2.8); the control check conditions, interlocking and synchrocheck, with CILO and RSYN bindings and an opt-in process response (§20); compound writes of every writable FC; and an injectable clock that carries an honest TimeQuality. The API is additive. Behaviour changes that dependants should check are listed under Changed: the default TimeQuality is now "not synchronised", reports leave without the fixed 100 ms delay, and clients must register report handlers before they enable an RCB.

### Added

#### R-GOOSE and R-SV

- **Package `rsession`**: the session protocol of IEC TR 61850-90-5 for routable GOOSE and SV (R-GOOSE, R-SV), unicast and multicast, UDP port 102 by default. `rsession.Transport` implements the layer-2 `Transport` of `goose` and `sv`, so their publishers and subscribers run over UDP unchanged. Protocol versions 1 and 2 are received; 2 is sent by default.
- **Authentication**: HMAC-SHA256 truncated to 80, 128 or 256 bits over every SPDU, with keys from a `KeyProvider` interface (`KeyRing` is a static provider that supports key changes). Secured by default: `Open` needs a key provider unless `Insecure` is set, and a receiver refuses unsecured, unknown-key, tampered and replayed SPDUs (64-entry window per sender and key), counting them in `Stats` and reporting them to `OnReject`.
- `rsession.Encode`, `Decode` and `Verify` for single SPDUs; fuzz targets `FuzzDecode` and `FuzzVerify`.
- [ADR 0001](docs/adr/0001-routable-goose-sv.md): the wire layout, the references it follows (IEC TR 61850-90-5 and IEC 62351-6 were not available; the layout follows libiec61850, Wireshark and dscsystems where they agree) and the scope.

#### Setting group value banks (IEC 61850-7-2 §16, 8-1 §16)

- **One value bank per setting group.** This closes the "no per-group value banks" limitation of v1.17. The server keeps a value for every FC=SG and FC=SE leaf of the logical device in each group. FC=SG reads give the active group. SelectActiveSG loads the selected group into FC=SG (and sets ActSG and LActTm). SelectEditSG copies the group into the edit buffer, FC=SE, which SetEditSGValue writes. ConfirmEditSGValues copies FC=SE into the group, and into FC=SG when that group is active. SelectEditSG 0, ResvTms expiry and loss of the association discard the edit. The edit reservation per association is as in v1.17.
- **Banks from SCL.** `SettingControl numOfSGs` gives the number of groups. Each DAI `Val sGroup="n"` is that group's value (IEC 61850-6 9.5.4.4). A group without one takes the `Val` without `sGroup`, else the DA's value. An `sGroup` above `numOfSGs` is a model warning. The active group's value becomes the attribute's initial value.
- `SettingGroupEngine.SettingKeys`, `SettingGroupValue` and `SetSettingGroupValue` read and seed the banks, for example to restore persisted groups at startup. Seeding the active group changes FC=SG.
- When the active group's values change (SelectActiveSG, or ConfirmEditSGValues of the active group), the change goes through `Server.SetValue`: data change reports, logs and GOOSE follow.

#### BufTm and the BRCB buffer (IEC 61850-7-2 §17.2.2.9, §17.2.3.2.2.8)

- `ReportBuffer`, the interface of a BRCB buffer: append with discard of the oldest entries, the entries after an EntryID, get, oldest, newest, remove, purge. `ReportEntry` is one entry; `ReportEntry.EncodedSize` gives its size.
- `MemoryReportBuffer`, the default buffer (`NewMemoryReportBuffer(maxEntries, maxBytes)`, `SetLimits`, `Limits`, `Bytes`, `Entries`). It is bounded by entries (default 1000) and, optionally, by encoded size. When unsent entries are discarded, BufOvfl=TRUE is set in the next report (§17.2.3.2.2.8).
- `ServerOptions.NewReportBuffer` installs a buffer per BRCB. A buffer that already holds entries at `EnableReports` is continued: EntryIDs follow its newest entry, resync to its entries works, and its GI entries are not replayed. No persistent implementation ships (KNOWN_LIMITATIONS.md).

#### Control checks and process response (IEC 61850-7-2 §20, IEC 61850-7-4)

- **Interlocking check** (§20.2.1, §20.5.2.7): `ControlHandler.CheckInterlock` and the CILO binding `ControlHandler.Interlocking` ("CILO1" or "LD/CILO1"). It is an operative test: it runs at SelectWithValue and at Operate when the request's Check has interlock-check. With a CILO, a close (ctlVal TRUE) needs `EnaCls.stVal` TRUE and an open `EnaOpn.stVal` TRUE (7-4 CILO). A refusal carries AddCause blocked-by-interlocking (10) in the LastApplError and gets object-access-denied.
- **Synchrocheck** (§20.2.1 dynamic test, Table 54): `ControlHandler.CheckSynchro`, the RSYN binding `ControlHandler.Synchrocheck` and `ControlHandler.SynchroTimeout`. It runs at Operate only, after the operative tests, when Check has synchrocheck. The RSYN binding waits up to SynchroTimeout for `Rel.stVal` TRUE (7-4 RSYN); otherwise the Operate is refused with AddCause blocked-by-synchrocheck (11).
- A check refusal keeps another AddCause the callback gives in a `*ControlError`. A time activated operate runs its checks at operTm; a failed check ends it with a negative termination (LastApplError, then the Oper). The owner's Cancel while its synchrocheck waits is accepted, and the operate ends with abortion-by-cancel (15). During the tests, Oper and SBOw get command-already-in-execution and another client's Cancel locked-by-other-client (Table 47). A check whose Check bit is clear, or that the registration does not supply, is not run (§20.5.2.7).
- **`ProcessResponse`** (`ControlHandler.Process`, opt-in, default operate only):
  - `TravelTime`: a DPC's stVal shows intermediate-state (00) until the end position. With enhanced security the CommandTermination is positive when the end position is reached and negative (time-limit-over) when CF operTimeout comes first (§20.3.3 d); with normal security the Operate response does not wait.
  - `OpCnt`: an INS operation counter.
  - `PositionReached`: refuses an operate to the current position with AddCause position-reached (Table 54).
- `RegisterControl` refuses a CILO, RSYN or operation counter the model does not have, with `ErrInvalidArgument`.

#### Clock and TimeQuality

- `Clock` interface (`Now`, `Quality`), the default `SystemClock` and `ClockWithQuality(func() TimeQuality)`, the hook for a synchronisation-status source (chrony, ptp4l, the kernel NTP state); the library has no OS integration. See API.md "Clock".
- `ServerOptions.Clock` stamps the `t` the default operate writes (also at the end of a `ProcessResponse` travel), report TimeStamp and BRCB TimeOfEntry, journal entry times (value writes and integrity), SGCB LActTm, GOOSE T and SV refrTm.
- `ClientOptions.Clock` and `DialOptions.Clock` stamp the T of Operate, SelectWithValue and Cancel; `OperateParams.TQuality` / `CancelParams.TQuality` still override the quality per call.
- `goose.PublisherConfig.Clock` and `sv.PublisherConfig.Clock` stamp T / refrTm; `Now` keeps pacing the frames. `sv.PublisherConfig.TimeQuality` and `sv.DefaultTimeQuality` (0x2a).
- Timers (sboTimeout, operTm wait, operTimeout, IntgPd, BufTm, ResvTms, reservation release) stay on the monotonic clock and never call the `Clock`, so a wall-clock step does not fire or stretch them; `TestSpec_7_2_ClockStepKeepsTimers` checks it.

### Changed

- **Built on go-mms v1.9.0** (was v1.8.0): the client enforces the negotiated number of outstanding requests and the data structure nesting level (ISO 9506-2 clause 8.2). See the go-mms v1.9.0 notes.
- **Default TimeQuality is "not synchronised".** With no `Clock` set, `SystemClock` sends the TimeQuality octet 0x2a (ClockNotSynchronized, 10 bits of accuracy), as GOOSE already did: the client's control T (was 0x0a, synchronised), the default operate's `t` and LActTm (were 0x0a), the `t` at the end of a `ProcessResponse` travel, and SV refrTm (was 0x00). The times are the same host time as before. Set a `Clock` that reports synchronised when the host is.
- A non-zero `GOOSEOptions.TimeQuality` now overrides the quality of `ServerOptions.Clock`; zero takes the clock's quality (the default `SystemClock` gives the same 0x2a as before).
- **Reports leave without a fixed delay.** The server no longer waits about 100 ms before the first report after a quiet period. A report to an association that has a request in progress waits until that request's response is sent (at most 5 s), so the response to RptEna=TRUE or GI=TRUE still comes first. This holds for connections served by `Server.Serve` and `Server.ListenAndServe`. The standard does not require this order; libiec61850 and the other peers drop a report that arrives before the response.
- **Clients must register their report handler before they enable an RCB.** A BRCB now sends its buffered entries right after the RptEna response. `Client.SubscribeReport` with `AutoEnable` now registers the subscription before it writes RptEna. Before, a replayed report could be lost when it arrived before the subscription was registered; the 100 ms delay usually hid this.
- `Server.ListenAndServe` serves every connection through `Server.Serve`, also without `OnConnect`/`OnDisconnect` hooks, and retries temporary accept errors as go-mms does. RCBs and control selections of a lost association are therefore released at once.
- BufTm (IEC 61850-7-2 §17.2.2.9) runs as the standard describes. The first internal notification starts the buffer time, and changes of other members within it join one report. A second change of a member already in the pending report sends that report at once and starts the next. BufTm 0 sends one report per notification. Integrity and GI reports are not delayed; the pending report is queued before them. TimeOfEntry is the time of the first notification (§17.2.2.16). This replaces the v1.17 aggregation, which ran on the wall clock only.
- `SetRCBBufMax` keeps the byte limit of the buffer and returns false for an RCB whose buffer is not a `MemoryReportBuffer`.
- A control leaves execution before its CommandTermination is sent, so a client that acts on the termination finds the control free. Before, a select sent straight after `OperateWait` could get command-already-in-execution.
- A DA with `fc="SE"` and no `fc="SG"` twin gets one in the server model, since `fc="SE"` implies `fc="SG"` (IEC 61850-6 Table 47). Before, such a setting (common in ABB CID files) had no FC=SG attribute to read its active value.
- A DAI without an FC sets the attributes of that name under every FC, so both SG and SE twins get it. Before, only the first one did.
- A `SettingGroupHandler` callback runs before the bank operation. Values that `OnActiveSGChanged` sets on FC=SG, or `OnEditSGSelected` sets on FC=SE, win over the bank and are kept in it. Applications that kept their own group values (as the v1.17 demo IED did) keep working. They can drop that code and give the values in SCL or through `SetSettingGroupValue`.
- The demo IED (`examples/internal/demoied`) takes its two PTOC1.StrVal groups from `Val sGroup` in `demo.icd`, with no handler code.

### Fixed

- A client write of a constructed data attribute of a writable FC (SE, CF, DC, SP, SV, BL and the others clients may write; for example `LN$CF$DO$cmp`), and SetEditSGValue of a constructed attribute (an FCDA such as `StrVal.setMag`), sets its leaves. Before, the structure was stored under its own key, while reads of the structure and of its leaves come from the leaves, so the write was lost. CO control services (Oper, SBOw, Cancel) are unchanged.
- Such a write is checked leaf by leaf before anything is stored, as a leaf write is: an enumeration value outside its EnumType answers object-value-invalid, and the write guard (for example the `ctlModel` of a registered control) applies to every leaf. A refused leaf refuses the whole write.
- Each leaf is stored and then notified under its own key, so data change reports, GOOSE, SV and logs follow a constructed write. Before, the notification carried the compound key, which no data set member matches. A write that changes several leaves of one data set member gives one report per changed leaf, also within BufTm (see KNOWN_LIMITATIONS.md).

### Tests

- Setting groups: `TestSpec_*` per SGCB service on an authored fixture (`testdata/setting-groups.cid`, MIT), a chaos test of concurrent select, edit, confirm and cancel, and unit tests of the SCL value handling.
- BufTm: `TestSpec_7_2_17_2_2_9_*`, `TestSpec_7_2_17_2_2_12_IntegrityNotDelayedByBufTm`, `TestSpec_7_2_17_2_2_13_GIAfterPendingEvents`, `TestSpec_8_1_17_1_2_BufTmChangeDropsPendingReport` and `TestSpec_7_2_17_2_3_2_2_5_BufTmReportSegmented` run the BufTm rules on an injected clock.
- BRCB buffer: `TestSpec_7_2_17_2_3_2_2_8_ByteLimitSetsBufOvfl`, buffer unit tests, adoption of a pre-filled buffer, and a buffer that cannot store an entry.
- Chaos and race tests with goroutine-leak checks: concurrent changes, GI and enable/disable against a short BufTm and a bounded buffer; an association dropping while a report waits for a response; `TestChaos_ControlChecks`.
- Interop with the pinned iec61850-interop images passes (libiec61850, iec61850bean, dscsystems, iec61850-rust, cB-Sigma). The go-to-go BRCB replay tests in `interop/` failed intermittently without the client fix above.

### Known limitations and not included

- No pinned iec61850-interop adapter sends Check bits, so the control checks are verified in-process only (`TestSpec_*` in `control_checks_test.go`, `TestChaos_ControlChecks`).
- Layer-2 IEC 62351-6 authentication (Reserved 1/2 and the extension field) is deferred until IEC 62351-6 is available; see KNOWN_LIMITATIONS.md.
- AES-GMAC, AES-GCM encryption and IEC 62351-9 key distribution.
- Interop: no independent R-GOOSE/R-SV peer has been tested yet.

---

## v1.17.1

**Date:** 2026-11-11
**Previous release:** v1.17.0

### Changed

- The bundled command-line tools (`sclparse`, `sclgen`) are built on v1.17.0; they were on v1.15.1. They now apply the v1.17.0 SCL checks: XSD keys, required attributes, FCDA resolution, address formats and ranges, and the `Flatten` status rows. Library code is the same as v1.17.0.

---

## v1.17.0

**Date:** 2026-11-11
**Previous release:** v1.16.2

Spec review against IEC 61850-7-2:2010, 7-3:2010, 8-1:2011, 9-2:2011 and 6 Ed2 (and the 2003 Edition 1 texts where they differ). The API is additive; many server and client behaviours change to what the standard requires, and each is listed under **Changed** with its clause. Interop with the pinned iec61850-interop images (libiec61850, iec61850bean, dscsystems, iec61850-rust, cB-Sigma) passes; `TestTSAPGate_ConfiguredSelector` now retries its upstream dial instead of failing on a Docker port-forward race.

### Changed

- **Built on go-mms v1.8.0** (was v1.7.2): the MMS spec review. It covers association refusal, negotiation and orderly release; Reject for unsupported or malformed requests; full BER on receive; and single-precision `NewFloat`. See the go-mms v1.8.0 notes.

**Reporting (IEC 61850-7-2 §17.2, 8-1 §17.2)**

- Report control blocks are owned per association: an RCB enabled or reserved by one client is temporarily-unavailable to the others and released when that association ends. Reports go only to the owner, in order, through a per-association queue (§17.2.4.4/5).
- SqNum starts at 0 and wraps at INT8U (URCB) or INT16U (BRCB).
- ConfRev, SqNum and TimeOfEntry are read-only; a write to the whole RCB structure is denied; an unknown component gives object-non-existent; attributes that may not change while RptEna=TRUE are refused, not stored. Write refusals use the 8-1 §8.1.3.4.4.2 codes.
- A GI on a disabled RCB fails. URCB OptFlds bits without URCB meaning are masked.
- BRCBs buffer while disabled; GI reports are not buffered; BufOvfl is set only when unsent entries are discarded; EntryID is kept on disable, an EntryID not in the buffer gives object-value-invalid, and replay keeps each entry's TimeOfEntry.
- Indexed ReportControl instances are `<name>01..<name>NN` (IEC 61850-6 §9.3.8, default indexed=true): `urcb` becomes `urcb01`, libiec61850-style `urcb01` becomes `urcb0101`.
- The reason for inclusion follows the per-attribute trigger options of the model.
- Client `SetReportControlBlock` writes in the 8-1 §17.1.2 order (RptEna last when enabling, first when disabling).

**Logging (7-2 §17.3, 8-1 §17.3)**

- `LogValueWrite` journals only writes that match an enabled LCB's data set and TrgOps; ReadJournal on an unknown journal is object-non-existent; zero time bounds mean the whole BinaryTime range.

**Control (7-2 §20 Table 47, 8-1 §20)**

- Server state machine per Table 47: services during execution or before operTm get command-already-in-execution (12); a re-select while Ready gets object-already-selected (19); Cancel by another client gets locked-by-other-client (27); parameters differing from the SBOw (ctlVal, ctlNum, origin, Test, Check) get inconsistent-parameters (26) and release the selection.
- TimeActivatedOperate waits for operTm; CF sboTimeout, sboClass (operate-many) and operTimeout take effect; an SBO-normal Select calls `OnSelect`; a lost association releases its selections.
- A control write without `RegisterControl`, a write to SBO or to a single Oper/SBOw/Cancel member, and a client write of a registered control's CF ctlModel are object-access-denied. Refusals use the 8-1 Table 87 codes (as libiec61850: 12/19/27 temporarily-unavailable, other refusals object-access-denied, malformed type-inconsistent).
- The Oper/SBOw/Cancel decoder is strict (member count and types); a Cancel that carries a trailing Check (models reusing the Oper type) is accepted.
- The default operate writes ctlVal only when it fits stVal (BOOLEAN to Dbpos for a DPC) and updates t, ctlNum, origin and stSeld.
- LastApplError.Error is 1, 2 or 3 per 8-1 §20.11. A granted SBO read returns `LD/LN$CO$DO`, as libiec61850 servers do. `ControlRequest.Operation` is `"cancel"` for a Cancel.
- Client: SelectWithValue followed by Operate or Cancel reuses the ctlNum; operTm (all zero when immediate) is sent when the server's type has it; Cancel carries Test, and Check when the server's Cancel type has one; a LastApplError is matched by ctlNum.

**Server data, setting groups, data sets, types**

- Write refusals carry the DataAccessError of 8-1 Table 23 (type-inconsistent, object-value-invalid, object-access-denied) instead of temporarily-unavailable; structure writes are type-checked; enum values are range-checked.
- `Tcmd` maps to BIT STRING(2), not INT8 (8-1 §8.1.2.3).
- SGCB: the edit reservation belongs to one association (ResvTms, released on association loss); FC=SE access without an edit group is temporarily-unavailable; the member is `NumOfSG` (8-1 Table 60).
- Control blocks are no longer listed as data objects; the LN variable lists FCs in 8-1 Figure 4 order.
- `CreateDataSet` and `ParseRefStrict` check names and lengths (7-2 §22.2, Table 4); `DecodeQuality` accepts short bit strings (8-1 §8.2).

**GOOSE and SV (8-1 §18, Annex A; 9-2)**

- The GOOSE subscriber recognises a publisher restart by a newer `T` (`EventRestart`); `SetEnabled(true)` restarts stNum at 1.
- `NewPublisher` rejects a non-multicast destination, over-long GoCBRef/GoID, MaxTime ≥ 60 s and a TimeAllowedToLive not above MaxTime; `MaxPayload` is 1499; the default TimeQuality is 0x2a (not synchronised).
- A GoCB with an unconfigured or unicast MAC is left disabled with NdsCom TRUE instead of failing `EnableGOOSE`; GoEna is refused while ConfRev is 0; every DatSet write increments ConfRev (and MSVCB ConfRev on MsvID/SmpRate/SmpMod/OptFlds changes); DatSet is stored qualified.
- GSE/SMV addresses without APPID use 0x0000/0x4000; VLAN priority defaults to 4. A unicast SampledValueControl (USVCB) is skipped with a warning.
- The SV subscriber resyncs on an smpCnt reset (`EventResync`); reserved smpSynch values are rejected; SV has the per-stream simulation switch of GOOSE.

**SCL (IEC 61850-6 Ed2, SCL 2007B schemas)**

- New errors: missing required attributes in LN0/DataSet/FCDA/ReportControl/GSEControl/SampledValueControl, `lnclass-mismatch`, XSD uniqueness (`duplicate-name`; a duplicate DataSet is an error), `missing-type`, zero smpRate/nofASDU. New warnings: address formats and ranges (`validate.Addresses`), unresolved FCDA paths and FC mismatches. `Flatten` reports unresolved and cyclic types with a Status row.

### Added

- Log control blocks (FC LG) with the 8-1 Table 65 attributes and write rules, built from SCL LogControl; journal entries carry ReasonCode.
- `OperateParams.CtlNumSet`, `CancelParams.CtlNumSet`, `CancelParams.Test`, `TQuality`; `DPCCtlVal`, `DbposValue`; `Client.WriteDataSet` (SetDataSetValues); association-scoped `@name` data sets; `Client.CancelEditSG`; `SettingGroupInfo.LActTm`; write-refusal sentinels `ErrWriteAccessDenied`, `ErrWriteTemporarilyUnavailable`, `ErrWriteTypeInconsistent`, `ErrWriteValueInvalid`, `ErrWriteHardwareFault`; `TimeAccuracyUnspecified`, `TimeQuality.IsValid`, `Ref.ValidateStrict`.
- GOOSE fixed-length encoding (Annex A.3) for decode and for `FixedOffs=TRUE` GoCBs: `goose.EncodePDUFixedLength`, `EncodeFixedLength`; `PublisherConfig.TimeQuality/FixedOffs/MemberTypes`, `GOOSEOptions.TimeQuality`.
- `sv.EventResync`, `sv.ResyncAfter`, `Stats.Resyncs`, `sv.ReservedSmpSynch`; `validate.Addresses`, `validate.Options.SkipAddresses`.

### Deprecated

- `Client.ReadLastApplError` (LastApplError is a transient VMD variable; use `ControlError.LastApplError`) and `DpCtlVal` (a DPC ctlVal is BOOLEAN; use `DPCCtlVal`).

### Tests

- `TestSpec_*` tests named after their clause for reporting, logging, control, setting groups, data sets, types, GOOSE, SV and SCL, with byte vectors for wire formats.
- Chaos and race tests with goroutine-leak checks: URCB reservation races, disconnect during reporting, replay racing live reports, six clients racing a control, LCB chaos.
- New fuzz targets `FuzzAssembleSegments`, `FuzzDecodeRCB`, `FuzzDecodeControlRequest`, `FuzzEncodeFixedLength`.
- Assertions that contradicted the standard were rewritten, each citing the clause in its commit.

### Notes

- The SCL corpus manifest (recorded with sclparse v1.13.1) needs regenerating with this release: `multi_substation-S1IED1.icd` (GSEControl written `Name=`) and libiec61850's `demos-beagle_demo.icd`/`.iid` (LN FSCC with an FSCH LNodeType) now get one genuine error each.

---

## v1.16.2

**Date:** 2026-11-10
**Previous release:** v1.16.1

### Changed

- **Built on go-mms v1.7.2** (was v1.7.1). A TimeOfDay (BinaryTime) whose millisecond field is one day or more now decodes instead of failing the whole PDU: ISO 9506-2:2003 §7.5.1 only requires bits 28–31 to be zero, so a 4-octet value keeps the field as is (up to 2^28−1 ms, a deadline past midnight) and a 6-octet value carries it into the following days. A field with bits 28–31 set is rejected (otfabric/go-mms#30). This applies to BinaryTime data attributes and to report and log entry times (TimeOfEntry, EntryTime). `mms.NewBinaryTime` now also sends 0 to 2^28−1 in the 4-octet form and instants after 2163-06-05 on the last day the form carries.

---

## v1.16.1

**Date:** 2026-11-10
**Previous release:** v1.16.0

### Fixed

- **SCL: an address written `<P type="IP" xsi:type="tP_IP">` keeps its type.** encoding/xml binds an attribute named `type` whatever its namespace, so the `xsi:type` after `type`, the order ABB's tools write, replaced the address type with `tP_IP`: the IP addresses, subnets, gateways, OSI selectors and GOOSE and SV MAC addresses of such files were lost (every anonymized ABB CID in `scl/testdata/abb` was affected). Attributes of the XML Schema instance namespace are now left out before binding; no binding reads one.
- **SCL: an optional boolean attribute whose schema default is true reads true when absent.** The bindings had no way to tell an absent attribute from false, so a file that leaves out the default read `gi` of TrgOps, `bufOvfl` of OptFields, `multicast` of an SMV control block and `modify` of ConfDataSet as false. The generator now emits these attributes as `*bool` and the converters apply the default (`default="true"` in the schemas).
- **SCL: an Edition 1 (schema 1.7) report control block allows general interrogation.** The 2003 schema's TrgOps has no `gi` attribute, so GI was always false; the 2007 schemas added `gi` with default true, which an Edition 1 file cannot turn off.

### Changed

- `make scl-generate` and `make scl-check-generate` run the generator of the checkout (`scl/internal/genir`) instead of the one of the released library that the nested `sclgen` module pins, so the check compares the bindings with the generator in the same commit.

### Tests

- `TestXSITypeAfterType` and `TestABBAddresses` (every ABB fixture keeps a valid IP address), `TestDefaultTrueAttributes`, `TestEdition1GI`, `TestEmitAttrField_DefaultTrueBoolean`. Found by the demo set of otfabric/iec61850-explorer.

---

## v1.16.0

**Date:** 2026-11-10
**Previous release:** v1.15.2

### Added

- `DecodeReport(values []*mms.Value) (*ReportIndication, error)` decodes the access results of an InformationReport that carries an IEC 61850 report, for reports not received through a subscription: those of a capture decoded with go-mms `mms.DecodeTSDU`, as the trace analysis of otfabric/iec61850-explorer does. It is strict: a missing or nil value, a field of the wrong MMS type and values left over after the last member are a `*ReportError`. A `Client` decoding the reports it subscribed to is unchanged.

### Tests

- `TestDecodeReportRoundTrip` decodes what the server's report engine encodes, for every optional field, and checks it against the client's decoder; `TestDecodeReportErrors` covers every field missing, nil or of the wrong type, and values left over. `FuzzDecodeReport` (4.6 million inputs locally, no failure) is in `make fuzz`.

---

## v1.15.2

**Date:** 2026-11-09
**Previous release:** v1.15.1

### Changed

- The bundled command-line tools (`sclparse`, `sclgen`) are built on v1.15.1 (they were on v1.15.0). Neither links go-mms, so their behaviour is the same as in v1.15.1. Library code is the same as v1.15.1.

---

## v1.15.1

**Date:** 2026-11-09
**Previous release:** v1.15.0

### Fixed

- go-mms v1.7.1 (was v1.4.0): `Client.Close` is bounded by its context. Against a peer that stopped reading, such as after a corrupted or hostile byte stream, `Close` could block until the connection failed (otfabric/go-mms#34). The client also closes the connection as soon as the MMS reader stops.
- Through go-mms v1.7.0: a GetVariableAccessAttributes response that encodes the optional `address` as a primitive (`0x81`, as some servers send it) is now accepted; before, `Client.GetVariableType` failed with "expected typeSpecification".

### Added

- go-mms v1.5.0 to v1.7.0 add `mms.DialOptions.AllowedServices`, reachable through `DialOptions.MMS`, and passive decoding (`mms.DecodeTSDU`, `mms.DecodePDU`). The go-iec61850 API is unchanged.

### Tests

- `TestChaosDialerByteFaults` tears down with `Close`, which must return within its context; it used `Abort` to avoid the go-mms v1.4.0 behaviour.

### Changed

- The bundled command-line tools (`sclparse`, `sclgen`) are built on v1.15.0 (they were on v1.13.0). `sclparse` now carries the v1.14.0 SCL change: every `Val` of an attribute is kept with its setting group, so `sclparse dump-json` writes `Vals` (value and `SGroup`) besides `Val`. Apart from the go-mms version, library code is the same as v1.15.0.

---

## v1.15.0

**Date:** 2026-11-09
**Previous release:** v1.14.0

### Added

- `DialOptions.Dialer` sets the function that opens the TCP connection of `Dial` (passed to go-mms `iso.WithDialer`), for a destination allowlist checked against the resolved IP, a proxy, custom socket options or an in-memory transport. It is called once per attempt; its error is wrapped, so `errors.Is` matches it. Nil keeps the default `net.Dialer`.

---

## v1.14.0

**Date:** 2026-11-09
**Previous release:** v1.13.1

### Added

- `scl.DAI`, `scl.DA` and `scl.BDA` have `Vals []scl.Value`: every `Val` element in document order, with its setting group (`sGroup`, IEC 61850-6) for a setting. Before, only the first value was kept (`Val`, unchanged), so the values of the other setting groups were lost.

### Changed

- `scl.Generate` writes every value of `Vals` with its `sGroup`, or `Val` when a model built in code sets only that. An attribute without a value is now written without an empty `Val` element.

---

## v1.13.1

**Date:** 2026-11-09
**Previous release:** v1.13.0

### Changed

- The bundled command-line tools (`sclparse`, `sclgen`) are built on v1.13.0. Library code is the same as v1.13.0.
- `sclparse --assume-namespace` (global flag) reads an SCL file whose root element declares no namespace as the IEC 61850-6 namespace, with a `namespace-assumed` warning (`ParseOptions.AssumeSCLNamespace`). Without it such a file is still refused with exit code 3. It applies to every subcommand that parses the file; `detect` reports what the file declares.
- `sclparse inspect` takes the schema version from the parse result instead of detecting it separately.

---

## v1.13.0

**Date:** 2026-11-09
**Previous release:** v1.12.1

### Added

- `scl.ParseOptions.AssumeSCLNamespace` parses an `SCL` root element without a namespace as if it declared the IEC 61850-6 namespace, with a `namespace-assumed` warning; without version attributes the document is read as schema 1.7. Some tools write such files and other stacks accept them. By default they are still refused, as the schema requires the namespace; a root in another namespace is refused either way.

---

## v1.12.1

**Date:** 2026-11-09
**Previous release:** v1.12.0

### Changed

- The bundled command-line tools (`sclparse`, `sclgen`) are built on v1.12.0, so `sclparse validate` reports a missing `Header` element and reads SCL declared as `utf8`. Library code is the same as v1.12.0.
- Tests cover the decode errors of every schema version and the `MaxDiagnostics` truncation (the partially covered lines of `scl/parse.go`).

---

## v1.12.0

**Date:** 2026-11-09
**Previous release:** v1.11.1

### Added

- `ServerOptions.AllowEmptyReportDatSet` serves report control blocks that have no data set (SCL `datSet` absent or empty), which SCL and IEC 61850-7-2 allow: a client writes `DatSet` before it enables the RCB. By default such a model is still refused. Enabling an RCB whose `DatSet` is empty is refused either way.
- `DocumentMetadata.HasHeader` reports whether a parsed document has a `Header` element. `scl.ValidateRequired` reports a parsed document without one (`missing-element`, error). An empty Header `id` stays valid, and a model built in code is not checked for it.

### Fixed

- SCL files that declare UTF-8 under another name (`encoding='utf8'`, as some tools write), or US-ASCII, were refused with "no SCL root element found". They now parse. Any other encoding is refused with an error that names it.

---

## v1.11.1

**Date:** 2026-11-09
**Previous release:** v1.11.0

### Changed

- The bundled command-line tools (`sclparse`, `sclgen`) are built on v1.11.0. Before, they were built on v1.9.0, so the binaries of v1.10.0 to v1.11.0 lacked that release's library changes. `sclparse validate` now parses unknown schema tuples with a `schema-fallback` warning and reports missing required attributes and file-kind content. Library code is the same as v1.11.0.

---

## v1.11.0

**Date:** 2026-11-09
**Previous release:** v1.10.1

### Added

- `scl.ValidateRequired` reports required attributes that are missing or empty (`missing-attribute`, error). These are the attributes the IEC 61850-6 schemas declare `use="required"` with a type that cannot be empty, such as the `name` of a Substation, VoltageLevel, Bay, IED or AccessPoint, LDevice `inst`, LN `lnClass` and `lnType`, FCDA `fc`, and the `id`, `cdc`, `bType` and `fc` attributes of the type templates.
- `scl.ValidateKind` reports content a file kind needs (`kind-content`):
  - an ICD, IID or CID without an IED is an error;
  - an ICD or IID with more than one IED, and an SSD without a substation, are warnings.
- `ParseOptions.ValidateSemantic` applies both, the kind rule with the parse's kind. The `scl/validate` package runs `ValidateRequired` as a pass (`Options.SkipRequired`) and `ValidateKind` when `Options.Kind` is set.

### Changed

- A document in the IEC namespace whose version tuple is not one of the supported schemas is parsed instead of refused: 2007A uses the 2007B bindings, other releases of revision B use 2007B4, revision C or later and versions other than 2007 use 2007C5. Detection still reports the schema as unknown, and the parse adds a `schema-fallback` warning; elements the chosen bindings do not define are ignored. A foreign namespace or a malformed release is still refused.

Found with the OT Fabric SCL corpus: OpenSCD's 2007B5 and 2019B1 test files and libiec61850's 2007A examples now parse; a Substation without a name and an IID without an IED are now reported.

---

## v1.10.1

**Date:** 2026-11-09
**Previous release:** v1.10.0

### Fixed

- The Check attribute of a control (Oper, SBOw) had its two bits swapped on the wire. IEC 61850-8-1 defines Check as BIT STRING { synchrocheck (0), interlock-check (1) }, and libiec61850 reads bit 0 (the first, 0x80) as synchrocheck. go-iec61850 sent `CheckSynchroCheck` in bit 1 and `CheckInterlockCheck` in bit 0, and its server decoded them the same way, so a go-iec61850 client and server agreed with each other but not with other stacks: an IED asked for an interlock check got a synchrocheck request, and the other way round. Both directions now use the standard positions. A go-iec61850 client and server must be updated together to keep agreeing.

---

## v1.10.0

**Date:** 2026-11-09
**Previous release:** v1.9.4

### Added

- `Client.OperateWait` operates a control with enhanced security and waits for its CommandTermination (IEC 61850-8-1). It returns a `CommandTermination`; a negative termination also returns a `ControlError` wrapping the new `ErrCommandTerminationNegative`, with the AddCause and LastApplError the server reported.
- A refused `Operate`, `SelectWithValue` or `Cancel` returns a `ControlError` with the `LastApplError` the server sent before the negative write response (new field `ControlError.LastApplError`), and its `AddCause`.
- Server: a refused control write (Oper, SBOw, Cancel) is preceded by a LastApplError InformationReport, as libiec61850 does. `ControlHandler.OnExecute` runs after a successful enhanced-security operate: an error turns the CommandTermination negative (LastApplError and Oper).
- `LastApplError.CtlNum`, and the AddCause values 18 to 27 (`AddCauseObjectNotSelected` … `AddCauseLockedByOtherClient`).
- Interop: `OperateWait` receives the positive CommandTermination of libiec61850, dscsystems, iec61850-rust and cbsigma; the LastApplError of a refused operate is decoded from libiec61850, dscsystems and cbsigma (`interop/termination_test.go`).

### Fixed

- `AddCause` values 8 to 17 followed no standard. They now follow IEC 61850-7-2 and libiec61850: for example `AddCauseBlockedByInterlocking` is 10 (was 9), `AddCauseBlockedByMode` 8 (was 16). Code that compares against the named constants keeps working; code that stored raw numbers must be checked. `AddCauseBlockedBySwitch`, `AddCause1of1` and `AddCauseAbort` are deprecated aliases of `AddCauseBlocked`, `AddCause1ofN` and `AddCauseAbortionByCancel`; `String` returns the names of the standard.
- The LastApplError decoder read the fourth member as AddCause. Servers send five members (CntrlObj, Error, Origin, ctlNum, AddCause), so the control number was reported as the cause. The four-member layout is still accepted.
- Server: an operate is refused with AddCause object-not-selected when there is no valid select (was select-failed), locked-by-other-client when another client holds it, and inconsistent-parameters when ctlNum or ctlVal differ from the select (was select-failed and parameter-change-in-execution). A select held by another client is refused with object-already-selected. These match libiec61850.

### Changed

- The client installs its InformationReport handler on the MMS client when it first sends a control, as it already did for report subscriptions, replacing a handler set on an `mms.Client` passed to `NewClient`.

---

## v1.9.4

**Date:** 2026-11-09
**Previous release:** v1.9.3

### Fixed

- Server reports: a data set member that names a data object or a constructed attribute (an FCD, such as `MMXU1.Hz[MX]` or `Mod[ST]`) is reported as the structure of its attributes of that functional constraint, as GOOSE already did; it was sent as `false`. A change, quality change or update of any attribute below such a member now triggers the report, with every member that holds the attribute included.
- Server reports: a member attribute that has no value yet is reported with its type's default, the value a client reads, instead of a missing value. Writing that default is no longer reported as a data change.

---

## v1.9.3

**Date:** 2026-11-08
**Previous release:** v1.9.2

### Changed

- No changes. v1.9.3 was released by mistake from the same commit as v1.9.2, a few seconds later; the library and `sclgen` and `sclparse` are identical to v1.9.2.

---

## v1.9.2

**Date:** 2026-11-08
**Previous release:** v1.9.1

### Changed

- `sclgen` and `sclparse` are built with the latest Go 1.26 patch release; v1.9.1 was built with go1.26.0.

---

## v1.9.1

**Date:** 2026-11-07
**Previous release:** v1.9.0

### Changed

- The bundled command-line tools are built on v1.9.0. Library code is the same as v1.9.0.

---

## v1.9.0

**Date:** 2026-11-07
**Previous release:** v1.8.1

### Changed

- `sclparse version` and `sclgen version` print the organisation's one-line format: `sclparse 1.9.0 (tag v1.9.0, commit <full sha>, built <RFC 3339 UTC>, go1.26.3, linux/amd64)` (was `sclparse v1.9.0 (v1.9.0) commit 1802d0f9 built …`). The version has no leading `v`; the `tag …,` part appears only for a release build; the commit is the full SHA. A build without release ldflags reports `dev` (the module version for `go install …@vX.Y.Z`; a pseudo-version or `+dirty` version is `dev`), the commit from the Go build information (`-dirty` for a modified tree) and `built unknown`.
- `make build` injects the full commit SHA, and the version (without `v`) and tag only when HEAD is exactly on a tag; otherwise the binaries report `dev`.
- `sclgen --versions` help lists the editions (`v17`, `v2007b`, `v2007b4`, `v2007c5`).

### Added

- `sclparse version --json` and `sclgen version --json`: one line of JSON with the keys `name`, `version`, `tag`, `commit`, `built`, `libraries`, `go`, `platform`, always in that order. `libraries` lists the other OT Fabric `go-*` libraries the binary is built on (not go-iec61850 itself; `[]` today); in text they follow `built …`.
- Shell completion of flag values: `sclgen --versions` completes `all` and the editions (comma lists too), `sclparse dump-json --output` completes `.json` files, `sclparse validate --max-errors` completes no file names. A test fails when a flag that takes a value has no completion. The README has one Shell completion section for both tools (bash, zsh, fish, PowerShell).

The library API is unchanged.

---

## v1.8.1

**Date:** 2026-11-07
**Previous release:** v1.8.0

### Changed

- `sclparse` and `sclgen` are built on go-iec61850 v1.8.0; they were on v1.4.5, so their binaries now carry the `scl` changes of v1.5.0 to v1.8.0, including the SAST fixes in `scl`.

---

## v1.8.0

**Date:** 2026-11-07
**Previous release:** v1.7.0

### Breaking

- **`sclparse` and `sclgen` exit with the organisation exit codes** (0 success, 1 findings, 2 usage, 3 invalid input, 10 internal/output, 130 interrupted); see `scl/cmd/sclparse/docs/exitcodes.md` and `scl/cmd/sclgen/docs/exitcodes.md`. A script that tests the exit status changes as follows.
  - `sclparse`: usage error 1 → 2; the SCL file cannot be read or parsed 2 → 3; the output cannot be written (`dump-json --output`, JSON to stdout) 2 → 10; `validate` found validation errors 3 → 1; interrupted (SIGINT/SIGTERM) → 130.
  - `sclgen`: everything was 1. Now `check` found out-of-date files stays 1; usage error (unknown flag, missing `--spec-root` or `--out`, no edition matches `--versions`) 1 → 2; a schema bundle cannot be parsed or resolved 1 → 3; the output cannot be written 1 → 10; interrupted → 130.

### Fixed

- `sclgen` prints a usage error such as a missing `--spec-root` to stderr; it exited silently.
- The `sclgen` help examples use `--spec-root` and `--out`; the single-dash `-spec-root` / `-out` they showed is rejected by the flag parser.

### Tests

- `sclparse` and `sclgen`: the exit-code classification (error → code) and each code a command produces, driving the root command in-process; the `sclgen` help examples parse.

---

## v1.7.0

**Date:** 2026-11-07
**Previous release:** v1.6.0

### Breaking

- **A server built from SCL names its logical devices as IEC 61850-8-1 does**, as the other IEC 61850 stacks do: the MMS domain is LDevice@ldName when the SCL gives one, otherwise the IED name followed by LDevice@inst (`InteropIEDInteropLD`). It was the bare instance (`InteropLD`). Data set members, report control block `DatSet`, GoCB and MSVCB references, the published `gocbRef`/`datSet`/`svID` and the keys of `Server.SetValue` and `RegisterControl` follow. A client that takes the logical devices from `ListLogicalDevices` (GetServerDirectory) is not affected; one that addresses `InteropLD/...` directly uses `DialOptions.IEDName`, the new name, or builds the server with `NewServerModelFromSCLWithOptions(..., ServerModelOptions{InstanceDomainNames: true})`, which keeps the old names. Setting `ldName` in the SCL is the standard way to give a device a short name. Released as a minor version at the maintainer's choice.

### Fixed

- **Reports carry the data references their OptFlds announce.** With the data-reference bit of OptFlds set, a report left the references out, so a client that parses the IEC 61850-8-1 report format read the first value as a reference: cB-Sigma/iec61850's client refused it ("the report's data reference is not a visible-string"). The server now sends one reference (`<LD>/<LN>$<FC>$<DO>...`) per included member between the inclusion bitstring and the values, in live reports and in replayed BRCB entries. Found with cB-Sigma's client in iec61850-interop.

### Added

- `NewServerModelFromSCLWithOptions` and `ServerModelOptions.InstanceDomainNames`.
- `scl.LDevice.LdName`: LDevice@ldName is parsed (schema 2007B and later) and written by the SCL generator.

### Tests

- A control write (`Oper`, `SBOw`, `Cancel`) to an object without a control structure is answered object-non-existent and leaves the status alone, even with `AllowReadOnlyFCWrites` (dscsystems/go-iec61850 v0.6.6 fixed the opposite in its server; go-iec61850 was not affected).
- Data references in live and replayed reports, and their place in the report.
- Domain names: IED name plus instance by default, ldName honoured, data set members across logical devices, duplicate names refused, `InstanceDomainNames`.
- Interop: the suite runs against the iec61850-interop v1.4.0 images, whose reporters and controllers take the logical device from the server directory (contract 1.3); the go server is addressed as `InteropIEDInteropLD`, and the data set of each peer server is read by its list name with and without `specificationWithResult` (iec61850bean answers only the latter, a registered limitation).

---

## v1.6.0

**Date:** 2026-11-04
**Previous release:** v1.5.0

### Fixed

- **A client is accepted by IEDs that require transport selectors** (otfabric/go-iec61850#28, an ABB REB500 that answered `iec61850ctl` with "cotp: connection refused (reason 3)"). The COTP CR carried no calling or called T-selector and no TPDU size, and some IEDs refuse such a CR with a Disconnect Request, reason 3 (address unknown). With go-mms v1.4.0, `Dial` sends the addressing libiec61850 sends: T-selector `0001`/`0001` and TPDU size 1024 in the CR, S-selector `0001`, P-selector `00000001`, AP-titles `1.1.1.999` / `1.1.1.999.1` and AE-qualifier 12. A non-nil empty value in `DialOptions.MMS` omits a parameter again. A CC whose responding T-selector differs from the called one is accepted (X.224 §13.3.4), and a server returns the CR's T-selectors in the CC.

### Added

- `ParseSelector`, `ParseAPTitle` and `ApplySCLAddress`: dial an IED with the OSI-TSEL, OSI-SSEL, OSI-PSEL, OSI-AP-Title and OSI-AE-Qualifier of its SCL ConnectedAP (IEC 61850-8-1 Table 132). Constants `PTypeOSITSEL`, `PTypeOSISSEL`, `PTypeOSIPSEL`, `PTypeOSIAPTitle`, `PTypeOSIAEQualifier`.
- `DialOptions.MaxTPDULength`: the COTP TPDU size proposed in the CR (default 1024; 65531 omits it).

### Tests

- Interop (`interop/tsap_test.go`): a strict COTP gate in front of each peer IED server accepts the default client and refuses one without a called T-selector with reason 3, reproducing #28; a gate requiring `00000001` refuses the default and accepts a configured client. Independent clients (libiec61850, iec61850-rust, iec61850bean; iec61850-interop v1.2.0 `--local-tsel`/`--remote-tsel`) with their default, 1-octet and 4-octet T-selectors against a go server that requires its selector, and a mismatch refused.
- Unit tests for the selector and AP-title parsers and the SCL address mapping (the ConnectedAP example of IEC 61850-8-1).

### Dependencies

- go-mms v1.4.0, go-cotp v1.3.0.

---

## v1.5.0

**Date:** 2026-11-04
**Previous release:** v1.4.5

### Breaking

- Requires Go 1.26 (was 1.25). Built on go-mms v1.3.2 (server model registration no longer quadratic in the number of variables), go-cotp v1.2.0 and go-tpkt v1.1.0. CI tests Go 1.26 and 1.27.
- A server built from a model refuses client writes to read-only functional constraints: ST, MX, SG, SR, OR and EX answer object-access-denied (IEC 61850-7-2). Before, any MMS client could overwrite status and measurements, for example a breaker's `Pos.stVal`. The application changes them with `Server.SetValue`; a simulator or test fixture whose peers drive status over MMS sets `ServerOptions.AllowReadOnlyFCWrites` (the interop suite does).

### Fixed

- Models from vendor SCL could not be served: ObjRef attributes (VISIBLE STRING129) were refused, Dbpos values given as literals (`off`, `on`, `intermediate-state`, `bad-state`) were refused, and a numeric enum literal such as ABB's baud rate `9600` (ordinal 6) was read as ordinal 9600. Every model of the SCL test corpus in a supported schema version is now served.
- GOOSE and SV: a UtcTime before 1970 or after 2106-02-07 was wrapped on the wire (one second before the epoch went out as 2106); encoding now fails with `ErrInvalidValue`. The zero `time.Time` is written as the epoch.
- `GetGoCB` and `GetMSVCB` truncated attributes outside their IEC 61850-7-2 types (ConfRev 2^32 read as 0, SmpRate 70000 as 4464, PRIORITY 256 as 0); they now return `ErrProtocol`.
- Server: a GoCB whose ConfRev passed 2^32-1 published confRev 0; enabling it is now refused. An MSVCB `SmpRate` write above 65535 was stored and published truncated; it is refused with object-value-invalid.
- Controls: a ctlNum above 255 in Oper, SBOw or Cancel was truncated and could match an unrelated Select; it is rejected.
- Setting groups: ActSG or EditSG writes above 255 wrapped (ActSG=257 activated group 1); the client truncated NumOfSGs and ResvTms; SCL `numOfSGs`/`actSG` above 255 were wrapped silently and now give the error diagnostic `invalid-setting-control`.
- Reports: a peer that kept sending segments with MoreSegmentsFollow grew the reassembly buffer without bound; it is capped at 65536 segments.
- `Quality.WithValidity` with a value outside 0..3 overwrote detail-quality bits; only the two validity bits are used.
- `Server.Close` cancels and waits for pending CommandTerminations, which ran untracked; CommandTermination and report sends are bounded by 10 s and stop with `ReportEngine.Stop`.
- The files example created downloads with mode 0644 under a server-chosen name; it now creates them 0600 inside the output directory (`os.OpenRoot`). The XSD bundle parser confines `xs:include` to the bundle directory.

### Changed

- Raw socket transports decode `tpacket_auxdata` and `bpf_hdr` without unsafe pointer casts; static analysis findings resolved in code (gosec low/low 116 -> 2, the two left are the seeded RNG of the in-memory fault hub).
- Interop CI pins the iec61850-interop v1.1.1 images; shared workflows at v3.3.1. `go fix` (Go 1.26) applied.
- Tests: end-to-end tests that serve every SCL model of the corpus and read every LN$FC, and that refuse hostile client writes; codec boundary tests for GOOSE, SV, BER, UtcTime and control blocks; fuzz targets `FuzzDecodePDU` (GOOSE) and `FuzzLayoutDecode` (SV).

---

## v1.4.5

**Date:** 2026-10-31
**Previous release:** v1.4.4

### Changed

- The bundled command-line tools are built on v1.4.4. Library code is the same as v1.4.4.

---

## v1.4.4

**Date:** 2026-10-31
**Previous release:** v1.4.3

### Changed

- Tests, CI and documents only. `make coverage-gate` is now `make coverage-pkg-check`, and `make build-all` writes to `release/`.
- Built on go-mms v1.2.4, go-cotp v1.1.1, go-tpkt v1.0.2.

---

## v1.4.3

**Date:** 2026-10-31
**Previous release:** v1.4.2

### Changed

- The bundled command-line tools are built on v1.4.2. Library code is the same as v1.4.2.

---

## v1.4.2

**Date:** 2026-10-31
**Previous release:** v1.4.1

### Changed

- Tests, CI and documents only. `make coverage-gate` is now `make coverage-pkg-check`, and `make build-all` writes to `release/`.
- Built on go-mms v1.2.3, go-cotp v1.1.1, go-tpkt v1.0.2.

---

## v1.4.1

**Date:** 2026-10-30
**Previous release:** v1.4.0

### Fixed

- `sclgen` and `sclparse` binaries are published again. Their nested modules still declared Go 1.24, so the builds for v1.4.0 failed and that release has no binaries. The library is unchanged.

---

## v1.4.0

**Date:** 2026-10-30
**Previous release:** v1.3.0

### Dependencies

- `golang.org/x/sys` → v0.44.0 (govulncheck). **The minimum Go version rises from 1.24 to 1.25**, which that version requires.

---

## v1.3.0

**Date:** 2026-10-30
**Previous release:** v1.2.2

### Changed

- **`scl/cmd/sclgen` and `scl/cmd/sclparse` are now nested modules.** The
  library's `go.mod` no longer requires cobra, so consumers stop inheriting it.
  Library import paths and the exported API are unchanged.
  - Release binaries are published as before.
  - Build from a checkout with `make build`, or `cd scl/cmd/<tool> && go build .`.
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
