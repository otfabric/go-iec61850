# svcb-client

Read and reconfigure a multicast SV control block (MSVCB) over MMS.

## What it shows

`GetMSVCB`, `SetMSVCB` (SvEna written last) changing SmpRate and OptFlds, `sv.SamplesPerSecond`.

## Run

```bash
go run ./examples/svcb-client -addr 127.0.0.1:10102 -rate 96
```

Run it against `ied-server -iface eth0`. It needs no raw socket.

## Test

`go test ./examples/svcb-client` runs it in-process against the demo IED (its SV on an in-memory hub) over loopback MMS, without privileges.
