// SPDX-License-Identifier: MIT

// Package rsession carries GOOSE and Sampled Values over UDP/IP: R-GOOSE
// and R-SV, with the session protocol of IEC TR 61850-90-5 and its
// per-SPDU authentication (HMAC-SHA256).
//
// # Using it
//
// A [Transport] implements the layer-2 transport interface of packages
// goose and sv, so their publishers and subscribers run over UDP
// unchanged:
//
//	keys, _ := rsession.NewKeyRing(rsession.Key{ID: 1, Material: secret, MAC: rsession.MACHMACSHA256_128})
//	t, _ := rsession.Open(rsession.Config{
//		SI:         rsession.SIGOOSE,
//		RemoteAddr: netip.MustParseAddrPort("239.192.0.1:102"),
//		Keys:       keys,
//	})
//	pub, _ := goose.NewPublisher(t, goose.PublisherConfig{...})
//
// and on the receiving side
//
//	t, _ := rsession.Open(rsession.Config{
//		SI:     rsession.SIGOOSE,
//		Groups: []netip.Addr{netip.MustParseAddr("239.192.0.1")},
//		Keys:   keys,
//	})
//	sub, _ := goose.NewSubscriber(t, goose.SubscriberConfig{})
//
// The publisher's Ethernet addresses and VLAN tag are not sent; frames a
// receiving Transport returns carry [Config.FrameDst] and [SourceMAC] of
// the sender instead. [Transport.RecvFrom] also returns the sender's UDP
// address and port, which the goose and sv subscribers report as
// Frame.From.
//
// # Security
//
// Secured is the default: [Open] refuses a configuration without a
// [KeyProvider] unless [Config.Insecure] is set, and a receiver refuses
// unsecured SPDUs unless it is set. A receiver also refuses SPDUs under
// keys it does not hold, SPDUs whose signature does not verify, and
// replays: per sender and key, an SPDU number accepted before or older
// than the [ReplayWindow]. Refusals are counted ([Transport.Stats]) and
// reported to [Config.OnReject].
//
// SPDU numbers do not repeat under one key. A receiver compares them as
// plain 32-bit unsigned numbers, without wrap-around: a number below the
// window is refused (until [Config.ReplayResetAfter] of silence, when
// set). A sender that has sent SPDU number 0xffffffff under a key refuses
// to send more under it ([ErrLimitExceeded]) until a key with another ID
// is current, and numbering goes on at 0 under the new key, which starts
// new windows at the receivers. Key IDs are not to be reused for new key
// material. Rotate keys well before 2^32 SPDUs: an R-SV stream of 4800
// SPDUs per second uses them up in about 10 days.
//
// The replay window is kept per sender address, which the signature does
// not cover, so an SPDU that verified is accepted once more from each
// source address the receiver has no window for (also after a sender's
// window was forgotten at [Config.MaxSenders], or after a restart).
// [Config.AllowedSenders] refuses datagrams from addresses outside the
// listed prefixes before they are verified; [Config.SingleSenderPerKey]
// keeps one window per key ID whatever the address, for keys that one
// publisher uses. Set one of them on a receiver, and filter the goose or
// sv subscription on the publisher with Src: [SourceMAC] of its address.
//
// Keys come from a [KeyProvider]: the current key secures what is sent,
// every key it holds is accepted on receipt. [KeyRing] is a static
// provider; key distribution (IEC 62351-9 GDOI) is outside this package.
//
// Only HMAC-SHA256 (80, 128 and 256 bits) is implemented: AES-GMAC and
// encryption are refused as [ErrUnsupported]. The layer-2 IEC 62351-6
// extension of GOOSE and SV frames is not implemented. See
// docs/adr/0001-routable-goose-sv.md for the wire layout, the references
// it follows and the scope.
//
// # Codec
//
// [Encode], [Decode] and [Verify] work on single SPDUs without a socket,
// for captures and tests.
package rsession
