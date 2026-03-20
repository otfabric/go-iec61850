// Package iec61850 provides a pure-Go IEC 61850 MMS client library
// built on top of [github.com/otfabric/go-mms].
//
// This package exposes IEC 61850 concepts — logical devices, logical
// nodes, data objects, data attributes, functional constraints, object
// references, report control blocks, datasets, quality, timestamps —
// as first-class Go types. Users work with IEC 61850 semantics, not
// raw MMS domains, item IDs, or alternate access selectors.
//
// # Quick start
//
//	client, err := iec61850.Dial(ctx, "10.0.0.1:102", iec61850.DialOptions{})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close(ctx)
//
//	// Browse the server model.
//	devices, _ := client.ListLogicalDevices(ctx)
//	for _, ld := range devices {
//	    nodes, _ := client.ListLogicalNodes(ctx, ld.Name)
//	    fmt.Println(ld.Name, nodes)
//	}
//
// # Layering
//
// This package sits above the protocol stack:
//
//   - go-tpkt / go-cotp: transport
//   - go-mms: generic MMS protocol
//   - go-iec61850 (this package): IEC 61850 semantics
//
// IEC 61850 semantics (references, functional constraints, typed values,
// reports, datasets, SCL) live here. The MMS wire protocol lives in
// go-mms. This separation is intentional and must be maintained.
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
// # Server (experimental)
//
// The [Server] type provides an experimental server-side model built
// from SCL. It handles variable registration, datasets, report
// control block structures, runtime report delivery, control
// operations, setting groups, and journal services. Use
// [Server.EnableReports] to activate the runtime report engine,
// [Server.EnableSettingGroups] to activate setting group support,
// [Server.EnableJournals] to activate runtime journal generation,
// and [Server.SetValue] to inject process values that automatically
// trigger data-change reports and journal entries. MMS client writes
// to RCB and SGCB subfields are automatically dispatched to the
// corresponding runtime engines via a write interceptor.
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
// All server runtime engines (reports, controls, setting groups,
// journals) are safe for concurrent use. Lock ordering follows
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
// All logging uses [log/slog]. Inject a logger via [DialOptions.Logger]
// or [ClientOptions.Logger]. When nil, no logging is emitted.
package iec61850
