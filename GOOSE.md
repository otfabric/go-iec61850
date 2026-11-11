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
- [R-GOOSE and R-SV over UDP](#r-goose-and-r-sv-over-udp)
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
| `Simulation` | the simulation bit (MSB of Reserved1, 8-1:2011 Figure C.5) |
| `Reserved1`, `Reserved2` | other reserved bits, kept as received |
| `PDU` | `goosePdu` (tag `0x61`): gocbRef, timeAllowedToLive (ms), datSet, goID (optional), t (UtcTime with time quality), stNum, sqNum, test (field `[7]`, `simulation` in the Edition 2 ASN.1), confRev, ndsCom, numDatSetEntries, allData, security (optional, opaque) |

The encoder pads frames to the Ethernet minimum (60 octets without FCS)
and writes `numDatSetEntries = len(AllData)`. Floats are sent as
FloatingPoint with an 8-bit exponent when the value is exact in float32,
otherwise with an 11-bit exponent. The Ethernet payload is limited to
`MaxPayload` = 1499 octets (Length = 8 + m, m < 1492, 8-1 Annex C).
`EncodePDU`/`Encode` do not check the 8-1 18.1.2.5.2 value ranges (so
negative test frames can be built); `NewPublisher` does. `VLAN.DEI` must
be 0 on the wire (8-1 Annex C: the CFI bit "shall be reset") and is
settable only for negative tests.

**Fixed-length encoding** (8-1 Annex A.3, SCL `fixedOffs="true"`):
`EncodeFixedLength`/`EncodePDUFixedLength` (and
`PublisherConfig.FixedOffs` with `MemberTypes`) keep every field at a
fixed offset: header INT32U fields (timeAllowedToLive, stNum, sqNum,
confRev, numDatSetEntries) with length 5 (Table A.1), INT8/16/32/64 with
2/3/5/9 octets, INT8U/16U/32U with 2/3/5 (INT24U as INT32U), OCTET STRING
20 octets zero padded, VISIBLE STRING 35 octets NUL padded (Table A.2).
Integer widths come from the member `TypeSpec.Size`; without a type an
integer is sent as 32 bits (64 when the value needs it). Deviations from
the letter of Table A.2: FLOAT32 keeps the 5-octet MMS FloatingPoint form
(exponent-width octet plus IEEE 754 single) instead of 4 octets;
MMSString, GeneralizedTime, BCD and object identifiers keep their BER
encoding; a longer OCTET/VISIBLE STRING fails with `ErrInvalidValue`.

The decoder is strict: field order and mandatory fields per Annex A,
definite BER lengths only, length fields checked against the frame, no
trailing data inside the APDU (Ethernet padding after the APDU is
accepted), and the MMS Data types usable in GOOSE (boolean, bit-string,
integer, unsigned, floating-point, octet-string, visible-string,
generalized-time, binary-time, BCD, boolean-array, object-identifier,
MMS string, UtcTime, arrays and structures). `Limits` bounds the nesting
depth and the total number of data elements (`DefaultLimits`: 16 and
4096); allocations are proportional to the frame size. Management APDUs
(tag `0x60`) return `ErrUnsupportedPDU`. Fixed-length frames (8-1 Annex
A.3) decode: a 9-octet integer whose first octet only sign-extends,
trailing NUL padding of VISIBLE STRINGs (header and `allData`) and the
4-octet FLOAT32 of Table A.2 are accepted; a NUL followed by other
characters is still rejected.

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
  retransmissions (wrapping to 1). `InitialStNum` sets the first stNum;
  `SetEnabled(true)` after a disable restarts at stNum 1 (7-2 18.2.3.6).
- **Configuration ranges** (`ErrInvalidConfig`): `Dst` must have the
  multicast bit (8-1 Table 12); GocbRef and DatSet at most 129 octets, an
  explicit GoID at most 65 (8-1 18.1.2.5.2; the default GoID, the
  GocbRef, is not checked); `MaxTime` below 60 s and `TimeAllowedToLive`
  at least 1 ms and greater than `MaxTime` (8-1 18.1.2.5.1 Figure 10).
- **TimeQuality** of `T` is `PublisherConfig.TimeQuality`; zero uses
  `DefaultTimeQuality` 0x2a (ClockNotSynchronized, 10 bits accuracy),
  since the publisher cannot know that the host clock is synchronised.
  Set e.g. 0x0a when it is. `PublisherConfig.Clock`, when set, stamps
  each new state with its time and TimeQuality instead; `Now` still
  paces the retransmissions, so a step of the stamping clock does not
  move them.
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
| `EventRestart` | publisher restart: stNum went back to 1, or stNum/sqNum did not advance (or stNum went down) while `T` is newer than that of the last accepted state (7-2 18.2.3.5: T only changes with a new state); a frame with an older `T` is reordering |
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

## R-GOOSE and R-SV over UDP

Package `rsession` carries GOOSE and SV over UDP/IP (unicast or
multicast, port 102 by default) with the session protocol of IEC TR
61850-90-5 and an HMAC-SHA256 signature on every SPDU. Its `Transport`
implements the `Transport` interface above, so the publisher, subscriber,
retransmission and supervision of this package (and of `sv`) run over it
unchanged:

```go
keys, _ := rsession.NewKeyRing(rsession.Key{ID: 1, Material: secret, MAC: rsession.MACHMACSHA256_128})

// Publisher side: send to a group (or a unicast address).
tx, _ := rsession.Open(rsession.Config{
    SI:           rsession.SIGOOSE,
    RemoteAddr:   netip.MustParseAddrPort("239.192.0.1:102"),
    MulticastTTL: 16,
    Keys:         keys,
})
pub, _ := goose.NewPublisher(tx, cfg)

// Subscriber side: join the group.
rx, _ := rsession.Open(rsession.Config{
    SI:     rsession.SIGOOSE,
    Groups: []netip.Addr{netip.MustParseAddr("239.192.0.1")},
    Keys:   keys,
})
sub, _ := goose.NewSubscriber(rx, goose.SubscriberConfig{})
```

- `Send` takes the layer-2 frame the publisher builds and sends its
  APPID, simulation bit and APDU; the Ethernet addresses and VLAN tag are
  not carried. `Recv` rebuilds a layer-2 frame for every APDU of a
  verified SPDU, with destination `Config.FrameDst` (broadcast by
  default) and source `rsession.SourceMAC(sender IP)`, so subscriptions
  can filter on the sender.
- **Secured by default.** `Open` refuses a configuration without a
  `KeyProvider` unless `Insecure` is set. A receiver refuses unsecured
  SPDUs (unless `Insecure`), SPDUs under unknown keys, SPDUs whose
  signature does not verify, and replays: per sender and key, an SPDU
  number already accepted or more than 64 below the highest. A restarted
  publisher is refused until the key changes, unless the receiver sets
  `ReplayResetAfter` or the publisher continues from a persisted
  `NextSPDUNumber` (`InitialSPDUNumber`). Refusals are counted in
  `Transport.Stats` and reported to `Config.OnReject`.
- **Keys** come from a `KeyProvider` (`CurrentKey` to send, `LookupKey` to
  verify); `KeyRing` is a static one that can hold the old and the new
  key during a change. Key distribution (IEC 62351-9 GDOI) is the
  application's.
- Protocol versions 1 and 2 are received; 2 is sent unless
  `Config.Version` is 1. HMAC-SHA256-80, -128 and -256 are implemented;
  AES-GMAC and encryption are refused as `ErrUnsupported`.
- `rsession.Encode`, `Decode` and `Verify` work on single SPDUs (captures,
  tests).
- Multicast group membership, the outgoing interface and the TTL use
  socket options on Linux, macOS and the BSDs; elsewhere only unicast
  works. Binding port 102 needs privileges on most systems.

The wire layout, the references it follows where no specification text
was available, and the scope are in
[docs/adr/0001-routable-goose-sv.md](docs/adr/0001-routable-goose-sv.md).
An independent implementation is tested against it:
`interop/rsession_test.go` exchanges R-GOOSE and R-SV with the
dscsystems adapter of iec61850-interop (dscsystems/go-iec61850
`rsession`, contract 1.4, `--transport udp`) in both directions, for
version 1 with HMAC-SHA256-80 and version 2 with HMAC-SHA256-128 and
-256, and checks that the peer refuses forged, unsecured and replayed
SPDUs. It passes against an adapter image built from
[iec61850-interop#24](https://github.com/otfabric/iec61850-interop/pull/24);
it is not interop evidence until a released image is pinned in CI (until
then the tests skip). See [interop/README.md](interop/README.md#r-goose-and-r-sv).

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
References are the on-wire form: the MMS domain is LDevice@ldName, or
the IED name followed by the LD instance (`IED1LD0/LLN0$GO$gcb1`). Blocks that cannot be
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
  or an MMS write, publishes a new state. A GoCB that cannot be enabled
  at start-up is logged and left with NdsCom true; the others publish.
- Address defaults: a GSE address without `APPID` uses 0x0000 (8-1 Annex
  C) and `VLAN-ID` without `VLAN-PRIORITY` uses priority 4 (Table C.1),
  each with a model warning. Without any VLAN parameter frames are sent
  untagged (VLAN use is optional, 8-1 Annex C; receivers must accept
  untagged frames), unlike libiec61850, which always tags.
- NdsCom is true, and GoEna TRUE fails with temporarily-unavailable, for
  a GoCB without a usable GSE address (none, `00-00-00-00-00-00` = not
  configured per 8-1 Annex B, or not multicast), with ConfRev 0 (7-2
  18.2.1.3) or with a dataset that cannot be resolved.
- `FixedOffs` true in SCL selects the fixed-length encoding (see
  [Frames](#frames)); `T` and its TimeQuality come from
  `ServerOptions.Clock` (see [API.md](API.md#clock)); a non-zero
  `GOOSEOptions.TimeQuality` overrides the clock's quality.
- The GoCB is an MMS variable `LLN0$GO$<name>` with GoEna, GoID, DatSet,
  ConfRev, NdsCom, DstAddress {Addr, PRIORITY, VID, APPID}, MinTime,
  MaxTime and FixedOffs (IEC 61850-8-1 GoCB). GoEna, GoID and DatSet are
  writable; GoID and DatSet only while GoEna is false
  (temporarily-unavailable otherwise); a DatSet must exist
  (object-value-invalid), is stored as the ObjectReference
  (`LD/LLN0$ds`, 8-1 Table 73) also when written by its bare name, and
  every DatSet write increments ConfRev, also to the same value (7-2
  18.2.1.6); once ConfRev would pass
  2^32-1 the GoCB cannot be enabled (object-value-invalid). Other attributes are
  read-only (object-access-denied). GoID and DatSet are writable as in
  7-2 Table 42 although 8-1 Table 73 maps them read-only (PIXIT choice).
- `GOOSEEngine.ControlBlocks()`, `Publisher(ref)`, `SetEnabled(ref, on)`
  and `Stop()`; `Server.Close` stops the engine.
- The server's MMS domain is the logical device's IEC 61850-8-1 name, and
  so is the gocbRef it publishes (`IED1LD0/LLN0$GO$gcb1`): LDevice@ldName
  when the SCL has one, else the IED name followed by the instance. Before
  v1.7.0 it was the bare instance (`LD0/LLN0$GO$gcb1`), which
  `ServerModelOptions.InstanceDomainNames` keeps.

## Client GoCB access

```go
cb, err := client.GetGoCB(ctx, "LD0", "LLN0$GO$gcb1")
off, on, ds := false, true, "LD0/LLN0$ds2"
err = client.SetGoCB(ctx, "LD0", "LLN0$GO$gcb1", iec61850.GoCBUpdate{GoEna: &off})
err = client.SetGoCB(ctx, "LD0", "LLN0$GO$gcb1", iec61850.GoCBUpdate{DatSet: &ds, GoEna: &on})
```

`GetGoCB` decodes the GoCB structure into `GoCB` (MinTime/MaxTime and
FixedOffs are optional, `HasTimes` tells); an attribute outside its IEC
61850-7-2 type (ConfRev, MinTime, MaxTime INT32U; APPID INT16U) or the
8-1 Table 12 range (PRIORITY 0..7, VID 0..4095) is an `ErrProtocol`, not
truncated. `SetGoCB` writes the
selected attributes, GoEna last.

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
- Fuzz targets: `FuzzDecode`, `FuzzDecodeData`, `FuzzDecodePDU`,
  `FuzzEncodeFixedLength` (seeded with Annex A.3 fixed-length vectors).
- Interop: `interop/goose_test.go` against libiec61850, dscsystems, iec61850-rust and cbsigma
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

- No layer-2 GOOSE security (IEC 62351-6 extension in Reserved 1/2): the
  `security` field and the reserved fields are carried opaquely, not
  generated or verified. Authenticated GOOSE is available over UDP
  ([R-GOOSE](#r-goose-and-r-sv-over-udp)); R-GOOSE interop with
  dscsystems awaits a pinned iec61850-interop image. Layer-2 IEC 62351-6
  is **open, to do**: it needs the IEC 62351-6 specification
  (preferably the 2020 edition) before it can be implemented.
- Fixed-length encoding deviates from Table A.2 for FLOAT32 (5 octets)
  and limits OCTET/VISIBLE STRING members to 20/35 octets; it is not
  verified against an independent fixed-length implementation.
- No GSSE, no GOOSE management APDUs. Sampled Values are in package
  [`sv`](SV.md); both share the raw Ethernet layer of package `l2`.
- No Windows transport.
- The subscriber does not map received values into a server model
  (no LGOS/`InRef` binding); applications handle `EventMessage`.
