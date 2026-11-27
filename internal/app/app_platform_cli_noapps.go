//go:build leafen_noapps

package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// RunAppCommand says this build has no app platform.
func RunAppCommand([]string) int {
	fmt.Fprintln(os.Stderr, "Error: this Leafen build has no app platform")
	return 1
}

// A build without the app platform has no `leafen app` commands.
func appCommand() *cobra.Command {
	return &cobra.Command{
		Use: "app", Short: "Leafen apps (not in this build)", Hidden: true, DisableFlagParsing: true,
		RunE: func(*cobra.Command, []string) error { return errors.New("this Leafen build has no app platform") },
	}
}
