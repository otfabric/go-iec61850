# Known Limitations

What `go-iec61850` does not do, or does only partly, as of v1.2. Each item
is a deliberate scope decision or a documented gap; behaviour that deviates
from a peer stack is listed in [INTEROP.md](INTEROP.md) and
[COMPATIBILITY.md](COMPATIBILITY.md).

## Protocol scope

- **MMS, GOOSE and Sampled Values** — this library implements IEC 61850
  over MMS (ISO 9506), GOOSE (IEC 61850-8-1, see [GOOSE.md](GOOSE.md)) and
  Sampled Values (IEC 61850-9-2, see [SV.md](SV.md)), and both over
  UDP/IP as R-GOOSE/R-SV (IEC TR 61850-90-5 session protocol, package
  `rsession`, see [GOOSE.md](GOOSE.md#r-goose-and-r-sv-over-udp)).
- **No ACSI mapping completeness guarantee** — not every ACSI service defined
  in IEC 61850-7-2 is mapped. The library focuses on the services needed for
  practical client/server use cases.

## Client limitations

- **Control: per-client sequence state** — the control models (direct,
  SBO, SBOw, normal and enhanced security) are typed APIs (`Operate`,
  `Select`, `SelectWithValue`, `Cancel`, and `OperateWait`, which waits for
  the CommandTermination). An Operate or Cancel without a ctlNum reuses
  the ctlNum of the last `SelectWithValue` of that control on the same
  `Client` only. Whether operTm is sent is learned once per control with
  GetVariableAccessAttributes; when the server does not answer it, operTm
  is sent only for a time-activated operate (non-zero `OperTm`).
  `ReadLastApplError` is deprecated: LastApplError is a transient
  VMD-specific variable (IEC 61850-8-1 20.11), so it returns the last one
  this client received. Once the client sends a control or subscribes to
  reports, it owns the MMS client's InformationReport handler.
- **Data sets: SetDataSetValues is one MMS Write** —
  `WriteDataSet` sends one Write of the named variable list; the server
  decides per member. `@name` data sets are scoped to the association and
  never cached.
- **No automatic model discovery caching persistence** — the client cache is
  in-memory only and does not survive process restarts.
- **`DialOptions.Dialer` covers only the TCP connect** — the COTP, ACSE
  and MMS handshakes run on the returned connection; TLS
  (`iso.WithTLSConfig`) is not reachable from `DialOptions`: dial with
  go-mms `iso.Dial` (`iso.WithDialer`, `iso.WithTLSConfig`) and wrap the
  result with `NewClient`, or return a `tls.Conn` from the dialer. `Client` built with
  `NewClient` dials nothing, and the server ignores the dialer.
- **One subscription per RptID** — the client routes InformationReports
  to subscriptions by RptID. The server, not the client, arbitrates RCB
  ownership between associations (see the report limitations below).

## Server limitations

- **Reports: no persistent BRCB buffer, no ResvTms/Owner** — the report
  engine implements the IEC 61850-7-2 §17.2 URCB and BRCB state machines
  per association, including BufTm (see API.md "Report engine"), with
  these limits:
  - The default BRCB buffer is in memory (`NewMemoryReportBuffer`: 1000
    entries, optionally a byte limit) and does not survive a server
    restart; EntryIDs restart at 1. No persistent `ReportBuffer` ships;
    one supplied through `ServerOptions.NewReportBuffer` is continued
    after a restart.
  - BufTm treats FC=MX members like FC=ST: a second change of a member
    within BufTm sends the pending report. The alternative the standard
    allows for MX, replacing the value in the pending report, is not
    offered.
  - The optional BRCB attributes ResvTms and Owner (IEC 61850-7-2
    Table 37 c3/c4) are not modelled: a BRCB is reserved only by the
    association that enables it, and released when it disables it or the
    association ends.
  - Segmentation uses the server's configured MMS `MaxPDUSize` (default
    65000), not the size negotiated with each client, which go-mms does
    not expose per connection. A data set member whose value does not fit
    one PDU is not reported.
  - Association loss is detected when `Server.Serve` returns, when a
    report cannot be sent, and by a one-second check of the go-mms
    connection list; a connection served by `mms.Server` directly can keep
    its RCBs up to one second.
  - An indexed ReportControl with ClientLN entries creates the instances
    `<name>01..<name>NN` but does not pre-assign them to those clients.
  - Without attribute trigger options in the model (SCL DA
    dchg/qchg/dupd), the reason for inclusion is derived by comparing the
    member value.
  - A client write of a constructed attribute (for example
    `LN$CF$DO$cmp`) stores and notifies its leaves one by one, as
    `SetValue` would: each changed leaf of a data set member is a GOOSE
    state change and a report of its own, also with BufTm above 0 (a
    second change of a member sends the pending report), and a
    concurrent reader can see the structure partly written.
- **Controls: process response is a model, not supervision** — control
  handlers (`RegisterControl`) follow the states of IEC 61850-7-2 Table 47
  (select, tests, time-activated wait, execution until the
  CommandTermination), with CF sboTimeout, sboClass and operTimeout. The
  Check conditions run when the request asks for them and the
  registration supplies them (`CheckInterlock`, a CILO via
  `Interlocking`, `CheckSynchro`, an RSYN via `Synchrocheck`): the
  interlocking algorithm itself (which positions a CILO depends on) is
  the application's, and the CILO and RSYN values are read from the
  value store, which the application or a GOOSE subscription keeps
  current; a waiting synchrocheck reads the RSYN release every 10 ms.
  The Select of SBO with normal security (a Read of SBO) carries no
  Check, so no check runs at it. Other operative tests of 7-2 20.2.1
  (Beh, blocking, local/remote) are not evaluated; use `OnSelect` and
  `OnOperate`. Without `OnOperate` the server mirrors ctlVal into
  `stVal` (BOOLEAN to Dbpos for a DPC) when it fits stVal's type, with
  `t`, `ctlNum` and `origin`; the opt-in `ProcessResponse` adds a DPC's
  travel through intermediate-state, an operation counter and
  position-reached, nothing else (no Beh mirror, pulse output or
  OpOpn/OpCls). APC, BSC and other CDCs need an `OnOperate`. A Cancel of
  a time activated operate before operTm sends no
  TimeActivatedOperate termination. A selection held by an association
  that ended is released when its `Server.Serve` returns (also for
  `ListenAndServe`, which serves every connection through it); for a
  connection given to `mms.Server` directly, it is released at the next
  control service on that object, and `stSeld` stays TRUE until then. Choices where the standard is
  ambiguous follow libiec61850; see [INTEROP.md](INTEROP.md#control-model-choices).
- **Setting groups: value banks in memory** — the server keeps one value
  bank per setting group (IEC 61850-7-2 §16, built from SCL `Val sGroup`)
  with the SGCB services and the edit reservation per association. Limits:
  - The banks are in memory. IEC 61850-8-1 §16.1 wants the groups
    non-volatile; an application persists them in `OnConfirmEdit` and
    restores them with `SettingGroupEngine.SetSettingGroupValue`.
  - A group switch changes each setting with its own `SetValue`: with
    BufTm 0 every changed setting is a report and a GOOSE state change
    of its own, not one per switch.
  - SetEditSGValue of a whole data object (an FCD, `LN$SE$DO`) is refused
    (object-access-denied); FCDA writes, leaf or constructed, work.
  - `Val sGroup` is read on DAI elements, not on DA/BDA template values.
  - No independent stack in the pinned iec61850-interop images exercises
    the SGCB services, so the banks have own tests only.
- **Journals: in memory, no EVENT-DATA entries** — log control blocks
  (FC LG, IEC 61850-8-1 §17.3) select what is journaled; the journal
  itself has these limits:
  - `MemoryJournalProvider` keeps entries in memory; "LogEntries shall be
    non-volatile" (8-1 §17.3.3.4.1) is up to a persistent provider.
  - LogEna transitions and power-up are not journaled as EVENT-DATA
    Event Transition Records (8-1 §17.3.3.4.3): go-mms journal entries
    carry DATA only.
  - The SCL LogControl `reasonCode` attribute is kept in the model but
    does not change the journal: IEC 61850-8-1 Table 65 does not map
    OptFlds and "forces the reasonCodes to always appear in the Log".
  - The LCB status attributes (OldEntrTm, NewEntrTm, OldEnt, NewEnt) are
    updated when the journal engine adds an entry; entries added with
    `MemoryJournalProvider.AddEntry` directly show up there with the next
    one.
  - Control operations are not journaled automatically.
- **File serving requires external provider** — file MMS services are
  enabled by setting `ServerOptions.FileProvider` (or `MMS.FileProvider`).
  No built-in filesystem-backed provider is included; applications must
  implement the `mms.FileProvider` interface.
- **No dynamic server model updates** — the server model is immutable after
  construction. Named variable lists that clients define (domain or
  association scope) are kept by go-mms in memory and are not persisted.

## GOOSE and Sampled Values

- **No layer-2 IEC 62351-6 security** — layer-2 GOOSE and SV frames are
  neither signed nor encrypted: the 62351-6 extension (the security bits
  of Reserved 1, Reserved 2 and the extension after the APDU) is not
  implemented, because IEC 62351-6 was not available to implement it from
  ([ADR 0001](docs/adr/0001-routable-goose-sv.md#layer-2-iec-62351-6-extension-in-reserved-12));
  the reserved fields and the SV `security` field are carried opaquely.
  Protect the process bus at the network layer (VLANs, port security,
  separate switches), or use R-GOOSE/R-SV.
- **R-GOOSE / R-SV: authentication only, unverified layout** — package
  `rsession` signs and verifies SPDUs with HMAC-SHA256 (80, 128, 256 bits).
  Not implemented: AES-GMAC, AES-GCM encryption, tunnelled and management
  SPDUs, several APDUs per SPDU on send, IEC 62351-9 (GDOI) key
  distribution (keys come from a `KeyProvider`), and SCL `IP` addresses of
  R-GOOSE/R-SV control blocks. The wire layout follows the IEC TR
  61850-90-5 structure as libiec61850, Wireshark and dscsystems implement
  it, since 90-5 itself was not available; it has not been exchanged with
  another implementation (libiec61850 at the pinned revision writes an
  APDU length its own decoder refuses). A restarted publisher's SPDUs are
  refused as replays until the key changes, unless `ReplayResetAfter` or
  `InitialSPDUNumber` is used. Multicast needs Linux, macOS or a BSD.
- **Raw Ethernet only on Linux, macOS and the BSDs** — Linux uses
  `AF_PACKET` and needs `CAP_NET_RAW` (or root); macOS/BSD use BPF and need
  read/write access to `/dev/bpf*`. Other platforms (Windows) return
  `ErrUnsupportedPlatform`; the in-memory `Hub` works everywhere.
- **GOOSE** — no GSSE and no GOOSE management APDUs
  (GetGoReference/GetGOOSEElementNumber). The fixed-length encoding
  (IEC 61850-8-1:2011 Annex A.3, `fixedOffs`) is implemented but sends
  FLOAT32 in 5 octets (not the 4 of Table A.2), limits OCTET/VISIBLE
  STRING members to 20/35 octets and is not verified against an
  independent fixed-length implementation. Without VLAN parameters in
  the GSE address frames are sent untagged.
  Received GOOSE values are not mapped into a server model (no `ExtRef` /
  `InRef` binding); applications map them from subscriber events.
- **Sampled Values** — multicast control blocks (MSVCB) only: no unicast
  SV (USVCB, `SendUSVMessage`); an SCL `SampledValueControl` with
  `multicast="false"` is skipped with a model warning. The server's SV engine publishes the
  dataset values from its value store at `smpRate`; waveform sources feed
  the engine's publishers (`SVEngine.Publisher`) directly. Software timing
  is subject to OS scheduling: the publisher paces on an absolute schedule
  and counts late and skipped samples, but it is not a substitute for a
  hardware merging unit with PTP or 1PPS synchronisation.

## SCL limitations

- **Supported schema versions** — the SCL parser has bindings for IEC 61850-6
  schema versions 1.7, 2007B, 2007B4 and 2007C5. A document in the IEC
  namespace with another version tuple (2007A, 2007B5, a non-2007 version) is
  detected as unknown and parsed with the closest bindings, with a
  `schema-fallback` warning; elements those bindings do not define are
  ignored. A foreign namespace or a malformed release is refused.
- **Encodings** — SCL files are read as UTF-8 (declared as UTF-8, utf8 or
  US-ASCII); other encodings such as ISO-8859-1 are refused with an error
  naming the encoding. An `SCL` root without the IEC namespace is refused
  unless `ParseOptions.AssumeSCLNamespace` is set (then parsed with a
  `namespace-assumed` warning).
- **Validation is semantic, not XSD** — `ValidateRequired` checks the
  required attributes of the normalized model whose schema type cannot be
  empty (and, for a parsed document, a missing `Header` element), and
  `ValidateKind` the content a file kind needs; there is no full XML Schema
  validation (patterns, enumerations, element order).
- **Normalized model coverage** — the normalized model covers IEDs, access
  points, servers, logical devices, logical nodes (LN0 and regular LN),
  data objects (DOI/DAI/SDI), datasets, report control blocks, GSE control
  blocks, SMV control blocks, log control blocks, setting control,
  substations (with voltage levels, bays, conducting equipment),
  topology LNode references, communication (sub-networks, connected APs,
  GSE/SMV addresses), and full data type templates (LNodeType, DOType,
  DAType, EnumType). Private elements are preserved with full inner XML
  content and vendor namespace metadata.
- **Elements not in the normalized model** — some SCL elements are parsed
  from XML by the raw types but are not mapped into the normalized model:
  `Inputs`, `ClientLN`, `ExtRef`, `History`, `Text`, `Function`,
  `SubFunction`, `PowerTransformer`, `GeneralEquipment`, `ConnectivityNode`.
  These are accessible only via raw XML unmarshalling.
- **No XSD schema validation** — the parser performs structural XML parsing
  and the semantic checks listed under Validation in scl/README.md
  (selected XSD keys and keyrefs, required attributes, type references,
  FCDA resolution, control block and address checks); it does not validate
  against the official IEC XSD schema files. Not checked: DOI/DAI
  uniqueness (the model does not keep `ix`), LogControl `datSet`
  references, ExtRef/Inputs (not in the model), most XSD patterns and
  enumerations (only the address P patterns), the IEC 61850-7-4/7-3
  mandatory-content rules for LN classes and CDCs, and array `count`
  (flattened as one element).
- **Round-trip fidelity** — `scl.Generate` preserves the model structure but
  may not reproduce the exact formatting, namespace prefixes, or ordering of
  the original XML file. GSE and SMV control blocks are not serialized by
  `Generate`.
- **Extension preservation** — vendor `Private` elements are preserved
  losslessly (`Type`, `Source`, and full `InnerXML` content). Vendor
  namespaces from the root element are captured in `DocumentMetadata`.
  However, vendor-specific child elements outside of `Private` (using
  custom XML namespaces) may be captured by `ExtraElements` in raw types
  but are not individually surfaced in the normalized model.
- **Content-based kind detection** — `DetectKind` classifies documents by
  content (IED count, substation presence, communication bindings) but
  cannot distinguish ICD from IID without application-level context.

## Value handling

- **No automatic type coercion** — reading a `FLOAT32` as `Int64()` returns
  an error, not a coerced value. Use the correct accessor for each type.
- **Quality bit strings** — a quality of fewer than 13 bits decodes with
  the missing bits 0 (IEC 61850-8-1 8.2); bits beyond 13 are ignored.
- **Timestamp precision** — timestamps are decoded to Go `time.Time` which
  has nanosecond precision. Sub-nanosecond IEC 61850 time accuracy bits are
  preserved in `TimeQuality.TimeAccuracy` but not reflected in the `Time` field.

## Concurrency

- **Client is goroutine-safe** — all Client methods are safe for concurrent use.
- **ValueStore is goroutine-safe** — server-side ValueStore uses read-write
  mutexes for safe concurrent access. Values are aliased (not copied) on
  Get/Set; callers must copy if mutation is needed.
- **Server runtime engines are goroutine-safe** — report, control, setting
  group, and journal engines are safe for concurrent use. Lock ordering is
  documented and verified with race-detector stress tests.
- **Server.Model() returns shared state** — the returned model pointer must
  not be mutated after server creation.

## Interoperability

- **Tested against software stacks only** — MMS client and server
  behaviour is tested against pinned libiec61850, iec61850bean, dscsystems,
  iec61850-rust and cbsigma adapters, GOOSE against the libiec61850,
  dscsystems, iec61850-rust and cbsigma publishers and subscribers, and SV
  against the libiec61850, dscsystems and iec61850-rust ones (see
  [INTEROP.md](INTEROP.md) and [COMPATIBILITY.md](COMPATIBILITY.md)).
  Interoperability with physical IEDs and vendor stacks has not been
  formally validated, and the library has no UCA conformance
  certification.
