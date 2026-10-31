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
| `ErrClosed` | Operation attempted after the client was closed or aborted |
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
| `ErrInvalidPDU` | BER structure error in the savPdu: bad tag or length, missing mandatory field, duplicate or out-of-order field, wrong field size, or noASDU not matching the ASDU count |
| `ErrInvalidData` | Sample does not match its dataset layout: wrong size or an undecodable member |
| `ErrLimitExceeded` | Decoder or encoder limit exceeded: ASDU count, sample size, frame size |
| `ErrInvalidValue` | Value cannot be encoded in a sample: nil, wrong type, out of range |
| `ErrSCL` | A control block could not be built from an SCL document |
| `ErrNotFound` | Requested control block or stream does not exist |
| `ErrValidation` | Base sentinel of subscriber validation failures |
| `ErrConfRevMismatch` | Received confRev differs from the expected configuration revision |
| `ErrDatSetMismatch` | Received datSet reference differs from the expected one |
| `ErrSmpRateMismatch` | Received smpRate or smpMod differs from the expected one |
| `ErrSampleSizeMismatch` | Sample length differs from the expected dataset layout |

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
after the subscription has been established.

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
    Operation string // "read", "write", or "read-dataset"
}
```

Common codes:

| Code | Meaning |
|------|---------|
| `DataAccessErrorObjectNonExistent` | Object not found on the server (unknown domain or variable; go-mms v1.1.0+) |
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
    Wrapped   error    // underlying error, if any
}
```

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
