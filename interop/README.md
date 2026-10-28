# go-iec61850 interoperability tests

This package contains interoperability tests for `go-iec61850` against independent IEC 61850 implementations provided by [iec61850-interop](https://github.com/otfabric/iec61850-interop).

## Test directions

| File | Go role | Adapter counterpart | Phase |
|------|---------|---------------------|-------|
| `libiec61850_client_test.go` | IEC 61850 client | `libiec61850-ied-server` | 2A |
| `libiec61850_server_test.go` | IEC 61850 server | `libiec61850-ied-client` | 2A |
| `iec61850bean_client_test.go` | IEC 61850 client | `iec61850bean-ied-server` | 2B |
| `iec61850bean_server_test.go` | IEC 61850 server | `iec61850bean-ied-client` | 2B |
| `libiec61850_urcb_client_test.go` | URCB client | `libiec61850-ied-server` | 2C-a |
| `libiec61850_urcb_server_test.go` | URCB server | `libiec61850-ied-reporter` | 2C-b |
| `goose_test.go` | GOOSE publisher, subscriber, IED server GoCB | `libiec61850-goose-*`, `dscsystems-goose-*`, `iec61850-rust-goose-*` | GOOSE |
| `sv_test.go` | SV publisher, subscriber, IED server MSVCB; MSVCB over MMS | `*-sv-publisher`, `*-sv-subscriber`, `*-ied-server` | SV |

The `TestLibIEC*` tests drive the libiec61850 adapters by default. With
`IEC61850_PEER_STACK=dscsystems` the same tests drive the
[dscsystems](https://github.com/dscsystems/go-iec61850) adapters, which
implement the same commands, flags, fixture and JSON Lines contract under
the `dscsystems-` prefix; `IEC61850_PEER_STACK=iec61850-rust` drives the
[iec61850-rust](https://github.com/csp0924/iec61850-rust) adapters
(`iec61850-rust-` prefix). CI runs one pass per stack; the shared
iec61850bean server is only started for the libiec61850 pass. Steps an
adapter reports as unsupported (`{"event":"error","code":"unsupported"}`
or a step error `unsupported`) skip the test with the adapter's reason.
The GOOSE and SV tests run against every stack in turn.

Tests start adapter containers, wait for the readiness event, exercise the `go-iec61850` API, and assert results. No pre-running containers are required.

## Running

```bash
# Build the adapter images first (in iec61850-interop)
cd ../iec61850-interop && make build

# Run all interop tests
LIBIEC61850_IMAGE=iec61850-interop-libiec61850:local \
IEC61850BEAN_IMAGE=iec61850-interop-iec61850bean:local \
make interop

# Or using published images
LIBIEC61850_IMAGE=ghcr.io/otfabric/iec61850-interop-libiec61850:v1.0.1 \
IEC61850BEAN_IMAGE=ghcr.io/otfabric/iec61850-interop-iec61850bean:v1.0.1 \
make interop
```

## GOOSE

GOOSE is raw Ethernet (EtherType 0x88B8), so the test opens a network
interface on the same layer-2 segment as the adapter containers: a Docker
bridge network with the bridge device `goose0` (iec61850-interop `docs/goose.md`).
Opening the interface needs `CAP_NET_RAW`, hence `-exec 'sudo -E'`:

```bash
docker network create -o com.docker.network.bridge.name=goose0 iec61850-interop-goose
GOOSE_IFACE=goose0 make interop-goose
```

The tests cover, against libiec61850, dscsystems and iec61850-rust:

- Go publisher → adapter subscriber: header fields, typed values of the
  `goose-v1` fixture (`values`, then `update` as stNum 2),
  retransmissions, supervision timeout after the publisher stops.
- Adapter publisher → Go subscriber: the subscription is built from the
  SCL (`goose.ControlBlockFromSCL`) and validates datSet, goID, confRev
  and member types; VLAN tag, values, state change via `{"cmd":"set"}`,
  timeout after `{"cmd":"stop"}`.
- Go IED server (`Server.EnableGOOSE`) → adapter subscriber: a
  `SetValue` on a dataset member produces a new state.

On macOS (Docker Desktop) the bridge lives in the Linux VM; build the test
binary for Linux and run it in a container on the network:

```bash
CGO_ENABLED=0 GOOS=linux go test -c -tags=interop -o /tmp/interop.test ./interop
docker run --rm --network iec61850-interop-goose --cap-add NET_RAW \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /tmp/interop.test:/interop.test:ro -v "$PWD/interop/testdata":/testdata:ro \
  -e GOOSE_IFACE=eth0 -e IEC61850_FIXTURE_DIR=/testdata -e IEC61850_SKIP_BEAN=1 \
  docker:27-cli /interop.test -test.run GOOSE -test.v
```

## Sampled Values

SV is raw Ethernet too (EtherType 0x88BA) and uses the same network and
`GOOSE_IFACE`; `make interop-goose` runs both (`-run 'GOOSE|SV_'`).
The SV tests also need `IEC61850_SV_FIXTURE=sv-v1`, which says the
pinned adapter images ship the SV fixture (iec61850-interop v0.4.0 or later).
Fixture `sv-v1` (iec61850-interop `fixtures/sv/`, copied to
`interop/testdata/sv/`): `MSVCB01` (9-2LE, 4000 samples/s, one ASDU per
frame) and `MSVCB02` (generic dataset, 1000 samples/s, four ASDUs per
frame, every optional field). Against libiec61850, dscsystems and
iec61850-rust:

- Go publisher → adapter subscriber, both streams: header and optional
  fields, values, `UpdateSample` reaching the subscriber, and the
  adapter's `stats` (no duplicates, at most 1 % lost or reordered).
  libiec61850's subscriber drops ASDUs with a two-octet smpMod (iec61850-interop
  `docs/sv.md`), so `MSVCB02` → libiec61850 is checked as a known
  limitation: it must still receive nothing.
- Adapter publisher → Go subscriber, both streams: the subscription is
  built from the SCL (`sv.ControlBlockFromSCL`) and validates datSet,
  confRev, smpRate and sample size; values before and after
  `{"cmd":"set"}`, loss accounting against the publisher's `stopped`
  totals, the measured rate, timeout after `{"cmd":"stop"}`.
- Go IED server (`Server.EnableSV`) → adapter subscriber: `SetValue` on a
  dataset member reaches the samples.
- `TestLibIECClient_SVCB_Read` reads `LLN0$MS$MSVCB02` from the peer IED
  server where `capabilities.json` `svcbOverMms` is true (libiec61850,
  dscsystems).

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LIBIEC61850_IMAGE` | Docker image for the libiec61850 adapter | `iec61850-interop-libiec61850:local` |
| `IEC61850BEAN_IMAGE` | Docker image for the iec61850bean adapter | `iec61850-interop-iec61850bean:local` |
| `DSCSYSTEMS_IMAGE` | Docker image for the dscsystems adapter | `iec61850-interop-dscsystems:local` |
| `IEC61850RUST_IMAGE` | Docker image for the iec61850-rust adapter | `iec61850-interop-iec61850-rust:local` |
| `IEC61850_PEER_STACK` | Stack behind the `TestLibIEC*` tests: `libiec61850`, `dscsystems` or `iec61850-rust` | `libiec61850` |
| `IEC61850_SKIP_BEAN` | Do not start the shared iec61850bean server | — |
| `GOOSE_IFACE` | Interface on the GOOSE network (`goose0` on the host, `eth0` in a container); GOOSE tests skip when unset | — |
| `IEC61850_SV_FIXTURE` | SV fixture revision the adapter images implement (`sv-v1` from iec61850-interop v0.4.0); SV tests skip unless it matches | — |
| `GOOSE_NETWORK` | Docker network of the GOOSE adapters (created if missing) | `iec61850-interop-goose` |
| `IEC61850_SERVER_BINARY` | Path to `libiec61850-ied-server` binary (skips Docker) | — |
| `IEC61850_CLIENT_BINARY` | Path to `libiec61850-ied-client` binary (skips Docker) | — |
| `IEC61850_REPORTER_BINARY` | Path to `libiec61850-ied-reporter` binary (skips Docker) | — |
| `IEC61850_FIXTURE_DIR` | Directory containing `interop.icd` and `values.json` | `testdata` |

## Fixture

`testdata/interop.icd` is the SCL model used by both the adapter servers and the `go-iec61850` server fixture. `testdata/values.json` contains the initial runtime values. `testdata/goose/` holds the `goose-v1` fixture (`interop-goose.cid`, `goose.json`). Both are versioned alongside the pinned adapter images.

See [iec61850-interop](https://github.com/otfabric/iec61850-interop) for adapter image publication and the JSON Lines adapter contract.
