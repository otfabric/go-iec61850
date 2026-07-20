package iec61850

import (
	"context"
	"fmt"

	"github.com/otfabric/go-mms"
)

// ReadResult holds the outcome of reading a single variable as part
// of a bulk read operation.
type ReadResult struct {
	// Ref is the reference that was read.
	Ref Ref

	// Value is the decoded value. Nil when Err is non-nil.
	Value *Value

	// Err is the per-item error, if any. When the MMS server
	// returns a per-variable DataAccessError, Err is set and
	// Value is nil.
	Err error
}

// WriteRequest specifies a single variable to write as part of a
// bulk write operation.
type WriteRequest struct {
	// Ref is the target reference. Must include LD, LN, FC,
	// and at least one path component.
	Ref Ref

	// Value is the MMS value to write.
	Value *mms.Value
}

// WriteResult holds the outcome of writing a single variable as part
// of a bulk write operation.
type WriteResult struct {
	// Ref is the reference that was written.
	Ref Ref

	// Success is true if the write was accepted by the server.
	Success bool

	// Err is the per-item error, if any.
	Err error
}

// HasDuplicateRefs reports whether the given ref slice contains
// duplicate entries (same LD/LN/Path/FC). Use this before
// [Client.ReadMultiple] or [Client.WriteMultiple] to detect
// accidental duplicates that the bulk APIs preserve by design.
func HasDuplicateRefs(refs []Ref) bool {
	seen := make(map[string]struct{}, len(refs))
	for _, r := range refs {
		key := r.String()
		if _, dup := seen[key]; dup {
			return true
		}
		seen[key] = struct{}{}
	}
	return false
}

// ReadMultiple reads multiple IEC 61850 data objects or data
// attributes in a single MMS request.
//
// Each ref must be an object reference (LD, LN, FC, and at least one
// path component). LN-level bulk reads are not supported; use
// [Client.Read] for those. Results are returned in the same order as
// the input refs.
//
// Duplicate refs are preserved: if the same ref appears multiple
// times, each occurrence produces a separate result at the
// corresponding index. The server may return identical or different
// values depending on timing.
//
// Per-item errors (e.g., object not found) are reported in
// [ReadResult.Err] as a [*DataAccessError] rather than failing the
// entire operation.
//
// Mixed-domain and mixed-FC reads are supported: refs may span
// different logical devices and functional constraints within a
// single call.
//
// If the MMS request itself fails (e.g., connection error), a
// non-nil error is returned and the result slice is nil.
func (c *Client) ReadMultiple(ctx context.Context, refs []Ref) ([]ReadResult, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}

	names := make([]mms.ObjectName, len(refs))
	for i, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, fmt.Errorf("iec61850: read multiple: ref[%d] %s: %w", i, ref.String(), err)
		}
		if !ref.IsObject() {
			return nil, fmt.Errorf("iec61850: read multiple: ref[%d]: %w: object reference required (LD/LN/FC with data path)", i, ErrInvalidArgument)
		}
		if ref.FC == "" {
			return nil, fmt.Errorf("iec61850: read multiple: ref[%d]: %w: functional constraint required", i, ErrInvalidArgument)
		}

		domainID, itemID, err := c.refToMMS(ref)
		if err != nil {
			return nil, fmt.Errorf("iec61850: read multiple: ref[%d] %s: %w", i, ref.String(), err)
		}
		names[i] = mms.ObjectName{
			Scope:  mms.ObjectScopeDomain,
			Domain: domainID,
			ItemID: itemID,
		}
	}

	accessResults, err := c.mmsClient.ReadMultiple(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("iec61850: read multiple: %w", err)
	}

	if len(accessResults) != len(refs) {
		return nil, fmt.Errorf("iec61850: read multiple: %w: response count mismatch: sent %d, got %d", ErrProtocol,
			len(refs), len(accessResults))
	}

	results := make([]ReadResult, len(refs))
	for i, ref := range refs {
		results[i].Ref = ref
		ar := accessResults[i]
		if ar.ErrorCode != mms.DataAccessErrorNone {
			results[i].Err = &DataAccessError{Ref: ref.String(), ErrorCode: int(ar.ErrorCode), Operation: "read"}
		} else if ar.Value == nil {
			results[i].Err = fmt.Errorf("iec61850: read %s: missing value in successful access result", ref.String())
		} else {
			results[i].Value = NewValue(ar.Value)
		}
	}

	c.logger.Debug("iec61850: read multiple", "count", len(refs))
	return results, nil
}

// WriteMultiple writes multiple IEC 61850 data attributes in a
// single MMS request.
//
// Each request must include a valid ref (LD, LN, FC, path) and a
// non-nil value. Results are returned in the same order as the
// input requests. Per-item errors are reported in [WriteResult.Err]
// as a [*DataAccessError].
//
// Duplicate refs are preserved: if the same ref appears multiple
// times, each write is sent in order. The final server-side value
// is the last successful write.
//
// Mixed-domain and mixed-FC writes are supported: requests may span
// different logical devices and functional constraints within a
// single call. Write order is preserved.
//
// If the MMS request itself fails, a non-nil error is returned and
// the result slice is nil.
func (c *Client) WriteMultiple(ctx context.Context, requests []WriteRequest) ([]WriteResult, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, nil
	}

	specs := make([]mms.VariableSpec, len(requests))
	values := make([]*mms.Value, len(requests))

	for i, req := range requests {
		if err := req.Ref.Validate(); err != nil {
			return nil, fmt.Errorf("iec61850: write multiple: request[%d] %s: %w", i, req.Ref.String(), err)
		}
		if !req.Ref.IsObject() {
			return nil, fmt.Errorf("iec61850: write multiple: request[%d]: %w: object reference required", i, ErrInvalidArgument)
		}
		if req.Ref.FC == "" {
			return nil, fmt.Errorf("iec61850: write multiple: request[%d]: %w: functional constraint required", i, ErrInvalidArgument)
		}
		if req.Value == nil {
			return nil, fmt.Errorf("iec61850: write multiple: request[%d] %s: %w: nil value", i, req.Ref.String(), ErrInvalidArgument)
		}

		domainID, itemID, err := c.refToMMS(req.Ref)
		if err != nil {
			return nil, fmt.Errorf("iec61850: write multiple: request[%d] %s: %w", i, req.Ref.String(), err)
		}
		specs[i] = mms.VariableSpec{
			Name: mms.ObjectName{
				Scope:  mms.ObjectScopeDomain,
				Domain: domainID,
				ItemID: itemID,
			},
		}
		values[i] = req.Value
	}

	writeResults, err := c.mmsClient.WriteVariables(ctx, specs, values)
	if err != nil {
		return nil, fmt.Errorf("iec61850: write multiple: %w", err)
	}

	// go-mms WriteVariables validates response count and returns
	// exactly one result per input with sequential indices.
	if len(writeResults) != len(requests) {
		return nil, fmt.Errorf("iec61850: write multiple: %w: response count mismatch: sent %d, got %d", ErrProtocol,
			len(requests), len(writeResults))
	}

	results := make([]WriteResult, len(requests))
	for i, req := range requests {
		results[i].Ref = req.Ref
		results[i].Success = writeResults[i].Success
		if !writeResults[i].Success {
			results[i].Err = &DataAccessError{Ref: req.Ref.String(), ErrorCode: int(writeResults[i].ErrorCode), Operation: "write"}
		}
	}

	c.logger.Debug("iec61850: write multiple", "count", len(requests))
	return results, nil
}
