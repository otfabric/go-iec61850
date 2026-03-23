# go-iec61850 Releases

## v0.1.3

**Changed**: CI/release pipeline, SCL tooling, and documentation overhaul.

- Add GitHub Actions CI (`ci.yml`) and release (`release.yml`) workflows for package and binary releases.
- `sclgen` and `sclparse` binaries now embed full build metadata via ldflags (`version`, `tag`, `commit`, `buildDate`) and expose it through a dedicated `version` subcommand.
- Makefile build targets derive version from git tags instead of `version.txt` files.
- SCL model: add topology `LNode` references on `Substation`, `VoltageLevel`, and `Bay`; lossless `Private.InnerXML` capture.
- SCL validation: add topology LNode resolution, GOOSE/SMV `cbName` linkage checks; deprecate `scl.Validate()` in favour of `scl/validate.All()`.
- SCL CLI: add `list-goose`, `list-smv`, `list-connected-ap`, `list-types`, and `inspect` commands; deprecate `ParseWithOptions`/`ParseFileWithOptions`.
- Add content-based `DetectKind` for document classification.
- New `API.md` (merged from `ERRORS.md`); updated `KNOWN_LIMITATIONS.md`, `OBSERVABILITY.md`, `interop/README.md`.

---

## v0.1.2

**Changed**: Increase go-mms dependency to v0.1.4 and upstep ci and release flows (both package and binary)

---

## v0.1.1

**Fixed**: Pump go-mms v0.1.3 (Race condition in Client.Close / conclude handshake)

- When the reader loop received a ConcludeResponse, it signaled concludeCh and then returned (closing readerDone). Because Go's select picks randomly among ready cases, conclude() could non-deterministically hit the readerDone case and return a spurious "connection closed before conclude response" error. The fix drains concludeCh when readerDone fires before reporting failure.

---

## v0.1.0

**Changed**: N/A

- Initial release.

---
