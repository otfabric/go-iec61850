# sclparse exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Usage error: an unknown subcommand or flag, a wrong number of arguments |
| 2 | The file cannot be read or parsed, or the output cannot be written (`dump-json --output`) |
| 3 | `validate` found validation errors (with `--warnings-as-errors`, warnings count as errors) |
