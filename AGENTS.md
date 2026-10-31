# AGENTS.md — go-iec61850

## What this is

Pure Go IEC 61850 stack: MMS client and server on [go-mms](https://github.com/otfabric/go-mms), GOOSE and Sampled Values publishers and subscribers, SCL parsing and validation, and the `sclparse` and `sclgen` tools. Profile: **protocol-library**. Nothing is certified; independently verified behaviour and its peers are listed in INTEROP.md and COMPATIBILITY.md.

## Organisation standards

This repository follows **otfabric/engineering policy 1.1**, profile **protocol-library** (`.otfabric/policy.yaml`, mode `enforce`). Organisation skills: `otfabric` Claude Code plugin. Where this file and the organisation standards disagree, this file wins here — say so in your report.

## Where the truth lives

| Question | Source |
|---|---|
| Wire behaviour | IEC 61850-8-1, -9-2, IEC 61869-9, UCA 9-2LE; GOOSE.md, SV.md |
| SCL bindings | `scl/specs` (XSD input), `scl/internal/raw` (generated, never edit by hand) |
| What is verified against independent stacks | INTEROP.md, COMPATIBILITY.md |
| Scope not implemented | KNOWN_LIMITATIONS.md |
| Errors | ERRORS.md |
| Release history | RELEASE.md (`## vX.Y.Z` … `---`) |

## Hard rules

1. Never lower the coverage minimum (`COVER_PKG_MIN`, 90 per package, `scripts/covergate.go`; 90 in total, go-ci), add lint suppressions, or skip/delete tests to make CI pass.
2. Never invent protocol semantics; cite the specification clause.
3. Interop claims only from passing runs against the pinned iec61850-interop images.
4. Follow semantic versioning for every non-internal package: this library stays on v0 or v1.
5. Never edit `scl/internal/raw` by hand: change the generator (`scl/internal/genir`) and run `make scl-generate`.
6. No new runtime dependency without an ADR.

## Commands

| When you change… | Run |
|---|---|
| anything | `make check` |
| coverage-relevant code | `make coverage-pkg-check` |
| a decoder | `make fuzz` |
| the generator or `scl/specs` | `make scl-generate scl-check-generate` |
| interop-relevant behaviour | `make interop`, `make interop-goose` (Docker; see interop/README.md) |

## Definition of Done

`make check` green; tests for new behaviour including error paths; fuzz target for any new decoder; docs updated in the same change; RELEASE.md section for user-visible changes.

## Git and releases

Branches and PRs. Do not push to `main`, tag or run the Release workflow unless the maintainer asks in the session.
