Here is the concrete deletion-and-refactor checklist.

Goal

End state:
	•	one XML decode path only
	•	generated raw contracts only
	•	one normalized handwritten runtime model
	•	one shared index/resolution layer
	•	one diagnostic model
	•	one validator pipeline
	•	one CLI path (sclparse) using the same runtime

⸻

Phase 0 — Freeze architecture rules

Before touching code, set these rules and keep them strict.

Rule 0.1

No handwritten XML mirror structs anywhere in scl.

Rule 0.2

No fallback parser path for unknown or unsupported SCL versions.

Rule 0.3

All parsing must go through:
	•	DetectVersion
	•	generated raw package
	•	version converter
	•	normalized model

Rule 0.4

All runtime consumers must use the same normalized model and diagnostics.

Rule 0.5

All cross-reference logic must converge on one shared index layer.

⸻

Phase 1 — Delete the legacy handwritten XML parser layer

This is the most important cleanup.

1.1 Find and remove handwritten XML transport structs

Delete any structs like:
	•	xmlSCL
	•	xmlHeader
	•	xmlIED
	•	xmlAccessPoint
	•	xmlServer
	•	xmlLDevice
	•	xmlLN
	•	xmlCommunication
	•	xmlDataTypeTemplates
	•	any other xml* transport types

These should no longer exist in parse.go or anywhere else.

1.2 Delete handwritten XML decode helpers

Delete any functions that:
	•	decode directly into handwritten XML transport structs
	•	convert handwritten XML transport structs into *SCL
	•	inspect XML structure outside of version detection

Examples to remove:
	•	legacy Parse(...) body that unmarshals directly into handwritten XML structs
	•	helper converters tied to xml* structs

1.3 Delete legacy fallback branches

If current runtime dispatch still has logic like:
	•	“if version detect fails, try old parser”
	•	“if decode fails, fall back to handwritten path”

delete it.

Unsupported version must be an error.

1.4 Remove compatibility tests for the old parser

Delete or rewrite tests whose only purpose is:
	•	old parser correctness
	•	equivalence between old handwritten parser and new generated path

Once the old parser is gone, those tests are no longer useful.

1.5 Update comments and docs

Search comments for phrases like:
	•	fallback
	•	legacy parser
	•	handwritten XML model
	•	old parse path

Rewrite them to describe the new single-path architecture.

⸻

Phase 2 — Harden version detection into a strict gateway

2.1 Refactor DetectVersion to be root-attribute only

Implementation checklist:
	•	use xml.Decoder
	•	stream tokens until first xml.StartElement
	•	verify it is SCL
	•	read only StartElement.Attr
	•	do not call DecodeElement
	•	stop immediately after collecting root metadata

2.2 Add exact schema tuple classification

Implement exact supported tuple mapping only:
	•	classic namespace + no version/revision/release → 1.7
	•	2007/B/(empty) → 2007B
	•	2007/B/4 → 2007B4
	•	2007/C/5 → 2007C5

Everything else:
	•	VersionUnknown

2.3 Parse release numerically

Do not compare release lexically as strings.

Implementation:
	•	parse release via strconv.Atoi
	•	absent release remains “unset”
	•	malformed release becomes “unknown tuple” or a diagnostic

2.4 Move confidence into core detection output

Extend VersionInfo to include:
	•	Confidence
	•	optionally Reason []string

Then sclparse detect should print directly from VersionInfo, not invent confidence externally.

2.5 Add vendor namespace collection

At detection time, collect non-IEC namespace declarations on the root element.

Store these in VersionInfo or metadata so downstream code can see:
	•	ABB
	•	Siemens
	•	other vendor extensions

2.6 Add strict tests for detection

Required tests:
	•	valid 1.7
	•	valid 2007B
	•	valid 2007B4
	•	valid 2007C5
	•	unknown tuple
	•	malformed root
	•	non-SCL XML
	•	detection does not parse whole file

⸻

Phase 3 — Make parse runtime explicit and single-path

3.1 Simplify parse.go

parse.go should become orchestration only.

Target responsibilities:
	•	read bytes
	•	detect version
	•	call internal decode/convert
	•	optionally build index
	•	optionally run validation
	•	return Result

It should not contain schema-shaped XML transport logic.

3.2 Standardize public parse entrypoints

Target API set:

func DetectVersion(data []byte) (VersionInfo, error)
func DetectFile(path string) (VersionInfo, error)

func ParseBytes(data []byte, opts ParseOptions) (*Result, error)
func ParseFile(path string, opts ParseOptions) (*Result, error)

If you want compatibility wrappers, keep them thin and explicit.

3.3 Expand ParseOptions

Target fields:

type ParseOptions struct {
	ValidateSemantic   bool
	PreserveExtensions bool
	Strict             bool
	KindHint           DocumentKind
	MaxDiagnostics     int
}

No legacy fallback option.

3.4 Define strict parse semantics

Decide and document:
	•	what Strict means for parse diagnostics
	•	whether semantic validation runs automatically
	•	whether warnings fail strict mode
	•	how MaxDiagnostics truncates results

3.5 Ensure one internal dispatch layer

All version-specific XML decoding must be centralized in:

scl/internal/decode/dispatch.go

That file should:
	•	switch on SchemaVersion
	•	unmarshal into correct generated raw SCL
	•	call matching converter
	•	return normalized model + diagnostics

No duplicated dispatch logic elsewhere.

⸻

Phase 4 — Strengthen the normalized handwritten model

4.1 Add metadata to root SCL

Add:

type DocumentMetadata struct {
	Version           VersionInfo
	Kind              DocumentKind
	OriginalNamespace string
	VendorNamespaces  []string
}

And on SCL:

Metadata *DocumentMetadata

4.2 Keep the handwritten model semantic, not schema-shaped

Review model fields and remove any remaining fields that exist only because the XML schema had them in that form.

The model should optimize for:
	•	queries
	•	validation
	•	flattening
	•	exports
	•	CLI inspection

4.3 Add generic extension preservation types

Add types like:

type ExtensionNode struct {
	Namespace string
	LocalName string
	Attrs     map[string]string
	InnerXML  string
}

type PrivateNode struct {
	Type     string
	Source   string
	InnerXML string
}

Then add them where needed in normalized structures.

4.4 Ensure converters populate metadata and extensions

Each convertV* file should:
	•	set root metadata
	•	preserve Private
	•	preserve vendor extensions if PreserveExtensions is enabled

⸻

Phase 5 — Replace fragmented runtime traversal with a shared index layer

This is the next major structural win.

5.1 Create scl/index

Suggested files:

scl/index/index.go
scl/index/keys.go
scl/index/resolve.go

5.2 Define stable key types

Introduce keys for:
	•	access point
	•	logical device
	•	logical node
	•	dataset
	•	control block

Examples:
	•	AccessPointKey
	•	LDeviceKey
	•	LNKey
	•	DataSetKey
	•	ControlKey

5.3 Build a full document index

Index should include:
	•	IED by name
	•	AccessPoint by key
	•	LDevice by key
	•	LN by key
	•	LNodeType by ID
	•	DOType by ID
	•	DAType by ID
	•	EnumType by ID
	•	DataSet by key
	•	ReportControl by key
	•	GSEControl by key
	•	SMVControl by key
	•	ConnectedAP by (IED, AP)

5.4 Make index build diagnostic-aware

If duplicates or ambiguous ownership are found during index construction, emit diagnostics rather than silently overwriting.

5.5 Add resolver helpers on top of the index

Examples:
	•	find template by ID
	•	resolve FCDA target
	•	resolve substation LNode
	•	resolve ConnectedAP
	•	find dataset by owner LN + name

These will power both validation and CLI inspection.

⸻

Phase 6 — Unify diagnostics and remove legacy validation types

6.1 Delete ValidationFinding

Completely retire it.

Use only:

type Diagnostic struct {
	Severity Severity
	Code     string
	Path     string
	Message  string
}

6.2 Standardize diagnostic codes

Define and use stable codes such as:
	•	unsupported-schema-version
	•	unknown-scl-version
	•	malformed-release
	•	duplicate-id
	•	duplicate-ied
	•	duplicate-access-point
	•	duplicate-ld
	•	duplicate-ln
	•	missing-lnodetype
	•	missing-dotype
	•	missing-datype
	•	missing-enumtype
	•	missing-connected-ap
	•	unresolved-fcda
	•	missing-dataset
	•	unresolved-substation-lnode

6.3 Unify all diagnostic sources

Diagnostics should come from:
	•	detection
	•	decode/convert
	•	index build
	•	semantic validation

and use the same structure.

6.4 Add path formatting rules

Make path formatting stable and predictable for tests/CLI:
	•	root paths
	•	indexed collection elements
	•	owner-qualified logical nodes
	•	template refs

Pick one style and use it everywhere.

⸻

Phase 7 — Split semantic validation into passes

7.1 Create scl/validate

Suggested files:

scl/validate/validate.go
scl/validate/templates.go
scl/validate/ieds.go
scl/validate/communication.go
scl/validate/datasets.go
scl/validate/controls.go
scl/validate/topology.go

7.2 Make validator input explicit

Validators should accept:
	•	normalized *scl.SCL
	•	shared *index.Index

No validator should walk raw structs directly.

7.3 Implement passes in this order

templates

Check:
	•	duplicate type IDs
	•	missing referenced types
	•	broken base chains if applicable
	•	orphan obvious template issues

ieds

Check:
	•	duplicate IED names
	•	duplicate AP names per IED
	•	duplicate LD instance names per AP
	•	duplicate LN identity (prefix, lnClass, inst) per LD
	•	missing LN type refs

communication

Check:
	•	ConnectedAP resolves to a real IED/AP
	•	address structures are coherent enough
	•	GSE/SMV linkage targets are not obviously broken

datasets

Check:
	•	dataset names unique in owner scope
	•	FCDA references resolve to actual nodes/data paths
	•	malformed FCDA ownership references

controls

Check:
	•	report control dataset exists
	•	GOOSE control dataset exists
	•	SMV control dataset exists
	•	owner scope is valid

topology

Check:
	•	substation LNode resolves to real IED/LD/LN
	•	basic topology cross-references hold

7.4 Add validator options later if needed

Once split passes exist, you can add filtering options more safely.

⸻

Phase 8 — Refactor flattening, lookup, and exports to use the shared index

8.1 Refactor Flatten

Remove local indexing/resolution logic from flatten.go.

Flatten should depend on:
	•	normalized model
	•	shared index
	•	shared template resolver

8.2 Refactor lookup.go

Any current linear-scan helper should:
	•	either become a wrapper over the index
	•	or be deleted if redundant

8.3 Refactor export helpers

Make these use the shared resolver:
	•	ExportDataSets
	•	ExportReports

Then add later:
	•	ExportGOOSE
	•	ExportSMV
	•	ExportCommunication
	•	ExportTemplates

8.4 Keep behavior stable with tests

Before refactoring, snapshot current outputs so the refactor does not quietly change semantics.

⸻

Phase 9 — Expand sclparse into the official integration harness

9.1 Ensure sclparse uses only public/runtime APIs

No private parser shortcuts in CLI code.

9.2 Keep current commands and improve them

Current:
	•	detect
	•	summary
	•	validate
	•	dump-json

Make them fully consume:
	•	unified diagnostics
	•	metadata on root model
	•	shared index if needed

9.3 Add next commands

Implement:
	•	list-ieds
	•	list-lns
	•	list-datasets
	•	list-reports

Then:
	•	list-goose
	•	list-smv
	•	list-connected-ap
	•	list-types

9.4 Improve validate command

Make it print unified diagnostics from:
	•	parse/conversion
	•	index
	•	semantic validation

Support:
	•	--json
	•	--strict
	•	--warnings-as-errors
	•	--max-errors

9.5 Add better summary detail

Optional next additions:
	•	logs
	•	setting controls
	•	services presence
	•	LN0 count vs non-LN0 count
	•	GSE/SMV addressed ConnectedAP counts

⸻

Phase 10 — Test and fixture cleanup after the parser deletion

10.1 Remove old parser-specific tests

Delete tests that only existed to cover handwritten XML mirror decode behavior.

10.2 Add end-to-end tests for the only supported runtime path

Per supported version:
	•	detect
	•	decode
	•	convert
	•	index
	•	validate
	•	summarize

10.3 Add negative fixtures

Required broken fixtures:
	•	unknown schema tuple
	•	malformed release
	•	missing template ref
	•	unresolved FCDA
	•	missing dataset for control block
	•	bad ConnectedAP
	•	duplicate LN identity
	•	unresolved topology LNode

10.4 Add extension-preservation tests

Use ABB-style examples to assert that:
	•	Private survives
	•	vendor namespace info survives
	•	extension nodes are preserved when enabled

10.5 Add CLI golden tests

Golden/snapshot tests for:
	•	detect
	•	summary
	•	validate
	•	selected list-* commands

Especially human-readable output.

⸻

Suggested execution order

Here is the practical order I would implement this in.

Step 1

Delete handwritten XML transport structs and legacy decode path.

Step 2

Fix and harden DetectVersion.

Step 3

Clean up parse.go so it is orchestration only.

Step 4

Add DocumentMetadata and extension preservation to normalized model.

Step 5

Introduce shared index package.

Step 6

Migrate to unified Diagnostic and delete ValidationFinding.

Step 7

Split validator into passes using the shared index.

Step 8

Refactor Flatten, lookups, and exports to use index/resolver.

Step 9

Expand sclparse with list-* commands and improved validation output.

Step 10

Clean and expand tests/fixtures for the single parser path.

⸻

Definition of done

You are done with this refactor when all of these are true:
	•	no handwritten XML mirror structs remain
	•	no parser fallback remains
	•	unknown schema tuple fails explicitly
	•	all parse entrypoints use generated raw contracts only
	•	normalized SCL contains parse metadata
	•	one shared index layer is used by validation, flattening, and lookups
	•	ValidationFinding is gone
	•	diagnostics are unified
	•	validator is split into passes
	•	sclparse exercises the same runtime path as the library
	•	tests cover all supported schema versions and key failure modes

⸻

Blunt recommendation

Do not do this halfway.

Once you commit to “generated contracts only,” the fastest path is to delete the old path early, not leave it around while refactoring everything else. That forces all improvements onto the architecture you actually want.

If you want, I can next turn this into a file-by-file task list with likely target files, expected edits, and commit grouping.