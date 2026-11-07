# sclgen exit codes

| Code | Meaning |
|---|---|
| 0 | Success: `generate` wrote the packages, `check` found them up to date, or help or the version was printed |
| 1 | Failure of any kind: `check` found out-of-date generated files, a required flag (`--spec-root`, `--out`) or an argument is missing or unknown, no edition matches `--versions`, or a schema bundle cannot be parsed or the output written |

A usage error exits with 1 without a message.
