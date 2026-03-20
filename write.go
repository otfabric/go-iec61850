package iec61850

import (
	"context"
	"fmt"

	"github.com/otfabric/go-mms"
)

// Write writes a single IEC 61850 data attribute.
//
// The ref must include LD, LN, FC, and a path to the target
// data attribute. The value is written as-is to the MMS layer.
//
// Returns an error if the write is rejected by the server.
func (c *Client) Write(ctx context.Context, ref Ref, value *mms.Value) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	if !ref.IsObject() {
		return &ReferenceError{Input: ref.String(), Reason: "object reference required for write"}
	}
	if ref.FC == "" {
		return &ReferenceError{Input: ref.String(), Reason: "functional constraint required for write"}
	}
	if value == nil {
		return fmt.Errorf("iec61850: write %s: %w: nil value", ref.String(), ErrInvalidArgument)
	}

	domainID, itemID, err := ref.ToMMS()
	if err != nil {
		return err
	}

	_, err = c.mmsClient.Write(ctx, mms.WriteRequest{
		DomainID: domainID,
		ItemID:   itemID,
		Value:    value,
	})
	if err != nil {
		return fmt.Errorf("iec61850: write %s: %w", ref.String(), err)
	}

	c.logger.Debug("iec61850: write", "ref", ref.String())
	return nil
}
