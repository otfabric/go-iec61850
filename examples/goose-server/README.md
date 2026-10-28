# goose-server

An IEC 61850 server that publishes its GoCBs on data change.

## What it shows

`Server.EnableGOOSE`: `SetValue` on a dataset member publishes a new state; the GoCB is served over MMS.

## Run

```bash
go build -o goose-server ./examples/goose-server && sudo ./goose-server -iface eth0 -addr 127.0.0.1:10102
```

Watch it with `goose-subscriber -any-ref`, control it with `goose-client`.

## Privileges

Needs raw Ethernet access: on Linux run as root or grant the binary
`CAP_NET_RAW` (`sudo setcap cap_net_raw+ep ./goose-server`); on macOS and the
BSDs read/write access to `/dev/bpf*` (on macOS, membership of the
`access_bpf` group that Wireshark's ChmodBPF installs). Without it the
program exits with a permission error. Use an interface on the same
Ethernet segment (or VLAN) as the peers; multicast does not cross
routers.

## Test

`go test ./examples/goose-server` runs it in-process with GOOSE on an in-memory Ethernet hub and MMS over loopback, without privileges.
