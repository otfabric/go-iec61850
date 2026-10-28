# resilient-client

A long-running poller that survives server restarts.

## What it shows

go-mms v1.2 client options: `DialOptions.Retry` (`mms.DefaultRetryPolicy`, exponential backoff with jitter, `OnRetry`), `DialOptions.Logger` (slog JSON), `DialOptions.Metrics` (an `mms.Metrics` embedding `mms.NopMetrics`); reconnecting on `ErrClosed` / `mms.IsRetryable`.

## Run

```bash
go run ./examples/resilient-client -addr 127.0.0.1:10102
```

Start the server first: `go run ./examples/ied-server -addr 127.0.0.1:10102` (port 102 needs root on Linux), then pass `-addr 127.0.0.1:10102`. Stop and restart it to see the client reconnect; the metrics are printed at exit.

## Test

`go test ./examples/resilient-client` runs it in-process against the demo IED over loopback MMS, without privileges.
