# goose-publisher

Publish a GOOSE control block from SCL and toggle a boolean member.

## What it shows

`goose.ControlBlockFromSCL`, `ControlBlock.PublisherConfig` (address, references, MinTime/MaxTime, dataset shape), `goose.NewPublisher`, `Run` (retransmission), `Publish` (new state).

## Run

```bash
go build -o goose-publisher ./examples/goose-publisher && sudo ./goose-publisher -iface eth0
```

Defaults to `gcb01` of the demo IED; `-scl`, `-ied`, `-ld`, `-gocb` select another control block.

## Privileges

Needs raw Ethernet access: on Linux run as root or grant the binary
`CAP_NET_RAW` (`sudo setcap cap_net_raw+ep ./goose-publisher`); on macOS and the
BSDs read/write access to `/dev/bpf*` (on macOS, membership of the
`access_bpf` group that Wireshark's ChmodBPF installs). Without it the
program exits with a permission error. Use an interface on the same
Ethernet segment (or VLAN) as the peers; multicast does not cross
routers.

## Test

`go test ./examples/goose-publisher` runs it in-process on an in-memory Ethernet hub, with a peer configured from the same SCL, without privileges.
