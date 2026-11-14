// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-mms"
)

// errCallbackRecovered is the error of a call of application code that
// did not return normally.
var errCallbackRecovered = errors.New("iec61850: callback did not return normally")

// callbackGuard records the calls of application code, made on the
// goroutines of this package and of go-mms, that did not return
// normally: each is logged with its stack at error level and counted
// ([ServerStats.CallbacksRecovered] for a server). The process goes on;
// the request concerned is answered with an error.
type callbackGuard struct {
	logger *slog.Logger
	n      atomic.Uint64
}

// recovered logs and counts a call of callback that ended with r and
// returns the error that answers the request. It is called by the
// deferred function that called recover, so the stack it logs is the one
// of the callback. A nil guard (a Server not made by NewServer) only
// returns the error.
func (g *callbackGuard) recovered(callback string, r any, attrs ...any) error {
	if g != nil {
		g.n.Add(1)
		g.logger.Error("iec61850: callback did not return normally",
			append([]any{"callback", callback, "value", r, "stack", string(debug.Stack())}, attrs...)...)
	}
	return fmt.Errorf("%w: %s: %v", errCallbackRecovered, callback, r)
}

// count returns the number of calls recovered; zero for a nil guard.
func (g *callbackGuard) count() uint64 {
	if g == nil {
		return 0
	}
	return g.n.Load()
}

// accessDenied is the answer to a variable read or write whose callback
// did not return normally: object-access-denied, as for a refused
// SettingGroupHandler callback.
func accessDenied(err error) error {
	return &writeError{err: err, dae: &mms.DataAccessError{Code: mms.DataAccessErrorObjectAccessDenied}}
}

// hook calls the connection hook fn (OnConnect, OnDisconnect) under the
// guard. A hook is a notification: the connection is served on.
func (g *callbackGuard) hook(callback string, fn func(ConnectionEvent), ev ConnectionEvent) {
	defer func() {
		if r := recover(); r != nil {
			_ = g.recovered(callback, r)
		}
	}()
	fn(ev)
}

// read wraps the Read of a variable.
func (g *callbackGuard) read(domain, item string, fn func(context.Context) (*mms.Value, error)) func(context.Context) (*mms.Value, error) {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context) (v *mms.Value, err error) {
		defer func() {
			if r := recover(); r != nil {
				v, err = nil, accessDenied(g.recovered("Read", r, "domain", domain, "item", item))
			}
		}()
		return fn(ctx)
	}
}

// write wraps the Write of a variable.
func (g *callbackGuard) write(domain, item string, fn func(context.Context, *mms.Value) error) func(context.Context, *mms.Value) error {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, v *mms.Value) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = accessDenied(g.recovered("Write", r, "domain", domain, "item", item))
			}
		}()
		return fn(ctx, v)
	}
}

// guardedRegistrar registers the variables of the server model with
// their Read and Write under the guard. The Read and Write of the model
// reach application code: the report buffers, the Clock and the engines'
// handlers.
type guardedRegistrar struct {
	srv *mms.Server
	g   *callbackGuard
}

func (r guardedRegistrar) RegisterDomain(name string) error { return r.srv.RegisterDomain(name) }

func (r guardedRegistrar) RegisterNamedVariableList(nvl mms.NamedVariableList) error {
	return r.srv.RegisterNamedVariableList(nvl)
}

func (r guardedRegistrar) RegisterVariable(v mms.Variable) error {
	domain, item := string(v.Name.Domain), string(v.Name.ItemID)
	v.Read = r.g.read(domain, item, v.Read)
	v.Write = r.g.write(domain, item, v.Write)
	return r.srv.RegisterVariable(v)
}

// guardedFileProvider calls an application [mms.FileProvider] under the
// guard: a call that does not return normally answers the file service
// with an error (MMS service error "other").
type guardedFileProvider struct {
	p mms.FileProvider
	g *callbackGuard
}

// done is deferred by every method; it recovers the call and sets *err.
func (f *guardedFileProvider) done(method string, err *error) {
	if r := recover(); r != nil {
		*err = f.g.recovered("FileProvider."+method, r)
	}
}

func (f *guardedFileProvider) List(ctx context.Context, req mms.FileListRequest) (res *mms.FileListResult, err error) {
	defer f.done("List", &err)
	return f.p.List(ctx, req)
}

func (f *guardedFileProvider) Open(ctx context.Context, path string) (h mms.FileHandle, attrs mms.FileAttributes, err error) {
	defer f.done("Open", &err)
	return f.p.Open(ctx, path)
}

func (f *guardedFileProvider) Read(ctx context.Context, h mms.FileHandle, maxBytes int) (data []byte, more bool, err error) {
	defer f.done("Read", &err)
	return f.p.Read(ctx, h, maxBytes)
}

func (f *guardedFileProvider) Close(ctx context.Context, h mms.FileHandle) (err error) {
	defer f.done("Close", &err)
	return f.p.Close(ctx, h)
}

func (f *guardedFileProvider) Delete(ctx context.Context, path string) (err error) {
	defer f.done("Delete", &err)
	return f.p.Delete(ctx, path)
}

func (f *guardedFileProvider) Rename(ctx context.Context, current, next string) (err error) {
	defer f.done("Rename", &err)
	return f.p.Rename(ctx, current, next)
}

func (f *guardedFileProvider) ObtainFile(ctx context.Context, source, destination string) (err error) {
	defer f.done("ObtainFile", &err)
	return f.p.ObtainFile(ctx, source, destination)
}

// authenticator wraps an application [mms.Authenticator]: a call that
// does not return normally rejects the association it was asked about.
func (g *callbackGuard) authenticator(fn mms.Authenticator) mms.Authenticator {
	return func(ctx context.Context, auth *mms.AuthContext) (res mms.AuthResult, err error) {
		defer func() {
			if r := recover(); r != nil {
				res, err = mms.AuthResult{}, g.recovered("Authenticate", r)
			}
		}()
		return fn(ctx, auth)
	}
}

// guardedMetrics calls an application [mms.Metrics] under the guard; a
// call that does not return normally loses that event.
type guardedMetrics struct {
	m mms.Metrics
	g *callbackGuard
}

func (m *guardedMetrics) done(hook string) {
	if r := recover(); r != nil {
		_ = m.g.recovered("Metrics."+hook, r)
	}
}

func (m *guardedMetrics) ObserveRequest(ev mms.RequestEvent) {
	defer m.done("ObserveRequest")
	m.m.ObserveRequest(ev)
}

func (m *guardedMetrics) ObserveConnection(ev mms.ConnectionEvent) {
	defer m.done("ObserveConnection")
	m.m.ObserveConnection(ev)
}

// guardMetrics returns m under g, or nil for a nil m.
func guardMetrics(m mms.Metrics, g *callbackGuard) mms.Metrics {
	if m == nil {
		return nil
	}
	if _, ok := m.(*guardedMetrics); ok {
		return m
	}
	return &guardedMetrics{m: m, g: g}
}

// guardedJournal calls an application [mms.JournalProvider] under the
// guard: a call that does not return normally answers the journal
// service with an error. guardJournal keeps the optional interfaces of
// the provider ([mms.JournalQueryProvider], [mms.JournalInitializer]).
type guardedJournal struct {
	p mms.JournalProvider
	g *callbackGuard
}

func (j *guardedJournal) done(method string, err *error) {
	if r := recover(); r != nil {
		*err = j.g.recovered("JournalProvider."+method, r)
	}
}

func (j *guardedJournal) ListJournals(ctx context.Context, domain string) (names []string, err error) {
	defer j.done("ListJournals", &err)
	return j.p.ListJournals(ctx, domain)
}

func (j *guardedJournal) ReadTimeRange(ctx context.Context, domain, journal string, start, stop time.Time, maxEntries int) (res *mms.JournalResult, err error) {
	defer j.done("ReadTimeRange", &err)
	return j.p.ReadTimeRange(ctx, domain, journal, start, stop, maxEntries)
}

func (j *guardedJournal) ReadStartAfter(ctx context.Context, domain, journal string, afterID []byte, afterTime time.Time, maxEntries int) (res *mms.JournalResult, err error) {
	defer j.done("ReadStartAfter", &err)
	return j.p.ReadStartAfter(ctx, domain, journal, afterID, afterTime, maxEntries)
}

func (j *guardedJournal) readJournal(ctx context.Context, q mms.JournalQuery) (res *mms.JournalResult, err error) {
	defer j.done("ReadJournal", &err)
	return j.p.(mms.JournalQueryProvider).ReadJournal(ctx, q)
}

func (j *guardedJournal) initializeJournal(ctx context.Context, domain, journal string, limit *mms.JournalLimit) (deleted uint32, err error) {
	defer j.done("InitializeJournal", &err)
	return j.p.(mms.JournalInitializer).InitializeJournal(ctx, domain, journal, limit)
}

type guardedJournalQuery struct{ *guardedJournal }

func (j guardedJournalQuery) ReadJournal(ctx context.Context, q mms.JournalQuery) (*mms.JournalResult, error) {
	return j.readJournal(ctx, q)
}

type guardedJournalInit struct{ *guardedJournal }

func (j guardedJournalInit) InitializeJournal(ctx context.Context, domain, journal string, limit *mms.JournalLimit) (uint32, error) {
	return j.initializeJournal(ctx, domain, journal, limit)
}

type guardedJournalQueryInit struct{ *guardedJournal }

func (j guardedJournalQueryInit) ReadJournal(ctx context.Context, q mms.JournalQuery) (*mms.JournalResult, error) {
	return j.readJournal(ctx, q)
}

func (j guardedJournalQueryInit) InitializeJournal(ctx context.Context, domain, journal string, limit *mms.JournalLimit) (uint32, error) {
	return j.initializeJournal(ctx, domain, journal, limit)
}

// guardJournal returns p under g with the optional interfaces p has.
func guardJournal(p mms.JournalProvider, g *callbackGuard) mms.JournalProvider {
	j := &guardedJournal{p: p, g: g}
	_, query := p.(mms.JournalQueryProvider)
	_, init := p.(mms.JournalInitializer)
	switch {
	case query && init:
		return guardedJournalQueryInit{j}
	case query:
		return guardedJournalQuery{j}
	case init:
		return guardedJournalInit{j}
	default:
		return j
	}
}
