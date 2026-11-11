# ADR 0001: Routable GOOSE and SV (R-GOOSE, R-SV) and IEC 62351-6 authentication

- Status: Accepted (2026-11-11)
- Scope: new package `rsession`; packages `goose` and `sv` unchanged

## Context

`KNOWN_LIMITATIONS.md` listed "No IEC 62351-6 security; no R-GOOSE / R-SV".
GOOSE (IEC 61850-8-1 Annex C) and Sampled Values (IEC 61850-9-2 5.3) are
layer-2 multicast: they do not leave the substation LAN and anyone on the
segment can inject or replay them. Two documents address this:

- **IEC TR 61850-90-5** defines a session protocol that carries the GOOSE
  and SV APDUs over UDP/IP (unicast or multicast), with a MAC per SPDU under
  keys identified in the header. IEC 61850-8-1 and 9-2 Amendment 1 adopt it
  as R-GOOSE and R-SV.
- **IEC 62351-6** defines the security of GOOSE and SV, including the
  layer-2 extension that IEC 61850-8-1:2011 Figure C.5 and 9-2:2011 Figure 3
  reserve for it: the "reserved security" bits of Reserved 1, all of
  Reserved 2, and an extension field after the APDU (8-1 Annex C, octets
  m+27 onward).

### Documents available

The specification set used for this repository's spec reviews (not
committed) holds IEC 61850-8-1:2011 and 9-2:2011, which define only the
reserved fields above and refer to IEC 62351-6 for their content. It does
**not** hold:

- IEC TR 61850-90-5 (2012), or IEC 61850-8-1/9-2 Amendment 1 (2020);
- IEC 62351-6 (2007 Ed1 or 2020 Ed2) or IEC 62351-9 (key management).

AGENTS.md rule 2 forbids inventing protocol semantics. The session layer is
therefore built from three independent implementations that agree with each
other and with the 90-5 structure as published in the IEC 61850 user
community, and where they disagree this record says which is followed and
why:

| Reference | What it shows |
|---|---|
| libiec61850, `src/r_session/r_session.c`, at the revision the interop harness pins (6e265c5) | encoder and decoder, protocol versions 1 and 2, HMAC-SHA256, algorithm code points |
| Wireshark, `epan/dissectors/packet-goose.c` (`dissect_rgoose`) | field order and sizes, payload type tags, padding tag |
| dscsystems go-iec61850 v0.6.6, package `rsession` (an interop peer, GPL-3.0) | encoder and decoder, its reading of the header lengths |

No source text or code of these was copied; the vectors in
`rsession/spec_test.go` are built by hand from the field tables below. None
of them gives the layer-2 IEC 62351-6 extension (see "Layer 2" below).

## Decision

### Package and integration

A new package `rsession` holds the SPDU codec, the MAC, the key provider,
replay protection and a UDP transport. The transport implements
`l2.Transport` (the `goose.Transport` / `sv.Transport` alias): `Send` takes
the layer-2 frame a `goose.Publisher` or `sv.Publisher` builds and sends its
APDU as an SPDU; `Recv` returns, for every SPDU that verifies, a layer-2
frame with the same APPID, simulation bit and APDU. The existing publishers,
subscribers, retransmission, supervision and restart detection therefore run
unchanged over UDP. Nothing in `goose` or `sv` changes; the API is additive.

### Session layer (IEC TR 61850-90-5 clause 11 structure)

All integers are big-endian.

| Octets | Field | Value |
|---|---|---|
| 1 | transport length indicator | `0x01` (ISO/IEC 8602 connectionless transport, UD TPDU) |
| 1 | transport TPDU code | `0x40` |
| 1 | session identifier (SI) | `0xA1` GOOSE, `0xA2` SV (`0xA0` tunnelled and `0xA3` management are decoded but not served) |
| 1 | session header length | octets after this field to the end of the security information |
| 1 | common session header PI | `0x80` |
| 1 | common session header length | `0x0A` (SPDU length, SPDU number, version) |
| 4 | SPDU length | octets after this field to the end of the SPDU |
| 4 | SPDU number | per sender, incremented for every SPDU |
| 2 | version | 1 or 2 |
| 4 | TimeOfCurrentKey | from the key |
| 2 | TimeToNextKey | from the key, signed |
| v1: 1+1 | encryption algorithm, MAC algorithm | v1 only |
| 4 | key ID | 0 when unsecured |
| v2: 1+n | IV length, IV | v2 only |
| 4 | payload length | octets of the payload elements |
| per element | type (1), simulation (1), APPID (2), APDU length (2), APDU | type `0x81` GOOSE, `0x82` SV |
| optional | padding: `0xAF`, length (1), octets | accepted, never sent |
| when secured | signature: `0x85`, length (1), MAC | |

Version 2 is sent by default; version 1 is sent on request and both are
received. Where the references differ:

- **Header lengths.** libiec61850 writes 24 and 18 whatever the header holds;
  dscsystems writes the lengths of what follows (common header 10). This
  package writes the latter, and on receive checks only that the lengths do
  not run past the datagram, as libiec61850 and Wireshark do.
- **SPDU length.** libiec61850 writes 20 plus the payload length; this
  package writes the octets that follow the field and does not check the
  received value (no reference checks it).
- **APDU length.** The length of the APDU (Wireshark, dscsystems).
  libiec61850 writes two more; the decoder accepts that on the last element
  only, where it is unambiguous.
- **Payload types.** `0x81`/`0x82` are common to all three; the tunnelled
  and management types differ between them and are out of scope.

**Port.** UDP 102 (`rsession.DefaultPort`), the port libiec61850 uses;
configurable.

### Authentication

- **Algorithms.** The MAC is HMAC-SHA256 truncated to 80, 128 or 256 bits,
  code points 1, 2 and 3 (the 90-5 table as libiec61850 numbers it). It
  covers the SPDU from its first octet up to, not including, the signature
  tag; the verifier compares in constant time. The signature length octet
  must equal the algorithm's MAC length (libiec61850's HMAC-SHA256-256 SPDUs
  carry 16 before a 32-octet MAC and are refused).
- **Not implemented:** AES-GMAC-64/128 (code points 4 and 5) and encryption
  (AES-128/256-GCM). No reference available here defines the GMAC nonce and
  input, and the references disagree on the layout of an SPDU that is both
  signed and encrypted. An SPDU that uses them is refused as unsupported.
- **Keys.** A key is an ID, the key material, the MAC algorithm and the two
  key times carried in the header. In version 2 the algorithm is a property
  of the key, not of the wire; in version 1 the wire octets must match the
  key's. Keys come from a `KeyProvider` interface: `CurrentKey` secures what
  is sent, `LookupKey(id)` verifies what is received. `KeyRing` is a static,
  concurrency-safe provider (add, remove, activate) that supports rotation
  by holding the old and the new key at once. GDOI/KDC distribution
  (IEC 62351-9) is out of scope: an application implements `KeyProvider`
  over its key distribution.
- **Secure by default** (organisation ADR-0010). A transport without a
  key provider is refused unless `Insecure` is set; a receiver refuses
  unsecured SPDUs unless `AllowUnsecured` is set.
- **Replay.** The receiver keeps, per sender address, SI and key ID, the
  highest SPDU number accepted and a 64-entry window below it (as IPsec
  does, RFC 4303 3.4.3), with serial number arithmetic (RFC 1982) for the
  wrap. A duplicate or an SPDU older than the window is refused. A new key
  starts a new window. A restarted publisher starts below its old numbers
  and is refused until the key changes, unless the receiver sets
  `ReplayResetAfter` (accept a lower number after that much silence) or the
  publisher sets `InitialSPDUNumber` from a persisted value. The window is a
  receiver policy of this package, not a 90-5 requirement.
- **Rejections** are counted (`Transport.Stats`) and reported to
  `OnReject` with the sender address; they never reach the subscriber.

### Layer 2 (IEC 62351-6 extension in Reserved 1/2)

Deferred. IEC 61850-8-1:2011 and 9-2:2011 give only the location: the
twelve "reserved security" bits of Reserved 1, Reserved 2 (together a
28-bit word in 9-2 5.3.3.4.5) and the extension field after the APDU, whose
octets 8-1 Annex C leaves outside the Length field. Their content, the
extension's ASN.1, the MAC input and the key binding are in IEC 62351-6,
which is not available here, and no reference implementation exists to
corroborate one (dscsystems states it does not implement it; libiec61850
and Wireshark do not parse it). Implementing it from memory would invent
semantics. The existing codecs already carry Reserved 1/2 verbatim and
keep the simulation bit separate, so the extension can be added later
without breaking the API: a `goose`/`sv` option that appends the
extension after the APDU and sets the reserved bits, and a subscriber
option that verifies them, both with the `rsession.KeyProvider`.

### Out of scope

- AES-GMAC and AES-GCM (above); IEC 62351-9 GDOI/KDC key distribution.
- Tunnelled (SI `0xA0`) and management (SI `0xA3`) SPDUs; several APDUs in
  one SPDU on send (they are decoded).
- The layer-2 IEC 62351-6 extension (above).
- IEC 61850-8-1 Amendment 1 changes to the GOOSE APDU itself and SCL
  binding of R-GOOSE addresses (`IP`, `IP-SUBNET` in `GSE/Address`): the
  caller passes the address.
- Interop: libiec61850 at the pinned revision writes an APDU length its own
  decoder refuses, and the pinned interop images are not built with
  R-GOOSE. Adding an R-GOOSE/R-SV peer to iec61850-interop is a separate
  change in that repository.

## Consequences

- R-GOOSE and R-SV work with the existing publishers and subscribers over
  UDP unicast and multicast, authenticated with HMAC-SHA256, behind
  `rsession.KeyProvider`.
- Interoperability with other stacks is unverified until a peer exists in
  iec61850-interop; FEATURE-STATUS says "none".
- When IEC TR 61850-90-5 or IEC 62351-6 becomes available, the layout
  choices above are checked against it; a difference is a wire change and
  gets its own record.
