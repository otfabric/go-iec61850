# sv-publisher

A software merging unit: a 9-2LE Sampled Values stream with a three-phase waveform.

## What it shows

`sv.ControlBlockFromSCL`, `ControlBlock.PublisherConfig`, a `Source` function filling `sv.LESample` per sample (`AmpsCount`, `VoltsCount`), pacing at 4000 samples/s, `AlignSecond`, `State()` counters.

## Run

```bash
go build -o sv-publisher ./examples/sv-publisher && sudo ./sv-publisher -iface eth0 -amps 100 -volts 63500
```

Defaults to `MSVCB01` of the demo IED (80 samples per period, one ASDU per frame).

## Privileges

Needs raw Ethernet access: on Linux run as root or grant the binary
`CAP_NET_RAW` (`sudo setcap cap_net_raw+ep ./sv-publisher`); on macOS and the
BSDs read/write access to `/dev/bpf*` (on macOS, membership of the
`access_bpf` group that Wireshark's ChmodBPF installs). Without it the
program exits with a permission error. Use an interface on the same
Ethernet segment (or VLAN) as the peers; multicast does not cross
routers.

## Test

`go test ./examples/sv-publisher` runs it in-process on an in-memory Ethernet hub, with a peer configured from the same SCL, without privileges.
