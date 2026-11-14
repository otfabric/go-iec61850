// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"log/slog"
	"net"

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
	//
	// When [DialOptions.MMS].Logger is also nil, this logger is passed
	// through to the underlying [mms.Client]. ISO transport logging
	// ([iso.WithLogger]) is independent and is not set from here.
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

	// Retry controls how [Dial] retries a failed connection attempt; it
	// is passed to [iso.WithRetry], so one attempt covers the TCP connect,
	// the COTP handshake and the MMS association, and errors another
	// attempt cannot fix (see [mms.IsRetryable]) are returned at once.
	// Each retry is logged at Warn before RetryPolicy.OnRetry is called.
	// The zero value makes a single attempt; [mms.DefaultRetryPolicy] is
	// a sensible reconnect policy.
	Retry mms.RetryPolicy

	// Metrics, when non-nil, receives the MMS request and connection
	// events of the client (see [mms.Metrics]). It is a convenience field
	// equivalent to MMS.Metrics, which takes precedence when both are set.
	// Either is called under a recover: a call that does not return
	// normally is logged with its stack at error level and loses that
	// event.
	Metrics mms.Metrics

	// MaxTPDULength is the COTP TPDU size proposed in the connection
	// request, passed to [iso.WithMaxTPDULength]. Zero means the go-mms
	// default, 1024 octets; 65531 omits the parameter (the RFC 1006
	// default). Valid values are 128..65531.
	MaxTPDULength int

	// Dialer, when non-nil, opens the TCP connection in place of a zero
	// [net.Dialer]'s DialContext; it is passed to [iso.WithDialer]. Use it
	// for a destination allowlist checked against the resolved IP, a
	// proxy, custom socket options, or an in-memory transport in tests.
	// It is called once per attempt (see Retry) with network "tcp" and
	// the address given to [Dial]. The context carries the caller's
	// deadline and cancellation; a custom dialer must honour it. Like
	// [net.Dialer], it returns a non-nil connection exactly when the error
	// is nil: a connection returned with an error is closed, and a nil
	// connection with a nil error fails the dial. The returned connection
	// is owned and closed by the client. An error it returns is wrapped by
	// [Dial] and can be matched with [errors.Is]. Nil keeps the default
	// dialer.
	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)

	// Clock stamps the T member of control commands (see
	// [ClientOptions.Clock]). Nil is [SystemClock].
	Clock Clock

	// FileLimits bounds the file services of the client (see
	// [ClientOptions.FileLimits]). The zero value uses the defaults.
	FileLimits FileLimits
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

	// Clock stamps the T member (time and TimeQuality) of Operate,
	// SelectWithValue and Cancel (IEC 61850-7-2 20.5.2.5). Nil is
	// [SystemClock]: the host time, "clock not synchronised". A
	// per-call OperateParams.TQuality or CancelParams.TQuality
	// overrides the clock's quality.
	Clock Clock

	// FileLimits bounds what [Client.ReadFile] and [Client.DownloadFile]
	// accept from the server. The zero value uses the defaults.
	FileLimits FileLimits
}

// Defaults of [FileLimits].
const (
	// DefaultMaxFileSize is the size of the largest file [Client.ReadFile]
	// reads into memory: 256 MiB.
	DefaultMaxFileSize int64 = 256 << 20

	// DefaultFileSizeTolerance is how many bytes more than the size the
	// server announced when it opened the file a file read accepts:
	// 64 MiB.
	DefaultFileSizeTolerance int64 = 64 << 20

	// DefaultMaxEmptyFileChunks is the number of consecutive FileRead
	// answers without data that announce more data a file read accepts.
	DefaultMaxEmptyFileChunks = 64
)

// FileLimits bounds the file services of a [Client]. A field that is
// zero or less uses its default. A read that goes beyond a limit is
// given up with an error that wraps [ErrLimitExceeded], and the file is
// closed on the server (FileClose).
type FileLimits struct {
	// MaxSize is the size of the largest file [Client.ReadFile] reads
	// into memory; zero or less means [DefaultMaxFileSize]. A file whose
	// announced size is larger is refused before it is read. It does not
	// apply to [Client.DownloadFile], which streams.
	MaxSize int64

	// SizeTolerance is how many bytes more than the size announced when
	// the file was opened [Client.ReadFile] and [Client.DownloadFile]
	// accept; zero or less means [DefaultFileSizeTolerance].
	SizeTolerance int64

	// MaxEmptyChunks is the number of consecutive FileRead answers that
	// carry no data and announce more (MoreFollows) a read accepts; zero
	// or less means [DefaultMaxEmptyFileChunks].
	MaxEmptyChunks int
}

// resolved returns the limits with the defaults filled in.
func (l FileLimits) resolved() FileLimits {
	if l.MaxSize <= 0 {
		l.MaxSize = DefaultMaxFileSize
	}
	if l.SizeTolerance <= 0 {
		l.SizeTolerance = DefaultFileSizeTolerance
	}
	if l.MaxEmptyChunks <= 0 {
		l.MaxEmptyChunks = DefaultMaxEmptyFileChunks
	}
	return l
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
