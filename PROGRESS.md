# PROGRESS.md — `otfabric/go-iec61850`

## Phase M0 — Foundation and public API skeleton

**Status:** Complete

**Date:** 2026-03-19

### What was built

| File | Purpose |
|---|---|
| `go.mod` | Module setup with `go-mms` dependency (local replace for dev) |
| `doc.go` | Package documentation covering layering, references, FCs, errors, logging |
| `fc.go` | `FunctionalConstraint` type with all 19 IEC 61850 FCs (ST, MX, SP, SV, CF, DC, SG, SE, SR, OR, BL, EX, CO, US, MS, RP, BR, LG, GO), parser, validation, descriptions |
| `fc_test.go` | Tests for FC parsing (valid + invalid), validation, descriptions, enumeration |
| `ref.go` | `Ref` type for parsed IEC 61850 object references with `ParseRef`, `String`, `Parent`, `Child`, `WithFC`, `ObjectReference`, `IsLD`/`IsLN`/`IsObject`/`HasPath`/`Depth`, `Validate`, `ToMMS`/`RefFromMMS` |
| `ref_test.go` | Tests for reference parsing (valid + malformed matrix), roundtrip formatting, parent/child navigation, validation, depth/IsObject, MMS conversion roundtrip |
| `errors.go` | Sentinel errors (`ErrInvalidReference`, `ErrNotFound`, `ErrTypeMismatch`, etc.) and typed error structs (`ReferenceError`, `DecodeError`, `ModelError`, `ReportError`, `SCLParseError`) with proper `errors.Is`/`errors.As` support |
| `errors_test.go` | Tests for all sentinels, typed error wrapping, wrapped error in error strings |
| `options.go` | `DialOptions`, `ClientOptions`, `StrictnessOptions` |
| `logging.go` | `discardHandler` for no-op slog logging (default when no logger is configured) |
| `client.go` | `Client` type wrapping `*mms.Client` with `Dial`, `NewClient`, `Close`, `Abort`, `MMS` escape hatch |

### Design decisions

1. **`FunctionalConstraint` as `string`-backed type.** Chosen over `iota` enum because FC values appear as strings in IEC 61850 references and MMS item IDs. String-backed types allow natural formatting (`"ST"`) and simple comparison while still providing type safety through the named type. The `ParseFC` function provides validation.

2. **`Ref` as a value type (not pointer).** References are small, immutable-friendly values that benefit from value semantics. `Parent()` and `Child()` return copies, not mutations. This avoids accidental aliasing.

3. **MMS mapping in `Ref.ToMMS` / `RefFromMMS`.** The IEC 61850 → MMS mapping (LD=domain, LN$FC$path=itemID with `$` separators) is encapsulated in the `Ref` type. This keeps the mapping logic visible and testable without requiring an internal package yet. The mapping matches the C reference implementation's `MmsMapping_createMmsVariableNameFromObjectReference`.

4. **Dual client creation paths.** `Dial` owns the MMS client and closes it. `NewClient` wraps an existing MMS client without ownership. This follows the `go-mms` pattern and supports both convenience and advanced use cases.

5. **Error taxonomy.** Sentinel errors for `errors.Is` + typed error structs for `errors.As`. Each typed error wraps the corresponding sentinel by default, or a more specific inner error when available. This preserves the full error chain through go-mms.

6. **No hidden globals.** Logger defaults to discard. No package-level mutable state. Options structs control all behavior.

### Test summary

- 37 top-level tests (119 including subtests), all passing
- Race detector clean
- Coverage: FC parsing (valid/invalid), reference parsing (valid/malformed matrix), reference roundtrip, parent/child navigation, Validate(), Depth(), IsObject(), MMS conversion roundtrip, all error sentinels, all typed errors with and without wrapped errors

### What was deferred

- Model cache scaffolding (will be added in M1 when browse results need caching)
- `MustParseRef` — not justified yet; `ParseRef` with explicit error handling is preferred
- Connection retry/reconnect logic — belongs in a higher layer

### Open questions

1. `Ref.ToMMS` for LN-only (no path, no FC) returns just the LN name as itemID. This is valid for GetNameList operations but not for Read. Per feedback: "ToMMS() should mean 'mapping exists', not 'operation is valid'." Read/write APIs (M1+) will enforce stricter shape requirements.

---

## M0 Hardening — Feedback pass

**Status:** Complete

**Date:** 2026-03-19

### Changes made

All items from `FEEDBACK.md` "concrete changes before M1" list, plus lower-priority improvements:

| # | Feedback item | Change |
|---|---|---|
| 1 | `NewClient(nil, ...)` should not be allowed | Changed signature to `NewClient(mmsClient *mms.Client, opts ClientOptions) (*Client, error)`. Returns error for nil. Added defensive nil check in `Dial` if transport returns nil with nil error. |
| 2 | `Close` / `Abort` should guard against nil mmsClient | Added `c.mmsClient != nil` guard to both methods. |
| 3 | Verify `go-mms.Client.Abort` exists | Confirmed: `func (c *Client) Abort(_ context.Context) error` exists in go-mms. Kept. |
| 4 | Add `Ref.Validate() error` | Added with checks: LD non-empty, path requires LN, no empty path components, FC validity, no separator characters in components. |
| 5 | `ToMMS()` should call `Validate()` | `ToMMS()` now calls `r.Validate()` before conversion. |
| 6a | `ParseRef` bracket hardening | Added upfront check: reject mismatched brackets, reject multiple bracket pairs. |
| 6b | LD-only semantics | Documented in `Ref` type doc that LD-only refs are valid for browsing but not all operations accept them. |
| 6c | `Child(name)` validation | Changed to `Child(name string) (Ref, error)`. Rejects empty names and names containing separators (`/`, `.`, `$`, `[`, `]`). |
| 6d | `Parent()` FC preservation | Added doc comment explaining FC is preserved and why. |
| 7 | `RefFromMMS` reject empty segments | Added explicit empty-segment check for FC field and all path segments after `$` split. Tests cover `LLN0$$foo`, `LLN0$ST$$stVal`, `LLN0$ST$`. |
| 8 | Sharpen `MMS()` docs | Documented: shared pointer (not copy), do not close when IEC layer owns it, follows go-mms closed semantics after Close, bypassing IEC layer can violate invariants. |
| 9 | `doc.go` quick-start | Replaced `ListLogicalDevices` call (not yet implemented) with comment referencing `client.MMS()` for now. |
| 10 | `DialOptions.MMS` comment | Changed from "passed through to mms.NewClient or the ISO transport dial function" to "passed through to iso.Dial via iso.WithClientDialOptions". |
| 11 | Add `Depth()` / `IsObject()` | Added `Depth() int` (0=empty, 1=LD, 2=LN, 2+len(Path)=object) and `IsObject() bool` (LN+path). |
| 12 | Typed error `Error()` includes `Wrapped` | `DecodeError`, `ModelError`, `ReportError` now include the wrapped error text in their `Error()` string when `Wrapped` is non-nil. |

### Breaking changes from M0

- `NewClient` now returns `(*Client, error)` instead of `*Client`.
- `Ref.Child` now returns `(Ref, error)` instead of `Ref`.

Both changes are intentional tightenings per feedback — better to break now (before any downstream code exists) than carry footguns forward.

### Test summary

- 37 top-level tests (119 including subtests), all passing
- Race detector clean
- Linter (golangci-lint): 0 issues
- New test coverage: `Validate()`, `Depth()`, `IsObject()`, `Child` validation (empty, separators), `RefFromMMS` empty segments (3 new invalid cases), typed error wrapped-error-in-string formatting

---

## M0 Hardening — Feedback pass 2

**Status:** Complete

**Date:** 2026-03-19

### Changes made

Final cleanup round from second feedback pass. All items from the "recommended final patch set before M1":

| # | Feedback item | Change |
|---|---|---|
| 1 | `Validate()`: reject FC when LN == "" | Added rule: `LD[ST]` is no longer a valid Ref shape. FC requires at least a logical node. This also means `ParseRef("LD1[ST]")` now fails via Validate(). |
| 2 | `Child()`: reject when r.LN == "" | Added early check: cannot add child path elements to an LD-only ref. Previously this would create an invalid Ref silently. |
| 3 | `ParseRef()`: centralize validation via Validate() | All three happy-path return branches now construct the Ref and call `Validate()` before returning. Parse-level syntax checks (brackets, length, empty separators) remain for clearer error messages. Structural invariants are centralized in `Validate()`. |
| 4 | `RefFromMMS()`: centralize validation via Validate() | Final return now constructs the Ref and calls `Validate()` before returning, matching the ParseRef pattern. |
| 5 | `Parent()` doc comment | Updated to explain that FC is preserved when the parent still has LN/path context, but dropped when parent becomes LD-only (because `LD[FC]` is not a meaningful IEC 61850 shape). |
| 6 | `SCLParseError.Error()`: include wrapped text | Now includes `Wrapped` error text in the formatted string, consistent with `DecodeError`, `ModelError`, and `ReportError`. |

Also fixed:
- `Depth()` comment: changed "empty or invalid ref" to "zero-value ref (empty LD)" for precision.

### Items noted as acceptable (no change needed)

- `Validate()` uses `r.String()` as error input — acknowledged, acceptable for both parsed and manually constructed refs.
- `validateComponent` rejecting `$` — confirmed correct as MMS separator.
- `WithFC()` does not validate FC — acceptable, callers use `Validate()` when needed.
- `Abort(ctx)` — previously verified that `go-mms.Client.Abort` exists.
- `fc.go` maps — no change needed.

### Test summary

- 38 top-level tests (125+ including subtests), all passing
- Race detector clean
- Linter (golangci-lint): 0 issues
- New test coverage: `ParseRef("LD1[ST]")` rejected, `Validate()` rejects FC-on-LD-only, `Child()` rejects LD-only refs, `Parent()` FC-drop on LN→LD explicitly verified, `SCLParseError` with wrapped error text

### M0 status

M0 is now locked. Ready for M1 (browse / model discovery).

---

## Phase M1 — Browse model and tree traversal

**Status:** Complete

**Date:** 2026-03-19

### What was built

| File | Purpose |
|---|---|
| `model.go` | Public model types: `LogicalDevice`, `LogicalNode`, `DataObject`, `ModelNode`, `FindQuery` |
| `browse.go` | Browse API methods on `Client`: `ListLogicalDevices`, `ListLogicalNodes`, `ListDataObjects`, `ListDataAttributes`, `Tree`, `FindPaths`, `GetVariableType` |
| `browse_test.go` | Loopback server tests for all browse APIs using a realistic IEC 61850 MMS variable model |
| `internal/mapping/names.go` | Internal helpers: `ExtractLogicalNodes`, `ExtractDataObjects`, `ExtractChildren`, `ParseItemID`, `GroupByFC` |
| `internal/mapping/names_test.go` | Unit tests for all mapping helpers with representative IEC 61850 MMS item names |

### Browse API summary

| Method | Description |
|---|---|
| `ListLogicalDevices(ctx)` | Returns all MMS domains as logical devices (sorted) |
| `ListLogicalNodes(ctx, ld)` | Extracts unique LN names from domain variables (sorted) |
| `ListDataObjects(ctx, ld, ln)` | Extracts unique top-level DO names for a given LN (sorted) |
| `ListDataAttributes(ctx, ref)` | Returns direct children at any depth under a reference |
| `Tree(ctx)` | Builds the full model tree: root → LDs → LNs → DOs → DAs |
| `FindPaths(ctx, query)` | Searches for references matching a glob pattern, optional FC filter and depth limit |
| `GetVariableType(ctx, ref)` | Retrieves MMS TypeSpec via GetVariableAccessAttributes |

### Design decisions

1. **Flat MMS name list as primary data source.** All browse operations use `GetNameListAll` to retrieve the full list of MMS variables in a domain, then parse item IDs to extract the IEC 61850 hierarchy. This is simple, requires no model cache, and works with any MMS server.

2. **Internal mapping package.** The `internal/mapping` package handles $-delimited item ID parsing and name extraction. This keeps the browse.go methods clean and makes the parsing logic independently testable. The Ref type's ToMMS/RefFromMMS handles individual reference conversion; the mapping package handles bulk name analysis.

3. **ModelNode as recursive tree.** The `Tree()` method returns a recursive `*ModelNode` structure that represents the full LD → LN → DO → DA hierarchy. Each node carries its `Ref`. Type information is not populated by default (use `GetVariableType` per-node).

4. **FindPaths uses path.Match.** Glob matching uses Go's `path.Match` against the ObjectReference string. This provides platform-independent `*` matching within components. FC filtering and MaxDepth are applied as post-filters.

5. **Loopback server for integration tests.** Browse tests use a channel-based transport pair with a real `mms.Server` instance that registers IEC 61850-style domain variables. This validates the full MMS round-trip, not just mapping logic.

6. **No model cache yet.** The plan calls for M1.4 (cache scaffolding), but per the plan note "do not overcomplicate in first pass," the cache is deferred. Each browse call re-queries the server. This is correct for the initial implementation; caching can be layered in without API changes.

### Test summary

- 58 top-level tests (across `iec61850` + `internal/mapping`), all passing
- Race detector clean
- Linter (golangci-lint): 0 issues
- Test coverage:
  - **Mapping:** LN extraction, item ID parsing (valid/invalid), DO extraction, child extraction at multiple depths, FC grouping
  - **Browse:** ListLogicalDevices, ListLogicalNodes (valid + empty LD), ListDataObjects (two LNs), ListDataAttributes (with children + empty ref), Tree structure verification (root → LD → LN → DO → DA), FindPaths (glob match + FC filter + empty pattern), LogicalNode.Ref(), GetVariableType

### What was deferred

- **Model cache (M1.4):** Browse operations re-query the server each call. A cache layer can be added later without changing the public API.
- **`**` glob support in FindPaths:** Reserved for future use. Only `*` within-component matching is currently supported.
- **FC annotation on tree nodes:** The tree-building pass does not yet annotate nodes with their FC(s). This requires either type-spec introspection or tracking which FCs each path appears under.

### Open questions

1. Should `Tree()` accept options (e.g., limit to specific LD, limit depth, include FC info)? Currently it always builds the full tree.
2. Should `FindPaths` support regex patterns in addition to glob? The current `path.Match` implementation is simple but limited.

---

## M1 Hardening — Feedback pass

**Status:** Complete

**Date:** 2026-03-19

### Changes made

All items from `FEEDBACK.md` M1 review, including the 4 "do immediately" fixes and all worthwhile tightenings:

| # | Feedback item | Change |
|---|---|---|
| 1 | `filepath.Match` → `path.Match` | Switched from `path/filepath` to `path` import in `browse.go`. `path.Match` is platform-independent, correct for IEC 61850 logical paths. Pattern errors are now returned instead of silently ignored. |
| 2 | `FindQuery.Pattern` docs contradict implementation about `**` | Removed `**` from `FindQuery.Pattern` doc comment and examples in `model.go`. Updated `FindPaths` doc to reference `path.Match` semantics with `*` and `?`. |
| 3 | `ref.Child(name)` error ignored in `ListDataAttributes` | Changed `childRef, _ := ref.Child(name)` to properly check and return the error. Malformed server-derived names now produce a clear error message. |
| 4 | `GetVariableType` accepts too-broad refs | Added explicit `IsObject()` and `FC != ""` checks before `ToMMS()`. Doc comment updated to match the stricter validation. Returns `ReferenceError` for invalid refs. |
| 5 | `ListDataAttributes` return type name (`DataObject`) | Noted but not renamed yet. Acceptable for M1 per feedback ("Not urgent"). |
| 6 | `Tree()` repeated scans | Noted as known scaling limit. No change per feedback ("Fine for M1, leave it"). |
| 7 | `ParseItemID` too permissive for empty path segments | Added validation: rejects item IDs with empty path segments (e.g., `LN$ST$Do$$da`). Returns `ok=false` for trailing `$` as well. |
| 8 | `ExtractLogicalNodes` treats non-IEC names as LNs | Refactored to use `ParseItemID` instead of `firstSegment`. Only items with valid IEC 61850 structure (at least `LN$FC`) are now counted. Plain MMS variables without `$` separators are ignored. Removed unused `firstSegment` helper. |
| 9 | Browse APIs: "empty result" vs "bad target" | No change per feedback ("empty slice is okay for M1"). Decision documented. |
| 10 | `FindPaths` silently skips malformed items | Added `c.logger.Debug(...)` call when `RefFromMMS` fails, logging LD name, item ID, and error. |
| 11 | `doc.go` outdated quick-start | Updated to show `ListLogicalDevices` and `ListLogicalNodes` calls instead of placeholder comment about M1+. |
| 12 | `FindQuery.Pattern` examples mention `**` | Removed the `**` example (`"*/MMXU*.TotW.**"`). Only `*` examples remain. |

### Test summary

- 62 top-level tests, all passing
- Race detector clean
- Linter (golangci-lint): 0 issues
- New test coverage: `GetVariableType` with non-object ref, `GetVariableType` without FC, `FindPaths` with invalid glob pattern (`[`), `ExtractLogicalNodes` filtering non-IEC names, `ParseItemID` rejecting empty path segments

---

## M1 Hardening — Feedback pass 2

**Status:** Complete

**Date:** 2026-03-19

### Changes made

Final polish round from third feedback pass. All recommended items implemented:

| # | Feedback item | Change |
|---|---|---|
| 1 | Add `checkOpen()` closed-state guard | Added `checkOpen() error` helper on `Client` that returns `ErrClosed` if the client has been closed or aborted. Called at the top of all 7 public browse methods (`ListLogicalDevices`, `ListLogicalNodes`, `ListDataObjects`, `ListDataAttributes`, `Tree`, `FindPaths`, `GetVariableType`). Keeps error taxonomy consistent at the IEC 61850 layer. |
| 2 | `FindPaths` recompiles glob pattern every iteration | Pattern is now validated once via `path.Match(pattern, "")` before the server walk. Inside the loop, the error is discarded since the pattern is known-valid. |
| 3 | `ListDataAttributes` should validate ref | Replaced ad-hoc `ref.LD == ""` check with `ref.Validate()` call. Catches malformed refs (e.g., illegal separator chars in path components) from manually constructed `Ref` values. Same change applied to `GetVariableType`. |
| 4 | `Tree()` root node convention | Added doc comment clarifying the returned root `ModelNode` is a synthetic container (Name="root", empty Reference) that does not represent a real IEC 61850 object. |
| 5 | `ModelNode.FC` and `DataObject.FC` underused | Sharpened doc comments on both fields to explain they are empty during browse/tree discovery (same node may appear under multiple FCs) and populated only when a single FC is unambiguously known. |
| 6 | `FindQuery.MaxDepth` semantics | Added doc comment mapping depth values to IEC 61850 hierarchy levels: 1=LD, 2=LN, 3=first DO, 4+=deeper DAs. |
| 7 | `ReferenceError` for LD-only string params | No change per feedback ("not worth changing now"). |
| 8 | `DataObject` return type naming | No change per feedback ("not a blocker, keep an eye on it"). |

### Test summary

- 63 top-level tests (70+ including subtests), all passing
- Race detector clean
- Linter (golangci-lint): 0 issues
- New test coverage: `TestBrowseMethods_ClosedClient` — verifies all 7 public browse methods return `ErrClosed` after `Close()`

### M1 status

M1 is now locked. Ready for M2 (values, read, write).

---

## Phase M2 — Values, read, write, and bulk operations

**Status:** Complete

**Date:** 2026-03-19

### What was built

| File | Purpose |
|---|---|
| `quality.go` | `Quality` type (uint16), `Validity` type, all 11 detail/source/test flags, decode/encode to/from MMS bit string, `String()` formatting |
| `quality_test.go` | Validity extraction, IsGood logic, flag checks, roundtrip encode/decode, bit layout verification, error cases |
| `timestamp.go` | `Timestamp` struct (time.Time + TimeQuality), decode/encode to/from MMS UTCTime |
| `timestamp_test.go` | IsZero, String formatting, roundtrip encode/decode, error cases, zero TimeQuality |
| `values.go` | `Value` wrapper over `*mms.Value` with typed accessors (Bool, Int32/64, Uint32/64, Float32/64, VisibleString, MmsString, OctetString, BitString, Quality, Timestamp, Elements), constructor functions (BoolValue, IntValue, UintValue, FloatValue, StringValue, OctetStringValue, QualityValue, TimestampValue, StructureValue, ArrayValue) |
| `values_test.go` | All typed accessors (success + type mismatch errors), nil handling, constructor type verification, Elements for struct and array |
| `read.go` | `Read(ctx, ref)` returns `*Value`, `ReadRaw(ctx, ref)` returns `*mms.Value` |
| `read_test.go` | Read integer/boolean/quality/timestamp, error cases (no FC, no LN, closed client) |
| `write.go` | `Write(ctx, ref, *mms.Value)` single variable write |
| `write_test.go` | Write integer + read-back, write boolean + read-back, error cases (no FC, not object, nil value, closed client). Uses writable loopback server with Write handlers. |
| `bulk.go` | `ReadMultiple(ctx, refs)` bulk read with per-item `ReadResult`, `WriteMultiple(ctx, requests)` bulk write with per-item `WriteResult` |
| `bulk_test.go` | ReadMultiple (3 variables, order preserved, quality decode), WriteMultiple (2 variables, read-back verification), empty/nil/closed/validation error cases |

### Read/Write API summary

| Method | Description |
|---|---|
| `Read(ctx, ref)` | Single read, returns `*Value` with typed accessors |
| `ReadRaw(ctx, ref)` | Single read, returns raw `*mms.Value` |
| `ReadMultiple(ctx, refs)` | Bulk read via MMS ReadMultiple, per-item error reporting |
| `Write(ctx, ref, value)` | Single write, requires object ref with FC |
| `WriteMultiple(ctx, requests)` | Bulk write via MMS WriteVariables, per-item success/error |

### Value model summary

| Type | Description |
|---|---|
| `Value` | Wrapper over `*mms.Value` with IEC 61850 typed accessors and `MMS()` escape hatch |
| `Quality` | uint16 bitfield matching IEC 61850 / C libiec61850 convention (bit 0–1 = validity, bits 2–12 = detail/source/test flags) |
| `Timestamp` | time.Time + TimeQuality (leap second, clock failure, clock not synchronized, time accuracy) |

### Design decisions

1. **Value as lightweight wrapper, not separate tagged union.** Since `mms.Value` is already a tagged union, the IEC 61850 `Value` wraps it rather than duplicating it. This avoids redundant storage and keeps MMS escape hatch simple. Typed accessors return `(T, error)` instead of `(T, bool)` for stronger error reporting via `DecodeError`.

2. **Quality bit layout matches C libiec61850 convention.** Bit 0 of `Quality` = first named bit of the MMS bit string = first validity bit. This makes constants like `QualityOverflow = 1 << 2` match the C `QUALITY_DETAIL_OVERFLOW = 4`. Bit reversal between MMS MSB-first encoding and uint16 storage is handled by `decodeQualityBits`/`encodeQualityBits`.

3. **Decode/encode on types, not in separate internal packages.** The plan suggested `internal/decode` and `internal/encode` packages, but quality/timestamp decode logic is ~20 lines each. Separate packages would create circular dependency (internal needs Quality type from main package, main needs decode from internal). Decode/encode functions live on the types themselves (`DecodeQuality`, `EncodeQuality`, `DecodeTimestamp`, `EncodeTimestamp`). If the decode pipeline becomes more complex (e.g., `ReadInto` with model/type metadata), a separate internal package can be introduced later.

4. **Write accepts `*mms.Value` directly, not `*Value`.** For writes, users know the target type and construct values with `mms.NewInteger(42)` etc. The `Value` wrapper is primarily for reading (decoding). This avoids unnecessary wrapping for the write path.

5. **ReadMultiple uses go-mms `ReadMultiple`.** Maps `[]Ref` → `[]ObjectName`, calls MMS ReadMultiple, maps `[]AccessResult` back to `[]ReadResult` with per-item error reporting. Order is preserved.

6. **WriteMultiple uses go-mms `WriteVariables`.** Maps `[]WriteRequest` → `([]VariableSpec, []*Value)`, calls MMS WriteVariables, maps `[]WriteAccessResult` back to `[]WriteResult` with per-item success/error.

7. **Writable loopback server for write tests.** Write tests use a separate `setupWritableLoopback` that registers variables with both Read and Write handlers, storing values in a mutex-protected map. This enables write + read-back verification in tests.

8. **TimeQuality not populated from go-mms.** go-mms decodes UTCTime to `time.Time`, discarding the time quality byte. The `TimeQuality` struct exists with proper fields (LeapSecondKnown, ClockFailure, ClockNotSynchronized, TimeAccuracy) but is zero-valued when decoding from go-mms. Documented as known limitation.

### Test summary

- 116 top-level tests (including subtests), all passing
- Race detector clean
- Linter (golangci-lint): 0 issues
- Test coverage:
  - **Quality:** Validity extraction (6 cases), IsGood logic (6 cases), Has/WithValidity, String formatting, encode/decode roundtrip (6 quality patterns including all-flags), bit layout verification, error cases (non-bit-string, too-short)
  - **Timestamp:** IsZero, String formatting, encode/decode roundtrip, error cases (non-UTCTime), zero TimeQuality
  - **Values:** All 10 typed accessors (success + type mismatch), nil/MMS escape hatch, constructor type verification (8 constructors), Elements for struct and array
  - **Read:** Read integer, boolean, quality, timestamp, error cases (no FC, no LN, closed client)
  - **Write:** Write integer/boolean + read-back, error cases (no FC, not object, nil value, closed)
  - **Bulk:** ReadMultiple (3 mixed types, order preserved), WriteMultiple (2 vars + read-back), empty/nil/closed/validation errors

### What was deferred

- **`ReadInto(ctx, ref, interface{})`**: Decoding MMS values into arbitrary Go structs requires model/type metadata and reflection. Deferred to a later phase when the decode pipeline is more mature.
- **`WriteValue(ctx, ref, interface{})`**: Inverse of ReadInto — encoding Go types to MMS values. Same deferral reason.
- **Time quality from go-mms**: The UTCTime quality byte (leap second, clock failure, etc.) is not accessible through go-mms's `UTCTime()` accessor. Requires a go-mms enhancement to expose raw bytes or quality fields.
- **Partial-success semantics for ReadMultiple**: Per-item DataAccessError is captured in `ReadResult.Err`, but no loopback test currently exercises a partial failure scenario (some refs succeed, some fail).

### Open questions

1. Should `Read` accept LN-level refs (no path) to read the entire LN as a structure? Currently it requires LN + FC but not a path. This works because `ToMMS()` maps LN-only refs to just the LN name as itemID.
2. Should `WriteMultiple` support mixed-domain writes (refs spanning multiple LDs)? Currently it works because each ref maps independently to an MMS ObjectName with its own domain.
3. Should we add a `ReadComponent(ctx, ref, component)` method that wraps `mms.Client.ReadComponent` for reading single structure elements without fetching the full structure?

---

## M2 Hardening — Feedback pass

**Status:** Complete

**Date:** 2026-03-19

### Changes made

All items from `FEEDBACK.md` M2 cleanup:

| # | Feedback item | Change |
|---|---|---|
| 1 | Value accessors nil-safe | Added `requireValue(expected string) error` helper on `*Value`. Called at the top of all 14 accessor methods (Bool, Int32, Int64, Uint32, Uint64, Float32, Float64, VisibleString, MmsString, OctetString, BitString, Quality, Timestamp, Elements). Returns `*DecodeError` with "nil value" message when `v == nil` or `v.mmsVal == nil`. Also made `typeError()` nil-safe. |
| 2 | Standalone decoders nil-safe | `DecodeQuality(nil)` and `DecodeTimestamp(nil)` now return `*DecodeError` instead of panicking. |
| 3 | Simplify ReadMultiple result mapping | Rewrote result mapping to use `ErrorCode` as primary signal: if `ErrorCode != 0` → per-item error; else if `Value == nil` → error for missing value; else → wrap with `NewValue`. No longer relies on `Value.Type() == DataAccessError`. |
| 4 | Validate bulk response cardinality | Added `len(accessResults) != len(refs)` check in `ReadMultiple` and `len(writeResults) != len(requests)` check in `WriteMultiple`. Both return error on mismatch. Documented that go-mms already validates response count at the MMS level. Simplified `WriteMultiple` to direct 1:1 index mapping since go-mms returns sequential indexed results. |
| 5 | Tighten timestamp docs | Rewrote `Timestamp` type doc to clarify: TimeQuality can hold values in memory but is not preserved on wire roundtrip; `DecodeTimestamp` always returns zero TimeQuality; `EncodeTimestamp` only writes the time value. |
| 6 | Quality.String() default validity | Added `default: validity(%d)` case for malformed external values. |
| 7 | Tests | Added: `TestValue_NilAccessors` (14 accessors on nil `*Value`), `TestDecodeQuality_Nil`, `TestDecodeTimestamp_Nil`, `TestReadMultiple_PartialError` (one valid, one non-existent, one valid — verifies order preserved and only missing ref has per-item error). |

### Test summary

- 120 top-level tests (including subtests), all passing
- Race detector clean
- Linter (golangci-lint): 0 issues
- New test coverage: nil `*Value` on all 14 accessors, nil input to `DecodeQuality`/`DecodeTimestamp`, partial bulk read with per-item error

### M2 status

M2 is now locked. Ready for M3 (datasets and reports).

---

## M2 Final review — Pre-M3 cleanup

**Status:** Complete

**Date:** 2026-03-19

### Changes made

| # | Item | Change |
|---|---|---|
| 1 | `ReadRaw` nil response guard | Added `result == nil \|\| result.Value == nil` check before the debug log line. Returns `"missing value in read response"` error instead of panicking. |
| 3 | `Value.Type()` nil doc | Clarified doc comment: returns `mms.ValueTypeDataAccessError` when Value or underlying MMS value is nil. |

### Notes acknowledged (no code changes needed)

- **#2** `checkOpen()` could use `sync.RWMutex` later — noted, not urgent.
- **#4** `FindPaths` glob pre-validation confirmed good.
- **#5** `WriteMultiple` reliance on go-mms sequential result invariant — verified, tested in go-mms.
- **#6** Browse APIs re-fetch domain variable lists — model cache deferred to post-M3.

### Test summary

- 120 tests passing, race-detector clean, 0 linter issues
- No new tests needed — existing coverage already exercises the changed path

---

## Phase M3 — Data sets and reports

**Status:** Complete

**Date:** 2026-03-19

### New files

| File | Purpose |
|------|---------|
| `dataset.go` | DataSet types, ListDataSets, GetDataSet, ReadDataSet |
| `dataset_test.go` | Loopback tests for all dataset operations |
| `report.go` | OptFlds, TrgOps, ReasonCode, RCBType, ReportControlBlock, ReportIndication, subscription engine |
| `report_test.go` | Type tests, RCB loopback tests, report decode tests, subscription tests |

### Modified files

| File | Change |
|------|--------|
| `client.go` | Added `reportOnce`, `reportMu`, `reportSubs` fields for report dispatch |

### Dataset API summary

| Method | Description |
|--------|-------------|
| `ListDataSets(ctx, ld)` | Lists NVL names in a domain |
| `GetDataSet(ctx, ld, dsName)` | Gets data set definition (members, deletable flag) |
| `ReadDataSet(ctx, ld, dsName)` | Reads all member values in a single MMS request |

**Types:** `DataSet`, `DataSetMember`, `DataSetValue`

### Report API summary

| Method | Description |
|--------|-------------|
| `ListReports(ctx, ld)` | Lists RCB names (BRCB/URCB) by filtering domain variables for 3-segment BR/RP patterns |
| `GetReportControlBlock(ctx, ld, rcbItemID)` | Reads all RCB attributes as a structure and decodes them |
| `SetReportControlBlock(ctx, ld, rcbItemID, update)` | Writes selected RCB attributes using field mask; each field written as individual MMS Write |
| `SubscribeReport(ctx, rptID, opts)` | Creates subscription that delivers decoded reports matching an RptID |

**Types:** `OptFlds`, `TrgOps`, `ReasonCode`, `RCBType`, `ReportControlBlock`, `RCBFieldMask`, `RCBUpdate`, `ReportIndication`, `ReportSubscription`, `SubscribeReportOptions`

### Design decisions

1. **Datasets map to MMS Named Variable Lists.** `ListDataSets` uses `GetNameListAll` with `ObjectClassNamedVariableList`. `GetDataSet` uses `GetNamedVariableListAttributes`. `ReadDataSet` uses `ReadNamedVariableList`. Value count validated against member count.

2. **RCB attributes written as individual MMS variables.** `SetReportControlBlock` writes each attribute as `rcbItemID$AttrName` using `mms.Client.Write`, not `WriteComponent`. This is more universally compatible. Field mask prevents accidental partial writes. `RptEna` is always written last to ensure other fields are set before enabling.

3. **Report subscription uses single MMS handler with demultiplexing.** `Client` installs one `OnInformationReport` handler lazily (via `sync.Once`) on first subscription. Incoming reports are decoded and dispatched by RptID to the matching `ReportSubscription` via a bounded channel. Overflow drops with warning log.

4. **Report indication decoded from flat MMS value list.** `decodeReportIndication` parses the standard IEC 61850 report field order: RptID, OptFlds, conditional fields (SeqNum, timestamp, DatSet, BufOvfl, EntryID, ConfRev, segmentation), inclusion bitstring, data references, values, reason codes. Bit reversal follows the same MSB-first convention as Quality.

5. **OptFlds and TrgOps use same MMS bit reversal as Quality.** Decoded from MMS BitString with MSB-first byte layout. TrgOps bits are offset by 1 (bit 0 is reserved in the MMS encoding), handled via shift in decode/encode.

6. **isRCBItemID matches exactly 3 `$`-separated segments.** This prevents matching RCB attribute sub-paths (e.g., `LLN0$BR$brcb01$RptEna`) when listing reports.

### Test summary

- 153 top-level tests (including subtests), all passing
- Race detector clean
- Linter (golangci-lint): 0 issues
- Dataset tests: ListDataSets, GetDataSet (members, deletable, refs), ReadDataSet (integer, quality, boolean), closed client guards
- Report type tests: OptFlds/TrgOps Has+String, ReasonCode String, RCBType String+FC, isRCBItemID, OptFlds/TrgOps encode/decode roundtrip
- RCB tests: ListReports (filtering), GetReportControlBlock for BRCB and URCB, SetReportControlBlock, closed client guards
- Report decode tests: basic decode with SeqNum + ReasonCode, partial inclusion, empty values error
- Subscription tests: end-to-end via Broadcast, idempotent Close, closed client guard, empty RptID validation

### Deferred items

1. `CreateDataSet` / `DeleteDataSet` — dynamic dataset creation/deletion
2. `ReadInto` for typed dataset member reads
3. `ReportSubscription` auto-enable RCB (configure → enable flow)
4. GI request via subscription
5. Reserve/owner for URCB
6. Report timestamp decoding (BinaryTime format from go-mms)
7. Segmented report reassembly
8. Queue overflow callback/policy configuration
9. Model cache for browse results (noted in M2 feedback)

### Open questions

1. Should `SubscribeReport` accept a glob/pattern for RptID matching, or should each subscription target exactly one RptID?
2. Should `SetReportControlBlock` support atomic all-or-nothing semantics via a transaction pattern, or is sequential per-field write acceptable?
3. Should `GetDataSet` cache member definitions to avoid re-fetching on every `ReadDataSet`?

---

## M3 Hardening — Feedback pass

**Date**: 2026-03-19

### Feedback items addressed

| # | Item | Changes |
|---|------|---------|
| 1 | **Subscription init race/panic risk** | `Client.Close` and `Client.Abort` now call `closeAllSubscriptions()` which atomically drains the subscription map, closes all channels, and marks every subscription as closed. `registerSubscription` and `unregisterSubscription` are nil-map safe. |
| 2 | **Dead code in GetReportControlBlock** | Removed unused `objName` variable and `_ = objName` statement. |
| 3 | **RptEna-last invariant** | Explicit code comments above the RptEna append and a separator between config writes and the final RptEna write make the ordering invariant impossible to miss. |
| 4 | **ReportSubscription.Close race with dispatch** | Close now sets `closed = true`, releases `sub.mu`, unregisters from the client map (under client lock), then closes the channel. Dispatch holds `sub.mu` across the channel send, so no send-on-closed-channel is possible. |
| 5 | **decodeRCB under-validated** | Added `minBRCBElements` (13) and `minURCBElements` (11) constants. `decodeRCB` returns `*ReportError` when structure length is below the minimum. |
| 6 | **ReadDataSet member identity in errors** | New `memberIdentity()` helper produces the best available identity (IEC ref, then domain/itemID, then index) for per-member error messages. |
| 7 | **variableSpecToMember fallback** | On `RefFromMMS` failure, `Ref` stays zero-value instead of creating a misleading partial Ref with only LD set. Raw `DomainID`/`ItemID` remain available. |
| 8 | **Malformed report decode tests** | Added: `TestDecodeReportIndication_MissingInclusion`, `_SeqNumMissing`, `_ReasonCodeTooFew`, `_DataRefTooFew`, `TestDecodeRCB_ShortBRCB`, `TestDecodeRCB_ShortURCB`. |
| 9 | **ListReports heuristic docs** | `ListReports` and `isRCBItemID` doc comments now explicitly state that discovery is by name-pattern heuristic, not semantic verification. |
| 10 | **Subscription hardening tests** | Added: `TestSubscribeReport_QueueOverflow`, `TestSubscribeReport_MultipleRptIDs`, `TestClientClose_ShutsAllSubscriptions`. |

### Files modified

- `client.go` — `closeAllSubscriptions()`, `Close`/`Abort` call it
- `report.go` — items 2–5, 9; race-safe dispatch; nil-map guards
- `dataset.go` — items 6–7; `memberIdentity()`, `variableSpecToMember` fallback
- `report_test.go` — items 8, 10; 9 new test functions

### Test summary

- **162** top-level tests passing
- Race-detector clean
- 0 linter issues

### Status

M3 hardened and locked.

---

## Phase M4 — Files, journals, and SCL baseline

**Date**: 2026-03-19

### Deliverables

#### New files

| File | Purpose |
|------|---------|
| `file.go` | File service wrappers (`ListFiles`, `ReadFile`, `DownloadFile`, `DeleteFile`) |
| `file_test.go` | File service loopback tests with in-memory `FileProvider` |
| `journal.go` | Journal wrappers (`ListJournals`, `ReadJournal`, `ReadJournalAfter`) |
| `journal_test.go` | Journal loopback tests with in-memory `JournalProvider` |
| `scl/model.go` | SCL data model types (SCL, IED, LDevice, LN, DataSet, ReportControl, DataTypeTemplates, etc.) |
| `scl/parse.go` | SCL XML parser (SCD/ICD/CID/IID formats) |
| `scl/flatten.go` | Flatten to rows (`FlatRow`), `WriteCSV`, `PrintTree` |
| `scl/parse_test.go` | SCL parsing tests against fixture file |
| `scl/flatten_test.go` | Flatten and export tests |
| `scl/testdata/simple.scd` | Test fixture with IED, LD, LN0, GGIO, DataSets, RCBs, DataTypeTemplates |

### API summary

#### File operations

| Method | Description |
|--------|-------------|
| `Client.ListFiles(ctx, pattern)` | List files matching a pattern (wraps `FileDirectoryAll`) |
| `Client.ReadFile(ctx, fileName)` | Read entire file into memory (wraps `DownloadFile`) |
| `Client.DownloadFile(ctx, fileName, w)` | Stream file to `io.Writer` (open → read loop → close) |
| `Client.DeleteFile(ctx, fileName)` | Delete a file on the server |

**Types**: `FileEntry` (Name, Size, LastModified)

#### Journal operations

| Method | Description |
|--------|-------------|
| `Client.ListJournals(ctx, ld)` | List journal names via `GetNameList` with `ObjectClassJournal` |
| `Client.ReadJournal(ctx, ld, journal, start, stop)` | Read entries by time range |
| `Client.ReadJournalAfter(ctx, ld, journal, afterTime, afterID)` | Page through entries using cursor |

**Types**: `JournalEntry` (EntryID, OccurrenceTime, Variables), `JournalVariable` (Tag, Value), `JournalReadResult` (Entries, MoreFollows)

#### SCL package (`scl/`)

| Function | Description |
|----------|-------------|
| `Parse(r io.Reader)` | Parse SCL XML from reader |
| `ParseFile(path)` | Parse SCL XML from file path |
| `Flatten(s *SCL)` | Expand model into flat `[]FlatRow` (one per leaf DA) |
| `WriteCSV(w, rows)` | Write flat rows as CSV |
| `PrintTree(w, s)` | Write hierarchical text tree |

**Model types**: `SCL`, `Header`, `IED`, `AccessPoint`, `Server`, `LDevice`, `LN`, `DOI`, `SDI`, `DAI`, `DataSet`, `FCDA`, `ReportControl`, `TrgOps`, `OptFields`, `Log`, `DataTypeTemplates`, `LNodeType`, `DOType`, `DAType`, `EnumType`, `DO`, `SDO`, `DA`, `BDA`, `EnumVal`, `FlatRow`

### Design decisions

1. **File wrappers**: `DownloadFile` streams to `io.Writer` chunk by chunk (open → read → close FRSM loop), avoiding full buffering for large files. `ReadFile` is the convenience all-in-memory alternative.
2. **Journal wrappers**: `JournalVariable.Value` wraps the MMS value in the IEC 61850 `*Value` type for consistent API. `ListJournals` uses `GetNameList` with `ObjectClassJournal` since there is no dedicated journal listing in `go-mms` client API.
3. **SCL parser**: Uses `encoding/xml` with separate XML mapping types (`xml*` structs with struct tags) and public model types (tag-free). Conversion validates numeric fields (confRev, bufTime, etc.) and returns errors for malformed values.
4. **SCL flatten**: Recursively expands LNodeType → DOType → DA/SDO → DAType chains using type template indexes. Struct-typed DAs are expanded through DAType BDAs. Missing type references produce rows with available metadata rather than errors.
5. **SCL tree**: `PrintTree` outputs a human-readable hierarchy showing IED → AP → LD → LN with DataSets (member count) and ReportControls (BRCB/URCB).
6. **Test approach**: File and journal tests use in-memory provider implementations (`memFileProvider`, `memJournalProvider`) registered on `mms.Server` via `ServerOptions`. SCL tests use a comprehensive fixture file (`testdata/simple.scd`).

### Test summary

- **195** top-level tests passing (162 existing + 33 new)
- Race-detector clean
- 0 linter issues

### Deferred items

1. SCL `Communication` section parsing (GSE/SMV addresses)
2. SCL `Substation` section parsing (topology)
3. SCL validation (cross-referencing types, checking required attributes) — basic required-field validation now in place
4. SCL round-trip / generation (write back to XML)
5. File upload (`ObtainFile`, `SetFile`)
6. Journal auto-pagination helper (follow `MoreFollows` automatically)
7. SCL `Services` section parsing

### Open questions

1. Should `Flatten` include data set members and report definitions as additional row types, or stay limited to leaf data attributes?
2. Should the SCL package provide lookup helpers (e.g., `FindIED`, `FindLDevice`, `FindLNodeType`) or leave navigation to the caller?
3. Should `DownloadFile` accept progress callback for large file transfers?

---

## M4 Hardening — Feedback pass

**Date**: 2026-03-19

Addressed all 10 feedback items from M4 review.

### Changes made

| # | Feedback | File(s) | Change |
|---|----------|---------|--------|
| 1 | File listing determinism | `file_test.go` | `memFileProvider.List` now sorts entries by name before returning |
| 2 | DownloadFile short write check | `file.go` | After `w.Write`, verify `n == len(chunk.Data)`, return `io.ErrShortWrite` if not |
| 3 | Journal paging afterID | `journal_test.go` | `memJournalProvider.ReadStartAfter` now disambiguates same-timestamp entries by `afterID` using `bytes.Equal` |
| 4 | convertJournalResult nil-guard | `journal.go` | Return empty `JournalReadResult` when input is nil |
| 5 | PrintTree error threading | `scl/flatten.go` | `printLNs` and `printLN` now return `error`; `PrintTree` propagates all writer failures |
| 6 | CSV encoding/csv | `scl/flatten.go` | Replaced manual escaping with stdlib `encoding/csv.Writer`; removed `csvEscape` helper |
| 7 | SCL bool parsing | `scl/parse.go` | Added `parseBool` helper that normalises case/whitespace, accepts `"true"`/`"false"` (case-insensitive). All boolean attribute parsing in `convertReportControl`, `convertTrgOps`, `convertOptFields` now uses `parseBool` |
| 8 | SCL required-field validation | `scl/parse.go` | Added validation: `IED.Name` non-empty, `LDevice.Inst` non-empty, `LN.LNClass` non-empty, `LN.LNType` non-empty, `DO.Name` non-empty, `DO.Type` non-empty |
| 9 | EnumVal.Desc → Value | `scl/model.go`, `scl/parse.go`, `scl/parse_test.go` | Renamed `EnumVal.Desc` to `EnumVal.Value` to correctly represent the character data content |
| 10 | Negative tests | `file_test.go`, `journal_test.go`, `scl/parse_test.go`, `scl/flatten_test.go` | Added: `TestDownloadFile_ShortWrite`, `TestDownloadFile_WriterError`, `TestReadJournal_EmptyResult`, `TestConvertJournalResult_Nil`, `TestParse_InvalidConfRev`, `TestParse_InvalidBufTime`, `TestParse_InvalidDACount`, `TestParse_MissingIEDName`, `TestParse_MissingLDeviceInst`, `TestParse_MissingLNClass`, `TestParse_MissingLNType`, `TestParseBool_Variants`, `TestPrintTree_WriterFailure`, `TestWriteCSV_WriterFailure` |

### Test summary

- **209 tests passing** (up from 195), race-detector clean
- **0 linter issues** (`golangci-lint run ./...`)

### Status

M4 hardened and locked. Ready to proceed to next phase.

---

## M4 Hardening — Final review

**Date**: 2026-03-19

Addressed all 4 remaining feedback items from final M4 review.

### Changes made

| # | Feedback | File(s) | Change |
|---|----------|---------|--------|
| 1 | Journal cursor semantics bug | `journal_test.go` | Rewrote `memJournalProvider.ReadStartAfter` to correctly skip the exact cursor entry `(afterTime, afterID)` and collect all subsequent entries — including those at the same timestamp with different EntryIDs. Added `TestReadJournalAfter_SameTimestamp` with 3 entries sharing one timestamp to validate same-timestamp pagination |
| 2 | Sort ListFiles in production | `file.go` | `Client.ListFiles` now sorts results by `Name` for stable, deterministic output regardless of server ordering |
| 3 | SCL invalid boolean integration tests | `scl/parse_test.go` | Added `TestParse_InvalidBuffered` (invalid `buffered` attr), `TestParse_InvalidTrgOps` (invalid `TrgOps.dchg`), `TestParse_InvalidOptFields` (invalid `OptFields.seqNum`) — all verify error plumbing from `parseBool` through the full parse pipeline |
| 4 | Document nil-result trade-off | `journal.go` | Added doc comment to `convertJournalResult` explaining the nil-guard rationale and noting that callers may want explicit error handling in the future |

### Test summary

- **213 tests passing** (up from 209), race-detector clean
- **0 linter issues** (`golangci-lint run ./...`)

### Status

M4 fully hardened and locked. Ready to proceed to next phase.

---

## Phase M5 — Browse, cache, tree, and read API completion

**Date**: 2026-03-19

Completed all 7 sub-tasks of Phase M5, adding client-side caching, tree options with FC annotation, regex find support, a ReadComponent API, explicit LN-level read semantics, and mixed-domain write documentation/tests.

### Files added

| File | Purpose |
|------|---------|
| `cache.go` | Client-side model cache (modelCache struct, Refresh/Invalidate APIs, fetch helpers) |
| `cache_test.go` | Tests for all 3 cache strategies + refresh/invalidate |

### Files changed

| File | Change |
|------|--------|
| `options.go` | Added `CacheStrategy` type with `CacheNone` / `CacheExplicit` / `CacheLazy` constants; added `Cache` field to `ClientOptions` and `DialOptions` |
| `client.go` | Added `cache *modelCache` and test hook fields (`fetchLDsFn`, `fetchItemsFn`) to `Client` struct; initialize cache in `Dial` and `NewClient` |
| `model.go` | Added `FCs []FunctionalConstraint` to `ModelNode`; updated `FC` doc comment; added `MatchMode` type with `MatchGlob` / `MatchRegex` constants; added `MatchMode` field to `FindQuery` |
| `browse.go` | Refactored all browse methods to use `cachedLDs` / `cachedItems` instead of direct MMS calls; `Tree()` now delegates to `TreeWithOptions(ctx, TreeOptions{})`; added `TreeWithOptions` with `TreeOptions` (LDFilter, MaxDepth, IncludeFCs); refactored tree builders (`buildLDTreeWithOptions`, `buildSubTreeWithOptions`) with options support; `FindPaths` now uses `matcher` abstraction supporting glob and regex; added `toFCs`, `matcher` helpers |
| `read.go` | Updated `Read` / `ReadRaw` doc comments for explicit LN-level support; added `ReadComponent(ctx, ref, component)` convenience API |
| `bulk.go` | Updated `ReadMultiple` / `WriteMultiple` doc comments to explicitly document mixed-domain/mixed-FC support |
| `internal/mapping/names.go` | Added `ExtractFCsForLN` and `ExtractFCsForPath` helpers |

### Tests added

| Test | File |
|------|------|
| `TestCacheNone_AlwaysFetches` | `cache_test.go` |
| `TestCacheLazy_CachesAfterFirstFetch` | `cache_test.go` |
| `TestCacheExplicit_CachesUntilInvalidate` | `cache_test.go` |
| `TestCacheLazy_ItemsCached` | `cache_test.go` |
| `TestCacheExplicit_RefreshLDCache` | `cache_test.go` |
| `TestCacheExplicit_RefreshCache` | `cache_test.go` |
| `TestInvalidateCache_Noop_WhenNone` | `cache_test.go` |
| `TestRefreshCache_Noop_WhenNone` | `cache_test.go` |
| `TestRefreshLDCache_EmptyLD` | `cache_test.go` |
| `TestInvalidateLDCache` | `cache_test.go` |
| `TestFindPaths_Regex` | `browse_test.go` |
| `TestFindPaths_InvalidRegex` | `browse_test.go` |
| `TestTreeWithOptions_LDFilter` | `browse_test.go` |
| `TestTreeWithOptions_LDFilter_NoMatch` | `browse_test.go` |
| `TestTreeWithOptions_MaxDepth1` | `browse_test.go` |
| `TestTreeWithOptions_MaxDepth2` | `browse_test.go` |
| `TestTreeWithOptions_MaxDepth3` | `browse_test.go` |
| `TestTreeWithOptions_IncludeFCs` | `browse_test.go` |
| `TestTreeWithOptions_IncludeFCs_MultiFCNode` | `browse_test.go` |
| `TestReadComponent` | `read_test.go` |
| `TestReadComponent_EmptyComponent` | `read_test.go` |
| `TestReadComponent_NotObject` | `read_test.go` |
| `TestReadComponent_InvalidComponent` | `read_test.go` |
| `TestRead_LNLevel` | `read_test.go` |
| `TestWriteMultiple_MixedFCs` | `bulk_test.go` |
| `TestReadMultiple_MixedFCs` | `bulk_test.go` |
| `TestExtractFCsForLN` | `internal/mapping/names_test.go` |
| `TestExtractFCsForPath` | `internal/mapping/names_test.go` |

### APIs added

| API | Description |
|-----|-------------|
| `CacheStrategy` | Type with `CacheNone`, `CacheExplicit`, `CacheLazy` constants |
| `ClientOptions.Cache` | Cache strategy selection |
| `DialOptions.Cache` | Cache strategy selection |
| `Client.RefreshCache(ctx)` | Re-fetch all cached model data |
| `Client.RefreshLDCache(ctx, ld)` | Re-fetch cached data for a single LD |
| `Client.InvalidateCache()` | Clear all cached data |
| `Client.InvalidateLDCache(ld)` | Clear cached data for a single LD |
| `TreeOptions` | Options for `TreeWithOptions` (LDFilter, MaxDepth, IncludeFCs) |
| `Client.TreeWithOptions(ctx, opts)` | Build model tree with filtering/annotation options |
| `ModelNode.FCs` | All FCs observed for a node (populated when IncludeFCs is true) |
| `MatchMode` | Pattern matching mode (`MatchGlob`, `MatchRegex`) |
| `FindQuery.MatchMode` | Select glob vs regex matching |
| `Client.ReadComponent(ctx, ref, component)` | Read a named sub-attribute of a structure |

### Design decisions

1. **Cache is internal**: `modelCache` is unexported. The public API is only the strategy selection and refresh/invalidate methods. Cache correctness > micro-optimization.
2. **Tree() delegates to TreeWithOptions**: `Tree(ctx)` is now equivalent to `TreeWithOptions(ctx, TreeOptions{})`, preserving backward compatibility while making the full API available.
3. **FC annotation uses separate FCs slice**: When multiple FCs apply (common for DOs containing attributes in ST, MX, CF, etc.), the `FCs` field holds all of them. The single `FC` field is set only when exactly one FC is observed. This avoids misleading single-FC assignment.
4. **Matcher abstraction**: Both glob and regex use a shared `matcher` type for clean separation from `FindPaths` logic. Invalid patterns fail clearly at call time.
5. **ReadComponent is convenience**: `ReadComponent(ctx, ref, "stVal")` is equivalent to reading with `ref.Child("stVal")`. It exists to avoid forcing callers to construct child refs manually.
6. **LN-level reads explicit**: The client explicitly allows refs with LN + FC but no path. Server support varies; the contract is documented.
7. **Mixed-domain writes documented**: `WriteMultiple` and `ReadMultiple` doc comments now explicitly state mixed-domain/FC support.

### Test summary

- **241 tests passing** (up from 213), race-detector clean
- **0 linter issues** (`golangci-lint run ./...`)

### Status

M5 complete. Ready for review.

---

## Phase M6 — Datasets and reports completion

### Objectives

Make datasets and reports functionally complete: dynamic datasets, lifecycle-aware subscriptions, GI, URCB ownership, full timestamp support, segmented report reassembly, configurable overflow policy, richer subscription matching, documented write semantics, and dataset definition caching.

### Files changed / added

| File | Change |
|------|--------|
| `dataset.go` | Added `CreateDataSet`, `DeleteDataSet`; integrated DS caching into `GetDataSet` |
| `report.go` | Added `TriggerGI`, `ReserveURCB`, `ReleaseURCB`; `OverflowPolicy` enum; lifecycle options in `SubscribeReportOptions`; `RptMatchMode` enum with glob support; segmented report reassembly; `Timestamp` field on `ReportIndication`; full timestamp decoding; `SetReportControlBlock` doc comments updated for write ordering semantics |
| `cache.go` | Added dataset caching (`cachedDS`, `invalidateDS`, `getDS`, `setDS`); integrated with `invalidateAll`/`invalidateLD` |
| `timestamp.go` | `DecodeTimestamp` now decodes full `TimeQuality` from go-mms `UTCTimeQuality()` byte |
| `dataset_test.go` | Added 8 tests: `TestCreateDataSet`, `TestDeleteDataSet`, `TestCreateDataSet_EmptyLD`, `TestCreateDataSet_NoMembers`, `TestDeleteDataSet_EmptyName`, `TestCreateDataSet_WithRawDomainItemID`, `TestGetDataSet_Cached` |
| `report_test.go` | Added 12 tests: `TestTriggerGI`, `TestTriggerGI_ClosedClient`, `TestReserveReleaseURCB`, `TestSubscribeReport_OverflowDropOldest`, `TestSubscribeReport_OverflowCallback`, `TestDecodeReportIndication_Timestamp`, `TestSubscribeReport_GlobMatch`, `TestSubscribeReport_GlobNoMatch`, `TestSegmentedReportReassembly`, `TestSubscribeReport_AutoEnable`, `TestSubscribeReport_GIOnSubscribe`, `TestSubscribeReport_ReserveAndEnable`, `TestSubscribeReport_AutoEnable_MissingLD` |

### New APIs

#### Dynamic datasets (M6.1)

- `Client.CreateDataSet(ctx, ld, dsName, members)` — creates a dynamic (deletable) NVL on the server via `DefineNamedVariableList`. Validates member refs and supports both IEC Ref-based and raw DomainID/ItemID member specification.
- `Client.DeleteDataSet(ctx, ld, dsName)` — deletes a dynamic data set via `DeleteNamedVariableList`. Returns error if the server did not delete.

#### Subscription lifecycle-awareness (M6.2)

Extended `SubscribeReportOptions` with lifecycle fields:
- `AutoEnable` — enables the RCB (writes RptEna=true) as part of subscribing
- `GIOnSubscribe` — triggers a General Interrogation after enabling
- `ReserveURCB` — reserves the URCB before enabling
- `LD` / `RCBItemID` — required when any lifecycle option is set

The user can still configure RCBs manually and subscribe passively by not setting any lifecycle options.

#### First-class GI (M6.3)

- `Client.TriggerGI(ctx, ld, rcbItemID)` — writes GI=true to the RCB via `SetReportControlBlock`

#### URCB reserve/release (M6.4)

- `Client.ReserveURCB(ctx, ld, rcbItemID)` — writes Resv=true
- `Client.ReleaseURCB(ctx, ld, rcbItemID)` — writes Resv=false

#### Full report timestamp decoding (M6.5)

- `ReportIndication.Timestamp` — decoded from UTCTime or BinaryTime in reports (was previously skipped)
- `DecodeTimestamp` — now decodes `TimeQuality` (LeapSecondKnown, ClockFailure, ClockNotSynchronized, TimeAccuracy) from the go-mms `UTCTimeQuality()` byte

#### Segmented report reassembly (M6.6)

- `ReportSubscription` internally buffers segmented reports (OptFldSegmentation + MoreSegments)
- Segments with the same SeqNum are collected; when MoreSegments=false arrives, all segments are assembled into a single `ReportIndication` with merged Inclusion, Values, ReasonCodes, and DataReferences
- Mismatched sequence numbers reset the buffer and start fresh

#### Queue overflow policy (M6.7)

`OverflowPolicy` enum with four strategies:
- `OverflowDropNewest` (default) — drops the incoming report, logs a warning
- `OverflowDropOldest` — evicts the oldest buffered report to make room
- `OverflowBlock` — blocks the dispatcher until space is available
- `OverflowCallback` — invokes `OnOverflow` callback if set, otherwise drops

Set via `SubscribeReportOptions.OverflowPolicy` and `OnOverflow`.

#### SubscribeReport matching modes (M6.8)

`RptMatchMode` enum:
- `RptMatchExact` (default) — exact string match
- `RptMatchGlob` — `path.Match` glob semantics (`*` matches any chars, `?` one char)

Set via `SubscribeReportOptions.MatchMode`. The dispatch engine tries exact key lookup first, then falls back to glob matching across subscriptions.

#### Sequential SetReportControlBlock semantics (M6.9)

`SetReportControlBlock` doc comments now explicitly document:
- Sequential per-field writes (no fake atomicity promises)
- RptEna always written last when included in the field mask
- Write ordering invariant preserved for IEC 61850 compliance

#### Dataset definition caching (M6.10)

Integrated with the M5 cache system:
- `modelCache` extended with `dsByLD` map for cached dataset definitions
- `GetDataSet` checks cache before fetching; caches results when caching is enabled
- `CreateDataSet` and `DeleteDataSet` invalidate the DS cache for the affected LD
- `InvalidateCache` / `InvalidateLDCache` / `RefreshCache` all clear DS entries

### Design decisions

1. **TriggerGI / ReserveURCB / ReleaseURCB are thin wrappers**: They delegate to `SetReportControlBlock` with the appropriate field mask. This keeps the write-ordering logic centralized and testable.
2. **Lifecycle options are opt-in**: The default `SubscribeReportOptions{}` still creates a passive subscription (no RCB modification). Users must explicitly set `AutoEnable`, `GIOnSubscribe`, or `ReserveURCB` to get lifecycle management.
3. **Segmented report reassembly is per-subscription**: Each `ReportSubscription` maintains its own segment buffer. Mismatched sequence numbers reset the buffer — partial/failed reassembly is handled by discarding stale segments and starting fresh.
4. **OverflowDropOldest uses drain loop**: When the channel is full, the oldest entry is drained in a loop until the new entry fits. This is safe because the subscription mutex is held during delivery.
5. **Glob matching falls through**: Exact key lookup is tried first for O(1) performance. Glob matching iterates subscriptions only when no exact match exists.
6. **Dataset caching is per-LD per-name**: Individual dataset definitions are cached independently (not batch-invalidated except via `InvalidateCache`/`InvalidateLDCache`), allowing fine-grained cache control.

### Test summary

- **261 tests passing** (up from 241), race-detector clean
- **0 linter issues** (`golangci-lint run ./...`)

### Status

M6 complete. Ready for review.

---

## Phase M7 — Files and journals completion

### Objectives

Complete missing file/journal client features: file upload symmetry (ObtainFile, RenameFile, GetFileAttributes) and journal convenience auto-pagination helpers.

### Files changed

| File | Change |
|------|--------|
| `file.go` | Added `RenameFile`, `ObtainFile`, `GetFileAttributes` |
| `journal.go` | Added `ReadJournalAll`, `ReadJournalAfterAll` |
| `file_test.go` | Added 11 tests for new file APIs |
| `journal_test.go` | Added 6 tests for journal auto-pagination (with paginating provider) |

### New APIs

#### File operations (M7.1)

- `Client.RenameFile(ctx, currentName, newName)` — renames a file on the server via MMS `FileRename`.
- `Client.ObtainFile(ctx, sourceFile, destinationFile)` — instructs the server to copy a file (MMS ObtainFile). This is the standard MMS mechanism for "uploading" — the server pulls the source file. MMS does not define a direct client-to-server file write (push) operation.
- `Client.GetFileAttributes(ctx, fileName)` — retrieves file metadata (size, last modified) without reading contents. Opens and immediately closes the file to obtain the `FileOpenResult` attributes.

#### Journal auto-pagination (M7.2)

- `Client.ReadJournalAll(ctx, ld, journal, start, stop)` — reads all journal entries in a time range, automatically following `MoreFollows` pagination. Uses `ReadJournal` for the first page and `ReadJournalAfter` for subsequent pages, chaining via the last entry's `OccurrenceTime` and `EntryID`.
- `Client.ReadJournalAfterAll(ctx, ld, journal, afterTime, afterID)` — reads all journal entries after a cursor, auto-paginating. Same chaining logic as `ReadJournalAll`.

Both methods:
- Preserve chronological order
- Handle same-timestamp cursor semantics correctly (EntryID-based continuation)
- Stop when `MoreFollows=false` or empty page returned
- Return all accumulated entries in a single slice

### Design decisions

1. **No direct file push**: MMS protocol does not define a client→server file write operation. `ObtainFile` is the closest equivalent where the server fetches a source file. This is documented in the API.
2. **GetFileAttributes uses open+close**: Since MMS `FileOpen` returns size and modification time, opening and immediately closing is the most reliable way to get file attributes without reading content.
3. **Journal pagination stops on empty page**: In addition to checking `MoreFollows=false`, the pagination loop also breaks on empty pages to prevent infinite loops if a server incorrectly sets `MoreFollows=true` with no data.
4. **Low-level paging preserved**: `ReadJournal` and `ReadJournalAfter` remain available for callers who want explicit control over memory usage during large journal reads.

### Test summary

- **278 tests passing** (up from 261), race-detector clean
- **0 linter issues** (`golangci-lint run ./...`)
- New tests include a `paginatingJournalProvider` that forces page size to verify multi-page auto-pagination correctness

### Status

M7 complete. Ready for review.

---

## Phase M8 — SCL serious-tooling completion

### M8.1: Communication parsing

**Files changed:** `scl/model.go`, `scl/parse.go`, `scl/testdata/simple.scd`

- Added `Communication`, `SubNetwork`, `ConnectedAP`, `P`, `GSEAddress`, `SMVAddress` model types
- Added XML mapping types: `xmlCommunication`, `xmlSubNetwork`, `xmlConnectedAP`, `xmlAddress`, `xmlP`, `xmlGSE`, `xmlSMV`
- Implemented `convertCommunication` and `convertPs` conversion functions
- `SCL.Communication` field added to root model
- Parses GSE addressing (MAC-Address, APPID, VLAN, MinTime, MaxTime)
- Parses SMV addressing (MAC-Address, APPID, VLAN)
- Parses ConnectedAP address parameters (IP, subnet, gateway, OSI selectors)

### M8.2: Substation parsing

**Files changed:** `scl/model.go`, `scl/parse.go`, `scl/testdata/simple.scd`

- Added `Substation`, `VoltageLevel`, `Bay`, `ConductingEquipment` model types
- Added XML mapping types: `xmlSubstation`, `xmlVoltageLevel`, `xmlBay`, `xmlConductingEquipment`, `xmlVal`
- Implemented `convertSubstation` conversion function
- `SCL.Substations` field added to root model
- Parses voltage level values and bay topology with conducting equipment (CBR, DIS, etc.)

### M8.3: Semantic validation

**New file:** `scl/validate.go`

- Introduced `ValidationSeverity` enum (`SeverityError`, `SeverityWarning`)
- Introduced `ValidationFinding` struct implementing `error` interface
- `Validate(s *SCL) []ValidationFinding` performs non-fail-fast validation:
  - **Type template chains:** LNodeType→DOType, DOType→DA(Struct→DAType, Enum→EnumType), DOType→SDO→DOType, DAType→BDA refs
  - **IED references:** LN→LNodeType existence
  - **Report-dataset references:** ReportControl.datSet against datasets in same LN (warning)
  - **Communication cross-refs:** ConnectedAP.iedName and apName against defined IEDs/APs

### M8.4: Round-trip / XML generation

**New file:** `scl/generate.go`

- `Generate(w io.Writer, s *SCL) error` writes SCL model as XML
  - Deterministic output (follows model slice ordering)
  - Preserves SCL namespace (`http://www.iec.ch/61850/2003/SCL`)
  - Pretty-printed with 2-space indentation
- Full round-trip coverage: Header, Substation, Communication, IED (AccessPoints, LDevices, LNs, DOIs/SDIs/DAIs, DataSets, ReportControls, LogControls), DataTypeTemplates (LNodeType, DOType, DAType, EnumType)
- Uses type conversions (S1016 compliant) where model and XML types share identical fields

### M8.5: Services parsing

**Files changed:** `scl/model.go`, `scl/parse.go`, `scl/testdata/simple.scd`

- Added `Services`, `ConfDataSet`, `ConfReportControl`, `ReportSettings`, `ConfLNs`, `GOOSEService`, `SMVService` model types
- Added XML mapping types with `*struct{}` for presence-only service flags
- `IED.Services` field added
- Implements `convertServices` with full numeric/boolean parsing and error propagation
- Parses: DynAssociation, GetDirectory, GetDataObjectDefinition, GetDataSetValue, DataSetDirectory, ReadWrite, GetCBValues, FileHandling, ConfDataSet, ConfReportControl, ReportSettings, ConfLNs, GOOSE, SMVsc

### M8.6: Flatten scope — dataset/report export helpers

**Files changed:** `scl/flatten.go`

- `ExportDataSets(s *SCL) []DataSetRow` — extracts all dataset definitions with IED/AP/LD/LN location and member count
- `ExportReports(s *SCL) []ReportRow` — extracts all report control blocks with IED/AP/LD/LN location, RptID, buffered flag, and ConfRev
- Both are separate, explicit helpers that complement the existing leaf-DA `Flatten`

### M8.7: SCL lookup helpers

**New file:** `scl/lookup.go`

- `SCL.FindIED(name) *IED`
- `SCL.FindLDevice(inst) *LDevice` — searches across all IEDs
- `IED.FindLDevice(inst) *LDevice`
- `LDevice.FindLN(prefix, lnClass, inst) *LN` — handles both LN0 and regular LNs
- `SCL.FindLNodeType(id) *LNodeType`
- `SCL.FindDOType(id) *DOType`
- `SCL.FindDAType(id) *DAType`
- `SCL.FindEnumType(id) *EnumType`

All helpers are small, direct, deterministic linear scans returning pointers into the existing model.

### Test data

**Updated:** `scl/testdata/simple.scd`

- Extended with `<Substation>` section (1 substation, 1 voltage level, 2 bays, 3 conducting equipments)
- Extended with `<Communication>` section (1 SubNetwork with ConnectedAP, GSE, SMV addressing)
- Extended with `<Services>` section (14 service declarations)

### Test summary

- **303 tests passing** (up from 278 in M7), race-detector clean
- **0 linter issues** (`golangci-lint run ./...`)
- **25 new SCL tests** covering:
  - Communication parsing (SubNetwork, ConnectedAP, GSE, SMV)
  - Substation parsing (topology, voltage levels, bays, equipment)
  - Services parsing (all 14 service categories)
  - Semantic validation (valid SCL, missing DOType, missing LNodeType, missing DAType, missing EnumType, bad CommRef)
  - Round-trip generation (parse → generate → re-parse fidelity)
  - Export helpers (datasets, reports)
  - Lookup helpers (FindIED, FindLDevice, FindLN, FindLNodeType, FindDOType, FindDAType, FindEnumType)

### Status

M8 complete. Ready for review.

---

## Post-M8 Feedback Round

### Feedback Items Implemented

All 14 feedback items addressed (10 code fixes + 4 smaller improvements):

#### High-value fixes (F1–F5)

1. **F1 — CacheExplicit semantics fixed**
   - `CacheExplicit` no longer auto-populates on first access (was identical to `CacheLazy`).
   - `cachedLDs()`, `cachedItems()`, and `GetDataSet()` only write to cache when `strategy == CacheLazy`.
   - `CacheExplicit` requires explicit `RefreshCache()` to populate; subsequent reads use cache; `InvalidateCache()` clears it.
   - `newModelCache()` now takes the strategy as a parameter.
   - Existing test replaced with `TestCacheExplicit_DoesNotAutoPopulate` and `TestCacheExplicit_CachesAfterRefresh`.

2. **F2 — DataSet cache deep-copy**
   - `modelCache.getDS()` and `setDS()` now deep-copy the `*DataSet` (via `copyDataSet()`) so callers cannot mutate cached data.
   - New test `TestGetDataSet_CachedMutationSafe` verifies mutating a returned `*DataSet` does not affect subsequent cache reads.

3. **F3 — Multi-subscriber report dispatch**
   - `reportSubs` changed from `map[string]*ReportSubscription` to `map[string][]*ReportSubscription`.
   - `registerSubscription()` appends to the slice; `unregisterSubscription()` removes by pointer identity.
   - `findSubscriptions()` (renamed from `findSubscription`) returns all matching subscribers (exact-key + glob fan-out).
   - `handleInformationReport()` dispatches to every matching subscriber, not just the first.
   - `closeAllSubscriptions()` iterates over the nested slice structure.
   - New tests: `TestSubscribeReport_MultipleExactSameID` and `TestSubscribeReport_ExactAndGlobBothReceive`.

4. **F4 — EncodeTimestamp with quality byte**
   - `EncodeTimestamp()` now uses `mms.NewUTCTimeWithQuality(ts.Time, qb)` with full quality byte encoding (leap-second, clock failure, clock sync, time accuracy bits).
   - Added `encodeTimeQuality()` helper.
   - Updated `Timestamp` struct doc comment (removed outdated "quality not preserved" text).
   - New tests: `TestTimestamp_QualityRoundtrip` and `TestTimestamp_QualityAllBits`.

5. **F5 — SCL Services generation**
   - Added `servicesToXML()` in `scl/generate.go` — converts all `Services` fields (DynAssociation, GetDirectory, ReadWrite, FileHandling, ConfDataSet, ConfReportCtrl, ReportSettings, ConfLNs, GOOSE, SMVsc) to their XML mapping types.
   - `iedToXML()` now writes `Services` into `xmlIED`.
   - New test: `TestGenerate_Services_Roundtrip` verifies Services survive parse→generate→re-parse.

#### Medium-priority fixes (F6–F10)

6. **F6 — OverflowBlock lock release**
   - `deliver()` for `OverflowBlock` now releases `sub.mu` before the blocking channel send, so `Close()`/shutdown cannot deadlock against a full channel.
   - All other overflow policies also use explicit unlock rather than `defer` for consistent lock management.

7. **F7 — Segmented report logging**
   - `handleSegment()` now logs warnings for: buffer resets (new sequence started while previous was in-flight), sequence mismatches (SeqNum change mid-segment), and non-contiguous `SubSeqNum` gaps.
   - Uses `s.client.logger.Warn()` for all segment anomalies.

8. **F8 — ReadDataSet uses cached definition**
   - `ReadDataSet()` now checks `cache.getDS()` first to reuse member definitions, avoiding a redundant `GetNamedVariableListAttributes` round-trip when the dataset definition is already cached.

9. **F9 — CreateDataSet stronger validation**
   - When deriving MMS names from `Ref`, now requires `LN != ""`, `FC != ""`, and `HasPath()` (rejects LN-only refs).
   - New tests: `TestCreateDataSet_RejectsLNOnlyRef` and `TestCreateDataSet_RejectsMissingFC`.

10. **F10 — checkOpen RLock; RefreshCache atomic swap; deeper SCL validation**
    - `checkOpen()` changed from `mu.Lock()` to `mu.RLock()` (with `sync.Mutex` → `sync.RWMutex` on Client).
    - `RefreshCache()` now builds a complete snapshot (LDs + items) into temp variables and swaps atomically under a single lock, so a mid-refresh failure preserves the previous cache state.
    - SCL `validateCommunication()` now cross-checks GSE/SMV `LDInst` against IED's actual LDevice instances, adding warnings for non-existent references.
    - New test: `TestValidate_GSE_BadLDInst`.

### Test summary

| Metric | Before | After |
|---|---|---|
| Total tests | 318 | 328 |
| Race detector | Pass | Pass |
| golangci-lint | 0 issues | 0 issues |
| Coverage (total) | 89.4% | 89.4% |

### New tests added this round

- `TestCacheExplicit_DoesNotAutoPopulate`
- `TestCacheExplicit_CachesAfterRefresh`
- `TestGetDataSet_CachedMutationSafe`
- `TestCreateDataSet_RejectsLNOnlyRef`
- `TestCreateDataSet_RejectsMissingFC`
- `TestSubscribeReport_MultipleExactSameID`
- `TestSubscribeReport_ExactAndGlobBothReceive`
- `TestTimestamp_QualityRoundtrip`
- `TestTimestamp_QualityAllBits`
- `TestGenerate_Services_Roundtrip`
- `TestValidate_GSE_BadLDInst`

### Files changed

- `cache.go` — CacheExplicit gating, deep-copy, strategy field, atomic refresh
- `client.go` — `sync.RWMutex`, `newModelCache(strategy)`, multi-subscriber map type, closeAllSubscriptions
- `dataset.go` — CacheLazy-only auto-populate, ReadDataSet cache reuse, stronger CreateDataSet validation
- `report.go` — multi-subscriber dispatch, OverflowBlock lock fix, segment logging
- `timestamp.go` — quality byte encoding, updated docs
- `scl/generate.go` — `servicesToXML()`, wired into `iedToXML()`
- `scl/validate.go` — GSE/SMV LDInst cross-validation
- `cache_test.go` — updated CacheExplicit tests
- `dataset_test.go` — mutation safety test, validation rejection tests
- `report_test.go` — multi-subscriber tests
- `timestamp_test.go` — quality roundtrip tests
- `scl/flatten_test.go` — Services roundtrip test, GSE validation test

### Status

All feedback items implemented. Ready for review.

---

## Post-M8 Feedback Round 2

### Feedback Items Implemented

All 6 feedback items addressed:

#### Must-fix correctness issues (F1–F2)

1. **F1 — Deep-copy Ref.Path per member in copyDataSet**
   - `copyDataSet()` now deep-copies each member's `Ref.Path` slice individually (`append([]string(nil), m.Ref.Path...)`) instead of shallow-copying via `copy(cp.Members, ds.Members)`.
   - Prevents nested slice aliasing where mutating `ds.Members[i].Ref.Path[j]` on a returned `*DataSet` would corrupt the cached copy.
   - Extended existing `TestGetDataSet_CachedMutationSafe` to verify `Ref.Path[0]` and `DomainID` mutation isolation.

2. **F2 — OverflowBlock send-on-closed race**
   - Added `done chan struct{}` and `delivering sync.WaitGroup` to `ReportSubscription`.
   - `deliver()` for `OverflowBlock`: increments WaitGroup under lock, releases lock, then does `select { case ch <- ri: case <-done: }`, decrements WaitGroup on exit.
   - `Close()`: closes `done` (under lock), then calls `delivering.Wait()` before `close(ch)` — guarantees no in-flight send races with channel close.
   - `closeAllSubscriptions()` follows the same pattern.
   - New test `TestOverflowBlock_CloseDoesNotPanic`: fills channel, starts blocking deliver in goroutine, calls Close, verifies deliver unblocks without panic. Passes with `-race`.

#### Robustness improvements (F3–F6)

3. **F3 — SubscribeReport lifecycle rollback**
   - `SubscribeReport()` now tracks `reserved` and `enabled` booleans as lifecycle steps succeed.
   - On any subsequent failure (auto-enable, GI trigger, or missing LD/RCBItemID), a `rollback()` function reverses prior steps: disables RptEna if enabled, releases URCB if reserved.
   - Prevents leaving server-side state dangling when subscription setup partially fails.

4. **F4 — CreateDataSet validates member LD consistency**
   - When deriving from `Ref`: rejects `Ref.LD != "" && Ref.LD != ld`.
   - When using raw `DomainID/ItemID`: rejects `DomainID != ld`.
   - New tests: `TestCreateDataSet_RejectsCrossDomainRef` and `TestCreateDataSet_RejectsCrossDomainRaw`.

5. **F5 — Stronger cache mutation test**
   - Extended `TestGetDataSet_CachedMutationSafe` to also mutate `ds.Members[0].Ref.Path[0]` and `ds.Members[0].DomainID`, then verify the cache returns the original values — catches the real Ref.Path aliasing bug.

6. **F6 — Client concurrency doc rewording**
   - Updated `Client` struct doc comment to explicitly list internal synchronization responsibility: cache, subscriptions, segmented report buffers, connection lifecycle — rather than delegating all concurrency claims to `mms.Client`.

### Test summary

| Metric | Before | After |
|---|---|---|
| Total tests | 328 | 331 |
| Race detector | Pass | Pass |
| golangci-lint | 0 issues | 0 issues |
| Coverage (total) | 89.4% | 89.4% |

### New tests added this round

- `TestOverflowBlock_CloseDoesNotPanic`
- `TestCreateDataSet_RejectsCrossDomainRef`
- `TestCreateDataSet_RejectsCrossDomainRaw`
- Extended `TestGetDataSet_CachedMutationSafe` with Ref.Path/DomainID mutation checks

### Files changed

- `cache.go` — deep-copy Ref.Path per member in `copyDataSet()`
- `client.go` — concurrency doc reword, `closeAllSubscriptions` done/delivering pattern
- `dataset.go` — LD consistency validation for Ref.LD and raw DomainID
- `report.go` — `done` channel, `delivering` WaitGroup, lifecycle rollback, OverflowBlock race fix
- `dataset_test.go` — cross-domain rejection tests, deeper mutation tests
- `report_test.go` — OverflowBlock close-safety test

### Status

All feedback items implemented. Ready for review.

---

## Post-M8 Feedback Round 3

### Feedback Items Implemented

All items from Patch sets A, B, C, and D addressed (12 changes total):

#### Patch set A — Correctness

1. **A1 — CacheExplicit docs/implementation alignment**
   - Updated `options.go` doc comment for `CacheExplicit` to accurately describe the implemented semantics: cache is only populated via explicit `RefreshCache`/`RefreshLDCache`; browse calls consult cache if populated, otherwise fetch live without storing.

2. **A2 — ReadMultiple requires object references**
   - `ReadMultiple` now requires `ref.IsObject()` (LD/LN/FC/path), rejecting LN-level bulk reads.
   - Updated doc comment to explicitly state LN-level bulk reads are not supported.
   - New test: `TestReadMultiple_RejectsLNOnlyRef`.

3. **A3 — Deep-copy exported byte slices from MMS decode**
   - `convertJournalResult`: `EntryID` deep-copied via `append([]byte(nil), ...)`.
   - `decodeRCB`: `EntryID` and `Owner` byte slices deep-copied.
   - `decodeReportIndication`: `EntryID` deep-copied.

4. **A4 — Journal pagination non-progress guard**
   - Both `ReadJournalAll` and `ReadJournalAfterAll` detect stuck pagination: if last entry's `(OccurrenceTime, EntryID)` is unchanged after a page fetch, returns an error instead of looping forever.

5. **A5 — Harden SubscribeReport vs Client.Close race**
   - `registerSubscription` returns `bool` (false when `reportSubs` is nil due to concurrent close).
   - `SubscribeReport` checks registration success; if false, cleans up and returns `ErrClosed`.

#### Patch set B — Semantics

6. **B6 — Remove unused StrictnessOptions.RejectUnknownFC**
   - Removed the `RejectUnknownFC` field since it was never wired into any decode path.
   - `StrictnessOptions` is now an empty struct placeholder.

7. **B7 — decodeRCB fails on required-field type mismatches**
   - Added `mustString`, `mustBool`, `mustUint32` helpers for required RCB fields (RptID, RptEna, DatSet, ConfRev).
   - Returns `ReportError` on type mismatch instead of silent zero values.
   - New test: `TestDecodeRCB_RequiredFieldTypeMismatch`.

8. **B8 — Dedup FindPaths results**
   - Added `seen` map keyed by `ref.String()` to eliminate duplicates.

#### Patch set C — Documentation

9. **C9 — Document glob semantics in FindPaths**
   - Expanded doc: `*` does not cross `/`, `[` brackets match character classes, recommends regex for cross-component matches, results are deduplicated.

10. **C10 — Document RefreshLDCache scope**
    - Clarified it only refreshes per-LD variable data, not the global LD list.

#### Patch set D — SCL improvements

11. **D11 — SCL cycle detection in recursive flattening**
    - `flattenDOTypeRec`: tracks visited DOType IDs to break SDO cycles.
    - `flattenDATypeRec`: tracks visited DAType IDs to break Struct BDA cycles.
    - Uses "mark on enter, unmark on exit" pattern for diamond-shaped type trees.
    - New test: `TestFlatten_CyclicDOType`.

12. **D12 — SCL duplicate ID validation**
    - `Validate` checks for duplicate IDs across LNodeType, DOType, DAType, EnumType, and duplicate IED names.
    - Reports as `SeverityWarning` findings.
    - New test: `TestValidate_DuplicateTypeIDs`.

### Test summary

| Metric | Before | After |
|---|---|---|
| Total tests | 331 | 335 |
| Race detector | Pass | Pass |
| golangci-lint | 0 issues | 0 issues |
| Coverage (total) | 89.4% | 89.0% |

### New tests added this round

- `TestReadMultiple_RejectsLNOnlyRef`
- `TestDecodeRCB_RequiredFieldTypeMismatch`
- `TestValidate_DuplicateTypeIDs`
- `TestFlatten_CyclicDOType`

### Files changed

- `options.go` — CacheExplicit doc fix, StrictnessOptions cleanup
- `bulk.go` — ReadMultiple requires IsObject()
- `journal.go` — deep-copy EntryID, pagination non-progress guard
- `report.go` — deep-copy EntryID/Owner, decodeRCB strictness, subscribe-vs-close hardening
- `browse.go` — FindPaths dedup, glob docs
- `cache.go` — RefreshLDCache doc
- `scl/flatten.go` — cycle detection in DOType/DAType recursion
- `scl/validate.go` — duplicate ID/name detection
- `bulk_test.go`, `report_test.go`, `scl/flatten_test.go` — new tests

### Status

All feedback items implemented. Ready for review.

---

## Feedback Round 4 — API naming, doc precision, report strictness

### Summary

Addressed 17 items covering naming precision, IEC-vs-MMS boundary clarity, report decode strictness, doc mismatches, and code hardening.

### Items Implemented

| # | Item | Action |
|---|------|--------|
| 1 | Fix `FindMatchRegex` doc typo | Changed `[FindMatchRegex]` → `[MatchRegex]` in `FindPaths` doc comment (`browse.go`) |
| 2 | Fix `DataSet.Reference` doc mismatch | Changed from `"LD/LN$dsName"` to `"LD/LLN0.dsName"` with note about separator convention (`dataset.go`) |
| 3 | Clarify `FindPaths` FC behavior | Added doc paragraph explaining pattern matching ignores FC suffix and duplicates may appear without `FindQuery.FC` (`browse.go`) |
| 4 | Clarify `Value` slice aliasing | Added aliasing warning to `OctetString()` and `BitString()` docs (`values.go`) |
| 5 | Clarify `SubscribeReport` rollback semantics | Added doc paragraph about non-atomic lifecycle and best-effort rollback (`report.go`) |
| 6 | Rename `ListDataAttributes` → `ListChildren` | Renamed function, updated doc to say "direct children", updated `DataObject` doc to say "browsed model component, which may be a data object, sub-data object, or data attribute", updated error messages and log lines, updated all tests (`browse.go`, `browse_test.go`) |
| 7 | `decodeRCB` strict for OptFlds/TrgOps | Added `decodeOptFldsStrict` and `decodeTrgOpsStrict` returning errors; OptFlds and TrgOps are now required fields in `decodeRCB` with type/length validation; added `TestDecodeRCB_OptFldsTrgOpsStrict` test (`report.go`, `report_test.go`) |
| 8 | Bit-shift comments in report decoders | Added explanatory comments for `>> 1` in `decodeTrgOpsStrict` and `decodeReasonCode` explaining IEC 61850 reserved bit 0 and MMS bit ordering (`report.go`) |
| 9 | Defensive copy in segmented report reassembly | Changed `EntryID: first.EntryID` to `EntryID: append([]byte(nil), first.EntryID...)` in `assembleSegments` (`report.go`) |
| 10 | Document `CreateDataSet` member precedence | Added doc explaining DomainID/ItemID take priority, Ref used as fallback, all members must share same LD (`dataset.go`) |
| 11 | Document ref length limits in `ParseRef` | Added doc explaining 129-char limit derived from IEC 61850-7-2 ObjectReference spec (`ref.go`) |
| 12 | Add `Ref.ToMMS()` examples | Added two examples in doc: LN-level and object-level with FC (`ref.go`) |
| 13 | `TreeOptions.MaxDepth` + `ModelNode.Reference` doc | MaxDepth doc now references `Ref.Depth()` semantics; `ModelNode.Reference` doc clarifies FC is typically empty during browse and points to `WithFC`/`FC`/`FCs` (`browse.go`, `model.go`) |
| 14 | Nil elements in `StructureValue`/`ArrayValue` | Added doc warning that nil elements are invalid; added `TestStructureValue_NilElement` and `TestArrayValue_NilElement` tests (`values.go`, `values_test.go`) |
| 15 | Duplicate type IDs → errors | Changed `addWarning` → `addError` for duplicate LNodeType, DOType, DAType, EnumType, IED entries; updated test to expect `SeverityError` (`scl/validate.go`, `scl/flatten_test.go`) |
| 16 | Expand `doc.go` with API group overview | Added "API groups" section listing all functional groups (Browse, Read/Write, Datasets, Reports, Journals, Files, SCL); added "IEC references vs MMS names" section explaining the naming boundary (`doc.go`) |
| 17 | Dataset boundary: document MMS vs IEC naming | Updated `GetDataSet` doc to clarify MMS NVL item ID input and IEC-format Reference output; updated `CreateDataSet` doc similarly (`dataset.go`) |

### Files Changed

- `browse.go` — `FindMatchRegex` typo fix, `ListChildren` rename, `FindPaths` FC doc, `TreeOptions.MaxDepth` doc
- `browse_test.go` — test renames for `ListChildren`
- `dataset.go` — `DataSet.Reference` doc fix, `GetDataSet` doc, `CreateDataSet` doc
- `doc.go` — API groups overview, IEC-vs-MMS naming section
- `model.go` — `DataObject` doc, `ModelNode.Reference` doc
- `ref.go` — `ParseRef` length limit doc, `Ref.ToMMS()` examples
- `report.go` — `SubscribeReport` rollback doc, strict OptFlds/TrgOps, bit-shift comments, defensive copy in `assembleSegments`
- `report_test.go` — `TestDecodeRCB_OptFldsTrgOpsStrict`
- `values.go` — `OctetString`/`BitString` aliasing doc, `StructureValue`/`ArrayValue` nil doc
- `values_test.go` — nil element tests
- `scl/validate.go` — duplicate IDs → errors
- `scl/flatten_test.go` — updated test severity expectations

### Metrics

- **Tests**: 340 (all passing)
- **Coverage**: 89.0%
- **Lint issues**: 0
- **Race conditions**: 0

### Status

All feedback items implemented. Ready for review.

---

## Phase M9 — Server groundwork and config generation path

### Summary

Created the server-side model, constructor scaffolding, and config generation adapter to support future `iec61850ctl server generate-config` workflows.

### M9.1 — Server-side model types

Created `internal/servermodel/` package with Go-native IEC 61850 server model types:

| Type | Description |
|------|-------------|
| `Model` | Top-level container for a server's data model |
| `LogicalDevice` | LD with name and logical nodes |
| `LogicalNode` | LN with name, class, data objects, datasets, reports |
| `DataObject` | DO/SDO with CDC, nested children, and leaf attributes |
| `DataAttribute` | Leaf DA with name, FC, BType, initial value, and structured children |
| `DataSetDef` | Dataset definition with FCDA members |
| `DataSetMemberDef` | Single FCDA member (LDInst, LNName, DOPath, FC) |
| `ReportDef` | RCB definition (name, rptID, datSet, confRev, buffered, trgOps, optFlds) |
| `TrgOpsDef` / `OptFieldsDef` | Trigger options and optional field flags |
| `Model.Validate()` | Validates consistency: required LLN0, no duplicate LD/LN/DS/RCB names, non-empty datasets, non-empty report datSet |

### M9.2 — Server constructor scaffolding

Created `server.go` with:

| API | Description |
|-----|-------------|
| `Server` struct | Wraps `mms.Server` with IEC 61850 model + value store |
| `ServerOptions` | Logger + MMS server options |
| `NewServer(model, opts)` | Creates server from model; validates model, registers with MMS |
| `Server.Model()` | Returns the data model |
| `Server.ValueStore()` | Returns the backing value store for direct value manipulation |
| `Server.MMS()` | Returns the underlying MMS server for advanced configuration |
| `Server.Serve(ctx, conn)` | Handles a single MMS connection |
| `Server.ListenAndServe(ctx, ln)` | Accept loop |
| `NewServerModelFromSCL(scl, ied, ap)` | Convenience wrapper for SCL → model |

Stability: documented as **experimental** in the doc comment.

### M9.3 — Config generation adapter

Created three conversion layers:

| Component | File | Description |
|-----------|------|-------------|
| SCL → Model | `internal/servermodel/fromscl.go` | `FromSCL()` converts SCL IED/AP/Server to server model, expanding DOTypes/DATypes, applying DAI overrides |
| Model → MMS | `internal/servermodel/register.go` | `RegisterModel()` registers all domains, variables, NVLs, and RCBs with MMS server; `ValueStore` provides thread-safe read/write backing |
| Model → Config | `internal/servermodel/config.go` | `GenerateConfig()` produces JSON config output; `GenerateMMS()` produces text summary of MMS variable registry |

Key features:
- Full type expansion from SCL DataTypeTemplates (LNodeType → DOType → DAType → BDA)
- Cycle detection in DOType/DAType expansion (reuses visited-map pattern)
- DAI value overrides applied during model building
- IEC 61850 basic type → MMS TypeSpec mapping (30+ BTypes)
- Thread-safe `ValueStore` for concurrent server value access
- RCB field pre-population in the value store (RptID, RptEna, DatSet, ConfRev, etc.)

### Files Created

| File | Description |
|------|-------------|
| `server.go` | IEC 61850 Server type, constructor, ListenAndServe |
| `server_test.go` | Server construction, nil model, invalid model, SCL integration |
| `internal/servermodel/model.go` | Server model types + validation |
| `internal/servermodel/model_test.go` | 8 validation tests (empty, duplicate LD/LN/DS, missing LLN0, empty dataset, report without datSet) |
| `internal/servermodel/fromscl.go` | SCL → server model conversion |
| `internal/servermodel/fromscl_test.go` | 10 SCL conversion tests (basic, data objects, DAI overrides, datasets, reports, error paths, validation roundtrip) |
| `internal/servermodel/register.go` | Model → MMS registration + ValueStore |
| `internal/servermodel/register_test.go` | 4 registration tests (basic, RCB defaults, pre-populated store, concurrent access) |
| `internal/servermodel/config.go` | Config generation (JSON + MMS text) |
| `internal/servermodel/config_test.go` | 4 config generation tests (basic, roundtrip, MMS output, empty model) |

### Metrics

- **Tests**: 368 (all passing, +28 new)
- **Coverage**: 87.4% overall, 74.3% servermodel
- **Lint issues**: 0
- **Race conditions**: 0

### Phase exit criteria

- [x] Enough groundwork for `iec61850ctl server generate-config`
- [x] No overdesigned server API (minimal surface, experimental flag)
- [x] Tests green

---

## Feedback Round 5 — M9 Review Follow-up

_Completed: 2026-03-19_

### Summary

Addressed all 10 items from the M9 review feedback covering critical fixes, strongly recommended improvements, and documentation clarifications.

### Critical fixes (F1–F5)

| # | Item | Status |
|---|------|--------|
| F1 | **Domain-qualified ValueStore keys** — changed store keys from bare `itemID` to `ldName/itemID` via new `StoreKey()` helper. Applied everywhere: DA registration, RCB registration, all Get/Set call sites, tests. Prevents key collisions across multiple LDs. | Done |
| F2 | **Real RCB registration** — register each RCB subfield (`RptID`, `RptEna`, `DatSet`, `ConfRev`, `OptFlds`, `BufTm`, `SqNum`, `TrgOps`, `IntgPd`, `GI`) plus BRCB-specific (`PurgeBuf`, `EntryID`) and URCB-specific (`Resv`) as individual MMS variables with Read/Write callbacks. Parent RCB variable assembles/disassembles the structure from store on read/write. | Done |
| F3 | **Fail hard on unresolved type refs** — `FromSCL` now returns errors for missing LNodeType, DOType, and DAType (for Struct DAs). No more silently producing empty/incomplete models. | Done |
| F4 | **Proper nested DAI override support** — dotted DAI names (e.g. `mag.f`) now recursively descend into DA children. SDI overrides also descend into structured DA attributes via new `applySDIOnAttr` / `setDAIValueOnAttr` helpers. | Done |
| F5 | **Error on unsupported BType** — removed silent `VisString(64)` fallback. Unknown BType now returns an error during registration. | Done |

### Strongly recommended fixes (F6–F10)

| # | Item | Status |
|---|------|--------|
| F6 | **Nil guards in GenerateConfig/GenerateMMS** — both functions now return `"servermodel: nil model"` error instead of panicking. | Done |
| F7 | **Strengthen Model.Validate()** — added checks for: report DatSet exists in same LN, dataset member non-empty LNName/DOPath/FC, empty LNClass, duplicate DO names, duplicate DA names, leaf DA empty FC/BType. | Done |
| F8 | **Document ValueStore aliasing policy** — documented that values are stored/returned as raw `*mms.Value` pointers without copying (alias-by-design), callers must copy if mutation is needed. | Done |
| F9 | **Aggregate validation errors in NewServer()** — `NewServer` now reports all validation errors via `errors.Join` instead of only the first. Error message includes count. | Done |
| F10 | **Document Server.Model() mutability** — doc comment now explicitly states the pointer is shared and must not be mutated after server creation. | Done |

### Files changed

| File | Change |
|------|--------|
| `internal/servermodel/register.go` | Domain-qualified keys, `StoreKey()`, full RCB subfield registration with Read/Write, BType error |
| `internal/servermodel/fromscl.go` | Strict type resolution errors, nested DAI path support, SDI→DA attribute descent |
| `internal/servermodel/config.go` | Nil guards |
| `internal/servermodel/model.go` | Strengthened validation (LNClass, DO/DA duplicates, member fields, DatSet cross-ref) |
| `server.go` | Aggregated validation errors, Model() mutability doc |
| `internal/servermodel/register_test.go` | Domain-qualified keys, RCB subfield tests |
| `internal/servermodel/fromscl_test.go` | Strict type ref errors, nested DAI/SDI override tests |
| `internal/servermodel/model_test.go` | New validation tests (LNClass, DO/DA duplicates, member fields, DatSet cross-ref) |
| `internal/servermodel/config_test.go` | Nil model tests |

### Metrics

- **Tests**: 384 (all passing, +16 new)
- **Coverage**: maintained
- **Lint issues**: 0
- **Race conditions**: 0

---

## Phase M10 — Hardening, docs, examples, interop, and release prep

_Completed: 2026-03-19_

### Summary

Turned the implementation into a release-ready repo with comprehensive documentation, runnable examples, fuzz testing, interop scaffolding, and API review.

### M10.1 Documentation pass

| File | Description |
|------|-------------|
| `README.md` | Comprehensive README with features, installation, quick start (client + SCL), full API overview tables, architecture diagram, object reference and FC reference |
| `KNOWN_LIMITATIONS.md` | Documented all known limitations: protocol scope (MMS only), client gaps (no control models, no GOOSE/SV), server gaps (experimental, no runtime reports), SCL subset, value handling, concurrency model, interop status |
| `ERRORS.md` | Complete error handling guide: sentinel errors table, typed error structs with usage examples, error wrapping chain documentation |
| `OBSERVABILITY.md` | Logging guide: slog integration, enabling/disabling, log levels table, structured attributes, logger propagation, metrics/tracing status |
| `doc.go` | Fixed stale `[Client.WriteFile]` reference (method doesn't exist), added all missing method references to API groups section (TreeWithOptions, ReadRaw, ReadComponent, ListDataSets, ListReports, ListJournals, ReserveURCB, ReleaseURCB, DownloadFile, RenameFile, ObtainFile, GetFileAttributes) |

### M10.2 Examples

| Example | Description |
|---------|-------------|
| `_examples/basic-client/` | Connect, list LDs/LNs, read a value |
| `_examples/browse-tree/` | Build and print model tree with FC annotations and lazy caching |
| `_examples/reports/` | Subscribe to buffered reports with auto-enable, GI trigger, Ctrl-C handling |
| `_examples/files/` | List files and download the first one |
| `_examples/scl-parse/` | Parse an SCD file, print IED/LD summary, run validation, show flat rows/datasets/reports counts, print model tree |

All examples compile successfully.

### M10.3 Fuzzing

| File | Fuzz targets |
|------|-------------|
| `fuzz_test.go` | `FuzzParseRef` — fuzz IEC 61850 reference parser with 11 seed inputs |
| `fuzz_test.go` | `FuzzParseFC` — fuzz FC parser with all valid FCs + invalid inputs |
| `fuzz_test.go` | `FuzzDecodeQuality` — fuzz quality decoding from arbitrary bit strings |
| `fuzz_test.go` | `FuzzNewValue` — fuzz value construction and accessor round-trips |
| `scl/fuzz_test.go` | `FuzzParse` — fuzz SCL XML parser with valid/invalid/malformed inputs, then validate and flatten results |

All fuzz targets verified clean over multi-second runs (500K+ executions for ParseRef, 300K+ for others).

### M10.4 Interop scaffolding

| File | Description |
|------|-------------|
| `interop/README.md` | Complete interop testing guide: test strategy, prerequisites, test matrix (8 planned scenarios), Docker and local running instructions, environment variables, build tag documentation |

Interop tests are behind the `interop` build tag and require a running counterpart.

### M10.5 API review

Systematic audit of the exported API surface:

| Finding | Action |
|---------|--------|
| `doc.go` references `Client.WriteFile` which doesn't exist | Fixed — replaced with actual file methods |
| `doc.go` missing 12 methods from API groups | Fixed — added all missing methods |
| `OptFlds` constants (9) missing doc comments | Fixed — added doc comment block + per-constant descriptions |
| `TrgOps` constants (5) missing doc comments | Fixed — added doc comment block + per-constant descriptions |
| `ReasonCode` constants (5) missing doc comments | Fixed — added doc comment block + per-constant descriptions |
| `RCBFieldMask` constants (12) missing doc comments | Fixed — added doc comment block + per-constant descriptions |
| `servermodel` types in `Server` public API | Accepted — experimental API, documented in KNOWN_LIMITATIONS.md |
| `go-mms` types in escape-hatch APIs | Accepted — intentional design for MMS-level access |
| `AllFunctionalConstraints` / `RefFromMMS` exports | Kept — useful for external tooling and validation |

### Files created/changed

| File | Status |
|------|--------|
| `README.md` | Created |
| `KNOWN_LIMITATIONS.md` | Created |
| `ERRORS.md` | Created |
| `OBSERVABILITY.md` | Created |
| `doc.go` | Updated (fixed stale ref, completed API groups) |
| `report.go` | Updated (added doc comments for 31 constants) |
| `_examples/basic-client/main.go` | Created |
| `_examples/browse-tree/main.go` | Created |
| `_examples/reports/main.go` | Created |
| `_examples/files/main.go` | Created |
| `_examples/scl-parse/main.go` | Created |
| `fuzz_test.go` | Created (4 fuzz targets) |
| `scl/fuzz_test.go` | Created (1 fuzz target) |
| `interop/README.md` | Created |

### Metrics

- **Tests**: 389 (all passing, +5 fuzz seed tests)
- **Coverage**: 87.1% root, 95.1% mapping, 76.7% servermodel, 92.7% scl
- **Lint issues**: 0
- **Race conditions**: 0
- **Examples**: 5 (all compile)
- **Fuzz targets**: 5

### Phase exit criteria

- [x] Polished README with API overview and quick start
- [x] Known limitations documented
- [x] Error handling guide
- [x] Observability guide
- [x] Runnable examples for core workflows
- [x] Fuzz targets for parsers and decoders
- [x] Interop scaffolding with test matrix
- [x] API review — no stale doc refs, all constants documented
- [x] Tests green, lint clean

---

## Feedback Round 6 — Post-M10 Review

**Date**: 2026-03-19

### F1: ReportSubscription.Close() cleanup

**ReportSubscription** now tracks lifecycle state (`didReserve`, `didEnable`, `lifecycleLD`, `lifecycleID`) set during `SubscribeReport`. `Close()` performs best-effort remote cleanup in the correct order:
1. Write `RptEna=false` (if AutoEnable was used)
2. Release URCB reservation (if ReserveURCB was used)

Cleanup errors are logged but never prevent local shutdown. Close remains idempotent.

**Files**: `report.go`
**Tests**: `TestSubscribeReport_CloseDisablesAndReleases` in `report_test.go`

### F2: BRCB client/server structural mismatch

Added `TimeOfEntry` as a registered subfield for server-side BRCBs (`BinaryTime` type). This aligns the server's BRCB structure with what the client-side decoder expects (the decoder already consumed `TimeOfEntry` at index 12).

Added `TestBRCB_ClientServerRoundTrip` — a full round-trip test where a Go server exposes a BRCB from SCL and the Go client reads it via `GetReportControlBlock`, verifying all fields decode correctly (RptID, DatSet, ConfRev, BufTm, IntgPd, TrgOps, OptFlds).

**Files**: `internal/servermodel/register.go`, `server_test.go`, `internal/servermodel/register_test.go`

### F3: Seed server values from SCL InitialValue

During DA registration (`registerDA`), if `attr.InitialValue` is non-empty, it is converted to an `mms.Value` via the new `parseInitialValue` function and stored in the `ValueStore` before the MMS variable is registered. Supported types: BOOLEAN, INT8–INT128, INT8U–INT32U, FLOAT32/64, VisString*, Unicode255, Enum, Tcmd. Invalid or unsupported initial values cause a clear error rather than being silently discarded.

**Files**: `internal/servermodel/register.go`
**Tests**: `TestRegisterModel_InitialValueSeeded` in `internal/servermodel/register_test.go`

### F4: Seed server RCB fields from ReportDef defaults

RCB registration now initializes `OptFlds` and `TrgOps` from the `ReportDef` configuration rather than using zero/empty bit strings. New helpers `encodeOptFieldsDef` and `encodeTrgOpsDef` convert the Go struct flags to the correct MMS BitString encoding (matching the IEC 61850 bit layout).

**Files**: `internal/servermodel/register.go`
**Tests**: `TestRegisterModel_RCBOptFldsFromReportDef` in `internal/servermodel/register_test.go`

### F5: Relax/fix cross-LD dataset member handling

`Client.CreateDataSet` no longer rejects members with a different LD than the dataset owner. Members may now carry their own explicit domain identity via `Ref.LD` or `DomainID`. When `Ref.LD` is empty, the dataset owner LD is used as default. Validation is applied after the LD default is filled in.

**Files**: `dataset.go`
**Tests**: `TestCreateDataSet_CrossLDMember`, `TestCreateDataSet_CrossLDRawDomainID`, `TestCreateDataSet_EmptyLDDefaultsToOwner` in `dataset_test.go`

### F6: Interop tests against libiec61850

Created black-box integration tests in `interop/interop_test.go` behind the `interop` build tag. Tests cover:
- Browse model (logical devices, logical nodes, tree)
- Read single value
- Write single value
- List/read datasets
- List/read report control blocks
- File listing

Tests use `IEC61850_INTEROP_ADDR` for server address and `IEC61850_INTEROP_SKIP` to disable. All tests compile clean with `go vet -tags interop`.

**Files**: `interop/interop_test.go`

### Files changed

| File | Change |
|------|--------|
| `report.go` | Added lifecycle tracking fields, `remoteCleanup()` method, updated `Close()` |
| `internal/servermodel/register.go` | Added `TimeOfEntry` for BRCB, `parseInitialValue()`, `encodeOptFieldsDef()`, `encodeTrgOpsDef()`, seed InitialValue + ReportDef |
| `dataset.go` | Relaxed cross-LD member validation, default empty LD to owner |
| `server_test.go` | Added `TestBRCB_ClientServerRoundTrip` |
| `report_test.go` | Added `TestSubscribeReport_CloseDisablesAndReleases` |
| `dataset_test.go` | Replaced cross-LD rejection tests with cross-LD allowance tests |
| `internal/servermodel/register_test.go` | Added InitialValue and RCB OptFlds/TrgOps seeding tests, updated TimeOfEntry subfield |
| `interop/interop_test.go` | Created (11 interop tests) |

### Metrics

- **Tests**: 394 (all passing, +5 from previous round)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Feedback Round 7 — Post-M10 Review (continued)

**Date**: 2026-03-19

### F1: Client.Close()/Abort() bypass subscription remote cleanup

`closeAllSubscriptions()` now delegates to `sub.Close()` for each subscription instead of manually closing channels. This ensures the remote cleanup path (disable RptEna, release URCB reservation) runs for subscriptions that used lifecycle options — especially important for `NewClient` users where the underlying MMS connection stays alive.

**Files**: `client.go`

### F2: BRCB TimeOfEntry initial value type mismatch

Changed the BRCB `TimeOfEntry` initial value from `mms.NewUnsigned(0)` to `mms.NewBinaryTime(0)`, matching the declared `ValueTypeBinaryTime` type spec.

**Files**: `internal/servermodel/register.go`

### F3: GIOnSubscribe failure path duplicate cleanup

Removed the redundant `rollback()` call from the GIOnSubscribe failure branches. After the subscription is registered, `sub.Close()` already handles remote cleanup (disable + release), so calling both `sub.Close()` and `rollback()` produced duplicate writes.

**Files**: `report.go`

### F4: parseInitialValue broadened for realistic SCL

Extended `parseInitialValue` to support:
- **Quality** (13-bit BitString)
- **Dbpos**, **Check** (2-bit BitString)
- **OptFlds** (10-bit BitString)
- **TrgOps** (6-bit BitString)
- **Timestamp** (UTCTime, zero default)
- **EntryTime** (BinaryTime, zero default)
- **Octet6**, **Octet16**, **Octet64** (OctetString, hex or decimal)

Added helper functions `parseInitialBitString`, `parseInitialOctetString`, `hexDecode`, `hexNibble` for the new conversions. Invalid values still produce clear errors.

**Files**: `internal/servermodel/register.go`

### F5: RCB Reference documented as display-only pseudo-reference

Updated the `ReportControlBlock.Reference` field doc to clearly state it is a display-only pseudo-reference derived from MMS item ID replacement, not a proper IEC 61850 object reference (BR/RP is an FC, not a path component). Users are directed to use the LD and MMS item ID for programmatic access.

**Files**: `report.go`

### F6: ListReports semantic validation path

Added `ListReportsVerified()` as an explicit method that reads and decodes each heuristic RCB candidate from the server. Also wired this into `ListReports()` via the new `StrictnessOptions.VerifyReportCandidates` knob: when true, `ListReports` automatically verifies candidates, excluding items that fail to decode.

**Files**: `report.go`

### F7: ReadDataSet populates cache on miss

`ReadDataSet()` now stores the dataset definition in the cache (under `CacheLazy` strategy) when it fetches attributes on a cache miss, making cache behavior symmetric with `GetDataSet()`.

**Files**: `dataset.go`

### F8: StrictnessOptions — real knobs added

Replaced the empty `StrictnessOptions` struct with two concrete knobs:
- `RejectUnknownFC` — when true, browse/tree operations reject non-standard FCs
- `VerifyReportCandidates` — when true, `ListReports` reads and verifies each candidate

**Files**: `options.go`, `report.go`

### F9: basic-client example made dynamic

Replaced the hardcoded `LLN0.Mod.stVal[ST]` read with dynamic tree browsing: the example now calls `TreeWithOptions` with `IncludeFCs: true`, walks to the first leaf node, and reads that attribute. Works on any server model.

**Files**: `_examples/basic-client/main.go`

### Files changed

| File | Change |
|------|--------|
| `client.go` | `closeAllSubscriptions` delegates to `sub.Close()` |
| `internal/servermodel/register.go` | Fixed TimeOfEntry initial value type; broadened `parseInitialValue` |
| `report.go` | Removed duplicate GI rollback; documented Reference; added `ListReportsVerified`; wired `VerifyReportCandidates` |
| `dataset.go` | `ReadDataSet` populates cache on miss |
| `options.go` | Added `RejectUnknownFC` and `VerifyReportCandidates` to `StrictnessOptions` |
| `_examples/basic-client/main.go` | Dynamic leaf browsing instead of hardcoded ref |

### Metrics

- **Tests**: 394 (all passing)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Feedback Round 8 — Comprehensive Review

**Date**: 2026-03-19

### Highest-value fixes

#### F1: Clarify FC-merged browse semantics
Updated doc comments on `ListChildren`, `Tree`, and `TreeWithOptions` to explicitly state that returned nodes are merged views across functional constraints. Callers must choose an FC before reading/writing.

**Files**: `browse.go`

#### F2: RejectUnknownFC wired into discovery paths
`RejectUnknownFC` is now enforced in:
- `FindPaths`: returns error on unknown FC instead of silently skipping (when enabled)
- Tree building: FC annotation uses `convertFCs()` which filters out non-standard FCs when `RejectUnknownFC` is true

**Files**: `browse.go`

#### F3: ListReportsVerified no longer re-verifies
Extracted `listReportCandidates()` and `verifyReportCandidates()` as shared helpers. Both `ListReports` (with `VerifyReportCandidates` enabled) and `ListReportsVerified` now use the same candidate discovery, so verification only runs once.

**Files**: `report.go`

#### F4: Segmented report reassembly consistency checks
`assembleSegments()` now validates that RptID, DatSet, ConfRev, and OptFlds are consistent across all buffered segments. On metadata mismatch, the buffer is dropped and only the first segment is returned (with a warning log).

**Files**: `report.go`

#### F5: Subscription cleanup suppresses warnings during shutdown
`remoteCleanup()` now checks `client.closed` before logging. When the parent client is already closed (normal shutdown path), cleanup errors are logged at Debug level instead of Warn, eliminating noisy failure logs during expected teardown.

**Files**: `report.go`

### Medium-value fixes

#### F6: Mixed-domain bulk read/write integration tests
Added three integration tests:
- `TestReadMultiple_MixedDomains`: reads from two different LDs in one call
- `TestReadMultiple_MixedDomainPartialError`: one LD succeeds, one has non-existent variable
- `TestWriteMultiple_MixedDomains`: writes to two different LDs in one call

**Files**: `bulk_test.go`

#### F7: ValueStore aliasing documented
Added explicit immutability requirements to `Get()` and `Set()` doc comments: callers must treat returned pointers as read-only, and values passed to `Set()` must not be mutated afterwards.

**Files**: `internal/servermodel/register.go`

#### F8: Arrays/Count and Enum limitations documented
Added "Known limitations" section to `Server` doc comment documenting:
- SCL array/count attributes are parsed but not expanded (registered as scalars)
- Enum BTypes map to generic INT8 without enum-range constraints

**Files**: `server.go`

#### F10: FindPaths LDFilter pruning
Added `LDFilter` field to `FindQuery`. When set, `FindPaths` skips variable fetching for non-matching LDs, avoiding expensive GetNameList calls on servers with many domains.

**Files**: `model.go`, `browse.go`

### Smaller polish / correctness fixes

#### F11: Ref length-limit rephrased
Rephrased the 129-character limit comment from normative spec language to "conservative library guardrail inspired by IEC 61850-7-2 VisibleString129".

**Files**: `ref.go`

#### F12: DataSet.Reference documented as display-only
Updated `DataSet.Reference` doc to explicitly state it is not a `Ref` and should not be used programmatically — callers must use raw `(ld, dsName)` MMS identifiers.

**Files**: `dataset.go`

#### F13: StructureValue/ArrayValue strict variants
Added `StrictStructureValue()` and `StrictArrayValue()` that return `(*Value, error)` and reject nil elements. The original convenience constructors remain but now reference the strict variants in their docs.

**Files**: `values.go`

#### F14: Files example writes to local file
Changed the files example from streaming to stdout (which could dump binary) to writing to a local file with a sanitized filename.

**Files**: `_examples/files/main.go`

#### F15: ListChildren docs updated
ListChildren doc now says "browse children" and explains the FC-merged view semantics (handled as part of F1).

**Files**: `browse.go`

### Tests

#### F16: FC-collision browse tests
Added two tests:
- `TestTree_FCCollision`: verifies that a DO appearing under MX, ST, and CF is merged into one tree node with multiple FCs annotated
- `TestListChildren_FCCollision_MergedView`: verifies ListChildren returns merged children when the same name appears under different FCs

**Files**: `browse_test.go`

#### F17: Segmented-report adversarial tests
Added three adversarial segmented report tests:
- `TestSegmentedReport_ResetOnNewSequence`: verifies buffer reset when a new sequence starts mid-reassembly
- `TestSegmentedReport_NonContiguousSubSeqNum`: verifies handling of gaps in sub-sequence numbers
- `TestSegmentedReport_InconsistentMetadata`: verifies DatSet mismatch across segments triggers fallback to first segment only

**Files**: `report_test.go`

#### F18: Server docs aligned with reality
Added server positioning to `doc.go` and the "Known limitations" section to `Server` type doc (covered in F8).

**Files**: `doc.go`, `server.go`

### Files changed

| File | Change |
|------|--------|
| `browse.go` | FC-merged docs; RejectUnknownFC wiring; LDFilter in FindPaths; `convertFCs` helper |
| `report.go` | Extracted report candidate helpers; segment consistency checks; shutdown log levels; `slog` import |
| `client.go` | (unchanged from Round 7) |
| `model.go` | Added `LDFilter` to `FindQuery` |
| `options.go` | (unchanged from Round 7) |
| `values.go` | Added `StrictStructureValue`, `StrictArrayValue` |
| `dataset.go` | DataSet.Reference doc update |
| `server.go` | Known limitations section |
| `doc.go` | Server positioning section |
| `ref.go` | Length-limit guardrail rephrasing |
| `internal/servermodel/register.go` | ValueStore aliasing docs |
| `_examples/files/main.go` | Write to file, sanitize filename |
| `_examples/basic-client/main.go` | (unchanged from Round 7) |
| `bulk_test.go` | 3 mixed-domain integration tests |
| `browse_test.go` | 2 FC-collision browse tests |
| `report_test.go` | 3 segmented-report adversarial tests |

### Metrics

- **Tests**: 402 (all passing, +8 from previous round)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Feedback Round 8 — Comprehensive Polish (40 items)

**Date**: 2026-03-19

All 40 feedback items implemented.

### Browse & Cache (F1, F2, F10–F15)

- **F1**: Moved `sort.Slice` from `ListLogicalDevices` to `fetchLDs`/`setLDs` so sorting happens once at cache-population time, not on every call.
- **F2**: `TreeOptions.MaxDepth` doc clarified that the synthetic root sits outside the depth model.
- **F10**: Added doc comment to `FindPaths` noting the O(n) reparse cost per call, suggesting cached parsed-item representation.
- **F11**: `convertFCs` now accepts `*slog.Logger` and logs dropped unknown FCs at Debug level when `RejectUnknownFC` is true.
- **F12**: Added doc comment to `buildSubTreeWithOptions` documenting quadratic scan complexity, noting trie-based index as future optimization.
- **F13**: `ListChildren` now clears `ref.FC` before validation, aligning with FC-merged browse semantics.
- **F14**: Introduced `golang.org/x/sync/singleflight` to deduplicate concurrent cache-miss fetches in `cachedLDs`/`cachedItems`.
- **F15**: `RefreshLDCache` doc now explicitly states dataset cache for the LD is also invalidated.

**Files**: `browse.go`, `cache.go`, `go.mod`, `go.sum`

### Ref (F26–F28)

- **F26**: `ParseRef` no longer enforces the 129-character limit. New `ParseRefStrict` provides the old strict behavior.
- **F27**: `RefFromMMS` now permissively accepts any 2-character FC string without `ParseFC` validation. `Ref.Validate` relaxed to only check FC length, not known values.
- **F28**: `Ref.ToMMS` doc now explicitly describes the LN-only special case (no FC → bare item ID for browse).

**Files**: `ref.go`, `ref_test.go`

### Value API (F29–F31)

- **F29**: `Quality.String()` now prints `"reserved-validity"` instead of ambiguous `"reserved"` for `ValidityReserved`.
- **F30**: Added `IsStructure()` and `IsArray()` helper methods to `Value`.
- **F31**: Inverted `StructureValue`/`ArrayValue` defaults — they now return `(*Value, error)` with nil-element validation by default. Old unchecked behavior available as `UnsafeStructureValue`/`UnsafeArrayValue`.

**Files**: `quality.go`, `values.go`, `values_test.go`

### Bulk APIs (F8–F9)

- **F8**: `ReadMultiple`/`WriteMultiple` docs explicitly state duplicate-ref behavior ("duplicates preserved in order").
- **F9**: New `DataAccessError` type (with `ErrDataAccess` sentinel) replaces formatted strings for per-item failures.

**Files**: `bulk.go`, `errors.go`

### Report & Connection (F21–F25)

- **F21**: Added doc comment to `decodeRCB` explaining fixed positional field ordering assumption.
- **F22**: `decodeReportIndication` now performs end-of-decode consistency checks (values/refs/reasons vs inclusion count); returns `ReportError` on mismatch.
- **F23**: `assembleSegments` now validates per-segment cardinality before concatenation. Drops buffer and returns first segment on mismatch.
- **F24**: `handleInformationReport` doc now notes `*ReportIndication` pointers are shared across subscribers (treat as immutable).
- **F25**: `Client.Close` doc explains marked-closed-before-cleanup ordering and debug-level shutdown error logging.

**Files**: `report.go`, `report_test.go`, `client.go`

### SCL (F32–F34)

- **F32**: `validateIEDs` now detects duplicate `AccessPoint.Name` within an IED and duplicate `LDevice.Inst` across APs (both `SeverityError`).
- **F33**: Report referencing dataset not in same LN changed from `SeverityWarning` to `SeverityError`.
- **F34**: `FlatRow` gains `Status` field (`"unresolved-lntype"` / `"unresolved-dotype"`); `WriteCSV` includes the Status column.

**Files**: `scl/validate.go`, `scl/flatten.go`, `scl/flatten_test.go`

### Server Model (F16–F20, F35–F38)

- **F16**: `ValueStore` now supports `DefensiveCopy` option via `NewValueStoreWithOptions`. `Get`/`Set` perform shallow copies when enabled.
- **F17**: `parseInitialValue` for `Timestamp` now returns an error on non-empty values instead of silently yielding zero.
- **F18**: Added note to `parseInitialValue` for `Enum` — values stored as plain integers without `EnumType` validation.
- **F19**: `registerRCB` write handler validates nil elements and type mismatches.
- **F20**: `registerRCB` doc lists all supported/unsupported RCB subfields.
- **F35**: `findServer` returns error on ambiguous multi-AP when `apName` is empty.
- **F36**: `validateDOs` includes full parent path in error messages (e.g., `LN.DO.SDO: duplicate DA`).
- **F37**: `RegisterModel` doc notes array/count attributes are registered as scalars.
- **F38**: `GenerateMMS` now outputs RCBs with type, item IDs, store keys, and subfield mappings.

**Files**: `internal/servermodel/register.go`, `internal/servermodel/config.go`, `internal/servermodel/model.go`, `internal/servermodel/fromscl.go`

### Docs & Examples (F39–F40)

- **F39**: `doc.go` SCL API group expanded to list `ParseFile`, `WriteCSV`, `PrintTree`, `ExportDataSets`, `ExportReports`.
- **F40**: All network examples (`basic-client`, `browse-tree`, `files`, `reports`) now use `context.WithTimeout`.

**Files**: `doc.go`, `_examples/basic-client/main.go`, `_examples/browse-tree/main.go`, `_examples/files/main.go`, `_examples/reports/main.go`

### Examples (F3–F7)

- **F3–F4**: `basic-client` `findFirstLeaf` returns `Ref` directly, skips `q`/`t` attributes, prefers single-FC leaves.
- **F5–F7**: `files` example uses `O_CREATE|O_EXCL`, cleans up partial downloads, improved `sanitizeFilename`.

**Files**: `_examples/basic-client/main.go`, `_examples/files/main.go`

### Files changed

| File | Change |
|------|--------|
| `browse.go` | F1 F2 F10 F11 F12 F13 |
| `cache.go` | F1 F14 F15 |
| `ref.go` | F26 F27 F28 |
| `ref_test.go` | F26 F27 test updates |
| `quality.go` | F29 |
| `values.go` | F30 F31 |
| `values_test.go` | F31 |
| `bulk.go` | F8 F9 |
| `errors.go` | F9 |
| `report.go` | F21 F22 F23 F24 |
| `report_test.go` | F22 |
| `client.go` | F25 |
| `doc.go` | F39 |
| `scl/validate.go` | F32 F33 |
| `scl/flatten.go` | F34 |
| `scl/flatten_test.go` | F33 F34 |
| `internal/servermodel/register.go` | F16 F17 F18 F19 F20 F37 |
| `internal/servermodel/config.go` | F38 |
| `internal/servermodel/model.go` | F36 |
| `internal/servermodel/fromscl.go` | F35 |
| `_examples/basic-client/main.go` | F3 F4 F40 |
| `_examples/files/main.go` | F5 F6 F7 F40 |
| `_examples/browse-tree/main.go` | F40 |
| `_examples/reports/main.go` | F40 |
| `go.mod` | F14 |

### Metrics

- **Tests**: 403 (all passing, +1 from previous round)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Round 9 — Feedback Round 9 (36 items)

Addressed 36 feedback items covering API consistency, error taxonomy,
server-model validation, caching, report handling, SCL override
warnings, example improvements, and documentation tightening.

### Ref & FC (F1–F2)

- **F1**: `Ref.Validate` doc clarified that FC validation is length-only, not semantic.
- **F2**: `ParseRef` doc updated to state unknown FCs are accepted by default.

**Files**: `ref.go`

### Browse & Model (F5–F7)

- **F5**: `FindPaths` now caches parsed `Ref` objects per LD via `modelCache.getParsedRefs`.
- **F6**: `TreeWithOptions` returns `ErrNotFound` when `LDFilter` matches nothing.
- **F7**: `ListChildren` returns `[]BrowseNode` instead of `[]DataObject`.

**Files**: `browse.go`, `browse_test.go`, `cache.go`, `internal/servermodel/model.go`

### Deterministic Output (F3–F4)

- **F3**: `ListReports` sorts results.
- **F4**: `ListDataSets` sorts results.

**Files**: `report.go`, `dataset.go`

### Bulk & Dataset (F8–F9)

- **F8**: `HasDuplicateRefs` helper for pre-detecting duplicate refs.
- **F9**: `ReadDataSet` returns `*DataAccessError` for per-member errors.

**Files**: `bulk.go`, `dataset.go`

### Cache (F10)

- **F10**: Split model-variable vs dataset cache invalidation.

**Files**: `cache.go`

### Server Model (F11–F15)

- **F11**: `ValueStore` docs use "shallow struct-level copy" consistently.
- **F12**: `registerDA` write callback validates type, size, and bit length.
- **F13**: `registerRCB` applies per-field validation for RCB subfields.
- **F14**: Enum initial values validated against `EnumType` definitions from SCL.
- **F15**: Added `TestParseInitialBitString_RoundTrips` for Quality/OptFlds/TrgOps/Dbpos/Check.

**Files**: `internal/servermodel/register.go`, `internal/servermodel/register_test.go`, `internal/servermodel/fromscl.go`, `internal/servermodel/model.go`

### Timestamp & Files (F16–F18)

- **F16**: `Timestamp` doc clarifies decode quality depends on go-mms `UTCTimeQuality()`.
- **F17**: `GetFileAttributes` doc notes it opens/closes the file for metadata.
- **F18**: `DownloadFile` wraps close error with primary error via `errors.Join`.

**Files**: `timestamp.go`, `file.go`

### Reports (F22–F26)

- **F22**: `decodeReportIndication` accepts logger and emits debug logs for field decode failures.
- **F23**: Segmented report reassembly keyed by `(RptID, SeqNum)` to avoid cross-source collisions.
- **F24**: `SubscribeReportOptions.CloneReports` for defensive per-subscription delivery.
- **F25**: `ReasonCode.String()` handles multi-bit bitmasks like `OptFlds`/`TrgOps`.
- **F26**: `decodeOptFldsStrict`/`decodeTrgOpsStrict` check `BitStringLength()` explicitly.

**Files**: `report.go`, `report_test.go`

### Journal (F27)

- **F27**: `convertJournalResult` returns error on nil input.

**Files**: `journal.go`, `journal_test.go`

### SCL & Server Model (F28–F31)

- **F28**: Created `internal/sclindex` stub package for future centralized SCL index helpers.
- **F29**: `FromSCL` emits warnings for unmatched DOI/DAI/SDI overrides into `Model.Warnings`.
- **F30**: `setDAIValue`/`setDAIValueOnAttr` set `DataAttribute.Overridden = true` for provenance tracking.
- **F31**: `Model.Validate` doc and `Server` doc clarify cross-LN limitation.

**Files**: `internal/sclindex/doc.go`, `internal/servermodel/fromscl.go`, `internal/servermodel/model.go`, `server.go`

### FC Optimization (F32–F33)

- **F32**: `AllFunctionalConstraints` uses package-level slice to avoid per-call allocation.
- **F33**: `validFCs`/`fcDescriptions` maps replaced with `switch` statements.

**Files**: `fc.go`

### Error Taxonomy (F34)

- **F34**: Added `ErrInvalidArgument` and `ErrProtocol` sentinel errors. Wrapped argument validation and protocol mismatch errors across all service modules.

**Files**: `errors.go`, `errors_test.go`, `file.go`, `dataset.go`, `report.go`, `bulk.go`, `journal.go`, `browse.go`

### Package Docs (F35)

- **F35**: Tightened `doc.go` promises: FC validation semantics, error sentinels, server limitations.

**Files**: `doc.go`

### Examples (F19–F21)

- **F19**: Examples distinguish `ErrNotFound`/`ErrDataAccess` (server capability) from fatal connection errors.
- **F20**: All examples use `defer client.Close(context.Background())` consistently.
- **F21**: `findFirstLeaf` expanded skip list (ctlModel, ctlVal, operTm), doc clarified as best-effort heuristic.

**Files**: `_examples/basic-client/main.go`, `_examples/browse-tree/main.go`, `_examples/reports/main.go`

### Interop Testing (F36)

- **F36**: Dedicated interop testing is the next priority. The library now has comprehensive API coverage and should be validated against real vendor IEDs and libiec61850-derived peers. Key areas: read/write encoding, report delivery, file services, journal pagination, and SCL model compatibility.

### Metrics

- **Tests**: 634 (all passing)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Phase M11 — Controls

Implemented a full IEC 61850 control layer with typed client APIs,
server-side control handlers, semantic types, and error taxonomy.

### M11.1 Client-side typed control APIs

- `Client.Operate(ctx, ref, params)` — direct-operate or operate after select.
- `Client.Select(ctx, ref)` — SBO select (normal security), returns selected ref.
- `Client.SelectWithValue(ctx, ref, params)` — SBOw select (enhanced security).
- `Client.Cancel(ctx, ref, params)` — cancel a pending control.
- `Client.ReadCtlModel(ctx, ref)` — reads ctlModel attribute (CF FC).
- `Client.ReadLastApplError(ctx, ref)` — reads and decodes LastApplError.

Control refs must specify LD/LN + data object path but NOT FC — the
library manages CO paths (Oper, SBO, SBOw, Cancel) automatically.

**Files**: `control.go`

### M11.2 Control model types

- `CtlModel` — status-only, direct-normal, SBO-normal, direct-enhanced, SBO-enhanced.
- `Origin` — originator (OrCat + OrIdent), with MMS encoding.
- `OrCat` — originator category enum (bay, station, remote, automatic, etc.).
- `CheckConditions` — synchrocheck/interlockCheck bitstring.
- `OperateParams` — full operate parameters (CtlVal, Origin, CtlNum, OperTm, Test, Check).
- `CancelParams` — cancel parameters matching the original operate.
- `AddCause` — additional cause enum (18 defined values).
- `LastApplError` — decoded server failure reason.
- `BoolCtlVal`, `IntCtlVal`, `FloatCtlVal`, `EnumCtlVal`, `StringCtlVal`, `BspCtlVal`, `DpCtlVal` — typed control value constructors.
- `buildOper`, `buildCancel` — MMS structure builders for Oper/Cancel.

**Files**: `control_types.go`

### M11.3 Server-side control execution hooks

- `ControlHandler` — struct with `OnSelect`, `OnOperate`, `OnCancel` callbacks.
- `ControlRequest` — decoded command parameters passed to handlers.
- `Server.RegisterControl(ldName, doRef, ctlModel, handler)` — registers control handlers.
- `controlRegistration` — internal state for SBO reservation tracking (selectOwner, selectTime, selectCtlNum).
- SBO timeout enforcement (30s default).
- Operate-without-select rejection for SBO models.
- `handleControlWrite` — server-side dispatch from MMS write path.
- `decodeControlRequest` — decodes Oper/SBOw/Cancel MMS structures.

**Files**: `control_server.go`, `server.go`

### M11.4 Error and state semantics

- `ErrControlFailed` — generic control failure sentinel.
- `ErrSelectFailed` — select denied sentinel.
- `ErrOperateFailed` — operate denied sentinel.
- `ErrCancelFailed` — cancel denied sentinel.
- `ErrNotControllable` — status-only model sentinel.
- `ControlError` — typed error with Ref, Operation, AddCause, and Wrapped fields.
- `ControlError.Unwrap()` dispatches to operation-specific sentinels.

**Files**: `errors.go`

### Tests

- `control_types_test.go`: CtlModel/OrCat/AddCause String(), predicates, Origin/Check MMS encoding, buildOper/buildCancel structure, ctlVal constructors, DpCtlVal, LastApplError decoding.
- `control_test.go`: validateControlRef, controlSubRef, Operate/Select/Cancel argument validation, decodeControlRequest (Oper/Cancel/invalid/too-few-members), pathWithoutSuffix, nextCtlNum, RegisterControl (status-only/duplicate/empty-args), server dispatch (direct-operate, SBO lifecycle, operate-without-select, cancel, operate-rejected, unregistered).
- `errors_test.go`: ControlError unwrap-by-operation, AddCause in error string, wrapped error chain.

### Example

- `_examples/control/main.go`: demonstrates browsing for controllable objects, reading ctlModel, and performing direct-operate or SBO workflows.

### Documentation updates

- `KNOWN_LIMITATIONS.md`: updated client and server control limitations.
- `doc.go`: added Controls API group listing.

### Files changed

| File | Change |
|------|--------|
| `control.go` | Client-side Operate/Select/SelectWithValue/Cancel/ReadCtlModel/ReadLastApplError |
| `control_types.go` | CtlModel, Origin, OrCat, CheckConditions, OperateParams, CancelParams, AddCause, LastApplError, ctlVal constructors, buildOper/buildCancel |
| `control_server.go` | ControlHandler, ControlRequest, Server.RegisterControl, handleControlWrite, SBO state tracking |
| `control_types_test.go` | Type/encoding/constructor tests |
| `control_test.go` | API validation, server dispatch lifecycle tests |
| `errors.go` | ErrControlFailed, ErrSelectFailed, ErrOperateFailed, ErrCancelFailed, ErrNotControllable, ControlError |
| `errors_test.go` | ControlError tests |
| `server.go` | controls map + controlMu added to Server |
| `doc.go` | Controls API group |
| `KNOWN_LIMITATIONS.md` | Updated control limitations |
| `_examples/control/main.go` | Control example |

### Metrics

- **Tests**: 685 (all passing, +51 from previous round)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Phase M12 — Runtime reports

**Status**: COMPLETED

### Objective

Turn the existing report groundwork (RCB structures, subfield registration,
dataset definitions) into a real runtime report engine that autonomously
sends InformationReports to connected clients based on trigger conditions.

### Deliverables

1. **Runtime report engine** (`ReportEngine`)
2. **RCB state manager** (`rcbRuntime`)
3. **Buffered report queue** (BRCB entry buffering)
4. **Change-detection path** tied to server value mutation
5. **Documentation and tests**

### M12.1 — RCB runtime state manager

Implemented `rcbRuntime` struct holding live state for each report control
block:

- `enabled` / `reserved` flags with proper enable/disable lifecycle.
- Configuration read from `ValueStore` on enable: RptID, DatSet, ConfRev,
  OptFlds, TrgOps, BufTm, IntgPd.
- URCB reserve/release semantics.
- Sequence number auto-incrementing (`seqNum`).
- Previous value tracking for change detection (`prevValues`).
- Integrity period timer management (goroutine with captured channels
  to avoid data races).

**Files**: `report_engine.go`

### M12.2 — Report generation engine

Implemented trigger detection and report encoding:

- **Data-change trigger**: `NotifyValueChanged` compares current vs
  previous values using `valuesEqual()` per dataset member; fires
  `ReasonDataChanged` when different.
- **Quality-change trigger**: `qualityChanged()` detects bitstring
  changes; fires `ReasonQualityChanged`.
- **Data-update trigger**: fires `ReasonDataUpdate` on any write when
  `TrgOpDataUpdate` is enabled.
- **Integrity trigger**: periodic timer generates full-dataset reports
  with `ReasonIntegrity`.
- **GI trigger**: `HandleRCBWrite("GI", true)` generates full-dataset
  report with `ReasonGI`; resets GI flag in store.
- **Report encoding**: `encodeReportValues()` builds the flat MMS value
  list following the IEC 61850 field order (RptID, OptFlds, optional
  fields, SubSeqNum, MoreSegments, inclusion bitmap, values, reason codes).
- **Delivery**: `sendReport()` sends `InformationReportRequest` to all
  connected clients via `mms.ServerConn.SendInformationReport`.

**Files**: `report_engine.go`

### M12.3 — Buffered report queue

Implemented in-memory buffered report queue for BRCBs:

- `bufQueue []*bufferedEntry` with configurable `bufMax` (default 1000).
- Auto-incrementing 8-byte `entryID` (big-endian uint64).
- FIFO overflow: oldest entry dropped when queue full; logged at debug.
- `PurgeBuf` support: clears the buffer queue on client request.
- Store updates: `EntryID`, `TimeOfEntry`, and `SqNum` in `ValueStore`
  kept in sync with each buffered report.

**Files**: `report_engine.go`

### M12.4 — Dataset binding and runtime resolution

Implemented `resolveDataset()`:

- Resolves `DatSet` string to the matching `DataSetDef` in the model.
- Builds `memberKeys` (ValueStore keys) and `memberVars`
  (MMS ObjectNames) for each dataset member.
- Handles cross-LD members via `LDInst`.
- `readMemberValues()` reads current values for all members.

**Files**: `report_engine.go`

### M12 — ValueStore change notifications

Added `Server.SetValue(ctx, storeKey, val)`:

- Sets the value in the store.
- Calls `ReportEngine.NotifyValueChanged()` if the report engine is
  active.
- Primary entry point for injecting process values that trigger reports.

Added `Server.ReportEngine()` accessor.

**Files**: `server.go`

### M12 — RCB write interception

`ReportEngine.HandleRCBWrite()` intercepts client writes to RCB
subfields:

- `RptEna`: enables/disables the RCB runtime.
- `GI`: triggers General Interrogation.
- `Resv`: reserves/releases URCBs (rejects Resv on BRCBs).
- `PurgeBuf`: clears the BRCB buffer queue.

**Files**: `report_engine.go`

### Helper functions

- `valuesEqual()`: type-aware MMS value comparison for data-change
  detection (boolean, integer, unsigned, float, string, bitstring,
  octet string).
- `qualityChanged()`: bitstring-specific change detection.
- `encodeInclusion()`: inclusion bitmap → MMS bitstring.
- `encodeReasonCode()`: reason code → 7-bit MMS bitstring.
- `encodeEntryID()`: uint64 → 8-byte big-endian.

### Tests

`report_engine_test.go` — 23 tests:

- `TestReportEngine_EnableDisable`: enable/disable lifecycle, config
  loading, dataset resolution.
- `TestReportEngine_URCB_Reserve`: URCB reserve/release.
- `TestReportEngine_ResvNotApplicableToBRCB`: Resv rejected for BRCBs.
- `TestReportEngine_DataChange`: data-change trigger increments seqNum.
- `TestReportEngine_GI`: GI trigger increments seqNum, resets flag.
- `TestReportEngine_GI_Disabled`: GI no-op when disabled.
- `TestReportEngine_IntegrityPeriod`: integrity timer fires reports.
- `TestReportEngine_BufferedQueue`: BRCB queue accumulates entries.
- `TestReportEngine_BufferOverflow`: oldest entry dropped at capacity.
- `TestReportEngine_PurgeBuf`: buffer purged on PurgeBuf write.
- `TestReportEngine_NoChangeNoReport`: same value → no report.
- `TestReportEngine_UnknownRCB`: unknown RCB → no-op, no error.
- `TestReportEngine_DoubleEnable`: double-enable is idempotent.
- `TestReportEngine_StopIdempotent`: Stop() is idempotent.
- `TestValuesEqual`: 16 sub-tests covering all MMS value types.
- `TestEncodeReportValues`: report value list structure verification.
- `TestEncodeInclusion`: inclusion bitmap encoding.
- `TestEncodeEntryID`: entry ID encoding.
- `TestEncodeReasonCode`: reason code bitstring encoding.
- `TestReportEngine_SequenceNumbering`: 10 changes → seqNum=10.
- `TestReportEngine_EntryIDEncoding`: first entry ID = 1.
- `TestReportEngine_EnableWithBadDataset`: error on nonexistent dataset.
- `TestReportEngine_SqNumInStore`: SqNum written to store after report.

### Documentation updates

- `KNOWN_LIMITATIONS.md`: replaced "No runtime report delivery" with
  description of the new engine and remaining limitations.
- `doc.go`: updated server description to mention runtime report
  delivery; added Server runtime reports API group.

### Files changed

| File | Change |
|------|--------|
| `report_engine.go` | ReportEngine, rcbRuntime, bufferedEntry, change detection, report encoding, delivery, integrity timer, GI, PurgeBuf, value comparison helpers |
| `report_engine_test.go` | 23 tests covering enable/disable, triggers, buffering, encoding |
| `server.go` | reportEngine field, SetValue(), ReportEngine() accessor |
| `doc.go` | Updated server section, added runtime reports API group |
| `KNOWN_LIMITATIONS.md` | Updated server report limitations |

### Metrics

- **Tests**: 724 (all passing, +39 from previous round)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Feedback Round 10 — M11/M12 refinements

**Status**: COMPLETED

15 feedback items addressing correctness bugs, API gaps, and
documentation staleness in the M11 Controls and M12 Runtime Reports
phases.

### F1: ControlError.Unwrap() matches both sentinel and wrapped cause

Changed `ControlError.Unwrap()` from returning a single `error` to
returning `[]error` (multi-unwrap per Go 1.20+). Now `errors.Is`
matches both the operation-specific sentinel (e.g. `ErrOperateFailed`)
AND the wrapped cause simultaneously. Previously a wrapped inner error
would hide the operation sentinel.

**Files**: `errors.go`, `errors_test.go`

### F2: SBO ownership enforcement in executeOperate

`executeOperate` now verifies that the originator identity
(`OrCat:OrIdent`) of the operate matches the `selectOwner` recorded
during select. A mismatched owner is rejected with
`AddCauseSelectFailed`. This prevents one client from operating after
a different client selected the same object.

**Files**: `control_server.go`, `control_test.go`

### F3: Enhanced-security ctlNum verification

For `CtlModelSBOEnhanced` and `CtlModelDirectEnhanced`, the server
now verifies that the `ctlNum` in the operate matches the `ctlNum`
from the select. Mismatches are rejected. Also, `executeSelectWithValue`
now rejects non-enhanced models with a clear error.

**Files**: `control_server.go`, `control_test.go`

### F4: Pass caller context through executeSelect/executeCancel

Both `executeSelect` and `executeCancel` now pass through the caller's
`context.Context` to handler callbacks instead of using
`context.Background()`. This preserves cancellation, deadlines, and
request-scoped values from the MMS layer.

**Files**: `control_server.go`

### F5: Direct-operate default path writes to ValueStore

When no `OnOperate` handler is registered, `executeOperate` now writes
`CtlVal` to the ValueStore at the derived storeKey (ST/stVal). This
makes the documented default behaviour ("operates are accepted and the
value is written") actually work, and ensures reports reflect control
actions without requiring every user to write a handler.

Added `controlStoreKey()` helper to derive the store key from a
control reference.

**Files**: `control_server.go`

### F6: Document Select decode path interop caveat

Added an "Interoperability note" to `Client.Select` documenting that
SBO decode assumes a VisibleString from a normal MMS read and that
behaviour may vary across devices.

**Files**: `control.go`

### F7: Strict LastApplError decoding

`decodeLastApplError` now validates each member's type strictly.
Missing or wrong-type members return a typed error with the failing
field name instead of silently producing a partially filled struct.
This prevents broken server responses from looking valid but empty.

**Files**: `control.go`, `control_test.go`

### F8: Document ctlNumCounter scope

Added documentation clarifying that `ctlNumCounter` is a process-wide
best-effort convenience, not a protocol-level or session-scoped
guarantee. Callers requiring strict ctlNum management should set
`OperateParams.CtlNum` explicitly.

**Files**: `control.go`

### F9: pathWithoutSuffix defensive copy

`pathWithoutSuffix` now returns a new allocated slice instead of an
alias into the original backing array. Test added to verify that
modifying the result does not affect the input.

**Files**: `control_server.go`, `control_test.go`

### F10: Connection-aware report delivery

`sendReport` now delivers URCBs only to the connection that enabled
the RCB (stored as `enableConn`), while BRCBs continue to broadcast
to all connections. `HandleRCBWrite` accepts an optional `*mms.ServerConn`
parameter to capture the enabling connection.

**Files**: `report_engine.go`

### F11: Complete BRCB buffering

- BufOvfl flag: `rcbRuntime` now tracks `bufOvfl` state and sets it
  to `true` when a buffered entry is dropped due to overflow. The
  flag is propagated to `encodeReportValuesEx` and written into the
  BufOvfl optional field.
- EntryID in reports: outgoing reports now include the actual entry ID
  from the buffer instead of zeroed bytes.
- Refactored `encodeReportValues` into `encodeReportValuesEx` accepting
  a `reportParams` struct for cleaner parameterization.

**Files**: `report_engine.go`

### F12: Extended valuesEqual() for all MMS types

`valuesEqual` now handles `ValueTypeUTCTime`, `ValueTypeBinaryTime`,
`ValueTypeStructure`, and `ValueTypeArray` in addition to the
previously supported scalar and byte-sequence types. Structure/array
comparison is recursive.

**Files**: `report_engine.go`, `report_engine_test.go`

### F13: Improved qualityChanged() for nested quality

`qualityChanged` now detects quality changes within structures. For
structures following the standard IEC 61850 CDC layout (stVal at
position 0, q at position 1), the quality bitstring member at
position 1 is compared specifically. Direct bitstring comparison is
retained as a fallback.

**Files**: `report_engine.go`

### F14: Document report encoding interop caveat

Added an "Interoperability note" to the `ReportEngine` doc comment
explaining that the field ordering has been validated in loopback tests
but should be verified against real devices or libiec61850 before
production use.

**Files**: `report_engine.go`

### F15: Updated server.go docs

Rewrote the `Server` doc comment to reflect the current state:
- Stability section now says "experimental but functional" and lists
  controls and runtime report delivery as supported.
- Known limitations section updated with accurate remaining gaps
  (URCB arbitration, BRCB persistence, segmentation).
- Removed stale "not yet implemented" language for controls and reports.

**Files**: `server.go`

### Tests added

- `TestSBOOwnershipEnforcement`: verifies owner mismatch rejection.
- `TestEnhancedSBO_CtlNumMismatch`: verifies ctlNum enforcement in
  enhanced mode.
- `TestDecodeLastApplError_Strict`: verifies strict type checking
  (wrong CntrlObj type, wrong Error type, Origin not a structure).
- `TestPathWithoutSuffix_DefensiveCopy`: verifies no aliasing.
- `TestControlError_UnwrapMatchesBoth`: verifies multi-unwrap for
  both sentinel and wrapped cause.
- Updated `TestControlError_WithWrapped` to assert both sentinel and
  cause match.

### Files changed

| File | Change |
|------|--------|
| `errors.go` | ControlError.Unwrap() returns []error |
| `errors_test.go` | Updated wrapped test to assert both matches |
| `control_server.go` | SBO ownership, enhanced ctlNum, ctx passthrough, default ValueStore write, controlStoreKey, pathWithoutSuffix copy |
| `control.go` | SBO interop doc, strict LastApplError, ctlNumCounter doc |
| `control_test.go` | 5 new tests for ownership, enhanced, strict decode, defensive copy, multi-unwrap |
| `report_engine.go` | Connection-aware delivery, BufOvfl, EntryID in reports, extended valuesEqual, improved qualityChanged, interop doc |
| `server.go` | Updated doc comment for current M11/M12 state |

### Metrics

- **Tests**: 729 (all passing, +5 new tests)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Phase M13 — Setting Groups

**Status**: Complete  
**Date**: 2026-03-19

### Objectives

Implement SGCB/setting-group behavior for real IEC 61850 applications,
covering client-side APIs, server-side runtime, and validation hooks.

### Deliverables

- `setting_groups.go` — client-side setting group APIs and SGCB decoding
- `setting_groups_server.go` — server-side `SettingGroupEngine` runtime
- `setting_groups_test.go` — 21 tests covering all paths
- SCL `SettingControl` parsing and model integration
- Server-side SGCB registration in MMS variable registry

### M13.1 — Client-side setting group APIs

- `GetSettingGroupInfo(ctx, ld)` — reads SGCB from LLN0$SP$SGCB and
  decodes NumOfSGs, ActSG, EditSG, CnfEdit, ResvTms
- `SelectActiveSG(ctx, ld, sg)` — writes ActSG via MMS WriteComponent
- `SelectEditSG(ctx, ld, sg)` — writes EditSG to start an edit session
- `ConfirmEditSG(ctx, ld)` — writes CnfEdit=true to commit edits
- `GetEditSGValue(ctx, ref)` — reads DA value with FC=SE
- `SetEditSGValue(ctx, ref, value)` — writes DA value with FC=SE
- `GetActiveSGValue(ctx, ref)` — reads DA value with FC=SG
- `decodeSGCB(v)` — strict structure decoder with type validation

### M13.2 — Server-side SGCB runtime

- `SettingGroupDef` added to `servermodel.LogicalNode` — holds NumOfSGs,
  ActSG, ResvTms
- SCL `SettingControl` element parsed in `scl.LN` and propagated
  through `FromSCL` to `SettingGroupDef`
- `registerSGCB` registers SGCB subfields (NumOfSGs, ActSG, EditSG,
  CnfEdit, LActTm, ResvTms) as MMS variables with read/write callbacks
- `SettingGroupEngine` manages live SGCB state per LD:
  - `sgcbRuntime` holds numOfSGs, actSG, editSG, handler
  - `HandleSGCBWrite` dispatches ActSG/EditSG/CnfEdit writes
  - `syncSGCBToStore` writes runtime state back to ValueStore
- `Server.EnableSettingGroups(handler)` scans model and registers SGCBs
- `Server.SettingGroupEngine()` accessor
- `Server.ChangeActiveSettingGroup(ctx, ld, sg)` programmatic API

### M13.3 — Validation hooks

- `SettingGroupHandler` with three optional callbacks:
  - `OnActiveSGChanged` — reject/accept active group change
  - `OnEditSGSelected` — reject/accept edit session start
  - `OnConfirmEdit` — reject/accept edit confirmation
- Edit conflict detection: attempting to edit a different group while
  another edit session is active returns an error
- Out-of-range group number validation for both ActSG and EditSG
- Read-only subfield protection (NumOfSGs, LActTm, ResvTms writes rejected)

### Tests

21 tests covering:
- SGCB decode (valid, without ResvTms, too few members, wrong type, not structure)
- Active SG (change, out-of-range, handler rejection, store sync)
- Edit SG (select, release, conflict, handler rejection, out-of-range)
- Confirm edit (success with callback, no session, handler rejection,
  CnfEdit=false no-op)
- Read-only subfield, unknown LD, engine not enabled, SGCB in store,
  no SGCB in model

### Files changed

| File | Change |
|------|--------|
| `setting_groups.go` | NEW — client APIs, SGCB decode, SettingGroupInfo type |
| `setting_groups_server.go` | NEW — SettingGroupEngine, SettingGroupHandler, server integration |
| `setting_groups_test.go` | NEW — 21 tests |
| `scl/model.go` | Added SettingControl type to LN |
| `scl/parse.go` | Added xmlSettingControl parsing, SettingControl conversion |
| `internal/servermodel/model.go` | Added SettingGroupDef type to LogicalNode |
| `internal/servermodel/fromscl.go` | Convert SCL SettingControl → SettingGroupDef |
| `internal/servermodel/register.go` | registerSGCB function, wire into registerLN |
| `server.go` | Added sgEngine field, updated doc comment |
| `doc.go` | Added setting group API groups |
| `KNOWN_LIMITATIONS.md` | Added setting group engine limitations |

### Metrics

- **Tests**: 821 (all passing, +92 from previous)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Phase M14 — Logs and journals runtime

**Status**: Complete  
**Date**: 2026-03-19

### Objectives

Move from journal-reading support to fuller runtime log/journal behavior
on the server side, including a provider abstraction, in-memory storage,
runtime entry generation, and deterministic cursor pagination.

### Deliverables

- `journal_provider.go` — `MemoryJournalProvider` implementing `mms.JournalProvider` with ring-buffer semantics and monotonic entry IDs
- `journal_engine.go` — `JournalEngine` for runtime journal entry generation from value changes and application events
- `journal_engine_test.go` — 33 tests covering all provider, engine, and integration paths
- Server model integration: `LogDef` type in server model, SCL log conversion, model-aware journal naming
- Server integration: `Server.EnableJournals`, `Server.JournalEngine`, automatic logging from `Server.SetValue`

### Tasks

#### M14.1 — Journal provider abstraction

- Created `MemoryJournalProvider` implementing `mms.JournalProvider`
  (ListJournals, ReadTimeRange, ReadStartAfter)
- Configurable max capacity via `WithMaxEntries` option (default 10000)
- Ring-buffer overflow: oldest entries discarded when capacity exceeded
- Thread-safe with `sync.RWMutex`
- `RegisterJournal` for explicit journal creation; `AddEntry` for
  implicit creation with auto-assigned monotonic entry IDs
- `EntryCount` accessor for testing and diagnostics

#### M14.2 — Runtime log entry generation

- Created `JournalEngine` with `LogEvent` for custom application events
  and `LogValueChange` for automatic value-change logging
- `LogValueChange` reads current value from `ValueStore`, logs to all
  journals in the matching logical device
- `Server.SetValue` automatically calls `LogValueChange` when the
  journal engine is enabled
- Configurable via `WithJournalMaxEntries` and `WithJournalProvider`
  options

#### M14.3 — Server journal exposure

- Added `LogDef` type to `internal/servermodel/model.go`
- Updated `internal/servermodel/fromscl.go` to convert SCL `Log`
  elements into `LogDef` entries on `LogicalNode`
- `JournalEngine.registerJournalsFromModel` scans the model for log
  definitions and registers MMS journals with naming convention
  `"LNName$logName"` (e.g. `"LLN0$log1"`)
- `Server.EnableJournals` creates the engine and stores it on the server
- `JournalEngine.Provider()` exposes the provider for passing to
  `ServerOptions.MMS.JournalProvider`

#### M14.4 — Cursor correctness

- Entry IDs are 8-byte big-endian monotonically increasing integers
- Same-timestamp entries get unique entry IDs, ensuring
  `ReadStartAfter` never skips or duplicates
- Dedicated tests verify no-duplicate/no-skip pagination for both
  unique-timestamp and same-timestamp scenarios across page boundaries

### Files Changed

| File | Change |
|------|--------|
| `journal_provider.go` | New: `MemoryJournalProvider`, ring-buffer journal storage |
| `journal_engine.go` | New: `JournalEngine`, runtime entry generation, server integration |
| `journal_engine_test.go` | New: 33 tests for provider, engine, pagination, integration |
| `internal/servermodel/model.go` | Added `LogDef` type and `Logs` field to `LogicalNode` |
| `internal/servermodel/fromscl.go` | Convert SCL `Log` elements to `LogDef` |
| `server.go` | Added `journalEngine` field, `time` import, journal-aware `SetValue`, updated doc comment |
| `doc.go` | Added server journals API group, updated Server description |
| `KNOWN_LIMITATIONS.md` | Replaced "no journal serving" with "journal engine is initial" |

### Metrics

- **Tests**: 854 (all passing, +33 from previous)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Feedback Round 11 — M13/M14 Refinements

**Status**: Complete  
**Date**: 2026-03-19

### Summary

Addressed 15 feedback items covering semantic correctness,
integration depth, and documentation for M13 (Setting Groups)
and M14 (Journals) implementations.

### Items

#### F1: ControlError multi-unwrap (already correct)

Verified that `ControlError.Unwrap() []error` already returns both
the operation sentinel and the wrapped error, confirmed by existing
test `TestControlError_WithWrapped`.

#### F2: SGCB writes now dispatched to SettingGroupEngine

Added `WriteInterceptor` to `ValueStore` that is called by SGCB
subfield Write callbacks. `EnableSettingGroups` installs a write
interceptor that dispatches ActSG/EditSG/CnfEdit writes to
`SettingGroupEngine.HandleSGCBWrite`. SGCB callbacks intercept
BEFORE the store write (the engine handles store sync).

#### F3: RCB writes now dispatched to ReportEngine

RCB subfield Write callbacks now call the write interceptor,
which dispatches RptEna/GI/Resv/PurgeBuf writes to
`ReportEngine.HandleRCBWrite`. RCB callbacks write to store FIRST,
then call the interceptor (the engine reads config from the store).

#### F4: Per-group value banks documented as not implemented

Updated `KNOWN_LIMITATIONS.md` to explicitly state that persistent
multi-bank SE/SG value storage is not implemented.

#### F5: Reservation semantics documented as initial

Updated limitation text to mention ResvTms timeout, multi-connection
ownership, and connection-scoped reservation as not yet implemented.

#### F6: LActTm only updated on activation

Changed `syncSGCBToStore` to accept `updateLActTm bool`. Only
`handleActSGWrite` passes `true`; edit and confirm operations pass
`false`. Added doc comment clarifying NumOfSGs/ResvTms as immutable.

#### F7: MoreFollows false positive fixed

Changed `ReadTimeRange` and `ReadStartAfter` to check for additional
entries BEYOND the page before setting `MoreFollows=true`. Previously,
`MoreFollows` was true when the page ended exactly on the last entry.
Added tests verifying exact-page-size scenarios return `false`.

#### F8: ListJournals deterministic

Added `sort.Strings` to `ListJournals` output. Added test verifying
sorted order.

#### F9: LogValueChange renamed to LogValueWrite

Renamed to `LogValueWrite` to accurately reflect that every write is
logged regardless of whether the value changed (audit trail semantics,
not change detection).

#### F10: Journal provider auto-adoption

`NewServer` auto-detects `*MemoryJournalProvider` in
`ServerOptions.MMS.JournalProvider` and stores it. If no provider is
set but the model has logs, one is auto-created and installed.
`EnableJournals` auto-adopts the stored provider. Added test.

#### F11: Buffered report replay gap documented

Updated `KNOWN_LIMITATIONS.md` to explicitly mention no replay/readout
path for reconnecting clients.

#### F12: qualityChanged documented as heuristic

Added doc comment explaining the position-1 quality detection is a
pragmatic approximation for common CDCs, not semantically complete.

#### F13: controlStoreKey documented as convenience fallback

Added doc comment explaining the `$ST$...$stVal` mapping is a
convenience for SPC/DPC-style CDCs and that applications with
different CDCs should handle stVal updates in their OnOperate callback.

#### F14: NumOfSGs=0 validation

Added validation in `model.Validate()` that `NumOfSGs >= 1` when a
`SettingGroupDef` is present. Also added empty log name validation.
Added tests for both.

#### F15: SGCB limitation text tightened

Updated `KNOWN_LIMITATIONS.md` to clearly state "SGCB state exists,
but persistent multi-bank SE/SG value storage is not implemented"
and to mention MMS write dispatch is now functional.

### Files Changed

| File | Change |
|------|--------|
| `internal/servermodel/register.go` | Added `WriteInterceptor`, `SetWriteInterceptor`, `callInterceptor`, `CallInterceptorForTest`; updated RCB/SGCB Write callbacks |
| `server.go` | Added `mmsJournalProvider` field, `installWriteInterceptor`, `parseRCBStoreKey`, `parseSGCBStoreKey`; auto-detect journal provider in `NewServer` |
| `setting_groups_server.go` | `syncSGCBToStore` takes `updateLActTm bool`; only activation updates LActTm; calls `installWriteInterceptor` |
| `report_engine.go` | Calls `installWriteInterceptor`; `qualityChanged` documented as heuristic |
| `journal_provider.go` | Fixed MoreFollows false positive; sorted `ListJournals` |
| `journal_engine.go` | Renamed `LogValueChange` → `LogValueWrite`; auto-adopt MMS provider |
| `journal_engine_test.go` | Added 5 new tests (MoreFollows, sort, auto-adopt) |
| `coverage_test.go` | Added 4 new tests (RCB/SGCB key parsing, interceptor dispatch) |
| `internal/servermodel/model.go` | Added NumOfSGs=0 and empty log name validation |
| `internal/servermodel/model_test.go` | Added 2 validation tests |
| `control_server.go` | Documented `controlStoreKey` as convenience fallback |
| `doc.go` | Mentioned write interceptor dispatch |
| `KNOWN_LIMITATIONS.md` | Tightened SGCB, report, and journal limitation text |

### Metrics

- **Tests**: 864 (all passing, +10 from previous)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Phase M15 — Fuller server services

**Status**: Complete  
**Date**: 2026-03-19

### Objectives

Complete the missing server-side service surface around the experimental
server by adding provider-oriented wiring, first-class configuration
fields, connection lifecycle hooks, capability introspection, and
service integration.

### Deliverables

- `server_services.go` — `ServerIdentity`, `ConnectionEvent`, `ServiceCapabilities` types; `Server.Capabilities()`, `Server.HandleIdentify()`, `Server.HandleStatus()` methods; `ServiceCapabilities.String()` for diagnostic output
- `server_services_test.go` — 15 tests covering identity, file provider, capabilities, connection hooks, identify/status round-trips, and string formatting
- Extended `ServerOptions` with first-class `Identity`, `FileProvider`, `Authenticate`, `OnConnect`, `OnDisconnect` fields
- Custom `listenAndServeWithHooks` for connection lifecycle callback support

### M15.1 — Provider-oriented server wiring

Extended `ServerOptions` with convenience fields:
- `Identity *ServerIdentity` — registers MMS Identify handler
- `FileProvider mms.FileProvider` — enables MMS file services
- `Authenticate mms.Authenticator` — configures association authentication

These fields are wired into `mms.ServerOptions` during `NewServer`
construction. Direct `MMS` field values take precedence when both
are set.

### M15.2 — File service integration

`ServerOptions.FileProvider` is automatically forwarded to
`mms.ServerOptions.FileProvider` in `NewServer`. The
`ServiceCapabilities.Files` flag reflects whether a file provider
is configured.

### M15.3 — Identify/status/session hooks

- **Identify**: `ServerOptions.Identity` or `Server.HandleIdentify()`
  registers a static MMS Identify response (vendor/model/revision).
- **Status**: `Server.HandleStatus()` is called automatically by
  `NewServer`, registering a static operational status response.
- **Connection lifecycle**: `ServerOptions.OnConnect` and
  `OnDisconnect` callbacks are invoked at the start and end of each
  `Server.Serve` call. When hooks are configured, `ListenAndServe`
  uses a custom accept loop (`listenAndServeWithHooks`) to route
  through `Server.Serve`.

### M15.4 — Error/service mapping cleanup

- `ServiceCapabilities` struct reports which services are active:
  variables, datasets, reports, controls, setting groups, journals,
  files, identify.
- `Server.Capabilities()` builds the capability snapshot from runtime
  state (engine fields, control registrations, model inspection).
- `ServiceCapabilities.String()` produces human-readable output for
  startup logging and diagnostics.
- `ErrUnsupportedService` sentinel (already present) covers
  unsupported-service errors.

### Files Changed

| File | Change |
|------|--------|
| `server.go` | Extended `ServerOptions` with `Identity`, `FileProvider`, `Authenticate`, `OnConnect`, `OnDisconnect`; added `hasFileProvider`, `hasIdentity`, `onConnect`, `onDisconnect` to `Server`; wired convenience fields in `NewServer`; wrapped `Serve` with lifecycle hooks; added `listenAndServeWithHooks` |
| `server_services.go` | New file: `ServerIdentity`, `ConnectionEvent`, `ServiceCapabilities` types; `Capabilities()`, `HandleIdentify()`, `HandleStatus()`, `String()` methods |
| `server_services_test.go` | New file: 15 tests for identity, file provider, MMS precedence, default capabilities, reports, controls, connection hooks, identify/status round-trips, string formatting |
| `doc.go` | Added server services API group; updated server section with ServerOptions documentation |
| `KNOWN_LIMITATIONS.md` | Updated file serving limitation to reflect provider-based architecture |

### Metrics

- **Tests**: 879 (all passing, +15 from previous)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Phase M16 — Hardening, conformance, and runtime stabilization

**Status**: Complete  
**Date**: 2026-03-19

### Objectives

Stabilize the server runtime engines (reports, controls, setting
groups, journals) before further expansion. Fix data races, add
failure-injection tests, document state machines, and ensure
deterministic shutdown.

### Deliverables

- Concurrency audit and race fix
- Failure-injection and state-machine misuse tests
- Server lifecycle (`Server.Close`) for orderly shutdown
- Conformance checklist in `interop/README.md`
- Documentation refresh

### M16.1 — Concurrency audit

**Race condition fixed** in `control_server.go` `executeOperate`:
`reg.selectOwner` was read in a log message after `reg.mu.Unlock()`,
causing a data race when multiple goroutines concurrently perform
SBO select/operate. Fixed by capturing all guarded fields into local
variables before releasing the mutex.

**Redundant atomic removed** in `report_engine.go` `sendReport`:
`atomic.AddUint64(&rt.entryID, 1)` was used while already holding
`rt.mu.Lock()`. Replaced with a plain increment since the mutex
provides the synchronization. Removed the `sync/atomic` import.

**Lock ordering documented**: Engine-level RWMutex → per-element
Mutex across all runtime engines (report, control, setting group,
journal). Verified with concurrent stress tests under `-race`.

### M16.2 — Failure-injection tests

Added 33 new tests covering:

| Category | Tests |
|----------|-------|
| Concurrent SetValue (50 goroutines) | 1 |
| Concurrent enable/disable RCB | 1 |
| Concurrent GI triggers | 1 |
| Concurrent SBO select/operate (10 goroutines) | 1 |
| Concurrent setting group writes | 1 |
| Concurrent journal add/read | 1 |
| Concurrent buffer overflow | 1 |
| Rapid enable/disable cycling | 1 |
| Stop-then-SetValue (post-shutdown safety) | 1 |
| Invalid RptEna/GI/Resv types | 3 |
| Unknown subfield / RCB writes | 1 |
| Setting group boundary conditions | 8 |
| Journal provider edge cases | 3 |
| Server.Close idempotency | 2 |
| SetValue without engines | 1 |
| Control misuse (no select, duplicate, status-only, empty args) | 4 |
| Full server lifecycle round-trip | 1 |
| SetValue triggers report + journal | 1 |

### M16.3 — Interop/conformance pass

Added a comprehensive conformance checklist to `interop/README.md`
covering all tested server-side state machine behaviors:
- Report engine (12 items)
- Control runtime (6 items)
- Setting group engine (9 items)
- Journal provider (4 items)
- Server lifecycle (4 items)

### M16.4 — Documentation refresh

- Updated `doc.go` with concurrency safety guarantees, `Server.Close`
  mention, and lock ordering documentation
- Updated `Server` struct doc comment to mention `Close`
- Updated `KNOWN_LIMITATIONS.md` concurrency section to document
  server runtime engine thread safety
- Added `Server.Close()` method for orderly runtime shutdown

### Files Changed

| File | Change |
|------|--------|
| `control_server.go` | Fixed data race in `executeOperate`: captured guarded fields before unlock |
| `report_engine.go` | Removed redundant `atomic.AddUint64` under mutex; removed `sync/atomic` import |
| `server.go` | Added `Server.Close()` method; updated struct doc comment |
| `hardening_test.go` | New file: 33 concurrency, failure-injection, and lifecycle tests |
| `doc.go` | Added concurrency documentation, `Server.Close` reference |
| `KNOWN_LIMITATIONS.md` | Added server runtime engine thread safety documentation |
| `interop/README.md` | Added server runtime conformance checklist (35 items) |

### Metrics

- **Tests**: 912 (all passing, +33 from previous)
- **Lint issues**: 0
- **Race conditions**: 0 (fixed 1 pre-existing race in control runtime)

---

## Feedback Round 12 — M15/M16 review

**Status**: Complete  
**Date**: 2026-03-19

### Summary

Addressed 7 actionable items from the M15/M16 review covering
API honesty, shutdown completeness, interceptor safety, and
documentation accuracy.

### Items

| # | Feedback | Action |
|---|----------|--------|
| F6 | `ConnectionEvent.Conn` is always nil | Removed `Conn *mms.ServerConn` field; documented that transport-level events do not expose connection context until the MMS API supports it |
| F7 | `Server.Close()` only stops report engine | Extended to also nil out `journalEngine` and `sgEngine` references; documented that those engines are stateless (no goroutines to stop) |
| F11 | Interceptor recursion hazard between `HandleRCBWrite`/`HandleSGCBWrite` and `store.Set` | Documented the no-recursion contract (interceptor fires only from MMS Write callbacks, not `ValueStore.Set`); added `TestInterceptor_NoRecursion` test |
| F12 | `parseRCBStoreKey` is string-fragile | Added doc comment explaining the heuristic nature and when a structural approach would be warranted |
| F13 | `HandleRCBWrite` silently ignores unknown subfields | Added `default` case with `re.logger.Debug` log for unhandled subfields |
| F16 | `OnConnect` fires before MMS association, not "after successful establishment" | Tightened `ServerOptions` and `Serve` doc comments to accurately describe transport-accept timing (OnConnect fires before handshake; OnDisconnect fires when Serve returns) |
| F17/F18 | Connection hook tests verify counts but not payload; `ConnectionEvent.Conn` overpromises | Resolved by F6 — simplified the type to match what is actually available |

### Files Changed

| File | Change |
|------|--------|
| `server_services.go` | Removed `Conn` field from `ConnectionEvent`; added doc explaining the limitation |
| `server.go` | Extended `Close()` to clear journal/SG engines; tightened `OnConnect`/`OnDisconnect` timing docs; documented interceptor no-recursion contract; documented `parseRCBStoreKey` heuristic |
| `report_engine.go` | Added `default` case in `HandleRCBWrite` switch with debug log |
| `hardening_test.go` | Added `TestInterceptor_NoRecursion` and `TestServer_Close_ClearsEngines` |

### Metrics

- **Tests**: 914 (all passing, +2 from previous)
- **Lint issues**: 0
- **Race conditions**: 0

---

## Feedback Round 13 — Stability, correctness, and API review

**Status**: Complete  
**Date**: 2026-03-19

### Summary

Addressed 18 actionable items from a comprehensive stability and
correctness review, covering cache correctness bugs, client lifecycle
improvements, error wrapping consistency, control documentation,
journal immutability, SCL validation, and API documentation.

### Items

| # | Feedback | Action |
|---|----------|--------|
| F2 | `Client.MMS()` escape hatch needs stronger docs | Rewrote doc comment as explicit warning listing cache, subscription, and lifecycle corruption risks |
| F3 | Close/Abort — lifecycle ambiguity | Introduced `clientState` enum (`clientOpen` → `clientClosing` → `clientClosed`); `isClosing()` helper; cleanup logs correctly downgraded during shutdown |
| F4 | `RefreshCache` forgets `parsedByLD` | Added `parsedByLD = make(...)` in atomic swap block alongside `itemsByLD` |
| F5 | `getParsedRefs` returns internal slice | Changed to return a defensive copy |
| F6 | `setParsedRefs` stores caller-owned slice | Changed to store a defensive copy |
| F7 | `RefreshLDCache` indirect coupling for parsed-ref reset | Added explicit documentation that `invalidateLD` clears both `itemsByLD` and `parsedByLD` |
| F9 | `FindPaths` FC deduplication semantics unclear | Documented that deduplication uses full reference (path + FC), yielding one result per FC |
| F12 | `Write` nil value returns plain error | Wrapped with `ErrInvalidArgument` for machine-detectable validation |
| F14 | Default ctlNum strategy weakly correct | Expanded doc comment with enhanced-security caveats and explicit ownership recommendation |
| F17 | SBO ownership under-specified for multi-client | Documented origin-only ownership limitation and future connection-binding improvement |
| F18 | Server control timeout not configurable | Renamed `selectTimeout` → `DefaultSelectTimeout` (exported); added wall-clock caveat |
| F20 | Default control fallback misleading | Labeled the default `OnOperate` fallback as demo-grade; documented that real apps need explicit handlers |
| F22 | `DefensiveCopy` name/docs create false confidence | Expanded `ValueStoreOptions.DefensiveCopy` doc to bluntly state shallow-only limitation and list affected types |
| F23 | Report engine interceptor recursion assumptions fragile | Added invariant comments at `store.Set` hot paths in `report_engine.go` and `setting_groups_server.go` |
| F29/F30 | `MemoryJournalProvider.AddEntry` — aliasing risk | Added defensive copy of `[]mms.JournalVariable` including shallow value copy to prevent journal corruption |
| F32 | SCL `Count > 1` silently flattened | `expandDO`/`expandDA` now emit model warnings when DA/BDA `count > 1` will be registered as scalar |
| F35 | Capabilities — configured vs enabled | Enhanced `ServiceCapabilities` docs to distinguish model presence from runtime enablement |

### Files Changed

| File | Change |
|------|--------|
| `cache.go` | `getParsedRefs` returns copy; `setParsedRefs` stores copy; `RefreshCache` resets `parsedByLD`; `RefreshLDCache` docs clarify invariant |
| `client.go` | `clientState` enum; `isClosing()` helper; `Close`/`Abort` use state transitions; duplicate Close doc deduplicated |
| `write.go` | Nil value error wraps `ErrInvalidArgument` |
| `browse.go` | `FindPaths` FC deduplication documented |
| `control.go` | `ctlNumCounter` doc expanded with enhanced-security caveats |
| `control_server.go` | `controlRegistration` SBO ownership limitation documented; `DefaultSelectTimeout` exported with wall-clock caveat; default fallback labeled demo-grade |
| `control_test.go` | Updated for `state` field rename |
| `report.go` | Uses `isClosing()` instead of raw `closed` field |
| `report_engine.go` | Invariant comment at `store.Set` calls |
| `setting_groups_server.go` | Invariant comment at `syncSGCBToStore` |
| `journal_provider.go` | `AddEntry` defensively copies variables |
| `server_services.go` | `ServiceCapabilities` docs distinguish configured vs runtime-enabled |
| `internal/servermodel/register.go` | `DefensiveCopy` docs expanded |
| `internal/servermodel/fromscl.go` | `expandDO`/`expandDA` emit warnings for `Count > 1`; threaded warnings parameter |

### Metrics

- **Tests**: 914 (all passing)
- **Lint issues**: 0
- **Race conditions**: 0

---
