# go-iec61850 Releases

## v0.1.1

**Fixed**: Pump go-mms v0.1.3 (Race condition in Client.Close / conclude handshake)

- When the reader loop received a ConcludeResponse, it signaled concludeCh and then returned (closing readerDone). Because Go's select picks randomly among ready cases, conclude() could non-deterministically hit the readerDone case and return a spurious "connection closed before conclude response" error. The fix drains concludeCh when readerDone fires before reporting failure.

---

## v0.1.0

**Changed**: N/A

- nitial release.

---
