// SPDX-License-Identifier: MIT

package servermodel

import (
	"context"
	"strings"

	"github.com/otfabric/go-mms"
)

// LCBItemID returns the MMS item of a log control block:
// "<LN>$LG$<name>" (IEC 61850-8-1 §17.3.3.2, FC LG).
func LCBItemID(lnName, name string) string {
	return lnName + "$LG$" + name
}

// LCBWritableFields are the LCB attributes a client may write
// (IEC 61850-8-1 Table 65: LogEna, LogRef, DatSet, TrgOps and IntgPd are
// r/w; OldEntrTm, NewEntrTm, OldEnt and NewEnt are r).
var LCBWritableFields = map[string]bool{"LogEna": true, "LogRef": true, "DatSet": true, "TrgOps": true, "IntgPd": true}

// LogRef returns the initial LogRef of l in LN lnName of LD ldName:
// "<Domain>/<LNName>$<journal>" (IEC 61850-8-1 §17.3.3.3.2).
func (l *LogDef) LogRef(ldName, lnName string) string {
	domain, journal := l.JournalName(ldName, lnName)
	return domain + "/" + journal
}

// buildLCBFields returns the LCB components in the IEC 61850-8-1
// Table 65 order. "If there are no journal entries ... the binary-time
// values shall be 0" and OldEnt/NewEnt are equal and NULL (8 zero
// octets, §17.3.3.3.4/5); the journal engine updates them.
func buildLCBFields(l *LogDef, ldName, lnName string) []rcbField {
	return []rcbField{
		{"LogEna", mms.TypeSpec{Type: mms.ValueTypeBoolean}, mms.NewBoolean(l.LogEna)},
		{"LogRef", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129}, mms.NewVisibleString(l.LogRef(ldName, lnName))},
		{"DatSet", mms.TypeSpec{Type: mms.ValueTypeVisibleString, Size: 129}, mms.NewVisibleString(QualifiedDatSet(ldName, lnName, l.DatSet))},
		{"OldEntrTm", mms.TypeSpec{Type: mms.ValueTypeBinaryTime}, emptyBinaryTime()},
		{"NewEntrTm", mms.TypeSpec{Type: mms.ValueTypeBinaryTime}, emptyBinaryTime()},
		{"OldEnt", mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 8}, mms.NewOctetString(make([]byte, 8))},
		{"NewEnt", mms.TypeSpec{Type: mms.ValueTypeOctetString, Size: 8}, mms.NewOctetString(make([]byte, 8))},
		{"TrgOps", mms.TypeSpec{Type: mms.ValueTypeBitString, Size: 6}, encodeTrgOpsDef(l.TrgOps)},
		{"IntgPd", mms.TypeSpec{Type: mms.ValueTypeUnsigned, Size: 32}, mms.NewUnsigned(uint64(l.IntgPd))},
	}
}

// registerLCB registers the LCB structure and its components. Writes of
// the r/w components go through the write interceptor (the journal
// engine applies the IEC 61850-8-1 §17.3.4.3 rules); the read-only
// components and the whole structure answer object-access-denied.
func registerLCB(srv Registrar, vs *ValueStore, ldName, lnName string, l *LogDef) error {
	return registerCtrlBlock(srv, vs, ldName, LCBItemID(lnName, l.Name), "LCB", buildLCBFields(l, ldName, lnName), LCBWritableFields)
}

// registerLGGroup registers the "LN$LG" FC-group variable. ok is false
// when the LN has no LCBs.
func registerLGGroup(srv Registrar, vs *ValueStore, ldName string, ln *LogicalNode) (mms.TypeSpecElement, func(context.Context) (*mms.Value, error), bool, error) {
	names := make([]string, len(ln.Logs))
	structs := make([]func() (mms.TypeSpec, func(context.Context) (*mms.Value, error)), len(ln.Logs))
	for i := range ln.Logs {
		l := &ln.Logs[i]
		names[i] = l.Name
		structs[i] = func() (mms.TypeSpec, func(context.Context) (*mms.Value, error)) {
			return cbStruct(vs, buildLCBFields(l, ldName, ln.Name), ldName, LCBItemID(ln.Name, l.Name))
		}
	}
	return registerFCGroup(srv, ldName, ln.Name, "LG", names, structs)
}

// ParseLCBItem splits "<LN>$LG$<name>$<attr>" into the LCB item and the
// attribute.
func ParseLCBItem(itemID string) (lcbItem, attr string, ok bool) {
	i := strings.Index(itemID, "$LG$")
	if i <= 0 {
		return "", "", false
	}
	rest := itemID[i+4:]
	j := strings.IndexByte(rest, '$')
	if j <= 0 || j == len(rest)-1 || strings.Contains(rest[j+1:], "$") {
		return "", "", false
	}
	return itemID[:i+4+j], rest[j+1:], true
}
