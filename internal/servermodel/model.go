// SPDX-License-Identifier: MIT

// Package servermodel defines the IEC 61850 server-side data model.
//
// These types describe the logical structure of an IEC 61850 MMS
// server: logical devices, logical nodes, data objects, data
// attributes, datasets, and report control blocks. The model is
// Go-native and independent of any wire format or config file layout.
//
// A [Model] is typically built from an SCL file via the config
// generation path and then registered with the MMS server to create
// the variable registry, named variable lists, and report hooks.
package servermodel

import "fmt"

// Model is the top-level container for an IEC 61850 server data model.
// It holds the complete logical structure served by a single MMS server
// (i.e., one AccessPoint/Server in SCL terms).
type Model struct {
	// LogicalDevices are the logical devices served by this model.
	LogicalDevices []LogicalDevice

	// Warnings collects non-fatal issues found during model building,
	// such as unmatched DAI/SDI overrides in the SCL.
	Warnings []string
}

// LogicalDevice represents an IEC 61850 logical device (MMS domain).
type LogicalDevice struct {
	// Name is the logical device instance name (MMS domain ID).
	Name string

	// LogicalNodes are the logical nodes within this LD.
	LogicalNodes []LogicalNode
}

// LogicalNode represents an IEC 61850 logical node.
type LogicalNode struct {
	// Name is the full LN name (prefix + lnClass + inst, e.g. "LLN0", "GGIO1").
	Name string

	// LNClass is the IEC 61850 logical node class (e.g. "LLN0", "GGIO").
	LNClass string

	// DataObjects are the data objects defined for this LN.
	DataObjects []DataObject

	// DataSets are the datasets defined under this LN.
	DataSets []DataSetDef

	// Reports are the report control blocks defined under this LN.
	Reports []ReportDef

	// Logs are the log control block definitions for this LN.
	Logs []LogDef

	// SettingGroup, when non-nil, defines the setting group control
	// block (SGCB) for this LN. Only valid on LLN0.
	SettingGroup *SettingGroupDef
}

// LogDef defines a log control block in the server model.
type LogDef struct {
	// Name is the log name (e.g. "log1").
	Name string
}

// SettingGroupDef defines a setting group control block in the
// server model. It mirrors the SCL SettingControl element.
type SettingGroupDef struct {
	// NumOfSGs is the total number of setting groups.
	NumOfSGs uint8

	// ActSG is the initially active setting group (1-based).
	ActSG uint8

	// ResvTms is the reservation timeout in seconds. 0 means no
	// timeout (the edit reservation does not expire).
	ResvTms uint16
}

// DataObject represents a data object or sub-data object within the
// server model. It contains either nested children (for structured
// DOs) or leaf data attributes.
type DataObject struct {
	// Name is the data object name (e.g. "Mod", "Health").
	Name string

	// CDC is the Common Data Class (e.g. "SPS", "DPC", "INS").
	CDC string

	// Children are nested data objects or sub-data objects.
	Children []DataObject

	// Attributes are the leaf data attributes of this DO.
	Attributes []DataAttribute
}

// DataAttribute represents a leaf data attribute in the server model.
type DataAttribute struct {
	// Name is the attribute name (e.g. "stVal", "q", "t").
	Name string

	// FC is the functional constraint (e.g. "ST", "MX", "CF").
	FC string

	// BType is the basic type (e.g. "BOOLEAN", "INT32", "Enum",
	// "Quality", "Timestamp", "VisString64", "Struct").
	BType string

	// EnumValues holds the valid ordinal values for Enum-typed
	// attributes. Populated from the SCL EnumType during model
	// expansion. Nil for non-Enum types.
	EnumValues []int

	// EnumNames maps SCL enumeration value names to their ordinals for
	// Enum-typed attributes. Enables DAI values specified as strings
	// (e.g. "direct-with-normal-security") to be resolved to integers.
	// Populated alongside EnumValues. Nil for non-Enum types.
	EnumNames map[string]int

	// InitialValue, when non-empty, is the initial value string
	// from the SCL DAI Val element.
	InitialValue string

	// Overridden is true when the InitialValue was set by a
	// DOI/DAI/SDI override rather than the template default.
	Overridden bool

	// Children are sub-attributes for structured types (BType="Struct").
	Children []DataAttribute
}

// DataSetDef defines a dataset in the server model.
type DataSetDef struct {
	// Name is the dataset name (e.g. "dsEvents").
	Name string

	// Members are the FCDAs that make up this dataset.
	Members []DataSetMemberDef
}

// DataSetMemberDef defines a single member (FCDA) of a server-side dataset.
type DataSetMemberDef struct {
	// LDInst is the logical device instance name. Empty means the
	// enclosing LD.
	LDInst string

	// LNName is the full logical node name (prefix+class+inst).
	LNName string

	// DOPath is the dot-separated data object path (e.g. "Mod.stVal").
	DOPath string

	// FC is the functional constraint.
	FC string
}

// ReportDef defines a report control block in the server model.
type ReportDef struct {
	// Name is the RCB name (e.g. "brcbEvents01").
	Name string

	// RptID is the report ID. Empty means auto-derived.
	RptID string

	// DatSet is the dataset name referenced by this RCB.
	DatSet string

	// ConfRev is the configuration revision.
	ConfRev uint32

	// Buffered indicates BRCB (true) vs URCB (false).
	Buffered bool

	// BufTime is the buffer time in milliseconds.
	BufTime uint32

	// IntgPd is the integrity period in milliseconds.
	IntgPd uint32

	// TrgOps specifies which trigger conditions are enabled.
	TrgOps TrgOpsDef

	// OptFlds specifies which optional fields are included.
	OptFlds OptFieldsDef
}

// TrgOpsDef holds trigger option flags for a report control block.
type TrgOpsDef struct {
	Dchg   bool
	Qchg   bool
	Dupd   bool
	Period bool
	GI     bool
}

// OptFieldsDef holds optional-field flags for a report control block.
type OptFieldsDef struct {
	SeqNum     bool
	TimeStamp  bool
	DataSet    bool
	ReasonCode bool
	DataRef    bool
	EntryID    bool
	ConfigRef  bool
	BufOvfl    bool
}

// Validate checks the model for internal consistency. It returns a list
// of validation errors. An empty return means the model is valid.
//
// Limitation: dataset and report cross-references are validated only
// within the same logical node. Cross-LN dataset references (e.g. a
// report in LLN0 referencing a dataset in GGIO1) are flagged as
// errors by this implementation even though IEC 61850 allows them.
// This restriction matches the current server runtime, which does
// not support cross-LN dataset resolution.
func (m *Model) Validate() []error {
	var errs []error

	if len(m.LogicalDevices) == 0 {
		errs = append(errs, fmt.Errorf("model: no logical devices"))
	}

	ldNames := make(map[string]bool)
	for i := range m.LogicalDevices {
		ld := &m.LogicalDevices[i]
		if ld.Name == "" {
			errs = append(errs, fmt.Errorf("model: logical device %d: empty name", i))
		}
		if ldNames[ld.Name] {
			errs = append(errs, fmt.Errorf("model: duplicate logical device name %q", ld.Name))
		}
		ldNames[ld.Name] = true

		errs = append(errs, validateLD(ld)...)
	}

	return errs
}

func validateLD(ld *LogicalDevice) []error {
	var errs []error
	lnNames := make(map[string]bool)
	hasLLN0 := false

	for j := range ld.LogicalNodes {
		ln := &ld.LogicalNodes[j]
		if ln.Name == "" {
			errs = append(errs, fmt.Errorf("model: LD %q: logical node %d: empty name", ld.Name, j))
		}
		if lnNames[ln.Name] {
			errs = append(errs, fmt.Errorf("model: LD %q: duplicate logical node %q", ld.Name, ln.Name))
		}
		lnNames[ln.Name] = true
		if ln.LNClass == "LLN0" {
			hasLLN0 = true
		}
		if ln.LNClass == "" {
			errs = append(errs, fmt.Errorf("model: LD %q LN %q: empty LNClass", ld.Name, ln.Name))
		}

		errs = append(errs, validateDOs(ld.Name, ln.Name, ln.DataObjects)...)

		dsNames := make(map[string]bool)
		for _, ds := range ln.DataSets {
			if ds.Name == "" {
				errs = append(errs, fmt.Errorf("model: LD %q LN %q: dataset with empty name", ld.Name, ln.Name))
			}
			if dsNames[ds.Name] {
				errs = append(errs, fmt.Errorf("model: LD %q LN %q: duplicate dataset %q", ld.Name, ln.Name, ds.Name))
			}
			dsNames[ds.Name] = true
			if len(ds.Members) == 0 {
				errs = append(errs, fmt.Errorf("model: LD %q LN %q: dataset %q has no members", ld.Name, ln.Name, ds.Name))
			}
			for k, m := range ds.Members {
				if m.LNName == "" {
					errs = append(errs, fmt.Errorf("model: LD %q LN %q: dataset %q member %d: empty LNName", ld.Name, ln.Name, ds.Name, k))
				}
				if m.DOPath == "" {
					errs = append(errs, fmt.Errorf("model: LD %q LN %q: dataset %q member %d: empty DOPath", ld.Name, ln.Name, ds.Name, k))
				}
				if m.FC == "" {
					errs = append(errs, fmt.Errorf("model: LD %q LN %q: dataset %q member %d: empty FC", ld.Name, ln.Name, ds.Name, k))
				}
			}
		}

		if ln.SettingGroup != nil && ln.SettingGroup.NumOfSGs == 0 {
			errs = append(errs, fmt.Errorf("model: LD %q LN %q: SGCB NumOfSGs must be >= 1", ld.Name, ln.Name))
		}

		for _, logDef := range ln.Logs {
			if logDef.Name == "" {
				errs = append(errs, fmt.Errorf("model: LD %q LN %q: log with empty name", ld.Name, ln.Name))
			}
		}

		rptNames := make(map[string]bool)
		for _, rpt := range ln.Reports {
			if rpt.Name == "" {
				errs = append(errs, fmt.Errorf("model: LD %q LN %q: report with empty name", ld.Name, ln.Name))
			}
			if rptNames[rpt.Name] {
				errs = append(errs, fmt.Errorf("model: LD %q LN %q: duplicate report %q", ld.Name, ln.Name, rpt.Name))
			}
			rptNames[rpt.Name] = true
			if rpt.DatSet == "" {
				errs = append(errs, fmt.Errorf("model: LD %q LN %q: report %q has empty DatSet", ld.Name, ln.Name, rpt.Name))
			} else if !dsNames[rpt.DatSet] {
				errs = append(errs, fmt.Errorf("model: LD %q LN %q: report %q references non-existent dataset %q", ld.Name, ln.Name, rpt.Name, rpt.DatSet))
			}
		}
	}

	if !hasLLN0 {
		errs = append(errs, fmt.Errorf("model: LD %q: missing mandatory LLN0 logical node", ld.Name))
	}

	return errs
}

func validateDOs(ldName, lnName string, dos []DataObject, parentPath ...string) []error {
	var errs []error
	names := make(map[string]bool)
	for _, do := range dos {
		fullPath := append(append([]string(nil), parentPath...), do.Name)
		pathStr := fmt.Sprintf("%s.%s", lnName, joinPath(fullPath))

		if names[do.Name] {
			errs = append(errs, fmt.Errorf("model: LD %q %s: duplicate DO %q", ldName, pathStr, do.Name))
		}
		names[do.Name] = true

		attrNames := make(map[string]bool)
		for _, attr := range do.Attributes {
			if attrNames[attr.Name] {
				errs = append(errs, fmt.Errorf("model: LD %q %s: duplicate DA %q", ldName, pathStr, attr.Name))
			}
			attrNames[attr.Name] = true
			if attr.FC == "" {
				errs = append(errs, fmt.Errorf("model: LD %q %s DA %q: empty FC", ldName, pathStr, attr.Name))
			}
			if attr.BType == "" && len(attr.Children) == 0 {
				errs = append(errs, fmt.Errorf("model: LD %q %s DA %q: empty BType", ldName, pathStr, attr.Name))
			}
		}

		errs = append(errs, validateDOs(ldName, lnName, do.Children, fullPath...)...)
	}
	return errs
}

func joinPath(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "."
		}
		result += p
	}
	return result
}
