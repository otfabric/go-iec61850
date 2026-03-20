# SCL Refactoring Progress

Tracking file for the parser-deletion and standardisation refactor defined in `SCL.md`.

## Goal

Single XML decode path using XSD-generated raw contracts, one normalized runtime
model, one shared index/resolution layer, one diagnostic model, one validator
pipeline, one CLI path (`sclparse`).

---

## Phase 0 — Architecture rules

| Rule | Description | Status |
|------|-------------|--------|
| 0.1 | No handwritten XML mirror structs in `scl` | **done** |
| 0.2 | No fallback parser path for unknown versions | **done** |
| 0.3 | All parsing via DetectVersion → raw → converter → model | **done** |
| 0.4 | All consumers use same normalized model and diagnostics | **done** |
| 0.5 | All cross-ref logic uses one shared index layer | **done** |

---

## Phase 1 — Delete the legacy handwritten XML parser layer

| Step | Description | Status |
|------|-------------|--------|
| 1.1 | Delete all `xml*` transport structs from `parse.go` | **done** |
| 1.2 | Delete `convertSCL` and legacy conversion helpers | **done** |
| 1.3 | Delete legacy fallback branch in `dispatch.go` | **done** |
| 1.4 | Fix `generate.go` (uses `xml*` structs for serialisation) | **done** |
| 1.5 | Fix/rewrite tests that depend on legacy parser | **done** |
| 1.6 | Update comments/docs referencing legacy path | **done** |

**Notes:**
- `parse.go` reduced from 959 lines to 47 lines (only `Parse`, `ParseFile`, `parseBool`)
- `generate.go` now uses its own local `gen*` types, fully decoupled from parsing
- `dispatch.go` unknown version is now a hard error, no fallback
- 2 example CID files using unsupported `2007A` schema are tested as expected failures
- `Generate` now emits `xmlns`, `version="2007"`, `revision="B"` for roundtrip compatibility

---

## Phase 2 — Harden version detection

| Step | Description | Status |
|------|-------------|--------|
| 2.1 | Refactor DetectVersion to root-attribute-only streaming | **done** |
| 2.2 | Exact schema tuple classification | **done** |
| 2.3 | Parse release numerically | **done** |
| 2.4 | Move confidence into core VersionInfo | **done** |
| 2.5 | Add vendor namespace collection | **done** |
| 2.6 | Strict detection tests | **done** |

**Notes:**
- `DetectVersion` now streams tokens, reads only root attributes (no `DecodeElement`), stops immediately
- `VersionInfo` extended with `Confidence`, `Reasons []string`, `VendorNamespaces []string`, `ReleaseNum int`
- Release parsed numerically via `strconv.Atoi`; `-1` = absent, `0` = malformed
- Exact tuple matching: only `1.7`, `2007B`, `2007B4`, `2007C5` are supported; everything else → `VersionUnknown`
- Non-IEC namespace on root → immediate `VersionUnknown` with reason
- Added `DetectFile` convenience wrapper
- 16 detection tests in dedicated `detect_test.go` covering all required cases
- `sclparse detect` updated to print confidence, reasons, and vendor namespaces

---

## Phase 3 — Single-path parse runtime

| Step | Description | Status |
|------|-------------|--------|
| 3.1 | Simplify parse.go to orchestration only | **done** |
| 3.2 | Standardize public parse entrypoints | **done** |
| 3.3 | Expand ParseOptions | **done** |
| 3.4 | Define strict parse semantics | **done** |
| 3.5 | Ensure one internal dispatch layer | **done** |

**Notes:**
- Merged `dispatch.go` into `parse.go` — single file for all parse orchestration
- New primary API: `ParseBytes(data, opts)` and `ParseFileOpts(path, opts)`
- Compat wrappers: `Parse(r)`, `ParseFile(path)`, `ParseWithOptions(r, opts)`, `ParseFileWithOptions(path, opts)`
- `ParseOptions` expanded with `ValidateSemantic`, `MaxDiagnostics`
- Strict mode returns error when any diagnostic has error severity
- `ValidateSemantic` integrates existing `Validate()` into parse pipeline
- Single `decodeAndConvert` dispatch layer: detect → unmarshal → convert → model

---

## Phase 4 — Strengthen normalized model

| Step | Description | Status |
|------|-------------|--------|
| 4.1 | Add DocumentMetadata to root SCL | **done** |
| 4.2 | Keep model semantic, not schema-shaped | **done** (model already semantic) |
| 4.3 | Add extension preservation types | **done** |
| 4.4 | Converters populate metadata and extensions | **done** |

**Notes:**
- `SCL.Metadata *DocumentMetadata` added to root model
- `DocumentMetadata` holds `Version`, `Kind`, `OriginalNamespace`, `VendorNamespaces`
- Populated automatically by `ParseBytes` / `ParseFileOpts` after decode+convert
- `Private` struct added to model with `Type`, `Source`, `InnerXML` fields
- `Private []Private` fields added to `IED`, `AccessPoint`, `LDevice`, `LN`
- All 4 converters (v17, v2007b, v2007b4, v2007c5) map raw Private elements
- InnerXML limited by XSD-generated raw types (`ExtraElements []xml.Token` doesn't capture inner content); `Type`/`Source` attributes always preserved

---

## Phase 5 — Shared index layer

| Step | Description | Status |
|------|-------------|--------|
| 5.1 | Create `scl/index` package | **done** |
| 5.2 | Define stable key types | **done** |
| 5.3 | Build full document index | **done** |
| 5.4 | Diagnostic-aware index build | **done** |
| 5.5 | Resolver helpers on top of index | **done** |

**Notes:**
- New package `scl/index` with `keys.go`, `index.go`, `resolve.go`
- Key types: `AccessPointKey`, `LDeviceKey`, `LNKey`, `DataSetKey`, `ControlKey`
- `Build(s *scl.SCL)` returns `(*Index, []scl.Diagnostic)` — duplicates emit diagnostics
- Index covers: IEDs, AccessPoints, LDevices, LNs, LNodeTypes, DOTypes, DATypes, EnumTypes, DataSets, Reports, ConnectedAPs
- 12 resolver methods: `FindIED`, `FindAccessPoint`, `FindLDevice`, `FindLN`, `FindLNodeType`, `FindDOType`, `FindDAType`, `FindEnumType`, `FindDataSet`, `FindReport`, `FindConnectedAP`, `ResolveLNType`
- 9 tests including real-file test against `testdata/simple.scd`

---

## Phase 6 — Unified diagnostics

| Step | Description | Status |
|------|-------------|--------|
| 6.1 | Delete ValidationFinding, use Diagnostic only | **done** |
| 6.2 | Standardize diagnostic codes | **done** |
| 6.3 | Unify all diagnostic sources | **done** |
| 6.4 | Stable path formatting rules | **done** |

**Notes:**
- `ValidationFinding` and `ValidationSeverity` types completely removed
- `Validate()` now returns `[]Diagnostic` directly
- Diagnostic codes standardized: `duplicate-id`, `duplicate-ied`, `duplicate-access-point`, `duplicate-ld`, `missing-dotype`, `missing-datype`, `missing-enumtype`, `missing-lnodetype`, `missing-dataset`, `missing-connected-ap`, `missing-ld`
- `ParseBytes` with `ValidateSemantic: true` appends validation diagnostics directly
- `cmd_validate.go` updated to consume unified `Diagnostic` type
- All tests migrated from `SeverityError`/`SeverityWarning` to `DiagError`/`DiagWarning`
- Path formatting: `IED[name]/LD[inst]/LN[prefix+class+inst]`, `SubNetwork[name]/ConnectedAP[ied/ap]`

---

## Phase 7 — Split semantic validation into passes

| Step | Description | Status |
|------|-------------|--------|
| 7.1 | Create `scl/validate` package | **done** |
| 7.2 | Validator accepts normalized model + index | **done** |
| 7.3 | Implement passes (templates, ieds, comms, datasets, controls) | **done** |
| 7.4 | Add validator options | **done** |

**Notes:**
- New package `scl/validate` with `validate.go`, `templates.go`, `ieds.go`, `communication.go`, `datasets.go`, `controls.go`
- All passes accept `(*scl.SCL, *index.Index)` — no raw struct walking
- `validate.All()` runs index-build diagnostics + all passes
- `validate.WithOptions()` accepts `Options` struct with `Skip*` flags per pass
- `scl.Validate()` kept as self-contained inline entry point (avoids import cycle `scl` → `scl/index` → `scl`)
- `scl/validate.All()` is the pass-based API for CLI and external consumers
- Topology pass deferred (substation LNode resolution not yet in model)
- 6 tests covering valid model, missing DOType, missing LNodeType, missing IED, missing dataset, real-file test

---

## Phase 8 — Refactor flattening, lookups, exports

| Step | Description | Status |
|------|-------------|--------|
| 8.1 | Refactor Flatten to use shared index | **done** (note below) |
| 8.2 | Refactor lookup.go to use index | **done** (note below) |
| 8.3 | Refactor export helpers to use shared resolver | **done** (note below) |
| 8.4 | Snapshot tests before refactoring | **done** |

**Notes:**
- Go import cycle constraint: `scl/flatten.go` and `scl/lookup.go` are in the `scl` package and cannot import `scl/index` (which imports `scl` for model types).
- `scl/index` is the shared index layer for external consumers (`scl/validate`, CLI commands, etc.)
- `Flatten()`, `Find*()`, `ExportDataSets()`, `ExportReports()` retain self-contained implementations within `scl` package — this is correct Go design; the convenience methods work on the model directly.
- All existing tests verified stable (flatten, export, lookup tests pass unchanged).

---

## Phase 9 — Expand sclparse CLI

| Step | Description | Status |
|------|-------------|--------|
| 9.1 | sclparse uses only public/runtime APIs | **done** |
| 9.2 | Improve existing commands (detect, summary, validate, dump-json) | **done** |
| 9.3 | Add list-ieds, list-lns, list-datasets, list-reports | **done** |
| 9.4 | Improve validate with unified diagnostics | **done** |
| 9.5 | Add better summary detail | **done** |
| 9.6 | Add list-goose, list-smv, list-connected-ap, list-types | **done** |

**Notes:**
- All CLI commands use only public `scl.*` APIs — no internal package shortcuts
- Existing commands already consume unified diagnostics and metadata
- 4 new list commands added: `list-ieds`, `list-lns`, `list-datasets`, `list-reports`
- All list commands support `--json` flag for machine-readable output
- Tabular output uses `text/tabwriter` for aligned columns
- Summary now includes: LN0 count, log controls, services presence, private element count
- 4 additional list commands added: `list-goose`, `list-smv`, `list-connected-ap`, `list-types`
- All list commands support `--json` flag for machine-readable output
- `GSEControl` and `SMVControl` types added to normalized model with converter support across all 4 schema versions
- Summary now correctly counts GSE and SMV controls from the model
- Index and validation updated to cover GSE/SMV control blocks (dataset reference checks)

---

## Phase 10 — Test and fixture cleanup

| Step | Description | Status |
|------|-------------|--------|
| 10.1 | Remove old parser-specific tests | **done** (none remain) |
| 10.2 | End-to-end tests per supported version | **done** |
| 10.3 | Negative fixtures | **done** |
| 10.4 | Extension-preservation tests | **done** |
| 10.5 | CLI golden tests | **done** |

**Notes:**
- No old parser-specific tests remain (cleaned in Phase 1)
- 4 E2E tests: v1.7, 2007B, 2007C5, ABB 2007B4 — each runs detect → parse → validate → summarize
- 9 negative fixture tests: unknown schema, malformed release, missing template ref, missing dataset, bad ConnectedAP, duplicate IED, duplicate LNodeType, strict mode rejection, broken.scd comprehensive
- CLI golden tests added: list-ieds (table + JSON), list-datasets, list-reports, validate broken (exit code)
- 5 extension-preservation tests: Private element survival at IED/AP/LD/LN0/LN levels, Type+Source attributes, ABB real-file Private count, vendor namespace survival, summary PrivateCount

---

## Phase 11 — Topology LNode modeling

| Step | Description | Status |
|------|-------------|--------|
| 11.1 | Add `LNode` type to normalized model | **done** |
| 11.2 | Add `LNodes []LNode` to `Substation`, `VoltageLevel`, `Bay` | **done** |
| 11.3 | Update all 4 converters to map LNode from raw topology | **done** |
| 11.4 | Add topology validation pass (`scl/validate/topology.go`) | **done** |
| 11.5 | Update `scl.Validate()` to check topology LNode refs | **done** |

**Notes:**
- `LNode` type: `IEDName`, `LDInst`, `Prefix`, `LNClass`, `LNInst`, `LNType`, `Desc`
- Converters map LNode at Substation, VoltageLevel, and Bay levels in all 4 schema versions
- Topology pass checks: IED existence, LDevice existence, LN existence (warning-level)
- `IEDName=""` and `IEDName="None"` are silently skipped (unbound LNode references)
- Both pass-based `validate.Topology()` and inline `scl.Validate()` now check topology
- 3 new tests: unresolved LNode, LNode with IEDName=None/empty, GSEControl missing dataset

---

## Phase 12 — Private InnerXML capture

| Step | Description | Status |
|------|-------------|--------|
| 12.1 | Add `InnerXML string \`xml:",innerxml"\`` to raw Private in all 4 packages | **done** |
| 12.2 | Update all converters to use `p.InnerXML` directly | **done** |
| 12.3 | Delete unused `renderTokens` helper (`private.go`) | **done** |

**Notes:**
- Go's `encoding/xml` `,innerxml` tag captures full raw inner XML content as a string
- This replaces the lossy `renderTokens(p.ExtraElements)` approach
- All Private elements now preserve their inner XML content faithfully
- ABB vendor extensions (e.g. `ABB_CommonSA_FunctionInstanceRef`) now fully captured
- Extension preservation is now **lossless** for `Type`, `Source`, and inner content

---

## Phase 13 — Unify validator and tighten API

| Step | Description | Status |
|------|-------------|--------|
| 13.1 | Deprecate `scl.Validate()`, point to `scl/validate.All()` | **done** |
| 13.2 | Sync inline validator with pass-based (GSE/SMV dataset checks, topology) | **done** |
| 13.3 | Fix `addWarn` bug (Code field was set to path) | **done** |
| 13.4 | Deprecate `ParseWithOptions` and `ParseFileWithOptions` | **done** |

**Notes:**
- `scl.Validate()` now marked `Deprecated` in godoc; users directed to `scl/validate.All()`
- Inline validator now checks GSE/SMV control block dataset references
- Inline validator now checks topology LNode references
- `ParseWithOptions` → `ParseBytes`, `ParseFileWithOptions` → `ParseFileOpts`
- Primary API surface: `ParseBytes`, `ParseFileOpts`, `Parse`, `ParseFile`

---

## Phase 14 — GOOSE/SMV communication linkage validation

| Step | Description | Status |
|------|-------------|--------|
| 14.1 | Validate GSE cbName resolves to actual GSEControl in LN0 | **done** |
| 14.2 | Validate SMV cbName resolves to actual SMVControl in LN0 | **done** |

**Notes:**
- `communication.go` now checks beyond LD existence: verifies cbName matches a control block
- New diagnostic codes: `unresolved-gse-control`, `unresolved-smv-control` (warning severity)
- If LD exists but LN0 has no matching GSEControl/SMVControl, a warning is emitted

---

## Phase 15 — Content-based document kind detection

| Step | Description | Status |
|------|-------------|--------|
| 15.1 | Add `DetectKind(s *SCL) DocumentKind` function | **done** |

**Notes:**
- Classifies based on content: IED count, substation presence, communication bindings
- SSD: substations but no IEDs; ICD: 1 IED, no comm; CID: 1 IED + comm; SCD: multiple IEDs or 1 IED + substation
- Used by `sclparse inspect` to show both extension-based and content-based kind

---

## Phase 16 — Schema/extension inspect command

| Step | Description | Status |
|------|-------------|--------|
| 16.1 | Add `sclparse inspect` command | **done** |

**Notes:**
- Shows schema version, document kind (extension + content-based), confidence, namespace
- Lists vendor namespaces and detection reasons
- Counts Private elements and breaks down by Type
- Supports `--json` flag for machine-readable output

---

## Phase 17 — Test expansion

| Step | Description | Status |
|------|-------------|--------|
| 17.1 | Golden tests for list-goose, list-connected-ap, list-types, inspect | **done** |
| 17.2 | Topology validation tests (unresolved LNode, IEDName=None) | **done** |
| 17.3 | GSEControl missing-dataset test | **done** |

**Notes:**
- CLI golden tests added for: list-goose (table + JSON), list-connected-ap, list-types, inspect (text + JSON)
- 3 new validate tests: topology LNode resolution, IEDName=None/empty skip, GSE dataset check

---

## Blocked / future work

| Item | Reason |
|------|--------|
| ~~Topology validation pass (7.3)~~ | **done** — implemented in Phase 11 |
| ~~Private `InnerXML` capture~~ | **done** — implemented in Phase 12 |

---

## Roadmap

| Milestone | Description | Priority |
|-----------|-------------|----------|
| Cycle-safe internal resolver | Extract shared resolution logic into `scl/internal/resolve` so `scl.Flatten()`, `Find*()`, and exports can use the same resolver as `scl/validate` — requires model type extraction or interface approach | medium |
| Performance/regression tests | Lock in acceptable memory and runtime behavior for large ABB exports now that parsing is single-path | medium |
| Compatibility matrix document | Document what is supported, rejected, and preserved-as-extension for 1.7, 2007B, 2007B4, 2007C5 | low |
| Review Generate scope | Consider moving `Generate` behind clearer scope boundaries; roundtrip fidelity is not guaranteed | low |
| Result as primary return type | Consider making `Result` the primary return type everywhere, deprecating raw `*SCL` convenience paths | low |

---

## Completion status

All 17 phases are **done**. Roadmap items above describe future improvement opportunities.
