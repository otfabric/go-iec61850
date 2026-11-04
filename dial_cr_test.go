// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/otfabric/go-cotp"
)

// dialCapturingCR dials a listener that records the COTP CR and closes the
// connection, and returns that CR.
func dialCapturingCR(t *testing.T, opts DialOptions) *cotp.CR {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	got := make(chan *cotp.CR, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			got <- nil
			return
		}
		defer func() { _ = c.Close() }()
		hdr := make([]byte, 4)
		if _, err := io.ReadFull(c, hdr); err != nil {
			got <- nil
			return
		}
		body := make([]byte, int(binary.BigEndian.Uint16(hdr[2:]))-4)
		if _, err := io.ReadFull(c, body); err != nil {
			got <- nil
			return
		}
		d, err := cotp.Decode(body)
		if err != nil {
			got <- nil
			return
		}
		got <- d.CR
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c, err := Dial(ctx, ln.Addr().String(), opts); err == nil {
		_ = c.Close(ctx)
		t.Fatal("Dial succeeded against a listener that closes after the CR")
	}
	cr := <-got
	if cr == nil {
		t.Fatal("no CR received")
	}
	return cr
}

// TestDialCRAddressing: the CR carries the T-selectors and TPDU size IEDs
// expect (go-iec61850#28), and DialOptions changes them.
func TestDialCRAddressing(t *testing.T) {
	cr := dialCapturingCR(t, DialOptions{})
	if !bytes.Equal(cr.CallingSelector, []byte{0, 1}) || !bytes.Equal(cr.CalledSelector, []byte{0, 1}) ||
		cr.TPDUSize == nil || *cr.TPDUSize != 0x0A {
		t.Fatalf("default CR calling=%x called=%x size=%v, want 0001/0001/0x0A", cr.CallingSelector, cr.CalledSelector, cr.TPDUSize)
	}

	opts := DialOptions{MaxTPDULength: 2048}
	opts.MMS.Transport.RemoteTSelector = []byte{0, 0, 0, 1}
	cr = dialCapturingCR(t, opts)
	if !bytes.Equal(cr.CalledSelector, []byte{0, 0, 0, 1}) || cr.TPDUSize == nil || *cr.TPDUSize != 0x0B {
		t.Fatalf("configured CR called=%x size=%v, want 00000001/0x0B", cr.CalledSelector, cr.TPDUSize)
	}
}
