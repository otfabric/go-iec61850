# Architecture

How the packages depend on each other, which goroutines run, who owns what,
and what each platform provides. The [README](README.md#architecture) has
the protocol overview; [API.md](API.md) has the types.

## Layers

Module imports, from `go list`. An arrow means "imports".

```
iec61850 (root) ──► goose ──┐
    │ │ │                   ├──► l2 ──► golang.org/x/sys/unix
    │ │ └───────────► sv ───┘
    │ │
    │ └──► internal/mapping   (no module imports)
    │
    ├──► internal/servermodel ──► scl, scl/index, go-mms
    ├──► scl ──► scl/internal/raw/{v17,v2007b,v2007b4,v2007c5} (generated)
    ├──► go-mms, go-mms/transport/iso ──► go-cotp ──► go-tpkt
    └──► golang.org/x/sync/singleflight

goose, sv ──► internal/servermodel, scl, go-mms
scl/validate ──► scl/index ──► scl
```

- The root package does not import `l2`: its GOOSE and SV engines use a
  transport from `goose` or `sv`.
- `goose` and `sv` do not import each other. Both reuse go-mms value types.
- `scl` has no dependency on MMS.
- `scl/cmd/sclparse` and `scl/cmd/sclgen` are nested modules: `./...` of this
  module does not reach them, and the Makefile and CI handle them separately.
- `scl/internal/raw` is generated from `scl/specs` by `sclgen`
  (`make scl-generate`); `make scl-check-generate` compares it in CI.

## Client

`Client` wraps an `mms.Client` and translates IEC 61850 references to MMS
domains and item IDs. `Dial` creates the MMS client and `Client.Close` closes
it. `NewClient` wraps a caller's MMS client; the caller keeps ownership.

go-mms runs the receive loop and correlates invoke IDs, so the client adds
few goroutines of its own:

| Goroutine | Started by | Stops when |
|---|---|---|
| Report subscription watcher | `Client.SubscribeReport` (`report.go`) | the subscription's context ends |

Model browsing is optionally cached in memory (`CacheStrategy`); concurrent
fetches share one request through `singleflight`.

## Server

`Server` owns one `mms.Server`, one `servermodel.ValueStore` and the engines
enabled on it. `SetValue` is the one write path: it updates the store and
notifies the report, journal, GOOSE and SV engines.

| Goroutine | Started by | Stops when |
|---|---|---|
| Accept loop | `ListenAndServe`, in the caller's goroutine | context ends, or `Shutdown` closes the listener |
| Connection | accept loop, when connection hooks are set; otherwise go-mms | the connection closes |
| Integrity timer, one per enabled RCB | enabling the RCB | RCB disabled or engine stopped |
| BRCB replay | enabling a BRCB with buffered entries | replay ends |
| Report send, one per report and connection | report engine | sent or failed |
| GOOSE publisher, one per enabled GoCB | `GoEna` | GoCB disabled or engine stopped |
| SV publisher, one per enabled MSVCB | `SvEna` | MSVCB disabled or engine stopped |
| CommandTermination send | operate under enhanced security | sent or failed; not joined by `Close` |

Report sends wait 100 ms so that the confirmed response to the write that
caused the report goes first (`report_engine.go` explains why: a client such
as libiec61850 can otherwise discard it). `commandTerminationDelay` does the
same for CommandTermination.

Lifecycle:

- `Close` stops the report, GOOSE and SV engines. It does not close
  listeners or connections.
- `Shutdown(ctx)` closes the listeners, lets in-flight requests finish,
  concludes every association and then calls `Close`. If `ctx` ends first the
  remaining connections are aborted. `ListenAndServe` then returns
  `mms.ErrServerClosed`.

The model is immutable after construction.

## GOOSE and Sampled Values

Publishers and subscribers are driven by `Run(ctx)` in the caller's goroutine
(on the server, in the goroutines above).

| Goroutine | Started by | Stops when |
|---|---|---|
| Subscription handler (`goose`) | a subscription with a `Handler` | the subscription ends |
| Supervision (`sv`) | `sv.Subscriber.Run` | `Run` returns |
| Demultiplexer (`l2`) | `l2.NewDemux` | `Demux.Close`; the Demux owns its transport and closes it |

`l2.Demux` lets GOOSE and SV share one interface by EtherType.

## Platform backends

| Platform | `l2` transport | File |
|---|---|---|
| Linux | `AF_PACKET` | `l2/transport_linux.go` |
| macOS and the BSDs | BPF | `l2/transport_bpf.go` |
| others | `ErrUnsupportedPlatform` | `l2/transport_other.go` |
| any | in-memory `Hub`, with fault injection | `l2/hub.go` |

Privileges and what CI runs per platform:
[COMPATIBILITY.md](COMPATIBILITY.md#supported-platforms). MMS is TCP through
go-mms on every platform.
