// SPDX-License-Identifier: MIT

package iec61850

import (
	"io"
	"log/slog"
	"testing"

	"github.com/otfabric/go-mms"
)

func TestDialMMSLoggerPropagation(t *testing.T) {
	iec := slog.New(slog.NewTextHandler(io.Discard, nil))
	other := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))

	got := dialMMSOptions(DialOptions{Logger: iec}, iec)
	if got.Logger != iec {
		t.Fatal("expected IEC logger when MMS.Logger is nil")
	}

	got = dialMMSOptions(DialOptions{
		Logger: iec,
		MMS:    mms.DialOptions{Logger: other},
	}, iec)
	if got.Logger != other {
		t.Fatal("explicit MMS.Logger must be preserved")
	}
}
