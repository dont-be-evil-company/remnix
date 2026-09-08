package daemon

import (
	"context"
	"log/slog"
	"net"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/protocol"
	"github.com/mistweaverco/syncsh/internal/search"
	"github.com/mistweaverco/syncsh/internal/terminal"
	"github.com/mistweaverco/syncsh/internal/version"
)

type Server struct {
	app       *app.App
	history   *history.Service
	sessions  *terminal.Manager
	syncer    *SyncScheduler
	started   time.Time
	mu        sync.Mutex
	lastSync  Status
	cfgMu     sync.RWMutex
	controlLn net.Listener
	termLn    net.Listener
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) Open() error {
	a, err := app.Open()
	if err != nil {
		return err
	}
	if err := a.EnsureLocalDevice(); err != nil {
		_ = a.Close()
		return err
	}
	cache := history.NewCache()
	store := history.NewStore(a.DB)
	svc := history.NewService(store, cache, a.DB.SQL, a.Config.DeviceID,
		func(e history.Entry) error { return a.EnqueueHistoryCreated(e) },
		func(e history.Entry) error { return a.TombstoneEntries([]history.Entry{e}) },
	)
	s.app = a
	s.history = svc
	s.sessions = terminal.NewManagerRPC(terminal.RPC{
		Start: svc.StartCommand,
		End:   svc.CompleteCommand,
		Suggest: func(prefix, cwd string) (string, error) {
			cands, err := svc.SuggestCandidates(prefix, cwd)
			if err != nil {
				return "", err
			}
			return search.BestSuggestion(prefix, cands, search.Context{Cwd: cwd, DeviceID: a.Config.DeviceID}), nil
		},
	})
	s.syncer = newScheduler(s, a.Config.Sync.IntervalDuration())
	s.started = time.Now()
	return nil
}

func (s *Server) rebuildCache() {
	if s.history == nil {
		return
	}
	if err := s.history.Rebuild(context.Background()); err != nil {
		slog.Error("syncsh daemon: cache rebuild", "err", err)
		return
	}
	st := s.history.Cache().Stats()
	slog.Info("syncsh daemon: cache ready",
		"rows", st.RowsScanned,
		"commands", st.UniqueCommands,
		"dur", st.BuildDuration,
		"bytes", st.Bytes,
	)
	reclaimMemory()
}

func reclaimMemory() {
	debug.FreeOSMemory()
}

func (s *Server) Close() error {
	if s.controlLn != nil {
		_ = s.controlLn.Close()
	}
	if s.termLn != nil {
		_ = s.termLn.Close()
	}
	if s.sessions != nil {
		s.sessions.CloseAll()
	}
	if s.app != nil {
		return s.app.Close()
	}
	return nil
}

func (s *Server) App() *app.App { return s.app }

func (s *Server) History() *history.Service { return s.history }

func (s *Server) Sessions() *terminal.Manager { return s.sessions }

func (s *Server) Config() *config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	if s.app == nil {
		return nil
	}
	return s.app.Config
}

func (s *Server) ReloadConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	if s.app != nil {
		s.app.Config = cfg
	}
	s.cfgMu.Unlock()
	if s.syncer != nil {
		s.syncer.SetInterval(cfg.Sync.IntervalDuration())
	}
	return nil
}

func (s *Server) CompactCache() protocol.Stats {
	if s.history != nil {
		s.history.CompactCache()
	}
	reclaimMemory()
	return s.Stats()
}

// compactInterval is long enough that FreeOSMemory's stop-the-world GC
// does not hit interactive suggest/search, and short enough that Ctrl+R
// spikes and intern churn do not pin RSS for the whole session.
const compactInterval = 5 * time.Minute

func (s *Server) compactLoop(ctx context.Context) {
	tick := time.NewTicker(compactInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.runPeriodicCompact()
		}
	}
}

func (s *Server) runPeriodicCompact() {
	internedBefore := 0
	if s.history != nil {
		internedBefore = s.history.Cache().Stats().InternedStrings
	}
	var rssBefore uint64
	if rss, err := processRSS(); err == nil {
		rssBefore = rss
	}
	st := s.CompactCache()
	if st.CacheInterned < internedBefore || (rssBefore > 0 && st.RSSBytes > 0 && st.RSSBytes < rssBefore) {
		slog.Info("syncsh daemon: cache compact",
			"interned", st.CacheInterned,
			"heap", st.HeapAlloc,
			"rss", st.RSSBytes,
		)
	}
}

func (s *Server) Stats() protocol.Stats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	st := protocol.Stats{
		PID:       os.Getpid(),
		UptimeSec: int64(time.Since(s.started).Seconds()),
		HeapAlloc: ms.HeapAlloc,
	}
	if s.history != nil {
		cs := s.history.Cache().Stats()
		st.CacheEntries = cs.UniqueCommands
		st.CacheBytes = cs.Bytes
		st.CacheInterned = cs.InternedStrings
		st.CacheDirty = cs.Dirty
	}
	if s.sessions != nil {
		st.Sessions, st.ActivePTYs = s.sessions.Counts()
	}
	if s.app != nil && s.app.DB != nil && s.app.DB.SQL != nil {
		dbst := s.app.DB.SQL.Stats()
		st.DBOpenConns = dbst.OpenConnections
		st.DBInUse = dbst.InUse
	}
	s.mu.Lock()
	last := s.lastSync
	s.mu.Unlock()
	st.LastSyncAt = last.At
	st.LastSyncOK = last.OK
	st.LastSyncClass = last.Class
	st.LastSyncError = last.Error
	if rss, err := processRSS(); err == nil {
		st.RSSBytes = rss
	}
	return st
}

func (s *Server) Version() protocol.VersionRes {
	return protocol.VersionRes{Protocol: protocol.Version, Version: version.Version}
}

func (s *Server) deviceID() string {
	if s.app == nil || s.app.Config == nil {
		return ""
	}
	return s.app.Config.DeviceID
}

func (s *Server) bestSuggestion(prefix, cwd string, cands []history.Entry) string {
	return search.BestSuggestion(prefix, cands, search.Context{Cwd: cwd, DeviceID: s.deviceID()})
}

func (s *Server) suggestList(prefix, cwd string, cands []history.Entry, limit int) []string {
	return search.Suggestions(prefix, cands, search.Context{Cwd: cwd, DeviceID: s.deviceID()}, limit)
}

func (s *Server) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		Protocol:  protocol.Version,
		Ops:       []string{protocol.OpPing, protocol.OpVersion, protocol.OpCapabilities, protocol.OpHistoryStart, protocol.OpHistoryEnd, protocol.OpSuggest, protocol.OpSuggestList, protocol.OpHistorySearch, protocol.OpHistoryDelete, protocol.OpHistoryTombEnt, protocol.OpHistoryImport, protocol.OpHistoryStats, protocol.OpSyncNow, protocol.OpDaemonStats, protocol.OpDaemonStatus, protocol.OpReloadConfig, protocol.OpCompactCache, protocol.OpScreenSnapshot},
		NULCompat: true,
		HasCache:  true,
		HasPty:    true,
		HasTheme:  true,
	}
}
