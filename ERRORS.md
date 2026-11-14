# Error Taxonomy

go-iec61850 uses Go's standard error model: sentinel values for category checks
(`errors.Is`) and typed error structs for structured detail (`errors.As`).
All errors wrap one of the sentinels listed here or carry a typed struct.

See also the underlying MMS layer: [go-mms/ERRORS.md](https://github.com/otfabric/go-mms/blob/main/ERRORS.md).

---

## Sentinel errors

Use `errors.Is(err, iec61850.ErrXxx)` for category classification.

| Sentinel | When returned |
|----------|---------------|
| `ErrClosed` | Operation attempted after the client was closed or aborted, or after its association ended (`Client.Err` matches it then, with the go-mms error) |
| `ErrNotFound` | Requested IEC 61850 object, logical device, or logical node not found |
| `ErrInvalidReference` | The supplied object reference string is malformed |
| `ErrInvalidFunctionalConstraint` | Unknown or invalid FC string |
| `ErrTypeMismatch` | Value type does not match the expected schema type |
| `ErrUnsupportedService` | The server does not support the requested service |
| `ErrSubscriptionClosed` | The report subscription channel has been closed |
| `ErrSCLParse` | SCL/ICD/CID file parsing failed |
| `ErrModelMismatch` | The server model does not match what the SCL describes |
| `ErrUnsupportedCDC` | Common Data Class is not supported |
| `ErrReportDecode` | A received report PDU could not be decoded |
| `ErrDatasetDecode` | A received dataset response could not be decoded |
| `ErrDataAccess` | Per-variable data access error from the server |
| `ErrInvalidArgument` | A caller-supplied argument is invalid |
| `ErrProtocol` | Wire-level protocol error from the underlying MMS stack |
| `ErrControlFailed` | A control operation failed (generic — wraps more specific sentinels) |
| `ErrSelectFailed` | Select step of an SBO control failed |
| `ErrOperateFailed` | Operate step of a control failed |
| `ErrCancelFailed` | Cancel step of a control failed |
| `ErrNotControllable` | Target data object has no controllable model |
| `ErrGOOSE` | Server GOOSE engine: already enabled, no transport, engine stopped, GoCB without address |
| `ErrSV` | Server SV engine: already enabled, no transport, engine stopped, MSVCB sample not encodable |
| `ErrLimitExceeded` | A limit of the package was reached: `ReadFile` beyond `FileLimits.MaxSize`, `ReadFile` or `DownloadFile` beyond `FileLimits.SizeTolerance` over the announced size or after more than `FileLimits.MaxEmptyChunks` empty answers, `ReadJournalAll` or `ReadJournalAfterAll` beyond `JournalLimits.MaxEntries` or `JournalLimits.MaxBytes`, a server model beyond `ServerModelOptions.MaxExpandedNodes` or with type references nested deeper than `scl.DefaultMaxTypeDepth` (both also match `scl.ErrLimitExceeded`). See [SECURITY.md](SECURITY.md#limits) |

### Server write refusals

A server application callback (`ControlHandler`, `SettingGroupHandler`)
returns, or wraps, one of these to choose the MMS DataAccessError of the
refused client write (IEC 61850-8-1 8.1.3.4.4.2, Table 23); an
`*mms.DataAccessError` works too. Without one the service default applies.

| Sentinel | DataAccessError |
|----------|-----------------|
| `ErrWriteAccessDenied` | object-access-denied |
| `ErrWriteTemporarilyUnavailable` | temporarily-unavailable |
| `ErrWriteTypeInconsistent` | type-inconsistent |
| `ErrWriteValueInvalid` | object-value-invalid |
| `ErrWriteHardwareFault` | hardware-fault |

Codes the server sends on its own:

| Situation | DataAccessError |
|-----------|-----------------|
| Value of another type or layout, integer outside INTn, string or bit string of the wrong size (including a malformed Oper/SBOw/Cancel) | type-inconsistent |
| ENUMERATED value outside the EnumType | object-value-invalid |
| Non-zero IntgPd of a BRCB, URCB or LCB below `ServerOptions.MinIntegrityPeriod` | object-value-invalid |
| GoCB DatSet or MSVCB change that would take ConfRev past 4294967295; SvEna TRUE while SmpRate and SmpMod select more than `SVOptions.MaxSamplesPerSecond` | object-value-invalid |
| Read-only attribute or SGCB member (NumOfSG, LActTm, ResvTms also without `EnableSettingGroups`), write of SBO or of an Oper/SBOw/Cancel member alone, CF ctlModel of a registered control, control without `RegisterControl` | object-access-denied |
| Control refused with AddCause 12, 19 or 27 (in execution, already selected, locked by another client) | temporarily-unavailable |
| Other control refusals (not selected, inconsistent parameters, application refusal) | object-access-denied |
| SGCB EditSG/CnfEdit/FC=SE while another client holds the edit reservation, FC=SE without an edit group (leaf, FC-group `LN$SE` and data object `LN$SE$DO` reads included) | temporarily-unavailable |
| SGCB ActSG/EditSG out of 1..NumOfSG | object-value-invalid |
| SGCB of an unknown LD | object-non-existent |

### Package `goose`

Use `errors.Is(err, goose.ErrXxx)`. Decoder failures are `*goose.DecodeError`
values that unwrap to one of the decode sentinels. `*goose.ValidationError`
unwraps to `ErrValidation` and to the specific mismatch sentinel. See
[GOOSE.md](GOOSE.md#errors).

| Sentinel | Meaning |
|----------|---------|
| `ErrInvalidFrame` | Ethernet frame cannot carry a GOOSE message: too short, wrong EtherType, or a malformed 802.1Q tag |
| `ErrNotGOOSE` | Well-formed Ethernet frame whose EtherType is not 0x88B8 |
| `ErrInvalidLength` | GOOSE header length field inconsistent with the frame or the encoded PDU |
| `ErrInvalidPDU` | BER structure error in the goosePdu: bad tag or length, missing mandatory field, duplicate or out-of-order field, out-of-range value |
| `ErrUnsupportedPDU` | GOOSE APDU choice other than goosePdu, for example a GSE management PDU |
| `ErrInvalidData` | Malformed or unsupported MMS Data element in allData |
| `ErrLimitExceeded` | Decoder or encoder limit exceeded: nesting depth, entry count, frame size |
| `ErrInvalidValue` | Value cannot be encoded: nil element, unsupported type, out of range |
| `ErrSCL` | A control block could not be built from an SCL document |
| `ErrNotFound` | Requested control block, dataset or stream does not exist |
| `ErrValidation` | Base sentinel of subscriber validation failures |
| `ErrConfRevMismatch` | Received confRev differs from the expected configuration revision |
| `ErrDatSetMismatch` | Received datSet reference differs from the expected one |
| `ErrGoIDMismatch` | Received goID differs from the expected one |
| `ErrMemberMismatch` | numDatSetEntries, the allData element count or an element type does not match the expected dataset layout |
| `ErrNeedsCommissioning` | Received message has ndsCom set |

### Package `sv`

Use `errors.Is(err, sv.ErrXxx)`. Decoder failures are `*sv.DecodeError` values
that unwrap to one of the decode sentinels. `*sv.ValidationError` unwraps to
`ErrValidation` and to the specific mismatch sentinel. See [SV.md](SV.md#errors).

| Sentinel | Meaning |
|----------|---------|
| `ErrInvalidFrame` | Ethernet frame cannot carry an SV message: too short or a malformed 802.1Q tag |
| `ErrNotSV` | Well-formed Ethernet frame whose EtherType is not 0x88BA |
| `ErrInvalidLength` | SV header length field inconsistent with the frame or the encoded APDU |
| `ErrInvalidPDU` | BER structure error in the savPdu: bad tag or length, missing mandatory field, duplicate or out-of-order field, wrong field size, an svID or datSet longer than `MaxIDLength` (129) octets, or noASDU not matching the ASDU count |
| `ErrInvalidData` | Sample does not match its dataset layout: wrong size or an undecodable member |
| `ErrLimitExceeded` | Decoder or encoder limit exceeded: ASDU count, sample size, frame size |
| `ErrInvalidValue` | Value cannot be encoded in a sample: nil, wrong type, out of range; an svID or datSet longer than `MaxIDLength` on encode |
| `ErrSCL` | A control block could not be built from an SCL document |
| `ErrNotFound` | Requested control block or stream does not exist |
| `ErrValidation` | Base sentinel of subscriber validation failures |
| `ErrConfRevMismatch` | Received confRev differs from the expected configuration revision |
| `ErrDatSetMismatch` | Received datSet reference differs from the expected one |
| `ErrSmpRateMismatch` | Received smpRate or smpMod differs from the expected one |
| `ErrSampleSizeMismatch` | Sample length differs from the expected dataset layout |

### Package `rsession`

Use `errors.Is(err, rsession.ErrXxx)`. Decoder failures are
`*rsession.DecodeError` values that unwrap to `ErrMalformed` or
`ErrUnsupported`. `ErrInvalidConfig` and `ErrClosed` are the `l2` values
below. See [GOOSE.md](GOOSE.md#r-goose-and-r-sv-over-udp).

| Sentinel | Meaning |
|----------|---------|
| `ErrMalformed` | Datagram is not a well-formed SPDU: transport header, a length past the datagram, a truncated field, trailing octets |
| `ErrUnsupported` | Protocol version other than 1 or 2, encryption, or a MAC algorithm other than HMAC-SHA256 |
| `ErrUnsecured` | SPDU without key ID and signature, refused without `Insecure` |
| `ErrUnknownKey` | SPDU under a key ID the key provider does not hold, or the provider returned a key with another ID, ID 0 or less than `MinKeySize` octets of material (then also `ErrInvalidKey`) |
| `ErrAuthentication` | Signature missing, of the wrong length or algorithm, or not verifying |
| `ErrReplay` | SPDU number already accepted, or older than the replay window, for that sender and key (for that key whatever the sender with `Config.SingleSenderPerKey`) |
| `ErrSenderNotAllowed` | SPDU from a source address outside `Config.AllowedSenders`, refused before verification |
| `ErrNoKey` | The key provider has no current key to send with |
| `ErrInvalidKey` | Key ID 0, key material shorter than 16 octets, or an unsupported MAC algorithm |
| `ErrInvalidFrame` | Frame passed to `Transport.Send` is not a GOOSE or SV frame of the transport's kind |
| `ErrLimitExceeded` | SPDU larger than 65507 octets, an APDU longer than 65535, or `Transport.Send` after SPDU number 0xffffffff was sent under the current key (make a key with another ID current) |
| `ErrUnsupportedPlatform` | Multicast socket options requested on a platform other than Linux, macOS or a BSD |

### Package `scl`

Parse failures are errors with text, and problems in a document are
`Diagnostic` values (see [API.md](API.md#scl-diagnostics)). One sentinel:

| Sentinel | Meaning |
|----------|---------|
| `ErrLimitExceeded` | A document beyond `ParseOptions.MaxBytes` or `ParseOptions.MaxDepth` (the parse is refused), or a type expansion beyond `FlattenOptions.MaxExpandedNodes`, type references nested deeper than `FlattenOptions.MaxTypeDepth` (`DefaultMaxTypeDepth`, 64), or the model limit of `ServerModelOptions`, `goose.SCLOptions` or `sv.SCLOptions`. See [scl/README.md](scl/README.md#limits) |

### Transport sentinels

These are the same values in `goose`, `sv` and `l2`, so `errors.Is` matches
across the three packages.

| Sentinel | Meaning |
|----------|---------|
| `ErrClosed` | Operation on a closed publisher, subscriber or transport |
| `ErrPermission` | Opening a raw Ethernet socket was denied: `CAP_NET_RAW` on Linux, read and write access to `/dev/bpf*` on macOS and BSD |
| `ErrInterfaceNotFound` | The named network interface does not exist |
| `ErrUnsupportedPlatform` | No raw Ethernet transport on this operating system |
| `ErrInvalidConfig` | Invalid publisher, subscriber or transport configuration; `*ConfigError` unwraps to it |

### `*goose.EncodeError`

Returned by `goose.Publisher.Publish` (and the GOOSE encoder) when a value
cannot be encoded or does not match the dataset layout. `Field` names the
part of the PDU (`values` or `allData`) and `Index` the element.

```go
type EncodeError struct {
    Field string // PDU part that failed: "values" or "allData"
    Index int    // index of the failing element
    Err   error  // underlying error
}
```

Unwraps to `Err`, which matches `goose.ErrInvalidValue` (or
`goose.ErrLimitExceeded` for nesting too deep).

### `*l2.TransportError`

Wraps an operating-system error from a raw Ethernet backend. `goose` and
`sv` re-export it as the alias `goose.TransportError` / `sv.TransportError`.

```go
type TransportError struct {
    Op        string // operation, for example "open" or "membership"
    Interface string // network interface name
    Err       error  // underlying operating-system error
    Kind      error  // optional classification sentinel
}
```

Unwraps to `Err` and, when set, to `Kind`: one of `ErrPermission`,
`ErrInterfaceNotFound`, `ErrUnsupportedPlatform` or `ErrClosed`.

---

## Typed errors

Use `errors.As(err, &target)` to extract structured detail.

### `*ReferenceError`

Returned when an IEC 61850 object reference cannot be parsed or resolved.

```go
type ReferenceError struct {
    Input   string // the reference string that caused the error
    Reason  string // human-readable explanation
    Wrapped error  // optional underlying error
}
```

Unwraps to `Wrapped` when set, otherwise to `ErrInvalidReference`.

### `*DecodeError`

Returned when an MMS value cannot be semantically decoded into an
IEC 61850 type.

```go
type DecodeError struct {
    Ref     string // object reference, if known
    Type    string // expected IEC 61850 type
    Message string // human-readable explanation
    Wrapped error  // optional underlying error
}
```

Unwraps to `Wrapped` when set, otherwise to `ErrTypeMismatch`.

### `*ModelError`

Returned when the server model (from SCL or live browse) is inconsistent
or incompatible with the requested operation.

```go
type ModelError struct {
    Ref     string // the object reference involved
    Message string
    Wrapped error
}
```

Unwraps to `Wrapped` when set, otherwise to `ErrModelMismatch`.

### `*ReportError`

Returned when a report subscription encounters a protocol-level problem
after the subscription has been established, and by `DecodeReport` for
report values that do not follow the report format.

```go
type ReportError struct {
    RCBRef  string // report control block reference
    Message string
    Wrapped error
}
```

Unwraps to `Wrapped` when set, otherwise to `ErrReportDecode`.

### `*SCLParseError`

Returned when an SCL/ICD/CID file cannot be parsed.

```go
type SCLParseError struct {
    File    string // file path, if known
    Line    int    // approximate line number (0 if unknown)
    Message string
    Wrapped error
}
```

Unwraps to `Wrapped` when set, otherwise to `ErrSCLParse`.

### `*DataAccessError`

Returned per item by bulk read/write and dataset reads when the server
reports a per-variable data access failure. Wraps `ErrDataAccess`;
`ErrorCode` holds the underlying MMS code as an `int` (convert with
`mms.DataAccessErrorCode(de.ErrorCode)`).

```go
type DataAccessError struct {
    Ref       string // object reference of the failed item
    ErrorCode int    // mms.DataAccessErrorCode value
    Operation string // "read", "write", "read-dataset" or "write-dataset"
}
```

Common codes:

| Code | Meaning |
|------|---------|
| `DataAccessErrorObjectNonExistent` | Object not found on the server (unknown domain or variable) |
| `DataAccessErrorObjectUndefined` | Alternate-access mismatch (component or index does not apply) |
| `DataAccessErrorObjectAccessDenied` | Access not permitted |
| `DataAccessErrorTemporarilyUnavailable` | Temporarily unavailable |
| `DataAccessErrorTypeInconsistent` | Write value type mismatch |

### `*ControlError`

Returned when a control operation (direct, SBO, or SBOw) fails with a
server-supplied reason.

```go
type ControlError struct {
    Ref       string   // the control object reference
    Operation string   // "select", "operate", or "cancel"
    AddCause  AddCause // server-reported cause via LastApplError (0 = unknown)
    LastApplError *LastApplError // the server's LastApplError, nil if it sent none
    Wrapped   error    // underlying error, if any
}
```

`LastApplError.Error` is 0 for a refusal, 1 (Unknown) for an internal
server error, 2 (Timeout Test Not OK) or 3 (Operator Test Not OK) for a
failed time-activated or immediate operate with Test (IEC 61850-8-1
20.11); a server callback sets another one with a `ControlError` whose
`LastApplError.Error` is non-zero. A server error in the `ControlError`
chain may carry the write's `*mms.DataAccessError` (`errors.As`).

`Client.OperateWait` returns a `*ControlError` wrapping
`ErrCommandTerminationNegative` when the server ends an accepted operate with a
negative CommandTermination; `AddCause` and `LastApplError` come from it.

Matches the operation-specific sentinel (`ErrSelectFailed`, `ErrOperateFailed`,
`ErrCancelFailed`; `ErrControlFailed` for any other operation) and `Wrapped`
via `errors.Is`.

---

## Underlying MMS errors

Operations that communicate with the server can return errors from the MMS
layer. These are wrapped and typically match one of:

- `mms.ErrClosed` — transport connection lost
- `mms.ErrInvokeTimeout` — `MMS.RequestTimeout` elapsed (also matches
  `context.DeadlineExceeded`)
- `mms.ErrAssociationFailed`, `mms.ErrConnectionRejected` —
  [`Dial`](API.md#dialoptions) could not associate
- `*mms.RejectError` — the peer answered with an MMS Reject PDU
- `mms.ErrServerClosed` — returned by `Server.ListenAndServe` after
  `Server.Shutdown`
- `*mms.ServiceError` — server returned a ConfirmedError PDU (`CodeName()`
  names the class/code; since go-mms v1.2.0 `Error()` ends with it, so do
  not compare error strings)
- `*mms.ProtocolError` — wire-level protocol violation
- `*mms.DecodeError` — received PDU could not be parsed

Use `errors.As` to inspect them directly when fine-grained MMS error handling
is needed. For most IEC 61850 application code, the sentinels above are
sufficient.

---

## Usage patterns

```go
import (
    "errors"
    iec61850 "github.com/otfabric/go-iec61850"
    "github.com/otfabric/go-mms"
)

val, err := client.Read(ctx, ref)
if err != nil {
    switch {
    case errors.Is(err, iec61850.ErrNotFound):
        // object does not exist on this server
    case errors.Is(err, iec61850.ErrDataAccess):
        var de *iec61850.DataAccessError
        if errors.As(err, &de) {
            log.Printf("access error: %v", mms.DataAccessErrorCode(de.ErrorCode))
        }
    case errors.Is(err, iec61850.ErrClosed):
        // connection was closed; reconnect if needed
    default:
        return err
    }
}
```

```go
params := iec61850.OperateParams{CtlVal: iec61850.BoolCtlVal(true)}
if err := client.Operate(ctx, ref, params); err != nil {
    var ce *iec61850.ControlError
    if errors.As(err, &ce) {
        log.Printf("control %s op=%s addCause=%s", ce.Ref, ce.Operation, ce.AddCause)
    }
}
```
