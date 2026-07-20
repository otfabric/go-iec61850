package iec61850

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/otfabric/go-mms"
	"github.com/otfabric/go-mms/transport/iso"
)

// Client is an IEC 61850 MMS client that wraps [mms.Client] with
// IEC 61850 semantics.
//
// A Client is created via [Dial] (which establishes a new connection)
// or [NewClient] (which wraps an existing [mms.Client]).
//
// Close is idempotent: calling it multiple times is safe.
//
// Concurrency: the Client is safe for concurrent use from multiple
// goroutines. It synchronizes its own state (cache, subscriptions,
// segmented report buffers, connection lifecycle) and delegates MMS
// wire-level concurrency to the underlying [mms.Client].
// clientState tracks the connection lifecycle.
type clientState int

const (
	clientOpen    clientState = iota // normal operation
	clientClosing                    // shutdown in progress (cleanup running)
	clientClosed                     // fully shut down
)

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

	// Test hooks for intercepting fetch calls (unexported, test-only).
	fetchLDsFn   func(ctx context.Context) ([]LogicalDevice, error)
	fetchItemsFn func(ctx context.Context, ld string) ([]string, error)
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

	mmsClient, err := iso.Dial(ctx, addr,
		iso.WithClientDialOptions(opts.MMS),
	)
	if err != nil {
		return nil, fmt.Errorf("iec61850: dial %s: %w", addr, err)
	}

	if mmsClient == nil {
		return nil, fmt.Errorf("iec61850: dial %s: transport returned nil client", addr)
	}

	copts := ClientOptions{
		Logger:     opts.Logger,
		Strictness: opts.Strictness,
		Cache:      opts.Cache,
		IEDName:    opts.IEDName,
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
// the closing phase, subscription cleanup runs (disabling RCBs,
// releasing URCBs). Remote cleanup errors during this phase are
// logged at Debug level because the connection may already be
// half-closed.
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

	c.closeAllSubscriptions()

	c.mu.Lock()
	c.state = clientClosed
	c.mu.Unlock()

	if c.ownsClient && c.mmsClient != nil {
		return c.mmsClient.Close(ctx)
	}
	return nil
}

// Abort performs a hard, immediate abort of the connection without
// graceful shutdown. Use this for emergency teardown or protocol
// desync recovery.
//
// If the client was created via [NewClient], Abort only marks the
// IEC 61850 layer as closed without aborting the MMS client.
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

	c.closeAllSubscriptions()

	c.mu.Lock()
	c.state = clientClosed
	c.mu.Unlock()

	if c.ownsClient && c.mmsClient != nil {
		return c.mmsClient.Abort(ctx)
	}
	return nil
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

// closeAllSubscriptions closes all active report subscriptions via
// their Close method, ensuring remote cleanup (disable RCB, release
// URCB) runs for subscriptions that used lifecycle options.
//
// The dispatch table is nilled out first so that unregisterSubscription
// inside sub.Close() is a safe no-op.
func (c *Client) closeAllSubscriptions() {
	c.reportMu.Lock()
	subs := c.reportSubs
	c.reportSubs = nil
	c.reportMu.Unlock()

	for _, group := range subs {
		for _, sub := range group {
			_ = sub.Close()
		}
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
