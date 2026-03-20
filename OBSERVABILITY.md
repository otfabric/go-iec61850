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
| `Debug` | Detailed protocol flow: MMS requests/responses, cache hits/misses, report decoding steps |
| `Info` | Lifecycle events: connection established, subscription created, server started |
| `Warn` | Recoverable issues: report overflow (dropped), best-effort cleanup failures, unexpected server behavior |
| `Error` | Failures that affect correctness: decode errors, registration failures |

### Structured attributes

Log entries use structured key-value attributes for machine-parseable output:

```
level=INFO msg="iec61850: report subscription created" rptID=rpt01 queueSize=64 overflow=drop_newest
level=WARN msg="iec61850: report queue overflow" rptID=rpt01 dropped=1
level=DEBUG msg="iec61850: browse cache hit" ld=LD1 cached_nodes=12
```

### Logger propagation

The logger configured on `Client` or `Server` is also passed to the underlying
`go-mms` layer (when not already configured), so MMS-level protocol events
appear in the same log stream.

## Metrics

The library does not currently emit metrics (counters, gauges, histograms).
Applications that need metrics can instrument at the boundary by wrapping
Client methods or processing log output.

## Tracing

The library does not currently integrate with OpenTelemetry or similar tracing
frameworks. All networked operations accept `context.Context`, which provides
a natural integration point for future tracing support.
