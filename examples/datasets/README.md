# datasets

List, read, create and delete data sets.

## What it shows

`ListDataSets`, `GetDataSet`, `ReadDataSet` with per-member errors, `CreateDataSet`, `DeleteDataSet`, and `ErrNotFound` (wrapping the `*mms.ServiceError`) for a missing data set.

## Run

```bash
go run ./examples/datasets -addr 127.0.0.1:10102
```

Start the server first: `go run ./examples/ied-server -addr 127.0.0.1:10102` (port 102 needs root on Linux), then pass `-addr 127.0.0.1:10102`.

## Test

`go test ./examples/datasets` runs it in-process against the demo IED over loopback MMS, without privileges.
