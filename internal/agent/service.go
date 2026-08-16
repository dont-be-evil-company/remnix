package agent

import (
	"os"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/app"
	"github.com/dont-be-evil-company/remnix/internal/history"
	"github.com/dont-be-evil-company/remnix/internal/search"
	"github.com/google/uuid"
)

type Service struct {
	app  *app.App
	host string
}

func NewService(a *app.App) *Service {
	host, _ := os.Hostname()
	return &Service{app: a, host: host}
}

func (s *Service) Suggest(prefix, cwd string) (string, error) {
	if prefix == "" {
		return "", nil
	}
	cands, err := history.NewStore(s.app.DB).SuggestPrefixCandidates(prefix, 64)
	if err != nil {
		return "", err
	}
	return search.BestSuggestion(prefix, cands, search.Context{
		Cwd:      cwd,
		DeviceID: s.app.Config.DeviceID,
	}), nil
}

func (s *Service) SuggestList(prefix, cwd string) ([]string, error) {
	if prefix == "" {
		return nil, nil
	}
	cands, err := history.NewStore(s.app.DB).SuggestPrefixCandidates(prefix, 64)
	if err != nil {
		return nil, err
	}
	return search.Suggestions(prefix, cands, search.Context{
		Cwd:      cwd,
		DeviceID: s.app.Config.DeviceID,
	}, s.app.Config.Suggest.MenuLimit()), nil
}

func (s *Service) Start(command, cwd, session, shell string) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	if history.ShouldSkip(command) {
		return id.String(), nil
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	e := history.Entry{
		ID:        id.String(),
		Command:   command,
		StartTS:   time.Now().UTC(),
		Cwd:       cwd,
		SessionID: session,
		Hostname:  s.host,
		DeviceID:  s.app.Config.DeviceID,
		Shell:     shell,
	}
	if _, err := history.NewStore(s.app.DB).Insert(e); err != nil {
		return "", err
	}
	if err := s.app.EnqueueHistoryCreated(e); err != nil {
		return "", err
	}
	return e.ID, nil
}

func (s *Service) End(id string, exit int) error {
	if id == "" {
		return nil
	}
	return history.NewStore(s.app.DB).Complete(id, time.Now().UTC(), exit)
}
