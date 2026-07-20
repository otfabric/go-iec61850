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

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LIBIEC61850_IMAGE` | Docker image for the libiec61850 adapter | `mms-interop-libiec61850:local` |
| `IEC61850BEAN_IMAGE` | Docker image for the iec61850bean adapter | `mms-interop-iec61850bean:local` |
| `IEC61850_SERVER_BINARY` | Path to `libiec61850-ied-server` binary (skips Docker) | — |
| `IEC61850_CLIENT_BINARY` | Path to `libiec61850-ied-client` binary (skips Docker) | — |
| `IEC61850_REPORTER_BINARY` | Path to `libiec61850-ied-reporter` binary (skips Docker) | — |
| `IEC61850_FIXTURE_DIR` | Directory containing `interop.icd` and `values.json` | `testdata` |

## Fixture

`testdata/interop.icd` is the SCL model used by both the adapter servers and the `go-iec61850` server fixture. `testdata/values.json` contains the initial runtime values. Both are versioned alongside the pinned adapter images.

See [mms-interop](https://github.com/otfabric/mms-interop) for adapter image publication and the JSON Lines adapter contract.
