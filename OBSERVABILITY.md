# Observability

This document describes logging and observability support in `go-iec61850`.

## Structured logging

All logging uses Go's standard `log/slog` package. No third-party logging
frameworks are required.

### Enabling logging

Pass a `*slog.Logger` via the options struct:

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{
    Logger: logger,
})
```

For the server:

```go
srv, err := iec61850.NewServer(model, iec61850.ServerOptions{
    Logger: logger,
})
```

### Default behavior

When `Logger` is nil (the default), no log output is emitted. The library
uses an internal discard handler that is completely zero-cost — `Enabled()`
returns false for all levels, so log arguments are never evaluated.

### Log levels

| Level | Usage |
|-------|-------|
| `Debug` | Detailed protocol flow: MMS requests/responses, cache hits/misses, report decoding steps, browse tree construction, dataset reads, file operations |
| `Info` | Lifecycle events: connection established, subscription created, server started, control operations completed, setting group changes |
| `Warn` | Recoverable issues: report overflow (dropped), best-effort cleanup failures, unexpected server behavior, journal overflow |
| `Error` | Failures that affect correctness: decode errors, registration failures, control rejections, and every call of application code that did not return normally (with its stack) |

### Structured attributes

Log entries use structured key-value attributes for machine-parseable output:

```
level=DEBUG msg="iec61850: subscribe report" rptID=rpt01 queueSize=64 ...
level=WARN msg="iec61850: report dropped (queue full)" rptID=rpt01
level=DEBUG msg="iec61850: cache refreshed" devices=3
level=DEBUG msg="iec61850: operate" ref=LD1/CSWI1.Pos ctlNum=1
level=WARN msg="iec61850: operate denied: no active SBO select" ref=LD1/CSWI1.Pos
level=WARN msg="iec61850: dial failed, retrying" addr=10.0.0.1:102 attempt=1 backoff=500ms error=...
level=INFO msg="goose: subscription created" gocbRef=IED1LD0/LLN0$GO$gcb1 dst=01:0c:cd:01:00:01 appIDs=[1]
level=WARN msg="goose: timeAllowedToLive expired" gocbRef=IED1LD0/LLN0$GO$gcb1 src=00:11:22:33:44:55
level=WARN msg="sv: publisher behind schedule, samples skipped" skipped=3
level=ERROR msg="iec61850: callback did not return normally" callback=FileProvider.Open value=... stack="goroutine 42 [running]: ..."
level=WARN msg="iec61850: connection refused: MaxConnections served" max=1024
```

Messages are prefixed with the package (`iec61850:`, `goose:`, `sv:`,
`l2:`); match on the message and attributes, not on the full text, which
may gain detail in patch releases.

### Logger propagation

When `DialOptions.Logger` / `ServerOptions.Logger` is set and the nested
`MMS.Logger` is nil, the IEC logger is passed through to the underlying
`go-mms` client or server so MMS-level protocol events appear in the same
log stream. An explicit `MMS.Logger` is left unchanged.

ISO transport logging (`iso.WithLogger`) is independent: it is **not**
inherited from the IEC or MMS logger. Configure it separately when ISO-layer
logs are needed. See go-mms `OBSERVABILITY.md` for MMS vs ISO details.

### Covered subsystems

Logging is integrated across all runtime subsystems:

| Subsystem | Key log points |
|-----------|----------------|
| Client core | Connection lifecycle, request/response flow |
| Browse/cache | Tree construction, cache hits/misses, logical device/node enumeration |
| Read/write | Value reads, bulk reads, write operations |
| Reports | Subscription setup, report decoding, RCB operations, queue overflow |
| Control | Select, operate, cancel operations and outcomes |
| Setting groups | Group selection, edit sessions, confirmation |
| Datasets | Dataset listing, dataset reads |
| Files | File listing, file reads |
| Server | Server startup, connection handling, identify/status |
| Report engine | RCB enable/disable, report generation, buffer management |
| Journal engine | Entry creation, query results, overflow |
| GOOSE | Publisher start/stop and send failures, subscriber stream state changes and validation failures, GoCB enable/disable and writes |
| SV | Publisher pacing (samples skipped when behind) and send failures, decode failures, MSVCB enable/disable and sample update failures |

## SCL diagnostics

The SCL parser and validator use a structured diagnostic system rather than
slog logging. Parse and validation issues are returned as `[]scl.Diagnostic`
values with severity, code, path, and message fields. See the
[Errors section in API.md](API.md#errors) for details on diagnostic codes.

## Metrics

GOOSE publishers and subscribers report to a `goose.Metrics`
implementation (`PublisherConfig.Metrics`, `SubscriberConfig.Metrics`,
`GOOSEOptions.Metrics` for the server engine):

| Method | Called when |
|--------|-------------|
| `FrameSent(gocbRef, retransmission)` | a frame was put on the wire |
| `SendError(gocbRef, err)` | the transport rejected a frame |
| `FrameReceived(gocbRef)` | a GOOSE frame was decoded |
| `FrameDropped(reason)` | a frame was dropped: `decode`, `simulation`, `filtered`, `streams` (a subscription at `MaxStreams` dropped a frame of a new stream) |
| `Event(gocbRef, kind, dropped)` | a subscription event was produced (dropped when the queue was full) |

Sampled Values publishers and subscribers report to an `sv.Metrics`
(`PublisherConfig.Metrics`, `SubscriberConfig.Metrics`,
`SVOptions.Metrics` for the server engine):

| Method | Called when |
|--------|-------------|
| `FrameSent(svID, asdus)` | a frame with `asdus` samples was put on the wire |
| `SendError(svID, err)` | the transport rejected a frame |
| `ASDUReceived(svID)` | an ASDU was decoded and accepted |
| `FrameDropped(reason)` | a frame was dropped: `decode`, `simulation`, `filtered`, `streams` (a subscription at `MaxStreams` dropped an ASDU of a new stream) |
| `Event(svID, kind, dropped)` | a subscription event was produced (dropped when the queue was full) |

Embed `goose.NopMetrics` (or `sv.NopMetrics`) to implement a subset; a nil `Metrics` costs
nothing. Adapters to Prometheus or OpenTelemetry are left to the
application. Publisher and subscriber state is also available as
snapshots: `goose.Publisher.State`, `goose.Subscription.Supervision`,
`sv.Publisher.State` (frames, samples, rate, late, skipped, send errors)
and `sv.Subscription.Stats` (per stream: ASDUs, lost, duplicates, out of
order, rate, status).

The MMS client and server report to an `mms.Metrics` implementation
set with `DialOptions.Metrics` / `ServerOptions.Metrics`
(or `MMS.Metrics`, which takes precedence): `ObserveRequest` once per
confirmed request with service, duration and error, `ObserveConnection`
when an association is established, rejected or ends. Embed
`mms.NopMetrics` to implement a subset. Dial retries are logged at Warn
(`iec61850: dial failed, retrying` with `addr`, `attempt`, `backoff`, `error`).

### Counters

| Snapshot | Counts |
|----------|--------|
| `Server.Stats()` | `Connections` served by `ListenAndServe` now, `ConnectionsRefused` beyond `MaxConnections`, `CallbacksRecovered` (server hooks, providers, handlers and metrics that did not return normally) |
| `ReportSubscription.Stats()` | `ReportsDropped` (queue full), `PartialReportsDropped` (segmented reports given up: `MaxSegmentBytes`, `SegmentTimeout`, 65536 segments, interrupted), `CallbacksRecovered` (`OnOverflow`) |
| `goose.Subscription.Counters()`, `sv.Subscription.Counters()` | `StreamsRefused` (beyond `MaxStreams`), `CallbacksRecovered` (`Handler`, `OnASDU`) |
| `goose.Subscriber.CallbacksRecovered()`, `sv.Subscriber.CallbacksRecovered()` | `OnDecodeError` and `Metrics` calls that did not return normally |
| `rsession.Transport.Stats()` | `NotAllowed` (sender outside `AllowedSenders`) and `CallbacksRecovered` (`OnReject`, `KeyProvider`), with the other refusals |

## Application code that does not return normally

The library calls application code under a recover on its own goroutines
and those of go-mms: server hooks, providers, handlers and `Metrics`,
`OnOverflow`, the GOOSE `Handler`, `OnASDU`, `OnDecodeError`, subscriber
and publisher `Metrics`, `rsession` `OnReject` and `KeyProvider`. A call
that does not return normally is logged at error level with the value
it ended with (`value`) and its `stack` (the server and client also name
the `callback`, the GOOSE and SV metrics guards the `hook`), and counted
in the snapshots above. The request is answered with the
service's error, or the frame is dropped; the connection, the
subscription and the process go on. A `slog.Handler` given as `Logger`
is called as it is.

## Tracing

The library does not currently integrate with OpenTelemetry or similar tracing
frameworks. All networked operations accept `context.Context`, which provides
a natural integration point for future tracing support.
