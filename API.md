# API Reference

Complete public API reference for `go-iec61850`.

---

## Table of Contents

- [Connection](#connection)
- [Browse and Model Discovery](#browse-and-model-discovery)
- [Read and Write](#read-and-write)
- [Datasets](#datasets)
- [Reports](#reports)
- [Control](#control)
- [Setting Groups](#setting-groups)
- [Journals](#journals)
- [Files](#files)
- [Caching](#caching)
- [Values and Types](#values-and-types)
- [Object References](#object-references)
- [Functional Constraints](#functional-constraints)
- [Server (experimental)](#server-experimental)
- [SCL Package](#scl-package)
- [SCL Index Package](#scl-index-package)
- [SCL Validate Package](#scl-validate-package)
- [Errors](#errors)

---

## Connection

### Dial

```go
func Dial(ctx context.Context, addr string, opts DialOptions) (*Client, error)
```

Connects to an IEC 61850 MMS server at the given address.

### NewClient

```go
func NewClient(mmsClient *mms.Client, opts ClientOptions) (*Client, error)
```

Wraps an existing `go-mms` client with IEC 61850 semantics.

### Client lifecycle

```go
func (c *Client) Close(ctx context.Context) error
func (c *Client) Abort(ctx context.Context) error
func (c *Client) MMS() *mms.Client
```

### DialOptions

```go
type DialOptions struct {
    MMS        mms.DialOptions
    Logger     *slog.Logger
    Strictness StrictnessOptions
    Cache      CacheStrategy
}
```

### ClientOptions

```go
type ClientOptions struct {
    Logger     *slog.Logger
    Strictness StrictnessOptions
    Cache      CacheStrategy
}
```

### CacheStrategy

```go
type CacheStrategy int

const (
    CacheNone     CacheStrategy = iota // no caching
    CacheExplicit                       // cache on explicit RefreshCache call
    CacheLazy                           // cache on first access per LD
)
```

### StrictnessOptions

```go
type StrictnessOptions struct {
    RejectUnknownFC        bool
    VerifyReportCandidates bool
}
```

---

## Browse and Model Discovery

```go
func (c *Client) ListLogicalDevices(ctx context.Context) ([]LogicalDevice, error)
func (c *Client) ListLogicalNodes(ctx context.Context, ld string) ([]LogicalNode, error)
func (c *Client) ListDataObjects(ctx context.Context, ld, ln string) ([]DataObject, error)
func (c *Client) ListChildren(ctx context.Context, ref Ref) ([]BrowseNode, error)
func (c *Client) Tree(ctx context.Context) (*ModelNode, error)
func (c *Client) TreeWithOptions(ctx context.Context, opts TreeOptions) (*ModelNode, error)
func (c *Client) FindPaths(ctx context.Context, query FindQuery) ([]Ref, error)
func (c *Client) GetVariableType(ctx context.Context, ref Ref) (*mms.TypeSpec, error)
```

### LogicalDevice

```go
type LogicalDevice struct {
    Name string
}
```

### LogicalNode

```go
type LogicalNode struct {
    Name string
    LD   string
}

func (ln LogicalNode) Ref() Ref
```

### DataObject

```go
type DataObject struct {
    Name      string
    Reference Ref
    FC        FunctionalConstraint
    Children  []DataObject
}
```

### ModelNode

Recursive tree representation of the full server model.

```go
type ModelNode struct {
    Name      string
    Reference Ref
    FC        FunctionalConstraint
    FCs       []FunctionalConstraint
    Type      *mms.TypeSpec
    Children  []*ModelNode
}
```

### BrowseNode

```go
type BrowseNode struct {
    Name      string
    Reference Ref
}
```

### TreeOptions

```go
type TreeOptions struct {
    LDFilter   string
    MaxDepth   int
    IncludeFCs bool
}
```

### FindQuery

```go
type MatchMode int

const (
    MatchGlob  MatchMode = iota
    MatchRegex
)

type FindQuery struct {
    Pattern   string
    MatchMode MatchMode
    FC        FunctionalConstraint
    LDFilter  string
    MaxDepth  int
}
```

---

## Read and Write

### Single operations

```go
func (c *Client) Read(ctx context.Context, ref Ref) (*Value, error)
func (c *Client) ReadRaw(ctx context.Context, ref Ref) (*mms.Value, error)
func (c *Client) ReadComponent(ctx context.Context, ref Ref, component string) (*Value, error)
func (c *Client) Write(ctx context.Context, ref Ref, value *mms.Value) error
```

### Bulk operations

```go
func (c *Client) ReadMultiple(ctx context.Context, refs []Ref) ([]ReadResult, error)
func (c *Client) WriteMultiple(ctx context.Context, requests []WriteRequest) ([]WriteResult, error)
func HasDuplicateRefs(refs []Ref) bool
```

### ReadResult

```go
type ReadResult struct {
    Ref   Ref
    Value *Value
    Err   error
}
```

### WriteRequest / WriteResult

```go
type WriteRequest struct {
    Ref   Ref
    Value *mms.Value
}

type WriteResult struct {
    Ref     Ref
    Success bool
    Err     error
}
```

---

## Datasets

```go
func (c *Client) ListDataSets(ctx context.Context, ld string) ([]string, error)
func (c *Client) GetDataSet(ctx context.Context, ld, dsName string) (*DataSet, error)
func (c *Client) ReadDataSet(ctx context.Context, ld, dsName string) ([]DataSetValue, error)
func (c *Client) CreateDataSet(ctx context.Context, ld, dsName string, members []DataSetMember) error
func (c *Client) DeleteDataSet(ctx context.Context, ld, dsName string) error
```

### DataSet

```go
type DataSet struct {
    Reference string
    Deletable bool
    Members   []DataSetMember
}
```

### DataSetMember

```go
type DataSetMember struct {
    Ref      Ref
    DomainID string
    ItemID   string
}
```

### DataSetValue

```go
type DataSetValue struct {
    Member DataSetMember
    Value  *Value
    Err    error
}
```

---

## Reports

### RCB operations

```go
func (c *Client) ListReports(ctx context.Context, ld string) ([]string, error)
func (c *Client) ListReportsVerified(ctx context.Context, ld string) ([]string, error)
func (c *Client) GetReportControlBlock(ctx context.Context, ld, rcbItemID string) (*ReportControlBlock, error)
func (c *Client) SetReportControlBlock(ctx context.Context, ld, rcbItemID string, update RCBUpdate) error
func (c *Client) TriggerGI(ctx context.Context, ld, rcbItemID string) error
func (c *Client) ReserveURCB(ctx context.Context, ld, rcbItemID string) error
func (c *Client) ReleaseURCB(ctx context.Context, ld, rcbItemID string) error
```

### Subscriptions

```go
func (c *Client) SubscribeReport(ctx context.Context, rptID string, opts SubscribeReportOptions) (*ReportSubscription, error)
```

### ReportControlBlock

```go
type ReportControlBlock struct {
    Reference string
    Type      RCBType
    RptID     string
    RptEna    bool
    DatSet    string
    ConfRev   uint32
    OptFlds   OptFlds
    BufTm     uint32
    SqNum     uint32
    TrgOps    TrgOps
    IntgPd    uint32
    GI        bool
    Resv      bool
    ResvTms   int32
    EntryID   []byte
    PurgeBuf  bool
    Owner     []byte
}
```

### RCBType

```go
type RCBType int

const (
    RCBBuffered   RCBType = iota
    RCBUnbuffered
)

func (t RCBType) String() string
func (t RCBType) FC() FunctionalConstraint
```

### RCBUpdate

```go
type RCBFieldMask uint32

const (
    RCBFieldRptID    RCBFieldMask = 1 << iota
    RCBFieldRptEna
    RCBFieldDatSet
    RCBFieldOptFlds
    RCBFieldBufTm
    RCBFieldTrgOps
    RCBFieldIntgPd
    RCBFieldGI
    RCBFieldResv
    RCBFieldPurgeBuf
    RCBFieldEntryID
    RCBFieldResvTms
)

type RCBUpdate struct {
    Fields   RCBFieldMask
    RptID    string
    RptEna   bool
    DatSet   string
    OptFlds  OptFlds
    BufTm    uint32
    TrgOps   TrgOps
    IntgPd   uint32
    GI       bool
    Resv     bool
    PurgeBuf bool
    EntryID  []byte
    ResvTms  int32
}
```

### OptFlds

```go
type OptFlds uint16

const (
    OptFldSeqNum       OptFlds = 1 << iota
    OptFldTimeStamp
    OptFldReasonCode
    OptFldDataSet
    OptFldDataRef
    OptFldBufOvfl
    OptFldEntryID
    OptFldConfRev
    OptFldSegmentation
)

func (o OptFlds) Has(flag OptFlds) bool
func (o OptFlds) String() string
```

### TrgOps

```go
type TrgOps uint8

const (
    TrgOpDataChanged    TrgOps = 1 << iota
    TrgOpQualityChanged
    TrgOpDataUpdate
    TrgOpIntegrity
    TrgOpGI
)

func (t TrgOps) Has(flag TrgOps) bool
func (t TrgOps) String() string
```

### SubscribeReportOptions

```go
type OverflowPolicy int

const (
    OverflowDropNewest OverflowPolicy = iota
    OverflowDropOldest
    OverflowBlock
    OverflowCallback
)

type RptMatchMode int

const (
    RptMatchExact RptMatchMode = iota
    RptMatchGlob
)

type SubscribeReportOptions struct {
    QueueSize      int
    OverflowPolicy OverflowPolicy
    OnOverflow     func(*ReportIndication)
    MatchMode      RptMatchMode
    CloneReports   bool
    AutoEnable     bool
    GIOnSubscribe  bool
    ReserveURCB    bool
    LD             string
    RCBItemID      string
}
```

### ReportSubscription

```go
type ReportSubscription struct { /* unexported */ }

func (s *ReportSubscription) Reports() <-chan *ReportIndication
func (s *ReportSubscription) Close() error
```

### ReportIndication

```go
type ReportIndication struct {
    RptID          string
    OptFlds        OptFlds
    SeqNum         uint32
    SubSeqNum      uint32
    MoreSegments   bool
    DatSet         string
    BufOvfl        bool
    EntryID        []byte
    ConfRev        uint32
    Timestamp      time.Time
    Inclusion      []bool
    DataReferences []string
    Values         []*Value
    ReasonCodes    []ReasonCode
}
```

### ReasonCode

```go
type ReasonCode uint8

const (
    ReasonDataChanged    ReasonCode = 1 << iota
    ReasonQualityChanged
    ReasonDataUpdate
    ReasonIntegrity
    ReasonGI
)

func (r ReasonCode) String() string
```

---

## Control

### Client methods

```go
func (c *Client) Operate(ctx context.Context, ref Ref, params OperateParams) error
func (c *Client) Select(ctx context.Context, ref Ref) (string, error)
func (c *Client) SelectWithValue(ctx context.Context, ref Ref, params OperateParams) error
func (c *Client) Cancel(ctx context.Context, ref Ref, params CancelParams) error
func (c *Client) ReadCtlModel(ctx context.Context, ref Ref) (CtlModel, error)
func (c *Client) ReadLastApplError(ctx context.Context, ref Ref) (*LastApplError, error)
```

### CtlModel

```go
type CtlModel int

const (
    CtlModelStatusOnly     CtlModel = 0
    CtlModelDirectNormal   CtlModel = 1
    CtlModelSBONormal      CtlModel = 2
    CtlModelDirectEnhanced CtlModel = 3
    CtlModelSBOEnhanced    CtlModel = 4
)

func (m CtlModel) String() string
func (m CtlModel) IsControllable() bool
func (m CtlModel) IsSBO() bool
func (m CtlModel) IsEnhanced() bool
```

### OperateParams

```go
type OperateParams struct {
    CtlVal *mms.Value
    Origin *Origin
    CtlNum uint8
    OperTm time.Time
    Test   bool
    Check  CheckConditions
}
```

### CancelParams

```go
type CancelParams struct {
    CtlVal *mms.Value
    Origin *Origin
    CtlNum uint8
    OperTm time.Time
}
```

### Origin

```go
type OrCat int

const (
    OrCatNotSupported OrCat = iota
    OrCatBayControl
    OrCatStationControl
    OrCatRemoteControl
    OrCatAutomaticBay
    OrCatAutomaticStation
    OrCatAutomaticRemote
    OrCatMaintenance
    OrCatProcess
)

func (c OrCat) String() string

type Origin struct {
    OrCat   OrCat
    OrIdent []byte
}
```

### CheckConditions

```go
type CheckConditions uint8

const (
    CheckSynchroCheck   CheckConditions = 1 << 0
    CheckInterlockCheck CheckConditions = 1 << 1
)

func (c CheckConditions) Has(flag CheckConditions) bool
```

### AddCause

```go
type AddCause int

// Values 0–17: AddCauseUnknown through AddCauseBlockedByProcess

func (c AddCause) String() string
```

### LastApplError

```go
type LastApplError struct {
    CntrlObj string
    Error    int
    Origin   Origin
    AddCause AddCause
}
```

### Control value constructors

```go
func BoolCtlVal(v bool) *mms.Value
func IntCtlVal(v int32) *mms.Value
func FloatCtlVal(v float32) *mms.Value
func EnumCtlVal(v int32) *mms.Value
func StringCtlVal(v string) *mms.Value
func BspCtlVal(bits []byte, bitLen int) *mms.Value
func DpCtlVal(on bool) *mms.Value
```

---

## Setting Groups

### Client methods

```go
func (c *Client) GetSettingGroupInfo(ctx context.Context, ld string) (*SettingGroupInfo, error)
func (c *Client) SelectActiveSG(ctx context.Context, ld string, sg uint8) error
func (c *Client) SelectEditSG(ctx context.Context, ld string, sg uint8) error
func (c *Client) ConfirmEditSG(ctx context.Context, ld string) error
func (c *Client) GetEditSGValue(ctx context.Context, ref Ref) (*Value, error)
func (c *Client) SetEditSGValue(ctx context.Context, ref Ref, value *mms.Value) error
func (c *Client) GetActiveSGValue(ctx context.Context, ref Ref) (*Value, error)
```

### SettingGroupInfo

```go
type SettingGroupInfo struct {
    NumOfSGs uint8
    ActSG    uint8
    EditSG   uint8
    CnfEdit  bool
    ResvTms  uint16
}
```

---

## Journals

```go
func (c *Client) ListJournals(ctx context.Context, ld string) ([]string, error)
func (c *Client) ReadJournal(ctx context.Context, ld, journal string, start, stop time.Time) (*JournalReadResult, error)
func (c *Client) ReadJournalAfter(ctx context.Context, ld, journal string, afterTime time.Time, afterID []byte) (*JournalReadResult, error)
func (c *Client) ReadJournalAll(ctx context.Context, ld, journal string, start, stop time.Time) ([]JournalEntry, error)
func (c *Client) ReadJournalAfterAll(ctx context.Context, ld, journal string, afterTime time.Time, afterID []byte) ([]JournalEntry, error)
```

### JournalEntry

```go
type JournalEntry struct {
    EntryID        []byte
    OccurrenceTime time.Time
    Variables      []JournalVariable
}
```

### JournalVariable

```go
type JournalVariable struct {
    Tag   string
    Value *Value
}
```

### JournalReadResult

```go
type JournalReadResult struct {
    Entries     []JournalEntry
    MoreFollows bool
}
```

---

## Files

```go
func (c *Client) ListFiles(ctx context.Context, pattern string) ([]FileEntry, error)
func (c *Client) ReadFile(ctx context.Context, fileName string) ([]byte, *FileEntry, error)
func (c *Client) DownloadFile(ctx context.Context, fileName string, w io.Writer) (*FileEntry, error)
func (c *Client) DeleteFile(ctx context.Context, fileName string) error
func (c *Client) RenameFile(ctx context.Context, currentName, newName string) error
func (c *Client) ObtainFile(ctx context.Context, sourceFile, destinationFile string) error
func (c *Client) GetFileAttributes(ctx context.Context, fileName string) (*FileEntry, error)
```

### FileEntry

```go
type FileEntry struct {
    Name         string
    Size         int64
    LastModified time.Time
}
```

---

## Caching

```go
func (c *Client) RefreshCache(ctx context.Context) error
func (c *Client) RefreshLDCache(ctx context.Context, ld string) error
func (c *Client) InvalidateCache()
func (c *Client) InvalidateLDCache(ld string)
```

Cache behavior is controlled by `CacheStrategy` in `DialOptions` / `ClientOptions`.

---

## Values and Types

### Value

Wraps an MMS value with IEC 61850 typed accessors.

```go
type Value struct { /* unexported */ }

// Constructors
func NewValue(v *mms.Value) *Value
func BoolValue(b bool) *Value
func IntValue(i int64) *Value
func UintValue(u uint64) *Value
func FloatValue(f float64) *Value
func StringValue(s string) *Value
func OctetStringValue(data []byte) *Value
func QualityValue(q Quality) *Value
func TimestampValue(ts Timestamp) *Value
func StructureValue(elements []*Value) (*Value, error)
func UnsafeStructureValue(elements []*Value) *Value
func ArrayValue(elements []*Value) (*Value, error)
func UnsafeArrayValue(elements []*Value) *Value

// Accessors
func (v *Value) MMS() *mms.Value
func (v *Value) Type() mms.ValueType
func (v *Value) Bool() (bool, error)
func (v *Value) Int32() (int32, error)
func (v *Value) Int64() (int64, error)
func (v *Value) Uint32() (uint32, error)
func (v *Value) Uint64() (uint64, error)
func (v *Value) Float32() (float32, error)
func (v *Value) Float64() (float64, error)
func (v *Value) VisibleString() (string, error)
func (v *Value) MmsString() (string, error)
func (v *Value) OctetString() ([]byte, error)
func (v *Value) BitString() ([]byte, error)
func (v *Value) Quality() (Quality, error)
func (v *Value) Timestamp() (Timestamp, error)
func (v *Value) IsStructure() bool
func (v *Value) IsArray() bool
func (v *Value) Elements() ([]*Value, error)
func (v *Value) String() string
```

### Quality

```go
type Quality uint16

type Validity int

const (
    ValidityGood         Validity = 0
    ValidityReserved     Validity = 1
    ValidityInvalid      Validity = 2
    ValidityQuestionable Validity = 3
)

const (
    QualityOverflow          Quality = 1 << 2
    QualityOutOfRange        Quality = 1 << 3
    QualityBadReference      Quality = 1 << 4
    QualityOscillatory       Quality = 1 << 5
    QualityFailure           Quality = 1 << 6
    QualityOldData           Quality = 1 << 7
    QualityInconsistent      Quality = 1 << 8
    QualityInaccurate        Quality = 1 << 9
    QualitySourceSubstituted Quality = 1 << 10
    QualityTest              Quality = 1 << 11
    QualityOperatorBlocked   Quality = 1 << 12
)

func DecodeQuality(v *mms.Value) (Quality, error)
func EncodeQuality(q Quality) *mms.Value

func (q Quality) Validity() Validity
func (q Quality) IsGood() bool
func (q Quality) Has(flag Quality) bool
func (q Quality) WithValidity(v Validity) Quality
func (q Quality) String() string
```

### Timestamp

```go
type Timestamp struct {
    Time    time.Time
    Quality TimeQuality
}

type TimeQuality struct {
    LeapSecondKnown      bool
    ClockFailure         bool
    ClockNotSynchronized bool
    TimeAccuracy         int
}

func DecodeTimestamp(v *mms.Value) (Timestamp, error)
func EncodeTimestamp(ts Timestamp) *mms.Value

func (ts Timestamp) IsZero() bool
func (ts Timestamp) String() string
```

---

## Object References

IEC 61850 object references use the format `LD/LN.DO.DA[FC]`.

```go
type Ref struct {
    LD   string
    LN   string
    Path []string
    FC   FunctionalConstraint
}

func ParseRef(s string) (Ref, error)
func ParseRefStrict(s string) (Ref, error)
func RefFromMMS(domain mms.DomainID, itemID mms.ItemID) (Ref, error)

func (r Ref) Validate() error
func (r Ref) String() string
func (r Ref) ObjectReference() string
func (r Ref) WithFC(fc FunctionalConstraint) Ref
func (r Ref) Parent() (Ref, bool)
func (r Ref) Child(name string) (Ref, error)
func (r Ref) IsLD() bool
func (r Ref) IsLN() bool
func (r Ref) IsObject() bool
func (r Ref) HasPath() bool
func (r Ref) Depth() int
func (r Ref) ToMMS() (domain mms.DomainID, itemID mms.ItemID, err error)
```

The MMS wire format uses `domain = LD`, `itemID = LN$FC$DO$DA`. Conversion
is handled by `Ref.ToMMS()` and `RefFromMMS()`.

---

## Functional Constraints

```go
type FunctionalConstraint string

const (
    FCST FunctionalConstraint = "ST" // Status
    FCMX FunctionalConstraint = "MX" // Measured values
    FCSP FunctionalConstraint = "SP" // Setpoint
    FCSV FunctionalConstraint = "SV" // Substitution
    FCCF FunctionalConstraint = "CF" // Configuration
    FCDC FunctionalConstraint = "DC" // Description
    FCSG FunctionalConstraint = "SG" // Setting group
    FCSE FunctionalConstraint = "SE" // Setting group editable
    FCSR FunctionalConstraint = "SR" // Service response
    FCOR FunctionalConstraint = "OR" // Operate received
    FCBL FunctionalConstraint = "BL" // Blocking
    FCEX FunctionalConstraint = "EX" // Extended definition
    FCCO FunctionalConstraint = "CO" // Control
    FCUS FunctionalConstraint = "US" // Unicast SV
    FCMS FunctionalConstraint = "MS" // Multicast SV
    FCRP FunctionalConstraint = "RP" // Unbuffered reporting
    FCBR FunctionalConstraint = "BR" // Buffered reporting
    FCLG FunctionalConstraint = "LG" // Log
    FCGO FunctionalConstraint = "GO" // GOOSE
)

func ParseFC(s string) (FunctionalConstraint, error)
func AllFunctionalConstraints() []FunctionalConstraint

func (fc FunctionalConstraint) IsValid() bool
func (fc FunctionalConstraint) Description() string
func (fc FunctionalConstraint) String() string
```

---

## Server (experimental)

### Construction

```go
func NewServer(model *servermodel.Model, opts ServerOptions) (*Server, error)
func NewServerModelFromSCL(s *scl.SCL, iedName, apName string) (*servermodel.Model, error)
```

### ServerOptions

```go
type ServerOptions struct {
    Logger       *slog.Logger
    Identity     *ServerIdentity
    FileProvider mms.FileProvider
    Authenticate mms.Authenticator
    OnConnect    func(ConnectionEvent)
    OnDisconnect func(ConnectionEvent)
    MMS          mms.ServerOptions
}

type ServerIdentity struct {
    Vendor   string
    Model    string
    Revision string
}
```

### Server methods

```go
func (s *Server) Model() *servermodel.Model
func (s *Server) ValueStore() *servermodel.ValueStore
func (s *Server) SetValue(ctx context.Context, storeKey string, val *mms.Value)
func (s *Server) MMS() *mms.Server
func (s *Server) Serve(ctx context.Context, conn mms.Transport) error
func (s *Server) ListenAndServe(ctx context.Context, ln mms.TransportListener) error
func (s *Server) Close()
func (s *Server) Capabilities() ServiceCapabilities
func (s *Server) HandleIdentify(id ServerIdentity)
func (s *Server) HandleStatus()
```

### ServiceCapabilities

```go
type ServiceCapabilities struct {
    Variables, DataSets, Reports, Controls     bool
    SettingGroups, Journals, Files, Identify   bool
}

func (c ServiceCapabilities) String() string
```

### Report engine

```go
func (s *Server) EnableReports() *ReportEngine

type ReportEngine struct { /* unexported */ }

func (re *ReportEngine) Stop()
func (re *ReportEngine) HandleRCBWrite(ctx context.Context, ldName, rcbItemID, subfield string, val *mms.Value, conn ...*mms.ServerConn) error
func (re *ReportEngine) NotifyValueChanged(ctx context.Context, storeKey string)
```

### Control registration

```go
const DefaultSelectTimeout = 30 * time.Second

func (s *Server) RegisterControl(ldName, doRef string, ctlModel CtlModel, handler ControlHandler) error

type ControlHandler struct {
    OnSelect  func(ctx context.Context, req ControlRequest) error
    OnOperate func(ctx context.Context, req ControlRequest) error
    OnCancel  func(ctx context.Context, req ControlRequest) error
}

type ControlRequest struct {
    Ref       string
    Operation string
    CtlVal    *mms.Value
    Origin    Origin
    CtlNum    uint8
    OperTm    time.Time
    Test      bool
    Check     CheckConditions
}
```

### Setting group engine

```go
func (s *Server) EnableSettingGroups(handler SettingGroupHandler)
func (s *Server) ChangeActiveSettingGroup(ctx context.Context, ld string, sg uint8) error

type SettingGroupHandler struct {
    OnActiveSGChanged func(ctx context.Context, ld string, newSG uint8) error
    OnEditSGSelected  func(ctx context.Context, ld string, editSG uint8) error
    OnConfirmEdit     func(ctx context.Context, ld string, editSG uint8) error
}

type SettingGroupEngine struct { /* unexported */ }

func (e *SettingGroupEngine) HandleSGCBWrite(ctx context.Context, ldName, subfield string, val *mms.Value) error
func (e *SettingGroupEngine) GetActiveSettingGroup(ldName string) uint8
func (e *SettingGroupEngine) GetEditSettingGroup(ldName string) uint8
```

### Journal engine

```go
func (s *Server) EnableJournals(opts ...JournalEngineOption) *JournalEngine

type JournalEngineOption func(*JournalEngine)

func WithJournalMaxEntries(n int) JournalEngineOption
func WithJournalProvider(p *MemoryJournalProvider) JournalEngineOption

type JournalEngine struct { /* unexported */ }

func (e *JournalEngine) Provider() *MemoryJournalProvider
func (e *JournalEngine) LogEvent(domain, journal string, occTime time.Time, vars []mms.JournalVariable) []byte
func (e *JournalEngine) LogValueWrite(ctx context.Context, storeKey string, occTime time.Time)
```

### MemoryJournalProvider

```go
type MemoryJournalOption func(*MemoryJournalProvider)

func WithMaxEntries(n int) MemoryJournalOption
func NewMemoryJournalProvider(opts ...MemoryJournalOption) *MemoryJournalProvider

func (p *MemoryJournalProvider) RegisterJournal(domain, journal string)
func (p *MemoryJournalProvider) AddEntry(domain, journal string, occTime time.Time, vars []mms.JournalVariable) []byte
func (p *MemoryJournalProvider) EntryCount(domain, journal string) int
func (p *MemoryJournalProvider) ListJournals(ctx context.Context, domain string) ([]string, error)
func (p *MemoryJournalProvider) ReadTimeRange(ctx context.Context, domain, journal string, start, stop time.Time, maxEntries int) (*mms.JournalResult, error)
func (p *MemoryJournalProvider) ReadStartAfter(ctx context.Context, domain, journal string, afterID []byte, afterTime time.Time, maxEntries int) (*mms.JournalResult, error)
```

---

## SCL Package

Package `scl` parses, validates, and exports IEC 61850 SCL (Substation
Configuration Language) files. Supports schema versions 1.7, 2007B, 2007B4,
and 2007C5.

### Parsing

```go
// Primary API
func ParseBytes(data []byte, opts ParseOptions) (*Result, error)
func ParseFileOpts(path string, opts ParseOptions) (*Result, error)

// Convenience wrappers (return *SCL directly)
func Parse(r io.Reader) (*SCL, error)
func ParseFile(path string) (*SCL, error)
```

`ParseWithOptions` and `ParseFileWithOptions` are deprecated; use `ParseBytes`
and `ParseFileOpts` directly.

### ParseOptions

```go
type ParseOptions struct {
    ValidateSemantic bool         // run semantic validation after parsing
    Strict           bool         // return error if any diagnostic has error severity
    Kind             DocumentKind // override document kind detection
    MaxDiagnostics   int          // truncate diagnostics (0 = unlimited)
}
```

### Result

```go
type Result struct {
    Version     VersionInfo
    Kind        DocumentKind
    Document    *SCL
    Diagnostics []Diagnostic
}

func (r *Result) HasErrors() bool
```

### Version detection

```go
type SchemaVersion string

const (
    VersionUnknown SchemaVersion = ""
    Version17      SchemaVersion = "1.7"
    Version2007B   SchemaVersion = "2007B"
    Version2007B4  SchemaVersion = "2007B4"
    Version2007C5  SchemaVersion = "2007C5"
)

type Confidence string

const (
    ConfidenceHigh Confidence = "high"
    ConfidenceLow  Confidence = "low"
)

type VersionInfo struct {
    Schema           SchemaVersion
    Namespace        string
    Version          string   // raw "version" attribute
    Revision         string   // raw "revision" attribute
    Release          string   // raw "release" attribute
    ReleaseNum       int      // parsed numeric release; -1 = absent, 0 = malformed
    Confidence       Confidence
    Reasons          []string
    VendorNamespaces []string
}

func DetectVersion(data []byte) (VersionInfo, error)
func DetectFile(path string) (VersionInfo, error)
```

### Document kind

```go
type DocumentKind string

const (
    KindUnknown DocumentKind = "unknown"
    KindSCD     DocumentKind = "scd"
    KindCID     DocumentKind = "cid"
    KindICD     DocumentKind = "icd"
    KindIID     DocumentKind = "iid"
    KindSSD     DocumentKind = "ssd"
)

func KindFromPath(path string) DocumentKind  // infer from file extension
func DetectKind(s *SCL) DocumentKind          // infer from model content
```

### Normalized model

The root model type:

```go
type SCL struct {
    Metadata          *DocumentMetadata
    Header            Header
    Substations       []Substation
    Communication     *Communication
    IEDs              []IED
    DataTypeTemplates DataTypeTemplates
}

type DocumentMetadata struct {
    Version           VersionInfo
    Kind              DocumentKind
    OriginalNamespace string
    VendorNamespaces  []string
}

type Header struct {
    ID       string
    Version  string
    Revision string
}
```

#### IED hierarchy

```go
type IED struct {
    Name, Desc, Manufacturer, Type string
    Services                       *Services
    AccessPoints                   []AccessPoint
    Private                        []Private
}

type AccessPoint struct {
    Name    string
    Server  *Server
    Private []Private
}

type Server struct {
    LDevices []LDevice
}

type LDevice struct {
    Inst, Desc string
    LN0        *LN
    LNs        []LN
    Private    []Private
}

type LN struct {
    Prefix, LNClass, Inst, LNType, Desc string
    DOIs                                 []DOI
    DataSets                             []DataSet
    Reports                              []ReportControl
    GSEControls                          []GSEControl
    SMVControls                          []SMVControl
    Logs                                 []Log
    SettingControl                       *SettingControl
    Private                              []Private
}
```

#### Data structures within LN

```go
type DOI struct {
    Name, Desc string
    DAIs       []DAI
    SDIs       []SDI
}

type SDI struct {
    Name string
    DAIs []DAI
    SDIs []SDI
}

type DAI struct {
    Name, SAddr, Val string
}

type DataSet struct {
    Name, Desc string
    FCDAs      []FCDA
}

type FCDA struct {
    LDInst, Prefix, LNClass, LNInst, DOName, DAName, FC string
}

type ReportControl struct {
    Name, Desc, RptID, DatSet string
    ConfRev                   uint32
    Buffered                  bool
    BufTime, IntgPd           uint32
    TrgOps                    TrgOps
    OptFields                 OptFields
}

type GSEControl struct {
    Name, Desc, AppID, Type, DatSet string
    ConfRev                         uint32
    FixedOffs                       bool
}

type SMVControl struct {
    Name, Desc, SmvID, DatSet string
    ConfRev, SmpRate, NofASDU uint32
    Multicast                 bool
}

type Log struct {
    Name, Desc string
}

type SettingControl struct {
    NumOfSGs uint8
    ActSG    uint8
    ResvTms  uint16
}

type Private struct {
    Type, Source, InnerXML string
}
```

#### Data type templates

```go
type DataTypeTemplates struct {
    LNodeTypes []LNodeType
    DOTypes    []DOType
    DATypes    []DAType
    EnumTypes  []EnumType
}

type LNodeType struct {
    ID, LNClass, Desc string
    DOs               []DO
}

type DO struct {
    Name, Type, Desc string
}

type DOType struct {
    ID, CDC, Desc string
    DAs           []DA
    SDOs          []SDO
}

type SDO struct {
    Name, Type, Desc string
}

type DA struct {
    Name, FC, BType, Type, Desc string
    Count                       int
    Val                         string
}

type DAType struct {
    ID, Desc string
    BDAs     []BDA
}

type BDA struct {
    Name, BType, Type, Desc string
    Count                   int
    Val                     string
}

type EnumType struct {
    ID, Desc string
    Vals     []EnumVal
}

type EnumVal struct {
    Ord   int
    Value string
}
```

#### Substation topology

```go
type Substation struct {
    Name, Desc    string
    LNodes        []LNode
    VoltageLevels []VoltageLevel
}

type VoltageLevel struct {
    Name, Desc, Voltage string
    LNodes              []LNode
    Bays                []Bay
}

type Bay struct {
    Name, Desc           string
    LNodes               []LNode
    ConductingEquipments []ConductingEquipment
}

type ConductingEquipment struct {
    Name, Type, Desc string
}

type LNode struct {
    IEDName, LDInst, Prefix, LNClass, LNInst, LNType, Desc string
}
```

#### Communication

```go
type Communication struct {
    SubNetworks []SubNetwork
}

type SubNetwork struct {
    Name, Desc, Type string
    ConnectedAPs     []ConnectedAP
}

type ConnectedAP struct {
    IEDName, APName string
    Address         []P
    GSEs            []GSEAddress
    SMVs            []SMVAddress
}

type P struct {
    Type, Value string
}

type GSEAddress struct {
    LDInst, CBName    string
    Address           []P
    MinTime, MaxTime  string
}

type SMVAddress struct {
    LDInst, CBName string
    Address        []P
}
```

### Summary

```go
type Summary struct {
    Substations    int  `json:"substations"`
    VoltageLevels  int  `json:"voltageLevels"`
    Bays           int  `json:"bays"`
    IEDs           int  `json:"ieds"`
    AccessPoints   int  `json:"accessPoints"`
    LogicalDevices int  `json:"logicalDevices"`
    LogicalNodes   int  `json:"logicalNodes"`
    LN0Count       int  `json:"ln0Count"`
    DataSets       int  `json:"dataSets"`
    ReportControls int  `json:"reportControls"`
    LogControls    int  `json:"logControls"`
    GSEControls    int  `json:"gseControls"`
    SMVControls    int  `json:"smvControls"`
    ConnectedAPs   int  `json:"connectedAPs"`
    LNodeTypes     int  `json:"lnodeTypes"`
    DOTypes        int  `json:"doTypes"`
    DATypes        int  `json:"daTypes"`
    EnumTypes      int  `json:"enumTypes"`
    HasServices    bool `json:"hasServices"`
    PrivateCount   int  `json:"privateCount"`
}

func Summarize(s *SCL) Summary
```

### Flatten and export

```go
type FlatRow struct {
    IED, AccessPoint, LD, LN, Path, FC, BType, CDC, Desc, Status string
}

func Flatten(s *SCL) []FlatRow
func WriteCSV(w io.Writer, rows []FlatRow) error
func PrintTree(w io.Writer, s *SCL) error

type DataSetRow struct {
    IED, AccessPoint, LD, LN, DataSet, Desc string
    MemberCount                             int
}

func ExportDataSets(s *SCL) []DataSetRow

type ReportRow struct {
    IED, AccessPoint, LD, LN, Name, RptID, DatSet string
    Buffered                                       bool
    ConfRev                                        uint32
}

func ExportReports(s *SCL) []ReportRow

type GSEControlRow struct {
    IED, AccessPoint, LD, Name, AppID, Type, DatSet, Desc string
    ConfRev                                                uint32
}

func ExportGSEControls(s *SCL) []GSEControlRow

type SMVControlRow struct {
    IED, AccessPoint, LD, Name, SmvID, DatSet, Desc string
    SmpRate, NofASDU, ConfRev                       uint32
    Multicast                                       bool
}

func ExportSMVControls(s *SCL) []SMVControlRow

type ConnectedAPRow struct {
    SubNetwork, IEDName, APName, Desc string
    GSECount, SMVCount                int
}

func ExportConnectedAPs(s *SCL) []ConnectedAPRow
```

### Lookup helpers

Methods on the model types for direct element lookup:

```go
func (s *SCL) FindIED(name string) *IED
func (s *SCL) FindLDevice(inst string) *LDevice
func (ied *IED) FindLDevice(inst string) *LDevice
func (ld *LDevice) FindLN(prefix, lnClass, inst string) *LN
func (s *SCL) FindLNodeType(id string) *LNodeType
func (s *SCL) FindDOType(id string) *DOType
func (s *SCL) FindDAType(id string) *DAType
func (s *SCL) FindEnumType(id string) *EnumType
```

### Generate

```go
func Generate(w io.Writer, s *SCL) error
```

Writes the SCL model as XML. Does not serialize GSE/SMV control blocks.

### Validate (deprecated)

```go
func Validate(s *SCL) []Diagnostic
```

Deprecated: use `scl/validate.All()` for pass-based validation with a shared
index.

---

## SCL Index Package

Package `scl/index` provides O(1) lookup of SCL elements via a pre-built
index.

### Building the index

```go
func Build(s *scl.SCL) (*Index, []scl.Diagnostic)
```

Returns the index and any duplicate-detection diagnostics.

### Index

```go
type Index struct {
    IEDs         map[string]*scl.IED
    AccessPoints map[AccessPointKey]*scl.AccessPoint
    LDevices     map[LDeviceKey]*scl.LDevice
    LNs          map[LNKey]*scl.LN
    LNodeTypes   map[string]*scl.LNodeType
    DOTypes      map[string]*scl.DOType
    DATypes      map[string]*scl.DAType
    EnumTypes    map[string]*scl.EnumType
    DataSets     map[DataSetKey]*scl.DataSet
    Reports      map[ControlKey]*scl.ReportControl
    GSEControls  map[ControlKey]*scl.GSEControl
    SMVControls  map[ControlKey]*scl.SMVControl
    ConnectedAPs map[AccessPointKey]*scl.ConnectedAP
}
```

### Key types

```go
type AccessPointKey struct { IED, AP string }
type LDeviceKey     struct { IED, LDInst string }
type LNKey          struct { IED, LDInst, Prefix, LNClass, Inst string }
type DataSetKey     struct { IED, LDInst, Prefix, LNClass, LNInst, Name string }
type ControlKey     struct { IED, LDInst, Prefix, LNClass, LNInst, Name string }
```

### Resolver methods

```go
func (idx *Index) FindIED(name string) *scl.IED
func (idx *Index) FindAccessPoint(ied, ap string) *scl.AccessPoint
func (idx *Index) FindLDevice(ied, ldInst string) *scl.LDevice
func (idx *Index) FindLN(ied, ldInst, prefix, lnClass, inst string) *scl.LN
func (idx *Index) FindLNodeType(id string) *scl.LNodeType
func (idx *Index) FindDOType(id string) *scl.DOType
func (idx *Index) FindDAType(id string) *scl.DAType
func (idx *Index) FindEnumType(id string) *scl.EnumType
func (idx *Index) FindDataSet(ied, ldInst, prefix, lnClass, lnInst, name string) *scl.DataSet
func (idx *Index) FindReport(ied, ldInst, prefix, lnClass, lnInst, name string) *scl.ReportControl
func (idx *Index) FindGSEControl(ied, ldInst, prefix, lnClass, lnInst, name string) *scl.GSEControl
func (idx *Index) FindSMVControl(ied, ldInst, prefix, lnClass, lnInst, name string) *scl.SMVControl
func (idx *Index) FindConnectedAP(ied, ap string) *scl.ConnectedAP
func (idx *Index) ResolveLNType(ln *scl.LN) *scl.LNodeType
```

---

## SCL Validate Package

Package `scl/validate` provides pass-based semantic validation.

### Running validation

```go
func All(s *scl.SCL, idx *index.Index, indexDiags []scl.Diagnostic) []scl.Diagnostic
func WithOptions(s *scl.SCL, idx *index.Index, indexDiags []scl.Diagnostic, opts Options) []scl.Diagnostic
```

### Options

```go
type Options struct {
    SkipTemplates     bool
    SkipIEDs          bool
    SkipCommunication bool
    SkipDatasets      bool
    SkipControls      bool
    SkipTopology      bool
}
```

### Individual passes

Each pass can be called independently:

```go
func Templates(s *scl.SCL, idx *index.Index) []scl.Diagnostic
func IEDs(s *scl.SCL, idx *index.Index) []scl.Diagnostic
func Communication(s *scl.SCL, idx *index.Index) []scl.Diagnostic
func Datasets(s *scl.SCL, idx *index.Index) []scl.Diagnostic
func Controls(s *scl.SCL, idx *index.Index) []scl.Diagnostic
func Topology(s *scl.SCL, idx *index.Index) []scl.Diagnostic
```

### Usage

```go
doc, err := scl.ParseFile("station.scd")
if err != nil {
    log.Fatal(err)
}

idx, idxDiags := index.Build(doc)
diags := validate.All(doc, idx, idxDiags)

for _, d := range diags {
    fmt.Printf("[%s] %s: %s: %s\n", d.Severity, d.Code, d.Path, d.Message)
}
```

---

## Errors

### Sentinel errors

Sentinel errors are package-level variables tested with `errors.Is()`:

| Sentinel | Meaning |
|----------|---------|
| `ErrInvalidReference` | Syntactically invalid IEC 61850 object reference |
| `ErrInvalidFunctionalConstraint` | Unrecognized functional constraint value |
| `ErrNotFound` | Requested IEC 61850 object does not exist on the server |
| `ErrTypeMismatch` | Value type does not match the expected IEC 61850 type |
| `ErrUnsupportedService` | Service not supported by the server or library |
| `ErrSubscriptionClosed` | Report subscription has been closed |
| `ErrSCLParse` | Failure to parse an SCL file |
| `ErrModelMismatch` | Mismatch between expected and actual data model |
| `ErrUnsupportedCDC` | Unsupported Common Data Class |
| `ErrReportDecode` | Failure to decode a report payload |
| `ErrDatasetDecode` | Failure to decode dataset contents |
| `ErrClosed` | Client connection has been closed |
| `ErrDataAccess` | Per-variable data access failure from server |
| `ErrInvalidArgument` | Invalid caller-supplied argument |
| `ErrProtocol` | Protocol-level mismatch between client and server |
| `ErrControlFailed` | Control operation (operate, select, cancel) rejected or failed |
| `ErrSelectFailed` | Select or select-with-value request denied |
| `ErrOperateFailed` | Operate request denied or could not be executed |
| `ErrCancelFailed` | Cancel request denied or could not be executed |
| `ErrNotControllable` | Target data object's ctlModel is status-only |

### Typed errors

Typed error structs provide additional context via `errors.As()`:

| Type | Fields | Usage |
|------|--------|-------|
| `ReferenceError` | `Input`, `Reason`, `Wrapped` | Malformed object reference |
| `DecodeError` | `Ref`, `Type`, `Message`, `Wrapped` | MMS value decode failure |
| `ModelError` | `Ref`, `Message`, `Wrapped` | Model inconsistency |
| `ReportError` | `RCBRef`, `Message`, `Wrapped` | Report/RCB failure |
| `SCLParseError` | `File`, `Line`, `Message`, `Wrapped` | SCL parse failure |
| `DataAccessError` | `Ref`, `ErrorCode`, `Operation` | Per-variable access failure |
| `ControlError` | `Ref`, `Operation`, `AddCause`, `Wrapped` | Control operation rejection |

### Error wrapping

All errors wrap lower-level `go-mms` errors using `fmt.Errorf` with `%w`,
preserving the full error chain. `errors.Is()` and `errors.As()` work
transitively.

### SCL diagnostics

The `scl` package uses structured `Diagnostic` values for non-fatal parse
and validation issues, returned via `Result.Diagnostics`:

```go
type Diagnostic struct {
    Severity DiagSeverity // "error", "warning", or "info"
    Code     string       // machine-readable category
    Path     string       // SCL path (e.g. "IED[IED1]/LD[LD0]/LN[LLN0]")
    Message  string       // human-readable description
}
```

| Code | Severity | Meaning |
|------|----------|---------|
| `duplicate-id` | error | Duplicate type template ID |
| `duplicate-ied` | error | Duplicate IED name |
| `duplicate-access-point` | error | Duplicate AccessPoint within IED |
| `duplicate-ld` | error | Duplicate LDevice inst within IED |
| `duplicate-ln` | error | Duplicate LN within LDevice |
| `duplicate-dataset` | warning | Duplicate DataSet name within LN |
| `duplicate-report` | warning | Duplicate ReportControl name within LN |
| `duplicate-connected-ap` | warning | Duplicate ConnectedAP in SubNetwork |
| `missing-dotype` | error | LNodeType or SDO references nonexistent DOType |
| `missing-datype` | error | DA or BDA references nonexistent DAType |
| `missing-enumtype` | error | DA or BDA references nonexistent EnumType |
| `missing-lnodetype` | error | LN references nonexistent LNodeType |
| `missing-dataset` | error | Control block references nonexistent DataSet |
| `missing-connected-ap` | error | ConnectedAP references nonexistent IED or AP |
| `missing-ld` | warning | GSE/SMV references nonexistent LDevice |
| `unresolved-gse-control` | warning | GSE cbName does not match any GSEControl |
| `unresolved-smv-control` | warning | SMV cbName does not match any SMVControl |
| `unresolved-fcda` | warning | FCDA references nonexistent LDevice |
| `unresolved-topology-lnode` | error/warning | Topology LNode references nonexistent IED/LD/LN |
| `invalid-count` | warning | Numeric attribute could not be parsed |

### Usage patterns

```go
// Test for a specific sentinel
if errors.Is(err, iec61850.ErrNotFound) {
    // object does not exist
}

// Extract typed context
var reportErr *iec61850.ReportError
if errors.As(err, &reportErr) {
    log.Printf("report %s failed: %s", reportErr.RCBRef, reportErr.Message)
}

// Control error with AddCause
var ctlErr *iec61850.ControlError
if errors.As(err, &ctlErr) {
    log.Printf("control %s %s failed: addCause=%d", ctlErr.Ref, ctlErr.Operation, ctlErr.AddCause)
}

// Data access error
var daErr *iec61850.DataAccessError
if errors.As(err, &daErr) {
    log.Printf("access %s failed: code=%d op=%s", daErr.Ref, daErr.ErrorCode, daErr.Operation)
}

// Check for underlying MMS errors
var mmsErr *mms.ServiceError
if errors.As(err, &mmsErr) {
    log.Printf("MMS error class=%d code=%d", mmsErr.Class, mmsErr.Code)
}
```
