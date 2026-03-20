# REQUIREMENTS.md — `otfabric/go-iec61850`

## 1. Purpose

`go-iec61850` is a **pure Go**, **IEC 61850 MMS-only** library that provides a high-level, strongly typed, ergonomic API for interacting with IEC 61850 servers and data models on top of `go-mms`.

It must:

- expose **IEC 61850 concepts**, not generic MMS internals
- remain **strictly MMS-based** in scope
- provide an excellent foundation for a polished CLI such as `iec61850ctl`
- make common IEC 61850 workflows easy, typed, observable, and testable
- avoid CGO and avoid mirroring the C reference structure directly

It must **not** include:

- GOOSE
- Sampled Values / SV / SMV
- raw Ethernet capture/transmit
- unrelated SCADA protocols
- direct exposure of ACSE / presentation / session / BER internals
- C-style mutable handle soup

## 2. Scope

### In scope

The initial `go-iec61850` library must support IEC 61850 client/server interactions implemented over MMS, including:

- connection/session establishment through `go-mms`
- IEC 61850 object reference parsing and formatting
- logical device / logical node / data object / data attribute traversal
- functional-constraint-aware reads and writes
- IEC 61850 type mapping on top of MMS values
- report control block discovery, inspection, subscription, enable/disable, GI
- data set discovery and reading
- file browsing and file download
- journal enumeration and journal reads
- device tree browsing
- SCL parsing and conversion helpers
- a server-facing model sufficient to back a future `iec61850ctl server` command

### Explicitly out of scope

For v0.x, the library must not implement:

- GOOSE
- SV / SMV
- raw Ethernet transport
- protection/control application logic
- station engineering workflows beyond SCL parsing
- full IEC 61850 edition-diff abstraction layer
- vendor-specific undocumented extensions as first-class API
- a giant all-in-one “IEC toolkit” package

## 3. Position in the stack

`go-iec61850` must sit above the already existing stack:

- `go-tpkt`
- `go-cotp`
- `go-mms`

`go-mms` is the generic ISO/MMS layer and already implements the MMS services and transport integration needed as a foundation. `go-iec61850` must treat it as the protocol substrate, not reimplement it.

### Layering rule

- `go-tpkt` / `go-cotp`: transport
- `go-mms`: generic MMS protocol
- `go-iec61850`: IEC 61850 semantics, references, FCs, reports, datasets, SCL, typed values

This separation is mandatory.

## 4. Primary design goals

### 4.1 Strong Go API

The API must feel like a modern Go library:

- `context.Context` first
- option structs over giant argument lists
- stable typed structs and enums
- predictable ownership and copy semantics
- good zero-value behavior where reasonable
- errors compatible with `errors.Is` / `errors.As`
- `log/slog` integration
- docs and examples as part of the design, not an afterthought

### 4.2 IEC-native abstractions

Users should work with IEC 61850 concepts such as:

- logical devices
- logical nodes
- data objects
- data attributes
- functional constraints
- object references
- report control blocks
- datasets
- quality
- timestamps
- trigger options
- report payloads

Users should **not** have to think in terms of raw MMS domains, item IDs, alternate access selectors, or raw `mms.Value` except in advanced escape hatches.

### 4.3 Strong typing

The library must favor typed references and typed values over stringly APIs and `map[string]any`.

### 4.4 Great CLI ergonomics

The public API must make the implementation of `iec61850ctl` elegant and unsurprising.

Every CLI feature should map cleanly to one or a few library calls.

### 4.5 Conservative surface

Expose stable, useful abstractions. Keep wire-level or protocol-plumbing details internal unless they clearly help users.

## 5. Functional requirements

Based on the target CLI shape, the library must support a client feature set covering:

- connection lifecycle
- browsing LD/LN/DO/DA hierarchy
- single-object reads
- bulk reads
- report control block inspection and mutation
- report subscription and graceful cleanup
- data set inspection
- file directory and file read
- journal listing and journal retrieval
- object tree serialization
- SCL parsing and flattening
- a server-side configuration/model path suitable for config generation and server start

## 6. Required public package shape

Recommended repo/package structure:

```text
go-iec61850/
  doc.go
  README.md

  client.go
  server.go
  options.go
  errors.go
  logging.go

  ref.go
  fc.go
  model.go
  browse.go
  read.go
  write.go
  bulk.go

  dataset.go
  report.go
  journal.go
  file.go
  timestamp.go
  quality.go
  values.go

  scl/
    parse.go
    model.go
    flatten.go

  internal/
    mapping/
    decode/
    encode/
    modelcache/
    subscription/
    servermodel/
```

### Package rules

- root package: client/server-facing IEC 61850 API
- `scl` subpackage: explicit SCL concerns
- no public BER, ACSE, session, or presentation packages
- no direct exposure of raw MMS wire structures
- no leaking C-derived naming or internal tables

## 7. Public API requirements

### 7.1 Client creation

A high-level client must be easy to construct on top of `go-mms`.

Example shape:

```go
type Client struct { ... }

type DialOptions struct {
    MMS        mms.DialOptions
    Logger     *slog.Logger
    Resolver   ReferenceResolver
    ModelCache ModelCacheOptions
    Strict     StrictnessOptions
}

func Dial(ctx context.Context, addr string, opts DialOptions) (*Client, error)
func NewClient(mmsClient *mms.Client, opts ClientOptions) *Client
func (c *Client) Close(ctx context.Context) error
func (c *Client) Abort() error
```

Requirements:

- support wrapping an already-created `*mms.Client`
- support direct dial convenience
- no hidden globals
- no package-level mutable config

### 7.2 Object reference model

The library must provide typed parsing and formatting for IEC 61850 references.

Example concepts:

```go
type ObjectReference string
type LogicalDeviceName string
type LogicalNodeName string
type DataObjectName string
type DataAttributeName string
type FunctionalConstraint string
```

Recommended typed parsed form:

```go
type Ref struct {
    LD   LogicalDeviceName
    LN   LogicalNodeName
    Path []string
    FC   FunctionalConstraint
}
```

Requirements:

- parse IEC 61850 references robustly
- preserve canonical formatting
- expose helpers for parent/child navigation
- expose validation errors as typed/sentinel errors
- avoid raw string slicing in user code

### 7.3 Browsing and tree traversal

The library must make these workflows first-class:

- list logical devices
- list logical nodes within LD
- list data objects within LN
- list data attributes within object
- build full tree
- flatten tree
- search by pattern/path prefix

Example API direction:

```go
func (c *Client) ListLogicalDevices(ctx context.Context) ([]LogicalDevice, error)
func (c *Client) ListLogicalNodes(ctx context.Context, ld LogicalDeviceName) ([]LogicalNode, error)
func (c *Client) ListDataObjects(ctx context.Context, ln Ref) ([]DataObject, error)
func (c *Client) ListDataAttributes(ctx context.Context, ref Ref) ([]DataAttribute, error)
func (c *Client) Tree(ctx context.Context, opts TreeOptions) (*ModelTree, error)
func (c *Client) FindPaths(ctx context.Context, q FindQuery) ([]Ref, error)
```

Requirements:

- stable ordering where possible
- pagination/continuation handled internally when MMS requires it
- caller should not manage GetNameList tokens manually
- metadata caching should be possible but optional

### 7.4 Read operations

The library must support:

- typed read of one object
- raw read of one object
- read of a functional-constraint-specific attribute
- bulk reads from a list
- best-effort partial-success bulk reads
- optional metadata-assisted decoding

Example direction:

```go
func (c *Client) Read(ctx context.Context, ref Ref) (*Value, error)
func (c *Client) ReadRaw(ctx context.Context, ref Ref) (*mms.Value, error)
func (c *Client) ReadMany(ctx context.Context, refs []Ref) ([]ReadResult, error)
func (c *Client) ReadInto(ctx context.Context, ref Ref, out any) error
```

Requirements:

- single-read convenience must be excellent
- bulk-read must preserve input order
- per-item error handling must be explicit
- typed decode should support common IEC 61850 semantic types like quality and timestamps
- advanced users must still have access to raw MMS-backed values

### 7.5 Write operations

Must support:

- writing typed/simple values
- writing structured values where valid
- control-related writes needed for MMS-based control support later
- bulk writes with explicit partial-success semantics

Example direction:

```go
func (c *Client) Write(ctx context.Context, ref Ref, v any) error
func (c *Client) WriteValue(ctx context.Context, ref Ref, v *Value) error
func (c *Client) WriteMany(ctx context.Context, reqs []WriteRequest) ([]WriteResult, error)
```

Requirements:

- validation before sending where possible
- clear distinction between local validation errors and remote service errors
- good type mismatch errors
- no ambiguous silent conversions

### 7.6 Data sets

Must support:

- list datasets
- get dataset metadata
- read dataset values
- optionally resolve dataset members into typed refs

Example direction:

```go
func (c *Client) ListDataSets(ctx context.Context, scope Ref) ([]DataSetRef, error)
func (c *Client) GetDataSet(ctx context.Context, ref DataSetRef) (*DataSet, error)
func (c *Client) ReadDataSet(ctx context.Context, ref DataSetRef) (*DataSetValues, error)
```

Requirements:

- distinguish dataset definition from dataset values
- member references must be typed
- ergonomic for `iec61850ctl get ds` and `list dss`

### 7.7 Reports and subscriptions

This is a major feature.

Must support:

- listing report control blocks
- fetching one RCB in detail
- enabling/disabling reporting
- GI trigger
- buffered/unbuffered reports
- report subscription handler
- graceful cleanup on close or context cancellation
- sync/interrogation support for CLI flows

Example direction:

```go
func (c *Client) ListReports(ctx context.Context, scope Ref) ([]ReportControlBlockRef, error)
func (c *Client) GetReportControlBlock(ctx context.Context, ref ReportControlBlockRef) (*ReportControlBlock, error)
func (c *Client) SetReportControlBlock(ctx context.Context, rcb *ReportControlBlock, mask ReportFieldMask) error
func (c *Client) SubscribeReport(ctx context.Context, req SubscribeReportRequest) (*ReportSubscription, error)
```

Handler model:

```go
type ReportHandler func(ReportIndication)

type ReportSubscription struct { ... }
func (s *ReportSubscription) Close(ctx context.Context) error
```

Requirements:

- no callback leaks
- deterministic unsubscribe/disable flow
- typed report fields
- decode entry ID, entry time, GI, reasons, dataset values, quality, timestamps
- report payload should be accessible both semantically and raw

### 7.8 File services

Must support:

- list files
- read/download file
- optional raw file open/read/close passthrough for advanced use

Example direction:

```go
func (c *Client) ListFiles(ctx context.Context, path string) ([]FileEntry, error)
func (c *Client) ReadFile(ctx context.Context, path string) ([]byte, error)
func (c *Client) DownloadFile(ctx context.Context, path string, w io.Writer) error
```

Requirements:

- elegant wrapper over underlying MMS file services
- hide FRSM/file handle complexity from ordinary users
- preserve advanced access as escape hatch only

### 7.9 Journals

Must support:

- list journals
- get journal entries
- support time-range and start-after styles where meaningful

Example direction:

```go
func (c *Client) ListJournals(ctx context.Context, ld LogicalDeviceName) ([]JournalRef, error)
func (c *Client) ReadJournal(ctx context.Context, req ReadJournalRequest) (*JournalResult, error)
```

Requirements:

- typed journal references
- structured journal entries
- practical API for CLI consumption

### 7.10 SCL support

A dedicated `scl` subpackage must support:

- parsing ICD/CID/SCD
- producing a structured Go model
- tree rendering
- flat list generation
- CSV conversion helpers
- serving as a metadata source for reference validation and typed decoding

Example direction:

```go
package scl

func Parse(data []byte) (*Model, error)
func ParseFile(path string) (*Model, error)
func Flatten(m *Model, opts FlattenOptions) ([]FlatNode, error)
```

Requirements:

- no dependency on a live server
- clear separation from runtime MMS client
- model reusable by both CLI and server packages

### 7.11 Server-facing API

Because the target CLI includes:

- `server generate-config`
- `server start`

the library must reserve room for a server-side API, but the first release may scope this conservatively.

Recommended split:

- runtime client API first-class in v0.x
- server model/config generation available in staged form
- do not overcommit to a huge server surface before the client side is proven

Potential API direction:

```go
type Server struct { ... }
type ServerModel struct { ... }

func NewServer(model *ServerModel, opts ServerOptions) (*Server, error)
func (s *Server) ListenAndServe(ctx context.Context, ln net.Listener) error
```

Requirements:

- server-side model must be Go-native
- generated config should not mimic libiec61850 `.cfg` internals unless absolutely necessary
- config generation is an adapter concern, not the core data model

## 8. Typing strategy

The library must prefer **named types** over raw strings for important IEC 61850 concepts.

Use named/string-backed types where helpful for logs and interop:

- `ObjectReference`
- `DataSetRef`
- `ReportControlBlockRef`
- `JournalRef`
- `FunctionalConstraint`
- `TriggerOption`
- `QualityFlag`
- `ControlModel`

Use structs when composition matters:

- parsed references
- datasets
- reports
- journal entries
- tree/model nodes

Avoid:

- `map[string]any` as primary API
- giant variant structs without clear meaning
- untyped raw string APIs for core identifiers

## 9. Value model requirements

`go-iec61850` must introduce a semantic value layer above `mms.Value`.

Required semantic types include at minimum:

- boolean
- signed/unsigned numbers
- float
- visible/unicode strings
- octet strings / bit strings
- quality
- timestamp
- entry time / entry ID helpers
- arrays / structures
- control values where needed
- raw fallback wrapper

Recommended shape:

```go
type ValueKind int

type Value struct {
    Kind ValueKind
    Raw  *mms.Value
    ...
}
```

Or typed decode helpers:

```go
func DecodeValue(raw *mms.Value, meta *TypeInfo) (*Value, error)
func DecodeTimestamp(raw *mms.Value) (Timestamp, error)
func DecodeQuality(raw *mms.Value) (Quality, error)
```

Requirements:

- quality and timestamp must be first-class
- raw MMS access must remain available
- semantic decoding must be deterministic and documented
- ownership/copy semantics must be explicit

## 10. Error strategy

The library must have a serious error model.

### Required sentinel categories

Examples:

- `ErrInvalidReference`
- `ErrInvalidFunctionalConstraint`
- `ErrNotFound`
- `ErrTypeMismatch`
- `ErrUnsupportedCDC`
- `ErrUnsupportedService`
- `ErrSubscriptionClosed`
- `ErrSCLParse`
- `ErrModelMismatch`
- `ErrReportDecode`
- `ErrDatasetDecode`

### Required typed errors

Examples:

```go
type ReferenceError struct { ... }
type DecodeError struct { ... }
type ModelError struct { ... }
type ReportError struct { ... }
type SCLParseError struct { ... }
```

### Requirements

- wrap lower-level `go-mms` errors with `%w`
- preserve `errors.Is` / `errors.As`
- cleanly distinguish:
  - local validation error
  - server-side MMS service error
  - transport/association error from `go-mms`
  - semantic IEC decode failure
  - SCL/model inconsistency
- no opaque text-only errors
- no comparing strings

## 11. Logging and observability

The library must integrate with `log/slog`.

### Requirements

- logger injectable via options
- no default noisy stdout logging
- structured fields
- report subscription lifecycle logged at debug/info levels
- browsing/read/write/report events optionally logged
- clear correlation fields where useful:
  - object reference
  - FC
  - dataset
  - RCB
  - report ID
  - reason / trigger
- password/auth secrets must never be logged
- no blocking hooks in critical paths

### Optional but desirable

- debug hooks around semantic decode
- optional raw passthrough visibility from underlying `go-mms`
- future room for metrics/tracing without forcing dependency

## 12. Documentation requirements

Required docs for v0.x:

- serious `README.md`
- package docs
- connection and reference examples
- browsing examples
- report subscription example
- dataset example
- file read example
- journal example
- SCL parsing example
- error handling guide
- observability/logging guide
- compatibility/limitations guide

Every major public type and method must have doc comments.

## 13. Testing requirements

The library must be built with the same discipline as `go-mms`.

### Required test categories

- unit tests for reference parsing
- typed decode/encode tests
- end-to-end tests against a loopback/fake IEC model
- integration tests on top of `go-mms`
- negative tests for malformed references and bad model assumptions
- race/concurrency tests for subscriptions and close behavior
- fuzzing for parsers and decoders
- SCL parse corpus tests
- interop tests against libIEC61850-backed scenarios where practical

### CLI-driven test requirement

The library must make it trivial to test the workflows needed by:

- `discover`
- `find path`
- `find bulk`
- `get object`
- `get report`
- `get ds`
- `get file`
- `get journal`
- `list lds/lns/dos/das/dss/reports/files/journals`
- `subscribe report`
- `tree`
- `scl parse`
- `scl convert`

## 14. Performance and resource requirements

Correctness and ergonomics come first, but the library must still:

- avoid pathological allocation patterns
- support large model traversal without quadratic behavior
- stream file download to writer when requested
- support bounded internal queues for report subscriptions
- expose clear backpressure/drop semantics for subscriptions
- avoid unbounded goroutine spawning

## 15. Compatibility and evolution requirements

Initial release posture should be **v0.x**.

Requirements:

- strong but conservative API
- reserve right to refine model/value APIs before v1.0
- document unstable areas explicitly
- prefer adding capabilities over breaking stable primitives
- keep advanced escape hatches for raw MMS interop

## 16. Explicit CLI-to-library mapping requirements

The API must elegantly support this command structure.

### `discover`

Must be implementable either in this repo or an adjacent utility layer, but the library should expose enough connection and identity primitives to support fast probing.

### `find path`

Needs tree traversal + query filtering.

### `find bulk`

Needs bulk reference parsing + batch reads + stable ordered results.

### `get object`

Needs simple typed read.

### `get file`

Needs file listing/read/download wrappers.

### `get report`

Needs RCB fetch + detailed typed representation.

### `get ds`

Needs dataset fetch and member/value rendering.

### `get journal`

Needs journal list and journal read APIs.

### `list *`

Needs first-class browsing APIs, not CLI-side string hacking.

### `subscribe report`

Needs lifecycle-safe subscription API with GI and cleanup.

### `server generate-config`

Needs SCL/model serialization helpers.

### `server start`

Needs staged server API.

### `tree`

Needs full model build + pretty/serialized output support.

### `scl parse` / `scl convert`

Needs dedicated SCL package.

## 17. Recommended phased implementation order

### Phase 1

- references
- FC model
- basic typed client wrapper over `go-mms`
- list LD/LN/DO/DA
- read one / read many
- tree building

### Phase 2

- datasets
- RCB inspect/set
- report subscribe/unsubscribe/GI
- quality/timestamp semantic decoding

### Phase 3

- files
- journals
- advanced search/bulk helpers
- SCL parse/flatten

### Phase 4

- initial server model/config path
- stronger interop coverage
- docs/examples polish

## 18. Non-negotiable design principles

- IEC 61850 concepts first, MMS internals second
- Go-native API, not C porting
- strong typing over string soup
- explicit errors over magic behavior
- structured logging over printf debugging
- convenience for the 90% case, escape hatch for the 10% case
- clean layering on top of `go-mms`
- CLI ergonomics must be a design input, not an afterthought

## 19. Acceptance criteria for the first serious version

A first serious `go-iec61850` release is ready when it can elegantly support:

- connect
- identify/basic browse
- list LD/LN/DO/DA
- read one reference
- bulk read multiple references
- inspect dataset
- inspect report control block
- subscribe to reports with GI and cleanup
- list/read files
- list/read journals
- parse SCL and flatten to CSV/tree
- provide docs/examples that make `iec61850ctl` straightforward to build
