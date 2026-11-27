//go:build !leafen_noapps

package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// `leafen app` (docs/app-platform.md 18): make an app's folder, check it as
// Leafen would, pack it, sign it, try it on a throwaway local Leafen, and
// write its entry in a catalog and sign that catalog's index. A Go worker
// whose source is the folder's worker/ builds to workers/main.wasm first.

// RunAppCommand runs `leafen app …` without opening a data folder: the app
// commands work on an app's files, and dev starts its own server.
func RunAppCommand(args []string) int {
	root := &cobra.Command{Use: "leafen", SilenceUsage: true}
	root.AddCommand(appCommand())
	root.SetArgs(append([]string{"app"}, args...))
	if err := root.Execute(); err != nil {
		return 1
	}
	return 0
}

func appCommand() *cobra.Command {
	command := &cobra.Command{Use: "app", Short: "Make, check, sign and try Leafen apps"}
	command.AddCommand(appInitCommand(), appValidateCommand(), appBuildCommand(), appKeyCommand(), appSignCommand(), appDevCommand(),
		appPublishCommand(), appCatalogCommand())
	return command
}

func appFolderArgument(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}

// readAppFolder builds a Go worker the folder holds and packs the folder
// into a new file in dir, with the problems Leafen would find; the caller
// moves or removes the file.
func readAppFolder(out io.Writer, folder, dir string) (appPackage, string, error) {
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return appPackage{}, "", fmt.Errorf("%s isn't a folder", folder)
	}
	if err := buildAppFolderWorker(out, folder); err != nil {
		return appPackage{}, "", err
	}
	packed, err := packAppFolderFile(folder, dir)
	if err != nil {
		return appPackage{}, "", err
	}
	pkg, err := readAppPackagePath(packed)
	if err != nil {
		_ = os.Remove(packed)
		return appPackage{}, "", err
	}
	for _, notice := range appFolderScreenNotices(folder, pkg.Manifest) {
		fmt.Fprintln(out, "warning: "+notice)
	}
	return pkg, packed, nil
}

// buildAppFolderWorker builds worker/ (a Go module) to workers/main.wasm
// when the folder has one.
func buildAppFolderWorker(out io.Writer, folder string) error {
	source := filepath.Join(folder, "worker")
	if _, err := os.Stat(filepath.Join(source, "go.mod")); err != nil {
		return nil
	}
	target, err := filepath.Abs(filepath.Join(folder, "workers", "main.wasm"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	build := exec.Command("go", "build", "-buildmode=c-shared", "-o", target, ".")
	build.Dir, build.Stdout, build.Stderr = source, out, out
	build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if err := build.Run(); err != nil {
		return fmt.Errorf("building the Go worker in %s: %w (it needs Go 1.24 or later)", source, err)
	}
	fmt.Fprintf(out, "built %s → workers/main.wasm\n", source)
	return nil
}

func describeAppPackage(out io.Writer, pkg appPackage) {
	signed := "unsigned"
	if pkg.Signed {
		signed = "signed by key " + pkg.KeyFingerprint[:16] + "…"
	}
	fmt.Fprintf(out, "%s %s (/apps/%s): %d files, %d bytes, %s\nfingerprint %s\n", pkg.Manifest.ID, pkg.Manifest.Version, pkg.Manifest.Slug,
		len(pkg.Files), pkg.Size, signed, pkg.Hash)
	if needs := appNeedsLeafen(pkg.Manifest); needs != "" {
		fmt.Fprintf(out, "note: it needs Leafen %s or later; this leafen is %s\n", needs, leafenReleaseVersion)
	}
	for _, notice := range appDeprecationNotices(pkg) {
		fmt.Fprintln(out, "warning: "+notice)
	}
	// What a person does on the screen their agent does too: every action is
	// an agent tool unless it says "tool": false (docs/apps.md "Agents").
	for _, action := range pkg.Manifest.Actions {
		if !action.offeredToAgents() {
			fmt.Fprintf(out, "note: action %s is kept from agents (\"tool\": false); a person's agent does what they do on the screen unless the app can't let it\n", action.Key)
		}
	}
}

// appDeprecation is something apps may still use that Leafen will remove or
// change (docs/apps.md "Versions and compatibility"): it keeps working until
// the next minor release after the one that announced it and for at least
// 90 days, and validate and build say so meanwhile. A release adds one here
// with its release notes; a security fix may close something at once.
type appDeprecation struct {
	announced string // the Leafen release that announced it
	until     string // the earliest day it may go, YYYY-MM-DD
	notice    string
	uses      func(appPackage) bool
}

// appDeprecations lists what is going.
var appDeprecations = []appDeprecation{
	// An app's frame has no origin, so no service worker reaches it or its files (checked in Chrome, 2026-10-07).
	{announced: "0.1.0", until: "2027-01-05", notice: "pwa.offline does nothing — an app's frame has no origin, so no service worker can keep its files " +
		"offline; keep what a person needs in storage or the app's databases",
		uses: func(pkg appPackage) bool { return pkg.Manifest.PWA != nil && len(pkg.Manifest.PWA.Offline) > 0 }},
}

// appDeprecationNotices says what of the package Leafen will remove or change.
func appDeprecationNotices(pkg appPackage) []string {
	notices := []string{}
	for _, deprecation := range appDeprecations {
		if deprecation.uses(pkg) {
			notices = append(notices, fmt.Sprintf("%s — announced in Leafen %s; Leafen still takes it until the next minor release and at least %s",
				deprecation.notice, deprecation.announced, deprecation.until))
		}
	}
	return notices
}

func appValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use: "validate [folder]", Short: "Check an app's folder as Leafen checks a package", Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			pkg, packed, err := readAppFolder(command.ErrOrStderr(), appFolderArgument(args), os.TempDir())
			if err != nil {
				return err
			}
			_ = os.Remove(packed)
			describeAppPackage(command.OutOrStdout(), pkg)
			fmt.Fprintln(command.OutOrStdout(), "ok")
			return nil
		},
	}
}

func appBuildCommand() *cobra.Command {
	output := ""
	command := &cobra.Command{
		Use: "build [folder]", Short: "Pack an app's folder into a .leafenapp file", Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			pkg, packed, err := readAppFolder(command.ErrOrStderr(), appFolderArgument(args), filepath.Dir(output))
			if err != nil {
				return err
			}
			if output == "" {
				output = pkg.Manifest.Slug + "-" + pkg.Manifest.Version + ".leafenapp"
			}
			if err := putAppPackageFile(packed, output); err != nil {
				_ = os.Remove(packed)
				return err
			}
			describeAppPackage(command.OutOrStdout(), pkg)
			fmt.Fprintf(command.OutOrStdout(), "wrote %s\n", output)
			return nil
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "", "the file to write (default <slug>-<version>.leafenapp)")
	return command
}

type appKeyFile struct {
	Algorithm  string `json:"algorithm"`
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
}

func appKeyCommand() *cobra.Command {
	return &cobra.Command{
		Use: "key <file>", Short: "Make a signing key and print the fingerprint the manifest names", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			public, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return err
			}
			encoded, _ := json.MarshalIndent(appKeyFile{Algorithm: "ed25519", PublicKey: base64.StdEncoding.EncodeToString(public),
				PrivateKey: base64.StdEncoding.EncodeToString(private.Seed())}, "", "  ")
			file, err := os.OpenFile(args[0], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return fmt.Errorf("%w (a key file is never overwritten)", err)
			}
			if _, err := file.Write(append(encoded, '\n')); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
			digest := sha256.Sum256(public)
			fmt.Fprintf(command.OutOrStdout(), "wrote %s — keep it secret\nput this in the manifest's developer.keyFingerprint:\n%s\n", args[0],
				hex.EncodeToString(digest[:]))
			return nil
		},
	}
}

func readAppKeyFile(path string) (ed25519.PrivateKey, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	key := appKeyFile{}
	if json.Unmarshal(raw, &key) != nil || key.Algorithm != "ed25519" {
		return nil, "", errors.New(path + " isn't a key `leafen app key` made")
	}
	seed, err := base64.StdEncoding.DecodeString(key.PrivateKey)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, "", errors.New(path + " has a malformed private key")
	}
	private := ed25519.NewKeyFromSeed(seed)
	digest := sha256.Sum256(private.Public().(ed25519.PublicKey))
	return private, hex.EncodeToString(digest[:]), nil
}

func appSignCommand() *cobra.Command {
	keyPath, output := "", ""
	command := &cobra.Command{
		Use: "sign <package.leafenapp>", Short: "Sign a package with your key", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			private, fingerprint, err := readAppKeyFile(keyPath)
			if err != nil {
				return err
			}
			if output == "" {
				output = args[0]
			}
			pkg, err := signAppPackageFile(args[0], output, private, fingerprint)
			if err != nil {
				return err
			}
			describeAppPackage(command.OutOrStdout(), pkg)
			fmt.Fprintf(command.OutOrStdout(), "wrote %s\n", output)
			return nil
		},
	}
	command.Flags().StringVar(&keyPath, "key", "", "the key file `leafen app key` made")
	command.Flags().StringVarP(&output, "output", "o", "", "the file to write (default: the package itself)")
	_ = command.MarkFlagRequired("key")
	return command
}

// appPublishCommand writes a signed package's entry into a catalog's folder
// and copies the package beside it as its release file; `leafen app catalog`
// then signs the folder's index. A maker publishes a catalog of their own,
// which administrators add by its address and list key: Leafen makes and
// signs its official catalog itself and takes no pull requests to it.
func appPublishCommand() *cobra.Command {
	output, packageURL := "", ""
	command := &cobra.Command{
		Use: "publish <package.leafenapp>", Short: "Write a signed package's entry and release file into a catalog's folder", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if address, err := url.Parse(packageURL); packageURL != "" && (err != nil || address.Scheme != "https" || address.Host == "") {
				return errors.New("--package-url takes the https address the release will serve the package at")
			}
			pkg, err := readAppPackagePath(args[0])
			if err != nil {
				return err
			}
			if !pkg.Signed {
				return errors.New("a catalog takes signed packages: run `leafen app sign` first")
			}
			manifest := pkg.Manifest
			fileDigest, size, err := appPackageFileDigest(args[0])
			if err != nil {
				return err
			}
			release := manifest.Slug + "-" + manifest.Version + ".leafenapp"
			languages := []string{}
			for language := range manifest.Name {
				languages = append(languages, language)
			}
			sort.Strings(languages)
			tags := manifest.Tags
			if tags == nil {
				tags = []string{}
			}
			entry := map[string]any{
				"id": manifest.ID, "slug": manifest.Slug, "version": manifest.Version, "name": manifest.Name, "summary": appTextOrEmpty(manifest.Description),
				// The catalog's index names the developer by name (catalog.json, docs/apps.md); the key stays in the package.
				"developer": manifest.Developer.Name, "category": appOneOfOr(manifest.Category, appCategories, "work"), "tags": tags,
				"languages": languages, "network": appManifestNetworkKind(manifest), "risk": appManifestRisk(manifest), "screenshots": []string{},
				"package": map[string]any{"url": packageURL, "file": release, "sha256": fileDigest, "packageHash": pkg.Hash,
					"size": size},
			}
			// The entry carries the package's icon inside the signed index, so the
			// lists show it before the app is installed.
			icon, iconLeftOut := appPublishIcon(args[0], manifest.Icon)
			for name, value := range map[string]string{"minLeafen": manifest.MinLeafen, "license": manifest.License, "icon": icon} {
				if value != "" {
					entry[name] = value
				}
			}
			encoded, _ := json.MarshalIndent(entry, "", "  ")
			entryPath := filepath.Join(output, "apps", manifest.ID, "app.json")
			releasePath := filepath.Join(output, "releases", release)
			for _, path := range []string{entryPath, releasePath} {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return err
				}
			}
			if err := os.WriteFile(entryPath, append(encoded, '\n'), 0o644); err != nil {
				return err
			}
			if err := copyAppPackageFile(args[0], releasePath); err != nil {
				return err
			}
			out := command.OutOrStdout()
			describeAppPackage(out, pkg)
			fmt.Fprintf(out, "wrote %s and %s\n", entryPath, releasePath)
			if iconLeftOut != "" {
				fmt.Fprintf(out, "the entry carries no icon: %s; lists show the app's first letter\n", iconLeftOut)
			}
			if packageURL == "" {
				fmt.Fprintln(out, "serve the package at an https address and put it in package.url, then sign the catalog: leafen app catalog "+output+" --key <your list key>")
			} else {
				fmt.Fprintln(out, "serve the package at that address, then sign the catalog: leafen app catalog "+output+" --key <your list key>")
			}
			fmt.Fprintln(out, "administrators add your catalog by the address of its catalog.json and its list key (Leafen signs its official catalog itself)")
			return nil
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "catalog", "the folder to write into")
	command.Flags().StringVar(&packageURL, "package-url", "", "where the release will serve the package")
	return command
}

// appDevVersion marks the n-th upload of a version while trying an app.
func appDevVersion(version string, n int) string {
	base, _, _ := strings.Cut(version, "-")
	return fmt.Sprintf("%s-dev.%d", base, n)
}
