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
| `Error` | Failures that affect correctness: decode errors, registration failures, control rejections |

### Structured attributes

Log entries use structured key-value attributes for machine-parseable output:

```
level=INFO msg="iec61850: report subscription created" rptID=rpt01 queueSize=64 overflow=drop_newest
level=WARN msg="iec61850: report queue overflow" rptID=rpt01 dropped=1
level=DEBUG msg="iec61850: browse cache hit" ld=LD1 cached_nodes=12
level=INFO msg="iec61850: control operate" ref=LD1/XCBR1.Pos operation=operate
level=DEBUG msg="iec61850: setting group change" ld=LD1 actSG=2
```

### Logger propagation

The logger configured on `Client` or `Server` is also passed to the underlying
`go-mms` layer (when not already configured), so MMS-level protocol events
appear in the same log stream.

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

## SCL diagnostics

The SCL parser and validator use a structured diagnostic system rather than
slog logging. Parse and validation issues are returned as `[]scl.Diagnostic`
values with severity, code, path, and message fields. See `ERRORS.md` for
details on diagnostic codes.

## Metrics

The library does not currently emit metrics (counters, gauges, histograms).
Applications that need metrics can instrument at the boundary by wrapping
Client methods or processing log output.

## Tracing

The library does not currently integrate with OpenTelemetry or similar tracing
frameworks. All networked operations accept `context.Context`, which provides
a natural integration point for future tracing support.
