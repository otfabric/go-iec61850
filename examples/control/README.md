# control

Operate a controllable object as its control model requires.

## What it shows

`ReadCtlModel`, direct operate, `Select` (SBO) and `SelectWithValue` (SBOw) before `Operate`, `*ControlError` with AddCause and `ReadLastApplError`. Without `-do` it lists the controllable objects.

## Run

```bash
go run ./examples/control -addr 127.0.0.1:10102 -do LD1/GGIO1.SPCSO1 -value true
```

Start the server first: `go run ./examples/ied-server -addr 127.0.0.1:10102` (port 102 needs root on Linux), then pass `-addr 127.0.0.1:10102`.

## Test

`go test ./examples/control` runs it in-process against the demo IED over loopback MMS, without privileges.
