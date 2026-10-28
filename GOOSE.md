# GOOSE

Package [`goose`](goose/) implements IEC 61850 GOOSE (Generic Object
Oriented Substation Event, IEC 61850-8-1 clause 18 and Annex A/C;
services of IEC 61850-7-2 clause 15) in pure Go, without cgo. It covers:

- the frame codec (Ethernet, 802.1Q VLAN, EtherType `0x88B8`, APDU header,
  BER `goosePdu`, `allData` as [go-mms](https://github.com/otfabric/go-mms)
  values),
- a publisher with the IEC 61850-8-1 retransmission scheme,
- a subscriber with filtering, validation against the configured
  dataset, and per-stream supervision (LGOS-style),
- raw Ethernet transports for Linux (`AF_PACKET`) and macOS/BSD (BPF), and
  an in-memory hub for tests,
- binding from SCL (`GSEControl`, `GSE` address, dataset), and
- server integration: an IEC 61850 server publishes its GoCBs on data
  change and exposes GoCB attributes over MMS; the client reads and writes
  them.

The implementation is clean-room from the IEC 61850 standards; it contains
no code derived from other GOOSE implementations.

## Contents

- [Frames](#frames)
- [Publisher](#publisher)
- [Subscriber](#subscriber)
- [Transports](#transports)
- [SCL binding](#scl-binding)
- [Server GoCBs](#server-gocbs)
- [Client GoCB access](#client-gocb-access)
- [Errors](#errors)
- [Logging and metrics](#logging-and-metrics)
- [Testing](#testing)
- [Deployment](#deployment)
- [Limitations](#limitations)

## Frames

```go
f := &goose.Frame{
    Dst:   goose.MAC{0x01, 0x0c, 0xcd, 0x01, 0x00, 0x01},
    Src:   tr.HardwareAddr(),
    VLAN:  &goose.VLAN{Priority: 4, ID: 0},
    AppID: 0x1001,
    PDU: goose.PDU{
        GocbRef: "IEDLD/LLN0$GO$gcb1", DatSet: "IEDLD/LLN0$ds1", GoID: "gcb1",
        TimeAllowedToLive: 2000, T: time.Now(), StNum: 1, ConfRev: 1,
        AllData: []*mms.Value{mms.NewBoolean(true)},
    },
}
b, err := goose.Encode(f)        // or AppendFrame(dst, f)
g, err := goose.Decode(b)        // or DecodeWithLimits(b, goose.Limits{...})
```

| Field | Wire |
|---|---|
| `Dst`, `Src` | Ethernet addresses (`goose.MAC`, `ParseMAC` accepts `01:0c:cd:…`, `01-0C-CD-…`, `010ccd…`) |
| `VLAN` | 802.1Q tag (priority, DEI, VID); nil for untagged |
| `AppID` | APPID |
| `Simulation` | the Edition 2 simulation bit (MSB of Reserved1) |
| `Reserved1`, `Reserved2` | other reserved bits, kept as received |
| `PDU` | `goosePdu` (tag `0x61`): gocbRef, timeAllowedToLive (ms), datSet, goID (optional), t (UtcTime with time quality), stNum, sqNum, test (field `[7]`, `simulation` in the Edition 2 ASN.1), confRev, ndsCom, numDatSetEntries, allData, security (optional, opaque) |

The encoder pads frames to the Ethernet minimum (60 octets without FCS)
and writes `numDatSetEntries = len(AllData)`. Floats are sent as
FloatingPoint with an 8-bit exponent when the value is exact in float32,
otherwise with an 11-bit exponent.

The decoder is strict: field order and mandatory fields per Annex A,
definite BER lengths only, length fields checked against the frame, no
trailing data inside the APDU (Ethernet padding after the APDU is
accepted), and the MMS Data types usable in GOOSE (boolean, bit-string,
integer, unsigned, floating-point, octet-string, visible-string,
generalized-time, binary-time, BCD, boolean-array, object-identifier,
MMS string, UtcTime, arrays and structures). `Limits` bounds the nesting
depth and the total number of data elements (`DefaultLimits`: 16 and
4096); allocations are proportional to the frame size. Management APDUs
(tag `0x60`) return `ErrUnsupportedPDU`.

`EncodeData`/`DecodeData` encode a single MMS Data value in the GOOSE
form; `EncodePDU`/`DecodePDU` handle the APDU alone.

## Publisher

```go
tr, err := goose.Open("eth0", goose.TransportOptions{})
pub, err := goose.NewPublisher(tr, goose.PublisherConfig{
    Dst: dst, VLAN: &goose.VLAN{Priority: 4}, AppID: 0x1001,
    GocbRef: "IEDLD/LLN0$GO$gcb1", DatSet: "IEDLD/LLN0$ds1", GoID: "gcb1", ConfRev: 1,
    MinTime: 10 * time.Millisecond, MaxTime: time.Second,
    Values: initial,
})
go pub.Run(ctx)             // sends Values as stNum 1, then retransmits
err = pub.Publish(values)   // state change: stNum+1, sqNum 0, schedule restarts
```

- **State and sequence numbers.** Each `Publish` is a new state: stNum
  increments (wrapping from 2^32-1 to 1), sqNum restarts at 0 and counts
  retransmissions (wrapping to 1). `InitialStNum` sets the first stNum.
- **Retransmission.** After a change the frame is repeated after
  `MinTime`, then at doubling intervals up to `MaxTime`, which then
  repeats as the heartbeat (defaults 10 ms and 1 s).
- **timeAllowedToLive** is `TimeAllowedToLive` (default `2 × MaxTime`) in
  every frame.
- **Shape check.** `Publish` rejects values whose number or structure
  differs from the configured dataset (`ErrInvalidValue`); the dataset can
  only change with a new publisher (and confRev).
- **Flags.** `Test`, `Simulation` and `NdsCom` are set in the config or at
  runtime with `SetFlags`. `SetEnabled(false)` stops sending (GoEna).
- **Concurrency and shutdown.** All methods are safe for concurrent use.
  `Publish` sends synchronously and returns the transport error; `Run`
  returns when its context ends; `Close` stops it. `State()` reports
  stNum, sqNum, t, the current interval and the frames sent.

## Subscriber

```go
sub, err := goose.NewSubscriber(tr, goose.SubscriberConfig{})
s, err := sub.Subscribe(goose.SubscriptionConfig{
    Dst: dst, AppIDs: []uint16{0x1001}, GocbRef: "IEDLD/LLN0$GO$gcb1",
    DatSet: "IEDLD/LLN0$ds1", ConfRev: 1, Members: memberTypes,
})
go sub.Run(ctx)
for ev := range s.Events() {
    switch ev.Kind {
    case goose.EventMessage:   // ev.Frame, ev.NewState
    case goose.EventTimeout:   // timeAllowedToLive expired
    case goose.EventInvalid:   // ev.Err is a *goose.ValidationError
    }
}
```

A subscription matches frames on any combination of destination MAC,
APPIDs, gocbRef and source MAC. It validates, when configured, datSet,
goID, confRev, the member count and each member's type
(`MatchesType`); a mismatch emits `EventInvalid` with a
`*ValidationError` that wraps `ErrValidation` and one of
`ErrConfRevMismatch`, `ErrDatSetMismatch`, `ErrGoIDMismatch`,
`ErrMemberMismatch`, `ErrNeedsCommissioning` (frame with ndsCom set).
Frames with the simulation bit are dropped unless `AcceptSimulation`.

Supervision is per stream (source MAC, APPID, gocbRef):

| Event | Condition |
|---|---|
| `EventMessage` | a valid frame (`NewState` when stNum advanced) |
| `EventTimeout` | no frame within the last timeAllowedToLive |
| `EventStNumGap` | stNum skipped states |
| `EventSqNumGap` | sqNum skipped retransmissions |
| `EventDuplicate` | same stNum and sqNum again |
| `EventOutOfOrder` | an older stNum/sqNum |
| `EventRestart` | stNum restarted at 1 (publisher restart), told apart from reordering by the frame time |
| `EventInvalid` | validation failed |
| `EventSupervision` | the stream status changed |

Sequence comparisons use serial-number arithmetic, so rollovers are not
gaps. `Subscription.Supervision()` returns a `Supervision` snapshot per
stream, modelled on LGOS: `Status` (`waiting`, `ok`, `timeout`,
`invalid`, `ndscom`), `St`, `SimSt`, `NdsCom`, `LastStNum`,
`LastSqNum`, `ConfRevNum`, `RxConfRevNum`, the last error and counters
(messages, state changes, duplicates, out-of-order, gaps, restarts,
timeouts, invalid frames, dropped events).

Events are delivered on a channel (`QueueSize`, default 256; when full,
events are dropped and counted) or to a `Handler` callback;
`OnlyStateChanges` suppresses retransmissions. `HandleFrame` feeds raw
frames from an external source. Undecodable frames go to
`SubscriberConfig.OnDecodeError`.

## Transports

```go
type Transport interface {
    Send(frame []byte) error
    Recv(ctx context.Context) ([]byte, error)
    HardwareAddr() goose.MAC
    Close() error
}
```

| Platform | `goose.Open(iface, opts)` |
|---|---|
| Linux | `AF_PACKET` raw socket with a kernel BPF filter for EtherType `0x88B8` (tagged or untagged); the VLAN tag stripped by the kernel is restored from `PACKET_AUXDATA`. Needs `CAP_NET_RAW`. |
| macOS, FreeBSD, NetBSD, OpenBSD, DragonFly | `/dev/bpf*` with the same filter. Needs read/write access to `/dev/bpf*` (on macOS the `access_bpf` group). |
| other | `ErrUnsupportedPlatform` |

`TransportOptions`: `Multicast` joins the given multicast addresses (when
empty the interface is put in promiscuous mode, or with `Promiscuous`),
`ReceiveLocal` also returns frames this host sent, `Logger`. Errors are
`*TransportError` values wrapping `ErrPermission`,
`ErrInterfaceNotFound` or the OS error.

`goose.NewHub()` is an in-memory segment: `hub.NewPort(mac)` returns a
`Transport`, and `hub.SetFaults(goose.Faults{Loss, Duplicate, Reorder,
Delay, Seed, Mutate})` injects faults for chaos tests.

## SCL binding

```go
doc, _ := scl.ParseFile("station.scd")
cbs, err := goose.ControlBlocksFromSCL(doc)       // every GoCB of every IED
cb, err := goose.ControlBlockFromSCL(doc, "IED1", "LD0", "gcb1")
pc, err := cb.PublisherConfig()                    // address, times, zero values
sc := cb.SubscriptionConfig()                      // filter + validation
```

A `ControlBlock` combines the `GSEControl` (name, `appID` as goID,
`datSet`, `confRev`, `fixedOffs`), the `GSE` address of the IED's
ConnectedAP (`MAC-Address`, `APPID`, `VLAN-ID`, `VLAN-PRIORITY`,
`MinTime`, `MaxTime`) and the dataset members with their MMS types
(data-object members expand to the attributes of the FC, recursively).
References are the on-wire form: the MMS domain is the IED name followed
by the LD instance (`IED1LD0/LLN0$GO$gcb1`). Blocks that cannot be
resolved are reported in the returned error (wrapping `ErrSCL`); the rest
are returned.

## Server GoCBs

`NewServerModelFromSCL` reads the `GSEControl` blocks of each LLN0 and
their GSE addresses. `Server.EnableGOOSE` starts publishing them:

```go
tr, _ := goose.Open("eth0", goose.TransportOptions{})
engine, err := srv.EnableGOOSE(iec61850.GOOSEOptions{Transport: tr})
srv.SetValue(ctx, "LD0/GGIO1$ST$Ind1$stVal", mms.NewBoolean(true)) // -> new GOOSE state
```

- Every GoCB with a GSE address starts enabled (`StartDisabled` to start
  with GoEna false). A change of any dataset member, through `SetValue`
  or an MMS write, publishes a new state.
- The GoCB is an MMS variable `LLN0$GO$<name>` with GoEna, GoID, DatSet,
  ConfRev, NdsCom, DstAddress {Addr, PRIORITY, VID, APPID}, MinTime,
  MaxTime and FixedOffs (IEC 61850-8-1 GoCB). GoEna, GoID and DatSet are
  writable; GoID and DatSet only while GoEna is false
  (temporarily-unavailable otherwise); a new DatSet must exist
  (object-value-invalid) and increments ConfRev. Other attributes are
  read-only (object-access-denied). A GoCB without a GSE address has
  NdsCom true and cannot be enabled.
- `GOOSEEngine.ControlBlocks()`, `Publisher(ref)`, `SetEnabled(ref, on)`
  and `Stop()`; `Server.Close` stops the engine.
- The server's MMS domain is the bare LD instance, and so is the gocbRef
  it publishes (`LD0/LLN0$GO$gcb1`).

## Client GoCB access

```go
cb, err := client.GetGoCB(ctx, "LD0", "LLN0$GO$gcb1")
off, on, ds := false, true, "LD0/LLN0$ds2"
err = client.SetGoCB(ctx, "LD0", "LLN0$GO$gcb1", iec61850.GoCBUpdate{GoEna: &off})
err = client.SetGoCB(ctx, "LD0", "LLN0$GO$gcb1", iec61850.GoCBUpdate{DatSet: &ds, GoEna: &on})
```

`GetGoCB` decodes the GoCB structure into `GoCB` (MinTime/MaxTime and
FixedOffs are optional, `HasTimes` tells). `SetGoCB` writes the selected
attributes, GoEna last.

## Errors

All errors wrap a sentinel from [goose/errors.go](goose/errors.go):

| Sentinel | Meaning |
|---|---|
| `ErrInvalidFrame`, `ErrNotGOOSE`, `ErrInvalidLength`, `ErrInvalidPDU`, `ErrInvalidData`, `ErrUnsupportedPDU`, `ErrLimitExceeded` | decoding (as `*DecodeError` with field and offset) |
| `ErrInvalidConfig` | publisher/subscription configuration (`*ConfigError`) |
| `ErrInvalidValue` | value cannot be encoded or does not match the dataset (`*EncodeError`) |
| `ErrValidation` + `ErrConfRevMismatch`, `ErrDatSetMismatch`, `ErrGoIDMismatch`, `ErrMemberMismatch`, `ErrNeedsCommissioning` | subscriber validation (`*ValidationError`) |
| `ErrClosed` | publisher, subscriber or transport closed |
| `ErrUnsupportedPlatform`, `ErrPermission`, `ErrInterfaceNotFound` | transport (`*TransportError`) |
| `ErrSCL`, `ErrNotFound` | SCL binding |

The server side wraps `iec61850.ErrGOOSE`; the client GoCB API uses the
library's `ErrInvalidArgument` and `ErrProtocol`.

## Logging and metrics

Publisher and subscriber configs take a `*slog.Logger` (silent when nil)
and a `goose.Metrics`:

```go
type Metrics interface {
    FrameSent(gocbRef string, retransmission bool)
    SendError(gocbRef string, err error)
    FrameReceived(gocbRef string)
    FrameDropped(reason string) // "decode", "simulation", "filtered"
    Event(gocbRef string, kind goose.EventKind, dropped bool)
}
```

Embed `goose.NopMetrics` to implement only some methods. See
[OBSERVABILITY.md](OBSERVABILITY.md).

## Testing

- Unit tests use the hub; chaos tests run the subscriber against loss,
  duplication, reordering and corruption.
- Raw transport tests run on `lo` on Linux as root (CI does) and on
  `GOOSE_TEST_IFACE` elsewhere, e.g. a macOS host-only bridge member
  interface.
- Fuzz targets: `FuzzDecode`, `FuzzDecodeData`.
- Interop: `interop/goose_test.go` against libiec61850, dscsystems and iec61850-rust
  publishers and subscribers on a Docker bridge (see
  [INTEROP.md](INTEROP.md)).

## Deployment

- **Privileges** — grant the binary the capability instead of running it
  as root: `sudo setcap cap_net_raw+ep ./myapp` (Linux). On macOS add the
  user to the `access_bpf` group (installed by Wireshark's ChmodBPF) or
  adjust `/dev/bpf*` permissions. `Open` returns an error wrapping
  `ErrPermission` when access is missing.
- **Multicast reception** — set `TransportOptions.Multicast` to the
  control blocks' destination addresses (`cb.Dst` from SCL) so the NIC
  filters in hardware; without it the transport receives all multicast
  (Linux) or enables promiscuous mode (macOS/BSD).
- **Network** — GOOSE and SV are layer-2 multicast: switches flood them
  unless static multicast filtering or VLANs limit them. Keep the VLAN
  ID and priority from SCL (default priority 4) and check that every
  switch on the path forwards 802.1Q-tagged frames. See
  [SECURITY.md](SECURITY.md#deployment-guidance) for isolation.
- **Supervision** — the subscriber times each stream out after the
  `timeAllowedToLive` carried in its last message (the Go publisher sends
  `2 × MaxTime` unless `PublisherConfig.TimeAllowedToLive` is set) and
  reports `EventTimeout`. It does not substitute values: the application
  decides what a lost input means (e.g. treat it as invalid, as LGOS
  `St` would show).

## Limitations

- No GOOSE security (IEC 62351-6): the `security` field is carried
  opaquely, not generated or verified.
- No fixed-offset encoding (`FixedOffs` is reported, not applied).
- No GSSE, no GOOSE management APDUs. Sampled Values are in package
  [`sv`](SV.md); both share the raw Ethernet layer of package `l2`.
- No Windows transport.
- The subscriber does not map received values into a server model
  (no LGOS/`InRef` binding); applications handle `EventMessage`.
