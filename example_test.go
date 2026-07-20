// SPDX-License-Identifier: MIT

// Package iec61850 provides an IEC 61850 MMS client and server implementation.
//
// See the package-level documentation in [doc.go] for a full overview.
// The examples below show the most common usage patterns.
package iec61850

import (
	"context"
	"fmt"
	"log"
	"time"

	mms "github.com/otfabric/go-mms"
)

// ────────────────────────────────────────────────────────────────────────────
// Client examples
// ────────────────────────────────────────────────────────────────────────────

// ExampleDial demonstrates connecting to a remote IED and reading a value.
func ExampleDial() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	client, err := Dial(ctx, "192.0.2.1:102", DialOptions{
		IEDName: "IED1",
	})
	if err != nil {
		cancel()
		log.Fatal(err)
	}
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	ref, err := ParseRef("LD1/GGIO1.Ind1.stVal[ST]")
	if err != nil {
		log.Fatal(err)
	}

	val, err := client.Read(ctx, ref)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(val)
}

// ExampleClient_SubscribeReport demonstrates subscribing to a URCB and
// receiving data-change reports.
func ExampleClient_SubscribeReport() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	client, err := Dial(ctx, "192.0.2.1:102", DialOptions{})
	if err != nil {
		cancel()
		log.Fatal(err)
	}
	defer cancel()
	defer func() { _ = client.Close(ctx) }()

	sub, err := client.SubscribeReport(ctx, "urcb01", SubscribeReportOptions{
		LD:             "LD1",
		RCBItemID:      "LLN0$RP$urcb01",
		AutoEnable:     true,
		GIOnSubscribe:  true,
		QueueSize:      64,
		OverflowPolicy: OverflowDropNewest,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = sub.Close() }()

	for rpt := range sub.Reports() {
		for i, v := range rpt.Values {
			if i < len(rpt.ReasonCodes) {
				fmt.Printf("member[%d]: reason=%v value=%v\n", i, rpt.ReasonCodes[i], v)
			} else {
				fmt.Printf("member[%d]: value=%v\n", i, v)
			}
		}
	}
}

// ExampleClient_Write demonstrates writing a value to an attribute.
func ExampleClient_Write() {
	ctx := context.Background()

	client, err := Dial(ctx, "192.0.2.1:102", DialOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = client.Close(ctx) }()

	ref, err := ParseRef("LD1/LLN0.Mod.stVal[ST]")
	if err != nil {
		log.Fatal(err)
	}

	if err := client.Write(ctx, ref, mms.NewInteger(1)); err != nil {
		log.Fatal(err)
	}
}

// ExampleClient_GetReportControlBlock demonstrates reading an RCB's current
// attributes without subscribing.
func ExampleClient_GetReportControlBlock() {
	ctx := context.Background()

	client, err := Dial(ctx, "192.0.2.1:102", DialOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = client.Close(ctx) }()

	rcb, err := client.GetReportControlBlock(ctx, "LD1", "LLN0$RP$urcb01")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("RptID=%q DatSet=%q ConfRev=%d RptEna=%v\n",
		rcb.RptID, rcb.DatSet, rcb.ConfRev, rcb.RptEna)
}

// ────────────────────────────────────────────────────────────────────────────
// Server examples
// ────────────────────────────────────────────────────────────────────────────

// ExampleNewServer demonstrates creating and serving a minimal IEC 61850 server
// built from an in-process SCL model.
func ExampleNewServer() {
	// Build the server model from SCL.
	model, err := NewServerModelFromSCL(testServerSCLWithURCB(), "IED1", "")
	if err != nil {
		log.Fatal(err)
	}

	// Create the server.
	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		log.Fatal(err)
	}

	// Optionally start the report engine.
	re := srv.EnableReports()
	defer re.Stop()

	// Register a custom control handler.
	_ = srv.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{
		OnOperate: func(ctx context.Context, req ControlRequest) error {
			val, _ := req.CtlVal.Bool()
			log.Printf("operate: ctlVal=%v", val)
			return nil
		},
	})
	// Serve connections (typically via iso.Listen for production use).
	ctx := context.Background()
	_ = ctx
	// In a real program:
	// ln, _ := iso.Listen(":102")
	// log.Fatal(srv.ListenAndServe(ctx, ln))
}

// ExampleServer_SetValue demonstrates pushing a data value into the server
// model, which triggers data-change reports to subscribed clients.
func ExampleServer_SetValue() {
	model, err := NewServerModelFromSCL(testServerSCLWithURCB(), "IED1", "")
	if err != nil {
		log.Fatal(err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		log.Fatal(err)
	}
	re := srv.EnableReports()
	defer re.Stop()
	defer srv.Close()

	// Push a new value — this triggers a dchg report to subscribed clients.
	srv.SetValue(context.Background(), "LD1/LLN0$ST$Mod$stVal", mms.NewInteger(1))
}

// ────────────────────────────────────────────────────────────────────────────
// Control examples
// ────────────────────────────────────────────────────────────────────────────

// ExampleServer_RegisterControl demonstrates registering a direct control.
func ExampleServer_RegisterControl() {
	model, err := NewServerModelFromSCL(testServerSCLWithURCB(), "IED1", "")
	if err != nil {
		log.Fatal(err)
	}

	srv, err := NewServer(model, ServerOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer srv.Close()

	_ = srv.RegisterControl("LD1", "GGIO1.SPCSO1", CtlModelDirectNormal, ControlHandler{
		OnOperate: func(ctx context.Context, req ControlRequest) error {
			val, _ := req.CtlVal.Bool()
			log.Printf("operate SPCSO1: %v", val)
			return nil
		},
		OnCancel: func(ctx context.Context, req ControlRequest) error {
			log.Println("SPCSO1 cancelled")
			return nil
		},
	})
}

// ────────────────────────────────────────────────────────────────────────────
// SCL examples
// ────────────────────────────────────────────────────────────────────────────

// ExampleParseRef demonstrates parsing an IEC 61850 object reference string.
func ExampleParseRef() {
	ref, err := ParseRef("LD1/GGIO1.Ind1.stVal[ST]")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(ref.LD)      // "LD1"
	fmt.Println(ref.LN)      // "GGIO1"
	fmt.Println(ref.Path[0]) // "Ind1"
	fmt.Println(ref.Path[1]) // "stVal"
	fmt.Println(ref.FC)      // "ST"
}
