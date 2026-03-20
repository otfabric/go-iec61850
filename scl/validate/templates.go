package validate

import (
	"fmt"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// Templates checks data type template cross-references:
// LNodeType→DOType, DOType→DAType/EnumType/SDO, DAType→DAType/EnumType.
func Templates(s *scl.SCL, idx *index.Index) []scl.Diagnostic {
	var diags []scl.Diagnostic

	for _, lnt := range s.DataTypeTemplates.LNodeTypes {
		path := fmt.Sprintf("LNodeType[%s]", lnt.ID)
		for _, do := range lnt.DOs {
			if idx.FindDOType(do.Type) == nil {
				diags = append(diags, scl.Diagnostic{
					Severity: scl.DiagError, Code: "missing-dotype",
					Path:    path + ".DO[" + do.Name + "]",
					Message: fmt.Sprintf("references DOType %q which does not exist", do.Type),
				})
			}
		}
	}

	for _, dot := range s.DataTypeTemplates.DOTypes {
		path := fmt.Sprintf("DOType[%s]", dot.ID)
		for _, da := range dot.DAs {
			diags = append(diags, checkTypeRef(idx, path+".DA["+da.Name+"]", da.BType, da.Type)...)
		}
		for _, sdo := range dot.SDOs {
			if idx.FindDOType(sdo.Type) == nil {
				diags = append(diags, scl.Diagnostic{
					Severity: scl.DiagError, Code: "missing-dotype",
					Path:    path + ".SDO[" + sdo.Name + "]",
					Message: fmt.Sprintf("references DOType %q which does not exist", sdo.Type),
				})
			}
		}
	}

	for _, dat := range s.DataTypeTemplates.DATypes {
		path := fmt.Sprintf("DAType[%s]", dat.ID)
		for _, bda := range dat.BDAs {
			diags = append(diags, checkTypeRef(idx, path+".BDA["+bda.Name+"]", bda.BType, bda.Type)...)
		}
	}

	return diags
}

func checkTypeRef(idx *index.Index, path, bType, typRef string) []scl.Diagnostic {
	if typRef == "" {
		return nil
	}
	switch bType {
	case "Struct":
		if idx.FindDAType(typRef) == nil {
			return []scl.Diagnostic{{
				Severity: scl.DiagError, Code: "missing-datype",
				Path: path, Message: fmt.Sprintf("references DAType %q which does not exist", typRef),
			}}
		}
	case "Enum":
		if idx.FindEnumType(typRef) == nil {
			return []scl.Diagnostic{{
				Severity: scl.DiagError, Code: "missing-enumtype",
				Path: path, Message: fmt.Sprintf("references EnumType %q which does not exist", typRef),
			}}
		}
	}
	return nil
}
