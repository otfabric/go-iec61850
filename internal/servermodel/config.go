// SPDX-License-Identifier: MIT

package servermodel

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// GenerateConfig writes a human-readable configuration summary of the
// server model. This is intended for `iec61850ctl server generate-config`
// style CLI output — it is an adapter/output format, not a canonical
// internal representation.
//
// The output format is JSON for easy consumption by tooling.
func GenerateConfig(w io.Writer, m *Model) error {
	if m == nil {
		return fmt.Errorf("servermodel: nil model")
	}
	cfg := modelToConfig(m)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}

// ServerConfig is the JSON-serializable config output.
type ServerConfig struct {
	LogicalDevices []LDConfig `json:"logicalDevices"`
}

// LDConfig describes a logical device in the config output.
type LDConfig struct {
	Name         string     `json:"name"`
	LogicalNodes []LNConfig `json:"logicalNodes"`
}

// LNConfig describes a logical node in the config output.
type LNConfig struct {
	Name     string      `json:"name"`
	LNClass  string      `json:"lnClass"`
	Objects  []DOConfig  `json:"dataObjects,omitempty"`
	DataSets []DSConfig  `json:"dataSets,omitempty"`
	Reports  []RptConfig `json:"reports,omitempty"`
}

// DOConfig describes a data object in the config output.
type DOConfig struct {
	Name       string     `json:"name"`
	CDC        string     `json:"cdc,omitempty"`
	Children   []DOConfig `json:"children,omitempty"`
	Attributes []DAConfig `json:"attributes,omitempty"`
}

// DAConfig describes a data attribute in the config output.
type DAConfig struct {
	Name         string     `json:"name"`
	FC           string     `json:"fc"`
	BType        string     `json:"bType"`
	InitialValue string     `json:"initialValue,omitempty"`
	Children     []DAConfig `json:"children,omitempty"`
}

// DSConfig describes a dataset in the config output.
type DSConfig struct {
	Name    string           `json:"name"`
	Members []DSMemberConfig `json:"members"`
}

// DSMemberConfig describes a dataset member in the config output.
type DSMemberConfig struct {
	LDInst string `json:"ldInst,omitempty"`
	LNName string `json:"lnName"`
	DOPath string `json:"doPath"`
	FC     string `json:"fc"`
}

// RptConfig describes a report control block in the config output.
type RptConfig struct {
	Name     string `json:"name"`
	RptID    string `json:"rptID,omitempty"`
	DatSet   string `json:"datSet"`
	ConfRev  uint32 `json:"confRev"`
	Buffered bool   `json:"buffered"`
	BufTime  uint32 `json:"bufTime,omitempty"`
	IntgPd   uint32 `json:"intgPd,omitempty"`
}

func modelToConfig(m *Model) ServerConfig {
	cfg := ServerConfig{}
	for _, ld := range m.LogicalDevices {
		ldCfg := LDConfig{Name: ld.Name}
		for _, ln := range ld.LogicalNodes {
			lnCfg := LNConfig{
				Name:    ln.Name,
				LNClass: ln.LNClass,
			}
			for _, obj := range ln.DataObjects {
				lnCfg.Objects = append(lnCfg.Objects, doToConfig(&obj))
			}
			for _, ds := range ln.DataSets {
				lnCfg.DataSets = append(lnCfg.DataSets, dsToConfig(&ds))
			}
			for _, rpt := range ln.Reports {
				lnCfg.Reports = append(lnCfg.Reports, rptToConfig(&rpt))
			}
			ldCfg.LogicalNodes = append(ldCfg.LogicalNodes, lnCfg)
		}
		cfg.LogicalDevices = append(cfg.LogicalDevices, ldCfg)
	}
	return cfg
}

func doToConfig(obj *DataObject) DOConfig {
	c := DOConfig{Name: obj.Name, CDC: obj.CDC}
	for _, child := range obj.Children {
		c.Children = append(c.Children, doToConfig(&child))
	}
	for _, attr := range obj.Attributes {
		c.Attributes = append(c.Attributes, daToConfig(&attr))
	}
	return c
}

func daToConfig(attr *DataAttribute) DAConfig {
	c := DAConfig{
		Name:         attr.Name,
		FC:           attr.FC,
		BType:        attr.BType,
		InitialValue: attr.InitialValue,
	}
	for _, child := range attr.Children {
		c.Children = append(c.Children, daToConfig(&child))
	}
	return c
}

func dsToConfig(ds *DataSetDef) DSConfig {
	c := DSConfig{Name: ds.Name}
	for _, m := range ds.Members {
		c.Members = append(c.Members, DSMemberConfig(m))
	}
	return c
}

func rptToConfig(rpt *ReportDef) RptConfig {
	return RptConfig{
		Name:     rpt.Name,
		RptID:    rpt.RptID,
		DatSet:   rpt.DatSet,
		ConfRev:  rpt.ConfRev,
		Buffered: rpt.Buffered,
		BufTime:  rpt.BufTime,
		IntgPd:   rpt.IntgPd,
	}
}

// GenerateMMS writes a summary of the MMS variable registry that would
// be created from the model. This shows the domain → item ID mapping
// without actually registering anything.
//
// The output includes leaf variables, named variable lists (datasets),
// and report control block pseudo-variables with their store keys.
func GenerateMMS(w io.Writer, m *Model) error {
	if m == nil {
		return fmt.Errorf("servermodel: nil model")
	}
	for _, ld := range m.LogicalDevices {
		if _, err := fmt.Fprintf(w, "domain: %s\n", ld.Name); err != nil {
			return err
		}
		for _, ln := range ld.LogicalNodes {
			for _, obj := range ln.DataObjects {
				if err := printDO(w, ld.Name, ln.Name, nil, &obj); err != nil {
					return err
				}
			}
			for _, ds := range ln.DataSets {
				nvlName := ln.Name + "$" + ds.Name
				if _, err := fmt.Fprintf(w, "  nvl: %s/%s\n", ld.Name, nvlName); err != nil {
					return err
				}
			}
			for _, rpt := range ln.Reports {
				if err := printRCB(w, ld.Name, ln.Name, &rpt); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func printDO(w io.Writer, ldName, lnName string, parentPath []string, obj *DataObject) error {
	for _, child := range obj.Children {
		childPath := append(append([]string(nil), parentPath...), obj.Name)
		if err := printDO(w, ldName, lnName, childPath, &child); err != nil {
			return err
		}
	}
	for _, attr := range obj.Attributes {
		attrPath := append(append([]string(nil), parentPath...), obj.Name)
		if err := printDA(w, ldName, lnName, attrPath, &attr); err != nil {
			return err
		}
	}
	return nil
}

func printRCB(w io.Writer, ldName, lnName string, rpt *ReportDef) error {
	prefix := "RP"
	if rpt.Buffered {
		prefix = "BR"
	}
	rcbItemID := lnName + "$" + prefix + "$" + rpt.Name
	storeKey := ldName + "/" + rcbItemID

	kind := "urcb"
	if rpt.Buffered {
		kind = "brcb"
	}
	if _, err := fmt.Fprintf(w, "  %s: %s/%s  store=%s  datSet=%s\n",
		kind, ldName, rcbItemID, storeKey, rpt.DatSet); err != nil {
		return err
	}

	subfields := []string{"RptID", "RptEna", "DatSet", "ConfRev", "OptFlds", "BufTm", "SqNum", "TrgOps", "IntgPd", "GI"}
	if !rpt.Buffered {
		subfields = append(subfields, "Resv")
	} else {
		subfields = append(subfields, "PurgeBuf", "EntryID", "TimeOfEntry")
	}
	for _, sf := range subfields {
		subItemID := rcbItemID + "$" + sf
		subKey := ldName + "/" + subItemID
		if _, err := fmt.Fprintf(w, "    sub: %s  store=%s\n", subItemID, subKey); err != nil {
			return err
		}
	}
	return nil
}

func printDA(w io.Writer, ldName, lnName string, path []string, attr *DataAttribute) error {
	if len(attr.Children) > 0 {
		childPath := append(append([]string(nil), path...), attr.Name)
		for _, child := range attr.Children {
			fc := child.FC
			if fc == "" {
				fc = attr.FC
			}
			childCopy := child
			childCopy.FC = fc
			if err := printDA(w, ldName, lnName, childPath, &childCopy); err != nil {
				return err
			}
		}
		return nil
	}

	fullPath := append(append([]string(nil), path...), attr.Name)
	itemID := lnName + "$" + attr.FC + "$" + strings.Join(fullPath, "$")
	_, err := fmt.Fprintf(w, "  var: %s/%s  [%s] %s\n", ldName, itemID, attr.FC, attr.BType)
	return err
}
