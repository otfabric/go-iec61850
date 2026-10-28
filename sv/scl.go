// SPDX-License-Identifier: MIT

package sv

import (
	"errors"
	"fmt"

	"github.com/otfabric/go-iec61850/internal/servermodel"
	"github.com/otfabric/go-iec61850/scl"
)

// ControlBlock is a multicast Sampled Values control block resolved
// from SCL: the SampledValueControl, its SMV address (ConnectedAP) and
// the dataset layout. References use the on-wire form of IEC 61850-9-2,
// where the MMS domain is the IED name followed by the LD instance.
type ControlBlock struct {
	IEDName, APName, LDInst, LNName, Name string

	// SvCBRef is "<IED><LD>/LLN0$MS$<name>".
	SvCBRef string

	// SvID is the smvID attribute (MsvID).
	SvID string

	// DatSet is "<IED><LD>/LLN0$<dataset>".
	DatSet string

	ConfRev uint32
	SmpRate uint16
	SmpMod  SmpMod
	NoASDU  int

	// Opts are the optional ASDU fields (SCL SmvOpts, MSVCB OptFlds).
	Opts OptFlds

	// HasAddress is false when the Communication section has no valid
	// SMV address for the block; Dst/AppID/VLAN are then zero.
	HasAddress bool
	Dst        MAC
	AppID      uint16
	VLAN       *VLAN // nil when the address has no VLAN parameters

	// Members describes the dataset entries in order; Layout is the
	// flattened sample layout of all their leaves.
	Members []Member
	Layout  Layout
}

// OptFlds are the optional fields of an SV control block (IEC 61850-8-1
// OptFlds, SCL SmvOpts). SampleSynchronized is reserved since Edition 2:
// smpSynch is always sent.
type OptFlds struct {
	RefreshTime, SampleSynchronized, SampleRate, DataSet, Security bool
}

// Member is one dataset entry of a [ControlBlock].
type Member struct {
	// Ref is "<LDInst>/<LN>.<DO>[.<DA>...]".
	Ref string

	// FC is the functional constraint.
	FC string

	// Kinds are the sample encodings of the member's leaves, in order.
	Kinds []Kind
}

// ControlBlocksFromSCL returns every SV control block of every IED
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
	return out, errors.Join(errs...)
}

// ControlBlockFromSCL returns the SV control block cbName in LD ldInst
// of IED iedName.
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
	return nil, fmt.Errorf("%w: %w: SvCB %s/%s/%s", ErrSCL, ErrNotFound, iedName, ldInst, cbName)
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
			for i := range ln.SvCBs {
				cb, err := newControlBlock(model, iedName, apName, ld.Name, ln.Name, &ln.SvCBs[i])
				if err != nil {
					errs = append(errs, err)
					continue
				}
				out = append(out, *cb)
			}
		}
	}
	return out, errors.Join(errs...)
}

func newControlBlock(m *servermodel.Model, iedName, apName, ldName, lnName string, d *servermodel.SvCBDef) (*ControlBlock, error) {
	domain := iedName + ldName
	cb := &ControlBlock{
		IEDName: iedName, APName: apName, LDInst: ldName, LNName: lnName, Name: d.Name,
		SvCBRef:    domain + "/" + servermodel.SvCBItemID(lnName, d.Name),
		SvID:       d.SvID,
		DatSet:     servermodel.QualifiedDatSet(domain, lnName, d.DatSet),
		ConfRev:    d.ConfRev,
		SmpRate:    d.SmpRate,
		SmpMod:     SmpMod(d.SmpMod),
		NoASDU:     int(d.NoASDU),
		Opts:       OptFlds(d.OptFlds),
		HasAddress: d.HasAddress,
		Dst:        MAC(d.DstAddr),
		AppID:      d.AppID,
	}
	if d.HasVLAN {
		cb.VLAN = &VLAN{Priority: d.VLANPriority, ID: d.VLANID}
	}
	if d.DatSet == "" {
		return nil, fmt.Errorf("%w: SvCB %s: no dataset", ErrSCL, cb.SvCBRef)
	}
	ds, _ := m.FindDataSet(ldName, d.DatSet)
	if ds == nil {
		return nil, fmt.Errorf("%w: SvCB %s: dataset %q not found", ErrSCL, cb.SvCBRef, d.DatSet)
	}
	for _, mem := range ds.Members {
		n, err := m.ResolveMember(ldName, mem)
		if err != nil {
			return nil, fmt.Errorf("%w: SvCB %s: %w", ErrSCL, cb.SvCBRef, err)
		}
		ld := mem.LDInst
		if ld == "" {
			ld = ldName
		}
		ref := ld + "/" + mem.LNName + "." + mem.DOPath
		kinds, err := leafKinds(n)
		if err != nil {
			return nil, fmt.Errorf("%w: SvCB %s: member %s: %w", ErrSCL, cb.SvCBRef, ref, err)
		}
		cb.Members = append(cb.Members, Member{Ref: ref, FC: mem.FC, Kinds: kinds})
		cb.Layout = append(cb.Layout, kinds...)
	}
	return cb, nil
}

// leafKinds returns the sample encoding of every leaf of a resolved
// dataset member. It fails with [ErrInvalidConfig] for a basic type SV
// cannot carry (strings, octet strings, ...).
func leafKinds(n *servermodel.MemberNode) ([]Kind, error) {
	leaves := n.Leaves()
	out := make([]Kind, len(leaves))
	for i, l := range leaves {
		k, ok := KindOf(l.BType)
		if !ok {
			return nil, configErr("Layout", "%s: basic type %q cannot be sampled", l.Name, l.BType)
		}
		out[i] = k
	}
	return out, nil
}

// PublisherConfig returns a publisher configuration for the block with
// a zero sample. Callers usually set Sample or Source, SmpSynch, Logger
// and Metrics before calling [NewPublisher]. It fails with
// [ErrInvalidConfig] when the block has no SMV address.
func (cb *ControlBlock) PublisherConfig() (PublisherConfig, error) {
	if !cb.HasAddress {
		return PublisherConfig{}, configErr("SMV", "SvCB %s has no SMV address", cb.SvCBRef)
	}
	var vlan *VLAN
	if cb.VLAN != nil {
		v := *cb.VLAN
		vlan = &v
	}
	c := PublisherConfig{
		Dst: cb.Dst, VLAN: vlan, AppID: cb.AppID, SvID: cb.SvID, ConfRev: cb.ConfRev,
		SmpRate: cb.SmpRate, SmpMod: cb.SmpMod, NoASDU: cb.NoASDU,
		RefrTm: cb.Opts.RefreshTime, SendSmpRate: cb.Opts.SampleRate,
		SendSmpMod: cb.Opts.SampleRate && cb.SmpMod != SmpPerPeriod,
		Layout:     append(Layout(nil), cb.Layout...),
	}
	if cb.Opts.DataSet {
		c.DatSet = cb.DatSet
	}
	return c, nil
}

// SubscriptionConfig returns a subscription that filters on the block's
// destination address, APPID and svID and validates datSet (when sent),
// confRev, smpRate (when sent) and the sample layout.
func (cb *ControlBlock) SubscriptionConfig() SubscriptionConfig {
	c := SubscriptionConfig{
		SvID: cb.SvID, DatSet: cb.DatSet, ConfRev: cb.ConfRev, SmpRate: cb.SmpRate, SmpMod: cb.SmpMod,
		Layout: append(Layout(nil), cb.Layout...),
	}
	if cb.HasAddress {
		c.Dst = cb.Dst
		c.AppIDs = []uint16{cb.AppID}
	}
	return c
}
