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
- `mms.ServiceError` — server returned a ConfirmedError PDU
- `mms.ProtocolError` — wire-level protocol violation
- `mms.DecodeError` — received PDU could not be parsed

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
