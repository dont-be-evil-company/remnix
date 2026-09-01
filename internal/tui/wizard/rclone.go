package wizard

import (
	"context"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/mistweaverco/syncsh/internal/browser"
	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/redact"
	"github.com/mistweaverco/syncsh/internal/repository"
	"github.com/mistweaverco/syncsh/internal/transport"
	rclonetr "github.com/mistweaverco/syncsh/internal/transport/rclone"
	"github.com/mistweaverco/syncsh/internal/transport/rclone/providers"
	"github.com/mistweaverco/syncsh/internal/tui/picker"
	"github.com/rclone/rclone/fs/rc"
)

func ConfigureRcloneRemote(ctx context.Context, cfg *config.Config) error {
	return ConfigureRcloneRemoteMode(ctx, cfg, false)
}

func ConfigureRcloneRemoteMode(ctx context.Context, cfg *config.Config, join bool) error {
	if err := rclonetr.Init(config.RcloneConfigPath()); err != nil {
		return err
	}
	imported, err := maybeImportRemote(ctx)
	if err != nil {
		return err
	}
	var (
		def     providers.Definition
		section string
		backend string
		params  rc.Params
		logical string
	)
	if imported != "" {
		section = imported
		logical = imported
		def, _ = providers.ByID("custom")
		def.DisplayName = imported
	} else {
		defs := providers.Featured()
		opts := make([]huh.Option[string], 0, len(defs)+1)
		for _, d := range defs {
			opts = append(opts, huh.NewOption(d.DisplayName+" - "+d.Description, d.ID))
		}
		opts = append(opts, huh.NewOption("Generic rclone backend (advanced)", "custom"))
		id := "google-drive"
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Provider").Description("Tokens live in the local data directory (next to local.yaml), not in config.yaml.").Options(opts...).Value(&id),
		))
		if err := form.RunWithContext(ctx); err != nil {
			return err
		}
		var ok bool
		def, ok = providers.ByID(id)
		if !ok {
			return fmt.Errorf("unknown provider %s", id)
		}
		fmt.Fprintln(os.Stderr, def.Intro())
		for _, lim := range def.Limitations {
			fmt.Fprintln(os.Stderr, "Note:", lim)
		}
		backend = def.RcloneType
		if backend == "" {
			types := rclonetr.BackendTypes()
			var t string
			to := make([]huh.Option[string], 0, len(types))
			for _, n := range types {
				to = append(to, huh.NewOption(n, n))
			}
			form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Backend").Options(to...).Value(&t)))
			if err := form.RunWithContext(ctx); err != nil {
				return err
			}
			backend = t
		}
		logical = "syncsh-" + def.ID
		_ = huh.NewForm(huh.NewGroup(huh.NewInput().Title("Remote name").Value(&logical))).RunWithContext(ctx)
		params, err = collectProviderParams(ctx, def)
		if err != nil {
			return err
		}
		sess := rclonetr.NewConfigSessionWith(backend, rclonetr.StagedName(), params)
		if err := runConfigSession(ctx, sess); err != nil {
			sess.Abort()
			return err
		}
		if err := sess.Commit(logical); err != nil {
			sess.Abort()
			return err
		}
		section = sess.Name
	}

	fmt.Fprintln(os.Stderr, "Opening remote (quota check only; not listing Drive)...")
	tr, err := rclonetr.Open(ctx, section, "")
	if err != nil {
		return fmt.Errorf("open remote: %w", err)
	}
	st, err := tr.HealthCheck(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "connection test:", redact.String(st.Message))
	if st.State != transport.HealthOK && !def.AllowSaveOnFailedTest() {
		return fmt.Errorf("connection test failed (%s): %s", st.State, redact.String(st.Message))
	}
	if st.State != transport.HealthOK && def.AllowSaveOnFailedTest() {
		save := false
		_ = huh.NewForm(huh.NewGroup(huh.NewConfirm().Title("Save even though the connection test failed?").Value(&save))).RunWithContext(ctx)
		if !save {
			rclonetr.DeleteSection(section)
			return fmt.Errorf("cancelled")
		}
	}
	path, err := pickRcloneFolder(ctx, tr, logical, join)
	if err != nil {
		rclonetr.DeleteSection(section)
		return err
	}
	rooted, err := rclonetr.Open(ctx, section, path)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Checking for syncsh metadata (not a full Drive scan)...")
	rep, err := repository.Probe(ctx, rooted)
	if err != nil {
		return err
	}
	path, _, rep, err = resolveRcloneDest(ctx, section, path, rooted, rep, join)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "summary:")
	fmt.Fprintln(os.Stderr, "  provider:", def.DisplayName)
	fmt.Fprintln(os.Stderr, "  remote:", logical)
	fmt.Fprintln(os.Stderr, "  path:", path)
	fmt.Fprintln(os.Stderr, "  probe:", rep.Result)
	on := true
	if cfg.Sync.Rclone == nil {
		cfg.Sync.Rclone = &config.RcloneConfig{}
	}
	cfg.Sync.Enabled = &on
	cfg.Sync.Transport = "rclone"
	cfg.Sync.Rclone.Engine = config.RcloneEngineEmbedded
	cfg.Sync.Rclone.Primary = logical
	replaced := false
	for i, r := range cfg.Sync.Rclone.Remotes {
		if r.ID == logical {
			cfg.Sync.Rclone.Remotes[i] = config.RemoteConfig{
				ID: logical, DisplayName: def.DisplayName, RcloneRemote: section,
				Provider: def.ID, Path: path, Enabled: true,
			}
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Sync.Rclone.Remotes = append(cfg.Sync.Rclone.Remotes, config.RemoteConfig{
			ID: logical, DisplayName: def.DisplayName, RcloneRemote: section,
			Provider: def.ID, Path: path, Enabled: true,
		})
	}
	return nil
}

func maybeImportRemote(ctx context.Context) (string, error) {
	names, err := rclonetr.ListUserRemotes()
	if err != nil || len(names) == 0 {
		return "", err
	}
	use := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Import a copy of an existing rclone remote?").
			Description("The original ~/.config/rclone/rclone.conf is not modified.").
			Value(&use),
	))
	if err := form.RunWithContext(ctx); err != nil || !use {
		return "", err
	}
	src := names[0]
	opts := make([]huh.Option[string], 0, len(names))
	for _, n := range names {
		opts = append(opts, huh.NewOption(n, n))
	}
	form = huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("User rclone remote").Options(opts...).Value(&src)))
	if err := form.RunWithContext(ctx); err != nil {
		return "", err
	}
	dst := src
	_ = huh.NewForm(huh.NewGroup(huh.NewInput().Title("Name inside syncsh").Value(&dst))).RunWithContext(ctx)
	if err := rclonetr.ImportUserRemote(src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func pickRcloneFolder(ctx context.Context, tr *rclonetr.Transport, logical string, join bool) (string, error) {
	title := "Choose folder / prefix on " + logical + "  (Enter opens, Space/Ctrl+Enter selects, n creates a folder)"
	if join {
		title += " - pick the existing syncsh folder"
	}
	res, err := picker.Run(rclonetr.NewBrowser(tr), picker.Options{
		Title:       title,
		CreateMode:  true,
		ConfirmDest: false,
	})
	if err != nil {
		return "", err
	}
	if res.Canceled {
		return "", fmt.Errorf("cancelled")
	}
	return strings.Trim(res.Path, "/"), nil
}

func resolveRcloneDest(ctx context.Context, section, path string, rooted *rclonetr.Transport, rep repository.Report, join bool) (string, *rclonetr.Transport, repository.Report, error) {
	for {
		switch {
		case join && rep.Result == repository.Valid:
			return path, rooted, rep, nil
		case !join && (rep.Result == repository.Valid || rep.Result == repository.Partial || rep.Result == repository.UnsupportedVersion):
			return path, rooted, rep, fmt.Errorf("this folder already looks like a syncsh repository (%s); use 'syncsh device add' to join", rep.Result)
		case !join && rep.Result == repository.Empty && path != "":
			return path, rooted, rep, nil
		}
		action := "subfolder"
		desc := fmt.Sprintf("This path is %s (%s).", rep.Result, redact.String(rep.Message))
		if join {
			desc += " Join needs an existing syncsh repository. Create a subfolder named syncsh, or pick another folder."
		} else {
			desc += " Create a dedicated syncsh subfolder so other files on this remote are not scanned."
		}
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("This folder is not a syncsh repository yet").
				Description(desc).
				Options(
					huh.NewOption(`Create a "syncsh" subfolder here`, "subfolder"),
					huh.NewOption("Use this folder anyway", "use"),
					huh.NewOption("Cancel", "cancel"),
				).Value(&action),
		))
		if err := form.RunWithContext(ctx); err != nil {
			return path, rooted, rep, err
		}
		switch action {
		case "cancel":
			return path, rooted, rep, fmt.Errorf("cancelled")
		case "use":
			if join && rep.Result != repository.Valid {
				return path, rooted, rep, fmt.Errorf("join requires a valid syncsh repository (got %s): %s", rep.Result, rep.Message)
			}
			return path, rooted, rep, nil
		default:
			if err := rooted.Mkdir(ctx, "syncsh"); err != nil {
				return path, rooted, rep, err
			}
			sub := "syncsh"
			if path != "" {
				sub = strings.Trim(path, "/") + "/syncsh"
			}
			next, err := rclonetr.Open(ctx, section, sub)
			if err != nil {
				return path, rooted, rep, err
			}
			fmt.Fprintln(os.Stderr, "Created", sub)
			nrep, err := repository.Probe(ctx, next)
			if err != nil {
				return sub, next, nrep, err
			}
			path, rooted, rep = sub, next, nrep
			if join && nrep.Result != repository.Valid {
				return sub, next, nrep, fmt.Errorf("created %s but it is not an existing repository (got %s). Run syncsh setup on the first device, or pick the folder that already contains metadata/", sub, nrep.Result)
			}
			if !join {
				return sub, next, nrep, nil
			}
		}
	}
}

func collectProviderParams(ctx context.Context, def providers.Definition) (rc.Params, error) {
	p := rc.Params{}
	switch def.ID {
	case "s3":
		kind := "AWS"
		form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("S3 flavor").Options(
			huh.NewOption("Amazon AWS", "AWS"),
			huh.NewOption("S3-compatible (MinIO, R2, B2, ...)", "Other"),
		).Value(&kind)))
		if err := form.RunWithContext(ctx); err != nil {
			return nil, err
		}
		p["provider"] = kind
		if kind == "Other" {
			ep := ""
			_ = huh.NewForm(huh.NewGroup(huh.NewInput().Title("Endpoint URL").Value(&ep))).RunWithContext(ctx)
			if ep != "" {
				p["endpoint"] = ep
			}
		}
	case "gcs":
		mode := "adc"
		form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Google Cloud auth").Options(
			huh.NewOption("Application Default Credentials", "adc"),
			huh.NewOption("Service account JSON file", "sa"),
		).Value(&mode)))
		if err := form.RunWithContext(ctx); err != nil {
			return nil, err
		}
		if mode == "adc" {
			p["env_auth"] = true
		} else {
			sa := ""
			_ = huh.NewForm(huh.NewGroup(huh.NewInput().Title("Service account JSON path").Value(&sa))).RunWithContext(ctx)
			sa = config.Expand(sa)
			if sa == "" {
				return nil, fmt.Errorf("service account file required")
			}
			p["service_account_file"] = sa
		}
	case "webdav":
		fmt.Fprintln(os.Stderr, "If the URL is http:// (not https://), credentials may be sent in the clear.")
	case "smb":
		guest := false
		_ = huh.NewForm(huh.NewGroup(huh.NewConfirm().Title("Connect as guest?").Value(&guest))).RunWithContext(ctx)
		if guest {
			p["spn"] = ""
			p["user"] = "guest"
		}
	case "azure-files":
		fmt.Fprintln(os.Stderr, "Account key, SAS, connection string, and Azure identity are all valid; pick one in the next questions.")
	}
	return p, nil
}

func runConfigSession(ctx context.Context, sess *rclonetr.ConfigSession) error {
	q, err := sess.Step(ctx, "")
	if err != nil {
		return err
	}
	for q != nil && !sess.Finished {
		ans, err := askQuestion(ctx, q)
		if err != nil {
			return err
		}
		q, err = sess.Step(ctx, ans)
		if err != nil {
			return err
		}
	}
	return nil
}

func askQuestion(ctx context.Context, q *rclonetr.Question) (string, error) {
	if q.Error != "" {
		fmt.Fprintln(os.Stderr, redact.String(q.Error))
	}
	help := q.Help
	url := q.URL
	if url == "" {
		url = extractURL(help)
	}
	if url != "" {
		if q.OAuth {
			_ = browser.Open(url)
		}
		fmt.Fprintln(os.Stderr, "If the browser did not open, visit:", url)
	}
	ans := q.Default
	if len(q.Choices) > 0 {
		opts := make([]huh.Option[string], 0, len(q.Choices))
		for _, c := range q.Choices {
			label := c.Value
			if c.Help != "" {
				label = c.Value + " - " + c.Help
			}
			opts = append(opts, huh.NewOption(label, c.Value))
		}
		form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title(q.Name).Description(help).Options(opts...).Value(&ans)))
		return ans, form.RunWithContext(ctx)
	}
	in := huh.NewInput().Title(q.Name).Description(help).Value(&ans)
	if q.IsPassword {
		in = in.EchoMode(huh.EchoModePassword)
	}
	form := huh.NewForm(huh.NewGroup(in))
	return ans, form.RunWithContext(ctx)
}

func extractURL(s string) string {
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
