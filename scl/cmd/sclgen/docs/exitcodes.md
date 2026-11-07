# sclgen exit codes

sclgen uses the organisation exit-code table. The codes are the `Exit…`
constants in [`main.go`](../main.go).

| Code | Constant | Meaning |
|---|---|---|
| 0 | `ExitOK` | Success: `generate` wrote the packages, `check` found them up to date, or help or the version was printed |
| 1 | `ExitFindings` | Findings: `check` completed and found out-of-date generated files |
| 2 | `ExitUsage` | Usage error: an unknown subcommand or flag, a missing required flag (`--spec-root`, `--out`), or no edition matches `--versions` |
| 3 | `ExitInvalidInput` | Invalid input: a schema bundle cannot be parsed or resolved |
| 10 | `ExitInternal` | Internal or output error: the generated packages cannot be written, or a temporary directory cannot be created |
| 130 | `ExitInterrupted` | Interrupted by SIGINT or SIGTERM |

Every non-zero exit prints the error to stderr, prefixed `sclgen: `.

## Changed in v1.8.0

Before go-iec61850 v1.8.0 sclgen exited with 1 for every failure, and a usage
error printed nothing.

| Old | New | Case |
|---|---|---|
| 1 | 1 | `check` found out-of-date generated files |
| 1 (no message) | 2 | Usage error |
| 1 | 2 | No edition matches `--versions` |
| 1 | 3 | A schema bundle cannot be parsed or resolved |
| 1 | 10 | The output cannot be written |
| (killed by the signal) | 130 | Interrupted |
