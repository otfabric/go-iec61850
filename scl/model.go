// Package scl provides parsing and inspection of IEC 61850 SCL
// (Substation Configuration Language) files.
//
// SCL is an XML schema defined in IEC 61850-6 used to describe the
// configuration of IEC 61850 systems, including IEDs (Intelligent
// Electronic Devices), logical devices, logical nodes, data objects,
// data sets, and report control blocks.
//
// This package parses SCD, ICD, CID, and IID file formats into a
// Go-native model that can be queried, flattened for CLI/CSV output,
// or used for runtime validation against a live server.
package scl

// SCL is the root element of an SCL configuration file.
type SCL struct {
	// Metadata holds parse-time information about the document
	// (schema version, document kind, vendor namespaces).
	// It is populated by ParseBytes / ParseFileOpts.
	Metadata *DocumentMetadata

	// Header contains version and revision metadata.
	Header Header

	// Substations contains substation topology definitions.
	Substations []Substation

	// Communication contains network and addressing information.
	Communication *Communication

	// IEDs is the list of Intelligent Electronic Devices defined in
	// the SCL file.
	IEDs []IED

	// DataTypeTemplates contains the type definitions (LNodeType,
	// DOType, DAType, EnumType) referenced by data attributes.
	DataTypeTemplates DataTypeTemplates
}

// DocumentMetadata captures information about the source document
// that is available after parsing.
type DocumentMetadata struct {
	Version           VersionInfo
	Kind              DocumentKind
	OriginalNamespace string
	VendorNamespaces  []string
}

// Private represents a vendor-specific Private element in SCL.
// These are preserved during parsing so that roundtrip or
// inspection tooling can access vendor extensions.
type Private struct {
	Type     string
	Source   string
	InnerXML string
}

// Header contains SCL file metadata.
type Header struct {
	ID       string
	Version  string
	Revision string
}

// IED represents an Intelligent Electronic Device.
type IED struct {
	Name         string
	Desc         string
	Manufacturer string
	Type         string
	Services     *Services
	AccessPoints []AccessPoint
	Private      []Private
}

// AccessPoint represents a communication access point of an IED.
type AccessPoint struct {
	Name    string
	Server  *Server
	Private []Private
}

// Server represents the MMS server within an access point.
type Server struct {
	LDevices []LDevice
}

// LDevice represents a logical device.
type LDevice struct {
	Inst    string
	Desc    string
	LN0     *LN
	LNs     []LN
	Private []Private
}

// LN represents a logical node (both LN0 and regular LN).
type LN struct {
	Prefix  string
	LNClass string
	Inst    string
	LNType  string
	Desc    string

	DOIs           []DOI
	DataSets       []DataSet
	Reports        []ReportControl
	GSEControls    []GSEControl
	SMVControls    []SMVControl
	Logs           []Log
	SettingControl *SettingControl
	Private        []Private
}

// SettingControl represents the SCL SettingControl element that
// defines setting group parameters for a logical node (LLN0).
type SettingControl struct {
	NumOfSGs uint8
	ActSG    uint8
	ResvTms  uint16
}

// DOI represents a Data Object Instance within a logical node.
type DOI struct {
	Name string
	Desc string
	DAIs []DAI
	SDIs []SDI
}

// SDI represents a Sub-Data Instance, used for nested structured
// data objects.
type SDI struct {
	Name string
	DAIs []DAI
	SDIs []SDI
}

// DAI represents a Data Attribute Instance with an optional value
// override.
type DAI struct {
	Name  string
	SAddr string
	Val   string
}

// DataSet represents a data set defined in a logical node.
type DataSet struct {
	Name  string
	Desc  string
	FCDAs []FCDA
}

// FCDA (Functionally Constrained Data Attribute) identifies a single
// data attribute included in a data set.
type FCDA struct {
	LDInst  string
	Prefix  string
	LNClass string
	LNInst  string
	DOName  string
	DAName  string
	FC      string
}

// ReportControl represents a report control block definition.
type ReportControl struct {
	Name     string
	Desc     string
	RptID    string
	DatSet   string
	ConfRev  uint32
	Buffered bool
	BufTime  uint32
	IntgPd   uint32

	TrgOps    TrgOps
	OptFields OptFields
}

// TrgOps contains the trigger options for a report control block.
type TrgOps struct {
	Dchg   bool
	Qchg   bool
	Dupd   bool
	Period bool
	GI     bool
}

// OptFields contains the optional field flags for a report control
// block.
type OptFields struct {
	SeqNum     bool
	TimeStamp  bool
	DataSet    bool
	ReasonCode bool
	DataRef    bool
	EntryID    bool
	ConfigRef  bool
	BufOvfl    bool
}

// GSEControl represents a GOOSE control block definition.
type GSEControl struct {
	Name      string
	Desc      string
	AppID     string
	Type      string
	DatSet    string
	ConfRev   uint32
	FixedOffs bool
}

// SMVControl represents a Sampled Values control block definition.
type SMVControl struct {
	Name      string
	Desc      string
	SmvID     string
	DatSet    string
	ConfRev   uint32
	SmpRate   uint32
	NofASDU   uint32
	Multicast bool
}

// Log represents a log control block definition.
type Log struct {
	Name string
	Desc string
}

// --- Data type templates ---

// DataTypeTemplates holds the type definitions section of an SCL file.
type DataTypeTemplates struct {
	LNodeTypes []LNodeType
	DOTypes    []DOType
	DATypes    []DAType
	EnumTypes  []EnumType
}

// LNodeType defines the structure of a logical node type.
type LNodeType struct {
	ID      string
	LNClass string
	Desc    string
	DOs     []DO
}

// DO defines a data object within a logical node type.
type DO struct {
	Name string
	Type string
	Desc string
}

// DOType defines the structure of a data object type.
type DOType struct {
	ID   string
	CDC  string
	Desc string
	DAs  []DA
	SDOs []SDO
}

// SDO defines a sub-data object within a data object type.
type SDO struct {
	Name string
	Type string
	Desc string
}

// DA defines a data attribute within a data object type.
type DA struct {
	Name  string
	FC    string
	BType string
	Type  string
	Desc  string
	Count int
	Val   string
}

// DAType defines the structure of a constructed data attribute type.
type DAType struct {
	ID   string
	Desc string
	BDAs []BDA
}

// BDA defines a basic data attribute within a constructed type.
type BDA struct {
	Name  string
	BType string
	Type  string
	Desc  string
	Count int
	Val   string
}

// EnumType defines an enumeration type.
type EnumType struct {
	ID   string
	Desc string
	Vals []EnumVal
}

// EnumVal is a single value in an enumeration type.
type EnumVal struct {
	Ord   int
	Value string
}

// --- Substation topology ---

// Substation represents a substation in the SCL topology.
type Substation struct {
	Name          string
	Desc          string
	LNodes        []LNode
	VoltageLevels []VoltageLevel
}

// VoltageLevel represents a voltage level within a substation.
type VoltageLevel struct {
	Name    string
	Desc    string
	Voltage string
	LNodes  []LNode
	Bays    []Bay
}

// Bay represents a bay within a voltage level.
type Bay struct {
	Name                 string
	Desc                 string
	LNodes               []LNode
	ConductingEquipments []ConductingEquipment
}

// ConductingEquipment represents a piece of conducting equipment
// (circuit breaker, disconnector, etc.) within a bay.
type ConductingEquipment struct {
	Name string
	Type string
	Desc string
}

// LNode represents a reference from the substation topology to a
// logical node inside an IED. It links physical equipment (bays,
// voltage levels) to their controlling IED logical nodes.
type LNode struct {
	IEDName string
	LDInst  string
	Prefix  string
	LNClass string
	LNInst  string
	LNType  string
	Desc    string
}

// --- Communication ---

// Communication contains the network configuration section.
type Communication struct {
	SubNetworks []SubNetwork
}

// SubNetwork represents a communication sub-network.
type SubNetwork struct {
	Name         string
	Desc         string
	Type         string
	ConnectedAPs []ConnectedAP
}

// ConnectedAP represents a connected access point within a sub-network.
type ConnectedAP struct {
	IEDName string
	APName  string
	Address []P
	GSEs    []GSEAddress
	SMVs    []SMVAddress
}

// P represents a communication parameter (address element).
type P struct {
	Type  string
	Value string
}

// GSEAddress contains GSE (GOOSE) addressing information.
type GSEAddress struct {
	LDInst  string
	CBName  string
	Address []P
	MinTime string
	MaxTime string
}

// SMVAddress contains Sampled Values addressing information.
type SMVAddress struct {
	LDInst  string
	CBName  string
	Address []P
}

// --- Services ---

// Services describes the IEC 61850 services supported by an IED.
type Services struct {
	DynAssociation          bool
	GetDirectory            bool
	GetDataObjectDefinition bool
	GetDataSetValue         bool
	DataSetDirectory        bool
	ReadWrite               bool
	GetCBValues             bool
	FileHandling            bool

	ConfDataSet    *ConfDataSet
	ConfReportCtrl *ConfReportControl
	ReportSettings *ReportSettings
	ConfLNs        *ConfLNs
	GOOSE          *GOOSEService
	SMVsc          *SMVService
}

// ConfDataSet describes dataset configuration capabilities.
type ConfDataSet struct {
	Max           int
	MaxAttributes int
	Modify        bool
}

// ConfReportControl describes report control configuration capabilities.
type ConfReportControl struct {
	Max     int
	BufMode string
}

// ReportSettings describes report setting configurability.
type ReportSettings struct {
	CBName    string
	DatSet    string
	RptID     string
	OptFields string
	BufTime   string
	TrgOps    string
	IntgPd    string
}

// ConfLNs describes logical node configuration constraints.
type ConfLNs struct {
	FixPrefix bool
	FixLnInst bool
}

// GOOSEService describes GOOSE capabilities.
type GOOSEService struct {
	Max       int
	FixedOffs bool
}

// SMVService describes Sampled Values capabilities.
type SMVService struct {
	Max int
}
