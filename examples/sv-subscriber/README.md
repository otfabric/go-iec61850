# sv-subscriber

Receive a Sampled Values stream from SCL and report its health and RMS values.

## What it shows

`ControlBlock.SubscriptionConfig`, the zero-allocation `OnASDU` callback with `sv.DecodeLE`, supervision events (start, gap, timeout, synch, invalid), per-stream `Stats` (lost, duplicates, out of order, measured rate).

## Run

```bash
go build -o sv-subscriber ./examples/sv-subscriber && sudo ./sv-subscriber -iface eth0
```

Run `sv-publisher` or `ied-server -iface` on the segment.

## Privileges

Needs raw Ethernet access: on Linux run as root or grant the binary
`CAP_NET_RAW` (`sudo setcap cap_net_raw+ep ./sv-subscriber`); on macOS and the
BSDs read/write access to `/dev/bpf*` (on macOS, membership of the
`access_bpf` group that Wireshark's ChmodBPF installs). Without it the
program exits with a permission error. Use an interface on the same
Ethernet segment (or VLAN) as the peers; multicast does not cross
routers.

## Test

`go test ./examples/sv-subscriber` runs it in-process on an in-memory Ethernet hub, with a peer configured from the same SCL, without privileges.
