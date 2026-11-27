//go:build !leafen_noapps

package app

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

// appCatalogCommand makes a catalog's signed index from the entries
// `leafen app publish` wrote (docs/apps.md "Distribution"): every
// apps/<id>/app.json under the folder, its featured.json (app ids) and
// revoked.json (id, packageHash, reason) when it has them, into catalog.json
// and catalog.json.sig, signed by the catalog's list key. It reads the
// result back as a server would, so a catalog it writes is one Leafen
// accepts. The official catalog is made this way at each release.
func appCatalogCommand() *cobra.Command {
	keyPath, name := "", ""
	command := &cobra.Command{
		Use: "catalog <folder>", Short: "Make a catalog's signed index from the entries `leafen app publish` wrote", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			private, _, err := readAppKeyFile(keyPath)
			if err != nil {
				return err
			}
			folder := args[0]
			paths, err := filepath.Glob(filepath.Join(folder, "apps", "*", "app.json"))
			if err != nil {
				return err
			}
			if len(paths) == 0 {
				return errors.New("no apps/<id>/app.json under " + folder + " (write them with `leafen app publish`)")
			}
			sort.Strings(paths)
			apps := make([]json.RawMessage, 0, len(paths))
			for _, path := range paths {
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if !json.Valid(raw) {
					return errors.New(path + " isn't JSON")
				}
				apps = append(apps, json.RawMessage(raw))
			}
			index := map[string]any{"schema": 1, "catalog": name, "apps": apps}
			for _, part := range []string{"featured", "revoked"} {
				raw, err := os.ReadFile(filepath.Join(folder, part+".json"))
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return err
				}
				if !json.Valid(raw) {
					return errors.New(part + ".json isn't JSON")
				}
				index[part] = json.RawMessage(raw)
			}
			encoded, err := json.MarshalIndent(index, "", "  ")
			if err != nil {
				return err
			}
			encoded = append(encoded, '\n')
			// A server reads an index up to a size; the entries' icons count.
			if len(encoded) > appCatalogIndexMaxBytes {
				return fmt.Errorf("the index is %d bytes, over the %d a server reads", len(encoded), appCatalogIndexMaxBytes)
			}
			signature := ed25519.Sign(private, append([]byte(appCatalogSignatureMessage), encoded...))
			public := base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey))
			sig := base64.StdEncoding.EncodeToString(signature)
			// Read back as a server reads a catalog it trusts: a bad entry stops here, not on every server.
			parsed, err := verifyAppCatalog(public, encoded, []byte(sig))
			if err != nil {
				return fmt.Errorf("the catalog wouldn't read: %w", err)
			}
			indexPath, sigPath := filepath.Join(folder, "catalog.json"), filepath.Join(folder, "catalog.json.sig")
			if err := os.WriteFile(indexPath, encoded, 0o644); err != nil {
				return err
			}
			if err := os.WriteFile(sigPath, []byte(sig+"\n"), 0o644); err != nil {
				return err
			}
			out := command.OutOrStdout()
			digest := sha256.Sum256(encoded)
			fmt.Fprintf(out, "wrote %s (%d apps, sha256 %s) and %s\n", indexPath, len(parsed.Apps), hex.EncodeToString(digest[:]), sigPath)
			fmt.Fprintf(out, "list key (a catalog's address is trusted with it):\n%s\n", public)
			fmt.Fprintf(out, "attach both to the release; a server reads <address of catalog.json> and that address with .sig\n")
			return nil
		},
	}
	command.Flags().StringVar(&keyPath, "key", "", "the catalog's list key, a file `leafen app key` made")
	command.Flags().StringVar(&name, "name", "", "the catalog's name in its index")
	_ = command.MarkFlagRequired("key")
	return command
}
