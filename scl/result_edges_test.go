// SPDX-License-Identifier: MIT

package scl

import "testing"

func TestDetectKind(t *testing.T) {
	ied := IED{Name: "IED1"}
	sub := Substation{Name: "S1"}
	comm := &Communication{SubNetworks: []SubNetwork{{Name: "N1"}}}

	tests := []struct {
		name string
		s    *SCL
		want DocumentKind
	}{
		{
			name: "SSD substations without IEDs",
			s:    &SCL{Substations: []Substation{sub}},
			want: KindSSD,
		},
		{
			name: "unknown empty",
			s:    &SCL{},
			want: KindUnknown,
		},
		{
			name: "ICD single IED no communication",
			s:    &SCL{IEDs: []IED{ied}},
			want: KindICD,
		},
		{
			name: "CID single IED with communication no substation",
			s:    &SCL{IEDs: []IED{ied}, Communication: comm},
			want: KindCID,
		},
		{
			name: "SCD single IED with communication and substation",
			s:    &SCL{IEDs: []IED{ied}, Communication: comm, Substations: []Substation{sub}},
			want: KindSCD,
		},
		{
			name: "SCD multiple IEDs",
			s:    &SCL{IEDs: []IED{ied, {Name: "IED2"}}},
			want: KindSCD,
		},
		{
			name: "ICD ignores empty communication section",
			s:    &SCL{IEDs: []IED{ied}, Communication: &Communication{}},
			want: KindICD,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectKind(tt.s); got != tt.want {
				t.Errorf("DetectKind() = %q, want %q", got, tt.want)
			}
		})
	}
}
