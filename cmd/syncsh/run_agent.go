package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/mistweaverco/syncsh/internal/agent"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	var stdio bool
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run the local history agent (unix socket or stdio)",
		Long:  "Keeps SQLite open and answers suggest/history RPCs so the shell does not spawn syncsh on every keystroke. If the socket is already serving, agent exits 0.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgent(cmd, stdio)
		},
	}
	cmd.Flags().BoolVar(&stdio, "stdio", false, "serve NUL-delimited RPCs on stdin/stdout (coproc fallback)")
	return cmd
}

func runAgent(cmd *cobra.Command, stdio bool) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	svc := agent.NewService(a)
	if stdio {
		return agent.Serve(cmd.InOrStdin(), cmd.OutOrStdout(), svc)
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return agent.ListenAndServe(ctx, svc)
}
