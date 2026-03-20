# Error Handling

This document describes the error taxonomy used by `go-iec61850`.

## Sentinel errors

Sentinel errors are package-level variables that can be tested with
`errors.Is()`:

| Sentinel | Meaning |
|----------|---------|
| `ErrInvalidReference` | Syntactically invalid IEC 61850 object reference |
| `ErrInvalidFunctionalConstraint` | Unrecognized functional constraint value |
| `ErrNotFound` | Requested IEC 61850 object does not exist on the server |
| `ErrTypeMismatch` | Value type does not match the expected IEC 61850 type |
| `ErrUnsupportedService` | Service not supported by the server or library |
| `ErrSubscriptionClosed` | Report subscription has been closed |
| `ErrSCLParse` | Failure to parse an SCL file |
| `ErrModelMismatch` | Mismatch between expected and actual data model |
| `ErrUnsupportedCDC` | Unsupported Common Data Class |
| `ErrReportDecode` | Failure to decode a report payload |
| `ErrDatasetDecode` | Failure to decode dataset contents |
| `ErrClosed` | Client connection has been closed |
| `ErrDataAccess` | Per-variable data access failure from server |
| `ErrInvalidArgument` | Invalid caller-supplied argument |
| `ErrProtocol` | Protocol-level mismatch between client and server |
| `ErrControlFailed` | Control operation (operate, select, cancel) rejected or failed |
| `ErrSelectFailed` | Select or select-with-value request denied |
| `ErrOperateFailed` | Operate request denied or could not be executed |
| `ErrCancelFailed` | Cancel request denied or could not be executed |
| `ErrNotControllable` | Target data object's ctlModel is status-only |

## Typed errors

Typed error structs provide additional context and can be inspected with
`errors.As()`:

### ReferenceError

Returned when an IEC 61850 object reference is malformed or invalid.

```go
var refErr *iec61850.ReferenceError
if errors.As(err, &refErr) {
    fmt.Println("Bad input:", refErr.Input)
    fmt.Println("Reason:", refErr.Reason)
}
```

Fields: `Input` (the raw reference string), `Reason` (why it failed),
`Wrapped` (underlying error).

### DecodeError

Returned when semantic decoding of an MMS value fails.

```go
var decErr *iec61850.DecodeError
if errors.As(err, &decErr) {
    fmt.Println("Reference:", decErr.Ref)
    fmt.Println("Expected type:", decErr.Type)
}
```

Fields: `Ref`, `Type`, `Message`, `Wrapped`.

### ModelError

Returned for model inconsistencies (e.g., expected DO not found).

Fields: `Ref`, `Message`, `Wrapped`.

### ReportError

Returned for report-related failures (RCB operations, report decoding).

Fields: `RCBRef`, `Message`, `Wrapped`.

### SCLParseError

Returned for SCL parsing failures with optional file/line context.

Fields: `File`, `Line`, `Message`, `Wrapped`.

### DataAccessError

Returned when a per-variable data access operation fails on the server.

```go
var daErr *iec61850.DataAccessError
if errors.As(err, &daErr) {
    fmt.Println("Reference:", daErr.Ref)
    fmt.Println("Error code:", daErr.ErrorCode)
    fmt.Println("Operation:", daErr.Operation)
}
```

Fields: `Ref`, `ErrorCode`, `Operation`.

### ControlError

Returned when a control operation (operate, select, cancel) is rejected.

```go
var ctlErr *iec61850.ControlError
if errors.As(err, &ctlErr) {
    fmt.Println("Reference:", ctlErr.Ref)
    fmt.Println("Operation:", ctlErr.Operation)
    fmt.Println("AddCause:", ctlErr.AddCause)
}
```

Fields: `Ref`, `Operation`, `AddCause`, `Wrapped`.

## SCL diagnostics

The `scl` package uses a separate diagnostic system for non-fatal parse
and validation issues. Diagnostics are returned via `Result.Diagnostics`
and do not use `error` — they use structured `Diagnostic` values:

```go
type Diagnostic struct {
    Severity DiagSeverity // "error", "warning", or "info"
    Code     string       // machine-readable category
    Path     string       // SCL path (e.g. "IED[IED1]/LD[LD0]/LN[LLN0]")
    Message  string       // human-readable description
}
```

### Diagnostic codes

| Code | Severity | Meaning |
|------|----------|---------|
| `duplicate-id` | error | Duplicate type template ID |
| `duplicate-ied` | error | Duplicate IED name |
| `duplicate-access-point` | error | Duplicate AccessPoint within IED |
| `duplicate-ld` | error | Duplicate LDevice inst within IED |
| `duplicate-ln` | error | Duplicate LN within LDevice |
| `duplicate-dataset` | warning | Duplicate DataSet name within LN |
| `duplicate-report` | warning | Duplicate ReportControl name within LN |
| `duplicate-connected-ap` | warning | Duplicate ConnectedAP in SubNetwork |
| `missing-dotype` | error | LNodeType or SDO references nonexistent DOType |
| `missing-datype` | error | DA or BDA references nonexistent DAType |
| `missing-enumtype` | error | DA or BDA references nonexistent EnumType |
| `missing-lnodetype` | error | LN references nonexistent LNodeType |
| `missing-dataset` | error | Control block references nonexistent DataSet |
| `missing-connected-ap` | error | ConnectedAP references nonexistent IED or AP |
| `missing-ld` | warning | GSE/SMV references nonexistent LDevice |
| `unresolved-gse-control` | warning | GSE cbName does not match any GSEControl |
| `unresolved-smv-control` | warning | SMV cbName does not match any SMVControl |
| `unresolved-fcda` | warning | FCDA references nonexistent LDevice |
| `unresolved-topology-lnode` | error/warning | Topology LNode references nonexistent IED/LD/LN |
| `invalid-count` | warning | Numeric attribute could not be parsed |

### Validation API

The primary validation API is `scl/validate.All()`, which runs all
validation passes against the normalized model and shared index:

```go
idx, idxDiags := index.Build(doc)
diags := validate.All(doc, idx, idxDiags)
```

Passes: templates, IEDs, communication (including GOOSE/SMV control block
linkage), datasets, controls, and topology.

## Error wrapping

All errors wrap lower-level `go-mms` errors using `fmt.Errorf` with `%w`,
preserving the full error chain. This means `errors.Is()` and `errors.As()`
work transitively — you can test for both IEC 61850 sentinels and underlying
MMS error types.

## Usage patterns

```go
// Test for a specific sentinel
if errors.Is(err, iec61850.ErrNotFound) {
    // object does not exist
}

// Extract typed context
var reportErr *iec61850.ReportError
if errors.As(err, &reportErr) {
    log.Printf("report %s failed: %s", reportErr.RCBRef, reportErr.Message)
}

// Control error with AddCause
var ctlErr *iec61850.ControlError
if errors.As(err, &ctlErr) {
    log.Printf("control %s %s failed: addCause=%d", ctlErr.Ref, ctlErr.Operation, ctlErr.AddCause)
}

// Check for underlying MMS errors
var mmsErr *mms.ServiceError
if errors.As(err, &mmsErr) {
    log.Printf("MMS error class=%d code=%d", mmsErr.Class, mmsErr.Code)
}
```
