// SPDX-License-Identifier: MIT

// Package sv implements IEC 61850 Sampled Values (SV) over Ethernet as
// specified by IEC 61850-9-2 (Ed2.1), IEC 61869-9 and the UCA "9-2LE"
// implementation guideline: frame codec, publisher with sample-rate
// pacing, subscriber with stream statistics and supervision, and the
// binding to SCL SampledValueControl blocks.
//
// # Wire format
//
// An SV frame is an Ethernet frame with EtherType 0x88BA, optionally
// 802.1Q tagged, carrying the header APPID / Length / Reserved1 /
// Reserved2 (the simulation bit is bit 15 of Reserved1) followed by the
// BER-encoded savPdu:
//
//	savPdu ::= [APPLICATION 0] IMPLICIT SEQUENCE {
//	    noASDU   [0] IMPLICIT INTEGER (1..65535),
//	    security [1] ANY OPTIONAL,
//	    asdu     [2] IMPLICIT SEQUENCE OF ASDU }
//	ASDU ::= SEQUENCE {
//	    svID       [0] IMPLICIT VisibleString,
//	    datSet     [1] IMPLICIT VisibleString OPTIONAL,
//	    smpCnt     [2] IMPLICIT OCTET STRING (SIZE(2)),
//	    confRev    [3] IMPLICIT OCTET STRING (SIZE(4)),
//	    refrTm     [4] IMPLICIT UtcTime OPTIONAL,
//	    smpSynch   [5] IMPLICIT OCTET STRING (SIZE(1)),
//	    smpRate    [6] IMPLICIT OCTET STRING (SIZE(2)) OPTIONAL,
//	    sample     [7] IMPLICIT OCTET STRING,
//	    smpMod     [8] IMPLICIT OCTET STRING (SIZE(2)) OPTIONAL,
//	    gmIdentity [9] IMPLICIT OCTET STRING (SIZE(8)) OPTIONAL }
//
// The sample is the dataset in the fixed-size binary encoding of
// IEC 61850-9-2 (see [Kind] and [Layout]); [LESample] covers the 9-2LE
// measurement dataset (4 currents and 4 voltages with quality).
//
// # Transport
//
// Frames are sent and received through the shared raw Ethernet layer of
// package l2 ([Open], [Hub], l2.Demux to share one socket with GOOSE).
//
// # Performance
//
// The publisher paces samples against an absolute schedule (no drift),
// batches noASDU samples per frame and encodes into a reused buffer.
// The subscriber decodes into reused storage ([DecodeInto]) and hands
// each ASDU to a callback without per-frame allocation; see the
// benchmarks in the package tests.
package sv
