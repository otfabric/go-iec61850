// SPDX-License-Identifier: MIT

package iec61850

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	sentinels := []error{
		ErrInvalidReference,
		ErrInvalidFunctionalConstraint,
		ErrNotFound,
		ErrTypeMismatch,
		ErrUnsupportedService,
		ErrSubscriptionClosed,
		ErrSCLParse,
		ErrModelMismatch,
		ErrUnsupportedCDC,
		ErrReportDecode,
		ErrDatasetDecode,
		ErrClosed,
		ErrInvalidArgument,
		ErrProtocol,
		ErrControlFailed,
		ErrSelectFailed,
		ErrOperateFailed,
		ErrCancelFailed,
		ErrNotControllable,
	}

	for _, sentinel := range sentinels {
		t.Run(sentinel.Error(), func(t *testing.T) {
			wrapped := fmt.Errorf("outer: %w", sentinel)
			if !errors.Is(wrapped, sentinel) {
				t.Errorf("errors.Is failed for wrapped %v", sentinel)
			}
		})
	}
}

func TestReferenceError_Unwrap(t *testing.T) {
	err := &ReferenceError{Input: "bad/ref", Reason: "test reason"}
	if !errors.Is(err, ErrInvalidReference) {
		t.Error("ReferenceError should wrap ErrInvalidReference")
	}

	var refErr *ReferenceError
	if !errors.As(err, &refErr) {
		t.Error("errors.As should extract *ReferenceError")
	}
	if refErr.Input != "bad/ref" {
		t.Errorf("Input = %q, want %q", refErr.Input, "bad/ref")
	}
}

func TestReferenceError_WithWrapped(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := &ReferenceError{Input: "ref", Reason: "reason", Wrapped: inner}
	if !errors.Is(err, inner) {
		t.Error("should unwrap to inner error")
	}
	s := err.Error()
	if s == "" {
		t.Error("empty error string")
	}
}

func TestDecodeError_Unwrap(t *testing.T) {
	err := &DecodeError{Ref: "LD/LN.DO", Type: "Quality", Message: "bad bits"}
	if !errors.Is(err, ErrTypeMismatch) {
		t.Error("DecodeError should wrap ErrTypeMismatch")
	}

	var decErr *DecodeError
	if !errors.As(err, &decErr) {
		t.Error("errors.As should extract *DecodeError")
	}
}

func TestModelError_Unwrap(t *testing.T) {
	err := &ModelError{Ref: "LD/LN", Message: "missing child"}
	if !errors.Is(err, ErrModelMismatch) {
		t.Error("ModelError should wrap ErrModelMismatch")
	}
}

func TestReportError_Unwrap(t *testing.T) {
	err := &ReportError{RCBRef: "LD/LN.RP.rcb01", Message: "decode failed"}
	if !errors.Is(err, ErrReportDecode) {
		t.Error("ReportError should wrap ErrReportDecode")
	}
}

func TestSCLParseError_Unwrap(t *testing.T) {
	err := &SCLParseError{File: "test.icd", Line: 42, Message: "bad XML"}
	if !errors.Is(err, ErrSCLParse) {
		t.Error("SCLParseError should wrap ErrSCLParse")
	}
	s := err.Error()
	if s == "" {
		t.Error("empty error string")
	}
}

func TestSCLParseError_NoFile(t *testing.T) {
	err := &SCLParseError{Message: "bad XML"}
	s := err.Error()
	if s == "" {
		t.Error("empty error string")
	}
}

func TestSCLParseError_FileNoLine(t *testing.T) {
	err := &SCLParseError{File: "test.icd", Message: "bad XML"}
	s := err.Error()
	if s == "" {
		t.Error("empty error string")
	}
}

func TestSCLParseError_WithWrapped(t *testing.T) {
	inner := fmt.Errorf("xml syntax error")
	err := &SCLParseError{File: "test.icd", Line: 10, Message: "bad element", Wrapped: inner}
	s := err.Error()
	if !errors.Is(err, inner) {
		t.Error("should unwrap to inner error")
	}
	if !containsSubstring(s, "xml syntax error") {
		t.Errorf("Error() = %q, should contain wrapped error text", s)
	}
}

func TestDecodeError_NoRef(t *testing.T) {
	err := &DecodeError{Type: "Timestamp", Message: "bad format"}
	s := err.Error()
	if s == "" {
		t.Error("empty error string")
	}
}

func TestDecodeError_WithWrapped(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := &DecodeError{Ref: "LD/LN.DO", Type: "Quality", Message: "bad bits", Wrapped: inner}
	s := err.Error()
	if !errors.Is(err, inner) {
		t.Error("should unwrap to inner error")
	}
	if s == "" {
		t.Error("empty error string")
	}
	if !containsSubstring(s, "inner error") {
		t.Errorf("Error() = %q, should contain wrapped error text", s)
	}
}

func TestModelError_NoRef(t *testing.T) {
	err := &ModelError{Message: "general error"}
	s := err.Error()
	if s == "" {
		t.Error("empty error string")
	}
}

func TestModelError_WithWrapped(t *testing.T) {
	inner := fmt.Errorf("inner model issue")
	err := &ModelError{Ref: "LD/LN", Message: "mismatch", Wrapped: inner}
	s := err.Error()
	if !errors.Is(err, inner) {
		t.Error("should unwrap to inner error")
	}
	if !containsSubstring(s, "inner model issue") {
		t.Errorf("Error() = %q, should contain wrapped error text", s)
	}
}

func TestReportError_NoRCB(t *testing.T) {
	err := &ReportError{Message: "general report error"}
	s := err.Error()
	if s == "" {
		t.Error("empty error string")
	}
}

func TestReportError_WithWrapped(t *testing.T) {
	inner := fmt.Errorf("decode failed")
	err := &ReportError{RCBRef: "LD/LN.RP.rcb01", Message: "corrupt", Wrapped: inner}
	s := err.Error()
	if !errors.Is(err, inner) {
		t.Error("should unwrap to inner error")
	}
	if !containsSubstring(s, "decode failed") {
		t.Errorf("Error() = %q, should contain wrapped error text", s)
	}
}

func TestControlError_UnwrapByOperation(t *testing.T) {
	tests := []struct {
		op       string
		sentinel error
	}{
		{"select", ErrSelectFailed},
		{"operate", ErrOperateFailed},
		{"cancel", ErrCancelFailed},
		{"unknown", ErrControlFailed},
	}
	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			err := &ControlError{Ref: "LD/LN.DO", Operation: tt.op}
			if !errors.Is(err, tt.sentinel) {
				t.Errorf("ControlError{Operation: %q} should unwrap to %v", tt.op, tt.sentinel)
			}
		})
	}
}

func TestControlError_WithAddCause(t *testing.T) {
	err := &ControlError{
		Ref:       "LD/LN.DO",
		Operation: "operate",
		AddCause:  AddCauseBlockedByInterlocking,
	}
	s := err.Error()
	if !containsSubstring(s, "blocked-by-interlocking") {
		t.Errorf("Error() = %q, should contain addCause text", s)
	}
}

func TestControlError_WithWrapped(t *testing.T) {
	inner := fmt.Errorf("write rejected")
	err := &ControlError{Ref: "LD/LN.DO", Operation: "operate", Wrapped: inner}
	if !errors.Is(err, inner) {
		t.Error("should unwrap to inner error")
	}
	if !errors.Is(err, ErrOperateFailed) {
		t.Error("should also match ErrOperateFailed even when wrapped")
	}
}

func containsSubstring(s, sub string) bool {
	return strings.Contains(s, sub)
}
