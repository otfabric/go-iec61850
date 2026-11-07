# Support, feature and interop status

What go-iec61850 supports, how each feature is tested and which independent
stacks exchanged it. Status: v1.7.0, interop suite on iec61850-interop v1.4.0
images. Go 1.26 or later (CI: 1.26–1.27).

None of this is a conformance claim: no conformance test has been run, nothing
is certified, and no physical IED has been validated
([KNOWN_LIMITATIONS.md](../KNOWN_LIMITATIONS.md#interoperability)).

Platforms and versioning: [COMPATIBILITY.md](../COMPATIBILITY.md). Scope not
implemented: [KNOWN_LIMITATIONS.md](../KNOWN_LIMITATIONS.md). Per-service and
per-capability detail: [INTEROP.md](../INTEROP.md#iec-61850-edition-service-profile).

## Feature support

✅ supported · ◐ partly (see the notes) · — not implemented · n/a not
applicable.

**Own tests**: go-iec61850 on both sides. *TCP* is real ISO/TCP on loopback
(`make e2e`), *in-memory* is a transport pair or the `l2` hub.

**Independent**: software that is not go-iec61850 exchanged it with
go-iec61850 in the `interop/` suites, on pinned images. "All five" is
libiec61850, iec61850bean, dscsystems, iec61850-rust and cbsigma
(cB-Sigma/iec61850 and iec61850-sim), and results and
exceptions per stack are in the
[per-stack matrix](../COMPATIBILITY.md#per-stack-matrix). "None" means no
independent evidence: the feature must not be described as interoperable.

| Feature | Client | Server | Own tests | Independent |
|---|:---:|:---:|---|---|
| Associate, browse, read, write, dataset read | ✅ | ✅ | TCP, in-memory | all five, both directions |
| Create and delete datasets | ✅ | — | in-memory | none |
| CDC reads: DPS, BCR | ✅ | ✅ | in-memory | all five, both directions |
| URCB reporting | ✅ | ✅ | TCP, in-memory | all five, both directions; iec61850bean has no `Resv`, iec61850-rust refuses TrgOps/IntgPd writes |
| BRCB reporting, EntryID resume, overflow | ✅ | ✅ | in-memory, go-to-go in `interop/` | none |
| Direct and SBO control | ✅ | ✅ | in-memory | all five, both directions |
| SBOw (enhanced security) | ✅ | ✅ | in-memory | libiec61850, dscsystems, iec61850-rust, cbsigma; not iec61850bean |
| CommandTermination | ◐ not awaited | ◐ positive only | in-memory | dscsystems's controller waits for it |
| Quality and timestamps | ✅ | ✅ | in-memory, go-to-go in `interop/` | none as a dedicated scenario |
| Several clients at once | ✅ | ✅ | TCP, go-to-go in `interop/` | none |
| Setting groups | ✅ | ◐ no value banks | in-memory, TCP | none |
| Journals | ✅ | ◐ in-memory | in-memory | none here; go-mms's interop covers the MMS journal services |
| Files | ✅ | ◐ needs a `FileProvider` | in-memory | none here; go-mms's interop covers the MMS file services |
| GOOSE publisher and subscriber | ✅ subscriber | ✅ publisher | in-memory hub, chaos, raw socket | libiec61850, dscsystems, iec61850-rust, cbsigma, both directions |
| Server GoCB published on data change | n/a | ✅ | in-memory hub | libiec61850, dscsystems, iec61850-rust, cbsigma |
| GoCB read and write over MMS | ✅ | ✅ | in-memory | none |
| SV publisher and subscriber (9-2LE, generic) | ✅ subscriber | ✅ publisher | in-memory hub, chaos | libiec61850, dscsystems, iec61850-rust, both directions; libiec61850's subscriber skips the generic stream |
| Server MSVCB published at `smpRate` | n/a | ✅ | in-memory hub | libiec61850, dscsystems, iec61850-rust |
| MSVCB read over MMS | ✅ | ✅ | in-memory | libiec61850, dscsystems |
| MSVCB write over MMS | ✅ | ✅ | in-memory | none |
| SCL parse, validate, flatten, generate | n/a | model from SCL | fixtures, specification examples, ABB CID files | peers load the same ICD fixture; no independent validator compares results |
| TLS | ◐ through go-mms | ◐ through go-mms | none here | none; see [SECURITY.md](../SECURITY.md) |

The go-to-go tests in `interop/` have the build tag but no adapter, so they
are not independent evidence, although they sit in that directory
([TESTING.md](../TESTING.md#interop)).
