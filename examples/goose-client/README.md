# goose-client

Read and write a GoCB over MMS.

## What it shows

`GetGoCB`, `SetGoCB` (GoEna written last), and the temporarily-unavailable answer to a GoID write while the GoCB is enabled (`*mms.DataAccessError`).

## Run

```bash
go run ./examples/goose-client -addr 127.0.0.1:10102
```

Run it against `goose-server` (or `ied-server -iface`). It needs no raw socket.

## Test

`go test ./examples/goose-client` runs it in-process against the demo IED (its GOOSE on an in-memory hub) over loopback MMS, without privileges.
