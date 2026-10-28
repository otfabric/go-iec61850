# Examples

Runnable programs for go-iec61850. Each directory has a README (what it
shows, how to run it, privileges) and a `main_test.go` smoke test that
runs the program in-process: MMS over loopback, GOOSE and Sampled Values
on an in-memory Ethernet hub. `go test ./examples/...` runs them all
without privileges; `go build ./examples/...` builds them.

The servers and the defaults of the clients use the demo IED in
[internal/demoied/demo.icd](internal/demoied/demo.icd): IED `DemoIED`,
logical device `LD1` (the MMS domain of the go-iec61850 server), with a
measurement, a controllable output, an unbuffered report, a log, two
setting groups, a GOOSE control block `gcb01` and a 9-2LE SV control
block `MSVCB01`. Start a server with

```bash
go run ./examples/ied-server -addr 127.0.0.1:10102
```

and point the clients at it with `-addr 127.0.0.1:10102`.

| Example | Shows |
|---|---|
| **MMS client** | |
| [basic-client](basic-client) | Connect, list logical devices and nodes, read a value |
| [browse-tree](browse-tree) | The model tree with functional constraints |
| [ied-client](ied-client) | A complete client: identify, discover, read, report subscription, shutdown |
| [resilient-client](resilient-client) | Retry with backoff, slog, metrics, reconnecting after a lost connection |
| [reports](reports) | Report control blocks and report subscriptions |
| [datasets](datasets) | List, read, create and delete data sets |
| [setting-groups](setting-groups) | Edit and activate setting groups |
| [journal](journal) | Read and follow logs (journals) |
| [control](control) | Direct, SBO and SBOw control |
| [files](files) | List and download files |
| **MMS server** | |
| [ied-server](ied-server) | A server with reports, logs, setting groups, files, a control, GOOSE and SV |
| **GOOSE** ([GOOSE.md](../GOOSE.md)) | |
| [goose-publisher](goose-publisher) | Publish a GoCB from SCL |
| [goose-subscriber](goose-subscriber) | Subscribe to and supervise a GoCB from SCL |
| [goose-server](goose-server) | A server publishing its GoCBs on data change |
| [goose-client](goose-client) | Read and write a GoCB over MMS |
| **Sampled Values** ([SV.md](../SV.md)) | |
| [sv-publisher](sv-publisher) | A 9-2LE merging unit with a synthetic waveform |
| [sv-subscriber](sv-subscriber) | Receive a stream with statistics and RMS values |
| [svcb-client](svcb-client) | Read and reconfigure an MSVCB over MMS |

The GOOSE and SV programs (and `ied-server -iface`) open a raw Ethernet
socket: on Linux they need root or `CAP_NET_RAW`, on macOS read/write
access to `/dev/bpf*`.
