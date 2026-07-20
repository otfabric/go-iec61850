// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"

	"github.com/otfabric/go-mms"
)

// Read reads a single IEC 61850 data attribute and returns it as
// an IEC 61850 [Value].
//
// The ref must include LD, LN, and FC.
//
// LN-level reads are supported: when the ref has LD + LN + FC but no
// path, the server returns the full structured value for all data
// attributes under that LN and FC combination.
//
// For raw MMS values without the IEC 61850 wrapper, use [Client.ReadRaw].
func (c *Client) Read(ctx context.Context, ref Ref) (*Value, error) {
	raw, err := c.ReadRaw(ctx, ref)
	if err != nil {
		return nil, err
	}
	return NewValue(raw), nil
}

// ReadRaw reads a single IEC 61850 data attribute and returns the
// underlying [mms.Value] directly, without IEC 61850 wrapping.
//
// This is useful when callers need full control over MMS value
// interpretation or want to avoid the [Value] abstraction overhead.
//
// Both object-level (LD/LN.DO.DA[FC]) and LN-level (LD/LN[FC])
// reads are supported. LN-level reads return the full structured
// value for the LN + FC combination as a single MMS structure.
func (c *Client) ReadRaw(ctx context.Context, ref Ref) (*mms.Value, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if ref.LN == "" {
		return nil, &ReferenceError{Input: ref.String(), Reason: "logical node required for read"}
	}
	if ref.FC == "" {
		return nil, &ReferenceError{Input: ref.String(), Reason: "functional constraint required for read"}
	}

	domainID, itemID, err := c.refToMMS(ref)
	if err != nil {
		return nil, err
	}

	result, err := c.mmsClient.Read(ctx, mms.ReadRequest{
		DomainID: domainID,
		ItemID:   itemID,
	})
	if err != nil {
		return nil, fmt.Errorf("iec61850: read %s: %w", ref.String(), err)
	}

	if result == nil || result.Value == nil {
		return nil, fmt.Errorf("iec61850: read %s: missing value in read response", ref.String())
	}

	c.logger.Debug("iec61850: read", "ref", ref.String(), "type", result.Value.Type())
	return result.Value, nil
}

// ReadComponent reads a named component (sub-attribute) of a data
// object or data attribute.
//
// This is a convenience API equivalent to reading with a ref that has
// the component appended to the path. For example, reading component
// "stVal" from ref "LD/LN.Mod[ST]" is equivalent to reading
// "LD/LN.Mod.stVal[ST]".
//
// The ref must include LD, LN, FC, and at least one path component
// (the parent structure). The component name must be non-empty and
// must not contain separator characters.
func (c *Client) ReadComponent(ctx context.Context, ref Ref, component string) (*Value, error) {
	if component == "" {
		return nil, fmt.Errorf("iec61850: read component: empty component name")
	}
	if !ref.IsObject() {
		return nil, &ReferenceError{Input: ref.String(), Reason: "object reference required for read component"}
	}

	childRef, err := ref.Child(component)
	if err != nil {
		return nil, fmt.Errorf("iec61850: read component %q of %s: %w", component, ref.String(), err)
	}

	return c.Read(ctx, childRef)
}
