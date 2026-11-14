# Testing

How to run each test tier. What the tests show per feature is in
[docs/FEATURE-STATUS.md](docs/FEATURE-STATUS.md).

## Tiers

| Tier | Command | Needs | CI |
|---|---|---|---|
| Unit | `make test` | nothing | `ci.yml`, Go 1.26–1.27 |
| Race | `make test-race` | nothing | `ci.yml` |
| End-to-end | `make e2e` | nothing | the same tests run in `ci.yml`, see below |
| Raw sockets | `sudo -E go test ./l2 ./goose` | root on Linux, or `GOOSE_TEST_IFACE` | `coverage-pkg-check` job, as root |
| Coverage | `make coverage-check`, `make coverage-pkg-check` | root for the raw socket tests | `ci.yml` |
| Fuzz | `make fuzz` | nothing | seconds in `ci.yml`, 5 minutes per target nightly (`fuzz.yml`) |
| Benchmarks | `make bench` | nothing | no |
| Interop | `make interop` | Docker and adapter images | `interop.yml` |
| GOOSE and SV interop | `make interop-goose` | Linux, root, Docker network | `interop.yml` |
| GOOSE captures | `IEC61850_GOOSE_CAPTURES=<dir> go test -run TestDecodeCaptures ./goose/` | A directory of third-party pcapng/pcap captures (not committed), e.g. the SCL corpus's `external/cache/adsc` | local only |
| SCL corpus | `make corpus` | `IEC61850_SCL_CORPUS`: a checkout of the private otfabric/iec61850-scl-corpus | local only (the corpus is private) |
| Generated code | `make scl-check-generate` | nothing | `ci.yml`, `generated` job |

## When CI runs

| Event | Workflow | What runs |
|---|---|---|
| Pull request | `pr.yml` | `docs.yml` (docsync of the library, sclparse and sclgen; otfabric-policy) always; `ci.yml` and `interop.yml` unless every changed file is documentation; then `OT Fabric Required Checks` |
| Push to a branch | none | nothing; dispatch `ci.yml` or `interop.yml` to test a branch without a pull request |
| Push to `main` | `docs.yml` | docsync and otfabric-policy; the merged commit was tested as the pull request's merge commit |
| Nightly, by hand | `fuzz.yml`, `self-interop.yml` | 5 minutes per Fuzz target; this commit against the latest published go-iec61850 |
| Release | `release.yml` | the shared release workflows test the release commit again |

Documentation means an added or modified `*.md` file outside `.github/`,
`testdata/` and `vendor/`, other than `AGENTS.md`, `CLAUDE.md` and
`README.md` (`TestREADMESnippets` reads it). A deleted or renamed document,
or any other file, runs everything. `OT Fabric Required Checks` fails unless
every job that applies succeeded, the nested modules' included; its summary
lists them.

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

`TestE2E_*` (in `scl_model_e2e_test.go`, over `loopbackPair`) serve models
built from SCL: `TestE2E_SCLCorpusReadEveryFC` serves every IED of every SCL
file under `scl/testdata` and `interop/testdata` (vendor CIDs included) and
reads each `LN$FC` before anything is written, and
`TestE2E_SCLModelRejectsHostileWrites` checks that client writes to status
attributes and writes of the wrong type are refused and change nothing.

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
- Dialer: `TestChaosDialerHandshakeFaults` and `TestChaosDialerByteFaults`
  (`dial_dialer_test.go`) dial through a `DialOptions.Dialer` that resets
  connections during the handshake, or corrupts, truncates and resets the
  byte stream from fixed seeds over an in-memory `net.Pipe`; `Dial`, every
  operation and `Close` must end within their deadlines.
- GOOSE: `TestChaosLossReorderDuplicate`, `TestChaosCorruption`.
- SV: `TestChaosAccounting`, `TestChaosMutation`.
- Hostile clients: `TestChaosHostileClientsAgainstSCLModel`
  (`chaos_hostile_test.go`) has four clients write random values of random
  types (oversized strings, out-of-range ctlNum, read-only FCs) to an
  SCL-built server while the application updates status; the model must stay
  intact and serve every LN$FC afterwards.
- BRCB buffer: `TestChaos_BRCBOverflowAcrossReconnect`
  (`report_v118_chaos_test.go`) drops the association at random, overflows an
  eight-entry buffer while the client is away, and resynchronises; EntryIDs
  arrive once and in order, and every gap or refused resync is reported with
  BufOvfl. `TestChaos_BufTmUnderLoad` has four writers change a BRCB member
  with BufTm 20 ms; every value arrives once, in store order.
- Control checks: `TestChaos_ControlChecks` races SelectWithValue and Operate
  against changing CILO, RSYN and hooks; `TestChaos_ControlChecksHostileOper`
  (`control_checks_hostile_test.go`) writes malformed Oper structures (Check of
  every length and type, members missing, extra or of the wrong type): no
  operation runs with a check bit set while every check refuses.
- R-GOOSE/R-SV: `TestChaos_ForgedReplayedSPDUsKeyRotation`
  (`rsession/chaos_test.go`) mixes authentic SPDUs with replays, bit flips,
  truncations, unsecured, unknown-key and random datagrams while the key ring
  rotates and retires a key; only authentic SPDUs reach `Recv`, each once, and
  every datagram is counted in `Stats`.
- Setting groups: `TestChaos_SettingGroupEditSelectRace`.
- Lifecycle: `TestServerClose_ConcurrentWithRequests` (Close while
  associations write settings and the application calls SetValue, under
  `-race`), `rsession` `TestTransport_CloseContract` (Close twice and
  concurrently with Send and Recv; both return `ErrClosed` afterwards; no
  goroutine left).
- Callbacks that do not return normally (`server_callback_recover_test.go`,
  `goose/`, `sv/` and `rsession/callback_recover_test.go`,
  `setting_groups_handler_recover_test.go`): for each hook, provider,
  handler and `Metrics` the library calls, the request is answered with an
  error or the frame is dropped, the call is logged with its stack and
  counted, and the server or subscriber goes on serving other requests,
  associations and streams.
- Limits: each limit of SECURITY.md has a test at the limit and one past it
  (the Test column there); past it the input is refused or dropped and
  counted, and the default is checked where a zero option applies it.
- Goroutine leaks: `internal/leakcheck` compares goroutines by identifier,
  not by count, and has no tolerance. `TestMain` of the root package, `goose`,
  `sv`, `l2` and `rsession` (`leakcheck_main_test.go`) runs the tests and then
  fails the run when a goroutine started by them is still running after 10 s
  (goroutines that are ending get that long; the check passes as soon as none
  is left). It reports each goroutine with its stack;
  `GODEBUG=tracebackancestors=5` adds the goroutines that started them, which
  names the test. Within a test, `checkGoroutines` (the chaos tests above,
  `report_chaos_test.go`) and `leakcheck.Check` (`TestTransport_CloseContract`)
  apply the same comparison around one test. Tests in a `testing/synctest`
  bubble are checked by synctest itself: the bubble ends only when its
  goroutines have ended. The in-memory transport of the root tests
  (`loopbackPair(t)`) closes both ends when the test ends, which ends the MMS
  client and server goroutines that read them.
- Waiting without sleeping: tests wait for events (channels, hooks such as
  `OnRetry`, the first `Accept` of a listener), run in a `testing/synctest`
  bubble where time matters (timers of the server, publishers and
  subscribers on a Hub, the in-memory transport), or use
  `leakcheck.WaitBlocked` to know that a goroutine waits in a given function
  before they interrupt it. `time.Sleep` remains for fake time inside a
  bubble, for pacing a sender or simulating a slow operation, and inside
  helpers that poll a condition up to a deadline; none decides an outcome.
- `readme_test.go` checks the README's code snippets.

## Lifecycle contract matrix

The rows are the scenarios L1–L16 of the otfabric/engineering lifecycle
standard; each cell names the test that asserts the row's invariant for
the type, or n/a and why. They run with `make test`. The
`TestLifecycle_*` tests synchronise with channels or `testing/synctest`;
a `time.Sleep` in a bubble only advances its fake time.
Columns: **Client** (`Client`, `ReportSubscription`), **Server**,
**goose** (`Publisher`, `Subscriber`, `Subscription`), **sv** (the same),
**l2** (`Hub`/`HubPort`, `Demux`/`DemuxPort`, the raw transports),
**rsession** (`Transport`). Each test is in its column's package (the
root package for Client and Server); the `TestLifecycle_*` tests are in
`lifecycle_matrix_test.go` and `<package>/lifecycle_test.go`.

| | Client | Server | goose | sv | l2 | rsession |
|---|---|---|---|---|---|---|
| L1 Close twice | `TestClientClose_DisablesAndReleasesRCB`, `TestReportSubscriptionClose_QuietPeer` | `TestLifecycle_ServerCloseTwice` | `TestLifecycle_CloseTwice` | `TestLifecycle_CloseTwice` | `TestLifecycle_CloseTwice` | `TestTransport_CloseContract` |
| L2 Concurrent Close | `TestLifecycle_ConcurrentClose` | `TestLifecycle_ConcurrentClose` (Close and Shutdown) | `TestLifecycle_ConcurrentClose` | `TestLifecycle_ConcurrentClose` | `TestLifecycle_ConcurrentCloseDuringRecv` | `TestTransport_CloseContract` |
| L3 Close during a blocked read | `TestLifecycle_ClientCloseDuringPendingRead`, `TestOverflowBlock_FullQueueBlocksDispatchUntilClose`, `TestReportSubscription_ClosedWhenAssociationEnds`, `TestClient_DoneAndErr` | `TestServerShutdown_TCP`, `TestServerShutdown_AcceptAfterShutdownIsNotServed`, `TestServer_AcceptBackoff` (accept loop) | n/a: `Subscriber.Close` does not end `Run`; closing the transport does (l2 column) | `TestSubscriberClose_StopsRunOnQuietTransport` | `TestLifecycle_ConcurrentCloseDuringRecv`; raw: `TestRawTransportCloseAndCancel` (skips without an interface off darwin) | `TestRecvContextAndClose` |
| L4 Close during a write | `TestConcurrent_ManyWritesThenClose` | `TestServerClose_ConcurrentWithRequests` | `TestLifecycle_PublisherCloseDuringRun` | `TestLifecycle_PublisherCloseDuringRun` | n/a: `HubPort.Send` copies the frame for each port under the hub's lock; there is no shared stream | `TestTransport_CloseContract` |
| L5 Close during the handshake | `TestLifecycle_DialCancelDuringHandshake`, `TestDialDialerHonoursContext`, `TestChaosDialerHandshakeFaults` | `TestLifecycle_ShutdownFromOnConnect`; the COTP handshake is go-mms's (`iso.Listener`) | n/a: no handshake | n/a: no handshake | n/a: no handshake | n/a: datagrams, no handshake |
| L6 Cancel a pending request | `TestLifecycle_CancelledRequestReleasesItsSlot`, `TestConcurrent_ContextCancelReleasesInflightReads` | n/a: no request is pending on the server side | n/a: no request/response | n/a: no request/response | n/a: no request/response | n/a: no request/response |
| L7 Late response | `TestLifecycle_CancelledRequestReleasesItsSlot` (completes no other request; the count is go-mms's) | n/a | n/a | n/a | n/a | n/a |
| L8 Response after reconnect | n/a: no reconnect | n/a: no reconnect | n/a: no reconnect | n/a: no reconnect | n/a: no reconnect | n/a: no reconnect |
| L9 Callback calls a public method | `TestReportOnOverflow_MayCloseItsSubscription` | `TestLifecycle_ControlHandlerCallsServerMethods` | `TestLifecycle_HandlerCallsPublicMethods` | `TestLifecycle_OnASDUCallsPublicMethods` | n/a: no callbacks | `TestLifecycle_OnRejectCallsPublicMethods` |
| L10 Callback calls Close | `TestReportOnOverflow_MayCloseItsSubscription` | `TestServerShutdown_FromOnDisconnect`, `TestLifecycle_ShutdownFromOnConnect` | `TestClose_FromHandler`, `TestClose_FromAnotherGoroutineWaitsForHandler` | `TestLifecycle_OnASDUClosesSubscriber` | n/a: no callbacks | `TestClose_FromOnReject` |
| L11 Slow callback | `TestOverflowBlock_FullQueueBlocksDispatchUntilClose` (OverflowBlock holds the dispatch, as documented) | `TestLifecycle_SlowControlHandlerHoldsOnlyItsAssociation` | `TestLifecycle_SlowHandlerHoldsOnlyItsSubscription` | n/a: `OnASDU` runs on the receive goroutine by contract; the queue is L12 | n/a: no callbacks | n/a: `OnReject` runs on the receive goroutine and must not block, by contract |
| L12 Queue at capacity | `TestReportSubscription_StatsCountDrops`, `TestOverflowBlock_FullQueueBlocksDispatchUntilClose` | `TestServer_MaxConnections`, `TestChaos_BRCBOverflowAcrossReconnect` | `TestSubscriberOnlyStateChangesAndOverflow`, `TestMaxStreams_Boundaries` | `TestSubscriberSimulationAndChannels` (QueueDrops), `TestMaxStreams_Boundaries` | `TestHubOverflow` | `TestQueueFull` |
| L13 Concurrent sessions | n/a: one association | `TestChaos_URCBReservationRace`, `TestChaosHostileClientsAgainstSCLModel` | n/a: no sessions | n/a: no sessions | n/a: no sessions | n/a: no sessions |
| L14 Failure during construction | `TestLifecycle_SubscribeReportFailureRollsBack`, `TestLifecycle_DialCancelDuringHandshake` | n/a: `NewServer` opens nothing and starts no goroutine; `ListenAndServe` takes the caller's listener | `TestPublisherValidation`, `TestSubscriberValidation` | `TestPublisherConfigErrors`, `TestSubscriberEventsAndValidation` | `TestOpenUnknownInterface` | `TestLifecycle_OpenFailureAfterBindReleasesSocket`, `TestOpenConfigErrors` |
| L15 Shutdown(ctx) | n/a: no Shutdown; `Close(ctx)` bounds its remote cleanup (`TestClientClose_QuietPeerBounds`) | `TestServerShutdown_WaitsForConnectionGoroutines`, `TestServerShutdown_TCP`, `TestServerShutdown_WithHooks`, `TestListenAndServe_ReturnsAfterConnections` | n/a: no Shutdown | n/a: no Shutdown | n/a: no Shutdown | n/a: no Shutdown |
| L16 Close racing reconnect | n/a: no reconnect | n/a: no reconnect | n/a: no reconnect | n/a: no reconnect | n/a: no reconnect | n/a: no reconnect |

Goroutines left by any of these tests fail the package: root, goose, sv,
l2 and rsession end their `TestMain` with `internal/leakcheck`.

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

`make fuzz` runs every target for `FUZZTIME` (default 15s) each, discovered
with `go test -list '^Fuzz'` as `fuzz.yml` and the CI fuzz smoke do, so a new
target needs no Makefile change. One target:

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
