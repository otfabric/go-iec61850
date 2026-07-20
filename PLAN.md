# PLAN.md — `otfabric/go-iec61850`

## 1. Mission

Build a pure-Go IEC 61850 MMS-only library on top of `go-mms` that delivers:

- a strong public Go API
- excellent support for browsing, reading, datasets, reports, files, journals, and SCL
- a clean foundation for `iec61850ctl`
- enough server-facing model groundwork to grow into a future MMS-backed IEC 61850 server
- a realistic, staged path from client completeness to real runtime server behavior

This plan is written for execution by AI coding agents with human review at each phase gate.

---

## 2. Ground rules for implementation agents

### 2.1 Architectural rules

Agents must respect these rules:

- do not reimplement TPKT, COTP, ISO session/presentation, or MMS
- do not import internal packages from `go-mms`
- use only the public `go-mms` API unless a public-gap review explicitly decides to extend `go-mms`
- keep IEC 61850 semantics in `go-iec61850`, not in `go-mms`
- do not port C code structure one-to-one
- do not expose raw MMS wire structures in the public API
- prefer typed APIs over stringly APIs
- keep server runtime subsystems modular: controls, reports, settings, logs, files, and journals should not collapse into one monolithic server package
- provider interfaces are preferred over hardcoded backends once runtime behavior becomes real

### 2.2 API quality rules

Agents must:

- use `context.Context` on all networked operations
- use option structs instead of wide function signatures
- add doc comments for all exported identifiers
- implement sentinel + typed errors
- use `log/slog` for observability
- keep copy/ownership semantics explicit
- add tests together with code
- not introduce compatibility/deprecation layers unless explicitly requested
- avoid fake atomicity promises for inherently sequential MMS/IEC 61850 operations
- keep lifecycle/state-machine semantics explicit and testable

### 2.3 Delivery rules

For each phase, agents must produce:

- code
- tests
- docs
- a short phase summary in `PROGRESS.md`

No phase is complete without tests and docs.

---

## 3. Recommended repo layout

```text
go-iec61850/
├── README.md
├── REQUIREMENTS.md
├── PLAN.md
├── PROGRESS.md
├── doc.go
├── go.mod
├── go.sum
│
├── client.go
├── server.go
├── options.go
├── errors.go
├── logging.go
│
├── ref.go
├── ref_test.go
├── fc.go
├── fc_test.go
├── values.go
├── values_test.go
├── timestamp.go
├── timestamp_test.go
├── quality.go
├── quality_test.go
│
├── model.go
├── model_test.go
├── browse.go
├── browse_test.go
├── read.go
├── read_test.go
├── write.go
├── write_test.go
├── bulk.go
├── bulk_test.go
│
├── dataset.go
├── dataset_test.go
├── report.go
├── report_test.go
├── file.go
├── file_test.go
├── journal.go
├── journal_test.go
│
├── control.go
├── control_test.go
├── setting_groups.go
├── setting_groups_test.go
│
├── scl/
│   ├── parse.go
│   ├── parse_test.go
│   ├── model.go
│   ├── flatten.go
│   ├── flatten_test.go
│   └── testdata/
│
├── internal/
│   ├── mapping/
│   │   ├── refs.go
│   │   ├── refs_test.go
│   │   ├── names.go
│   │   └── names_test.go
│   ├── decode/
│   │   ├── values.go
│   │   ├── values_test.go
│   │   ├── report.go
│   │   └── report_test.go
│   ├── encode/
│   │   ├── values.go
│   │   └── values_test.go
│   ├── modelcache/
│   │   ├── cache.go
│   │   └── cache_test.go
│   ├── subscription/
│   │   ├── report_sub.go
│   │   └── report_sub_test.go
│   ├── controlrt/
│   │   ├── runtime.go
│   │   └── runtime_test.go
│   ├── reportrt/
│   │   ├── engine.go
│   │   └── engine_test.go
│   ├── journalrt/
│   │   ├── provider.go
│   │   └── provider_test.go
│   └── servermodel/
│       ├── model.go
│       └── model_test.go
│
├── examples/
│   ├── basic-client/
│   ├── browse-tree/
│   ├── reports/
│   ├── files/
│   ├── controls/
│   └── scl-parse/
│
└── interop/
    ├── README.md
    └── ...
```

---

## 4. Milestones overview

| Milestone | Goal | Exit condition |
|---|---|---|
| M0 | Foundation and reference model | refs, FCs, errors, options, client wrapper done |
| M1 | Browse and tree | LD/LN/DO/DA traversal and tree building done |
| M2 | Read/write semantics | single read/write, bulk read/write, value decode baseline done |
| M3 | Datasets and reports | DS inspection plus RCB inspection/subscription done |
| M4 | Files, journals, SCL | file/journal wrappers and SCL package baseline done |
| M5 | Browse/cache/read API completion | caching, tree options, richer find/read completion done |
| M6 | Datasets/reports completion | dynamic datasets and full report lifecycle/completeness done |
| M7 | Files/journals completion | upload symmetry and journal convenience paging done |
| M8 | SCL serious-tooling completion | network/topology-aware parse/validate/export/generation done |
| M9 | Server groundwork | minimal server model and config generation path done |
| M10 | Hardening and release prep | docs, examples, fuzz, interop, cleanup done |
| M11 | Controls | typed control APIs and basic server control execution done |
| M12 | Runtime reports | real URCB/BRCB runtime behavior and report generation done |
| M13 | Setting groups | SGCB model/runtime and client APIs done |
| M14 | Logs and journals runtime | runtime journal/log generation and server journal behavior done |
| M15 | Fuller server services | files/journals/controls/reports integrated behind clean providers |
| M16 | Hardening and conformance | runtime subsystems hardened, race-tested, documented |

---

## 5. Detailed execution phases

# Phase M0 — Foundation and public API skeleton

## Objectives

Create the minimal public skeleton of the library so later phases have stable landing zones.

## Deliverables

- `doc.go`
- `options.go`
- `errors.go`
- `logging.go`
- `client.go`
- `ref.go`
- `fc.go`
- initial tests and package docs

## Tasks

### M0.1 Define options and client wrapper
Implement:

- `DialOptions`
- `ClientOptions`
- `Client`
- `Dial`
- `NewClient`
- `Close`
- `Abort`

Requirements:

- wrap `*mms.Client`
- support direct dial and wrapper mode
- logger defaults to discard/no-op
- no hidden package globals

### M0.2 Define error taxonomy
Implement:

Sentinels:
- `ErrInvalidReference`
- `ErrInvalidFunctionalConstraint`
- `ErrNotFound`
- `ErrTypeMismatch`
- `ErrUnsupportedService`
- `ErrSubscriptionClosed`
- `ErrSCLParse`
- `ErrModelMismatch`

Typed errors:
- `ReferenceError`
- `DecodeError`
- `ModelError`
- `ReportError`
- `SCLParseError`

Requirements:

- use `errors.Is` / `errors.As`
- preserve wrapped `go-mms` errors

### M0.3 Define functional constraints
Implement:

- `FunctionalConstraint` type
- FC constants (`ST`, `MX`, `SP`, `SV`, `CF`, `DC`, `SG`, `SE`, `SR`, `OR`, `BL`, `EX`, `CO`, `RP`, `BR`, etc. as needed)
- parser and validation helpers

### M0.4 Define object reference model
Implement:

- `Ref`
- `ParseRef`
- `MustParseRef` only if really justified
- `String`
- `Parent`
- `Child`
- helpers for LD/LN/path/FC decomposition

Requirements:

- canonical formatting
- strict validation
- typed errors on malformed refs

## Tests

- malformed reference matrix
- FC parse/format tests
- zero-value client option behavior
- error wrapping tests

## Phase exit criteria

- package compiles
- public API skeleton stable enough for M1
- doc comments present
- tests green

---

# Phase M1 — Browse model and tree traversal

## Objectives

Support the CLI browsing workflows cleanly.

## Deliverables

- `model.go`
- `browse.go`
- `internal/mapping/*`
- browse tests
- initial tree example

## Tasks

### M1.1 Define public model structs
Implement public types:

- `LogicalDevice`
- `LogicalNode`
- `DataObject`
- `DataAttribute`
- `ModelTree`
- `TreeNode`
- `FindQuery`

Keep them ergonomic and serializable.

### M1.2 Implement MMS→IEC name mapping
Inside `internal/mapping`, implement translation between:

- MMS domains/item IDs
- IEC 61850 LD/LN/DO/DA references
- FC-qualified references where applicable

This layer is critical and must stay internal.

### M1.3 Implement browse APIs
Implement:

- `ListLogicalDevices`
- `ListLogicalNodes`
- `ListDataObjects`
- `ListDataAttributes`
- `Tree`
- `FindPaths`

Requirements:

- internally use `go-mms` GetNameList/GetVariableAccessAttributes as appropriate
- continuation handled internally
- stable order where possible
- clean distinction between object-not-found and unsupported browse cases

### M1.4 Add model cache scaffolding
Implement optional internal cache package:

- type metadata cache
- browse result cache
- invalidation strategy can be conservative

Do not overcomplicate in first pass.

## Tests

- loopback/fake server browse tests
- mapping tests for representative IEC names
- tree-building tests
- find-path tests with prefix and pattern matching

## Phase exit criteria

- `list lds`, `list lns`, `list dos`, `list das`, `tree`, `find path` all have obvious library equivalents
- browse output is deterministic
- tests green

---

# Phase M2 — Values, read, write, and bulk operations

## Objectives

Make reading and writing IEC 61850 values ergonomic and typed.

## Deliverables

- `values.go`
- `timestamp.go`
- `quality.go`
- `read.go`
- `write.go`
- `bulk.go`
- decode/encode internal helpers

## Tasks

### M2.1 Define semantic value model
Implement public `Value` model above `mms.Value`.

Must support at minimum:

- bool
- int/uint
- float
- strings
- octet string / bit string
- arrays / structures
- quality
- timestamp
- raw fallback

Decide whether to use:
- tagged union style `Value`, or
- typed decode helpers plus lightweight wrappers

Choose one and stay consistent.

### M2.2 Implement timestamp support
Implement IEC 61850 timestamp type support suitable for:

- timestamp value
- fractional precision if present
- time quality / flags if present
- round-trip string/debug formatting

### M2.3 Implement quality support
Implement:

- quality bitfield type
- named flag helpers
- string/debug formatting
- bitfield decode/encode helpers

### M2.4 Implement read APIs
Implement:

- `Read`
- `ReadRaw`
- `ReadMany`
- `ReadInto`

Requirements:

- preserve order in bulk reads
- explicit per-item status in `ReadMany`
- no silent type coercion surprises

### M2.5 Implement write APIs
Implement:

- `Write`
- `WriteValue`
- `WriteMany`

Requirements:

- preflight validation where possible
- partial-result type for bulk writes

### M2.6 Implement semantic decode pipeline
Internal decode path must:

- take raw `mms.Value`
- optionally use model/type metadata
- produce IEC semantic value representation
- return typed decode errors when semantics are unknown or unsupported

## Tests

- value decode tests
- quality decode tests
- timestamp decode tests
- read/write end-to-end tests
- bulk partial-success tests
- ownership/copy tests

## Phase exit criteria

- `get object` and `find bulk` are naturally supported
- timestamp and quality are first-class
- tests green

---

# Phase M3 — Data sets and reports

## Objectives

Implement the features that most clearly differentiate IEC 61850 semantics from generic MMS.

## Deliverables

- `dataset.go`
- `report.go`
- `internal/subscription/*`
- examples for RCB inspection and subscription

## Tasks

### M3.1 Implement dataset model
Implement public types:

- `DataSetRef`
- `DataSet`
- `DataSetMember`
- `DataSetValues`

Implement:

- `ListDataSets`
- `GetDataSet`
- `ReadDataSet`

Requirements:

- typed member refs
- clear separation of metadata and value readout

### M3.2 Implement report control block model
Implement public types:

- `ReportControlBlockRef`
- `ReportControlBlock`
- `ReportFieldMask`
- `TriggerOptions`
- `ReportIndication`
- `ReportReason`

### M3.3 Implement RCB browse/read/write
Implement:

- `ListReports`
- `GetReportControlBlock`
- `SetReportControlBlock`

Mask-based mutation is recommended to avoid accidental partial writes.

### M3.4 Implement subscription engine
Implement:

- `SubscribeReport`
- `ReportSubscription.Close`

Internal responsibilities:

- configure/enable RCB
- optionally reserve/report-owner if needed by design
- GI support
- receive InformationReports via `go-mms`
- decode payloads into typed `ReportIndication`
- bounded queue / backpressure policy
- deterministic cleanup on context cancellation and client close

### M3.5 Cleanup guarantees
Define and test:

- unsubscribe behavior
- disable behavior
- behavior on broken connection
- handler panic isolation
- duplicate close safety

## Tests

- dataset listing and fetch tests
- RCB inspection tests
- subscription tests
- GI tests
- cleanup tests
- race tests
- queue overflow/backpressure tests

## Phase exit criteria

- `get ds`, `get report`, `list reports`, `subscribe report` all have solid library support
- close/cancel semantics are deterministic
- tests green

---

# Phase M4 — Files, journals, and SCL baseline

## Objectives

Finish the big remaining CLI-facing features.

## Deliverables

- `file.go`
- `journal.go`
- `scl/parse.go`
- `scl/model.go`
- `scl/flatten.go`
- examples and docs

## Tasks

### M4.1 File wrappers
Implement:

- `ListFiles`
- `ReadFile`
- `DownloadFile`

Requirements:

- stream to `io.Writer`
- hide FRSM complexity
- preserve lower-level escape hatch only if needed later

### M4.2 Journal wrappers
Implement:

- `ListJournals`
- `ReadJournal`

Public model should include:

- journal reference
- entry list
- time metadata
- payload fields if available/decodable

### M4.3 SCL model and parser
Implement `scl` package:

- parse XML
- build Go-native model
- expose LD/LN/DO/DA structure
- capture enough metadata to assist runtime validation and CLI views

### M4.4 SCL flatten/export helpers
Implement:

- flatten to rows
- CSV-friendly output structs
- tree rendering helpers

## Tests

- file wrapper tests
- journal wrapper tests
- SCL fixture parse tests
- flatten/export tests

## Phase exit criteria

- `get file`, `list files`, `get journal`, `list journals`, `scl parse`, `scl convert` all have clear library support
- tests green

---

# Phase M5 — Browse, cache, tree, and read API completion

#### Goals

Complete browse/read-side missing API work:
- caching
- tree options
- FC-aware browse
- richer path search
- clean component reads
- explicit LN-level read support
- mixed-domain write support documentation/tests

---

### M5.1 Client-side cache system

#### Requirements

Implement client-side caching for discovered IEC 61850 model information to reduce server load.

Supported strategies must be:
1. No cache
   - default behavior
   - every browse/read-discovery call hits server
2. Explicit refresh/invalidate
   - cached results retained until caller refreshes or invalidates
   - caller-controlled lifecycle
3. Lazy transparent cache
   - first access loads data
   - later accesses reuse cache automatically
   - explicit invalidation/refresh should still be possible

#### Scope

Cacheable data should include at least:
- logical devices
- logical nodes
- data objects / data attributes discovery
- tree results
- optionally dataset definitions (GetDataSet) as part of same cache framework

#### Public API requirements

Add cache-related options and methods in a clean client-side form.

Must support:
- choose caching strategy in client options
- explicit refresh of:
  - full model cache
  - per-LD cache
  - dataset definition cache if included
- explicit invalidation of:
  - full model cache
  - per-LD cache
  - dataset definition cache if included

#### Constraints
- Default remains no cache
- API must not force caching
- Cache behavior must be documented clearly
- Cache correctness matters more than micro-optimization

---

### M5.2 Tree options

#### Requirements

Extend Tree() to support options.

Must support:
- LD filter
- max depth
- include FCs

#### Public API expectation

Implement a new options-based tree API, either:
- Tree(ctx, opts) or
- keep Tree(ctx) and add TreeWithOptions(ctx, opts)

Need clean public options struct.

#### Semantics
- root node remains synthetic unless explicitly changed
- max depth must align with documented Ref.Depth()
- include FCs should trigger FC-aware node annotation

---

### M5.3 FC annotation on tree nodes

#### Requirements

Allow FC information to be attached to tree nodes.

This must be optional from public API perspective.

#### Expected behavior

When enabled:
- nodes should expose FC information where known
- when multiple FCs apply, represent that clearly
- avoid misleading single-FC assignment on ambiguous nodes

#### Design note

Do not break existing tree usage.
Prefer extending model node semantics cleanly rather than replacing them.

---

### M5.4 FindPaths richer matching

#### Requirements

Extend FindPaths to support:
- current glob matching
- regex matching

#### Public API

Add explicit match mode, do not rely on guesswork.

Example direction:
- MatchModeGlob
- MatchModeRegex

#### Constraints
- invalid patterns must fail clearly
- behavior must be deterministic and documented
- preserve current glob support

---

### M5.5 ReadComponent API

#### Requirements

Add a clean IEC-61850-native component read API.

This is required now.

#### Goal

Allow reading a component of a structure without forcing callers to drop down to go-mms.

#### Public API

Implement a dedicated public API such as:
- ReadComponent(ctx, ref, component)
or equivalent IEC-61850-native design.

#### Constraints
- keep API go-mms agnostic
- validate refs clearly
- document how component names relate to structure fields / DA paths

---

### M5.6 LN-level read semantics

#### Decision

Officially support LN-level refs for reads.

#### Requirements
- LN-level reads are valid and supported
- if needed, expose dedicated public API for LN-level structure reads
- document behavior clearly

#### Constraints
- do not leave this as accidental behavior
- make contract explicit in docs/tests

---

### M5.7 Mixed-domain WriteMultiple

#### Decision

Officially support mixed-domain writes.

#### Requirements
- document mixed-domain writes as supported
- ensure tests explicitly cover it
- preserve order / per-item semantics

---

### M5 non-goals

Do not prioritize in this phase:
- generic app-level ReadInto(interface{})
- generic app-level WriteValue(interface{})

These are not core requirements for this library at this stage.

---

# Phase M6 — Datasets and reports completion

#### Goals

Make datasets and reports functionally complete enough before server work:
- dynamic datasets
- lifecycle-aware subscriptions
- GI
- URCB ownership
- full timestamp support
- segmented report reassembly
- configurable overflow policy
- richer subscription matching

---

### M6.1 Dynamic datasets

#### Requirements

Implement:
- CreateDataSet
- DeleteDataSet

#### Scope

Support dynamic dataset lifecycle before server phase.

#### Constraints
- public API should be IEC 61850 oriented
- validate names/members clearly
- return useful errors
- add loopback/integration tests

---

### M6.2 Subscription lifecycle-awareness

#### Decision

SubscribeReport should become lifecycle-aware.

#### Requirements

Support flows such as:
- configure RCB without subscribing
- configure + enable + subscribe
- subscribe with managed lifecycle behavior

#### Needed capabilities

A user must still be able to:
- create/configure RCB without auto-subscription
- subscribe passively if desired
- opt into lifecycle-aware behavior

#### Public API

Likely add subscription options that control:
- auto-enable
- auto-configure
- GI on subscribe
- URCB reserve handling where relevant

---

### M6.3 First-class GI support

#### Requirements

Add first-class GI support before server phase.

#### Scope

Support GI as explicit API behavior, not ad-hoc field writes only.

Possible API directions:
- TriggerGI(...)
- GI option in subscription/configuration flow

#### Constraints
- make semantics clear for BRCB/URCB
- document ordering with enable/config

---

### M6.4 URCB reserve / owner semantics

#### Requirements

Implement robust URCB ownership semantics.

Must support:
- reserve handling
- owner-related fields/flows
- meaningful API behavior for claim/release/use patterns

#### Constraints
- should be explicit and testable
- must not rely on undocumented side effects

---

### M6.5 Full report timestamp decoding

#### Dependency status

This is now feasible because go-mms v0.1.1 added UTCTime quality-byte support and related timestamp improvements.

#### Requirements

Implement full report timestamp decode support using current go-mms.

#### Scope
- decode report timestamps fully
- include quality/precision semantics where available
- update report indication model if needed

---

### M6.6 Segmented report reassembly

#### Requirements

Implement segmented report reassembly before server phase.

#### Scope
- detect segmented reports
- buffer/reassemble segments
- deliver complete logical report to subscribers
- handle error/timeout/reset cases clearly

#### Constraints
- preserve order/correctness
- document partial/failed reassembly behavior

---

### M6.7 Queue overflow policy

#### Requirements

Current fixed behavior (drop + warn) is not enough.

Expose configurable overflow policy.

#### Must support policy selection

At minimum:
- drop newest
- drop oldest
- block
- callback/hook on overflow

Exact names may differ, but policy must be public and configurable.

#### Constraints
- default should remain safe and documented
- implementation must not introduce send-on-closed-channel races

---

### M6.8 SubscribeReport matching modes

#### Decision

Support:
- exact RptID
- glob/pattern matching

#### Requirements

Subscription API must support more than exact match.

Recommend explicit match mode/options, not magical detection.

---

### M6.9 Sequential SetReportControlBlock semantics

#### Decision

Keep sequential per-field writes, document clearly.

#### Requirements
- no fake atomicity promises
- document write ordering guarantees
- preserve special ordering such as RptEna last when needed

---

### M6.10 Dataset definition caching

#### Decision

Fold into broader cache system from M5.

#### Requirements

If browse/model caching exists, dataset definition caching should integrate with the same strategy set:
- no cache
- explicit refresh/invalidate
- lazy transparent cache

---

### M6 non-goals

Still do not prioritize:
- generic dataset ReadInto(interface{})
- app-level arbitrary struct binding

---

# Phase M7 — Files and journals completion

#### Goals

Complete missing file/journal client features before server work:
- file upload symmetry
- journal convenience pagination

---

### M7.1 File upload support

#### Requirements

Implement file upload support before server.

Support deferred file operations:
- ObtainFile
- SetFile
or whichever exact operations map correctly to go-mms / MMS file service

#### Scope

Public API should mirror existing file read/download/delete quality.

#### Constraints
- stream where appropriate
- avoid unnecessary full-memory buffering for large files
- validate names/inputs clearly
- test with in-memory provider loopback

---

### M7.2 Journal auto-pagination helper

#### Requirements

Add convenience journal pagination helper now.

#### Scope

Current low-level paging stays.
Add higher-level helper that follows MoreFollows automatically.

#### Possible API direction

A method like:
- ReadJournalAll(...)
- ReadJournalAfterAll(...)
or equivalent

#### Constraints
- preserve order
- handle repeated same-timestamp cursor semantics correctly
- document stop conditions

---

### M7 non-goals
- download progress callback is not needed now

---

# Phase M8 — SCL serious-tooling completion

#### Goals

Elevate `scl/` from baseline parser to serious tooling:
- network-aware parsing
- topology parsing
- semantic validation
- services parsing
- lookup helpers
- round-trip / edit / generation
- richer flatten/export support

---

### M8.1 SCL Communication parsing

#### Requirements

Implement Communication section parsing.

Must include network-aware details such as:
- GSE / SMV addressing
- connected AP/network information
- relevant communication parameters

#### Goal

SCL must become network-aware, not just logical-model aware.

---

### M8.2 SCL Substation parsing

#### Requirements

Implement Substation section parsing.

Reason:

Topology matters now and is required before server phase.

#### Scope

Parse enough structure to represent plant/substation topology meaningfully.

---

### M8.3 SCL semantic validation

#### Requirements

Implement validation beyond basic required-field parsing.

Must include:
- cross-reference validation
- type reference validation
- logical consistency checks where feasible

#### Examples
- referenced types exist
- LNType/DOType/DAType/EnumType chains are valid
- dataset/report references resolve
- communication/topology references resolve where applicable

#### Deliverable

Provide validation API, likely returning a list of structured validation findings rather than only fail-fast parsing errors.

---

### M8.4 SCL round-trip / generation

#### Requirements

Both are needed:
- config edit
- config generation

So SCL must become more than read-only.

#### Scope

Add ability to:
- preserve/edit model
- write model back to XML
- generate valid SCL structures programmatically

#### Constraints
- output should be deterministic
- round-trip loss should be minimized
- may start with supported subset if documented clearly

---

### M8.5 SCL Services parsing

#### Requirements

Implement Services section parsing before server phase.

Reason:

Needed now.

---

### M8.6 Flatten scope

#### Decision

Need both:
- current leaf-DA flattening
- separate export helpers for datasets/reports

#### Requirements

Keep Flatten focused on leaf data attributes if that remains the clean core behavior, but implement additional export helpers for:
- datasets
- reports

These should be separate and explicit.

---

### M8.7 SCL lookup helpers

#### Requirements

Add lookup/navigation helpers before server phase.

Examples:
- FindIED
- FindLDevice
- FindLN
- FindLNodeType
- FindDOType
- FindDAType
- FindEnumType

#### Constraints
- helpers should be small, direct, and deterministic
- avoid overbuilding a query language

---

### M8 non-goals
- none of the earlier deferred SCL items remain optional now, except UI niceties like progress callbacks which do not belong here

---

## Explicit decisions already made

These should be treated as settled requirements, not open questions.

### Settled decisions
1. Caching is required
   - client-side
   - support all 3 strategies:
     - no cache (default)
     - explicit refresh/invalidate
     - lazy transparent cache
2. FC info on tree nodes is required
   - optional from public API perspective
3. Tree options are required
   - LD filter
   - max depth
   - include FCs
4. Regex support in FindPaths is required
   - in addition to glob
5. ReadInto(interface{}) / WriteValue(interface{}) are not current priorities
   - unless they map to strong IEC 61850 semantic typing
   - do not invent app-level generic object binding just to fill the gap
6. Timestamp quality support is now possible
   - must use newer go-mms support
7. ReadComponent is required now
8. LN-level reads are officially supported
   - may also expose dedicated API if useful
9. Mixed-domain WriteMultiple is officially supported
10. Dynamic dataset create/delete is required before server
11. Subscription should become lifecycle-aware
   - but configuring/using RCB without auto-subscription must remain possible
12. First-class GI support is required before server
13. URCB reserve/owner semantics are required
14. Full report timestamp decoding is required
15. Segmented report reassembly is required
16. Queue overflow policy must be exposed
17. SubscribeReport must support exact + glob/pattern matching
18. SetReportControlBlock stays sequential per-field and must be documented clearly
19. Dataset caching follows the broader cache design
20. File upload is required before server
21. Journal auto-pagination helper is required now
22. SCL must become network-aware
   - Communication parsing required
23. SCL topology matters now
   - Substation parsing required
24. SCL semantic validation is required now
25. SCL round-trip / generation is required
   - edit/generation is now a goal
26. SCL Services parsing is required now
27. Flatten + separate dataset/report exports are both required
28. SCL lookup helpers are required now
29. Download progress callback is not needed now
30. GOOSE, SV/SMV runtime protocol support, and richer dynamic model builders are explicitly out of scope for this plan
31. Controls, real runtime server behavior, setting groups, and fuller server services are in scope for the next server-focused phase set

---

## Recommended implementation order

### Phase M5
1. cache framework
2. tree options
3. FC annotation
4. regex find
5. ReadComponent
6. LN-level read contract/docs/tests
7. mixed-domain write docs/tests

### Phase M6
1. dynamic datasets
2. lifecycle-aware subscriptions
3. GI support
4. URCB reserve/owner
5. full report timestamp decode
6. segmented report reassembly
7. queue overflow policies
8. glob/pattern subscription matching
9. documentation tightening

### Phase M7
1. file upload
2. journal auto-pagination
3. polish file/journal tests and docs

### Phase M8
1. SCL Communication parsing
2. SCL Substation parsing
3. SCL semantic validation
4. SCL lookup helpers
5. dataset/report export helpers
6. SCL Services parsing
7. SCL round-trip / generation

---

## Deliverable expectations for AI Agent

For each phase, the AI agent must produce:
1. Updated implementation
   - code
   - docs
   - tests
2. Phase summary
   - files added/changed
   - APIs added/changed
   - design decisions
   - deferred items still remaining
3. Hardening pass
   - nil-safety
   - concurrency/race review
   - deterministic behavior review
   - error taxonomy consistency
   - loopback/integration tests where possible
4. No silent semantic changes
   - if public behavior changes, document it explicitly

---

## Important constraints for AI Agent
- Keep the public API clean and IEC 61850-native
- Do not add random abstraction layers
- Do not invent application-level object mapping just because ReadInto(interface{}) was mentioned
- Preserve strong semantic typing at MMS / IEC 61850 layer
- Prefer explicit options structs over ambiguous overloads
- Tests must be added for every meaningful new semantic path
- If a dependency on go-mms exists, use the current released capability before inventing workarounds

---

## Final target before server phase

Before starting server-side work, the library should have:
- complete browse/model client support with optional caching
- complete read/write/component access semantics
- complete dataset management
- robust report lifecycle handling
- file upload/download/delete completeness
- journal paging convenience
- serious SCL tooling:
  - parse
  - inspect
  - validate
  - export
  - edit/generate
  - network/topology aware

If you want, I can also convert this into a compact `AGENT_TASKS.md` checklist version with explicit acceptance criteria per phase.

---

# Phase M9 — Server groundwork and config generation path

## Objectives

Create enough server-side model to support future config generation and staged server startup work.

## Deliverables

- `server.go`
- `internal/servermodel/*`
- initial config-generation structures
- docs describing stability level

## Tasks

### M9.1 Define server-side model
Implement Go-native server model types:

- logical devices
- logical nodes
- DO/DA values
- datasets
- reports
- files/journals hooks as appropriate

Do not mimic libiec61850 `.cfg` layout internally.

### M9.2 Server constructor scaffolding
Implement conservative staged APIs, for example:

- `NewServer`
- `ServerOptions`
- placeholder or minimal listen/serve path if practical

### M9.3 Config generation adapter
Implement an adapter layer that can generate config/artifacts for the CLI flow.

Treat generation as adapter/output, not canonical internal representation.

## Tests

- model validation tests
- config generation tests
- serialization tests

## Phase exit criteria

- enough groundwork exists for `iec61850ctl server generate-config`
- no overdesigned server API
- tests green

---

# Phase M10 — Hardening, docs, examples, interop, and release prep

## Objectives

Turn the implementation into a serious repo.

## Deliverables

- polished README
- examples
- compatibility/limitations docs
- interop scaffolding
- fuzz targets
- release checklist

## Tasks

### M10.1 Documentation pass
Create/update:

- `README.md`
- package docs
- `KNOWN_LIMITATIONS.md`
- `ERRORS.md`
- `OBSERVABILITY.md`

### M10.2 Examples
Implement examples:

- basic connect + identify
- browse tree
- read values
- reports subscription
- file read
- SCL parse

### M10.3 Fuzzing
Add fuzz targets for:

- reference parser
- FC parser
- value decode
- report decode
- SCL parser

### M10.4 Interop
Add interop scaffolding against a C/libiec61850-backed environment where feasible.

### M10.5 API review
Audit exported API:

- naming
- doc sharpness
- unnecessary exposure
- ergonomic rough edges revealed by early `iec61850ctl` work

## Phase exit criteria

- docs/examples are solid
- fuzz targets exist
- API feels stable enough for v0.x
- release checklist prepared

---

# Phase M11 — Controls

## Objectives

Add a proper IEC 61850 control layer on top of the existing read/write core without leaking raw MMS details into user code.

## Deliverables

- `control.go`
- `control_types.go` or equivalent
- `control_errors.go` or equivalent
- control examples
- initial server-side control hooks

## Tasks

### M11.1 Client-side typed control APIs
Implement typed client flows for:

- direct operate
- select-before-operate
- cancel
- command result/termination handling
- last-appl-error read/decode where applicable

### M11.2 Control model types
Implement semantic types for at least:

- `ctlModel`
- `ctlVal`
- `operTm`
- `origin`
- `ctlNum`
- test/check bits and related control metadata

### M11.3 Server-side control execution hooks
Add server control runtime hooks for:

- select handler
- operate handler
- cancel handler
- validation/interlock/synchro-check style hooks
- timeout/reservation tracking

### M11.4 Error and state semantics
Define typed errors and explicit state transitions for:

- select denied
- operate denied
- stale select
- cancel failure
- command completion / command timeout

## Tests

- select/operate/cancel lifecycle tests
- direct-operate tests
- rejection/error mapping tests
- Go client ↔ Go server control integration tests

## Phase exit criteria

- control operations are first-class typed APIs, not generic writes on CO paths
- select/operate/cancel works against the Go server model
- control failures are reported with meaningful typed errors
- tests green

---

# Phase M12 — Runtime reports

## Objectives

Turn current report groundwork into a real runtime report engine.

## Deliverables

- runtime report engine
- RCB state manager
- buffered queue implementation
- change-detection path tied to server value mutation
- runtime report docs/tests

## Tasks

### M12.1 Real URCB/BRCB runtime behavior
Implement actual runtime behavior for:

- reserve / enable / disable
- URCB ownership semantics
- BRCB vs URCB differences
- configuration revision handling

### M12.2 Report generation triggers
Generate runtime reports on:

- data change
- quality change
- data update
- integrity period
- GI

### M12.3 Buffered report behavior
Implement buffered runtime state including:

- entry ID
- time of entry
- sequence handling
- overflow behavior
- queue retention semantics within defined scope

### M12.4 Dataset binding/runtime resolution
Ensure reports resolve and use runtime dataset bindings cleanly and deterministically.

## Tests

- data-change trigger tests
- GI report tests
- integrity-period tests
- BRCB buffer behavior tests
- enable/disable/reserve lifecycle tests
- end-to-end delivery tests

## Phase exit criteria

- value changes on the server can produce real report delivery
- GI and integrity work in runtime, not just pre-server scaffolding
- buffered/unbuffered semantics are clearly separated and tested
- tests green

---

# Phase M13 — Setting groups

## Objectives

Implement useful SGCB/setting-group behavior for real IEC 61850 applications.

## Deliverables

- `setting_groups.go`
- SGCB runtime model
- client APIs for setting-group inspection/use
- tests/docs/examples

## Tasks

### M13.1 Client-side setting group APIs
Implement APIs to:

- inspect active/edit group state
- select an edit group
- read settings from the relevant group
- commit/activate group changes

### M13.2 Server-side SGCB runtime
Implement runtime state for:

- active group
- edit group
- edit session lifecycle
- commit/activate semantics

### M13.3 Validation hooks
Add hooks for:

- edit conflict detection
- invalid group handling
- activation refusal
- server-side validation before commit

## Tests

- active/edit group tests
- commit/activate tests
- conflict/error tests
- Go client ↔ Go server SGCB integration tests

## Phase exit criteria

- setting groups are explicit semantics, not generic CF/SE writes only
- client and server behavior is deterministic and testable
- tests green

---

# Phase M14 — Logs and journals runtime

## Objectives

Move from journal-reading support to fuller runtime log/journal behavior on the server side.

## Deliverables

- journal/log provider abstraction
- in-memory provider
- runtime journal generation path
- server journal integration tests/docs

## Tasks

### M14.1 Journal provider abstraction
Define a provider/storage abstraction for server-side log/journal behavior.

### M14.2 Runtime log entry generation
Implement creation of journal/log entries from runtime events such as:

- controls
- report-worthy value changes where desired
- application/server events as designed

### M14.3 Server journal exposure
Wire journal provider behavior into MMS-exposed journal services with correct cursor/pagination semantics.

### M14.4 Cursor correctness
Guarantee no-duplicate/no-skip paging behavior, especially for same-timestamp entry sequences.

## Tests

- runtime journal creation tests
- paging/cursor tests
- provider abstraction tests
- Go client ↔ Go server journal tests

## Phase exit criteria

- Go server can produce runtime journal entries
- client journal APIs work cleanly against the Go server
- pagination semantics are deterministic and tested

---

# Phase M15 — Fuller server services

## Objectives

Complete the missing server-side service surface around the experimental server.

## Deliverables

- provider interfaces for runtime subsystems
- cleaner service registration/configuration
- file/journal/control/report integration examples
- updated server docs

## Tasks

### M15.1 Provider-oriented server wiring
Refactor or extend server runtime so that files, journals, controls, and reports are attached through focused providers/interfaces rather than hardcoded behavior.

### M15.2 File service integration
Integrate file-service provider behavior cleanly with the server runtime.

### M15.3 Identify/status/session hooks
Add practical server hooks for:

- identify/status behavior
- session/connection lifecycle if supported
- capability/service exposure based on actual implementation

### M15.4 Error/service mapping cleanup
Ensure unsupported-service and service-state behavior is explicit, typed, and consistent with actual runtime support.

## Tests

- provider integration tests
- service-capability tests
- file/journal/control/report server integration tests

## Phase exit criteria

- server runtime is modular and no longer just groundwork
- capability exposure matches implemented behavior
- tests green

---

# Phase M16 — Hardening, conformance, and runtime stabilization

## Objectives

Stabilize the new runtime server behavior before further expansion.

## Deliverables

- race/deadlock review
- failure-injection tests
- expanded interop matrix
- updated examples/docs for runtime server behavior
- conformance-style checklist

## Tasks

### M16.1 Concurrency audit
Review and harden:

- control runtime
- report runtime
- subscription cleanup
- journal/log providers
- shutdown/abort/lifecycle interactions

### M16.2 Failure-injection tests
Add tests for:

- connection loss
- partial cleanup failure
- queue overflow
- invalid server callbacks
- state-machine misuse

### M16.3 Interop/conformance pass
Add broader integration coverage against available clients/servers where feasible.

### M16.4 Documentation refresh
Update README/examples/docs to reflect the intended stable runtime API, not just experimental internals.

## Phase exit criteria

- race tests are clean
- runtime shutdown and cleanup are deterministic
- major server state machines are documented and tested
- repo is ready for a stronger runtime-focused release candidate

---

## 6. AI-agent work packets

These are compact packets that can be assigned sequentially.

### Packet P1 — Foundation
Files:
- `doc.go`
- `options.go`
- `errors.go`
- `logging.go`
- `client.go`
- `fc.go`
- `ref.go`

Output:
- compile-ready skeleton
- tests for refs/errors/FC parsing

### Packet P2 — Browse
Files:
- `model.go`
- `browse.go`
- `internal/mapping/*`

Output:
- list/tree/find support
- browse tests

### Packet P3 — Value layer
Files:
- `values.go`
- `timestamp.go`
- `quality.go`
- `internal/decode/*`
- `internal/encode/*`

Output:
- semantic value model
- decode tests

### Packet P4 — Read/write/bulk
Files:
- `read.go`
- `write.go`
- `bulk.go`

Output:
- single and batch operations
- end-to-end tests

### Packet P5 — Datasets
Files:
- `dataset.go`

Output:
- list/get/read dataset support
- tests

### Packet P6 — Reports
Files:
- `report.go`
- `internal/subscription/*`

Output:
- RCB inspect/set
- subscribe/unsubscribe/GI
- race tests

### Packet P7 — Files and journals
Files:
- `file.go`
- `journal.go`

Output:
- wrapper APIs
- tests

### Packet P8 — SCL
Files:
- `scl/*`

Output:
- parser + model + flatten
- test fixtures

### Packet P9 — Pre-server browse/cache/read
Files:
- browse/model cache files
- tree option extensions
- `read.go` / related browse/read APIs

Output:
- cache strategies
- tree options + optional FC annotation
- regex find + component/LN-level read polish

### Packet P10 — Pre-server datasets/reports
Files:
- `dataset.go`
- `report.go`
- subscription internals

Output:
- dynamic datasets
- lifecycle-aware subscriptions
- GI / URCB / segmentation / overflow policy completion

### Packet P11 — Pre-server files/journals
Files:
- `file.go`
- `journal.go`

Output:
- upload symmetry
- journal auto-pagination helpers

### Packet P12 — Pre-server SCL
Files:
- `scl/*`

Output:
- communication/substation/services parsing
- validation / lookup / export / generation

### Packet P13 — Server groundwork
Files:
- `server.go`
- `internal/servermodel/*`

Output:
- minimal server model/config path
- tests

### Packet P14 — Hardening and release prep
Files:
- docs/examples/fuzz/interop materials

Output:
- polished repo

### Packet P15 — Controls
Files:
- `control.go`
- control runtime internals
- control docs/examples/tests

Output:
- typed control API
- select/operate/cancel flows
- server control execution hooks

### Packet P16 — Runtime reports
Files:
- report runtime internals
- server report state/runtime wiring

Output:
- real URCB/BRCB runtime behavior
- report generation engine
- runtime report tests

### Packet P17 — Setting groups
Files:
- `setting_groups.go`
- SGCB runtime internals

Output:
- setting-group APIs and runtime support
- tests

### Packet P18 — Logs and journals runtime
Files:
- journal/log runtime internals
- provider abstractions

Output:
- runtime journal generation
- server journal runtime tests

### Packet P19 — Fuller server services
Files:
- `server.go`
- provider integrations
- server service docs/tests

Output:
- modular runtime server service surface
- integrated files/journals/controls/reports

### Packet P20 — Runtime hardening/conformance
Files:
- docs/examples/interop/failure-injection materials

Output:
- stabilized runtime server behavior
- conformance-oriented test and doc pass

---

## 7. Definition of done per phase

A phase is done only when all are true:

- code builds
- tests pass
- race-sensitive code has race tests where appropriate
- exported identifiers have doc comments
- README or phase docs updated if user-facing behavior changed
- `PROGRESS.md` updated with:
  - what was built
  - what was deferred
  - notable design decisions
  - open questions

---

## 8. Suggested implementation order for fastest value

If you want the fastest path to a useful `iec61850ctl`, do the milestones in this exact order:

1. M0 Foundation
2. M1 Browse
3. M2 Read/write/value layer
4. M3 Reports + datasets
5. M4 Files/journals/SCL
6. M5 Pre-server browse/cache/read
7. M6 Pre-server datasets/reports
8. M7 Pre-server files/journals
9. M8 Pre-server SCL serious tooling
10. M9 Server groundwork
11. M10 Hardening/docs/examples
12. M11 Controls
13. M12 Runtime reports
14. M15 Fuller server services
15. M13 Setting groups
16. M14 Logs/journals runtime
17. M16 Hardening/conformance

Reason:
- client workflows deliver immediate value and validate the API
- report support is core for IEC 61850 usage
- server groundwork should come before the deeper runtime server phases
- controls and runtime reports are the next highest-value functional gaps after groundwork
- fuller server services should be shaped after controls/report runtime exist
- setting groups and logs/journals runtime come after the foundational runtime server pieces

---

## 9. Immediate next action

Start with **Packet P1 — Foundation** and require the agent to produce:

- `go.mod` setup if needed
- `doc.go`
- `options.go`
- `errors.go`
- `logging.go`
- `client.go`
- `fc.go`
- `ref.go`
- `ref_test.go`
- `fc_test.go`
- `errors_test.go`

along with a short `PROGRESS.md` entry explaining API decisions.
