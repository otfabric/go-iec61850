# files

List the server's files and download one.

## What it shows

`ListFiles` and `DownloadFile` (MMS file services); a download never overwrites a local file.

## Run

```bash
go run ./examples/files -addr 127.0.0.1:10102 -get demo.icd -out /tmp
```

Start the server first: `go run ./examples/ied-server -addr 127.0.0.1:10102` (port 102 needs root on Linux), then pass `-addr 127.0.0.1:10102`. The demo server serves its SCL as `demo.icd`.

## Test

`go test ./examples/files` runs it in-process against the demo IED over loopback MMS, without privileges.
