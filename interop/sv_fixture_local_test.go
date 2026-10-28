//go:build interop

package interop

import "testing"

// TestSVFixtureBinding checks the SCL binding of the SV fixture without
// adapters.
func TestSVFixtureBinding(t *testing.T) {
	fix := loadSVFixture(t)
	for _, s := range []string{"MSVCB01", "MSVCB02"} {
		cb, fx := fix.stream(t, s)
		b := encodeFixture(t, cb, fx.Values)
		if got, want := decodeCanon(t, cb, b), canonJSON(t, fx.Values); got != want {
			t.Errorf("%s values:\n got %s\nwant %s", s, got, want)
		}
		b = encodeFixture(t, cb, fx.Update)
		if got, want := decodeCanon(t, cb, b), canonJSON(t, fx.Update); got != want {
			t.Errorf("%s update:\n got %s\nwant %s", s, got, want)
		}
	}
}
