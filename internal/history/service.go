package history

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

type EnqueueFunc func(Entry) error

type Service struct {
	store            *Store
	cache            *Cache
	sql              *sql.DB
	deviceID         string
	host             string
	enqueueCreated   EnqueueFunc
	enqueueTombstone EnqueueFunc
	dataVersion      int64
	rebuildMu        sync.Mutex
}

func NewService(store *Store, cache *Cache, sqlDB *sql.DB, deviceID string, enqueueCreated, enqueueTombstone EnqueueFunc) *Service {
	host, _ := os.Hostname()
	return &Service{
		store:            store,
		cache:            cache,
		sql:              sqlDB,
		deviceID:         deviceID,
		host:             host,
		enqueueCreated:   enqueueCreated,
		enqueueTombstone: enqueueTombstone,
	}
}

func (s *Service) Cache() *Cache { return s.cache }

func (s *Service) Rebuild(ctx context.Context) error {
	s.rebuildMu.Lock()
	defer s.rebuildMu.Unlock()
	return s.rebuildLocked(ctx)
}

func (s *Service) CompactCache() {
	if s == nil || s.cache == nil {
		return
	}
	s.rebuildMu.Lock()
	defer s.rebuildMu.Unlock()
	s.cache.Compact()
}

func (s *Service) rebuildLocked(ctx context.Context) error {
	if err := s.cache.Rebuild(ctx, s.store); err != nil {
		return err
	}
	s.dataVersion, _ = pragmaDataVersion(s.sql)
	return nil
}

func (s *Service) EnsureFresh(ctx context.Context) error {
	if s.cache.Dirty() {
		return s.Rebuild(ctx)
	}
	cur, err := pragmaDataVersion(s.sql)
	if err != nil {
		return err
	}
	if s.dataVersion != 0 && cur != s.dataVersion {
		s.cache.MarkDirty()
		return s.Rebuild(ctx)
	}
	return nil
}

func (s *Service) SuggestCandidates(prefix, cwd string) ([]Entry, error) {
	if prefix == "" {
		return nil, nil
	}
	if err := s.EnsureFresh(context.Background()); err != nil {
		return nil, err
	}
	_ = cwd
	return s.cache.Suggest(prefix), nil
}

func (s *Service) SearchCandidates(query, cwd, session string, exact bool, limit int) ([]Entry, error) {
	if err := s.EnsureFresh(context.Background()); err != nil {
		return nil, err
	}
	_ = query
	_ = cwd
	_ = session
	_ = exact
	_ = limit
	return s.cache.AllUnique(), nil
}

func (s *Service) StartCommand(command, cwd, session, shell string) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	if ShouldSkip(command) {
		return id.String(), nil
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	e := Entry{
		ID:        id.String(),
		Command:   command,
		StartTS:   time.Now().UTC(),
		Cwd:       cwd,
		SessionID: session,
		Hostname:  s.host,
		DeviceID:  s.deviceID,
		Shell:     shell,
	}
	if _, err := s.store.Insert(e); err != nil {
		return "", err
	}
	if s.enqueueCreated != nil {
		if err := s.enqueueCreated(e); err != nil {
			return "", err
		}
	}
	if err := s.afterCommit(func() { s.cache.ApplyCreated(e) }); err != nil {
		return e.ID, err
	}
	return e.ID, nil
}

func (s *Service) CompleteCommand(id string, exit int) error {
	if id == "" {
		return nil
	}
	end := time.Now().UTC()
	if err := s.store.Complete(id, end, exit); err != nil {
		return err
	}
	return s.afterCommit(func() { s.cache.ApplyCompleted(id, end, exit) })
}

func (s *Service) TombstoneCommand(command string) error {
	if command == "" {
		return nil
	}
	entries, err := s.store.ListByCommand(command)
	if err != nil {
		return err
	}
	return s.TombstoneEntries(entries)
}

func (s *Service) TombstoneEntries(entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	ids := make([]string, 0, len(entries))
	cmds := map[string]struct{}{}
	for _, e := range entries {
		if s.enqueueTombstone != nil {
			if err := s.enqueueTombstone(e); err != nil {
				return err
			}
		} else if err := s.store.Tombstone(e.ID); err != nil {
			return err
		}
		ids = append(ids, e.ID)
		if e.Command != "" {
			cmds[e.Command] = struct{}{}
		}
	}
	return s.afterCommit(func() {
		s.cache.ApplyTombstoned(ids)
		for cmd := range cmds {
			s.cache.ApplyTombstoneCommand(cmd)
		}
	})
}

func (s *Service) Import(entries []Entry) (int, error) {
	var inserted int
	var created []Entry
	for _, e := range entries {
		if ShouldSkip(e.Command) {
			continue
		}
		if e.ID == "" {
			id, err := uuid.NewV7()
			if err != nil {
				return inserted, err
			}
			e.ID = id.String()
		}
		if e.DeviceID == "" {
			e.DeviceID = s.deviceID
		}
		ok, err := s.store.Insert(e)
		if err != nil {
			return inserted, err
		}
		if !ok {
			continue
		}
		inserted++
		if s.enqueueCreated != nil {
			if err := s.enqueueCreated(e); err != nil {
				return inserted, err
			}
		}
		created = append(created, e)
	}
	if err := s.afterCommit(func() {
		s.cache.ApplyBatch(ChangeSet{Created: created})
	}); err != nil {
		return inserted, err
	}
	return inserted, nil
}

func (s *Service) ApplyRemoteChanges(cs ChangeSet) error {
	if cs.Empty() {
		return s.noteVersion()
	}
	return s.afterCommit(func() { s.cache.ApplyBatch(cs) })
}

func (s *Service) afterCommit(update func()) error {
	s.rebuildMu.Lock()
	defer s.rebuildMu.Unlock()
	func() {
		defer func() {
			if recover() != nil {
				s.cache.MarkDirty()
			}
		}()
		update()
	}()
	if s.cache.Dirty() {
		return s.rebuildLocked(context.Background())
	}
	return s.noteVersion()
}

func (s *Service) noteVersion() error {
	v, err := pragmaDataVersion(s.sql)
	if err != nil {
		return err
	}
	s.dataVersion = v
	return nil
}

func pragmaDataVersion(sqlDB *sql.DB) (int64, error) {
	if sqlDB == nil {
		return 0, nil
	}
	var v int64
	if err := sqlDB.QueryRow(`PRAGMA data_version`).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}
