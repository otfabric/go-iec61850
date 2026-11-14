// SPDX-License-Identifier: MIT

package scl

import (
	"encoding/csv"
	"fmt"
	"io"
)

// FlatRow represents a single flattened row in an SCL model,
// suitable for CSV export or tabular display.
type FlatRow struct {
	// IED is the IED name.
	IED string

	// AccessPoint is the access point name.
	AccessPoint string

	// LD is the logical device instance name.
	LD string

	// LN is the logical node name (prefix + lnClass + inst).
	LN string

	// Path is the dot-separated data path (DO.DA or DO.SDO.DA).
	Path string

	// FC is the functional constraint, if available.
	FC string

	// BType is the basic type of the data attribute.
	BType string

	// CDC is the Common Data Class of the containing DO type.
	CDC string

	// Desc is the description, if available.
	Desc string

	// Status indicates the resolution status of this row.
	// Empty means fully resolved. Non-empty values indicate
	// incomplete flattening: "unresolved-lntype", "unresolved-dotype"
	// (DO or SDO type not found), "unresolved-datype" (Struct DA or BDA
	// whose DAType is missing) and "type-cycle" (an SDO or BDA that
	// refers back to a type being expanded). Such a row is the stub of
	// the branch that could not be expanded. "expansion-limit" marks
	// the row where the expansion stopped at its limit
	// ([FlattenOptions.MaxExpandedNodes]); no row follows it. Arrays
	// (count attribute) are flattened as a single element; the count is
	// not expanded.
	Status string
}

// Flatten expands an SCL model into a flat list of rows, one per data
// attribute. Each row represents a leaf data attribute with its full
// path through the IEC 61850 hierarchy.
//
// Type resolution uses the DataTypeTemplates to expand LNodeType →
// DOType → DA/SDO chains. Attributes whose types cannot be resolved
// are still emitted with available metadata.
//
// The expansion stops after [DefaultMaxExpandedNodes] nodes; the last
// row then has the status "expansion-limit". [FlattenWithOptions] sets
// another limit and reports it as a diagnostic.
func Flatten(s *SCL) []FlatRow {
	rows, _ := FlattenWithOptions(s, FlattenOptions{})
	return rows
}

// FlattenOptions control [FlattenWithOptions].
type FlattenOptions struct {
	// MaxExpandedNodes is the number of nodes the expansion may
	// produce; zero or less means [DefaultMaxExpandedNodes]. A node is
	// one DO, SDO, DA or BDA at one place of the expanded model (a type
	// referenced from n places counts n times), or the stub row of a
	// branch that cannot be expanded.
	MaxExpandedNodes int
}

// FlattenWithOptions is [Flatten] with options. When the expansion
// reaches opts.MaxExpandedNodes it stops: the rows expanded so far are
// returned, the last with the status "expansion-limit", together with
// an error diagnostic with code "expansion-limit" that names the
// logical node where the expansion stopped. The diagnostics are empty
// otherwise.
func FlattenWithOptions(s *SCL, opts FlattenOptions) ([]FlatRow, []Diagnostic) {
	max := opts.MaxExpandedNodes
	if max <= 0 {
		max = DefaultMaxExpandedNodes
	}
	f := &flattener{
		lntIndex: make(map[string]*LNodeType, len(s.DataTypeTemplates.LNodeTypes)),
		dotIndex: make(map[string]*DOType, len(s.DataTypeTemplates.DOTypes)),
		datIndex: make(map[string]*DAType, len(s.DataTypeTemplates.DATypes)),
		left:     max,
		max:      max,
	}
	for i := range s.DataTypeTemplates.LNodeTypes {
		lnt := &s.DataTypeTemplates.LNodeTypes[i]
		f.lntIndex[lnt.ID] = lnt
	}
	for i := range s.DataTypeTemplates.DOTypes {
		dot := &s.DataTypeTemplates.DOTypes[i]
		f.dotIndex[dot.ID] = dot
	}
	for i := range s.DataTypeTemplates.DATypes {
		dat := &s.DataTypeTemplates.DATypes[i]
		f.datIndex[dat.ID] = dat
	}

	for _, ied := range s.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				f.flattenLD(ied.Name, ap.Name, ld)
				if f.stopped {
					return f.rows, []Diagnostic{f.limitDiagnostic()}
				}
			}
		}
	}

	return f.rows, nil
}

// flattener is the state of one [FlattenWithOptions] call.
type flattener struct {
	lntIndex map[string]*LNodeType
	dotIndex map[string]*DOType
	datIndex map[string]*DAType
	rows     []FlatRow

	left    int  // nodes the expansion may still produce
	max     int  // the limit, for the diagnostic
	stopped bool // the limit was reached; nothing more is expanded
	at      FlatRow
}

// take counts one node and reports whether the expansion may go on.
// When the limit is reached it appends the "expansion-limit" stub row
// for row and stops the expansion.
func (f *flattener) take(row FlatRow) bool {
	if f.stopped {
		return false
	}
	if f.left == 0 {
		f.stopped = true
		row.Status = "expansion-limit"
		f.at = row
		f.rows = append(f.rows, row)
		return false
	}
	f.left--
	return true
}

func (f *flattener) limitDiagnostic() Diagnostic {
	sink := newDiagSink(1, DefaultMaxDiagnosticName)
	return Diagnostic{
		Severity: DiagError,
		Code:     "expansion-limit",
		Path:     fmt.Sprintf("IED[%s]/LD[%s]/LN[%s]", sink.name(f.at.IED), sink.name(f.at.LD), sink.name(f.at.LN)),
		Message: fmt.Sprintf("%v: the type templates expand to more than %d nodes (FlattenOptions.MaxExpandedNodes); the rows end here",
			ErrLimitExceeded, f.max),
	}
}

func (f *flattener) flattenLD(iedName, apName string, ld LDevice) {
	if ld.LN0 != nil {
		f.flattenLN(iedName, apName, ld.Inst, *ld.LN0)
	}
	for _, ln := range ld.LNs {
		if f.stopped {
			return
		}
		f.flattenLN(iedName, apName, ld.Inst, ln)
	}
}

func (f *flattener) flattenLN(iedName, apName, ldInst string, ln LN) {
	lnName := ln.Prefix + ln.LNClass + ln.Inst
	base := FlatRow{IED: iedName, AccessPoint: apName, LD: ldInst, LN: lnName}

	lnt, ok := f.lntIndex[ln.LNType]
	if !ok {
		if f.take(base) {
			row := base
			row.Status = "unresolved-lntype"
			f.rows = append(f.rows, row)
		}
		return
	}

	for _, do := range lnt.DOs {
		row := base
		row.Path, row.Desc = do.Name, do.Desc
		if !f.take(row) {
			return
		}
		dot, ok := f.dotIndex[do.Type]
		if !ok {
			row.Status = "unresolved-dotype"
			f.rows = append(f.rows, row)
			continue
		}

		visited := map[string]bool{dot.ID: true}
		f.flattenDOTypeRec(base, do.Name, dot, visited)
	}
}

func (f *flattener) flattenDOTypeRec(base FlatRow, prefix string, dot *DOType, visitedDO map[string]bool) {
	for _, da := range dot.DAs {
		f.flattenDA(base, prefix, da, dot.CDC)
		if f.stopped {
			return
		}
	}

	for _, sdo := range dot.SDOs {
		row := base
		row.Path, row.Desc = prefix+"."+sdo.Name, sdo.Desc
		if !f.take(row) {
			return
		}
		if visitedDO[sdo.Type] {
			row.Status = "type-cycle"
			f.rows = append(f.rows, row)
			continue
		}
		subDOT, ok := f.dotIndex[sdo.Type]
		if !ok {
			row.Status = "unresolved-dotype"
			f.rows = append(f.rows, row)
			continue
		}
		visitedDO[sdo.Type] = true
		f.flattenDOTypeRec(base, prefix+"."+sdo.Name, subDOT, visitedDO)
		delete(visitedDO, sdo.Type)
		if f.stopped {
			return
		}
	}
}

func (f *flattener) flattenDA(base FlatRow, prefix string, da DA, cdc string) {
	row := base
	row.Path, row.FC, row.BType, row.CDC, row.Desc = prefix+"."+da.Name, da.FC, da.BType, cdc, da.Desc
	if !f.take(row) {
		return
	}

	if da.BType == "Struct" {
		dat, ok := f.datIndex[da.Type]
		if ok {
			visited := make(map[string]bool)
			visited[da.Type] = true
			f.flattenDATypeRec(base, row.Path, da.FC, cdc, dat, visited)
			return
		}
		row.Status = "unresolved-datype"
	}

	f.rows = append(f.rows, row)
}

func (f *flattener) flattenDATypeRec(base FlatRow, prefix, fc, cdc string, dat *DAType, visitedDA map[string]bool) {
	for _, bda := range dat.BDAs {
		row := base
		row.Path, row.FC, row.BType, row.CDC, row.Desc = prefix+"."+bda.Name, fc, bda.BType, cdc, bda.Desc
		if !f.take(row) {
			return
		}

		if bda.BType == "Struct" {
			subDAT, ok := f.datIndex[bda.Type]
			switch {
			case visitedDA[bda.Type]:
				row.Status = "type-cycle"
			case ok:
				visitedDA[bda.Type] = true
				f.flattenDATypeRec(base, row.Path, fc, cdc, subDAT, visitedDA)
				delete(visitedDA, bda.Type)
				if f.stopped {
					return
				}
				continue
			default:
				row.Status = "unresolved-datype"
			}
		}

		f.rows = append(f.rows, row)
	}
}

// WriteCSV writes flattened rows as CSV to the given writer using
// [encoding/csv]. The first line is the header row.
func WriteCSV(w io.Writer, rows []FlatRow) error {
	cw := csv.NewWriter(w)

	if err := cw.Write([]string{"IED", "AccessPoint", "LD", "LN", "Path", "FC", "BType", "CDC", "Desc", "Status"}); err != nil {
		return fmt.Errorf("scl: write CSV header: %w", err)
	}

	for _, r := range rows {
		if err := cw.Write([]string{r.IED, r.AccessPoint, r.LD, r.LN, r.Path, r.FC, r.BType, r.CDC, r.Desc, r.Status}); err != nil {
			return fmt.Errorf("scl: write CSV row: %w", err)
		}
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("scl: flush CSV: %w", err)
	}
	return nil
}

// PrintTree writes a hierarchical text representation of the SCL model
// to the given writer.
func PrintTree(w io.Writer, s *SCL) error {
	for _, ied := range s.IEDs {
		if _, err := fmt.Fprintf(w, "IED: %s", ied.Name); err != nil {
			return err
		}
		if ied.Desc != "" {
			if _, err := fmt.Fprintf(w, " (%s)", ied.Desc); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}

		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "  AP: %s\n", ap.Name); err != nil {
				return err
			}
			for _, ld := range ap.Server.LDevices {
				if _, err := fmt.Fprintf(w, "    LD: %s\n", ld.Inst); err != nil {
					return err
				}
				if err := printLNs(w, ld, "      "); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func printLNs(w io.Writer, ld LDevice, indent string) error {
	if ld.LN0 != nil {
		if err := printLN(w, *ld.LN0, indent); err != nil {
			return err
		}
	}
	for _, ln := range ld.LNs {
		if err := printLN(w, ln, indent); err != nil {
			return err
		}
	}
	return nil
}

// DataSetRow represents a single data set with its location in the
// IEC 61850 hierarchy.
type DataSetRow struct {
	IED         string
	AccessPoint string
	LD          string
	LN          string
	DataSet     string
	Desc        string
	MemberCount int
}

// ExportDataSets extracts all data set definitions from the SCL model
// into a flat list suitable for tabular display.
func ExportDataSets(s *SCL) []DataSetRow {
	var rows []DataSetRow
	for _, ied := range s.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				exportLDDataSets(&rows, ied.Name, ap.Name, ld)
			}
		}
	}
	return rows
}

func exportLDDataSets(rows *[]DataSetRow, iedName, apName string, ld LDevice) {
	exportLNDataSets := func(ln LN) {
		lnName := ln.Prefix + ln.LNClass + ln.Inst
		for _, ds := range ln.DataSets {
			*rows = append(*rows, DataSetRow{
				IED: iedName, AccessPoint: apName,
				LD: ld.Inst, LN: lnName,
				DataSet: ds.Name, Desc: ds.Desc,
				MemberCount: len(ds.FCDAs),
			})
		}
	}
	if ld.LN0 != nil {
		exportLNDataSets(*ld.LN0)
	}
	for _, ln := range ld.LNs {
		exportLNDataSets(ln)
	}
}

// ReportRow represents a single report control block with its
// location in the IEC 61850 hierarchy.
type ReportRow struct {
	IED         string
	AccessPoint string
	LD          string
	LN          string
	Name        string
	RptID       string
	DatSet      string
	Buffered    bool
	ConfRev     uint32
}

// ExportReports extracts all report control block definitions from
// the SCL model into a flat list suitable for tabular display.
func ExportReports(s *SCL) []ReportRow {
	var rows []ReportRow
	for _, ied := range s.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				exportLDReports(&rows, ied.Name, ap.Name, ld)
			}
		}
	}
	return rows
}

func exportLDReports(rows *[]ReportRow, iedName, apName string, ld LDevice) {
	exportLNReports := func(ln LN) {
		lnName := ln.Prefix + ln.LNClass + ln.Inst
		for _, rc := range ln.Reports {
			*rows = append(*rows, ReportRow{
				IED: iedName, AccessPoint: apName,
				LD: ld.Inst, LN: lnName,
				Name: rc.Name, RptID: rc.RptID,
				DatSet: rc.DatSet, Buffered: rc.Buffered,
				ConfRev: rc.ConfRev,
			})
		}
	}
	if ld.LN0 != nil {
		exportLNReports(*ld.LN0)
	}
	for _, ln := range ld.LNs {
		exportLNReports(ln)
	}
}

// GSEControlRow represents a single GSE (GOOSE) control block with
// its location in the IEC 61850 hierarchy.
type GSEControlRow struct {
	IED         string `json:"ied"`
	AccessPoint string `json:"accessPoint"`
	LD          string `json:"ld"`
	Name        string `json:"name"`
	AppID       string `json:"appID"`
	Type        string `json:"type,omitempty"`
	DatSet      string `json:"datSet,omitempty"`
	ConfRev     uint32 `json:"confRev"`
	Desc        string `json:"desc,omitempty"`
}

// ExportGSEControls extracts all GSE control block definitions from
// the SCL model into a flat list suitable for tabular display.
func ExportGSEControls(s *SCL) []GSEControlRow {
	var rows []GSEControlRow
	for _, ied := range s.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				if ld.LN0 == nil {
					continue
				}
				for _, gc := range ld.LN0.GSEControls {
					rows = append(rows, GSEControlRow{
						IED: ied.Name, AccessPoint: ap.Name,
						LD: ld.Inst, Name: gc.Name,
						AppID: gc.AppID, Type: gc.Type,
						DatSet: gc.DatSet, ConfRev: gc.ConfRev,
						Desc: gc.Desc,
					})
				}
			}
		}
	}
	return rows
}

// SMVControlRow represents a single Sampled Values control block with
// its location in the IEC 61850 hierarchy.
type SMVControlRow struct {
	IED         string `json:"ied"`
	AccessPoint string `json:"accessPoint"`
	LD          string `json:"ld"`
	Name        string `json:"name"`
	SmvID       string `json:"smvID"`
	DatSet      string `json:"datSet,omitempty"`
	SmpRate     uint32 `json:"smpRate"`
	SmpMod      string `json:"smpMod,omitempty"`
	NofASDU     uint32 `json:"nofASDU"`
	Multicast   bool   `json:"multicast"`
	ConfRev     uint32 `json:"confRev"`
	Desc        string `json:"desc,omitempty"`
}

// ExportSMVControls extracts all Sampled Values control block
// definitions from the SCL model into a flat list.
func ExportSMVControls(s *SCL) []SMVControlRow {
	var rows []SMVControlRow
	for _, ied := range s.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				if ld.LN0 == nil {
					continue
				}
				for _, sv := range ld.LN0.SMVControls {
					rows = append(rows, SMVControlRow{
						IED: ied.Name, AccessPoint: ap.Name,
						LD: ld.Inst, Name: sv.Name,
						SmvID: sv.SmvID, DatSet: sv.DatSet,
						SmpRate: sv.SmpRate, SmpMod: sv.SmpMod, NofASDU: sv.NofASDU,
						Multicast: sv.Multicast, ConfRev: sv.ConfRev,
						Desc: sv.Desc,
					})
				}
			}
		}
	}
	return rows
}

// ConnectedAPRow represents a connected access point with its
// sub-network and addressing information.
type ConnectedAPRow struct {
	SubNetwork string `json:"subNetwork"`
	IEDName    string `json:"iedName"`
	APName     string `json:"apName"`
	GSECount   int    `json:"gseCount"`
	SMVCount   int    `json:"smvCount"`
	Desc       string `json:"desc,omitempty"`
}

// ExportConnectedAPs extracts all ConnectedAP entries from the
// SCL communication section.
func ExportConnectedAPs(s *SCL) []ConnectedAPRow {
	var rows []ConnectedAPRow
	if s.Communication == nil {
		return rows
	}
	for _, sn := range s.Communication.SubNetworks {
		for _, cap := range sn.ConnectedAPs {
			rows = append(rows, ConnectedAPRow{
				SubNetwork: sn.Name,
				IEDName:    cap.IEDName,
				APName:     cap.APName,
				GSECount:   len(cap.GSEs),
				SMVCount:   len(cap.SMVs),
			})
		}
	}
	return rows
}

func printLN(w io.Writer, ln LN, indent string) error {
	name := ln.Prefix + ln.LNClass + ln.Inst
	if _, err := fmt.Fprintf(w, "%sLN: %s", indent, name); err != nil {
		return err
	}
	if ln.Desc != "" {
		if _, err := fmt.Fprintf(w, " (%s)", ln.Desc); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	for _, ds := range ln.DataSets {
		if _, err := fmt.Fprintf(w, "%s  DS: %s [%d members]\n", indent, ds.Name, len(ds.FCDAs)); err != nil {
			return err
		}
	}
	for _, rc := range ln.Reports {
		kind := "URCB"
		if rc.Buffered {
			kind = "BRCB"
		}
		if _, err := fmt.Fprintf(w, "%s  %s: %s\n", indent, kind, rc.Name); err != nil {
			return err
		}
	}
	return nil
}
