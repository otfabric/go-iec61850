# setting-groups

Edit and activate setting groups.

## What it shows

`GetSettingGroupInfo`, `GetActiveSGValue` (FC SG), `SelectEditSG`, `GetEditSGValue`/`SetEditSGValue` (FC SE), `ConfirmEditSG`, `SelectActiveSG`. The server side (where the setting values live) is `enableSettingGroups` in [`examples/internal/demoied`](../internal/demoied/demoied.go).

## Run

```bash
go run ./examples/setting-groups -addr 127.0.0.1:10102 -value 175
```

Start the server first: `go run ./examples/ied-server -addr 127.0.0.1:10102` (port 102 needs root on Linux), then pass `-addr 127.0.0.1:10102`.

## Test

`go test ./examples/setting-groups` runs it in-process against the demo IED over loopback MMS, without privileges.
