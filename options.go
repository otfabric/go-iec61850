// SPDX-License-Identifier: MIT

package iec61850

import (
	"log/slog"

	"github.com/otfabric/go-mms"
)

// DialOptions configures an IEC 61850 client connection established
// via [Dial].
type DialOptions struct {
	// MMS holds the underlying MMS dial options. These are passed
	// through to [iso.Dial] via [iso.WithClientDialOptions].
	MMS mms.DialOptions

	// Logger, when non-nil, enables structured logging for IEC 61850
	// operations. When nil (the default), no logging is emitted.
	Logger *slog.Logger

	// Strictness controls validation behavior.
	Strictness StrictnessOptions

	// Cache controls the client-side caching strategy for model
	// discovery results. The default ([CacheNone]) disables caching.
	Cache CacheStrategy

	// IEDName, when non-empty, is the IED identifier prefix used by
	// the target server to form MMS domain names.
	//
	// Compliant IEC 61850-8-1 servers form MMS domain names by
	// concatenating the IED name and the LD instance name (e.g.
	// IEDName="InteropIED" + LDInst="InteropLD" →
	// domain="InteropIEDInteropLD"). When IEDName is set, the
	// client automatically strips the prefix when reporting logical
	// device names and prepends it when making MMS requests, so
	// callers can always use the bare LD instance name.
	IEDName string
}

// ClientOptions configures an IEC 61850 client created via [NewClient]
// from an existing [mms.Client].
type ClientOptions struct {
	// Logger, when non-nil, enables structured logging for IEC 61850
	// operations. When nil (the default), no logging is emitted.
	Logger *slog.Logger

	// Strictness controls validation behavior.
	Strictness StrictnessOptions

	// Cache controls the client-side caching strategy for model
	// discovery results (logical devices, logical nodes, data
	// objects, tree). The default ([CacheNone]) disables caching.
	Cache CacheStrategy

	// IEDName, when non-empty, is the IED identifier prefix used by
	// the target server to form MMS domain names. See [DialOptions.IEDName].
	IEDName string
}

// CacheStrategy controls client-side caching of IEC 61850 model
// discovery results to reduce server load during repeated browse
// operations.
type CacheStrategy int

const (
	// CacheNone disables caching. Every browse/discovery call hits
	// the server. This is the default.
	CacheNone CacheStrategy = iota

	// CacheExplicit enables a cache that is only populated via
	// explicit calls to [Client.RefreshCache] or
	// [Client.RefreshLDCache]. Browse methods consult the cache
	// if populated, otherwise fetch from the server without
	// storing the result. Use [Client.InvalidateCache] to clear.
	CacheExplicit

	// CacheLazy transparently caches results on first access.
	// Subsequent accesses reuse cached data. Explicit invalidation
	// and refresh are still available via [Client.InvalidateCache]
	// and [Client.RefreshCache].
	CacheLazy
)

// StrictnessOptions controls how strictly the client validates server
// responses and reference formats. The zero value uses pragmatic
// defaults suitable for interop with real-world IEC 61850 servers.
type StrictnessOptions struct {
	// RejectUnknownFC, when true, causes browse and tree operations
	// to return an error when an item ID contains a functional
	// constraint that is not in the IEC 61850 standard set. When
	// false (the default), unknown FCs are silently accepted and
	// stored as-is.
	RejectUnknownFC bool

	// VerifyReportCandidates, when true, causes [Client.ListReports]
	// to read and decode each heuristic RCB candidate, excluding
	// items that fail to decode as valid RCB structures. When false
	// (the default), discovery uses the fast naming-heuristic only.
	VerifyReportCandidates bool
}
