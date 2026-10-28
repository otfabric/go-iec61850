// SPDX-License-Identifier: MIT

// Package iec61850 is a pure-Go IEC 61850 stack: an MMS client and
// server (IEC 61850-8-1 over [github.com/otfabric/go-mms]), GOOSE
// (IEC 61850-8-1) and Sampled Values (IEC 61850-9-2, UCA 9-2LE), with
// SCL (IEC 61850-6) parsing, validation and model binding. No cgo.
//
// IEC 61850 concepts — logical devices, logical nodes, data objects,
// data attributes, functional constraints, object references, datasets,
// report, GOOSE and SV control blocks, quality, timestamps — are
// first-class Go types. Applications work with IEC 61850 semantics, not
// raw MMS domains, item IDs or alternate access selectors.
//
// # Packages
//
//   - iec61850 (this package): MMS client ([Client]) and server
//     ([Server]), including the server's GOOSE and SV engines and the
//     client's GoCB and MSVCB access.
//   - [github.com/otfabric/go-iec61850/goose]: GOOSE frame codec,
//     publisher and supervised subscriber, SCL binding.
//   - [github.com/otfabric/go-iec61850/sv]: Sampled Values frame codec,
//     9-2LE helpers, paced publisher and subscriber with loss accounting,
//     SCL binding.
//   - [github.com/otfabric/go-iec61850/l2]: raw Ethernet transport
//     (Linux AF_PACKET, macOS/BSD BPF) and an in-memory hub, shared by
//     goose and sv.
//   - [github.com/otfabric/go-iec61850/scl] with scl/index and
//     scl/validate: SCL parsing (schema editions 1.7 to 2007C5),
//     normalized model, cross-reference index, semantic validation.
//
// # Quick start
//
//	client, err := iec61850.Dial(ctx, "10.0.0.1:102", iec61850.DialOptions{
//	    Retry: mms.DefaultRetryPolicy(),
//	})
//	if err != nil {
//	    return err
//	}
//	defer func() { _ = client.Close(ctx) }()
//
//	ref, _ := iec61850.ParseRef("LD1/MMXU1.PhV.phsA.cVal.mag.f[MX]")
//	val, err := client.Read(ctx, ref)
//
// The README and the examples directory have complete programs for the
// client, the server, GOOSE and SV.
//
// # Layering
//
//   - go-tpkt / go-cotp: ISO transport over TCP
//   - go-mms: generic MMS protocol (ISO 9506)
//   - go-iec61850 (this module): IEC 61850 semantics over MMS, plus
//     GOOSE and SV directly on Ethernet (package l2)
//
// IEC 61850 semantics live here; the MMS wire protocol lives in go-mms.
//
// # Object references
//
// IEC 61850 object references follow the format:
//
//	LD/LN.DO.DA[FC]
//
// Use [ParseRef] to parse references and [Ref.String] to format them.
// The [Ref] type provides helpers for parent/child navigation and
// MMS domain/item-ID translation.
//
// # Functional constraints
//
// Functional constraints (FCs) classify data attributes by purpose:
// ST (status), MX (measured), SP (setpoint), CF (configuration), etc.
// Use [FunctionalConstraint.IsValid] or [ParseFC] to validate FC
// strings. Note that [ParseRef] accepts unknown FC values by default;
// use [FunctionalConstraint.IsValid] for semantic validation after
// parsing.
//
// # API groups
//
// The package is organised into functional groups:
//
//   - Browse / model discovery: [Client.ListLogicalDevices],
//     [Client.ListLogicalNodes], [Client.ListDataObjects],
//     [Client.ListChildren], [Client.Tree], [Client.TreeWithOptions],
//     [Client.FindPaths], [Client.GetVariableType]
//   - Read / write: [Client.Read], [Client.ReadRaw],
//     [Client.ReadComponent], [Client.Write],
//     [Client.ReadMultiple], [Client.WriteMultiple]
//   - Datasets: [Client.ListDataSets], [Client.GetDataSet],
//     [Client.ReadDataSet], [Client.CreateDataSet],
//     [Client.DeleteDataSet]
//   - Reports: [Client.ListReports], [Client.SubscribeReport],
//     [Client.GetReportControlBlock], [Client.SetReportControlBlock],
//     [Client.TriggerGI], [Client.ReserveURCB], [Client.ReleaseURCB]
//   - Controls: [Client.Operate], [Client.Select],
//     [Client.SelectWithValue], [Client.Cancel],
//     [Client.ReadCtlModel], [Client.ReadLastApplError]
//   - Setting groups: [Client.GetSettingGroupInfo],
//     [Client.SelectActiveSG], [Client.SelectEditSG],
//     [Client.ConfirmEditSG], [Client.GetEditSGValue],
//     [Client.SetEditSGValue], [Client.GetActiveSGValue]
//   - Journals: [Client.ListJournals], [Client.ReadJournal],
//     [Client.ReadJournalAfter], [Client.ReadJournalAll],
//     [Client.ReadJournalAfterAll]
//   - GOOSE and SV control blocks: [Client.GetGoCB], [Client.SetGoCB],
//     [Client.GetMSVCB], [Client.SetMSVCB]
//   - Files: [Client.ListFiles], [Client.ReadFile],
//     [Client.DownloadFile], [Client.DeleteFile], [Client.RenameFile],
//     [Client.ObtainFile], [Client.GetFileAttributes]
//   - SCL: [scl.Parse], [scl.ParseFile], [scl.Generate],
//     [scl.Validate], [scl.Flatten], [scl.WriteCSV],
//     [scl.PrintTree], [scl.ExportDataSets], [scl.ExportReports]
//   - Server runtime reports: [Server.EnableReports],
//     [Server.SetValue], [Server.ReportEngine],
//     [ReportEngine.HandleRCBWrite], [ReportEngine.NotifyValueChanged],
//     [ReportEngine.Stop]
//   - Server setting groups: [Server.EnableSettingGroups],
//     [Server.SettingGroupEngine], [Server.ChangeActiveSettingGroup],
//     [SettingGroupEngine.HandleSGCBWrite],
//     [SettingGroupEngine.GetActiveSettingGroup],
//     [SettingGroupEngine.GetEditSettingGroup]
//   - Server journals: [Server.EnableJournals],
//     [Server.JournalEngine], [JournalEngine.LogEvent],
//     [JournalEngine.LogValueWrite], [JournalEngine.Provider],
//     [MemoryJournalProvider], [NewMemoryJournalProvider]
//   - Server GOOSE and SV: [Server.EnableGOOSE], [GOOSEEngine],
//     [Server.EnableSV], [SVEngine]
//   - Server services: [ServerOptions] (Identity, FileProvider,
//     Authenticate, OnConnect, OnDisconnect), [Server.Capabilities],
//     [Server.HandleIdentify], [Server.HandleStatus],
//     [ServiceCapabilities]
//
// # IEC references vs MMS names
//
// This package uses IEC 61850 object references (LD/LN.DO.DA[FC])
// throughout its public API. The underlying MMS names use a different
// convention:
//
//   - MMS domain = logical device name
//   - MMS item ID = LN$FC$DO$DA (components joined by $)
//   - MMS dataset/NVL name = LLN0$dsName
//
// Use [Ref.ToMMS] for conversion when needed.
//
// # Errors
//
// The package defines sentinel errors ([ErrInvalidReference],
// [ErrNotFound], [ErrInvalidArgument], [ErrProtocol], etc.) for use
// with [errors.Is], and typed error structs ([ReferenceError],
// [DecodeError], [DataAccessError], etc.) for use with [errors.As].
// Lower-level go-mms errors are wrapped with [fmt.Errorf] and %w to
// preserve the error chain.
//
// # Server
//
// The [Server] type serves an IEC 61850 model built from SCL
// ([NewServerModelFromSCL]) over MMS. It handles variable access,
// datasets, report control blocks with runtime report delivery,
// controls (direct, SBO, SBOw with CommandTermination), setting groups,
// journals, files, and the GOOSE and SV control blocks. Use
// [Server.EnableReports], [Server.EnableSettingGroups],
// [Server.EnableJournals], [Server.EnableGOOSE] and [Server.EnableSV]
// to start the runtime engines, and [Server.SetValue] to update process
// values: a change drives reports, journal entries, GOOSE state changes
// and SV samples. MMS client writes to RCB, SGCB, GoCB and MSVCB
// attributes are dispatched to the corresponding engine.
//
// [ServerOptions] provides first-class fields for common server
// configuration: [ServerIdentity] for MMS Identify, FileProvider
// for MMS file services, Authenticate for association security, and
// OnConnect/OnDisconnect for connection lifecycle hooks. The MMS
// Status service is always registered and returns operational status.
// Use [Server.Capabilities] to query which services are active at
// runtime. Call [Server.Close] for orderly shutdown of runtime
// engines.
//
// # Concurrency
//
// [Client] methods are safe for concurrent use. All server runtime
// engines (reports, controls, setting groups, journals, GOOSE, SV) are
// safe for concurrent use. Lock ordering follows
// engine-level lock → per-element lock. The control runtime uses
// per-registration mutexes for SBO state; the report engine uses
// engine-level RWMutex → per-RCB mutex. These have been validated
// with concurrent stress tests and Go's race detector.
//
// Cross-LN dataset references are not supported. See [Server] and
// [NewServerModelFromSCL] for details and known limitations.
//
// # Logging
//
// All logging uses [log/slog]. Inject a logger via [DialOptions.Logger],
// [ClientOptions.Logger], or [ServerOptions.Logger]. When nil, no logging
// is emitted. On Dial/NewServer, a nil nested MMS logger inherits the IEC
// logger; [iso.WithLogger] is independent. [DialOptions.Metrics],
// [ServerOptions.Metrics] and the goose and sv Metrics interfaces expose
// counters. See OBSERVABILITY.md.
//
// # Stability
//
// The module follows semantic versioning; the v1 API of every
// non-internal package, client and server alike, is stable. See
// COMPATIBILITY.md.
package iec61850
