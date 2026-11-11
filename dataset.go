// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
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
	listName, err := c.dataSetListName("get data set", ld, dsName)
	if err != nil {
		return nil, err
	}
	cacheable := c.cache != nil && !isAssociationDataSet(dsName)

	if cacheable {
		if ds, ok := c.cache.getDS(ld, dsName); ok {
			return ds, nil
		}
	}

	attrs, err := c.mmsClient.GetNamedVariableListAttributes(ctx, listName)
	if err != nil {
		if isObjectNotFound(err) {
			return nil, fmt.Errorf("iec61850: get data set %s/%s: %w: %w", ld, dsName, ErrNotFound, err)
		}
		return nil, fmt.Errorf("iec61850: get data set %s/%s: %w", ld, dsName, err)
	}

	members := make([]DataSetMember, len(attrs.Variables))
	for i, v := range attrs.Variables {
		members[i] = variableSpecToMember(v)
	}

	dsRef := ld + "/" + mmsItemIDToIECDSName(dsName)
	if isAssociationDataSet(dsName) {
		dsRef = dsName
	}

	ds := &DataSet{
		Reference: dsRef,
		Deletable: attrs.Deletable,
		Members:   members,
	}

	if cacheable && c.cache.strategy == CacheLazy {
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
	listName, err := c.dataSetListName("read data set", ld, dsName)
	if err != nil {
		return nil, err
	}
	cacheable := c.cache != nil && !isAssociationDataSet(dsName)

	// Try to reuse cached dataset definition to avoid an extra
	// GetNamedVariableListAttributes round-trip.
	var members []DataSetMember
	if cacheable {
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
		if cacheable && c.cache.strategy == CacheLazy {
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

// WriteDataSet is SetDataSetValues (IEC 61850-7-2 13.3.3): it writes
// values to the members of the data set dsName of ld in one MMS Write of
// the named variable list (IEC 61850-8-1 8.1.3.4.3.3), values[i] to
// member i. A dsName beginning with "@" addresses a non-persistent data
// set of the association (see [Client.CreateDataSet]).
//
// The returned slice has one entry per member: nil when the member was
// written, a [*DataAccessError] with the server's code otherwise. The
// error is non-nil when the request itself failed.
func (c *Client) WriteDataSet(ctx context.Context, ld, dsName string, values []*mms.Value) ([]error, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	listName, err := c.dataSetListName("write data set", ld, dsName)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("iec61850: write data set: %w: no values", ErrInvalidArgument)
	}
	results, err := c.mmsClient.WriteNamedVariableList(ctx, listName, values)
	if err != nil {
		return nil, fmt.Errorf("iec61850: write data set %s/%s: %w", ld, dsName, err)
	}
	if len(results) != len(values) {
		return nil, fmt.Errorf("iec61850: write data set %s/%s: %w: %d results for %d values", ld, dsName, ErrProtocol, len(results), len(values))
	}
	errs := make([]error, len(values))
	for i, r := range results {
		if !r.Success {
			errs[i] = &DataAccessError{Ref: fmt.Sprintf("%s/%s[%d]", ld, dsName, i), ErrorCode: int(r.ErrorCode), Operation: "write-dataset"}
		}
	}
	c.logger.Debug("iec61850: write data set", "ld", ld, "name", dsName, "values", len(values))
	return errs, nil
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
//
// A dsName beginning with "@" ("@dsNew") creates a non-persistent data
// set of this association (IEC 61850-7-2 13.2.2.2, 13.3.4): visible to
// this client only and deleted by the server when the association ends;
// ld then only defaults the members' LD. The same "@name" addresses it
// in [Client.GetDataSet], [Client.ReadDataSet], [Client.WriteDataSet]
// and [Client.DeleteDataSet].
//
// The name is checked before any request (IEC 61850-7-2 22.2): the part
// after "LN$" (or "@") has 1 to 32 characters, begins with a letter and
// uses A-Z, a-z, 0-9 and _, the MMS item has at most 64 characters (IEC
// 61850-8-1 8.1.3.2.2); a member's FC must be a valid functional
// constraint other than XX (IEC 61850-8-1 7.3.1 NOTE 2). A violation
// returns [ErrInvalidArgument].
func (c *Client) CreateDataSet(ctx context.Context, ld, dsName string, members []DataSetMember) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if ld == "" && !isAssociationDataSet(dsName) {
		return fmt.Errorf("iec61850: create data set: %w: empty logical device name", ErrInvalidArgument)
	}
	listName, err := c.dataSetListName("create data set", ld, dsName)
	if err != nil {
		return err
	}
	if err := validateDataSetName(string(listName.ItemID)); err != nil {
		return fmt.Errorf("iec61850: create data set: %w: %w", ErrInvalidArgument, err)
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
			// XX (any FC) is not a FunctionalConstraint, IEC 61850-8-1 7.3.1
			// NOTE 2.
			if !m.Ref.FC.IsValid() {
				return fmt.Errorf("iec61850: create data set: member[%d]: %w: FC %q is not a data set member FC", i, ErrInvalidArgument, m.Ref.FC)
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
		if len(itemID) > maxMMSIdentifier {
			return fmt.Errorf("iec61850: create data set: member[%d]: %w: item %q longer than %d characters", i, ErrInvalidArgument, itemID, maxMMSIdentifier)
		}

		vars[i] = mms.VariableSpec{
			Name: mms.ObjectName{
				Scope:  mms.ObjectScopeDomain,
				Domain: mms.DomainID(domainID),
				ItemID: mms.ItemID(itemID),
			},
		}
	}

	err = c.mmsClient.DefineNamedVariableList(ctx, mms.DefineNamedVariableListRequest{
		ListName:  listName,
		Variables: vars,
	})
	if err != nil {
		return fmt.Errorf("iec61850: create data set %s/%s: %w", ld, dsName, err)
	}

	if c.cache != nil && !isAssociationDataSet(dsName) {
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
	listName, err := c.dataSetListName("delete data set", ld, dsName)
	if err != nil {
		return err
	}

	result, err := c.mmsClient.DeleteNamedVariableList(ctx, []mms.ObjectName{listName})
	if err != nil {
		return fmt.Errorf("iec61850: delete data set %s/%s: %w", ld, dsName, err)
	}

	if result.NumberDeleted == 0 {
		return fmt.Errorf("iec61850: delete data set %s/%s: server did not delete (matched=%d, deleted=%d)",
			ld, dsName, result.NumberMatched, result.NumberDeleted)
	}

	if c.cache != nil && !isAssociationDataSet(dsName) {
		c.cache.invalidateDS(ld)
	}

	c.logger.Debug("iec61850: deleted data set", "ld", ld, "name", dsName)
	return nil
}

// dataSetListName returns the MMS name of the data set dsName of ld. A
// dsName starting with "@" is a non-persistent data set of the
// association (IEC 61850-7-2 13.2.2.2: "@DataSetName"; IEC 61850-8-1
// 8.1.3.2.2: an aa-specific named variable list, deleted when the
// association ends); ld is then not used. Otherwise ld is required and
// the name is domain-specific.
func (c *Client) dataSetListName(op, ld, dsName string) (mms.ObjectName, error) {
	if dsName == "" {
		return mms.ObjectName{}, fmt.Errorf("iec61850: %s: %w: empty data set name", op, ErrInvalidArgument)
	}
	if name, ok := strings.CutPrefix(dsName, "@"); ok {
		if name == "" {
			return mms.ObjectName{}, fmt.Errorf("iec61850: %s: %w: empty data set name after @", op, ErrInvalidArgument)
		}
		return mms.ObjectName{Scope: mms.ObjectScopeAssociation, ItemID: mms.ItemID(name)}, nil
	}
	if ld == "" {
		return mms.ObjectName{}, fmt.Errorf("iec61850: %s: %w: empty logical device name", op, ErrInvalidArgument)
	}
	return mms.ObjectName{Scope: mms.ObjectScopeDomain, Domain: c.ldDomain(ld), ItemID: mms.ItemID(dsName)}, nil
}

// isAssociationDataSet reports whether dsName names a non-persistent
// data set of the association ("@name"); those are not cached.
func isAssociationDataSet(dsName string) bool { return strings.HasPrefix(dsName, "@") }

// maxMMSIdentifier is the length limit of an MMS identifier (IEC
// 61850-8-1 8.1.3.2.2: 64 characters).
const maxMMSIdentifier = 64

// validateDataSetName checks the name part of a data set to create
// (after "LN$" or "@"): IEC 61850-7-2 22.2, at most 32 characters,
// beginning with a letter, of A-Z, a-z, 0-9 and _; and the MMS item of
// at most 64 characters (IEC 61850-8-1 8.1.3.2.2).
func validateDataSetName(itemID string) error {
	if len(itemID) > maxMMSIdentifier {
		return fmt.Errorf("data set item %q longer than %d characters", itemID, maxMMSIdentifier)
	}
	name := itemID
	if i := strings.IndexByte(itemID, '$'); i >= 0 {
		name = itemID[i+1:]
	}
	if name == "" || len(name) > 32 {
		return fmt.Errorf("data set name %q: 1 to 32 characters", name)
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		alpha := ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
		if i == 0 && !alpha {
			return fmt.Errorf("data set name %q: must begin with a letter", name)
		}
		if !alpha && !(ch >= '0' && ch <= '9') && ch != '_' {
			return fmt.Errorf("data set name %q: character %q not in A-Za-z0-9_", name, ch)
		}
	}
	return nil
}

// mmsItemIDToIECDSName converts an MMS NVL item ID to an IEC 61850
// data set name by replacing the first $ with . (e.g., "LLN0$dsName"
// → "LLN0.dsName").
func mmsItemIDToIECDSName(itemID string) string {
	return strings.Replace(itemID, "$", ".", 1)
}

// isObjectNotFound reports whether err is an MMS ServiceError saying the
// object does not exist: access/object-non-existent or
// definition/object-undefined (servers answer an unknown named variable
// list with either).
func isObjectNotFound(err error) bool {
	var se *mms.ServiceError
	if !errors.As(err, &se) {
		return false
	}
	return se.Class == mms.ErrorClassAccess && se.CodeName() == "object-non-existent" ||
		se.Class == mms.ErrorClassDefinition && se.CodeName() == "object-undefined"
}
