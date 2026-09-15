package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/dont-be-evil-company/remnix/internal/changelog"
	"github.com/spf13/cobra"
)

func newChangelogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "changelog {latest|all}",
		Short: "Show the baked-in changelog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sel := strings.ToLower(strings.TrimSpace(args[0]))
			var content string
			switch sel {
			case "all":
				content = changelog.Markdown
			case "latest":
				section, err := changelog.Select(changelog.Markdown, "latest")
				if err != nil {
					return err
				}
				content = section
			default:
				return fmt.Errorf("changelog requires latest or all, got %q", args[0])
			}

			out, err := glamour.RenderWithEnvironmentConfig(content)
			if err != nil {
				return fmt.Errorf("failed to render changelog: %w", err)
			}
			fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
	return cmd
}
