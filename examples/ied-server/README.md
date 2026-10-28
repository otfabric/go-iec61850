# ied-server

A complete server: the demo IED with reports, logs, setting groups, files, a control and, with `-iface`, GOOSE and Sampled Values.

## What it shows

`NewServerModelFromSCL`, `NewServer` with identity, logger and connection hooks, `EnableReports`, `EnableJournals`, `EnableSettingGroups`, `RegisterControl`, `EnableGOOSE`, `EnableSV`, `SetValue` driving reports/GOOSE/SV/logs, `Capabilities`, shutdown with `Close`. The model is [`examples/internal/demoied/demo.icd`](../internal/demoied/demo.icd).

## Run

```bash
go run ./examples/ied-server -addr 127.0.0.1:10102 [-iface eth0]
```

Port 102 (the default) needs root on Linux; use `-addr` with a high port otherwise. MMS only needs TCP.

## Privileges

With `-iface`, GOOSE and SV need raw Ethernet access: on Linux run as root or grant the binary
`CAP_NET_RAW` (`sudo setcap cap_net_raw+ep ./ied-server`); on macOS and the
BSDs read/write access to `/dev/bpf*` (on macOS, membership of the
`access_bpf` group that Wireshark's ChmodBPF installs). Without it the
program exits with a permission error. Use an interface on the same
Ethernet segment (or VLAN) as the peers; multicast does not cross
routers.

## Test

`go test ./examples/ied-server` runs it in-process over loopback MMS, with GOOSE and SV on in-memory Ethernet hubs, without privileges.
