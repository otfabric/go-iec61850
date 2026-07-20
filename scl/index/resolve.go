// SPDX-License-Identifier: MIT

package index

import "github.com/otfabric/go-iec61850/scl"

// FindIED returns the IED with the given name, or nil.
func (idx *Index) FindIED(name string) *scl.IED {
	return idx.IEDs[name]
}

// FindAccessPoint returns the AccessPoint for the given IED and AP name.
func (idx *Index) FindAccessPoint(ied, ap string) *scl.AccessPoint {
	return idx.AccessPoints[AccessPointKey{IED: ied, AP: ap}]
}

// FindLDevice returns the LDevice for the given IED and instance.
func (idx *Index) FindLDevice(ied, ldInst string) *scl.LDevice {
	return idx.LDevices[LDeviceKey{IED: ied, LDInst: ldInst}]
}

// FindLN returns the logical node for the given key components.
func (idx *Index) FindLN(ied, ldInst, prefix, lnClass, inst string) *scl.LN {
	return idx.LNs[LNKey{
		IED: ied, LDInst: ldInst,
		Prefix: prefix, LNClass: lnClass, Inst: inst,
	}]
}

// FindLNodeType returns the LNodeType with the given ID, or nil.
func (idx *Index) FindLNodeType(id string) *scl.LNodeType {
	return idx.LNodeTypes[id]
}

// FindDOType returns the DOType with the given ID, or nil.
func (idx *Index) FindDOType(id string) *scl.DOType {
	return idx.DOTypes[id]
}

// FindDAType returns the DAType with the given ID, or nil.
func (idx *Index) FindDAType(id string) *scl.DAType {
	return idx.DATypes[id]
}

// FindEnumType returns the EnumType with the given ID, or nil.
func (idx *Index) FindEnumType(id string) *scl.EnumType {
	return idx.EnumTypes[id]
}

// FindDataSet returns the DataSet owned by the given LN with the
// specified name.
func (idx *Index) FindDataSet(ied, ldInst, prefix, lnClass, lnInst, name string) *scl.DataSet {
	return idx.DataSets[DataSetKey{
		IED: ied, LDInst: ldInst,
		Prefix: prefix, LNClass: lnClass, LNInst: lnInst,
		Name: name,
	}]
}

// FindReport returns the ReportControl owned by the given LN with
// the specified name.
func (idx *Index) FindReport(ied, ldInst, prefix, lnClass, lnInst, name string) *scl.ReportControl {
	return idx.Reports[ControlKey{
		IED: ied, LDInst: ldInst,
		Prefix: prefix, LNClass: lnClass, LNInst: lnInst,
		Name: name,
	}]
}

// FindGSEControl returns the GSEControl owned by the given LN with
// the specified name.
func (idx *Index) FindGSEControl(ied, ldInst, prefix, lnClass, lnInst, name string) *scl.GSEControl {
	return idx.GSEControls[ControlKey{
		IED: ied, LDInst: ldInst,
		Prefix: prefix, LNClass: lnClass, LNInst: lnInst,
		Name: name,
	}]
}

// FindSMVControl returns the SMVControl owned by the given LN with
// the specified name.
func (idx *Index) FindSMVControl(ied, ldInst, prefix, lnClass, lnInst, name string) *scl.SMVControl {
	return idx.SMVControls[ControlKey{
		IED: ied, LDInst: ldInst,
		Prefix: prefix, LNClass: lnClass, LNInst: lnInst,
		Name: name,
	}]
}

// FindConnectedAP returns the ConnectedAP for the given IED and AP.
func (idx *Index) FindConnectedAP(ied, ap string) *scl.ConnectedAP {
	return idx.ConnectedAPs[AccessPointKey{IED: ied, AP: ap}]
}

// ResolveLNType resolves the LNodeType referenced by a logical node.
// Returns nil if the LN's lnType does not match any template.
func (idx *Index) ResolveLNType(ln *scl.LN) *scl.LNodeType {
	if ln == nil {
		return nil
	}
	return idx.LNodeTypes[ln.LNType]
}
