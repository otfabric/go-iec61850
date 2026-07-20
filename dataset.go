// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/otfabric/go-mms"
)

// DataSet represents an IEC 61850 data set (MMS Named Variable List).
//
// A data set groups related data attributes for efficient bulk
// reading and for use as the trigger source of report control blocks.
type DataSet struct {
	// Reference is a display-friendly data set identifier in
	// IEC 61850 format (e.g., "LD/LLN0.dsName"). This is a
	// convenience string, not a [Ref] and not suitable for
	// programmatic use with [Client.ReadDataSet] or
	// [Client.DeleteDataSet] — those require the raw (ld, dsName)
	// MMS identifiers used when the DataSet was obtained.
	Reference string

	// Deletable indicates whether the data set can be deleted by the
	// client. Static (SCL-configured) data sets are not deletable;
	// dynamically created data sets are.
	Deletable bool

	// Members lists the data set members in definition order.
	Members []DataSetMember
}

// DataSetMember represents a single member (FCDA) of a data set.
type DataSetMember struct {
	// Ref is the IEC 61850 object reference for this member.
	// Includes LD, LN, path, and FC when available.
	Ref Ref

	// DomainID is the MMS domain name.
	DomainID string

	// ItemID is the MMS item ID (LN$FC$DO$DA path).
	ItemID string
}

// DataSetValue holds the result of reading a single data set member.
type DataSetValue struct {
	// Member is the data set member definition.
	Member DataSetMember

	// Value is the decoded value. Nil when Err is non-nil.
	Value *Value

	// Err is the per-member error, if any.
	Err error
}

// ListDataSets returns the names of all data sets (MMS Named Variable
// Lists) defined in the specified logical device (MMS domain).
//
// The returned names are MMS item IDs (e.g., "LLN0$dsName").
//
// Results are sorted alphabetically by name for deterministic output.
func (c *Client) ListDataSets(ctx context.Context, ld string) ([]string, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" {
		return nil, fmt.Errorf("iec61850: list data sets: %w: empty logical device name", ErrInvalidArgument)
	}

	names, err := c.mmsClient.GetNameListAll(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariableList,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    c.ldDomain(ld),
	})
	if err != nil {
		return nil, fmt.Errorf("iec61850: list data sets for %q: %w", ld, err)
	}

	sort.Strings(names)
	c.logger.Debug("iec61850: list data sets", "ld", ld, "count", len(names))
	return names, nil
}

// GetDataSet retrieves the definition (members) of a data set.
//
// The ld parameter is the logical device (MMS domain) and dsName is
// the data set name as an MMS NVL item ID (e.g. "LLN0$dsName").
// The returned [DataSet.Reference] uses IEC 61850 format
// (e.g. "LD/LLN0.dsName").
func (c *Client) GetDataSet(ctx context.Context, ld, dsName string) (*DataSet, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" {
		return nil, fmt.Errorf("iec61850: get data set: %w: empty logical device name", ErrInvalidArgument)
	}
	if dsName == "" {
		return nil, fmt.Errorf("iec61850: get data set: %w: empty data set name", ErrInvalidArgument)
	}

	if c.cache != nil {
		if ds, ok := c.cache.getDS(ld, dsName); ok {
			return ds, nil
		}
	}

	listName := mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: c.ldDomain(ld),
		ItemID: mms.ItemID(dsName),
	}

	attrs, err := c.mmsClient.GetNamedVariableListAttributes(ctx, listName)
	if err != nil {
		return nil, fmt.Errorf("iec61850: get data set %s/%s: %w", ld, dsName, err)
	}

	members := make([]DataSetMember, len(attrs.Variables))
	for i, v := range attrs.Variables {
		members[i] = variableSpecToMember(v)
	}

	dsRef := ld + "/" + mmsItemIDToIECDSName(dsName)

	ds := &DataSet{
		Reference: dsRef,
		Deletable: attrs.Deletable,
		Members:   members,
	}

	if c.cache != nil && c.cache.strategy == CacheLazy {
		c.cache.setDS(ld, dsName, ds)
	}

	c.logger.Debug("iec61850: get data set", "ref", dsRef, "members", len(members))
	return ds, nil
}

// ReadDataSet reads all member values of a data set in a single MMS
// request.
//
// The ld parameter is the logical device (MMS domain) and dsName is
// the data set name (MMS item ID, e.g. "LLN0$dsName").
//
// To understand the structure of the returned values, first call
// [Client.GetDataSet] to obtain the member definitions.
func (c *Client) ReadDataSet(ctx context.Context, ld, dsName string) ([]DataSetValue, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if ld == "" {
		return nil, fmt.Errorf("iec61850: read data set: %w: empty logical device name", ErrInvalidArgument)
	}
	if dsName == "" {
		return nil, fmt.Errorf("iec61850: read data set: %w: empty data set name", ErrInvalidArgument)
	}

	listName := mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: c.ldDomain(ld),
		ItemID: mms.ItemID(dsName),
	}

	// Try to reuse cached dataset definition to avoid an extra
	// GetNamedVariableListAttributes round-trip.
	var members []DataSetMember
	if c.cache != nil {
		if ds, ok := c.cache.getDS(ld, dsName); ok {
			members = ds.Members
		}
	}

	var memberVarNames []mms.ObjectName
	if members == nil {
		attrs, err := c.mmsClient.GetNamedVariableListAttributes(ctx, listName)
		if err != nil {
			return nil, fmt.Errorf("iec61850: read data set %s/%s: get attributes: %w", ld, dsName, err)
		}
		members = make([]DataSetMember, len(attrs.Variables))
		memberVarNames = make([]mms.ObjectName, len(attrs.Variables))
		for i, v := range attrs.Variables {
			members[i] = variableSpecToMember(v)
			memberVarNames[i] = v.Name
		}
		if c.cache != nil && c.cache.strategy == CacheLazy {
			dsRef := ld + "/" + mmsItemIDToIECDSName(dsName)
			c.cache.setDS(ld, dsName, &DataSet{
				Reference: dsRef,
				Deletable: attrs.Deletable,
				Members:   members,
			})
		}
	} else {
		memberVarNames = make([]mms.ObjectName, len(members))
		for i, m := range members {
			memberVarNames[i] = mms.ObjectName{
				Scope:  mms.ObjectScopeDomain,
				Domain: mms.DomainID(m.DomainID),
				ItemID: mms.ItemID(m.ItemID),
			}
		}
	}

	// Use ReadMultiple with expanded member variables rather than ReadNamedVariableList
	// (variableListName form) to ensure compatibility with servers that do not support
	// the variableListName CHOICE in the MMS Read PDU (e.g. iec61850bean).
	accessResults, err := c.mmsClient.ReadMultiple(ctx, memberVarNames)
	if err != nil {
		return nil, fmt.Errorf("iec61850: read data set %s/%s: %w", ld, dsName, err)
	}

	if len(accessResults) != len(members) {
		return nil, fmt.Errorf("iec61850: read data set %s/%s: value count %d != member count %d",
			ld, dsName, len(accessResults), len(members))
	}

	results := make([]DataSetValue, len(accessResults))
	for i, ar := range accessResults {
		results[i].Member = members[i]
		memberID := memberIdentity(results[i].Member, i)
		switch {
		case ar.ErrorCode != mms.DataAccessErrorNone:
			results[i].Err = &DataAccessError{Ref: memberID, ErrorCode: int(ar.ErrorCode), Operation: "read-dataset"}
		case ar.Value == nil:
			results[i].Err = fmt.Errorf("iec61850: read data set member %s: missing value", memberID)
		default:
			results[i].Value = NewValue(ar.Value)
		}
	}

	c.logger.Debug("iec61850: read data set", "ld", ld, "name", dsName, "values", len(results))
	return results, nil
}

// memberIdentity returns a human-readable identity for a data set
// member, preferring the IEC reference, falling back to domain/itemID,
// and using the index as last resort.
func memberIdentity(m DataSetMember, idx int) string {
	ref := m.Ref.String()
	if ref != "" {
		return ref
	}
	if m.DomainID != "" && m.ItemID != "" {
		return m.DomainID + "/" + m.ItemID
	}
	return fmt.Sprintf("[%d]", idx)
}

// variableSpecToMember converts an MMS VariableSpec to a DataSetMember,
// attempting to decode the IEC 61850 reference.
func variableSpecToMember(v mms.VariableSpec) DataSetMember {
	m := DataSetMember{
		DomainID: string(v.Name.Domain),
		ItemID:   string(v.Name.ItemID),
	}

	// Attempt to decode the full IEC 61850 reference. On failure, Ref
	// stays zero-value rather than creating a misleading partial Ref
	// with only LD set. Callers can still identify the member via the
	// raw DomainID/ItemID fields.
	ref, err := RefFromMMS(v.Name.Domain, v.Name.ItemID)
	if err == nil {
		m.Ref = ref
	}

	return m
}

// CreateDataSet creates a dynamic (deletable) data set on the server.
//
// The ld parameter is the logical device (MMS domain) that owns the
// data set, dsName is the data set name as an MMS item ID (e.g.
// "LLN0$dsNew"), and members lists the FCDAs.
//
// Each member must have a valid Ref with LN and FC, or explicit
// DomainID/ItemID. Members may reference data in different logical
// devices (cross-LD members); when a member's Ref.LD is empty, the
// owning ld is used as the default domain. The data set itself is
// always created under the specified ld.
//
// Member field precedence: if DomainID and ItemID are set explicitly,
// they are used as-is. Otherwise they are derived from the member's
// Ref via [Ref.ToMMS], defaulting the domain to ld when Ref.LD is
// empty.
//
// Dynamic data sets can be deleted with [Client.DeleteDataSet].
func (c *Client) CreateDataSet(ctx context.Context, ld, dsName string, members []DataSetMember) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if ld == "" {
		return fmt.Errorf("iec61850: create data set: %w: empty logical device name", ErrInvalidArgument)
	}
	if dsName == "" {
		return fmt.Errorf("iec61850: create data set: %w: empty data set name", ErrInvalidArgument)
	}
	if len(members) == 0 {
		return fmt.Errorf("iec61850: create data set: %w: no members", ErrInvalidArgument)
	}

	vars := make([]mms.VariableSpec, len(members))
	for i, m := range members {
		domainID := m.DomainID
		itemID := m.ItemID

		if domainID == "" || itemID == "" {
			if m.Ref.LN == "" {
				return fmt.Errorf("iec61850: create data set: member[%d]: LN is required", i)
			}
			if m.Ref.FC == "" {
				return fmt.Errorf("iec61850: create data set: member[%d]: FC is required", i)
			}
			if !m.Ref.HasPath() {
				return fmt.Errorf("iec61850: create data set: member[%d]: data path is required (LN-only refs not allowed)", i)
			}
			// Default empty LD to the dataset owner LD.
			ref := m.Ref
			if ref.LD == "" {
				ref.LD = ld
			}
			if err := ref.Validate(); err != nil {
				return fmt.Errorf("iec61850: create data set: member[%d]: %w", i, err)
			}
			d, id, err := c.refToMMS(ref)
			if err != nil {
				return fmt.Errorf("iec61850: create data set: member[%d]: %w", i, err)
			}
			domainID = string(d)
			itemID = string(id)
		}

		vars[i] = mms.VariableSpec{
			Name: mms.ObjectName{
				Scope:  mms.ObjectScopeDomain,
				Domain: mms.DomainID(domainID),
				ItemID: mms.ItemID(itemID),
			},
		}
	}

	err := c.mmsClient.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{
		ListName: mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: c.ldDomain(ld),
			ItemID: mms.ItemID(dsName),
		},
		Variables: vars,
	})
	if err != nil {
		return fmt.Errorf("iec61850: create data set %s/%s: %w", ld, dsName, err)
	}

	if c.cache != nil {
		c.cache.invalidateDS(ld)
	}

	c.logger.Debug("iec61850: created data set", "ld", ld, "name", dsName, "members", len(members))
	return nil
}

// DeleteDataSet deletes a dynamic data set from the server.
//
// Only dynamically created data sets (Deletable=true) can be deleted.
// Attempting to delete a static (SCL-configured) data set returns an
// error from the server.
func (c *Client) DeleteDataSet(ctx context.Context, ld, dsName string) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if ld == "" {
		return fmt.Errorf("iec61850: delete data set: %w: empty logical device name", ErrInvalidArgument)
	}
	if dsName == "" {
		return fmt.Errorf("iec61850: delete data set: %w: empty data set name", ErrInvalidArgument)
	}

	listNames := []mms.ObjectName{
		{
			Scope:  mms.ObjectScopeDomain,
			Domain: c.ldDomain(ld),
			ItemID: mms.ItemID(dsName),
		},
	}

	result, err := c.mmsClient.DeleteNamedVariableList(ctx, listNames)
	if err != nil {
		return fmt.Errorf("iec61850: delete data set %s/%s: %w", ld, dsName, err)
	}

	if result.NumberDeleted == 0 {
		return fmt.Errorf("iec61850: delete data set %s/%s: server did not delete (matched=%d, deleted=%d)",
			ld, dsName, result.NumberMatched, result.NumberDeleted)
	}

	if c.cache != nil {
		c.cache.invalidateDS(ld)
	}

	c.logger.Debug("iec61850: deleted data set", "ld", ld, "name", dsName)
	return nil
}

// mmsItemIDToIECDSName converts an MMS NVL item ID to an IEC 61850
// data set name by replacing the first $ with . (e.g., "LLN0$dsName"
// → "LLN0.dsName").
func mmsItemIDToIECDSName(itemID string) string {
	return strings.Replace(itemID, "$", ".", 1)
}
