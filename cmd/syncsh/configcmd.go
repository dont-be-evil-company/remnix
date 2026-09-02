package main

import (
	"fmt"

	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/redact"
	rclonetr "github.com/mistweaverco/syncsh/internal/transport/rclone"
	"github.com/mistweaverco/syncsh/internal/tui/picker"
	"github.com/mistweaverco/syncsh/internal/tui/wizard"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Interactive configuration wizard",
		RunE:  runConfig,
	}
	cmd.AddCommand(&cobra.Command{Use: "sync", Short: "Configure synchronization", RunE: runConfig})
	return cmd
}

func newRemoteCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "remote", Short: "Manage sync endpoints"}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List configured endpoints", RunE: runRemoteList})
	cmd.AddCommand(&cobra.Command{Use: "add", Short: "Add a sync endpoint", RunE: runRemoteAdd})
	cmd.AddCommand(&cobra.Command{Use: "edit [name]", Short: "Edit an endpoint", Args: cobra.MaximumNArgs(1), RunE: runRemoteAdd})
	cmd.AddCommand(&cobra.Command{Use: "remove [name]", Short: "Remove an endpoint", Args: cobra.MaximumNArgs(1), RunE: runRemoteRemove})
	cmd.AddCommand(&cobra.Command{Use: "test [name]", Short: "Test an endpoint", Args: cobra.MaximumNArgs(1), RunE: runRemoteTest})
	cmd.AddCommand(&cobra.Command{Use: "reconnect [name]", Short: "Reconnect / reauthenticate an rclone endpoint", Args: cobra.MaximumNArgs(1), RunE: runRemoteReconnect})
	cmd.AddCommand(&cobra.Command{Use: "browse [name]", Short: "Browse an endpoint", Args: cobra.MaximumNArgs(1), RunE: runRemoteBrowse})
	return cmd
}

func runConfig(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	draft := *a.Config
	if err := wizard.RunConfig(cmd.Context(), &draft); err != nil {
		return err
	}
	*a.Config = draft
	return a.Config.Save()
}

func runRemoteList(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	if len(a.Config.Sync.Endpoints) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no endpoints configured")
		return nil
	}
	for _, r := range a.Config.Sync.Endpoints {
		mark := " "
		if r.Enabled {
			mark = "*"
		}
		kind := r.Type
		if r.Provider != "" {
			kind = r.Provider
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s\t%s\n", mark, r.ID, kind, r.Path)
	}
	return nil
}

func runRemoteAdd(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	draft := *a.Config
	ep, err := wizard.AddEndpoint(cmd.Context(), &draft, false)
	if err != nil {
		return err
	}
	if ep.ID == "" {
		return fmt.Errorf("cancelled")
	}
	on := true
	draft.Sync.Enabled = &on
	draft.Sync.UpsertEndpoint(ep)
	*a.Config = draft
	return a.Config.Save()
}

func runRemoteReconnect(cmd *cobra.Command, args []string) error {
	fmt.Fprintln(cmd.ErrOrStderr(), "Re-run the provider wizard. For iCloud Drive this may require Apple ID + 2FA.")
	if len(args) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Reconnecting %s\n", args[0])
	}
	return runRemoteAdd(cmd, args)
}

func runRemoteRemove(cmd *cobra.Command, args []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	if len(args) == 0 {
		return fmt.Errorf("name required")
	}
	name := args[0]
	removed, found := a.Config.Sync.RemoveEndpoint(name)
	if !found {
		return fmt.Errorf("endpoint %q not found", name)
	}
	if removed.Type == config.TypeRclone && removed.RcloneRemote != "" {
		if err := rclonetr.Init(config.RcloneConfigPath()); err == nil {
			rclonetr.DeleteSection(removed.RcloneRemote)
		}
	}
	return a.Config.Save()
}

func pickEndpoint(cfg *config.Config, name string) (config.Endpoint, error) {
	if name != "" {
		ep, ok := cfg.Sync.Endpoint(name)
		if !ok {
			return config.Endpoint{}, fmt.Errorf("endpoint %q not found", name)
		}
		return ep, nil
	}
	eps := cfg.Sync.EnabledEndpoints()
	if len(eps) == 0 {
		if len(cfg.Sync.Endpoints) == 0 {
			return config.Endpoint{}, fmt.Errorf("no endpoints configured")
		}
		return cfg.Sync.Endpoints[0], nil
	}
	return eps[0], nil
}

func runRemoteTest(cmd *cobra.Command, args []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	ep, err := pickEndpoint(a.Config, name)
	if err != nil {
		return err
	}
	tr, err := a.OpenTransport(ep)
	if err != nil {
		return err
	}
	st, err := tr.HealthCheck(cmd.Context())
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "state=%s %s\n", st.State, redact.String(st.Message))
	if st.State == "auth_required" {
		fmt.Fprintln(cmd.OutOrStdout(), "hint: syncsh remote reconnect "+ep.ID)
	}
	return nil
}

func runRemoteBrowse(cmd *cobra.Command, args []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	ep, err := pickEndpoint(a.Config, name)
	if err != nil {
		return err
	}
	tr, err := a.OpenTransport(ep)
	if err != nil {
		return err
	}
	rt, ok := tr.(*rclonetr.Transport)
	if !ok {
		res, err := wizard.PickLocalDir(cmd.Context(), "Browse", false)
		if err != nil {
			return err
		}
		if res.Canceled {
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), res.Path)
		return nil
	}
	res, err := picker.Run(rclonetr.NewBrowser(rt), picker.Options{Title: "Browse remote", ConfirmDest: false})
	if err != nil {
		return err
	}
	if res.Canceled {
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), res.Path)
	return nil
}
