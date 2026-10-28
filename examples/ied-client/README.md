# ied-client

A complete client application: connect, identify, discover, read, subscribe to reports, shut down cleanly.

## What it shows

slog logging, `MMS().Identify`, model discovery, `GetReportControlBlock` and `SubscribeReport` routed by RptID, graceful shutdown on SIGINT/SIGTERM.

## Run

```bash
go run ./examples/ied-client -addr 127.0.0.1:10102
```

Start the server first: `go run ./examples/ied-server -addr 127.0.0.1:10102` (port 102 needs root on Linux), then pass `-addr 127.0.0.1:10102`.

## Test

`go test ./examples/ied-client` runs it in-process against the demo IED over loopback MMS, without privileges.
