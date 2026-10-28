# basic-client

Connect, list logical devices and nodes, and read one measurement.

## What it shows

`iec61850.Dial`, `ListLogicalDevices`, `ListLogicalNodes`, `TreeWithOptions`, `Read`; telling a refused attribute (`ErrDataAccess`) apart from a failed connection.

## Run

```bash
go run ./examples/basic-client -addr 127.0.0.1:10102
```

Start the server first: `go run ./examples/ied-server -addr 127.0.0.1:10102` (port 102 needs root on Linux), then pass `-addr 127.0.0.1:10102`.

## Test

`go test ./examples/basic-client` runs it in-process against the demo IED over loopback MMS, without privileges.
