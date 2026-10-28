# goose-subscriber

Subscribe to and supervise a GOOSE control block from SCL.

## What it shows

`ControlBlock.SubscriptionConfig` (filters and validation: confRev, member types, gocbRef, datSet), `OnlyStateChanges`, events (state changes, timeAllowedToLive timeouts, invalid messages via `errors.Is` on the validation sentinels), per-stream `Supervision`; joins the multicast group instead of going promiscuous.

## Run

```bash
go build -o goose-subscriber ./examples/goose-subscriber && sudo ./goose-subscriber -iface eth0
```

Run `goose-publisher` (same SCL references) or `goose-server` (add `-any-ref`: the go-iec61850 server publishes with the bare LD as MMS domain) on the segment.

## Privileges

Needs raw Ethernet access: on Linux run as root or grant the binary
`CAP_NET_RAW` (`sudo setcap cap_net_raw+ep ./goose-subscriber`); on macOS and the
BSDs read/write access to `/dev/bpf*` (on macOS, membership of the
`access_bpf` group that Wireshark's ChmodBPF installs). Without it the
program exits with a permission error. Use an interface on the same
Ethernet segment (or VLAN) as the peers; multicast does not cross
routers.

## Test

`go test ./examples/goose-subscriber` runs it in-process on an in-memory Ethernet hub, with a peer configured from the same SCL, without privileges.
