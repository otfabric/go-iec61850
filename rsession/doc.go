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
// the sender instead.
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
