# sclparse exit codes

sclparse uses the organisation exit-code table. The codes are the `Exit…`
constants in [`main.go`](../main.go).

| Code | Constant | Meaning |
|---|---|---|
| 0 | `ExitOK` | Success |
| 1 | `ExitFindings` | Findings: `validate` completed and found validation errors (with `--strict` or `--warnings-as-errors`, warnings count as errors) |
| 2 | `ExitUsage` | Usage error: an unknown subcommand or flag, a wrong number of arguments |
| 3 | `ExitInvalidInput` | Invalid input: the SCL file cannot be read, is not SCL, or cannot be parsed |
| 10 | `ExitInternal` | Internal or output error: the output cannot be written (`dump-json --output`, JSON to stdout) |
| 130 | `ExitInterrupted` | Interrupted by SIGINT or SIGTERM |

Every non-zero exit prints the error to stderr.

## Changed in v1.8.0

The codes before go-iec61850 v1.8.0 did not follow the organisation table.

| Old | New | Case |
|---|---|---|
| 1 | 2 | Usage error |
| 2 | 3 | The file cannot be read or parsed |
| 2 | 10 | The output cannot be written |
| 3 | 1 | `validate` found validation errors |
| (killed by the signal) | 130 | Interrupted |
