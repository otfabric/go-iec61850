package iec61850

import (
	"context"

	"github.com/otfabric/go-mms"
)

// ServerIdentity describes the IEC 61850 server identity returned
// by the MMS Identify service. Setting this on [ServerOptions]
// automatically registers an Identify handler on the MMS server.
type ServerIdentity struct {
	// Vendor is the vendor/manufacturer name (e.g. "OTFabric").
	Vendor string

	// Model is the device model or product name.
	Model string

	// Revision is the firmware or software revision string.
	Revision string
}

// ConnectionEvent is passed to [ServerOptions.OnConnect] and
// [ServerOptions.OnDisconnect] callbacks.
//
// The event currently provides no connection-specific context because
// the underlying MMS layer does not expose the [mms.ServerConn] at
// the transport-accept boundary. Future versions may add a connection
// identifier or authentication token when the MMS API supports it.
type ConnectionEvent struct{}

// ServiceCapabilities reports which IEC 61850 server-side services
// are active on this server instance.
//
// There is a distinction between "configured" (the model contains
// the necessary definitions) and "runtime-enabled" (the application
// called the Enable method). For reports, setting groups, and
// journals the boolean reflects runtime enablement, not merely
// model presence. Use this to verify that all expected services
// have been enabled before accepting connections.
type ServiceCapabilities struct {
	// Variables is always true (the server always registers variables).
	Variables bool

	// DataSets is true when the model contains dataset definitions.
	// This is a model-level property, not a runtime toggle.
	DataSets bool

	// Reports is true when [Server.EnableReports] has been called
	// (runtime-enabled, not just configured in the model).
	Reports bool

	// Controls is true when at least one control has been registered
	// via [Server.RegisterControl] (runtime-enabled).
	Controls bool

	// SettingGroups is true when [Server.EnableSettingGroups] has been
	// called (runtime-enabled, not just present in the model).
	SettingGroups bool

	// Journals is true when [Server.EnableJournals] has been called
	// (runtime-enabled, not just present in the model).
	Journals bool

	// Files is true when a [mms.FileProvider] was configured.
	Files bool

	// Identify is true when server identity was configured.
	Identify bool
}

// Capabilities returns a snapshot of which IEC 61850 services are
// currently active on this server. This is useful for diagnostics
// and for verifying that all expected services have been enabled
// before starting to accept connections.
func (s *Server) Capabilities() ServiceCapabilities {
	hasDataSets := false
	for i := range s.model.LogicalDevices {
		for j := range s.model.LogicalDevices[i].LogicalNodes {
			if len(s.model.LogicalDevices[i].LogicalNodes[j].DataSets) > 0 {
				hasDataSets = true
				break
			}
		}
		if hasDataSets {
			break
		}
	}

	s.controlMu.RLock()
	hasControls := len(s.controls) > 0
	s.controlMu.RUnlock()

	return ServiceCapabilities{
		Variables:     true,
		DataSets:      hasDataSets,
		Reports:       s.reportEngine != nil,
		Controls:      hasControls,
		SettingGroups: s.sgEngine != nil,
		Journals:      s.journalEngine != nil,
		Files:         s.hasFileProvider,
		Identify:      s.hasIdentity,
	}
}

// HandleIdentify registers the MMS Identify handler using the given
// identity. This is called automatically by [NewServer] when
// [ServerOptions.Identity] is set.
func (s *Server) HandleIdentify(id ServerIdentity) {
	s.hasIdentity = true
	s.mms.HandleIdentify(func(_ context.Context, _ mms.IdentifyRequest) (*mms.ServerIdentity, error) {
		return &mms.ServerIdentity{
			Vendor:   id.Vendor,
			Model:    id.Model,
			Revision: id.Revision,
		}, nil
	})
}

// HandleStatus registers a static MMS Status response. The server
// always reports operational status.
func (s *Server) HandleStatus() {
	s.mms.HandleStatus(func(_ context.Context, _ mms.StatusRequest) (*mms.ServerStatus, error) {
		return &mms.ServerStatus{
			Logical:  mms.VMDLogicalStatusStateChangesAllowed,
			Physical: mms.VMDPhysicalStatusOperational,
		}, nil
	})
}

// String returns a human-readable summary of enabled services,
// useful for diagnostics and startup logging.
func (c ServiceCapabilities) String() string {
	var services []string
	if c.Variables {
		services = append(services, "variables")
	}
	if c.DataSets {
		services = append(services, "datasets")
	}
	if c.Reports {
		services = append(services, "reports")
	}
	if c.Controls {
		services = append(services, "controls")
	}
	if c.SettingGroups {
		services = append(services, "setting-groups")
	}
	if c.Journals {
		services = append(services, "journals")
	}
	if c.Files {
		services = append(services, "files")
	}
	if c.Identify {
		services = append(services, "identify")
	}
	if len(services) == 0 {
		return "ServiceCapabilities(none)"
	}
	result := "ServiceCapabilities("
	for i, svc := range services {
		if i > 0 {
			result += ", "
		}
		result += svc
	}
	result += ")"
	return result
}
