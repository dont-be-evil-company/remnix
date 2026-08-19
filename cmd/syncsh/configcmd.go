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
	cmd := &cobra.Command{Use: "remote", Short: "Manage rclone remotes"}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List configured remotes", RunE: runRemoteList})
	cmd.AddCommand(&cobra.Command{Use: "add", Short: "Add an rclone remote", RunE: runRemoteAdd})
	cmd.AddCommand(&cobra.Command{Use: "edit [name]", Short: "Edit a remote", Args: cobra.MaximumNArgs(1), RunE: runRemoteAdd})
	cmd.AddCommand(&cobra.Command{Use: "remove [name]", Short: "Remove a remote", Args: cobra.MaximumNArgs(1), RunE: runRemoteRemove})
	cmd.AddCommand(&cobra.Command{Use: "test [name]", Short: "Test a remote", Args: cobra.MaximumNArgs(1), RunE: runRemoteTest})
	cmd.AddCommand(&cobra.Command{Use: "reconnect [name]", Short: "Reconnect / reauthenticate a remote", Args: cobra.MaximumNArgs(1), RunE: runRemoteReconnect})
	cmd.AddCommand(&cobra.Command{Use: "browse [name]", Short: "Browse a remote", Args: cobra.MaximumNArgs(1), RunE: runRemoteBrowse})
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
	if a.Config.Sync.Rclone == nil || len(a.Config.Sync.Rclone.Remotes) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no rclone remotes configured")
		return nil
	}
	for _, r := range a.Config.Sync.Rclone.Remotes {
		mark := " "
		if r.ID == a.Config.Sync.Rclone.Primary {
			mark = "*"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s\t%s\n", mark, r.ID, r.Provider, r.Path)
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
	if err := wizard.ConfigureRcloneRemote(cmd.Context(), &draft); err != nil {
		return err
	}
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
	if a.Config.Sync.Rclone == nil {
		return fmt.Errorf("no remotes")
	}
	if len(args) == 0 {
		return fmt.Errorf("name required")
	}
	name := args[0]
	kept := a.Config.Sync.Rclone.Remotes[:0]
	var removed config.RemoteConfig
	found := false
	for _, r := range a.Config.Sync.Rclone.Remotes {
		if r.ID == name {
			removed = r
			found = true
			continue
		}
		kept = append(kept, r)
	}
	if !found {
		return fmt.Errorf("remote %q not found", name)
	}
	a.Config.Sync.Rclone.Remotes = kept
	if a.Config.Sync.Rclone.Primary == name {
		a.Config.Sync.Rclone.Primary = ""
		if len(kept) > 0 {
			a.Config.Sync.Rclone.Primary = kept[0].ID
		}
	}
	if removed.RcloneRemote != "" {
		if err := rclonetr.Init(config.RcloneConfigPath()); err == nil {
			rclonetr.DeleteSection(removed.RcloneRemote)
		}
	}
	return a.Config.Save()
}

func runRemoteTest(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	tr, err := a.Transport()
	if err != nil {
		return err
	}
	st, err := tr.HealthCheck(cmd.Context())
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "state=%s %s\n", st.State, redact.String(st.Message))
	if st.State == "auth_required" {
		fmt.Fprintln(cmd.OutOrStdout(), "hint: syncsh remote reconnect <name>")
	}
	return nil
}

func runRemoteBrowse(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	tr, err := a.Transport()
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
