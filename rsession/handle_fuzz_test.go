// SPDX-License-Identifier: MIT

package rsession

import (
	"net/netip"
	"testing"
)

// FuzzTransportHandle passes one datagram to Transport.handle from a
// sequence of sender addresses, for each sender configuration, and
// checks that the frames of a verified SPDU are delivered at most once
// per sender address (no option), per listed sender address
// (AllowedSenders), or once in all (SingleSenderPerKey). An SPDU that
// does not verify delivers nothing.
func FuzzTransportHandle(f *testing.F) {
	k := testKey(1, MACHMACSHA256_128)
	ring, err := NewKeyRing(k)
	if err != nil {
		f.Fatal(err)
	}
	one := []Payload{{Type: PayloadGOOSE, AppID: 1, APDU: tinyGOOSE}}
	for _, p := range [][]Payload{one, append(one, one...)} {
		for _, v := range []uint16{1, 2} {
			b, err := Encode(&SPDU{SI: SIGOOSE, Version: v, Number: 9, Payloads: p}, &k)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(b, []byte{0, 0, 1, 2, 5})
		}
	}
	b, _ := Encode(&SPDU{SI: SIGOOSE, Number: 9, Payloads: one}, nil)
	f.Add(b, []byte{0, 1})

	allowed := netip.MustParsePrefix("192.0.2.0/30") // senders 0 to 3 of 8
	open := func(cfg Config) *Transport {
		cfg.SI, cfg.Keys, cfg.LocalAddr = SIGOOSE, ring, netip.MustParseAddrPort("127.0.0.1:0")
		tr, err := Open(cfg)
		if err != nil {
			f.Fatal(err)
		}
		f.Cleanup(func() { _ = tr.Close() })
		return tr
	}
	type variant struct {
		name  string
		tr    *Transport
		bound func(distinct, listed int) int // deliveries per payload
	}
	variants := []variant{
		{"no option", open(Config{}), func(d, _ int) int { return d }},
		{"AllowedSenders", open(Config{AllowedSenders: []netip.Prefix{allowed}}), func(_, l int) int { return l }},
		{"SingleSenderPerKey", open(Config{SingleSenderPerKey: true}), func(d, _ int) int { return min(d, 1) }},
	}

	f.Fuzz(func(t *testing.T, b []byte, senders []byte) {
		if len(senders) > 16 {
			senders = senders[:16]
		}
		payloads := 0
		if s, key, err := Verify(b, VerifyOptions{Keys: ring}); err == nil && key != nil {
			payloads = len(s.Payloads)
		}
		for _, v := range variants {
			v.tr.guard = newReplayGuard(0, 0) // each input starts without replay state
			before := v.tr.received.Load()
			distinct := map[netip.Addr]bool{}
			listed := map[netip.Addr]bool{}
			for _, s := range senders {
				a := netip.AddrFrom4([4]byte{192, 0, 2, s % 8})
				v.tr.handle(b, netip.AddrPortFrom(a, 102))
				distinct[a] = true
				if allowed.Contains(a) {
					listed[a] = true
				}
			}
			got := int(v.tr.received.Load() - before)
			if limit := payloads * v.bound(len(distinct), len(listed)); got > limit {
				t.Fatalf("%s: %d frames delivered from %d senders (%d listed), at most %d allowed", v.name, got, len(distinct), len(listed), limit)
			}
			for len(v.tr.queue) > 0 {
				<-v.tr.queue
			}
		}
	})
}
