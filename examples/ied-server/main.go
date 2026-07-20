// SPDX-License-Identifier: MIT

// Command ied-server is an end-to-end example of how a production
// IEC 61850 server application is structured using go-iec61850.
//
// It demonstrates the intended application lifecycle:
//
//  1. Parse an ICD file to build the data model.
//  2. Create the server and seed initial values.
//  3. Register a direct-operate control and an SBO control.
//  4. Enable the report engine (URCB/BRCB).
//  5. Start serving connections.
//  6. Simulate periodic measurement updates.
//  7. Handle SIGINT for graceful shutdown.
//
// Usage:
//
//	go run ./examples/ied-server [addr] [icd-file]
//	    addr      TCP address to listen on (default: 0.0.0.0:102)
//	    icd-file  Path to an IEC 61850 ICD/CID file (default: built-in model)
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strings"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/scl"
	mms "github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

const minimalICD = `<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL">
  <IED name="DemoIED">
    <AccessPoint name="S1">
      <Server>
        <LDevice inst="LD1">
          <LN0 lnClass="LLN0" inst="" lnType="LLN0T">
            <DataSet name="dsAll">
              <FCDA ldInst="LD1" lnClass="MMXU" lnInst="1" doName="PhV" daName="phsA.cVal.mag.f" fc="MX"/>
              <FCDA ldInst="LD1" lnClass="GGIO" lnInst="1" doName="SPCSO1" daName="stVal" fc="ST"/>
            </DataSet>
            <ReportControl name="urcb01" confRev="1" datSet="dsAll" rptID="demo_urcb01"
              buffered="false" intgPd="0">
              <TrgOps dchg="true" qchg="true" dupd="false" period="false" gi="true"/>
              <OptFields seqNum="true" timeStamp="true" dataRef="false" reasonCode="true"
                entryID="false" configRef="false" bufOvfl="false"/>
              <RptEnabled max="5"/>
            </ReportControl>
          </LN0>
          <LN lnClass="GGIO" inst="1" lnType="GGIO1T">
            <DOI name="SPCSO1">
              <DAI name="ctlModel"><Val>direct-with-normal-security</Val></DAI>
              <DAI name="stVal"><Val>false</Val></DAI>
            </DOI>
          </LN>
          <LN lnClass="MMXU" inst="1" lnType="MMXU1T">
            <DOI name="PhV">
              <SDI name="phsA">
                <SDI name="cVal">
                  <SDI name="mag">
                    <DAI name="f"><Val>0.0</Val></DAI>
                  </SDI>
                </SDI>
              </SDI>
            </DOI>
          </LN>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="LLN0T" lnClass="LLN0"/>
    <LNodeType id="GGIO1T" lnClass="GGIO">
      <DO name="SPCSO1" type="SPCSO_T"/>
    </LNodeType>
    <LNodeType id="MMXU1T" lnClass="MMXU">
      <DO name="PhV" type="WYE_T"/>
    </LNodeType>
    <DOType id="SPCSO_T" cdc="SPC">
      <DA name="stVal" bType="BOOLEAN" fc="ST"/>
      <DA name="ctlModel" bType="Enum" type="CtlModelEnum" fc="CF"/>
    </DOType>
    <DOType id="WYE_T" cdc="WYE">
      <SDO name="phsA" type="CMV_T"/>
    </DOType>
    <DOType id="CMV_T" cdc="CMV">
      <SDO name="cVal" type="Vector_T"/>
    </DOType>
    <DOType id="Vector_T" cdc="Vector">
      <SDO name="mag" type="AnalogueVal_T"/>
    </DOType>
    <DOType id="AnalogueVal_T" cdc="AnalogueValue">
      <DA name="f" bType="FLOAT32" fc="MX"/>
    </DOType>
    <DAType id="ignored"/>
    <EnumType id="CtlModelEnum">
      <EnumVal ord="0">status-only</EnumVal>
      <EnumVal ord="1">direct-with-normal-security</EnumVal>
      <EnumVal ord="2">sbo-with-normal-security</EnumVal>
    </EnumType>
  </DataTypeTemplates>
</SCL>`

func main() {
	addr := "0.0.0.0:102"
	icdPath := ""
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	if len(os.Args) > 2 {
		icdPath = os.Args[2]
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// -----------------------------------------------------------------------
	// 1. Parse ICD and build server model
	// -----------------------------------------------------------------------

	var sclData *scl.SCL
	var err error
	if icdPath != "" {
		sclData, err = scl.ParseFile(icdPath)
		if err != nil {
			log.Fatalf("parse ICD %s: %v", icdPath, err)
		}
		logger.Info("loaded ICD", "path", icdPath)
	} else {
		sclData, err = scl.Parse(strings.NewReader(minimalICD))
		if err != nil {
			log.Fatalf("parse built-in model: %v", err)
		}
		logger.Info("using built-in minimal model")
	}

	model, err := iec61850.NewServerModelFromSCL(sclData, "DemoIED", "")
	if err != nil {
		log.Fatalf("build server model: %v", err)
	}

	// -----------------------------------------------------------------------
	// 2. Create server and seed initial values
	// -----------------------------------------------------------------------

	srv, err := iec61850.NewServer(model, iec61850.ServerOptions{
		Identity: &iec61850.ServerIdentity{
			Vendor:   "OTFabric",
			Model:    "DemoIED",
			Revision: "1.0",
		},
		Logger: logger,
	})
	if err != nil {
		log.Fatalf("new server: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	store := srv.ValueStore()

	// Seed initial measurement value.
	phsAKey := servermodel.StoreKey("LD1", "MMXU1$MX$PhV$phsA$cVal$mag$f")
	store.Set(phsAKey, mms.NewFloat(0.0))

	// Seed initial switch state.
	spcsoKey := servermodel.StoreKey("LD1", "GGIO1$ST$SPCSO1$stVal")
	store.Set(spcsoKey, mms.NewBoolean(false))

	// -----------------------------------------------------------------------
	// 3. Register controls
	// -----------------------------------------------------------------------

	// Direct-operate control for GGIO1.SPCSO1.
	if err := srv.RegisterControl("LD1", "GGIO1.SPCSO1", iec61850.CtlModelDirectNormal,
		iec61850.ControlHandler{
			OnOperate: func(ctx context.Context, req iec61850.ControlRequest) error {
				b, _ := req.CtlVal.Bool()
				logger.Info("direct operate", "SPCSO1", b)
				return nil
			},
		},
	); err != nil {
		log.Fatalf("register direct control: %v", err)
	}

	// -----------------------------------------------------------------------
	// 4. Enable report engine
	// -----------------------------------------------------------------------

	re := srv.EnableReports()
	_ = re // use re.TriggerGI / re.SetValue etc. if needed directly

	// -----------------------------------------------------------------------
	// 5. Start listening
	// -----------------------------------------------------------------------

	ln, err := iso.Listen(addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	fmt.Printf("IEC 61850 server listening on %s\n", addr)

	go func() {
		if err := srv.ListenAndServe(ctx, ln); err != nil {
			if ctx.Err() == nil {
				logger.Error("serve", "err", err)
			}
		}
	}()

	// -----------------------------------------------------------------------
	// 6. Simulate periodic measurement updates (sine wave)
	// -----------------------------------------------------------------------

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var t float64
		for {
			select {
			case <-ticker.C:
				t += 0.2
				v := float32(230.0 * math.Sin(t))
				srv.SetValue(ctx, phsAKey, mms.NewFloat(float64(v)))
				logger.Debug("updated PhV.phsA", "value", v)

			case <-ctx.Done():
				return
			}
		}
	}()

	// -----------------------------------------------------------------------
	// 7. Wait for shutdown signal
	// -----------------------------------------------------------------------

	<-ctx.Done()
	fmt.Println("\nShutting down server...")
	srv.Close()
	fmt.Println("Done.")
}
