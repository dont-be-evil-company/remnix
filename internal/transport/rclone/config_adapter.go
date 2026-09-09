package rclone

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/rc"
)

type Question struct {
	State      string
	Name       string
	Help       string
	Required   bool
	Sensitive  bool
	IsPassword bool
	OAuth      bool
	URL        string
	Default    string
	Choices    []Choice
	Exclusive  bool
	Error      string
}

type Choice struct {
	Value string
	Help  string
}

type ConfigSession struct {
	Name     string
	Type     string
	State    string
	Finished bool
	Params   rc.Params
}

func NewConfigSession(backendType, name string) *ConfigSession {
	return NewConfigSessionWith(backendType, name, nil)
}

func NewConfigSessionWith(backendType, name string, params rc.Params) *ConfigSession {
	if name == "" {
		name = StagedName()
	}
	if params == nil {
		params = rc.Params{}
	}
	return &ConfigSession{Name: name, Type: backendType, Params: params}
}

func StagedName() string {
	return "remnix-tmp-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}

func (s *ConfigSession) Step(ctx context.Context, result string) (*Question, error) {
	mu.Lock()
	defer mu.Unlock()
	opts := config.UpdateRemoteOpt{
		NonInteractive: true,
		Continue:       s.State != "",
		All:            true,
		State:          s.State,
		Result:         result,
		Obscure:        true,
	}
	var (
		out *fs.ConfigOut
		err error
	)
	if s.State == "" {
		out, err = config.CreateRemote(ctx, s.Name, s.Type, s.Params, opts)
	} else {
		out, err = config.UpdateRemote(ctx, s.Name, s.Params, opts)
	}
	if err != nil {
		return nil, err
	}
	if out == nil || out.State == "" {
		s.Finished = true
		s.State = ""
		return nil, nil
	}
	s.State = out.State
	q := &Question{State: out.State, Error: out.Error}
	if out.Option != nil {
		opt := out.Option
		q.Name = opt.Name
		q.Help = opt.Help
		q.Required = opt.Required
		q.Sensitive = opt.Sensitive || opt.IsPassword
		q.IsPassword = opt.IsPassword
		if opt.Default != nil {
			q.Default = fmt.Sprint(opt.Default)
		}
		q.Exclusive = opt.Exclusive
		for _, ex := range opt.Examples {
			q.Choices = append(q.Choices, Choice{Value: ex.Value, Help: ex.Help})
		}
	}
	q.URL = firstURL(q.Help + "\n" + out.Result + "\n" + out.Error)
	if out.OAuth != nil {
		q.OAuth = true
		if q.URL == "" {
			q.URL = firstURL(fmt.Sprint(out.OAuth))
		}
	}
	if q.URL != "" || strings.Contains(strings.ToLower(q.Name), "token") || strings.Contains(strings.ToLower(q.Help), "oauth") {
		q.OAuth = true
	}
	return q, nil
}

func (s *ConfigSession) Commit(finalName string) error {
	if s == nil {
		return fmt.Errorf("nil config session")
	}
	if finalName == "" || finalName == s.Name {
		HardenRemote(s.Name)
		return nil
	}
	if err := RenameSection(s.Name, finalName); err != nil {
		return err
	}
	s.Name = finalName
	HardenRemote(s.Name)
	return nil
}

func (s *ConfigSession) Abort() {
	if s == nil || s.Name == "" {
		return
	}
	DeleteSection(s.Name)
}

func firstURL(s string) string {
	for _, p := range []string{"https://", "http://"} {
		i := strings.Index(s, p)
		if i < 0 {
			continue
		}
		u := s[i:]
		if j := strings.IndexAny(u, " \n\t<>\""); j > 0 {
			u = u[:j]
		}
		return u
	}
	return ""
}

func BackendTypes() []string {
	var out []string
	for _, ri := range fs.Registry {
		if ri == nil {
			continue
		}
		out = append(out, ri.Prefix)
	}
	return out
}
