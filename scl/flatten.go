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
	// incomplete flattening (e.g., "unresolved-lntype",
	// "unresolved-dotype").
	Status string
}

// Flatten expands an SCL model into a flat list of rows, one per data
// attribute. Each row represents a leaf data attribute with its full
// path through the IEC 61850 hierarchy.
//
// Type resolution uses the DataTypeTemplates to expand LNodeType →
// DOType → DA/SDO chains. Attributes whose types cannot be resolved
// are still emitted with available metadata.
func Flatten(s *SCL) []FlatRow {
	lntIndex := make(map[string]*LNodeType, len(s.DataTypeTemplates.LNodeTypes))
	for i := range s.DataTypeTemplates.LNodeTypes {
		lnt := &s.DataTypeTemplates.LNodeTypes[i]
		lntIndex[lnt.ID] = lnt
	}

	dotIndex := make(map[string]*DOType, len(s.DataTypeTemplates.DOTypes))
	for i := range s.DataTypeTemplates.DOTypes {
		dot := &s.DataTypeTemplates.DOTypes[i]
		dotIndex[dot.ID] = dot
	}

	datIndex := make(map[string]*DAType, len(s.DataTypeTemplates.DATypes))
	for i := range s.DataTypeTemplates.DATypes {
		dat := &s.DataTypeTemplates.DATypes[i]
		datIndex[dat.ID] = dat
	}

	var rows []FlatRow

	for _, ied := range s.IEDs {
		for _, ap := range ied.AccessPoints {
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				flattenLD(&rows, ied.Name, ap.Name, ld, lntIndex, dotIndex, datIndex)
			}
		}
	}

	return rows
}

func flattenLD(rows *[]FlatRow, iedName, apName string, ld LDevice,
	lntIndex map[string]*LNodeType, dotIndex map[string]*DOType, datIndex map[string]*DAType) {

	if ld.LN0 != nil {
		flattenLN(rows, iedName, apName, ld.Inst, *ld.LN0, lntIndex, dotIndex, datIndex)
	}
	for _, ln := range ld.LNs {
		flattenLN(rows, iedName, apName, ld.Inst, ln, lntIndex, dotIndex, datIndex)
	}
}

func flattenLN(rows *[]FlatRow, iedName, apName, ldInst string, ln LN,
	lntIndex map[string]*LNodeType, dotIndex map[string]*DOType, datIndex map[string]*DAType) {

	lnName := ln.Prefix + ln.LNClass + ln.Inst

	lnt, ok := lntIndex[ln.LNType]
	if !ok {
		*rows = append(*rows, FlatRow{
			IED: iedName, AccessPoint: apName,
			LD: ldInst, LN: lnName,
			Status: "unresolved-lntype",
		})
		return
	}

	for _, do := range lnt.DOs {
		dot, ok := dotIndex[do.Type]
		if !ok {
			*rows = append(*rows, FlatRow{
				IED: iedName, AccessPoint: apName,
				LD: ldInst, LN: lnName,
				Path: do.Name, Desc: do.Desc,
				Status: "unresolved-dotype",
			})
			continue
		}

		flattenDOType(rows, iedName, apName, ldInst, lnName, do.Name, dot, dotIndex, datIndex)
	}
}

func flattenDOType(rows *[]FlatRow, iedName, apName, ldInst, lnName, prefix string,
	dot *DOType, dotIndex map[string]*DOType, datIndex map[string]*DAType) {

	visited := make(map[string]bool)
	flattenDOTypeRec(rows, iedName, apName, ldInst, lnName, prefix, dot, dotIndex, datIndex, visited)
}

func flattenDOTypeRec(rows *[]FlatRow, iedName, apName, ldInst, lnName, prefix string,
	dot *DOType, dotIndex map[string]*DOType, datIndex map[string]*DAType, visitedDO map[string]bool) {

	for _, da := range dot.DAs {
		flattenDA(rows, iedName, apName, ldInst, lnName, prefix, da, dot.CDC, datIndex)
	}

	for _, sdo := range dot.SDOs {
		if visitedDO[sdo.Type] {
			continue
		}
		subDOT, ok := dotIndex[sdo.Type]
		if !ok {
			continue
		}
		visitedDO[sdo.Type] = true
		flattenDOTypeRec(rows, iedName, apName, ldInst, lnName, prefix+"."+sdo.Name, subDOT, dotIndex, datIndex, visitedDO)
		delete(visitedDO, sdo.Type)
	}
}

func flattenDA(rows *[]FlatRow, iedName, apName, ldInst, lnName, prefix string,
	da DA, cdc string, datIndex map[string]*DAType) {

	path := prefix + "." + da.Name

	if da.BType == "Struct" && da.Type != "" {
		dat, ok := datIndex[da.Type]
		if ok {
			visited := make(map[string]bool)
			visited[da.Type] = true
			flattenDATypeRec(rows, iedName, apName, ldInst, lnName, path, da.FC, cdc, dat, datIndex, visited)
			return
		}
	}

	*rows = append(*rows, FlatRow{
		IED: iedName, AccessPoint: apName,
		LD: ldInst, LN: lnName,
		Path: path, FC: da.FC,
		BType: da.BType, CDC: cdc,
		Desc: da.Desc,
	})
}

func flattenDATypeRec(rows *[]FlatRow, iedName, apName, ldInst, lnName, prefix, fc, cdc string,
	dat *DAType, datIndex map[string]*DAType, visitedDA map[string]bool) {

	for _, bda := range dat.BDAs {
		path := prefix + "." + bda.Name

		if bda.BType == "Struct" && bda.Type != "" {
			if visitedDA[bda.Type] {
				continue
			}
			subDAT, ok := datIndex[bda.Type]
			if ok {
				visitedDA[bda.Type] = true
				flattenDATypeRec(rows, iedName, apName, ldInst, lnName, path, fc, cdc, subDAT, datIndex, visitedDA)
				delete(visitedDA, bda.Type)
				continue
			}
		}

		*rows = append(*rows, FlatRow{
			IED: iedName, AccessPoint: apName,
			LD: ldInst, LN: lnName,
			Path: path, FC: fc,
			BType: bda.BType, CDC: cdc,
			Desc: bda.Desc,
		})
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
						SmpRate: sv.SmpRate, NofASDU: sv.NofASDU,
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
