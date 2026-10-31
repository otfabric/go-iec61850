# Testing

How to run each test tier. What the tests show per feature is in
[docs/FEATURE-STATUS.md](docs/FEATURE-STATUS.md).

## Tiers

| Tier | Command | Needs | CI |
|---|---|---|---|
| Unit | `make test` | nothing | `ci.yml`, Go 1.25–1.26 |
| Race | `make test-race` | nothing | `ci.yml` |
| End-to-end | `make e2e` | nothing | the same tests run in `ci.yml`, see below |
| Raw sockets | `sudo -E go test ./l2 ./goose` | root on Linux, or `GOOSE_TEST_IFACE` | `coverage-pkg-check` job, as root |
| Coverage | `make coverage-check`, `make coverage-pkg-check` | root for the raw socket tests | `ci.yml` |
| Fuzz | `make fuzz` | nothing | seconds in `ci.yml`, 5 minutes per target nightly (`fuzz.yml`) |
| Benchmarks | `make bench` | nothing | no |
| Interop | `make interop` | Docker and adapter images | `interop.yml` |
| GOOSE and SV interop | `make interop-goose` | Linux, root, Docker network | `interop.yml` |
| Generated code | `make scl-check-generate` | nothing | `ci.yml`, `generated` job |

`make check` is the pre-commit set: it formats, tidies, then runs vet, lint,
`vuln`, tests, race and coverage. It does not run `coverage-pkg-check`, `e2e`,
`fuzz` or interop.

## End-to-end

There is no `e2e/` directory. `make e2e` selects the tests that run a real
`Client` against a real `Server` over ISO/TCP on `127.0.0.1`:
`TestClientReconnect*`, `TestServerShutdown*`, `TestListenAndServe*`,
`TestConcurrent*` and `TestResourceExhaust*` in the root package, and the
`TestRun` smoke test of every example, which runs the program in-process
against the demo IED ([examples/README.md](examples/README.md)). The GOOSE and
SV examples use the in-memory `Hub`.

Most other client tests in the root package use an in-memory transport pair
(`loopbackPair`), which skips ISO/TCP but runs the same MMS encoding.

Peers in `make e2e` are this repository's own code: it proves nothing about
interoperability.

`ci.yml`'s shared job excludes examples through `test-exclude-regex`; the
`coverage-pkg-check` job runs `./...` and so runs them.

## Interop

Tests in `interop/` have the build tag `interop` and start adapter containers
themselves. Not every test in that directory has an independent peer: the
`TestGoServer_` tests of `brcb_test.go`, `brcb_resume_test.go`,
`quality_timestamp_test.go` and `multi_client_test.go` run go-iec61850 on both
sides. Setup, environment variables and results:
[INTEROP.md](INTEROP.md), [interop/README.md](interop/README.md),
[COMPATIBILITY.md](COMPATIBILITY.md#per-stack-matrix).

## Prerequisites and skips

No test has a `*_REQUIRE_*` switch, so a missing prerequisite skips rather
than fails. Tests that skip:

| Tests | Skip when |
|---|---|
| `l2`, `goose` raw transport | no raw socket permission, or no `GOOSE_TEST_IFACE` where an interface is needed |
| `scl`, `scl/internal/genir`, `sclparse` fixtures | a fixture or specification file is not present |
| `interop` GOOSE | `GOOSE_IFACE` is unset |
| `interop` SV | `IEC61850_SV_FIXTURE` is not `sv-v1` |
| `interop` others | the adapter answers `unsupported`, or a stack limitation noted in [COMPATIBILITY.md](COMPATIBILITY.md#per-stack-matrix) |

## Fault and leak tests

Run with `make test`:

- MMS: `TestChaosMMS*` (`chaos_test.go`) corrupt, truncate and drop frames
  from fixed seeds, and drop the connection.
- GOOSE: `TestChaosLossReorderDuplicate`, `TestChaosCorruption`.
- SV: `TestChaosAccounting`, `TestChaosMutation`.
- Goroutine leaks: `TestClient_NoGoroutineLeakOnConnectDisconnect`,
  `TestConcurrent_TimeoutDoesNotLeakGoroutines`.
- `readme_test.go` checks the README's code snippets.

## Coverage policy

- At least 90 % of statements per package: `make coverage-pkg-check`, enforced by
  `scripts/covergate.go`. CI runs it as root so that the `AF_PACKET` tests
  count; without root, the raw socket code is not covered.
- At least 90 % in total for the module and for `scl/cmd/sclparse`:
  `coverage-min` in `ci.yml`.

`scripts/covergate.go` reports these packages but does not enforce them:

| Excluded | Why |
|---|---|
| `/examples/` | programs, not library code |
| `/scl/cmd/sclgen` | the generator tool; its output is checked by `scl-check-generate`, and it has no tests of its own |
| `/scl/internal/raw/` | generated bindings |
| `/interop` | test-only code |

Do not lower a gate.

## Fuzzing

`make fuzz` runs a fixed list of targets for `FUZZTIME` (default 15s). It does
not list the two `sv` targets, `FuzzDecode` and `FuzzSubscriber`. `fuzz.yml`
discovers every target nightly, so run these two directly:

```bash
go test -run '^$' -fuzz '^FuzzSubscriber$' -fuzztime 30s ./sv
```

Every new decoder of untrusted input gets a target.

### When a target fails

1. Take the input from `testdata/fuzz/<Target>/` in the package, or, for a
   nightly failure, from the `fuzz-failure-<Target>-<run>` artifact.
2. Reproduce it: `go test -run '<Target>/<file>' <package>`.
3. Fix the decoder, and commit the input file with the fix. `go test` then
   replays it as a regression test.
