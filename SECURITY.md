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
  both held at the receivers, and log `OnReject`. Set `MulticastTTL` no
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
| Buffered segments of one segmented report | 64 MiB, estimated from the decoded values: a report of the largest data sets is a few MB. Dropped, `PartialReportsDropped` | `SubscribeReportOptions.MaxSegmentBytes` | client report reassembly (`report.go`) | `TestReportSegments_ByteBudget`, `TestReportSegments_DefaultsAndEstimate` |
| Time from the first to the last segment of a report | 60 s: a server sends the segments of a report back to back. Dropped, segments released, `PartialReportsDropped` | `SubscribeReportOptions.SegmentTimeout` | client report reassembly | `TestReportSegments_ReassemblyTimeLimit` |
| Segments of one report | 65536 (SubSeqNum is 16 bits). Dropped, `PartialReportsDropped` | none | client report reassembly | `TestReportSegments_OtherDropsAndClose` |
| Report channel of a subscription | 64 reports. Overflow policy (`OverflowDropNewest` by default), `ReportsDropped` | `SubscribeReportOptions.QueueSize`, `OverflowPolicy` | `ReportSubscription` | `TestSubscribeReport_QueueOverflow`, `TestReportSubscription_StatsCountDrops` |
| How far ahead an operTm may be | 24 hours: a time activated operate is scheduled seconds to hours ahead. Refused, AddCause time-limit-over, the control as it was | `ControlHandler.MaxOperDelay` | server Operate (`control_server.go`) | `TestControl_MaxOperDelay` |
| Connections `ListenAndServe` serves at once | 1024: an IED serves a few SCADA, gateway and engineering clients. Closed at once, before `OnConnect`, `ConnectionsRefused` | `ServerOptions.MaxConnections` | `Server.ListenAndServe` (`server.go`) | `TestServer_MaxConnections` |
| Pause after a temporary accept error | 5 ms doubling to 1 s, as `net/http`; reset by an accepted connection | none | `Server.ListenAndServe` | `TestServer_AcceptBackoff` |
| BRCB buffer | 1000 entries, no byte limit; the oldest entry goes, `BufOvfl` | `ServerOptions.NewReportBuffer` (`NewMemoryReportBuffer(entries, bytes)`) | report engine | `TestMemoryReportBuffer_EntryLimit`, `TestMemoryReportBuffer_ByteLimit` |
| Streams a GOOSE subscription supervises | 4096: the largest substations publish a few thousand GOOSE streams in all, which a subscription without filters sees. A frame of a new stream is dropped, `StreamsRefused`, `FrameDropped("streams")`; known streams go on | `goose.SubscriptionConfig.MaxStreams` | `goose.Subscriber` | `goose` `TestMaxStreams_Boundaries`, `TestMaxStreams_NewStreamAtLimitAllocatesNothing` |
| Streams an SV subscription supervises | 4096: a process bus carries some hundreds of SV streams; the limit also bounds the supervision scan. As GOOSE | `sv.SubscriptionConfig.MaxStreams` | `sv.Subscriber` | `sv` `TestMaxStreams_Boundaries`, `TestMaxStreams_NewStreamAtLimitAllocatesNothing` |
| GOOSE decoding | nesting depth 16, 4096 data elements (`goose.DefaultLimits`); `ErrLimitExceeded` | `goose.SubscriberConfig.Limits` | `goose.Decode`, subscriber | `TestDecodeLimits` |
| SV decoding | 64 ASDUs per frame, samples up to the frame payload (`sv.DefaultLimits`); `ErrLimitExceeded` | `sv.SubscriberConfig.Limits` | `sv.Decode`, subscriber | `sv` `TestDecodeErrors` |
| Subscriber event queues | 256 events; dropped and counted (`DroppedEvents`, `QueueDrops`) | `QueueSize`, `EventQueue` | `goose`, `sv` subscriptions | `TestSubscriberOnlyStateChangesAndOverflow`, `TestSubscriberSimulationAndChannels` |
| R-GOOSE/R-SV senders | any address (off): the source address is not covered by the signature, so the deployment sets it (see Deployment guidance). Refused before verification, `NotAllowed` | `rsession.Config.AllowedSenders` | `rsession.Transport` | `TestHandle_NotAllowedBeforeVerification`, `TestAllowedSenders_IPv4Mapped` |
| R-GOOSE/R-SV replay window per key | per sender and key (off). With it, one window per key whatever the sender | `rsession.Config.SingleSenderPerKey` | `rsession.Transport` | `TestHandle_SameSPDUFromSeveralSenders` |
| R-GOOSE/R-SV replay state | 1024 senders; the least recently heard is forgotten | `rsession.Config.MaxSenders` | `rsession.Transport` | `TestReplayEviction` |
| R-GOOSE/R-SV receive queue | 1024 frames; dropped and counted | `rsession.Config.Queue` | `rsession.Transport` | `TestQueueFull` |
| SCL document size | 1 GiB: substation documents are a few hundred MB at the most. Refused, `scl.ErrLimitExceeded`; a file's size is checked before it is read | `scl.ParseOptions.MaxBytes` | `scl.ParseBytes`, `ParseFileOpts`, `Parse`, `ParseWithOptions` | `TestParse_MaxBytesBoundary`, `TestParseWithOptions_ReaderStopsAtMaxBytes`, `TestParseFileOpts_LargeFileRefusedBeforeRead` |
| SCL element nesting, also of skipped content | 1024: the SCL schema nests about a dozen levels. Refused, `scl.ErrLimitExceeded` | `scl.ParseOptions.MaxDepth` | `scl` parse | `TestParse_MaxDepthBoundary`, `TestParse_DeepSkippedContentAllocation` |
| SCL diagnostics returned | all; with the option, parsing and validation stop adding them once the list is full | `scl.ParseOptions.MaxDiagnostics` | `scl` parse and semantic validation | `TestParse_MaxDiagnosticsBoundary`, `TestParse_MaxDiagnosticsEveryPass` |
| Name quoted in an SCL diagnostic | 256 bytes: schema names are at most 64 characters. Cut at a character boundary, ends with "…" | `scl.ParseOptions.MaxDiagnosticName` | `scl` parse | `TestParse_DiagnosticNameBoundary`, `TestCapName` |
| Nodes of an SCL type expansion | 33,554,432 DO, SDO, DA and BDA instances: the vendor files of the test data expand to about 32,000 nodes for their largest IED, so this is above a thousand such IEDs. Stopped: `expansion-limit` row and diagnostic, or the model is refused with `ErrLimitExceeded` | `scl.FlattenOptions.MaxExpandedNodes`, `ServerModelOptions.MaxExpandedNodes`, `goose.SCLOptions`, `sv.SCLOptions` | `scl.Flatten`, `NewServerModelFromSCL`, `ControlBlocksFromSCL` | `TestFlatten_MaxExpandedNodesBoundary`, `TestFromSCL_MaxExpandedNodesBoundary`, `TestNewServerModelFromSCL_MaxExpandedNodes`, `goose`/`sv` `TestControlBlocksFromSCL_ExpansionBudget` |


## Disclosure policy

We follow coordinated disclosure. We request that you allow a reasonable fix
window before public disclosure. We will credit reporters in release notes
unless they prefer to remain anonymous.
