package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/spf13/cobra"
)

// addressCommand makes an https address the server's own from the command
// line — the "do it yourself" way when Tailscale can't be used (a closed
// network with a company certificate, port forwarding, a proxy of one's
// own). The running server takes it within seconds; the command then checks
// the address as the server does.
func addressCommand(pb *pocketbase.PocketBase) *cobra.Command {
	var address, certificate, key string
	port := 443
	command := &cobra.Command{
		Use:   "address",
		Short: "Make an https address this server's own, with a certificate it serves or behind your proxy",
		Long: "Sets the address everything uses (links, invitations, agents, login providers). With --cert and --key the server takes HTTPS " +
			"itself on --port; without them your own reverse proxy does.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			certificatePEM, keyPEM := "", ""
			if certificate != "" || key != "" {
				read := func(path string) (string, error) {
					data, err := os.ReadFile(path)
					return string(data), err
				}
				var err error
				if certificatePEM, err = read(certificate); err != nil {
					return fmt.Errorf("the certificate couldn't be read: %w", err)
				}
				if keyPEM, err = read(key); err != nil {
					return fmt.Errorf("the key couldn't be read: %w", err)
				}
			}
			settings, err := setOwnAddress(pb, address, certificatePEM, keyPEM, port)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s is this server's address now; the running server takes it within seconds.\n", settings.Address)
			time.Sleep(5 * time.Second)
			check := checkOwnAddress(cmd.Context(), settings.Address)
			if len(check.Resolves) > 0 {
				fmt.Fprintf(out, "Resolves to %s.\n", strings.Join(check.Resolves, ", "))
			}
			if check.NotAfter > 0 {
				fmt.Fprintf(out, "Certificate for %s until %s.\n", strings.Join(check.Names, ", "), time.UnixMilli(check.NotAfter).Format("2006-01-02"))
			}
			switch check.Problem {
			case "":
				fmt.Fprintln(out, "Checked: open it on a phone to see it from outside.")
			case "dns":
				fmt.Fprintln(out, "Its name doesn't resolve yet: add the DNS record, then run this again.")
			case "unreachable":
				fmt.Fprintln(out, "Nothing answers HTTPS there yet: check the proxy, the port forwarding or the firewall.")
			case "name":
				fmt.Fprintln(out, "The certificate answering there is for another name.")
			case "expired":
				fmt.Fprintln(out, "The certificate answering there has expired.")
			case "untrusted":
				fmt.Fprintln(out, "This computer doesn't trust the certificate's issuer; devices that don't trust it will refuse it too.")
			}
			// What a Claude connector can't do there, if anything (connector_reach.go).
			if note := claudeConnectorPortNote(settings.Address); note != "" {
				fmt.Fprintln(out, note)
			}
			return nil
		},
	}
	command.Flags().StringVar(&address, "address", "", "the https address, such as https://wiki.example.com")
	command.Flags().StringVar(&certificate, "cert", "", "the certificate chain (PEM file) when the server takes HTTPS itself")
	command.Flags().StringVar(&key, "key", "", "the certificate's private key (PEM file)")
	command.Flags().IntVar(&port, "port", 443, "the port the server takes HTTPS on with --cert")
	_ = command.MarkFlagRequired("address")
	return command
}
