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

- **Control: no CommandTermination wait, no time-activated operate** —
  the control models (direct, SBO, SBOw, normal and enhanced security) are
  typed APIs (`Operate`, `Select`, `SelectWithValue`, `Cancel`), but the
  client does not wait for or surface the CommandTermination of an
  enhanced-security operate, and does not schedule time-activated
  operations (`operTm`).
- **No automatic model discovery caching persistence** — the client cache is
  in-memory only and does not survive process restarts.
- **Report subscription assumes single-client ownership** — the library does
  not handle multi-client RCB arbitration beyond basic URCB reserve/release.

## Server limitations

- **Reports: in-memory BRCB buffer, no segmentation** — the server generates
  InformationReports for data-change, quality-change, data-update,
  integrity, and GI triggers. RCB writes from MMS clients are
  automatically dispatched to the report engine. BRCB buffering is
  in-memory with configurable depth and no persistent storage; buffered
  entries are replayed to a client that re-enables the BRCB (optionally
  resuming after a given `EntryID`, see [INTEROP.md](INTEROP.md)), but the
  buffer does not survive a server restart. Multi-client RCB
  ownership arbitration and segmented report generation are not yet
  implemented.
- **Controls: no interlocking, no negative CommandTermination** — control handlers
  (`RegisterControl`) support select/operate/cancel dispatch with
  SBO timeout tracking, but interlocking and synchrocheck are
  application-responsibility via callbacks. After a successful
  enhanced-security operate the server sends a positive
  CommandTermination; negative termination (an operate that fails after
  it was accepted) is not generated.
- **Setting groups: no per-group value banks** — the server supports SGCB
  registration, active/edit group selection, confirmation, handler
  callbacks, and automatic dispatch from MMS client writes. However,
  persistent multi-bank SE/SG value storage is not yet implemented:
  the SGCB lifecycle (select/edit/confirm) exists, but there are no
  per-group value banks to store separate FC=SE/FC=SG attribute sets.
  Reservation timeouts (ResvTms), multi-connection edit ownership
  tracking, and connection-scoped reservation are also not yet
  implemented.
- **Journals: in-memory, no LCB runtime configuration** — the server supports runtime journal
  entry generation, in-memory storage via `MemoryJournalProvider`, and
  MMS journal service integration. Persistent journal storage, log
  control block (LCB) runtime configuration, and automatic event
  logging from control operations are not yet implemented.
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
- **References use the server's MMS domain** — the server's MMS domain is
  the bare LD instance, so GoCB/MSVCB references and the published
  `gocbRef`/`datSet` are `LD/LLN0$...` rather than the IED-name-prefixed
  form used by IEDs that name domains `<IED><LD>`. Subscribers that
  validate against an SCL with IED-prefixed references must relax the
  reference check (see `examples/goose-subscriber -any-ref`).

## SCL limitations

- **Supported schema versions** — the SCL parser supports IEC 61850-6 schema
  versions 1.7, 2007B, 2007B4, and 2007C5. Other versions (e.g. 2007A) are
  detected and rejected with a clear error.
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
  behaviour is tested against pinned libiec61850, iec61850bean, dscsystems
  and iec61850-rust adapters, and GOOSE and SV against the libiec61850,
  dscsystems and iec61850-rust publishers and subscribers (see
  [INTEROP.md](INTEROP.md) and [COMPATIBILITY.md](COMPATIBILITY.md)).
  Interoperability with physical IEDs and vendor stacks has not been
  formally validated, and the library has no UCA conformance
  certification.
