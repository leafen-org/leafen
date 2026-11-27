package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/spf13/cobra"
)

// tailscaleCommand turns remote access to the administrator's own Tailscale
// from the command line, for an install whose screen hasn't been opened yet
// (a NAS, Docker, an agent's terminal; decided 2026-10-19): the running
// server picks the setting up within seconds, and the command prints where to
// sign in and to allow the public address — any browser, a phone's too — and
// then the address.
func tailscaleCommand(pb *pocketbase.PocketBase) *cobra.Command {
	return &cobra.Command{
		Use:   "tailscale",
		Short: "Give this server a free public address with your own Tailscale account",
		Long: "Turns remote access to Tailscale. The running server logs in to your Tailscale account (this prints where, for any browser) " +
			"and takes a public https address from Tailscale Funnel. Connections pass Tailscale but are decrypted on this server.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !tailscaleBuiltIn {
				return errors.New("Tailscale isn't built into Leafen on phones; use it on an always-on server")
			}
			started := time.Now().UnixMilli()
			if _, err := pb.DB().NewQuery(`
				INSERT INTO {{leafen_server_tunnel}} ([[id]], [[mode]], [[updated]]) VALUES ('tunnel', 'tailscale', {:updated})
				ON CONFLICT ([[id]]) DO UPDATE SET [[mode]] = 'tailscale', [[updated]] = excluded.[[updated]]
			`).Bind(dbx.Params{"updated": started}).Execute(); err != nil {
				return fmt.Errorf("remote access couldn't be changed: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Remote access now uses Tailscale. Waiting for the server…")
			statusPath := filepath.Join(pb.DataDir(), tailscaleFolderName, tailscaleStatusFile)
			shownLogin, shownAllow, hinted, shownPublishing := "", "", false, false
			deadline := time.Now().Add(20 * time.Minute)
			for time.Now().Before(deadline) {
				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				case <-time.After(2 * time.Second):
				}
				status := tailscaleStatus{}
				if data, err := os.ReadFile(statusPath); err == nil {
					_ = json.Unmarshal(data, &status)
				}
				if status.At < started {
					if !hinted && time.Since(time.UnixMilli(started)) > 30*time.Second {
						hinted = true
						fmt.Fprintln(out, "No running server has answered yet. Start it (leafen serve, or start the container); it carries on from here.")
					}
					continue
				}
				switch {
				case status.Address != "":
					fmt.Fprintf(out, "Public address: https://%s\n", status.Address)
					return nil
				case status.Login != "" && status.Login != shownLogin:
					shownLogin = status.Login
					fmt.Fprintf(out, "Open this address in any browser and log in to Tailscale (make an account there if you have none):\n\n  %s\n\n", status.Login)
				case status.Allow != "" && status.Allow != shownAllow:
					shownAllow = status.Allow
					fmt.Fprintf(out, "Now allow this server a public address (HTTPS and Funnel) in Tailscale:\n\n  %s\n\n", status.Allow)
				case status.Publishing && !shownPublishing:
					shownPublishing = true
					fmt.Fprintln(out, "Tailscale is publishing the address on the internet; the first time this can take a few minutes.")
				}
			}
			return errors.New("the public address didn't come up in 20 minutes; " +
				mcpScreen("", "settings", "server", "serverRemoteAccess") + " shows how it stands")
		},
	}
}
