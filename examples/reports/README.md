# reports

Subscribe to a report control block and print the reports.

## What it shows

`ListReports`, `GetReportControlBlock`, `SubscribeReport` with `AutoEnable`, `GIOnSubscribe` and an overflow policy; reason codes per value.

## Run

```bash
go run ./examples/reports -addr 127.0.0.1:10102
```

Start the server first: `go run ./examples/ied-server -addr 127.0.0.1:10102` (port 102 needs root on Linux), then pass `-addr 127.0.0.1:10102`. Its measurement changes every two seconds.

## Test

`go test ./examples/reports` runs it in-process against the demo IED over loopback MMS, without privileges.
