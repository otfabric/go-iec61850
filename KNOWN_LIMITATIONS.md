# Known Limitations

What `go-iec61850` does not do, or does only partly, as of v1.2. Each item
is a deliberate scope decision or a documented gap; behaviour that deviates
from a peer stack is listed in [INTEROP.md](INTEROP.md) and
[COMPATIBILITY.md](COMPATIBILITY.md).

## Protocol scope

- **MMS, GOOSE and Sampled Values** — this library implements IEC 61850
  over MMS (ISO 9506), GOOSE (IEC 61850-8-1, see [GOOSE.md](GOOSE.md)) and
  Sampled Values (IEC 61850-9-2, see [SV.md](SV.md)). R-GOOSE/R-SV
  (IEC 61850-90-5) are out of scope.
- **No ACSI mapping completeness guarantee** — not every ACSI service defined
  in IEC 61850-7-2 is mapped. The library focuses on the services needed for
  practical client/server use cases.

## Client limitations

- **Control: no time-activated operate** — the control models (direct,
  SBO, SBOw, normal and enhanced security) are typed APIs (`Operate`,
  `Select`, `SelectWithValue`, `Cancel`, and `OperateWait`, which waits for
  the CommandTermination), but the client does not schedule time-activated
  operations (`operTm`). Once the client sends a control or subscribes to
  reports, it owns the MMS client's InformationReport handler.
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

- **Reports: in-memory BRCB buffer, no ResvTms/Owner** — the report engine
  implements the IEC 61850-7-2 §17.2 URCB and BRCB state machines per
  association (see API.md "Report engine"), with these limits:
  - The BRCB buffer is in memory (default 1000 entries, `SetRCBBufMax`)
    and does not survive a server restart; EntryIDs restart at 1.
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
- **Controls: no interlocking** — control handlers (`RegisterControl`)
  support select/operate/cancel dispatch with SBO timeout tracking, but
  interlocking and synchrocheck are application-responsibility via
  callbacks. A refused control write is preceded by a LastApplError. After
  a successful enhanced-security operate the server sends a positive
  CommandTermination, or a negative one when `ControlHandler.OnExecute`
  fails.
- **Setting groups: no per-group value banks** — the server supports SGCB
  registration, active/edit group selection, confirmation, handler
  callbacks, and automatic dispatch from MMS client writes. However,
  persistent multi-bank SE/SG value storage is not yet implemented:
  the SGCB lifecycle (select/edit/confirm) exists, but there are no
  per-group value banks to store separate FC=SE/FC=SG attribute sets.
  Reservation timeouts (ResvTms), multi-connection edit ownership
  tracking, and connection-scoped reservation are also not yet
  implemented.
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
  construction, and clients cannot create or delete named variable lists
  on the server (`DefineNamedVariableList` / `DeleteNamedVariableList`);
  the client supports both against other servers.

## GOOSE and Sampled Values

- **No IEC 62351-6 security** — GOOSE and SV frames are neither signed nor
  encrypted; the SV `security` field is carried opaquely. Protect the
  process bus at the network layer (VLANs, port security, separate
  switches).
- **No R-GOOSE / R-SV** (IEC 61850-90-5, routable over UDP/IP).
- **Raw Ethernet only on Linux, macOS and the BSDs** — Linux uses
  `AF_PACKET` and needs `CAP_NET_RAW` (or root); macOS/BSD use BPF and need
  read/write access to `/dev/bpf*`. Other platforms (Windows) return
  `ErrUnsupportedPlatform`; the in-memory `Hub` works everywhere.
- **GOOSE** — no fixed-offset (IEC 61850-8-1 Ed. 2.1) encoding, no GSSE and
  no GOOSE management APDUs (GetGoReference/GetGOOSEElementNumber).
  Received GOOSE values are not mapped into a server model (no `ExtRef` /
  `InRef` binding); applications map them from subscriber events.
- **Sampled Values** — multicast control blocks (MSVCB) only: no unicast
  SV (USVCB, `SendUSVMessage`). The server's SV engine publishes the
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
  and comprehensive semantic validation (type templates, IED structure,
  communication linkage, dataset references, control block references,
  topology LNode resolution), but does not validate against the official
  IEC XSD schema files.
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
