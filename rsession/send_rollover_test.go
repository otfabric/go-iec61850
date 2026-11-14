// SPDX-License-Identifier: MIT

package rsession_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-iec61850/rsession"
)

// TestSendNoNumberRepeatUnderKey: after SPDU number 0xffffffff a sender
// refuses to send under the same key; with another key current it goes
// on at 0, which the receiver accepts as a new stream.
func TestSendNoNumberRepeatUnderKey(t *testing.T) {
	keys := ring(t)
	if err := keys.Add(rsession.Key{ID: 2, Material: bytes.Repeat([]byte{0x22}, 32), MAC: rsession.MACHMACSHA256_128}); err != nil {
		t.Fatal(err)
	}
	recv := open(t, rsession.Config{SI: rsession.SIGOOSE, LocalAddr: loopback, Keys: keys})
	send := open(t, rsession.Config{SI: rsession.SIGOOSE, RemoteAddr: recv.LocalAddr(), Keys: keys, InitialSPDUNumber: 0xffffffff})
	frame := gooseFrame(t)
	if err := send.Send(frame); err != nil {
		t.Fatal(err)
	}
	if err := send.Send(frame); !errors.Is(err, rsession.ErrLimitExceeded) {
		t.Fatalf("send after 0xffffffff under the same key: %v, want ErrLimitExceeded", err)
	}
	if got := send.Stats().SendErrors; got != 1 {
		t.Fatalf("SendErrors = %d, want 1", got)
	}
	if err := keys.SetCurrent(2); err != nil {
		t.Fatal(err)
	}
	if n := send.NextSPDUNumber(); n != 0 {
		t.Fatalf("NextSPDUNumber = %d, want 0", n)
	}
	if err := send.Send(frame); err != nil {
		t.Fatalf("send under the next key: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := range 2 {
		if _, err := recv.Recv(ctx); err != nil {
			t.Fatalf("recv %d: %v", i, err)
		}
	}
	if st := recv.Stats(); st.Replayed != 0 || st.Received != 2 {
		t.Fatalf("receiver stats %+v", st)
	}
}
