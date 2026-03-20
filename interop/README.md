# Interoperability Testing

This directory contains scaffolding for interoperability testing of
`go-iec61850` against third-party IEC 61850 implementations.

## Test strategy

Interop testing validates that the Go library correctly communicates with
real-world IEC 61850 servers and clients, not just the co-developed `go-mms`
loopback. Testing covers:

1. **Client -> C server** — connect the Go client to a libiec61850-based server
2. **C client -> Go server** — connect a libiec61850-based client to the Go server
3. **Protocol conformance** — verify MMS PDU compatibility at the wire level

## Prerequisites

- Docker (for containerized libiec61850 server/client)
- Or: locally built libiec61850 (https://github.com/mz-automation/libiec61850)

## Test matrix

| Test | Go role | Counterpart | Status |
|------|---------|-------------|--------|
| Browse logical devices | Client | libiec61850 server | Implemented |
| Browse logical nodes | Client | libiec61850 server | Implemented |
| Read single value | Client | libiec61850 server | Implemented |
| Write single value | Client | libiec61850 server | Implemented |
| List datasets | Client | libiec61850 server | Implemented |
| Read dataset | Client | libiec61850 server | Implemented |
| List reports | Client | libiec61850 server | Implemented |
| Get report control block | Client | libiec61850 server | Implemented |
| List files | Client | libiec61850 server | Implemented |
| Full tree browse | Client | libiec61850 server | Implemented |
| Report subscription | Client | libiec61850 server | Planned |
| Server identify | Server | libiec61850 client | Planned |
| Server read | Server | libiec61850 client | Planned |

## Running with Docker

A Dockerfile for a libiec61850 test server will be added here. The general
workflow is:

```bash
# Build and start the C server
docker build -t iec61850-test-server .
docker run -d --name iec61850-srv -p 102:102 iec61850-test-server

# Run interop tests
go test -tags interop ./interop/...

# Clean up
docker stop iec61850-srv && docker rm iec61850-srv
```

## Running locally

If libiec61850 is installed locally:

```bash
# Start the libiec61850 example server
./libiec61850/examples/server_example_basic/server_example_basic &

# Run interop tests
IEC61850_INTEROP_ADDR=127.0.0.1:102 go test -tags interop ./interop/...

# Stop the server
kill %1
```

## Test file structure

```
interop/
├── README.md          <- this file
├── interop_test.go    <- Go interop tests (10 tests implemented)
├── Dockerfile         <- libiec61850 test server (planned)
└── testdata/
    └── test.scd       <- SCL config for the test server (planned)
```

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `IEC61850_INTEROP_ADDR` | Address of the test server | `127.0.0.1:102` |
| `IEC61850_INTEROP_SKIP` | Set to `1` to skip interop tests | unset |

## Notes

- Interop tests are behind the `interop` build tag and do not run in normal
  `go test` invocations.
- These tests require network access and a running counterpart — they are
  integration tests, not unit tests.
- The test matrix will grow as more features are validated.

## Server runtime conformance checklist

This checklist documents tested server-side state machine behaviors (M16):

### Report engine
- [x] Enable/disable RCB is idempotent
- [x] GI on disabled RCB is a no-op
- [x] Resv on BRCB is rejected
- [x] PurgeBuf clears the buffer queue
- [x] Buffer overflow drops oldest entry, sets bufOvfl flag
- [x] Stop is idempotent (multiple calls safe)
- [x] SetValue after Stop is a no-op (no panic)
- [x] Concurrent enable/disable is race-free
- [x] Concurrent SetValue from multiple goroutines is race-free
- [x] Concurrent GI triggers are race-free
- [x] Invalid RptEna/GI/Resv types are rejected
- [x] Unknown RCB writes are silently ignored

### Control runtime
- [x] Operate without prior Select is rejected (SBO model)
- [x] Duplicate RegisterControl is rejected
- [x] StatusOnly ctlModel is rejected
- [x] Empty ldName/doRef is rejected
- [x] Concurrent SBO/operate from multiple goroutines is race-free
- [x] Owner/ctlNum mismatch detection under mutex (no data race)

### Setting group engine
- [x] ActSG=0 or out-of-range is rejected
- [x] EditSG out-of-range is rejected
- [x] CnfEdit without active edit session is rejected
- [x] Double edit for different group is rejected
- [x] Unknown LD is rejected
- [x] Read-only subfield writes are rejected
- [x] Invalid type for ActSG is rejected
- [x] Handler rejection propagates error
- [x] Concurrent ActSG writes are race-free

### Journal provider
- [x] Overflow respects maxEntries cap
- [x] Empty journal returns empty result
- [x] Non-existent journal returns empty result
- [x] Concurrent add/read is race-free

### Server lifecycle
- [x] Server.Close is idempotent
- [x] Close without engines is safe
- [x] SetValue without engines stores value without panic
- [x] Full lifecycle (Identify + Status + Report + Journal) round-trip
