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

// Check for underlying MMS errors
var mmsErr *mms.ServiceError
if errors.As(err, &mmsErr) {
    log.Printf("MMS error class=%d code=%d", mmsErr.Class, mmsErr.Code)
}
```
