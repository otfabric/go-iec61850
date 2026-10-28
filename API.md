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
- [GOOSE control blocks (client)](#goose-control-blocks-client)
- [SV control blocks (client)](#sv-control-blocks-client)
- [Server (experimental)](#server-experimental)
- [GOOSE package](#goose-package)
- [SV package](#sv-package)
- [L2 package](#l2-package)
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
    Logger     *slog.Logger // also passed to MMS when MMS.Logger is nil
    Strictness StrictnessOptions
    Cache      CacheStrategy
    IEDName    string
    Retry      mms.RetryPolicy // passed to iso.WithRetry; zero value: one attempt
    Metrics    mms.Metrics     // used when MMS.Metrics is nil
}
```

`Retry` covers TCP connect, COTP handshake and MMS association per attempt;
errors another attempt cannot fix (`mms.IsRetryable`) return at once. Each
retry is logged at Warn, then `Retry.OnRetry` is called.
`mms.DefaultRetryPolicy()` is a reasonable reconnect policy. Per-request
timeouts are set with `MMS.RequestTimeout`; `Client.MMS().Done()` and
`Err()` report the end of the association.

### ClientOptions

```go
type ClientOptions struct {
    Logger     *slog.Logger
    Strictness StrictnessOptions
    Cache      CacheStrategy
    IEDName    string
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

`GetDataSet` wraps `ErrNotFound` when the server answers that the data set
does not exist (MMS access/object-non-existent or definition/object-undefined);
the `*mms.ServiceError` stays in the chain.

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
    LastModified time.Time // zero if the server omitted it
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

## GOOSE control blocks (client)

```go
func (c *Client) GetGoCB(ctx context.Context, ld, gocbItemID string) (*GoCB, error)
func (c *Client) SetGoCB(ctx context.Context, ld, gocbItemID string, upd GoCBUpdate) error

type GoCB struct {
    Reference        string // "LD/LLN0$GO$name"
    GoEna            bool
    GoID, DatSet     string
    ConfRev          uint32
    NdsCom           bool
    DstAddress       GoCBDstAddress
    MinTime, MaxTime uint32 // ms, valid when HasTimes
    HasTimes         bool
    FixedOffs        bool
}

type GoCBDstAddress struct {
    Addr     goose.MAC
    Priority uint8
    VID      uint16
    AppID    uint16
}

// Nil fields are not written; GoEna is written last.
type GoCBUpdate struct {
    GoID, DatSet *string
    GoEna        *bool
}
```

`gocbItemID` is the MMS item (`"LLN0$GO$gcb01"`). See [GOOSE.md](GOOSE.md#client-gocb-access).

---

## SV control blocks (client)

```go
func (c *Client) GetMSVCB(ctx context.Context, ld, msvcbItemID string) (*MSVCB, error)
func (c *Client) SetMSVCB(ctx context.Context, ld, msvcbItemID string, upd MSVCBUpdate) error

type MSVCB struct {
    Reference     string // "LD/LLN0$MS$name"
    SvEna         bool
    MsvID, DatSet string
    ConfRev       uint32
    SmpRate       uint16
    OptFlds       sv.OptFlds
    SmpMod        sv.SmpMod // SmpPerPeriod when !HasSmpMod (Edition 1)
    HasSmpMod     bool
    DstAddress    SvCBDstAddress
    NoASDU        uint16 // 0 when absent
}

type SvCBDstAddress struct {
    Addr     sv.MAC
    Priority uint8
    VID      uint16
    AppID    uint16
}

// Nil fields are not written; SvEna is written last.
type MSVCBUpdate struct {
    MsvID, DatSet *string
    SmpRate       *uint16
    OptFlds       *sv.OptFlds
    SmpMod        *sv.SmpMod
    SvEna         *bool
}
```

`msvcbItemID` is the MMS item (`"LLN0$MS$MSVCB01"`). See [SV.md](SV.md#client-msvcb-access).

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
    Metrics      mms.Metrics // used when MMS.Metrics is nil
    MMS          mms.ServerOptions // incl. AssociationTimeout
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
func (s *Server) Shutdown(ctx context.Context) error // graceful; ListenAndServe returns mms.ErrServerClosed
func (s *Server) Close()
func (s *Server) Capabilities() ServiceCapabilities
func (s *Server) HandleIdentify(id ServerIdentity)
func (s *Server) HandleStatus()
func (s *Server) EnableGOOSE(opts GOOSEOptions) (*GOOSEEngine, error)
func (s *Server) GOOSEEngine() *GOOSEEngine
func (s *Server) EnableSV(opts SVOptions) (*SVEngine, error)
func (s *Server) SVEngine() *SVEngine
```

### ServiceCapabilities

```go
type ServiceCapabilities struct {
    Variables, DataSets, Reports, Controls     bool
    SettingGroups, Journals, Files, Identify   bool
    GOOSE, SV                                  bool
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

After a successful operate of an enhanced-security control
(`CtlModelDirectEnhanced`, `CtlModelSBOEnhanced`) the server sends a
positive CommandTermination: an InformationReport of `LN$CO$DO$Oper`
with the operated value, to the operating connection.

### GOOSE engine

```go
type GOOSEOptions struct {
    Transport     goose.Transport // required
    StartDisabled bool            // GoEna false initially
    Logger        *slog.Logger
    Metrics       goose.Metrics
}

type GOOSEEngine struct { /* unexported */ }

func (e *GOOSEEngine) ControlBlocks() []string // "LD/LLN0$GO$name", sorted
func (e *GOOSEEngine) Publisher(ref string) (*goose.Publisher, error)
func (e *GOOSEEngine) SetEnabled(ref string, enabled bool) error
func (e *GOOSEEngine) Stop()
```

Errors wrap `ErrGOOSE` (or `ErrNotFound` for an unknown reference). See
[GOOSE.md](GOOSE.md#server-gocbs).

### SV engine

```go
type SVOptions struct {
    Transport        sv.Transport // required
    StartDisabled    bool         // SvEna false initially
    SmpSynch         uint8        // smpSynch sent
    NominalFrequency float64      // for SmpPerPeriod, default 50
    Logger           *slog.Logger
    Metrics          sv.Metrics
}

type SVEngine struct { /* unexported */ }

func (e *SVEngine) ControlBlocks() []string // "LD/LLN0$MS$name", sorted
func (e *SVEngine) Publisher(ref string) (*sv.Publisher, error) // nil while disabled
func (e *SVEngine) SetEnabled(ref string, enabled bool) error
func (e *SVEngine) Stop()
```

Errors wrap `ErrSV` (or `ErrNotFound` for an unknown reference). See
[SV.md](SV.md#server-msvcbs).

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

## GOOSE package

`import "github.com/otfabric/go-iec61850/goose"` — full guide in
[GOOSE.md](GOOSE.md).

### Frames

```go
const EtherType uint16 = 0x88B8
const EtherTypeVLAN uint16 = 0x8100
const SimulationBit uint16 = 0x8000
const MaxPayload = 1500

type MAC = l2.MAC // see the L2 package
var Broadcast MAC
func ParseMAC(s string) (MAC, error) // fails with *goose.ConfigError

type VLAN struct { Priority uint8; DEI bool; ID uint16 }

type Frame struct {
    Dst, Src             MAC
    VLAN                 *VLAN
    AppID                uint16
    Simulation           bool
    Reserved1, Reserved2 uint16
    PDU                  PDU
}

type PDU struct {
    GocbRef           string
    TimeAllowedToLive uint32 // ms
    DatSet, GoID      string
    T                 time.Time
    TimeQuality       uint8
    StNum, SqNum      uint32
    Test              bool
    ConfRev           uint32
    NdsCom            bool
    NumDatSetEntries  uint32
    AllData           []*mms.Value
    Security          []byte
}

type Limits struct { MaxDepth, MaxElements int }
var DefaultLimits = Limits{MaxDepth: 16, MaxElements: 4096}

func Encode(f *Frame) ([]byte, error)
func AppendFrame(dst []byte, f *Frame) ([]byte, error)
func EncodePDU(p *PDU) ([]byte, error)
func Decode(b []byte) (*Frame, error)
func DecodeWithLimits(b []byte, lim Limits) (*Frame, error)
func DecodePDU(b []byte) (*PDU, error)
func EncodeData(v *mms.Value) ([]byte, error)
func DecodeData(b []byte) (*mms.Value, error)
func SameShape(a, b *mms.Value) bool
func MatchesType(v *mms.Value, ts mms.TypeSpec) bool
func ZeroValue(ts mms.TypeSpec) *mms.Value
```

### Publisher

```go
const DefaultMinTime = 10 * time.Millisecond
const DefaultMaxTime = time.Second

type PublisherConfig struct {
    Dst, Src             MAC   // Src defaults to the transport address
    VLAN                 *VLAN
    AppID                uint16
    GocbRef, DatSet, GoID string
    ConfRev              uint32
    MinTime, MaxTime     time.Duration
    TimeAllowedToLive    time.Duration // default 2*MaxTime
    Test, Simulation, NdsCom bool
    InitialStNum         uint32
    Values               []*mms.Value
    Logger               *slog.Logger
    Metrics              Metrics
    Now                  func() time.Time
}

type PublisherState struct {
    StNum, SqNum uint32
    T            time.Time
    Enabled      bool
    Interval     time.Duration
    Sent         uint64
}

func NewPublisher(t Transport, cfg PublisherConfig) (*Publisher, error)
func (p *Publisher) Run(ctx context.Context) error
func (p *Publisher) Publish(values []*mms.Value) error
func (p *Publisher) SetFlags(test, simulation, ndsCom bool) error
func (p *Publisher) SetEnabled(enabled bool) error
func (p *Publisher) State() PublisherState
func (p *Publisher) Config() PublisherConfig
func (p *Publisher) Close() error
```

### Subscriber

```go
type SubscriberConfig struct {
    Limits        Limits
    Logger        *slog.Logger
    Metrics       Metrics
    OnDecodeError func(raw []byte, err error)
    Now           func() time.Time
}

type SubscriptionConfig struct {
    Dst              MAC      // zero: any
    AppIDs           []uint16 // empty: any
    GocbRef          string
    Src              MAC
    DatSet, GoID     string   // validated when set
    ConfRev          uint32   // validated when non-zero
    Members          []mms.TypeSpec
    AcceptSimulation bool
    OnlyStateChanges bool
    Handler          func(Event) // instead of the channel
    QueueSize        int         // default 256
}

func NewSubscriber(t Transport, cfg SubscriberConfig) (*Subscriber, error)
func (s *Subscriber) Subscribe(cfg SubscriptionConfig) (*Subscription, error)
func (s *Subscriber) Run(ctx context.Context) error
func (s *Subscriber) HandleFrame(raw []byte)
func (s *Subscriber) Close() error
func (sub *Subscription) Events() <-chan Event
func (sub *Subscription) Supervision() []Supervision
func (sub *Subscription) Close() error

type EventKind int // EventMessage, EventTimeout, EventStNumGap, EventSqNumGap,
                   // EventDuplicate, EventOutOfOrder, EventRestart,
                   // EventInvalid, EventSupervision
type Status int    // StatusWaiting, StatusOK, StatusTimeout, StatusInvalid,
                   // StatusNeedsCommissioning

type Event struct {
    Kind                 EventKind
    Received             time.Time
    Frame                *Frame
    NewState             bool
    PrevStNum, PrevSqNum uint32
    State                Supervision
    Err                  error
}

type StreamKey struct { Src MAC; AppID uint16; GocbRef string }

type Supervision struct {
    Stream                    StreamKey
    Status                    Status
    St, SimSt, NdsCom         bool
    LastStNum, LastSqNum      uint32
    ConfRevNum, RxConfRevNum  uint32
    LastRx                    time.Time
    TimeAllowedToLive         time.Duration
    LastErr                   error
    Messages, StateChanges, Duplicates, OutOfOrder,
    StNumGaps, SqNumGaps, Restarts, Timeouts,
    Invalid, DroppedEvents    uint64
}
```

### Transports

```go
type Transport interface {
    Send(frame []byte) error
    Recv(ctx context.Context) ([]byte, error)
    HardwareAddr() MAC
    Close() error
}

type TransportOptions struct {
    Promiscuous  bool
    Multicast    []MAC
    ReceiveLocal bool
    Logger       *slog.Logger
}

// AF_PACKET on Linux, BPF on macOS/BSD, ErrUnsupportedPlatform elsewhere.
func Open(iface string, opts TransportOptions) (Transport, error)

func NewHub() *Hub
func (h *Hub) NewPort(mac MAC) *HubPort // implements Transport
func (h *Hub) SetFaults(f Faults)
func (h *Hub) Stats() HubStats
func (h *Hub) Flush()

type Faults struct {
    Loss, Duplicate, Reorder float64 // probabilities
    Delay                    time.Duration
    Seed                     int64
    Mutate                   func(frame []byte) []byte
}
type HubStats struct { Sent, Delivered, Lost, Duplicated, Reordered, Overflow uint64 }
```

### SCL binding

```go
type ControlBlock struct {
    IEDName, APName, LDInst, LNName, Name string
    GocbRef, DatSet, GoID string // on-wire references
    ConfRev    uint32
    FixedOffs  bool
    HasAddress bool
    Dst        MAC
    AppID      uint16
    VLAN       *VLAN
    MinTime, MaxTime time.Duration
    Members    []Member
}

type Member struct { Ref, FC string; Type mms.TypeSpec }

func ControlBlocksFromSCL(doc *scl.SCL) ([]ControlBlock, error)
func ControlBlockFromSCL(doc *scl.SCL, iedName, ldInst, cbName string) (*ControlBlock, error)
func (cb *ControlBlock) MemberTypes() []mms.TypeSpec
func (cb *ControlBlock) DefaultValues() []*mms.Value
func (cb *ControlBlock) PublisherConfig() (PublisherConfig, error)
func (cb *ControlBlock) SubscriptionConfig() SubscriptionConfig
```

### Observability and errors

```go
type Metrics interface {
    FrameSent(gocbRef string, retransmission bool)
    SendError(gocbRef string, err error)
    FrameReceived(gocbRef string)
    FrameDropped(reason string)
    Event(gocbRef string, kind EventKind, dropped bool)
}
type NopMetrics struct{}

type DecodeError struct { Field string; Offset int; Message string; Err error }
type EncodeError struct { Field string; Index int; Err error }
type ValidationError struct { GocbRef, Field, Want, Got string; Err error }
type ConfigError struct { Field, Message string }
type TransportError struct { Op, Interface string; Err error; Kind error }
```

Sentinels are listed in [GOOSE.md](GOOSE.md#errors).

---

## SV package

`import "github.com/otfabric/go-iec61850/sv"` — full guide in [SV.md](SV.md).

### Frames

```go
const EtherType uint16 = 0x88BA
const EtherTypeVLAN uint16 = 0x8100
const SimulationBit uint16 = 0x8000
const MaxPayload = 1500
const SmpSynchNone, SmpSynchLocal, SmpSynchGlobal uint8 = 0, 1, 2

type SmpMod uint16 // SmpPerPeriod, SmpPerSec, SecPerSmp
func (m SmpMod) String() string

type Frame struct {
    Dst, Src             MAC
    VLAN                 *VLAN
    AppID                uint16
    Simulation           bool
    Reserved1, Reserved2 uint16
    PDU                  PDU
}
func (f *Frame) String() string

type PDU struct {
    NoASDU   int    // 0 on encode: len(ASDUs)
    Security []byte // opaque
    ASDUs    []ASDU
}

type ASDU struct {
    SvID, DatSet  string // DatSet optional
    SmpCnt        uint16
    ConfRev       uint32
    RefrTm        time.Time; RefrTmQuality uint8; HasRefrTm bool
    SmpSynch      uint8
    SmpRate       uint16; HasSmpRate bool
    Sample        []byte
    SmpMod        SmpMod; HasSmpMod bool
    GmIdentity    [8]byte; HasGmIdentity bool
}

type Limits struct { MaxASDUs, MaxSample int }
var DefaultLimits = Limits{MaxASDUs: 64, MaxSample: 1500}

func Encode(f *Frame) ([]byte, error)
func AppendFrame(dst []byte, f *Frame) ([]byte, error)
func EncodePDU(p *PDU) ([]byte, error)
func Decode(b []byte) (*Frame, error)                // copies
func DecodeInto(f *Frame, b []byte, lim Limits) error // reuses f, aliases b
func DecodePDU(b []byte) (*PDU, error)
```

### Datasets

```go
type Kind uint8 // KindBoolean, KindInt8 … KindInt64, KindUint8 … KindUint32,
                // KindFloat32, KindFloat64, KindEnum, KindDbpos, KindQuality, KindTimestamp
func (k Kind) Size() int
func (k Kind) String() string
func KindOf(bType string) (Kind, bool)

type Layout []Kind
func (l Layout) Size() int
func (l Layout) Offsets() []int
func (l Layout) Validate() error
func (l Layout) Encode(dst []byte, values []*mms.Value) ([]byte, error)
func (l Layout) Decode(sample []byte) ([]*mms.Value, error)
func (l Layout) String() string

type Quality uint32 // QualityGood, QualityInvalid, QualityReserved, QualityQuestionable,
                    // QualityOverflow … QualityTest, QualityOperatorBlocked, QualityDerived
func (q Quality) Validity() Quality
func (q Quality) IsGood() bool
func (q Quality) Value() *mms.Value
func QualityFromValue(v *mms.Value) (Quality, error)

func Int32At(sample []byte, off int) int32
func Float32At(sample []byte, off int) float32
func QualityAt(sample []byte, off int) Quality
func TimestampAt(sample []byte, off int) (time.Time, uint8)

// UCA 9-2LE
const LESampleSize = 64
var LELayout Layout
type Point struct { Value int32; Q Quality }
type LESample struct { Current, Voltage [4]Point }
func (s *LESample) Append(dst []byte) []byte
func DecodeLE(sample []byte, s *LESample) error
func Amps(count int32) float64; func AmpsCount(a float64) int32   // 1 mA/count
func Volts(count int32) float64; func VoltsCount(v float64) int32 // 10 mV/count
```

### Publisher

```go
type PublisherConfig struct {
    Dst, Src         MAC
    VLAN             *VLAN
    AppID            uint16
    Simulation       bool
    SvID, DatSet     string
    ConfRev          uint32
    SmpRate          uint16
    SmpMod           SmpMod
    NominalFrequency float64 // default 50
    NoASDU           int     // default 1
    SmpSynch         uint8
    RefrTm, SendSmpRate, SendSmpMod bool
    GmIdentity       *[8]byte
    SmpCntWrap       int  // default: samples per second when integral, else 65536
    AlignSecond      bool // start at a whole second with smpCnt 0
    Layout           Layout
    Sample           []byte
    Source           func(info SampleInfo, dst []byte) []byte
    Disabled         bool
    MaxBurst         int // default 16
    Now              func() time.Time
    Logger           *slog.Logger
    Metrics          Metrics
}
type SampleInfo struct { Index uint64; SmpCnt uint16; Time time.Time }
type PublisherState struct {
    Enabled bool; SmpSynch uint8; NextSmpCnt uint16
    Samples, Frames, Skipped, Late, SendErrors uint64
    Rate float64
}

func NewPublisher(t Transport, cfg PublisherConfig) (*Publisher, error)
func (p *Publisher) Run(ctx context.Context) error
func (p *Publisher) Update(values []*mms.Value) error
func (p *Publisher) UpdateSample(sample []byte) error
func (p *Publisher) SetEnabled(enabled bool)
func (p *Publisher) SetSmpSynch(s uint8)
func (p *Publisher) SetSimulation(sim bool)
func (p *Publisher) State() PublisherState
func (p *Publisher) Config() PublisherConfig
func (p *Publisher) Close() error
func SamplesPerSecond(smpRate uint16, mod SmpMod, nominalFrequency float64) float64
func DefaultSmpCntWrap(rate float64) int
```

### Subscriber

```go
type SubscriberConfig struct {
    Limits              Limits
    AcceptSimulation    bool
    SupervisionInterval time.Duration // default 10 ms
    OnDecodeError       func(frame []byte, err error)
    Now                 func() time.Time
    Logger              *slog.Logger
    Metrics             Metrics
}
type SubscriptionConfig struct {
    Dst, Src         MAC
    AppIDs           []uint16
    SvID, DatSet     string
    ConfRev          uint32
    SmpRate          uint16
    SmpMod           SmpMod
    SampleSize       int
    Layout           Layout
    NominalFrequency float64
    SmpCntWrap       int
    Timeout          time.Duration // default 100 ms
    OnASDU           func(*Sample) // or:
    SampleQueue      int           // Samples() channel
    EventQueue       int           // default 256
}
type StreamKey struct { Src MAC; AppID uint16; SvID string }
type Sample struct {
    Stream   StreamKey
    Received time.Time
    Frame    *Frame
    Index    int
    ASDU     *ASDU
    Lost     int
}
type EventKind int // EventStart, EventGap, EventDuplicate, EventOutOfOrder, EventTimeout,
                   // EventRestored, EventSynch, EventConfRev, EventInvalid
type Event struct {
    Kind     EventKind
    Stream   StreamKey
    Time     time.Time
    SmpCnt   uint16
    Lost     int
    SmpSynch uint8
    ConfRev  uint32
    Err      error
}
type Status int // StatusOK, StatusTimeout, StatusInvalid
type Stats struct {
    Stream     StreamKey
    Status     Status
    Frames, ASDUs, Lost, Duplicates, OutOfOrder, Invalid, Timeouts, QueueDrops uint64
    LastSmpCnt uint16
    SmpSynch   uint8
    ConfRev    uint32
    Simulation bool
    SmpCntWrap int
    Rate       float64
    First, Last time.Time
}

func NewSubscriber(t Transport, cfg SubscriberConfig) (*Subscriber, error) // t may be nil
func (s *Subscriber) Subscribe(cfg SubscriptionConfig) (*Subscription, error)
func (s *Subscriber) Run(ctx context.Context) error
func (s *Subscriber) HandleFrame(raw []byte)
func (s *Subscriber) CheckTimeouts()
func (s *Subscriber) Close() error
func (sub *Subscription) Events() <-chan Event
func (sub *Subscription) Samples() <-chan Sample
func (sub *Subscription) Stats() []Stats
func (sub *Subscription) Close() error
```

### SCL binding

```go
type ControlBlock struct {
    IEDName, APName, LDInst, LNName, Name string
    SvCBRef, SvID, DatSet string
    ConfRev    uint32
    SmpRate    uint16
    SmpMod     SmpMod
    NoASDU     int
    Opts       OptFlds
    HasAddress bool
    Dst        MAC
    AppID      uint16
    VLAN       *VLAN
    Members    []Member
    Layout     Layout
}
type OptFlds struct { RefreshTime, SampleSynchronized, SampleRate, DataSet, Security bool }
type Member struct { Ref, FC string; Kinds []Kind }

func ControlBlocksFromSCL(doc *scl.SCL) ([]ControlBlock, error)
func ControlBlockFromSCL(doc *scl.SCL, iedName, ldInst, cbName string) (*ControlBlock, error)
func (cb *ControlBlock) PublisherConfig() (PublisherConfig, error)
func (cb *ControlBlock) SubscriptionConfig() SubscriptionConfig
```

### Transports, metrics and errors

```go
type MAC = l2.MAC; type VLAN = l2.VLAN
type Transport = l2.Transport; type TransportOptions = l2.TransportOptions
type TransportError = l2.TransportError
type Hub = l2.Hub; type HubPort = l2.HubPort; type HubStats = l2.HubStats; type Faults = l2.Faults
func Open(iface string, opts TransportOptions) (Transport, error) // EtherTypes default: SV
func NewHub() *Hub
func ParseMAC(s string) (MAC, error) // fails with *sv.ConfigError

type Metrics interface {
    FrameSent(svID string, asdus int)
    SendError(svID string, err error)
    ASDUReceived(svID string)
    FrameDropped(reason string)
    Event(svID string, kind EventKind, dropped bool)
}
type NopMetrics struct{}

type DecodeError struct { Field string; Offset int; Message string; Err error }
type ValidationError struct { SvID, Field, Want, Got string; Err error }
type ConfigError struct { Field, Message string }
```

Sentinels are listed in [SV.md](SV.md#errors).

---

## L2 package

`import "github.com/otfabric/go-iec61850/l2"` — the raw Ethernet layer
shared by `goose` and `sv` (their `MAC`, `Transport`, `Hub` … are
aliases of these types).

```go
const EtherTypeGOOSE, EtherTypeGSE, EtherTypeSV, EtherTypeVLAN uint16 = 0x88B8, 0x88B9, 0x88BA, 0x8100

type MAC [6]byte
var Broadcast MAC
func ParseMAC(s string) (MAC, error)
func (m MAC) String() string
func (m MAC) IsZero() bool
func (m MAC) IsMulticast() bool
func (m MAC) IsGOOSEMulticast() bool // 01-0C-CD-01-xx-xx
func (m MAC) IsSVMulticast() bool    // 01-0C-CD-04-xx-xx
func (m MAC) MarshalText() ([]byte, error)
func (m *MAC) UnmarshalText(b []byte) error

type VLAN struct { Priority uint8; DEI bool; ID uint16 }
func (v VLAN) TCI() (uint16, bool)
func VLANFromTCI(tci uint16) VLAN

type Transport interface {
    Send(frame []byte) error
    Recv(ctx context.Context) ([]byte, error)
    HardwareAddr() MAC
    Close() error
}
type TransportOptions struct {
    EtherTypes   []uint16 // frames received (max 32); empty: GOOSE only
    Multicast    []MAC
    Promiscuous  bool
    ReceiveLocal bool
    Logger       *slog.Logger
}
func Open(iface string, opts TransportOptions) (Transport, error)

// Demux splits one transport by EtherType.
func NewDemux(t Transport, queue int) *Demux // queue < 1: DefaultDemuxQueue
func EtherTypeOf(frame []byte) (uint16, bool)
func (d *Demux) Port(etherTypes ...uint16) (*DemuxPort, error) // a Transport
func (d *Demux) Stats() DemuxStats // Received, Unmatched, Overflow
func (d *Demux) Err() error
func (d *Demux) Close() error

type Hub struct { /* in-memory segment */ }
func NewHub() *Hub
func (h *Hub) NewPort(mac MAC) *HubPort
func (h *Hub) SetFaults(f Faults) // Loss, Duplicate, Reorder, Delay, Seed, Mutate
func (h *Hub) Stats() HubStats    // Sent, Delivered, Lost, Duplicated, Reordered, Overflow
func (h *Hub) Flush()
```

Sentinels: `ErrClosed`, `ErrUnsupportedPlatform`, `ErrPermission`,
`ErrInterfaceNotFound`, `ErrInvalidConfig` (shared by `goose` and `sv`).

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
| `ErrGOOSE` | Server GOOSE engine failure (see [GOOSE.md](GOOSE.md)) |
| `ErrSV` | Server SV engine failure (see [SV.md](SV.md)) |

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
