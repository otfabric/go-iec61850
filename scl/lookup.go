package scl

// FindIED returns the IED with the given name, or nil if not found.
func (s *SCL) FindIED(name string) *IED {
	for i := range s.IEDs {
		if s.IEDs[i].Name == name {
			return &s.IEDs[i]
		}
	}
	return nil
}

// FindLDevice returns the first LDevice with the given inst across
// all IEDs and access points, or nil if not found.
func (s *SCL) FindLDevice(inst string) *LDevice {
	for i := range s.IEDs {
		if ld := s.IEDs[i].FindLDevice(inst); ld != nil {
			return ld
		}
	}
	return nil
}

// FindLDevice returns the first LDevice with the given inst in this
// IED, or nil if not found.
func (ied *IED) FindLDevice(inst string) *LDevice {
	for i := range ied.AccessPoints {
		if ied.AccessPoints[i].Server == nil {
			continue
		}
		for j := range ied.AccessPoints[i].Server.LDevices {
			if ied.AccessPoints[i].Server.LDevices[j].Inst == inst {
				return &ied.AccessPoints[i].Server.LDevices[j]
			}
		}
	}
	return nil
}

// FindLN returns the first logical node matching prefix+lnClass+inst
// within this logical device. For LN0, use prefix="" lnClass="LLN0"
// inst="".
func (ld *LDevice) FindLN(prefix, lnClass, inst string) *LN {
	if ld.LN0 != nil && ld.LN0.Prefix == prefix && ld.LN0.LNClass == lnClass && ld.LN0.Inst == inst {
		return ld.LN0
	}
	for i := range ld.LNs {
		ln := &ld.LNs[i]
		if ln.Prefix == prefix && ln.LNClass == lnClass && ln.Inst == inst {
			return ln
		}
	}
	return nil
}

// FindLNodeType returns the LNodeType with the given ID, or nil.
func (s *SCL) FindLNodeType(id string) *LNodeType {
	for i := range s.DataTypeTemplates.LNodeTypes {
		if s.DataTypeTemplates.LNodeTypes[i].ID == id {
			return &s.DataTypeTemplates.LNodeTypes[i]
		}
	}
	return nil
}

// FindDOType returns the DOType with the given ID, or nil.
func (s *SCL) FindDOType(id string) *DOType {
	for i := range s.DataTypeTemplates.DOTypes {
		if s.DataTypeTemplates.DOTypes[i].ID == id {
			return &s.DataTypeTemplates.DOTypes[i]
		}
	}
	return nil
}

// FindDAType returns the DAType with the given ID, or nil.
func (s *SCL) FindDAType(id string) *DAType {
	for i := range s.DataTypeTemplates.DATypes {
		if s.DataTypeTemplates.DATypes[i].ID == id {
			return &s.DataTypeTemplates.DATypes[i]
		}
	}
	return nil
}

// FindEnumType returns the EnumType with the given ID, or nil.
func (s *SCL) FindEnumType(id string) *EnumType {
	for i := range s.DataTypeTemplates.EnumTypes {
		if s.DataTypeTemplates.EnumTypes[i].ID == id {
			return &s.DataTypeTemplates.EnumTypes[i]
		}
	}
	return nil
}
