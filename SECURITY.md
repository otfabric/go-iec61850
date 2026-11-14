# Security Policy

## Supported releases

Security issues are addressed in the latest v1 patch release.

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | No |

## Reporting a vulnerability

Please report security vulnerabilities **privately** rather than opening a
public GitHub issue.

Send a report to: **security@otfabric.io**

Include:

- A description of the vulnerability and its potential impact.
- Steps to reproduce or a minimal proof-of-concept.
- The affected version(s).

**Expected acknowledgement:** within 5 business days.

We will coordinate a fix and disclosure timeline with you. We aim to release a
patch within 90 days of confirmation.

## Scope

go-iec61850 is a protocol library that parses untrusted network data. The
following are in scope for this policy:

- Panics triggered by malformed MMS PDUs, IEC 61850 report payloads, GOOSE
  or Sampled Values frames, or SCL files.
- Memory exhaustion caused by peer-controlled allocations (e.g. unbounded
  datasets, oversized report queues, deeply nested SCL models).
- Incorrect control authorisation — a client operating a control object it
  should not have access to.
- Incorrect URCB/BRCB reservation enforcement allowing unauthorised report
  enabling.
- A client changing data it may only read: status (ST), measurands (MX),
  setting-group values (SG), service responses (SR), operate-received (OR)
  and extended definitions (EX) answer client writes with
  object-access-denied; the application changes them with
  `Server.SetValue`. `ServerOptions.AllowReadOnlyFCWrites` turns this off
  for simulators and test fixtures; never set it on a device.
- Data races exposed by the race detector.
- Any defect that allows a remote peer to crash or deadlock the server.

The following are **out of scope**:

- Vulnerabilities in dependencies not maintained by this project.
- Issues requiring physical access to the machine running the library.
- Theoretical attacks with no practical exploitation path.
- IEC 62351 role-based access control and layer-2 IEC 62351-6 GOOSE/SV
  authentication (not implemented; see below).
- Spoofed or replayed layer-2 GOOSE/SV frames: without IEC 62351-6, any
  host on the Ethernet segment can publish frames a subscriber accepts.
  Subscriber validation (APPID, gocbRef/svID, datSet, confRev, member
  types) protects against misconfiguration, not against an attacker.

In scope, on the other hand: a forged, tampered or replayed R-GOOSE/R-SV
SPDU that a `rsession.Transport` with a key provider accepts.

## Certification statement

go-iec61850 has not received a formal security audit or certification. The
implementation includes decoder strictness hardening, race-detector coverage,
fuzz testing, and panic containment in server callbacks, but no independent
security evaluation has been performed.

Optional TLS transport is provided through Go's standard `crypto/tls`
implementation. This is **not** equivalent to IEC 62351 conformance or
certification. Do not claim IEC 62351 compliance based solely on TLS transport
support.

## Deployment guidance

- **MMS** — expose port 102 only to trusted clients. Use
  `ServerOptions.Authenticate` to check the association (ACSE)
  authentication value, and `iso.WithTLSConfig` (go-mms) for
  confidentiality. Configure control models (SBO, enhanced security) and
  handler checks for every operable object; the library does not decide
  who may operate what.
- **Restricting destinations** — an application that lets users choose
  which IED to reach should enforce its allowlist in `DialOptions.Dialer`,
  for example a `net.Dialer` with a `Control` hook, which sees the resolved
  IP right before connecting; checking the host name before `Dial` leaves a
  DNS-rebinding gap. `Dial` is the only path that opens a connection: reports
  arrive on the same association, and GOOSE/SV are layer 2.
- **GOOSE and SV** — these are layer-2 multicast without authentication.
  Run them on a dedicated, physically or VLAN-separated process bus;
  restrict which ports may source the multicast addresses (switch port
  security, IEEE 802.1X); do not bridge the process bus to IT networks.
- **R-GOOSE and R-SV** (`rsession`) — keep the default: a key provider and
  no `Insecure`. HMAC authenticates but does not encrypt, so the values
  are readable on the network. Generate keys of at least 32 random octets,
  distribute them out of band, change them with the old and the new key
  both held at the receivers, and log `OnReject`. Change the key well
  before 2^32 SPDUs (about 10 days of an R-SV stream at 4800 SPDUs/s):
  a sender refuses to reuse an SPDU number under one key. Give each new
  key a new ID. Set `MulticastTTL` no
  higher than the routed path needs. The replay window is kept per sender
  address and key: set `Config.AllowedSenders` to the publishers'
  addresses, or `Config.SingleSenderPerKey` when each key is used by one
  publisher, and filter each subscription on its publisher with
  `Src: rsession.SourceMAC(publisherIP)`.
- **Raw socket privileges** — publishers and subscribers need
  `CAP_NET_RAW` on Linux or `/dev/bpf*` access on macOS/BSD. Grant the
  capability to the binary (`setcap cap_net_raw+ep`) instead of running
  as root.
- **Resource limits** — every input from a peer or a file is bounded; the
  limits, their defaults and the options that raise them are in
  [Limits](#limits). Keep the defaults unless a peer or a file requires
  larger values. An SCL file from an untrusted source may expand to
  `scl.DefaultMaxExpandedNodes` nodes, several gigabytes of memory: set a
  lower `ServerModelOptions.MaxExpandedNodes` (and `FlattenOptions`,
  `goose.SCLOptions`, `sv.SCLOptions`) for such files.
- **Application code** — the library calls hooks, providers, handlers
  and `Metrics` under a recover and answers the request with an error when
  a call does not return normally; it logs the call with its stack at
  error level and counts it (`Server.Stats`, the subscription counters).
  Alert on these counters: the state the application code held at that
  moment is as the code left it.

## Limits

Each limit applies when its option is zero (or negative); the option
raises or lowers it. The defaults are chosen above what a real substation
sends or holds, so that only an input far outside it meets them. An
input beyond a limit is refused with an error that matches the package's
`ErrLimitExceeded`, or dropped and counted, as the table says; the
association, subscription or parse in progress is not otherwise
affected. Limits of the MMS layer (PDU size, outstanding requests,
nesting, value counts) are go-mms's:
[go-mms LIMITS.md](https://github.com/otfabric/go-mms/blob/main/LIMITS.md).

| Limit | Default, and why | Option | Enforced in | Test |
|---|---|---|---|---|
| Size of a file `ReadFile` holds in memory | 256 MiB: the files an IED serves (COMTRADE records, logs, SCL) are a few tens of MB at most. Beyond it: refused before the read when announced, else stopped; `ErrLimitExceeded`, file closed | `FileLimits.MaxSize` in `DialOptions` / `ClientOptions` | `Client.ReadFile` (`file.go`) | `TestReadFile_MaxFileSize`, `TestReadFile_EndlessFileAllocation` |
| Bytes beyond the size FileOpen announced | 64 MiB: a file that grows while it is read (a log being appended) grows by far less. `ErrLimitExceeded`, file closed | `FileLimits.SizeTolerance` | `ReadFile`, `DownloadFile` | `TestDownloadFile_StopsBeyondAnnouncedSize` |
| Consecutive FileRead answers without data that announce more | 64: a server sends one only while data is not ready yet. `ErrLimitExceeded`, file closed | `FileLimits.MaxEmptyChunks` | `ReadFile`, `DownloadFile` | `TestDownloadFile_StopsAfterEmptyChunks` |
| Buffered segments of one segmented report | 64 MiB, estimated from the decoded values (256 bytes per value plus its content: octets of strings, bit strings and boolean arrays, 8 bytes per object identifier arc): a report of the largest data sets is a few MB. Dropped, `PartialReportsDropped` | `SubscribeReportOptions.MaxSegmentBytes` | client report reassembly (`report.go`) | `TestReportSegments_ByteBudget`, `TestReportSegments_DefaultsAndEstimate`, `TestReportSegments_VariableLengthValuesCounted`, `TestValueFootprint_UpperBound` |
| Entries `ReadJournalAll` and `ReadJournalAfterAll` collect | 1,000,000 entries and 256 MiB estimated (entry IDs, tags, decoded values), so a server that keeps sending pages cannot grow the result without bound. `ErrLimitExceeded`; a page cursor already used is refused | `JournalLimits.MaxEntries`, `JournalLimits.MaxBytes` in `DialOptions` / `ClientOptions` (negative: no limit) | `Client.ReadJournalAll`, `ReadJournalAfterAll` (`journal.go`) | `TestReadJournalAll_MaxEntries`, `TestReadJournalAll_MaxBytes`, `TestReadJournalAll_CursorNotAdvancing` |
| Cost of building the model tree | one pass over the name list of each logical device, linear in the number of names; the context is checked every 4096 names or nodes, its error returned | the context of `Tree` / `TreeWithOptions` | `Client.Tree`, `TreeWithOptions` (`browse.go`) | `TestBuildLDTree_LinearInItems`, `TestBuildLDTree_ContextEnds`, `TestTree_ContextEndsDuringBuild` |
| Time from the first to the last segment of a report | 60 s: a server sends the segments of a report back to back. Dropped, segments released, `PartialReportsDropped` | `SubscribeReportOptions.SegmentTimeout` | client report reassembly | `TestReportSegments_ReassemblyTimeLimit` |
| Segments of one report | 65536 (SubSeqNum is 16 bits). Dropped, `PartialReportsDropped` | none | client report reassembly | `TestReportSegments_OtherDropsAndClose` |
| Report channel of a subscription | 64 reports. Overflow policy (`OverflowDropNewest` by default), `ReportsDropped` | `SubscribeReportOptions.QueueSize`, `OverflowPolicy` | `ReportSubscription` | `TestSubscribeReport_QueueOverflow`, `TestReportSubscription_StatsCountDrops` |
| How far ahead an operTm may be | 24 hours: a time activated operate is scheduled seconds to hours ahead. Refused, AddCause time-limit-over, the control as it was | `ControlHandler.MaxOperDelay` | server Operate (`control_server.go`) | `TestControl_MaxOperDelay` |
| Integrity period a client writes | 100 ms at the least, IntgPd 0 allowed, so a client cannot make the server produce integrity reports or journal entries at an unbounded rate. A non-zero IntgPd below it is refused with object-value-invalid; the periods of the model (SCL) are not checked | `ServerOptions.MinIntegrityPeriod` (negative: no minimum) | BRCB, URCB and LCB writes (`report_engine_write.go`, `journal_lcb.go`) | `TestRCBIntgPdMinimum`, `TestRCBIntgPdMinimumOptions`, `TestRCBIntgPdModelNotChecked`, `TestLCBIntgPdMinimum` |
| Sample rate a client selects for an MSVCB | 96000 samples/s, the highest IEC 61869-9 preferred rate, or the SCL rate of the MSVCB when that is higher. Checked when the MSVCB is enabled, not per SmpRate or SmpMod write; refused with object-value-invalid | `SVOptions.MaxSamplesPerSecond` (negative: no bound) | `SVEngine` (`sv_server.go`) | `TestSVServerMaxSamplesPerSecond`, `TestSVServerMaxSamplesPerSecondOptions` |
| ConfRev of a GoCB or MSVCB | 4294967295 (INT32U). A change that would take it further is refused with object-value-invalid, so the control block stays enablable | none | `GOOSEEngine`, `SVEngine` | `TestGOOSEServerConfRevOverflow`, `TestSVServerConfRevBound` |
| Connections `ListenAndServe` serves at once | 1024: an IED serves a few SCADA, gateway and engineering clients. Closed at once, before `OnConnect`, `ConnectionsRefused` | `ServerOptions.MaxConnections` | `Server.ListenAndServe` (`server.go`) | `TestServer_MaxConnections` |
| Pause after a temporary accept error | 5 ms doubling to 1 s, as `net/http`; reset by an accepted connection | none | `Server.ListenAndServe` | `TestServer_AcceptBackoff` |
| BRCB buffer | 1000 entries, no byte limit; the oldest entry goes, `BufOvfl` | `ServerOptions.NewReportBuffer` (`NewMemoryReportBuffer(entries, bytes)`) | report engine | `TestMemoryReportBuffer_EntryLimit`, `TestMemoryReportBuffer_ByteLimit` |
| Streams a GOOSE subscription supervises | 4096: the largest substations publish a few thousand GOOSE streams in all, which a subscription without filters sees. A frame of a new stream is dropped, `StreamsRefused`, `FrameDropped("streams")`; known streams go on | `goose.SubscriptionConfig.MaxStreams` | `goose.Subscriber` | `goose` `TestMaxStreams_Boundaries`, `TestMaxStreams_NewStreamAtLimitAllocatesNothing` |
| Streams an SV subscription supervises | 4096: a process bus carries some hundreds of SV streams; the limit also bounds the supervision scan. As GOOSE | `sv.SubscriptionConfig.MaxStreams` | `sv.Subscriber` | `sv` `TestMaxStreams_Boundaries`, `TestMaxStreams_NewStreamAtLimitAllocatesNothing` |
| GOOSE decoding | nesting depth 16, 4096 data elements (`goose.DefaultLimits`); `ErrLimitExceeded` | `goose.SubscriberConfig.Limits` | `goose.Decode`, subscriber | `TestDecodeLimits` |
| SV decoding | 64 ASDUs per frame, samples up to the frame payload (`sv.DefaultLimits`); `ErrLimitExceeded`. A zero field of `SubscriberConfig.Limits` takes its default | `sv.SubscriberConfig.Limits` | `sv.Decode`, subscriber | `sv` `TestDecodeErrors`, `TestSubscriberLimitsPerField` |
| SV svID and datSet | 129 octets (VisibleString129, IEC 61850-9-2 Table 14). `ErrInvalidPDU` on decode, `ErrInvalidValue` on encode, `ErrInvalidConfig` in `NewPublisher` | none (`sv.MaxIDLength`) | `sv` decoder, encoder, publisher | `sv` `TestIDLength` |
| `l2.Demux` port queue | 4096 frames per port (`l2.DefaultDemuxQueue`), by frame count, not octets: about 6 MiB of 1500-octet frames, more with jumbo frames. Dropped and counted (`DemuxStats.Overflow`) | the queue length of `l2.NewDemux` | `l2.Demux` | `l2` `TestDemux` |
| Subscriber event queues | 256 events; dropped and counted (`DroppedEvents`, `QueueDrops`) | `QueueSize`, `EventQueue` | `goose`, `sv` subscriptions | `TestSubscriberOnlyStateChangesAndOverflow`, `TestSubscriberSimulationAndChannels` |
| R-GOOSE/R-SV senders | any address (off): the source address is not covered by the signature, so the deployment sets it (see Deployment guidance). Refused before verification, `NotAllowed` | `rsession.Config.AllowedSenders` | `rsession.Transport` | `TestHandle_NotAllowedBeforeVerification`, `TestAllowedSenders_IPv4Mapped` |
| R-GOOSE/R-SV replay window per key | per sender and key (off). With it, one window per key whatever the sender | `rsession.Config.SingleSenderPerKey` | `rsession.Transport` | `TestHandle_SameSPDUFromSeveralSenders` |
| R-GOOSE/R-SV replay window | 64 SPDU numbers below the highest accepted, compared as plain 32-bit unsigned values: a number below the window is old, never ahead. `ErrReplay` | none; `rsession.Config.ReplayResetAfter` accepts a lower number after that much silence | `rsession.Transport` | `TestReplayWindow`, `TestReplayWrap`, `TestReplayOldNumberFarBehind`, `TestReplayRestartAfterSilenceBelowTop` |
| SPDU numbers sent under one key | 2^32: numbers do not repeat under a key. After 0xffffffff, `Send` fails with `ErrLimitExceeded` until a key with another ID is current; numbering goes on at 0. An R-SV stream of 4800 SPDUs/s uses them up in about 10 days: rotate keys before | none | `rsession.Transport.Send` | `TestTakeNumberNoRepeatUnderKey`, `TestSendNoNumberRepeatUnderKey` |
| R-GOOSE/R-SV replay state | 1024 senders; the least recently heard is forgotten | `rsession.Config.MaxSenders` | `rsession.Transport` | `TestReplayEviction` |
| R-GOOSE/R-SV receive queue | 1024 frames; dropped and counted | `rsession.Config.Queue` | `rsession.Transport` | `TestQueueFull` |
| SCL document size | 1 GiB: substation documents are a few hundred MB at the most. Refused, `scl.ErrLimitExceeded`; a file's size is checked before it is read | `scl.ParseOptions.MaxBytes` | `scl.ParseBytes`, `ParseFileOpts`, `Parse`, `ParseWithOptions` | `TestParse_MaxBytesBoundary`, `TestParseWithOptions_ReaderStopsAtMaxBytes`, `TestParseFileOpts_LargeFileRefusedBeforeRead` |
| SCL element nesting, also of skipped content | 1024: the SCL schema nests about a dozen levels. Refused, `scl.ErrLimitExceeded` | `scl.ParseOptions.MaxDepth` | `scl` parse | `TestParse_MaxDepthBoundary`, `TestParse_DeepSkippedContentAllocation` |
| SCL diagnostics returned | all; with the option, parsing and validation stop adding them once the list is full | `scl.ParseOptions.MaxDiagnostics` | `scl` parse and semantic validation | `TestParse_MaxDiagnosticsBoundary`, `TestParse_MaxDiagnosticsEveryPass` |
| Name quoted in an SCL diagnostic | 256 bytes: schema names are at most 64 characters. Cut at a character boundary, ends with "…" | `scl.ParseOptions.MaxDiagnosticName` (parse); `scl/index` and `scl/validate` use the default | `scl` parse, `scl/index.Build`, `scl/validate` | `TestParse_DiagnosticNameBoundary`, `TestCapName`, `TestBuild_DiagnosticNamesCapped`, `TestAll_DiagnosticNamesCapped` |
| Nodes of an SCL type expansion | 33,554,432 DO, SDO, DA and BDA instances (for a server model also report control block instances; `scl.Flatten` counts one more node per 256 bytes of a row's path): the vendor files of the test data expand to about 32,000 nodes for their largest IED, so this is above a thousand such IEDs. Stopped: `expansion-limit` row and diagnostic, or the model is refused with `ErrLimitExceeded` | `scl.FlattenOptions.MaxExpandedNodes`, `ServerModelOptions.MaxExpandedNodes`, `goose.SCLOptions`, `sv.SCLOptions` | `scl.Flatten`, `NewServerModelFromSCL`, `ControlBlocksFromSCL` | `TestFlatten_MaxExpandedNodesBoundary`, `TestFromSCL_MaxExpandedNodesBoundary`, `TestNewServerModelFromSCL_MaxExpandedNodes`, `goose`/`sv` `TestControlBlocksFromSCL_ExpansionBudget`, `TestFlatten_PathBytesCount`, `TestFromSCL_ReportInstancesCount` |
| Type references one path of an SCL type expansion follows | 64 (a DO's DOType, each SDO's DOType, each Struct DA's or BDA's DAType): the common data classes of IEC 61850-7-3 nest them less than ten deep. Stopped: `expansion-limit` row and diagnostic, or the model is refused with `ErrLimitExceeded` | `scl.FlattenOptions.MaxTypeDepth`; the server model and `ControlBlocksFromSCL` use `scl.DefaultMaxTypeDepth` | `scl.Flatten`, `NewServerModelFromSCL`, `goose`/`sv` `ControlBlocksFromSCL` | `TestFlatten_TypeChainDepth`, `TestFromSCL_TypeChainDepth` |


## Disclosure policy

We follow coordinated disclosure. We request that you allow a reasonable fix
window before public disclosure. We will credit reporters in release notes
unless they prefer to remain anonymous.
