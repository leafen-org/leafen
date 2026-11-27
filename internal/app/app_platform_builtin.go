//go:build !leafen_noapps

package app

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Built-in apps (docs/app-platform.md 16): the apps Leafen ships inside its
// executable, which vouches for their bytes (trust "builtin"). None is
// installed at first; an administrator finds them in "앱 둘러보기" as
// Leafen's own, reviews and installs one like any other, on a closed network
// too, and a newer executable's version shows as an update.

//go:embed builtinapps
var builtinAppFiles embed.FS

// documentPreviewAssets holds the PDF app's pdf.js the file preview draws
// PDFs with: here the built-in apps themselves, one copy in the executable.
var documentPreviewAssets fs.FS = builtinAppFiles

const appBuiltinCatalogID = "builtin"

type appBuiltinList struct {
	Schema int                `json:"schema"`
	Apps   []appBuiltinListed `json:"apps"`
}

type appBuiltinListed struct {
	Folder   string `json:"folder"`
	Featured bool   `json:"featured,omitzero"`
	Example  bool   `json:"example,omitzero"`
}

type appBuiltin struct {
	appBuiltinListed
	Package appPackage
}

var appBuiltins struct {
	sync.Mutex
	source fs.FS
	read   bool
	apps   []appBuiltin
}

func init() {
	source, err := fs.Sub(builtinAppFiles, "builtinapps")
	if err != nil {
		panic(err)
	}
	useAppBuiltinSource(source)
}

// useAppBuiltinSource reads built-in apps from source from now on.
func useAppBuiltinSource(source fs.FS) {
	appBuiltins.Lock()
	defer appBuiltins.Unlock()
	appBuiltins.source, appBuiltins.read, appBuiltins.apps = source, false, nil
}

// loadAppBuiltins is the executable's built-in apps, read once. A broken one
// is the build's mistake: it is logged and no built-in app is offered.
func loadAppBuiltins() []appBuiltin {
	appBuiltins.Lock()
	defer appBuiltins.Unlock()
	if !appBuiltins.read {
		apps, err := readAppBuiltins(appBuiltins.source)
		if err != nil {
			slog.Error("built-in apps unreadable", "error", err)
		}
		appBuiltins.read, appBuiltins.apps = true, apps
	}
	return appBuiltins.apps
}

// readAppBuiltins reads every folder builtin.json lists as a package.
func readAppBuiltins(source fs.FS) ([]appBuiltin, error) {
	raw, err := fs.ReadFile(source, "builtin.json")
	if err != nil {
		return nil, err
	}
	list := appBuiltinList{}
	if err := json.Unmarshal(raw, &list); err != nil || list.Schema != 1 {
		return nil, fmt.Errorf("builtin.json: %w", errAppPackageInvalid)
	}
	apps, seen := []appBuiltin{}, map[string]bool{}
	for _, listed := range list.Apps {
		if !fs.ValidPath(listed.Folder) || listed.Folder == "." || strings.Contains(listed.Folder, "/") {
			return nil, fmt.Errorf("built-in folder %q: %w", listed.Folder, errAppPackageInvalid)
		}
		packed, err := packAppFolder(source, listed.Folder)
		if err != nil {
			return nil, fmt.Errorf("built-in app %s: %w", listed.Folder, err)
		}
		pkg, err := readAppPackage(packed)
		if err != nil {
			return nil, fmt.Errorf("built-in app %s: %w", listed.Folder, err)
		}
		if seen[pkg.Manifest.ID] {
			return nil, fmt.Errorf("built-in app %s: %s twice: %w", listed.Folder, pkg.Manifest.ID, errAppPackageInvalid)
		}
		seen[pkg.Manifest.ID] = true
		apps = append(apps, appBuiltin{appBuiltinListed: listed, Package: pkg})
	}
	return apps, nil
}

// appPackageTops are what a package holds at its top.
var appPackageTops = map[string]bool{appManifestFile: true, appSignatureFile: true, "ui": true, "workers": true, "data": true, "templates": true, "site": true}

// packAppFolder zips a folder's package files as a .leafenapp holds them:
// the manifest, the signature and the ui/, workers/, data/, templates/ and
// site/ folders.
// Anything else there — sources, notes, hidden files — stays out.
//
// The built-in apps are packed this way only to be read back at once, so
// their files are stored rather than compressed: a package's hash is its
// files', not its archive's, and compressing 60 MB to unpack it again was
// most of the seconds the first look at them took after a start (2026-11-16:
// the first "앱 둘러보기" after a start waited 9 s on the NAS).
func packAppFolder(source fs.FS, folder string) ([]byte, error) {
	buffer := &bytes.Buffer{}
	if err := writeAppFolderPackageWith(buffer, source, folder, zip.Store); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// writeAppFolderPackage writes a folder's package to w a file at a time,
// never holding more than one file's piece in memory.
func writeAppFolderPackage(w io.Writer, source fs.FS, folder string) error {
	return writeAppFolderPackageWith(w, source, folder, zip.Deflate)
}

// writeAppFolderPackageWith writes it with its files compressed by method.
func writeAppFolderPackageWith(w io.Writer, source fs.FS, folder string, method uint16) error {
	archive := zip.NewWriter(w)
	err := fs.WalkDir(source, folder, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative := name
		if folder != "." {
			relative = strings.TrimPrefix(strings.TrimPrefix(name, folder), "/")
		}
		if relative == "" || relative == "." {
			return nil
		}
		top, _, _ := strings.Cut(relative, "/")
		if !appPackageTops[top] || strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		data, err := source.Open(name)
		if err != nil {
			return err
		}
		defer data.Close()
		file, err := archive.CreateHeader(&zip.FileHeader{Name: relative, Method: method})
		if err == nil {
			_, err = io.Copy(file, data)
		}
		return err
	})
	if err != nil {
		return err
	}
	return archive.Close()
}

// appBuiltinEntries shows the built-in apps in "앱 둘러보기".
func appBuiltinEntries(installed map[string]string, revoked map[string]appCatalogRevoke) []appCatalogEntryWire {
	entries := []appCatalogEntryWire{}
	for _, builtin := range loadAppBuiltins() {
		manifest := builtin.Package.Manifest
		_, isRevoked := revoked[builtin.Package.Hash]
		languages := []string{}
		for language := range manifest.Name {
			languages = append(languages, language)
		}
		sort.Strings(languages)
		tags := manifest.Tags
		if tags == nil {
			tags = []string{}
		}
		entries = append(entries, appCatalogEntryWire{IconURL: appBuiltinIcon(builtin), CatalogID: appBuiltinCatalogID, CatalogName: "Leafen", CatalogKind: "builtin", ID: manifest.ID,
			Slug: manifest.Slug, Name: manifest.Name, Summary: appTextOrEmpty(manifest.Description), Developer: manifest.Developer.Name,
			Category: appOneOfOr(manifest.Category, appCategories, "work"), Tags: tags, Version: manifest.Version, Reviewed: true,
			Languages: languages, Network: appManifestNetworkKind(manifest), Risk: appManifestRisk(manifest),
			Featured: builtin.Featured, Example: builtin.Example, InstalledVersion: installed[manifest.ID],
			InstalledChange: appInstalledChange(installed[manifest.ID], manifest.Version), Revoked: isRevoked})
	}
	return entries
}

// appBuiltinIcon is a built-in app's icon as a data address, as nothing serves
// the files of an app that isn't installed yet; a catalog's entry carries its
// own in its signed index (app_platform_catalog_entry.go).
func appBuiltinIcon(builtin appBuiltin) string {
	icon := builtin.Package.Manifest.Icon
	for _, file := range builtin.Package.Files {
		if icon != "" && file.Path == icon {
			return appIconDataURL(icon, file.Data, appInlineIconMaxBytes)
		}
	}
	return ""
}

// appManifestNetworkKind says how an app reaches outside: straight from its
// window, only through Leafen, or not at all.
func appManifestNetworkKind(manifest appManifest) string {
	switch network := manifest.Network; {
	case len(network.Direct) > 0:
		return "direct"
	case len(network.Relay) > 0 || len(network.Sockets) > 0 || len(network.OAuth) > 0:
		return "relay"
	}
	return "none"
}

// appManifestRisk is the highest risk of what an app asks for.
func appManifestRisk(manifest appManifest) string {
	risk := "low"
	for _, permission := range appPermissions(manifest) {
		if permission.Risk == "high" {
			return "high"
		}
		if permission.Risk == "medium" {
			risk = "medium"
		}
	}
	return risk
}

// fetchAppBuiltinPackage keeps a built-in app's package for review as the
// executable's own, however its bytes first came.
func fetchAppBuiltinPackage(app core.App, appID, actorID string) (string, error) {
	for _, builtin := range loadAppBuiltins() {
		if builtin.Package.Manifest.ID != appID {
			continue
		}
		if revoked, err := appPackageRevoked(app, builtin.Package.Hash); err != nil {
			return "", err
		} else if revoked {
			return "", errAppRevoked
		}
		if err := storeAppPackage(app, builtin.Package, appTrustBuiltin, actorID); err != nil {
			return "", err
		}
		if _, err := app.DB().Update(appPackagesTable, dbx.Params{"trust": appTrustBuiltin, "catalog_id": ""},
			dbx.HashExp{"hash": builtin.Package.Hash}).Execute(); err != nil {
			return "", err
		}
		return builtin.Package.Hash, nil
	}
	return "", errAppCatalogAppMissing
}
