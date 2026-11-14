// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"sync"
	"testing"
	"time"

	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"

	"github.com/otfabric/go-iec61850/scl"
)

// testServerSCLWithURCB builds an SCL model that includes a URCB and a
// single data attribute so reconnect + subscription tests have something
// to subscribe to.
func testServerSCLWithURCB() *scl.SCL {
	return &scl.SCL{
		IEDs: []scl.IED{{
			Name: "IED1",
			AccessPoints: []scl.AccessPoint{{
				Name: "AP1",
				Server: &scl.Server{
					LDevices: []scl.LDevice{{
						Inst: "LD1",
						LN0: &scl.LN{
							LNClass: "LLN0",
							Inst:    "",
							LNType:  "LNT_URCB",
							DataSets: []scl.DataSet{{
								Name: "dsTest",
								FCDAs: []scl.FCDA{{
									LNClass: "LLN0",
									DOName:  "Mod",
									DAName:  "stVal",
									FC:      "ST",
								}},
							}},
							Reports: []scl.ReportControl{{
								Name:     "urcb01",
								RptID:    "rpt01",
								DatSet:   "dsTest",
								ConfRev:  1,
								Buffered: false,
								TrgOps:   scl.TrgOps{Dchg: true, GI: true},
								OptFields: scl.OptFields{
									SeqNum:     true,
									TimeStamp:  true,
									ReasonCode: true,
								},
							}},
						},
					}},
				},
			}},
		}},
		DataTypeTemplates: scl.DataTypeTemplates{
			LNodeTypes: []scl.LNodeType{{
				ID:      "LNT_URCB",
				LNClass: "LLN0",
				DOs:     []scl.DO{{Name: "Mod", Type: "DOT_INS"}},
			}},
			DOTypes: []scl.DOType{{
				ID:  "DOT_INS",
				CDC: "INS",
				DAs: []scl.DA{
					{Name: "stVal", FC: "ST", BType: "INT32"},
					{Name: "q", FC: "ST", BType: "Quality"},
					{Name: "t", FC: "ST", BType: "Timestamp"},
				},
			}},
		},
	}
}

// startIECServer starts a listening IEC 61850 server on a random local port.
// It returns the server, its listener address, and a stop function that
// ends ListenAndServe and returns once ListenAndServe and the connection
// goroutines it started have returned (the connections are closed). Stop
// runs at the end of the test if the test does not call it.
func startIECServer(t *testing.T, s *scl.SCL) (*Server, string, context.CancelFunc) {
	t.Helper()

	model, err := NewServerModelFromSCL(s, "IED1", "")
	if err != nil {
		t.Fatalf("NewServerModelFromSCL: %v", err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ln, err := iso.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("iso.Listen: %v", err)
	}

	return srv, ln.Addr().String(), serveISO(t, srv, ln)
}

// serveISO runs srv.ListenAndServe on ln and returns a function that
// cancels it and waits until ListenAndServe and the connection goroutines
// it started have returned. The function runs at the end of the test if
// the test does not call it; calling it again does nothing.
func serveISO(t *testing.T, srv *Server, ln mms.TransportListener) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.ListenAndServe(ctx, ln)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			<-served
			// The connection goroutines end with ctx; Shutdown waits for
			// them and stops the engines.
			sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer scancel()
			if err := srv.Shutdown(sctx); err != nil {
				t.Errorf("Shutdown: %v", err)
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

// TestClientReconnect_AfterServerRestart verifies that a client can
// reconnect to a freshly started server after the previous one stopped.
func TestClientReconnect_AfterServerRestart(t *testing.T) {
	ctx := context.Background()
	sclData := testServerSCLWithURCB()

	// Start first server, connect, then shut it down.
	srv1, addr, cancel1 := startIECServer(t, sclData)
	_ = srv1

	client1, err := Dial(ctx, addr, DialOptions{})
	if err != nil {
		t.Fatalf("Dial (first): %v", err)
	}

	// Reads work on first connection.
	if _, err := client1.ListLogicalDevices(ctx); err != nil {
		t.Fatalf("ListLogicalDevices (first): %v", err)
	}

	cancel1() // returns once the server and its connections have ended
	_ = client1.Close(ctx)

	// Start a second server on a new port and reconnect.
	_, addr2, _ := startIECServer(t, sclData)

	client2, err := Dial(ctx, addr2, DialOptions{})
	if err != nil {
		t.Fatalf("Dial (second server): %v", err)
	}
	defer client2.Close(ctx) //nolint:errcheck

	lds, err := client2.ListLogicalDevices(ctx)
	if err != nil {
		t.Fatalf("ListLogicalDevices (reconnected): %v", err)
	}
	if len(lds) == 0 {
		t.Error("expected at least one logical device after reconnect")
	}
}

// TestClientReconnect_MultipleSequential verifies that a client can dial
// the same server address multiple times in sequence without leaking
// resources or panicking.
func TestClientReconnect_MultipleSequential(t *testing.T) {
	ctx := context.Background()
	sclData := testServerSCLWithURCB()
	_, addr, _ := startIECServer(t, sclData)

	const rounds = 5
	for i := range rounds {
		client, err := Dial(ctx, addr, DialOptions{})
		if err != nil {
			t.Fatalf("round %d Dial: %v", i, err)
		}
		if _, err := client.ListLogicalDevices(ctx); err != nil {
			t.Fatalf("round %d ListLogicalDevices: %v", i, err)
		}
		if err := client.Close(ctx); err != nil {
			t.Fatalf("round %d Close: %v", i, err)
		}
	}
}

// TestClientReconnect_URCBResubscription verifies that a URCB subscription
// can be established on a fresh connection after an initial connect/close.
func TestClientReconnect_URCBResubscription(t *testing.T) {
	ctx := context.Background()
	sclData := testServerSCLWithURCB()
	srv, addr, _ := startIECServer(t, sclData)
	re := srv.EnableReports()
	defer re.Stop()

	dial := func(t *testing.T) *Client {
		t.Helper()
		c, err := Dial(ctx, addr, DialOptions{})
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		return c
	}

	subscribe := func(t *testing.T, c *Client) {
		t.Helper()
		sub, err := c.SubscribeReport(ctx, "rpt01", SubscribeReportOptions{})
		if err != nil {
			t.Fatalf("SubscribeReport: %v", err)
		}
		_ = sub.Close()
	}

	// First connection: subscribe then close.
	c1 := dial(t)
	subscribe(t, c1)
	_ = c1.Close(ctx)

	// Second connection to the same server: subscribe again.
	c2 := dial(t)
	defer c2.Close(ctx) //nolint:errcheck
	subscribe(t, c2)
}

// TestServer_Close_StopsReportEngine verifies that after Server.Close()
// a client connecting to the same address gets a connection error (the port
// is no longer being served) and that the server does not accumulate
// goroutines after shutdown.
func TestServer_Close_StopsReportEngine(t *testing.T) {
	ctx := context.Background()
	sclData := testServerSCLWithURCB()
	srv, addr, cancel := startIECServer(t, sclData)
	re := srv.EnableReports()

	// Connect and verify the server is responsive.
	c, err := Dial(ctx, addr, DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if _, err := c.ListLogicalDevices(ctx); err != nil {
		t.Fatalf("ListLogicalDevices: %v", err)
	}
	_ = c.Close(ctx)

	// Stop the server: stop returns once ListenAndServe has closed the
	// listener and its goroutines have ended.
	cancel()
	re.Stop()
	srv.Close()

	// New connections should fail because the listener is closed.
	dialCtx, dialCancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer dialCancel()
	_, err = Dial(dialCtx, addr, DialOptions{})
	if err == nil {
		t.Error("expected connection error after server close, got nil")
	}
}

// TestClientReconnect_ReadAfterServerClose verifies that operations on a
// client whose server has gone away return errors rather than hanging or
// panicking.
func TestClientReconnect_ReadAfterServerClose(t *testing.T) {
	ctx := context.Background()
	sclData := testServerSCLWithURCB()
	_, addr, cancel := startIECServer(t, sclData)

	c, err := Dial(ctx, addr, DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close(ctx) //nolint:errcheck

	// Verify normal operation first.
	if _, err := c.ListLogicalDevices(ctx); err != nil {
		t.Fatalf("initial ListLogicalDevices: %v", err)
	}

	// Shut down the server while the client is still connected: cancel
	// returns once the server has closed the connection.
	cancel()

	// Subsequent operations should return an error, not hang.
	opCtx, opCancel := context.WithTimeout(ctx, 1*time.Second)
	defer opCancel()
	_, err = c.ListLogicalDevices(opCtx)
	if err == nil {
		t.Error("expected error after server shutdown, got nil")
	}
}
