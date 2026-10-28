// SPDX-License-Identifier: MIT

package goose

import (
	"errors"
	"fmt"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/scl"
)

// ControlBlock is a GOOSE control block resolved from SCL: the
// GSEControl, its GSE address (ConnectedAP) and the dataset layout.
// References use the on-wire form of IEC 61850-8-1, where the MMS
// domain is the IED name followed by the LD instance.
type ControlBlock struct {
	IEDName, APName, LDInst, LNName, Name string

	// GocbRef is "<IED><LD>/LLN0$GO$<name>".
	GocbRef string

	// DatSet is "<IED><LD>/LLN0$<dataset>".
	DatSet string

	// GoID is the GSEControl appID attribute, or GocbRef when empty.
	GoID string

	ConfRev   uint32
	FixedOffs bool

	// HasAddress is false when the Communication section has no valid
	// GSE address for the block; Dst/AppID/VLAN are then zero.
	HasAddress bool
	Dst        MAC
	AppID      uint16
	VLAN       *VLAN // nil when the address has no VLAN parameters

	// MinTime and MaxTime are zero when not configured.
	MinTime, MaxTime time.Duration

	// Members describes the dataset entries in order.
	Members []Member
}

// Member is one dataset entry of a [ControlBlock].
type Member struct {
	// Ref is "<LDInst>/<LN>.<DO>[.<DA>...]".
	Ref string

	// FC is the functional constraint.
	FC string

	// Type is the MMS type of the value sent in allData.
	Type mms.TypeSpec
}

// ControlBlocksFromSCL returns every GOOSE control block of every IED
// access point with a Server in doc. Blocks that cannot be resolved are
// skipped and reported in the returned error (which wraps [ErrSCL]);
// the blocks that could be resolved are returned regardless.
func ControlBlocksFromSCL(doc *scl.SCL) ([]ControlBlock, error) {
	if doc == nil {
		return nil, fmt.Errorf("%w: nil SCL document", ErrSCL)
	}
	var out []ControlBlock
	var errs []error
	for i := range doc.IEDs {
		ied := &doc.IEDs[i]
		for j := range ied.AccessPoints {
			ap := &ied.AccessPoints[j]
			if ap.Server == nil {
				continue
			}
			cbs, err := blocksForAP(doc, ied.Name, ap.Name)
			out = append(out, cbs...)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}

// ControlBlockFromSCL returns the GOOSE control block cbName in LD
// ldInst of IED iedName.
func ControlBlockFromSCL(doc *scl.SCL, iedName, ldInst, cbName string) (*ControlBlock, error) {
	all, err := ControlBlocksFromSCL(doc)
	for i := range all {
		cb := &all[i]
		if cb.IEDName == iedName && cb.LDInst == ldInst && cb.Name == cbName {
			return cb, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: %w: GoCB %s/%s/%s", ErrSCL, ErrNotFound, iedName, ldInst, cbName)
}

func blocksForAP(doc *scl.SCL, iedName, apName string) ([]ControlBlock, error) {
	model, err := servermodel.FromSCL(doc, iedName, apName)
	if err != nil {
		return nil, fmt.Errorf("%w: IED %s AP %s: %w", ErrSCL, iedName, apName, err)
	}
	var out []ControlBlock
	var errs []error
	for _, ld := range model.LogicalDevices {
		for _, ln := range ld.LogicalNodes {
			for _, g := range ln.GoCBs {
				cb, err := newControlBlock(model, iedName, apName, ld.Name, ln.Name, &g)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				out = append(out, *cb)
			}
		}
	}
	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}

func newControlBlock(m *servermodel.Model, iedName, apName, ldName, lnName string, g *servermodel.GoCBDef) (*ControlBlock, error) {
	domain := iedName + ldName
	cb := &ControlBlock{
		IEDName: iedName, APName: apName, LDInst: ldName, LNName: lnName, Name: g.Name,
		GocbRef:    domain + "/" + servermodel.GoCBItemID(lnName, g.Name),
		DatSet:     servermodel.QualifiedDatSet(domain, lnName, g.DatSet),
		GoID:       g.GoID,
		ConfRev:    g.ConfRev,
		FixedOffs:  g.FixedOffs,
		HasAddress: g.HasAddress,
		Dst:        MAC(g.DstAddr),
		AppID:      g.AppID,
		MinTime:    time.Duration(g.MinTime) * time.Millisecond,
		MaxTime:    time.Duration(g.MaxTime) * time.Millisecond,
	}
	if cb.GoID == "" {
		cb.GoID = cb.GocbRef
	}
	if g.HasVLAN {
		cb.VLAN = &VLAN{Priority: g.VLANPriority, ID: g.VLANID}
	}
	if g.DatSet == "" {
		return cb, nil
	}
	ds, _ := m.FindDataSet(ldName, g.DatSet)
	if ds == nil {
		return nil, fmt.Errorf("%w: GoCB %s: dataset %q not found", ErrSCL, cb.GocbRef, g.DatSet)
	}
	for _, mem := range ds.Members {
		n, err := m.ResolveMember(ldName, mem)
		if err != nil {
			return nil, fmt.Errorf("%w: GoCB %s: %w", ErrSCL, cb.GocbRef, err)
		}
		ld := mem.LDInst
		if ld == "" {
			ld = ldName
		}
		cb.Members = append(cb.Members, Member{Ref: ld + "/" + mem.LNName + "." + mem.DOPath, FC: mem.FC, Type: n.Type})
	}
	return cb, nil
}

// MemberTypes returns the MMS type of every member, for
// [SubscriptionConfig.Members].
func (cb *ControlBlock) MemberTypes() []mms.TypeSpec {
	out := make([]mms.TypeSpec, len(cb.Members))
	for i, m := range cb.Members {
		out[i] = m.Type
	}
	return out
}

// DefaultValues returns a zero value for every member (false, 0, empty
// strings, the 1984 epoch for TimeOfDay).
func (cb *ControlBlock) DefaultValues() []*mms.Value {
	out := make([]*mms.Value, len(cb.Members))
	for i, m := range cb.Members {
		out[i] = ZeroValue(m.Type)
	}
	return out
}

// ZeroValue returns the zero value of an MMS type that can be encoded
// in GOOSE.
func ZeroValue(ts mms.TypeSpec) *mms.Value {
	switch ts.Type {
	case mms.ValueTypeStructure:
		elems := make([]*mms.Value, len(ts.Elements))
		for i, e := range ts.Elements {
			elems[i] = ZeroValue(e.Type)
		}
		return mms.NewStructure(elems)
	case mms.ValueTypeArray:
		elems := make([]*mms.Value, ts.Count)
		for i := range elems {
			if ts.Element != nil {
				elems[i] = ZeroValue(*ts.Element)
			} else {
				elems[i] = mms.NewInteger(0)
			}
		}
		return mms.NewArray(elems)
	case mms.ValueTypeBinaryTime:
		return mms.NewBinaryTime(epoch1984.UnixMilli())
	case mms.ValueTypeBitString:
		return mms.NewBitStringWithLength(make([]byte, (ts.Size+7)/8), ts.Size)
	case mms.ValueTypeUTCTime:
		return mms.NewUTCTimeWithQuality(time.Unix(0, 0).UTC(), timeQuality)
	default:
		return ts.DefaultValue()
	}
}

// PublisherConfig returns a publisher configuration for the block with
// default member values. Callers usually set Values, Logger and Metrics
// before calling [NewPublisher]. It fails with [ErrInvalidConfig] when
// the block has no GSE address.
func (cb *ControlBlock) PublisherConfig() (PublisherConfig, error) {
	if !cb.HasAddress {
		return PublisherConfig{}, configErr("GSE", "GoCB %s has no GSE address", cb.GocbRef)
	}
	var vlan *VLAN
	if cb.VLAN != nil {
		v := *cb.VLAN
		vlan = &v
	}
	return PublisherConfig{
		Dst: cb.Dst, VLAN: vlan, AppID: cb.AppID, GocbRef: cb.GocbRef, DatSet: cb.DatSet,
		GoID: cb.GoID, ConfRev: cb.ConfRev, MinTime: cb.MinTime, MaxTime: cb.MaxTime,
		NdsCom: cb.DatSet == "", Values: cb.DefaultValues(),
	}, nil
}

// SubscriptionConfig returns a subscription that filters on the block's
// destination address, APPID and GocbRef and validates datSet, goID,
// confRev and the member layout.
func (cb *ControlBlock) SubscriptionConfig() SubscriptionConfig {
	c := SubscriptionConfig{GocbRef: cb.GocbRef, DatSet: cb.DatSet, GoID: cb.GoID, ConfRev: cb.ConfRev, Members: cb.MemberTypes()}
	if cb.HasAddress {
		c.Dst = cb.Dst
		c.AppIDs = []uint16{cb.AppID}
	}
	return c
}
