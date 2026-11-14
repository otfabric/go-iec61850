// SPDX-License-Identifier: MIT

package sv

import (
	"errors"
	"strings"
	"testing"
)

// TestIDLength: svID and datSet are VisibleString129 (IEC 61850-9-2
// Table 14); 129 octets go through, 130 are refused on both sides.
func TestIDLength(t *testing.T) {
	long := strings.Repeat("A", MaxIDLength)
	f := fullFrame()
	f.PDU.ASDUs[0].SvID, f.PDU.ASDUs[0].DatSet = long, long
	raw, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if a := g.PDU.ASDUs[0]; a.SvID != long || a.DatSet != long {
		t.Fatal("129-octet svID/datSet not decoded")
	}

	for _, field := range []string{"svID", "datSet"} {
		f := fullFrame()
		if field == "svID" {
			f.PDU.ASDUs[0].SvID = long + "A"
		} else {
			f.PDU.ASDUs[0].DatSet = long + "A"
		}
		if _, err := Encode(f); !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("encode %s of %d octets: %v, want ErrInvalidValue", field, MaxIDLength+1, err)
		}
	}

	over := []byte(long + "A")
	for _, tag := range []byte{tagSvID, tagDatSet} {
		c := append(appendHdr(nil, tag, len(over)), over...)
		var a ASDU
		err := decodeASDU(c, 0, DefaultLimits, &a, map[string]string{})
		var de *DecodeError
		if !errors.Is(err, ErrInvalidPDU) || !errors.As(err, &de) {
			t.Fatalf("decode tag 0x%02x of %d octets: %v, want ErrInvalidPDU", tag, len(over), err)
		}
	}
}

// TestSubscriberLimitsPerField: a zero field of SubscriberConfig.Limits
// takes its DefaultLimits value; set fields are kept.
func TestSubscriberLimitsPerField(t *testing.T) {
	for _, tc := range []struct {
		in, want Limits
	}{
		{Limits{}, DefaultLimits},
		{Limits{MaxSample: 100}, Limits{MaxASDUs: DefaultLimits.MaxASDUs, MaxSample: 100}},
		{Limits{MaxASDUs: 2}, Limits{MaxASDUs: 2, MaxSample: DefaultLimits.MaxSample}},
	} {
		s, err := NewSubscriber(nil, SubscriberConfig{Limits: tc.in})
		if err != nil {
			t.Fatal(err)
		}
		if s.cfg.Limits != tc.want {
			t.Errorf("Limits %+v: got %+v, want %+v", tc.in, s.cfg.Limits, tc.want)
		}
		_ = s.Close()
	}
	if _, err := NewSubscriber(nil, SubscriberConfig{Limits: Limits{MaxASDUs: 1, MaxSample: -1}}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("negative MaxSample: %v", err)
	}
}
