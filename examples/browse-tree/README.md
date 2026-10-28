# browse-tree

Print the server's model tree with functional constraints.

## What it shows

`TreeWithOptions` (depth, LD filter, FCs) and `CacheLazy` model caching.

## Run

```bash
go run ./examples/browse-tree -addr 127.0.0.1:10102 -depth 4
```

Start the server first: `go run ./examples/ied-server -addr 127.0.0.1:10102` (port 102 needs root on Linux), then pass `-addr 127.0.0.1:10102`.

## Test

`go test ./examples/browse-tree` runs it in-process against the demo IED over loopback MMS, without privileges.
