# Sampled Values

Package [`sv`](sv/) implements IEC 61850 Sampled Values (IEC 61850-9-2,
with the IEC 61869-9 and UCA 9-2LE profiles) in pure Go, without cgo. It
covers:

- the frame codec (Ethernet, 802.1Q VLAN, EtherType `0x88BA`, APDU header
  with the simulation bit, BER `savPdu` with `noASDU`, optional
  `security` and the ASDU sequence),
- dataset encoding: a generic `Layout` of fixed-size basic types and
  9-2LE helpers (4 currents and 4 voltages, INT32 with scaling, plus
  Quality),
- a publisher that paces samples at `smpRate` on an absolute schedule,
  batches `noASDU` samples per frame and wraps `smpCnt`,
- a subscriber with filtering, validation, sequence accounting (loss,
  duplicates, reordering), rate estimation, synchronisation tracking and
  timeout supervision, without allocations per frame,
- the raw Ethernet transports shared with GOOSE (package [`l2`](l2/)),
- binding from SCL (`SampledValueControl`, `SMV` address, dataset), and
- server integration: an IEC 61850 server publishes its multicast SV
  control blocks (MSVCB) and exposes their attributes over MMS; the
  client reads and writes them.

The implementation is clean-room from IEC 61850-9-2, IEC 61869-9 and the
9-2LE guideline; it contains no code derived from other Sampled Values
implementations.

## Contents

- [Frames](#frames)
- [Datasets](#datasets)
- [Publisher](#publisher)
- [Subscriber](#subscriber)
- [Transports](#transports)
- [SCL binding](#scl-binding)
- [Server MSVCBs](#server-msvcbs)
- [Client MSVCB access](#client-msvcb-access)
- [Errors](#errors)
- [Metrics](#metrics)
- [Performance](#performance)
- [Testing](#testing)
- [Deployment](#deployment)
- [Limitations](#limitations)

## Frames

```go
f := &sv.Frame{
    Dst:   sv.MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x01},
    Src:   tr.HardwareAddr(),
    VLAN:  &sv.VLAN{Priority: 4},
    AppID: 0x4000,
    PDU: sv.PDU{ASDUs: []sv.ASDU{{
        SvID: "MU01", SmpCnt: 17, ConfRev: 1, SmpSynch: sv.SmpSynchGlobal,
        Sample: le.Append(nil),
    }}},
}
b, err := sv.Encode(f)            // or AppendFrame(dst, f): no allocation with capacity
g, err := sv.Decode(b)            // copies Sample and Security
err = sv.DecodeInto(&g2, b, sv.DefaultLimits) // reuses g2, aliases b
```

| Field | Wire |
|---|---|
| `Dst`, `Src` | Ethernet addresses |
| `VLAN` | 802.1Q tag (priority, DEI, VID); nil for untagged |
| `AppID` | APPID (9-2 reserves `0x4000`–`0x7FFF`) |
| `Simulation` | the simulation bit (MSB of Reserved1) |
| `Reserved1`, `Reserved2` | other reserved bits, kept as received |
| `PDU.NoASDU` | `noASDU` (zero on encode means `len(ASDUs)`) |
| `PDU.Security` | optional `security` field, carried opaquely |
| `ASDU` | `svID`, `datSet` (optional), `smpCnt`, `confRev`, `refrTm` (optional UtcTime), `smpSynch`, `smpRate` (optional), `sample`, `smpMod` (optional), `gmIdentity` (optional, IEC 61850-9-2 Ed2.1) |

Optional ASDU fields are sent when their `Has*` flag is set (`datSet`
when non-empty). `smpSynch` is 0 (none), 1 (local), 2 (global) or 5–254
(the PTP grandmaster's `smpSynch` id); `SmpSynchNone`, `SmpSynchLocal`
and `SmpSynchGlobal` name the first three.

The decoder is strict: field order and mandatory fields per IEC 61850-9-2,
definite BER lengths only, length fields checked against the
frame, `noASDU` equal to the number of ASDUs, fixed sizes for the
integer and time fields, visible strings for `svID`/`datSet`, no trailing
data inside the APDU (Ethernet padding after it is accepted). `Limits`
bounds the ASDUs per frame and the sample size (`DefaultLimits`: 64 and
1500 octets).

`DecodeInto` decodes into a caller-owned `Frame` and reuses its storage:
the ASDU slice, the VLAN tag and the `svID`/`datSet` strings (interned
per `Frame`), so steady-state decoding of interleaved streams does not
allocate. `Sample` and `Security` then alias the input buffer.

## Datasets

The `sample` field holds the dataset members back to back, each with the
fixed size of its basic type (IEC 61850-9-2):

| `Kind` | SCL bType | Octets |
|---|---|---|
| `KindBoolean` | BOOLEAN | 1 |
| `KindInt8`, `KindInt16`, `KindInt32`, `KindInt64` | INT8 … INT64 | 1, 2, 4, 8 |
| `KindUint8`, `KindUint16`, `KindUint24`, `KindUint32` | INT8U … INT32U | 1, 2, 3, 4 |
| `KindFloat32`, `KindFloat64` | FLOAT32, FLOAT64 | 4, 8 |
| `KindEnum` | Enum | 4 |
| `KindDbpos` | Dbpos, Tcmd | 4 |
| `KindQuality` | Quality | 4 (bit 0 of the 13-bit Quality in the LSB) |
| `KindTimestamp` | Timestamp | 8 (UtcTime) |

A `Layout` (`[]Kind`) converts between samples and
[go-mms](https://github.com/otfabric/go-mms) values:

```go
layout := sv.Layout{sv.KindInt32, sv.KindQuality, sv.KindFloat32}
sample, err := layout.Encode(nil, []*mms.Value{mms.NewInteger(5), sv.QualityGood.Value(), mms.NewFloat(1.5)})
values, err := layout.Decode(sample)
v := sv.Int32At(sample, layout.Offsets()[0])        // zero-copy accessors
```

`Quality` is the 13-bit IEC 61850-7-3 quality with IEC bit *i* at integer
bit *i* (`QualityInvalid` = 2, `QualityQuestionable` = 3, `QualityTest`,
`QualityDerived` …); `Validity`, `IsGood`, `Value` and
`QualityFromValue` convert it.

### 9-2LE

`LELayout` is the UCA 9-2LE dataset: `IA, IB, IC, IN, UA, UB, UC, UN`,
each an INT32 value followed by its Quality (64 octets). `LESample`
holds it, `Append` encodes and `DecodeLE` decodes without allocating.
Currents are in 1 mA per count and voltages in 10 mV per count:
`AmpsCount`, `VoltsCount` (saturating) and `Amps`, `Volts` convert.
9-2LE streams run at 80 samples per period (one ASDU per frame) for
protection and 256 samples per period (eight ASDUs per frame) for
metering.

## Publisher

```go
pub, err := sv.NewPublisher(tr, sv.PublisherConfig{
    Dst: sv.MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x01}, AppID: 0x4000,
    VLAN: &sv.VLAN{Priority: 4},
    SvID: "MU01", ConfRev: 1,
    SmpRate: 80, SmpMod: sv.SmpPerPeriod, NominalFrequency: 50, // 4000 samples/s
    NoASDU: 1, SmpSynch: sv.SmpSynchGlobal,
    Layout: sv.LELayout,
    Source: func(info sv.SampleInfo, dst []byte) []byte {      // per sample
        return waveform(info.Index, info.Time).Append(dst)
    },
})
go pub.Run(ctx)
```

- Samples are due at `t0 + k/rate` on an absolute schedule, so timer
  jitter does not accumulate. A frame of `NoASDU` samples is sent when
  its last sample is due. When the publisher falls behind it sends up to
  `MaxBurst` frames at once (default 16) and then skips the samples it
  cannot catch up with; `State()` reports `Samples`, `Frames`,
  `Skipped`, `Late` and `SendErrors`.
- `smpCnt` counts from 0 and wraps at `SmpCntWrap`, by default the
  number of samples per second when that is integral (IEC 61869-9), else
  65536. `AlignSecond` starts at the next whole second with `smpCnt` 0,
  as merging units synchronised to a 1 PPS or PTP reference do.
- Without `Source`, every ASDU carries the current sample: `Update(values)`
  (through `Layout`) or `UpdateSample(raw)` replace it; both are safe
  to call while `Run` is running. With `Source` the function is called
  for every sample with its index, `smpCnt` and due time.
- `SetEnabled`, `SetSmpSynch` and `SetSimulation` change the stream at
  run time. `Disabled` starts it disabled.
- `RefrTm`, `SendSmpRate`, `SendSmpMod`, `GmIdentity` and a non-empty
  `DatSet` add the optional ASDU fields.
- `Run` returns when `ctx` is cancelled or the publisher is closed;
  `ErrClosed` from the transport stops it.

## Subscriber

```go
s, err := sv.NewSubscriber(tr, sv.SubscriberConfig{})
sub, err := s.Subscribe(sv.SubscriptionConfig{
    Dst: sv.MAC{0x01, 0x0c, 0xcd, 0x04, 0x00, 0x01}, AppIDs: []uint16{0x4000},
    SvID: "MU01", ConfRev: 1, SmpRate: 80, SmpMod: sv.SmpPerPeriod,
    Layout: sv.LELayout, Timeout: 10 * time.Millisecond,
    OnASDU: func(smp *sv.Sample) {             // called on the Run goroutine
        var le sv.LESample
        _ = sv.DecodeLE(smp.ASDU.Sample, &le)
    },
})
go s.Run(ctx)
for ev := range sub.Events() { … }             // gaps, timeouts, …
```

- A subscription filters on destination address, source address, APPIDs
  and `svID` (zero values match anything) and validates `datSet` (when
  the ASDU carries it), `confRev`, `smpRate`/`smpMod` (when carried) and
  the sample size. Invalid ASDUs produce `EventInvalid` with a
  `*ValidationError` and are not delivered.
- Streams are keyed by source address, APPID and `svID`. For each the
  subscriber tracks `smpCnt` modulo the wrap: derived from `smpRate`,
  configured with `SmpCntWrap`, or learned from the first wrap to 0.
  A forward jump is a gap (`EventGap`, `Sample.Lost`), a repeat of the
  last counter a duplicate, and an older counter out of order; duplicate
  and late ASDUs are counted and not delivered.
- Supervision: `EventStart`, `EventTimeout` (no ASDU within `Timeout`,
  default 100 ms), `EventRestored`, `EventSynch` (`smpSynch` changed),
  `EventConfRev` (when `ConfRev` is not configured).
- Delivery: `OnASDU` (synchronous, no allocation; `Sample` and its
  references are valid only during the call) or the `Samples()` channel
  (`SampleQueue` > 0; full queues drop and count `QueueDrops`). Events go
  to `Events()` (`EventQueue`, default 256).
- `Stats()` returns per stream: `Status` (ok, timeout, invalid), frame
  and ASDU counts, `Lost`, `Duplicates`, `OutOfOrder`, `Invalid`,
  `Timeouts`, `QueueDrops`, `LastSmpCnt`, `SmpSynch`, `ConfRev`,
  `Simulation`, the wrap and the measured `Rate` (samples/s over 500 ms).
- Frames with the simulation bit are dropped unless
  `SubscriberConfig.AcceptSimulation` is set.
- `HandleFrame` and `CheckTimeouts` drive the subscriber without a
  transport (e.g. from a pcap).

## Transports

`sv.Transport` is the shared `l2.Transport`. `sv.Open(iface, opts)`
opens the raw transport of [GOOSE.md](GOOSE.md#transports) filtered on
EtherType `0x88BA`; set `opts.EtherTypes` to receive GOOSE and SV on one
socket and split them with `l2.NewDemux`:

```go
tr, err := l2.Open("eth1", l2.TransportOptions{EtherTypes: []uint16{l2.EtherTypeGOOSE, l2.EtherTypeSV}})
d := l2.NewDemux(tr, 0)
gooseTr, _ := d.Port(l2.EtherTypeGOOSE)
svTr, _ := d.Port(l2.EtherTypeSV)
```

`sv.NewHub()` is the in-memory segment with fault injection.

## SCL binding

```go
doc, _ := scl.ParseFile("station.scd")
cb, err := sv.ControlBlockFromSCL(doc, "MUIED", "MU01", "MSVCB01")
pc, err := cb.PublisherConfig()     // address, svID, rates, OptFlds, Layout
sc := cb.SubscriptionConfig()       // filters and validation
```

`ControlBlocksFromSCL` returns every `SampledValueControl` of every IED.
A `ControlBlock` carries the reference (`<IED><LD>/LLN0$MS$<name>`),
`svID`, the qualified dataset, `confRev`, `smpRate`, `smpMod`,
`nofASDU`, `OptFlds` (from `SmvOpts`), the `SMV` address (MAC, APPID,
VLAN) and the dataset `Members` with their flattened `Layout`. Members
of a type SV cannot carry (strings, octet strings) are reported as
errors wrapping `ErrSCL`.

## Server MSVCBs

```go
srv, _ := iec61850.NewServer(model, iec61850.ServerOptions{})
engine, err := srv.EnableSV(iec61850.SVOptions{
    Transport: tr, SmpSynch: sv.SmpSynchLocal,
})
```

Every MSVCB (SCL `SampledValueControl` with an `SMV` address) gets an
`sv.Publisher` sending the current dataset values at `SmpRate`; changes
through `Server.SetValue` or MMS writes reach the next samples. For
waveforms, take `engine.Publisher("MU01/LLN0$MS$MSVCB01")` and call
`UpdateSample`. `StartDisabled` leaves `SvEna` false;
`engine.SetEnabled` toggles it like a client write.

The MSVCB attributes are served under functional constraint `MS`
(IEC 61850-8-1): `SvEna`, `MsvID`, `DatSet`, `ConfRev`, `SmpRate`,
`OptFlds`, `SmpMod`, `DstAddress` (`Addr`, `PRIORITY`, `VID`, `APPID`)
and `noASDU`. `SvEna`, `MsvID`, `DatSet`, `SmpRate`, `OptFlds` and
`SmpMod` are writable; all but `SvEna` only while `SvEna` is false
(temporarily-unavailable otherwise). A `DatSet` change increments
`ConfRev`. Invalid values (empty `MsvID`, zero `SmpRate`, `SmpMod`
outside 0–2, an unknown dataset) are rejected with object-value-invalid.
`Capabilities().SV` reports the engine.

## Client MSVCB access

```go
cb, err := client.GetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01")
on, rate := true, uint16(256)
err = client.SetMSVCB(ctx, "MU01", "LLN0$MS$MSVCB01", iec61850.MSVCBUpdate{SmpRate: &rate, SvEna: &on})
```

`SetMSVCB` writes `SvEna` last. `GetMSVCB` also decodes Edition 1
MSVCBs without `SmpMod`.

## Errors

| Sentinel | Meaning |
|---|---|
| `ErrInvalidFrame` | not an Ethernet frame, or truncated header |
| `ErrNotSV` | another EtherType |
| `ErrInvalidLength` | an APDU or BER length disagrees with the frame |
| `ErrInvalidPDU` | malformed `savPdu` or ASDU (field order, mandatory fields, sizes, strings) |
| `ErrInvalidData` | a sample does not match its layout |
| `ErrLimitExceeded` | a `Limits` bound was exceeded (also on encode: frame over 1500 octets) |
| `ErrInvalidValue` | a value cannot be encoded |
| `ErrInvalidConfig` | invalid publisher, subscriber or layout configuration (`*ConfigError`) |
| `ErrValidation` | an ASDU failed subscription validation (`*ValidationError`, with `ErrConfRevMismatch`, `ErrDatSetMismatch`, `ErrSmpRateMismatch` or `ErrSampleSizeMismatch`) |
| `ErrSCL`, `ErrNotFound` | SCL binding failures |
| `ErrClosed`, `ErrUnsupportedPlatform`, `ErrPermission`, `ErrInterfaceNotFound` | transport errors, shared with `l2` and `goose` |

Decode failures are `*DecodeError` values with the field and offset. On
the server, `ErrSV` wraps `EnableSV` failures. See [ERRORS.md](ERRORS.md).

## Metrics

`PublisherConfig.Metrics` and `SubscriberConfig.Metrics` take an
`sv.Metrics` (`FrameSent`, `SendError`, `ASDUReceived`, `FrameDropped`
with reason `decode`, `simulation` or `filtered`, `Event`);
`sv.NopMetrics` can be embedded. Loggers receive transport and pacing
warnings.

## Performance

Measured on an Apple M-series laptop (`go test -bench . ./sv`):

| Benchmark | Time | Allocations |
|---|---|---|
| `DecodeInto`, 1 ASDU | 89 ns | 0 |
| `DecodeInto`, 8 ASDUs | 530 ns | 0 |
| `AppendFrame` | 66 ns | 0 |
| Publisher, one frame | 85 ns | 0 |
| Subscriber, 1 stream | 180 ns/frame | 0 |
| Subscriber, 32 interleaved 4 kHz streams | 217 ns/frame (2.8 % of a core) | 0 |

`TestSubscriberZeroAlloc` pins the steady-state receive and send paths
to zero allocations.

## Testing

- Unit tests use the hub; `TestChaosAccounting` checks that under loss,
  duplication and reordering across counter wraps every sample is
  either delivered once or counted lost, and every duplicate and late
  frame is reported; `TestChaosMutation` flips bits in every frame.
- Fuzz targets: `FuzzDecode` (round trip and `Decode`/`DecodeInto`
  agreement), `FuzzSubscriber`.
- Raw transport tests run on `lo` on Linux as root (CI does) and on
  `GOOSE_TEST_IFACE` elsewhere (package `l2`).
- Interop: see [INTEROP.md](INTEROP.md).

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
- **Timing** — the publisher is software-timed: it paces on an absolute
  schedule, catches up in bursts of at most `MaxBurst` frames and counts
  what it could not send in `PublisherState.Late` and `Skipped`. Run it on
  a lightly loaded host, keep GC pressure low in the `Source` callback
  (the publisher itself does not allocate per frame) and monitor `Late`,
  `Skipped` and the subscribers' `Lost`. It does not replace a hardware
  merging unit with PTP or 1PPS synchronisation: set `SmpSynch` to what
  the host clock really provides (`SmpSynchLocal` without a reference).
- **Receive buffers** — at 4000 frames/s per stream a subscriber stall
  of a few hundred milliseconds overruns the default Linux socket buffer;
  the transport does not change `SO_RCVBUF`, so raise
  `net.core.rmem_default` for hosts that subscribe to many streams.

## Limitations

- No SV security (IEC 62351-6/-9): `security` is carried opaquely.
- No unicast SV (USVCB) on the server; no SCL `multicast="false"`
  handling (the SCL parser cannot tell an absent attribute from false).
- `SmvOpts sampleSynchronized` is reported but has no effect: `smpSynch`
  is always sent (Edition 2).
- The server publishes dataset values from its store; waveform sources
  are fed through `SVEngine.Publisher`.
- No Windows transport.
