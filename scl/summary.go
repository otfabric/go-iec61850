// SPDX-License-Identifier: MIT

package scl

// Summary holds aggregate counts extracted from a parsed SCL model.
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

// Summarize walks the SCL model and returns aggregate counts.
func Summarize(s *SCL) Summary {
	var sum Summary

	for _, sub := range s.Substations {
		sum.Substations++
		for _, vl := range sub.VoltageLevels {
			sum.VoltageLevels++
			sum.Bays += len(vl.Bays)
		}
	}

	if s.Communication != nil {
		for _, sn := range s.Communication.SubNetworks {
			sum.ConnectedAPs += len(sn.ConnectedAPs)
		}
	}

	for _, ied := range s.IEDs {
		sum.IEDs++
		sum.PrivateCount += len(ied.Private)
		if ied.Services != nil {
			sum.HasServices = true
		}
		for _, ap := range ied.AccessPoints {
			sum.AccessPoints++
			sum.PrivateCount += len(ap.Private)
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				sum.LogicalDevices++
				sum.PrivateCount += len(ld.Private)
				if ld.LN0 != nil {
					sum.LogicalNodes++
					sum.LN0Count++
					sum.DataSets += len(ld.LN0.DataSets)
					sum.ReportControls += len(ld.LN0.Reports)
					sum.GSEControls += len(ld.LN0.GSEControls)
					sum.SMVControls += len(ld.LN0.SMVControls)
					sum.LogControls += len(ld.LN0.Logs)
					sum.PrivateCount += len(ld.LN0.Private)
				}
				for _, ln := range ld.LNs {
					sum.LogicalNodes++
					sum.DataSets += len(ln.DataSets)
					sum.ReportControls += len(ln.Reports)
					sum.LogControls += len(ln.Logs)
					sum.PrivateCount += len(ln.Private)
				}
			}
		}
	}

	sum.LNodeTypes = len(s.DataTypeTemplates.LNodeTypes)
	sum.DOTypes = len(s.DataTypeTemplates.DOTypes)
	sum.DATypes = len(s.DataTypeTemplates.DATypes)
	sum.EnumTypes = len(s.DataTypeTemplates.EnumTypes)

	return sum
}
