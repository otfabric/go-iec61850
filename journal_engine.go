// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/servermodel"
)

// JournalEngine is the server-side runtime journal engine. It
// generates journal (log) entries in response to value changes and
// application events, storing them in a [MemoryJournalProvider] that
// backs the MMS journal services.
//
// The engine is created by [Server.EnableJournals] and attached to
// the server's MMS layer as the [mms.JournalProvider].
type JournalEngine struct {
	logger   *slog.Logger
	provider *MemoryJournalProvider
	store    *servermodel.ValueStore
	model    *servermodel.Model

	mu       sync.RWMutex
	journals map[string]*journalDef // keyed by "ldName/journalName"
	lcbs     map[string]*lcbRuntime // keyed by "ldName/<LN>$LG$<name>"

	refMu     sync.Mutex
	byJournal map[journalKey][]*lcbRuntime // LCBs by LogRef

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

type journalDef struct {
	ldName      string
	journalName string
	logName     string
}

// JournalEngineOption configures the [JournalEngine].
type JournalEngineOption func(*JournalEngine)

// WithJournalMaxEntries sets the maximum entries per journal in the
// underlying [MemoryJournalProvider]. Default is 10000.
func WithJournalMaxEntries(n int) JournalEngineOption {
	return func(e *JournalEngine) {
		if n > 0 {
			e.provider = NewMemoryJournalProvider(WithMaxEntries(n))
		}
	}
}

// WithJournalProvider replaces the default [MemoryJournalProvider]
// with a pre-configured one. This is useful when the caller needs
// direct access to the provider for inspection or pre-population.
func WithJournalProvider(p *MemoryJournalProvider) JournalEngineOption {
	return func(e *JournalEngine) {
		if p != nil {
			e.provider = p
		}
	}
}

func newJournalEngine(logger *slog.Logger, store *servermodel.ValueStore, model *servermodel.Model, opts []JournalEngineOption) *JournalEngine {
	e := &JournalEngine{
		logger:    logger,
		provider:  NewMemoryJournalProvider(),
		store:     store,
		model:     model,
		journals:  make(map[string]*journalDef),
		lcbs:      make(map[string]*lcbRuntime),
		byJournal: make(map[journalKey][]*lcbRuntime),
		stopCh:    make(chan struct{}),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Provider returns the underlying [MemoryJournalProvider] used by
// this engine, allowing direct inspection of stored entries.
func (e *JournalEngine) Provider() *MemoryJournalProvider {
	return e.provider
}

// LogEvent records a custom application event in the specified
// journal. The domain is the logical device name and journal is the
// MMS journal name (typically "LLN0$logName"). Variables contain
// the data to record.
func (e *JournalEngine) LogEvent(domain, journal string, occTime time.Time, vars []mms.JournalVariable) []byte {
	return e.addEntry(journalKey{domain, journal}, occTime, vars)
}

// LogValueWrite evaluates the log control blocks for a write of the
// value at storeKey ("ldName/itemID"). Every enabled LCB (LogEna TRUE)
// whose data set contains the attribute and whose trigger options the
// write meets journals the member value followed by its reason code
// (IEC 61850-7-2 §17.3.4, IEC 61850-8-1 §17.3.3.4.4). Writes outside an
// LCB data set are not journaled.
func (e *JournalEngine) LogValueWrite(_ context.Context, storeKey string, occTime time.Time) {
	e.notifyLCBs(storeKey, occTime)
}

// registerJournalsFromModel registers the journals the model's LCBs
// write to: "<LN>$<logName>" in the LD of the log (IEC 61850-8-1
// §17.3.3.3.2), and creates the LCB runtimes.
func (e *JournalEngine) registerJournalsFromModel() int {
	count := 0
	for i := range e.model.LogicalDevices {
		ld := &e.model.LogicalDevices[i]
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			for k := range ln.Logs {
				logDef := &ln.Logs[k]
				domain, journalName := logDef.JournalName(ld.Name, ln.Name)
				key := domain + "/" + journalName
				e.mu.Lock()
				if _, dup := e.journals[key]; !dup {
					e.journals[key] = &journalDef{ldName: domain, journalName: journalName, logName: strings.TrimPrefix(journalName, lnPrefix(journalName))}
					count++
				}
				e.mu.Unlock()
				e.provider.RegisterJournal(domain, journalName)
			}
		}
	}
	e.registerLCBs()
	for k := range e.byJournal {
		e.refreshStatus(k)
	}
	return count
}

// lnPrefix returns "<LN>$" of a journal item "<LN>$<name>".
func lnPrefix(item string) string {
	if i := strings.IndexByte(item, '$'); i >= 0 {
		return item[:i+1]
	}
	return ""
}

// EnableJournals creates and starts the [JournalEngine] for this
// server. It scans the model for log definitions, registers journals,
// and makes the provider available as [mms.JournalProvider].
//
// If the MMS server was constructed with a [MemoryJournalProvider]
// in [ServerOptions.MMS.JournalProvider], EnableJournals
// automatically adopts it so that client MMS ReadJournal requests
// are served from the same data the engine writes to. If no
// provider was configured, the engine creates one internally. In
// that case, the engine records entries for programmatic access
// via [JournalEngine.Provider], but client MMS journal reads will
// not be served (they require the provider to be set at server
// construction time).
//
// To ensure full client↔server journal support:
//
//	jp := iec61850.NewMemoryJournalProvider()
//	srv, _ := iec61850.NewServer(model, iec61850.ServerOptions{
//	    MMS: mms.ServerOptions{JournalProvider: jp},
//	})
//	engine := srv.EnableJournals(iec61850.WithJournalProvider(jp))
func (s *Server) EnableJournals(opts ...JournalEngineOption) *JournalEngine {
	if s.mmsJournalProvider != nil {
		opts = append([]JournalEngineOption{WithJournalProvider(s.mmsJournalProvider)}, opts...)
	}

	engine := newJournalEngine(s.logger, s.store, s.model, opts)
	count := engine.registerJournalsFromModel()

	s.journalEngine = engine

	s.logger.Info("iec61850: journal engine enabled", "journals", count)
	return engine
}

// JournalEngine returns the journal engine, or nil if journals have
// not been enabled via [Server.EnableJournals].
func (s *Server) JournalEngine() *JournalEngine {
	return s.journalEngine
}

// modelHasLogs reports whether the model contains any log definitions.
func modelHasLogs(m *servermodel.Model) bool {
	for i := range m.LogicalDevices {
		for j := range m.LogicalDevices[i].LogicalNodes {
			if len(m.LogicalDevices[i].LogicalNodes[j].Logs) > 0 {
				return true
			}
		}
	}
	return false
}
