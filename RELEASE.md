# go-iec61850 Releases

## v0.1.2

**Changed**: Dependency upgrades, CI/release v2 workflows, and git-based binary versioning.

- Bump go-mms to v0.1.4, go-cotp to v0.1.4, go-tpkt to v0.1.2.
- Upgrade CI workflow to `go-ci.yml@v2`.
- Split release workflow into `go-package-release.yml@v2` + `go-binary-release.yml@v2`, producing cross-platform `sclgen` and `sclparse` binaries with ldflags (version, tag, commit, buildDate).
- Replace `version.txt`-based versioning with `git describe` in Makefile; all build targets now inject the same 4 ldflags as CI.
- `sclgen --version` and `sclparse --version` now show version, tag, commit, and build date.

---

## v0.1.1

**Fixed**: Pump go-mms v0.1.3 (Race condition in Client.Close / conclude handshake)

- When the reader loop received a ConcludeResponse, it signaled concludeCh and then returned (closing readerDone). Because Go's select picks randomly among ready cases, conclude() could non-deterministically hit the readerDone case and return a spurious "connection closed before conclude response" error. The fix drains concludeCh when readerDone fires before reporting failure.

---

## v0.1.0

**Changed**: N/A

- Initial release.

---
