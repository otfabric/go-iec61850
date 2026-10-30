# Contributing to go-iec61850

## Development setup

- Go 1.24 or later (see [go.mod](go.mod)).
- [staticcheck](https://staticcheck.dev/), [golangci-lint](https://golangci-lint.run/) and [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) for `make check`.
- Docker for the interop suites only. The GOOSE and SV interop suite needs Linux and root, see [interop/README.md](interop/README.md).

```sh
git clone https://github.com/otfabric/go-iec61850.git
cd go-iec61850
make check
```

Run `make help` for the list of targets.

## Tests

- `make test`, `make test-race`: unit tests.
- `make coverage-gate`: fails below 90% per package. CI runs this gate as root so that the GOOSE raw socket tests run; do not lower it.
- `make fuzz`: fuzz targets for `FUZZTIME` (default 15s).
- `make interop`, `make interop-goose`: tests against independent stacks, see [INTEROP.md](INTEROP.md).

Add a fuzz target for any new decoder of untrusted input.

## Generated code

`scl/internal/raw` is generated from the schemas in `scl/specs` by `sclgen`; CI fails if it drifts. Never edit it by hand. Change the generator in `scl/internal/genir`, then run `make scl-generate scl-check-generate`.

## Submitting changes

1. Work on a branch and open a pull request against `main`.
2. Add tests, including error paths.
3. Update the documents the change touches ([API.md](API.md), [ERRORS.md](ERRORS.md), [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)) and add a section to [RELEASE.md](RELEASE.md) for user-visible changes.

Report security issues as described in [SECURITY.md](SECURITY.md), not in public issues.
