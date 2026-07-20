# Security Policy

## Supported releases

Security issues are addressed in the latest v1 patch release.

## Reporting a vulnerability

Please report security vulnerabilities **privately** rather than opening a
public GitHub issue.

Send a report to: **security@otfabric.io**

Include:

- A description of the vulnerability and its potential impact.
- Steps to reproduce or a minimal proof-of-concept.
- The affected version(s).

**Expected acknowledgement:** within 5 business days.

We will coordinate a fix and disclosure timeline with you. We aim to release a
patch within 90 days of confirmation.

## Scope

go-iec61850 is a protocol library that parses untrusted network data. The
following are in scope for this policy:

- Panics triggered by malformed MMS PDUs or IEC 61850 report payloads.
- Memory exhaustion caused by peer-controlled allocations (e.g. unbounded
  datasets, oversized report queues, deeply nested SCL models).
- Incorrect control authorisation — a client operating a control object it
  should not have access to.
- Incorrect URCB/BRCB reservation enforcement allowing unauthorised report
  enabling.
- Data races exposed by the race detector.
- Any defect that allows a remote peer to crash or deadlock the server.

The following are **out of scope**:

- Vulnerabilities in dependencies not maintained by this project.
- Issues requiring physical access to the machine running the library.
- Theoretical attacks with no practical exploitation path.
- IEC 62351 role-based access control (not implemented).

## Certification statement

go-iec61850 has not received a formal security audit or certification. The
implementation includes decoder strictness hardening, race-detector coverage,
fuzz testing, and panic containment in server callbacks, but no independent
security evaluation has been performed.

Optional TLS transport is provided through Go's standard `crypto/tls`
implementation. This is **not** equivalent to IEC 62351 conformance or
certification. Do not claim IEC 62351 compliance based solely on TLS transport
support.

## Disclosure policy

We follow coordinated disclosure. We request that you allow a reasonable fix
window before public disclosure. We will credit reporters in release notes
unless they prefer to remain anonymous.
