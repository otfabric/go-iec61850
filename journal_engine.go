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
		logger:   logger,
		provider: NewMemoryJournalProvider(),
		store:    store,
		model:    model,
		journals: make(map[string]*journalDef),
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
	return e.provider.AddEntry(domain, journal, occTime, vars)
}

// LogValueWrite records a value-write entry in all journals defined
// for the logical device that contains the given store key. Every
// write is logged regardless of whether the value actually changed;
// this provides an audit trail rather than semantic change detection.
// The store key format is "ldName/itemID".
func (e *JournalEngine) LogValueWrite(ctx context.Context, storeKey string, occTime time.Time) {
	parts := strings.SplitN(storeKey, "/", 2)
	if len(parts) != 2 {
		return
	}
	ldName := parts[0]
	itemID := parts[1]

	val := e.store.Get(storeKey)
	if val == nil {
		return
	}

	vars := []mms.JournalVariable{{
		Tag:   itemID,
		Value: val,
	}}

	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, jd := range e.journals {
		if jd.ldName == ldName {
			e.provider.AddEntry(ldName, jd.journalName, occTime, vars)
			e.logger.Debug("iec61850: journal entry logged",
				"ld", ldName, "journal", jd.journalName,
				"key", storeKey)
		}
	}

	_ = ctx
}

// registerJournalsFromModel scans the model for log definitions and
// registers corresponding journals in the provider. The MMS journal
// name follows the convention "LNName$logName" (e.g., "LLN0$log1").
func (e *JournalEngine) registerJournalsFromModel() int {
	count := 0
	for i := range e.model.LogicalDevices {
		ld := &e.model.LogicalDevices[i]
		for j := range ld.LogicalNodes {
			ln := &ld.LogicalNodes[j]
			for _, logDef := range ln.Logs {
				journalName := ln.Name + "$" + logDef.Name
				key := ld.Name + "/" + journalName

				jd := &journalDef{
					ldName:      ld.Name,
					journalName: journalName,
					logName:     logDef.Name,
				}

				e.mu.Lock()
				e.journals[key] = jd
				e.mu.Unlock()

				e.provider.RegisterJournal(ld.Name, journalName)
				count++
			}
		}
	}
	return count
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
