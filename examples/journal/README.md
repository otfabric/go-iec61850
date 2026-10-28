# journal

Read and follow IEC 61850 logs (MMS journals).

## What it shows

`ListJournals`, `ReadJournalAll` over a time range, and polling with `ReadJournalAfterAll` from the last entry ID, as a historian does.

## Run

```bash
go run ./examples/journal -addr 127.0.0.1:10102 -follow
```

Start the server first: `go run ./examples/ied-server -addr 127.0.0.1:10102` (port 102 needs root on Linux), then pass `-addr 127.0.0.1:10102`. Its measurement changes are logged in `LLN0$EventLog`.

## Test

`go test ./examples/journal` runs it in-process against the demo IED over loopback MMS, without privileges.
