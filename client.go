// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// clientState tracks the connection lifecycle.
type clientState int

const (
	clientOpen    clientState = iota // normal operation
	clientClosing                    // shutdown in progress (cleanup running)
	clientClosed                     // fully shut down
)

// Client is an IEC 61850 MMS client that wraps [mms.Client] with
// IEC 61850 semantics.
//
// A Client is created via [Dial] (which establishes a new connection)
// or [NewClient] (which wraps an existing [mms.Client]).
//
// Close is idempotent: calling it multiple times is safe.
//
// When the association ends (the server concludes or aborts it, or the
// connection is lost), [Client.Done] is closed, [Client.Err] says why,
// and the report subscriptions are closed: their Reports channels are
// closed. Requests then fail; a new association needs a new Client.
//
// Concurrency: the Client is safe for concurrent use from multiple
// goroutines. It synchronizes its own state (cache, subscriptions,
// segmented report buffers, connection lifecycle) and delegates MMS
// wire-level concurrency to the underlying [mms.Client].
type Client struct {
	mu     sync.RWMutex
	state  clientState
	logger *slog.Logger
	opts   ClientOptions
	cache  *modelCache // nil when CacheNone

	mmsClient  *mms.Client
	ownsClient bool // true when created via Dial (we close the MMS client)

	// iedName is the IED name prefix used by the server to form MMS
	// domain names (from DialOptions.IEDName). When non-empty,
	// ldDomain prepends it and fetchLDs strips it.
	iedName string

	reportOnce sync.Once
	reportMu   sync.RWMutex
	reportSubs map[string][]*ReportSubscription
	// watchStop ends the goroutine that closes the subscriptions when the
	// association ends; watchDone is closed when it has returned. Both
	// are set with the first subscription (initReportDispatch).
	watchStop chan struct{}
	watchDone chan struct{}

	// Control model reports (control_termination.go).
	ctlMu      sync.Mutex
	ctlClosed  bool
	ctlWaiters map[string][]*terminationWaiter
	ctlWrites  map[*lastApplErrorWatch]struct{}
	ctlNums    map[string]uint8        // ctlNum of the last SelectWithValue per control
	ctlLayouts map[string]operTmLayout // operTm in the Oper/SBOw/Cancel type
	ctlChecks  map[string]bool         // a Check member in the Cancel type
	ctlLAE     map[string]laeEntry     // last LastApplError per addressed control
	ctlLAESeq  uint64
	// ctlAddressed are the controls (controlKey) this client sent a control
	// service to; only their LastApplError is kept in ctlLAE.
	ctlAddressed map[string]struct{}

	// Test hooks for intercepting fetch calls (unexported, test-only).
	fetchLDsFn   func(ctx context.Context) ([]LogicalDevice, error)
	fetchItemsFn func(ctx context.Context, ld string) ([]string, error)
}

// dialMMSOptions returns MMS dial options with Logger filled from the
// IEC logger when MMS.Logger is unset (mirrors NewServer behaviour). The
// Metrics run under a recover, as those of a server: a call that does not
// return normally is logged with its stack and loses that event.
func dialMMSOptions(opts DialOptions, logger *slog.Logger) mms.DialOptions {
	mmsOpts := opts.MMS
	if mmsOpts.Logger == nil {
		mmsOpts.Logger = logger
	}
	if mmsOpts.Metrics == nil {
		mmsOpts.Metrics = opts.Metrics
	}
	mmsOpts.Metrics = guardMetrics(mmsOpts.Metrics, &callbackGuard{logger: logger})
	return mmsOpts
}

// Dial establishes a new IEC 61850 MMS connection to the specified
// address (host:port).
//
// The context controls the connection timeout. The caller must call
// [Client.Close] when done.
func Dial(ctx context.Context, addr string, opts DialOptions) (*Client, error) {
	logger := opts.Logger
	if logger == nil {
		logger = discardLogger()
	}

	logger.Info("iec61850: dialing", "addr", addr)

	retry := opts.Retry
	userOnRetry := retry.OnRetry
	retry.OnRetry = func(attempt int, err error, d time.Duration) {
		logger.Warn("iec61850: dial failed, retrying", "addr", addr, "attempt", attempt, "backoff", d, "error", err)
		if userOnRetry != nil {
			userOnRetry(attempt, err, d)
		}
	}
	isoOpts := []iso.Option{
		iso.WithClientDialOptions(dialMMSOptions(opts, logger)),
		iso.WithRetry(retry),
	}
	if opts.MaxTPDULength != 0 {
		isoOpts = append(isoOpts, iso.WithMaxTPDULength(opts.MaxTPDULength))
	}
	if opts.Dialer != nil {
		isoOpts = append(isoOpts, iso.WithDialer(opts.Dialer))
	}
	mmsClient, err := iso.Dial(ctx, addr, isoOpts...)
	if err != nil {
		return nil, fmt.Errorf("iec61850: dial %s: %w", addr, err)
	}

	if mmsClient == nil {
		return nil, fmt.Errorf("iec61850: dial %s: transport returned nil client", addr)
	}

	copts := ClientOptions{
		Logger:        opts.Logger,
		Strictness:    opts.Strictness,
		Cache:         opts.Cache,
		IEDName:       opts.IEDName,
		Clock:         opts.Clock,
		FileLimits:    opts.FileLimits,
		JournalLimits: opts.JournalLimits,
	}
	var cache *modelCache
	if copts.Cache != CacheNone {
		cache = newModelCache(copts.Cache)
	}

	c := &Client{
		logger:     logger,
		mmsClient:  mmsClient,
		ownsClient: true,
		opts:       copts,
		cache:      cache,
		iedName:    opts.IEDName,
	}

	logger.Info("iec61850: connected", "addr", addr)
	return c, nil
}

// NewClient creates an IEC 61850 client wrapping an already-created
// [mms.Client]. The caller retains ownership of the MMS client and
// is responsible for closing it separately.
//
// Returns an error if mmsClient is nil.
//
// Use this when you need custom MMS connection setup or want to share
// a single MMS client across layers.
//
// The client installs its own InformationReport handler on the MMS
// client ([mms.Client.OnInformationReport]) when it first subscribes to
// reports or sends a control (to receive LastApplError and
// CommandTermination), replacing a handler set before.
func NewClient(mmsClient *mms.Client, opts ClientOptions) (*Client, error) {
	if mmsClient == nil {
		return nil, fmt.Errorf("iec61850: new client: nil mms client")
	}

	logger := opts.Logger
	if logger == nil {
		logger = discardLogger()
	}

	var cache *modelCache
	if opts.Cache != CacheNone {
		cache = newModelCache(opts.Cache)
	}

	return &Client{
		logger:     logger,
		mmsClient:  mmsClient,
		ownsClient: false,
		opts:       opts,
		cache:      cache,
		iedName:    opts.IEDName,
	}, nil
}

// Close performs a graceful shutdown of the IEC 61850 client.
//
// If the client was created via [Dial], Close also closes the
// underlying MMS connection. If created via [NewClient], the caller
// is responsible for closing the MMS client.
//
// The client transitions through open → closing → closed. During
// the closing phase new requests are refused with [ErrClosed] and
// subscription cleanup runs: every subscription is closed locally (its
// Reports channel is closed), then the RCBs the subscriptions enabled
// are disabled and the URCBs they reserved are released. The remote
// cleanup ends when ctx ends, and after 5 seconds at the latest. The
// Conclude of a client made by [Dial] ends the same way, after which the
// connection is closed, so Close returns also when the server does not
// answer.
// Remote cleanup errors during this phase are logged at Debug level
// because the connection may already be half-closed.
//
// Close is idempotent.
func (c *Client) Close(ctx context.Context) error {
	c.mu.Lock()
	if c.state != clientOpen {
		c.mu.Unlock()
		return nil
	}
	c.state = clientClosing
	c.mu.Unlock()

	c.logger.Info("iec61850: closing")

	c.stopAssociationWatch()
	c.closeAllSubscriptions(ctx, true)
	c.closeControlWaiters()

	c.mu.Lock()
	c.state = clientClosed
	c.mu.Unlock()

	if c.ownsClient && c.mmsClient != nil {
		cctx, cancel := context.WithTimeout(ctx, closeConcludeTimeout)
		defer cancel()
		return c.mmsClient.Close(cctx)
	}
	return nil
}

// closeConcludeTimeout bounds the Conclude that [Client.Close] sends for a
// client made by [Dial]: a server answers it once its outstanding
// requests are answered, and one that does not is left with the
// connection closed.
const closeConcludeTimeout = 5 * time.Second

// Abort performs a hard, immediate abort of the connection without
// graceful shutdown. Use this for emergency teardown or protocol
// desync recovery.
//
// If the client was created via [NewClient], Abort only marks the
// IEC 61850 layer as closed without aborting the MMS client.
//
// Abort closes the report subscriptions locally (their Reports channels
// are closed) and sends nothing: RCBs are not disabled and URCB
// reservations are not released. A server releases them when the
// association ends; with [NewClient], where the association outlives
// the client, use [Client.Close] or [ReportSubscription.Close] first.
//
// Abort is idempotent.
func (c *Client) Abort(ctx context.Context) error {
	c.mu.Lock()
	if c.state != clientOpen {
		c.mu.Unlock()
		return nil
	}
	c.state = clientClosing
	c.mu.Unlock()

	c.logger.Info("iec61850: aborting")

	c.stopAssociationWatch()
	c.closeAllSubscriptions(ctx, false)
	c.closeControlWaiters()

	c.mu.Lock()
	c.state = clientClosed
	c.mu.Unlock()

	if c.ownsClient && c.mmsClient != nil {
		return c.mmsClient.Abort(ctx)
	}
	return nil
}

// Done returns a channel that is closed when the association of the
// client has ended: after [Client.Close] or [Client.Abort] of a client
// made by [Dial], when the server concludes or aborts the association, or
// when the connection is lost. For a client made by [NewClient] it is the
// association of the [mms.Client] it wraps ([mms.Client.Done]), which
// Close and Abort of this Client do not end.
//
// When the association ends, the report subscriptions are closed (their
// Reports channels are closed) as by Abort: nothing is sent.
func (c *Client) Done() <-chan struct{} {
	return c.mmsClient.Done()
}

// Err returns nil while the association is up. After [Client.Done] is
// closed it returns why the association ended; the error matches
// [ErrClosed] and [mms.ErrClosed] ([errors.Is]) and wraps the cause go-mms
// reports ([mms.Client.Err]).
func (c *Client) Err() error {
	err := c.mmsClient.Err()
	if err == nil {
		return nil
	}
	return associationEndedError{err}
}

// associationEndedError is the error of [Client.Err]: it reads as the
// go-mms error and also matches [ErrClosed].
type associationEndedError struct{ err error }

func (e associationEndedError) Error() string   { return "iec61850: association ended: " + e.err.Error() }
func (e associationEndedError) Unwrap() []error { return []error{ErrClosed, e.err} }

// watchAssociation runs from the first subscription until Close or Abort:
// when the association ends, it closes the subscriptions locally (their
// Reports channels are closed). It sends nothing: the association is
// gone.
func (c *Client) watchAssociation(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	select {
	case <-c.mmsClient.Done():
		c.logger.Info("iec61850: association ended, closing report subscriptions", "error", c.mmsClient.Err())
		c.closeAllSubscriptions(context.Background(), false)
	case <-stop:
	}
}

// stopAssociationWatch ends the goroutine of watchAssociation, if it was
// started, and waits for it.
func (c *Client) stopAssociationWatch() {
	c.reportMu.Lock()
	stop, done := c.watchStop, c.watchDone
	c.watchStop = nil
	c.reportMu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

// checkOpen returns [ErrClosed] if the client is closing or closed.
func (c *Client) checkOpen() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state != clientOpen {
		return ErrClosed
	}
	return nil
}

// isClosing reports whether the client is in the closing state
// (cleanup running but not yet fully closed). This lets cleanup
// helpers distinguish intentional shutdown from normal operation.
func (c *Client) isClosing() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state == clientClosing
}

// closeAllSubscriptions closes all active report subscriptions: locally
// first (every Reports channel is closed before anything is sent), then,
// when remote is set, the remote cleanup (disable RCB, release URCB) of
// the subscriptions that used lifecycle options, bounded by ctx and by
// subscriptionCleanupTimeout for all of them together. The cleanup uses
// setRCB, which is not refused while the client is closing.
//
// The dispatch table is nilled out first so that unregisterSubscription
// inside closeLocal is a safe no-op.
func (c *Client) closeAllSubscriptions(ctx context.Context, remote bool) {
	c.reportMu.Lock()
	subs := c.reportSubs
	c.reportSubs = nil
	c.reportMu.Unlock()

	var closed []*ReportSubscription
	for _, group := range subs {
		for _, sub := range group {
			if sub.closeLocal() {
				closed = append(closed, sub)
			}
		}
	}
	if !remote || len(closed) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, subscriptionCleanupTimeout)
	defer cancel()
	for _, sub := range closed {
		sub.remoteCleanup(ctx)
	}
}

// MMS returns the underlying [mms.Client] for advanced operations
// that are not exposed by the IEC 61850 API.
//
// # Warning — unsafe escape hatch
//
// The returned pointer is shared with this Client. Operations
// performed on it bypass all IEC 61850 state management and can
// silently corrupt higher-level invariants including:
//
//   - Model cache freshness (raw MMS writes can change server state
//     without invalidating the cached browse results).
//   - Report subscription state (disabling or re-enabling an RCB
//     directly may desynchronize the subscription table).
//   - Connection lifecycle (calling Close or Abort on the returned
//     client while the IEC 61850 layer owns it leads to double-close
//     or use-after-close).
//
// Rules:
//
//   - Do NOT call Close or Abort on the returned client when the
//     [Client] was created via [Dial] (the IEC 61850 layer owns it).
//   - After [Client.Close] or [Client.Abort], the returned MMS client
//     follows the underlying go-mms closed semantics (all calls return
//     [mms.ErrClosed]).
//   - Treat this as a read-only escape hatch (e.g. Identify, Status,
//     GetNameList) unless you fully understand the interaction with
//     the IEC 61850 layer's state.
//
// Most users should not need this method.
func (c *Client) MMS() *mms.Client {
	return c.mmsClient
}

// ldDomain returns the MMS domain name for a logical device. When the
// client is configured with an IED name, it prepends it to ld; otherwise
// it returns ld unchanged.
func (c *Client) ldDomain(ld string) mms.DomainID {
	if c.iedName != "" {
		return mms.DomainID(c.iedName + ld)
	}
	return mms.DomainID(ld)
}

// refToMMS converts a Ref to an MMS domain and item ID, applying the
// IED name prefix to the domain when configured.
func (c *Client) refToMMS(ref Ref) (mms.DomainID, mms.ItemID, error) {
	domain, itemID, err := ref.ToMMS()
	if err != nil {
		return "", "", err
	}
	if c.iedName != "" {
		domain = mms.DomainID(c.iedName + string(domain))
	}
	return domain, itemID, nil
}

// stripIEDPrefix removes the IED name prefix from a MMS domain name,
// returning the bare LD instance name. If iedName is empty or the
// domain does not have that prefix, the domain is returned unchanged.
func (c *Client) stripIEDPrefix(domain string) string {
	if c.iedName != "" && strings.HasPrefix(domain, c.iedName) {
		return domain[len(c.iedName):]
	}
	return domain
}
