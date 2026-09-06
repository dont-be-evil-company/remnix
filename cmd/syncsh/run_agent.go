package main

import (
	"fmt"

	"github.com/mistweaverco/syncsh/internal/client"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	var stdio bool
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Deprecated: ensure the SyncSH daemon is running",
		Long:  "Compatibility wrapper. Starts or connects to the unified syncsh daemon and exits. Prefer `syncsh daemon`.",
		Deprecated: "use syncsh daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgent(cmd, stdio)
		},
	}
	cmd.Flags().BoolVar(&stdio, "stdio", false, "ignored; daemon control socket is used instead")
	return cmd
}

func runAgent(cmd *cobra.Command, _ bool) error {
	c, err := client.Ensure()
	if err != nil {
		return err
	}
	_ = c.Close()
	fmt.Fprintln(cmd.OutOrStdout(), "daemon ready")
	return nil
}
