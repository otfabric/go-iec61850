# go-iec61850 interoperability tests

This package contains interoperability tests for `go-iec61850` against independent IEC 61850 implementations provided by [mms-interop](https://github.com/otfabric/mms-interop).

## Test directions

| File | Go role | Adapter counterpart | Phase |
|------|---------|---------------------|-------|
| `libiec61850_client_test.go` | IEC 61850 client | `libiec61850-ied-server` | 2A |
| `libiec61850_server_test.go` | IEC 61850 server | `libiec61850-ied-client` | 2A |
| `iec61850bean_client_test.go` | IEC 61850 client | `iec61850bean-ied-server` | 2B |
| `iec61850bean_server_test.go` | IEC 61850 server | `iec61850bean-ied-client` | 2B |
| `libiec61850_urcb_client_test.go` | URCB client | `libiec61850-ied-server` | 2C-a |
| `libiec61850_urcb_server_test.go` | URCB server | `libiec61850-ied-reporter` | 2C-b |
| `goose_test.go` | GOOSE publisher, subscriber, IED server GoCB | `libiec61850-goose-*`, `dscsystems-goose-*` | GOOSE |

The `TestLibIEC*` tests drive the libiec61850 adapters by default. With
`IEC61850_PEER_STACK=dscsystems` the same tests drive the
[dscsystems](https://github.com/dscsystems/go-iec61850) adapters, which
implement the same commands, flags, fixture and JSON Lines contract under
the `dscsystems-` prefix (CI runs both passes; the shared iec61850bean
server is not started for the dscsystems pass).

Tests start adapter containers, wait for the readiness event, exercise the `go-iec61850` API, and assert results. No pre-running containers are required.

## Running

```bash
# Build the adapter images first (in mms-interop)
cd ../mms-interop && make build

# Run all interop tests
LIBIEC61850_IMAGE=mms-interop-libiec61850:local \
IEC61850BEAN_IMAGE=mms-interop-iec61850bean:local \
make interop

# Or using published images
LIBIEC61850_IMAGE=ghcr.io/otfabric/mms-interop-libiec61850:v0.1.0 \
IEC61850BEAN_IMAGE=ghcr.io/otfabric/mms-interop-iec61850bean:v0.1.0 \
make interop
```

## GOOSE

GOOSE is raw Ethernet (EtherType 0x88B8), so the test opens a network
interface on the same layer-2 segment as the adapter containers: a Docker
bridge network with the bridge device `goose0` (mms-interop `docs/goose.md`).
Opening the interface needs `CAP_NET_RAW`, hence `-exec 'sudo -E'`:

```bash
docker network create -o com.docker.network.bridge.name=goose0 mms-interop-goose
GOOSE_IFACE=goose0 make interop-goose
```

The tests cover, against both libiec61850 and dscsystems:

- Go publisher → adapter subscriber: header fields, typed values of the
  `goose-v1` fixture (`values`, then `update` as stNum 2),
  retransmissions, supervision timeout after the publisher stops.
- Adapter publisher → Go subscriber: the subscription is built from the
  SCL (`goose.ControlBlockFromSCL`) and validates datSet, goID, confRev
  and member types; VLAN tag, values, state change via `{"cmd":"set"}`,
  timeout after `{"cmd":"stop"}`.
- Go IED server (`Server.EnableGOOSE`) → libiec61850 subscriber: a
  `SetValue` on a dataset member produces a new state.

On macOS (Docker Desktop) the bridge lives in the Linux VM; build the test
binary for Linux and run it in a container on the network:

```bash
CGO_ENABLED=0 GOOS=linux go test -c -tags=interop -o /tmp/interop.test ./interop
docker run --rm --network mms-interop-goose --cap-add NET_RAW \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /tmp/interop.test:/interop.test:ro -v "$PWD/interop/testdata":/testdata:ro \
  -e GOOSE_IFACE=eth0 -e IEC61850_FIXTURE_DIR=/testdata -e IEC61850_SKIP_BEAN=1 \
  docker:27-cli /interop.test -test.run GOOSE -test.v
```

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LIBIEC61850_IMAGE` | Docker image for the libiec61850 adapter | `mms-interop-libiec61850:local` |
| `IEC61850BEAN_IMAGE` | Docker image for the iec61850bean adapter | `mms-interop-iec61850bean:local` |
| `DSCSYSTEMS_IMAGE` | Docker image for the dscsystems adapter | `mms-interop-dscsystems:local` |
| `IEC61850_PEER_STACK` | Stack behind the `TestLibIEC*` tests: `libiec61850` or `dscsystems` | `libiec61850` |
| `IEC61850_SKIP_BEAN` | Do not start the shared iec61850bean server | — |
| `GOOSE_IFACE` | Interface on the GOOSE network (`goose0` on the host, `eth0` in a container); GOOSE tests skip when unset | — |
| `GOOSE_NETWORK` | Docker network of the GOOSE adapters (created if missing) | `mms-interop-goose` |
| `IEC61850_SERVER_BINARY` | Path to `libiec61850-ied-server` binary (skips Docker) | — |
| `IEC61850_CLIENT_BINARY` | Path to `libiec61850-ied-client` binary (skips Docker) | — |
| `IEC61850_REPORTER_BINARY` | Path to `libiec61850-ied-reporter` binary (skips Docker) | — |
| `IEC61850_FIXTURE_DIR` | Directory containing `interop.icd` and `values.json` | `testdata` |

## Fixture

`testdata/interop.icd` is the SCL model used by both the adapter servers and the `go-iec61850` server fixture. `testdata/values.json` contains the initial runtime values. `testdata/goose/` holds the `goose-v1` fixture (`interop-goose.cid`, `goose.json`). Both are versioned alongside the pinned adapter images.

See [mms-interop](https://github.com/otfabric/mms-interop) for adapter image publication and the JSON Lines adapter contract.
