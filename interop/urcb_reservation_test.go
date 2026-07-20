//go:build interop

// SPDX-License-Identifier: MIT

// Phase E5 — URCB reservation and ownership tests.
//
// These tests verify the reservation lifecycle of the go-iec61850 server's
// unbuffered report control block:
//
//   - Same-connection double reserve is idempotent.
//   - A second connection cannot steal a reservation held by another client.
//   - Enabling the RCB without being the reservation owner is rejected.
//   - Closing the owning connection releases the reservation.
//   - Multiple URCBs can be reserved independently by different clients.
//
// NOTE: As of Phase E5 (⬜ not yet implemented), the go server does not yet
// enforce reservation ownership.  Tests that depend on ownership enforcement
// (DoubleReserve_SecondConn, EnableWithoutReserve) are expected to fail
// against the current implementation.  They serve as the acceptance criteria
// for E5 and should pass once connection-scoped reservation tracking is added.
package interop

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

// TestGoServer_URCB_DoubleReserve_SameConn verifies that writing Resv=true
// twice on the same connection is idempotent: the second write must succeed
// (or return a benign no-op) and the RCB must remain reserved.
func TestGoServer_URCB_DoubleReserve_SameConn(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c.Close(ctx)

	// First reserve.
	if err := c.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Fatalf("first ReserveURCB: %v", err)
	}

	// Second reserve on the same connection must also succeed.
	if err := c.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Errorf("second ReserveURCB on same connection: unexpected error: %v", err)
	}

	// RCB must still be reserved.
	rcb, err := c.GetReportControlBlock(ctx, urcbLD, goServerURCBID)
	if err != nil {
		t.Fatalf("GetReportControlBlock: %v", err)
	}
	if !rcb.Resv {
		t.Error("Resv should be true after double reserve on same connection")
	}
}

// TestGoServer_URCB_DoubleReserve_SecondConn verifies that a second
// connection cannot reserve a URCB that is already held by another client.
//
// Per IEC 61850-7-2, a second client writing Resv=true must receive an
// object-access-denied error when the URCB is already reserved.
//
// NOTE: This test documents the required behaviour of Phase E5.  It will
// fail against the current go-iec61850 server because connection-scoped
// reservation ownership is not yet enforced.
func TestGoServer_URCB_DoubleReserve_SecondConn(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Client A reserves the URCB.
	cA := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer cA.Close(ctx)

	if err := cA.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Fatalf("client A ReserveURCB: %v", err)
	}

	// Client B must be rejected when it tries to reserve the same URCB.
	cB := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer cB.Close(ctx)

	err := cB.ReserveURCB(ctx, urcbLD, goServerURCBID)
	if err == nil {
		t.Error("client B ReserveURCB: expected error (URCB already reserved by client A), got nil")
	} else {
		t.Logf("client B ReserveURCB correctly rejected: %v", err)
	}
}

// TestGoServer_URCB_EnableWithoutReserve verifies that a non-owning client
// cannot enable a URCB that has been reserved by another client.
//
// Per IEC 61850-7-2, writing RptEna=true to a URCB reserved by a different
// association must be rejected with an access error.
//
// NOTE: This test documents the required behaviour of Phase E5.  It will
// fail against the current go-iec61850 server because RptEna ownership
// checks are not yet implemented.
func TestGoServer_URCB_EnableWithoutReserve(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Client A reserves and enables the URCB.
	cA := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer cA.Close(ctx)

	subA, err := cA.SubscribeReport(ctx, urcbRptID, iec61850.SubscribeReportOptions{
		LD:          urcbLD,
		RCBItemID:   goServerURCBID,
		ReserveURCB: true,
		AutoEnable:  true,
	})
	if err != nil {
		t.Fatalf("client A SubscribeReport: %v", err)
	}
	defer subA.Close() //nolint:errcheck

	// Client B must not be able to enable the URCB owned by client A.
	cB := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer cB.Close(ctx)

	err = cB.SetReportControlBlock(ctx, urcbLD, goServerURCBID, iec61850.RCBUpdate{
		Fields: iec61850.RCBFieldRptEna,
		RptEna: true,
	})
	if err == nil {
		t.Error("client B SetReportControlBlock(RptEna=true): expected access error, got nil")
	} else {
		t.Logf("client B RptEna correctly rejected: %v", err)
	}
}

// TestGoServer_URCB_DisconnectReleasesReservation verifies that closing the
// client connection that holds the URCB reservation releases it, allowing a
// second client to reserve the same URCB.
func TestGoServer_URCB_DisconnectReleasesReservation(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Client A reserves the URCB then disconnects.
	cA := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	if err := cA.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Fatalf("client A ReserveURCB: %v", err)
	}
	cA.Close(ctx) //nolint:errcheck

	// Allow the server to process the connection close.
	time.Sleep(100 * time.Millisecond)

	// Client B must now be able to reserve.
	cB := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer cB.Close(ctx)

	if err := cB.ReserveURCB(ctx, urcbLD, goServerURCBID); err != nil {
		t.Errorf("client B ReserveURCB after client A disconnect: %v", err)
	}
}

// TestGoServer_URCB_MultipleURCBs_DifferentClients verifies that two clients
// can reserve and enable different URCBs simultaneously.
//
// The test skips if the fixture exposes fewer than two URCBs.
func TestGoServer_URCB_MultipleURCBs_DifferentClients(t *testing.T) {
	h := startGoIEDServerWithReports(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Discover the available URCBs.
	c0 := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer c0.Close(ctx)

	rcbs, err := c0.ListReports(ctx, urcbLD)
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	var urcbs []string
	for _, r := range rcbs {
		if strings.Contains(r, "$RP$") {
			urcbs = append(urcbs, r)
		}
	}
	if len(urcbs) < 2 {
		t.Skipf("fixture only has %d URCB(s); need at least 2 for this test", len(urcbs))
	}

	urcb0 := urcbs[0]
	urcb1 := urcbs[1]
	t.Logf("using URCBs: %q and %q", urcb0, urcb1)

	// Client A reserves the first URCB.
	cA := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer cA.Close(ctx)
	if err := cA.ReserveURCB(ctx, urcbLD, urcb0); err != nil {
		t.Fatalf("client A ReserveURCB(%q): %v", urcb0, err)
	}

	// Client B reserves the second URCB.
	cB := dialIED(t, ctx, fmt.Sprintf("127.0.0.1:%d", h.port))
	defer cB.Close(ctx)
	if err := cB.ReserveURCB(ctx, urcbLD, urcb1); err != nil {
		t.Fatalf("client B ReserveURCB(%q): %v", urcb1, err)
	}

	// Both reservations must be reflected in the server state.
	rcbA, err := cA.GetReportControlBlock(ctx, urcbLD, urcb0)
	if err != nil {
		t.Fatalf("GetReportControlBlock(%q): %v", urcb0, err)
	}
	if !rcbA.Resv {
		t.Errorf("URCB %q: Resv should be true (reserved by client A)", urcb0)
	}

	rcbB, err := cB.GetReportControlBlock(ctx, urcbLD, urcb1)
	if err != nil {
		t.Fatalf("GetReportControlBlock(%q): %v", urcb1, err)
	}
	if !rcbB.Resv {
		t.Errorf("URCB %q: Resv should be true (reserved by client B)", urcb1)
	}
}
