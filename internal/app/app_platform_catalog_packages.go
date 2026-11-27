//go:build !leafen_noapps

package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A catalog's packages and revocations (docs/app-platform.md 16, T5): a
// closed network's bundle brings the packages its signed index names; a
// package is fetched as exactly the file and version its index names; a
// version any trusted catalog revoked stops at once wherever installed.

// importAppCatalogBundle trusts a closed network's catalog from a bundle of
// its index, signature and the packages the index names.
func importAppCatalogBundle(app core.App, name, keyText, actorID string, raw []byte, now time.Time) error {
	if err := appCatalogNameRefusal(name); err != nil {
		return err
	}
	// A list key that can't be one is refused before the bundle is read, as
	// for a catalog at an address (fetchVerifiedAppCatalog).
	if _, err := parseAppCatalogKey(keyText); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return refuse(errAppCatalogInvalid, "bundle isn't a zip archive")
	}
	read := func(entry *zip.File, limit int64) ([]byte, error) {
		if entry.UncompressedSize64 > uint64(limit) {
			return nil, refuse(errAppCatalogInvalid, "the bundle's %s is over %d bytes", entry.Name, limit)
		}
		opened, err := entry.Open()
		if err != nil {
			return nil, refuse(errAppCatalogInvalid, "the bundle's %s can't be read", entry.Name)
		}
		defer opened.Close()
		data, err := io.ReadAll(io.LimitReader(opened, limit+1))
		if err != nil {
			return nil, refuse(errAppCatalogInvalid, "the bundle's %s can't be read", entry.Name)
		}
		if int64(len(data)) > limit {
			return nil, refuse(errAppCatalogInvalid, "the bundle's %s is over %d bytes", entry.Name, limit)
		}
		return data, nil
	}
	var index, signature []byte
	packages := map[string][]byte{}
	for _, entry := range archive.File {
		switch {
		case entry.Name == "catalog.json":
			index, err = read(entry, appCatalogIndexMaxBytes)
		case entry.Name == "catalog.json.sig":
			signature, err = read(entry, 4096)
		case strings.HasPrefix(entry.Name, "packages/") && strings.HasSuffix(entry.Name, ".leafenapp") && !strings.Contains(entry.Name[len("packages/"):], "/"):
			var data []byte
			if data, err = read(entry, appPackageMaxBytes); err == nil {
				digest := sha256.Sum256(data)
				packages[hex.EncodeToString(digest[:])] = data
			}
		}
		if err != nil {
			return err
		}
	}
	if index == nil {
		return refuse(errAppCatalogInvalid, "the bundle has no catalog.json")
	}
	if signature == nil {
		return refuse(errAppCatalogInvalid, "the bundle has no catalog.json.sig")
	}
	parsed, err := verifyAppCatalog(keyText, index, signature)
	if err != nil {
		return err
	}
	catalogID := security.RandomString(15)
	return app.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().Insert(appCatalogsTable, dbx.Params{
			"id": catalogID, "name": name, "kind": "partner", "source": "file", "public_key": strings.TrimSpace(keyText),
			"index_json": string(index), "fetched_at": now.UnixMilli(), "created_by": actorID, "created": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		digests := make([]string, 0, len(parsed.Apps))
		for _, entry := range parsed.Apps {
			digests = append(digests, entry.Package.SHA256)
		}
		for _, digest := range digests {
			if data, found := packages[digest]; found {
				if _, err := tx.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_app_catalog_packages}} ([[catalog_id]], [[sha256]], [[data]])
					VALUES ({:catalog}, {:digest}, {:data})`).Bind(dbx.Params{"catalog": catalogID, "digest": digest, "data": data}).Execute(); err != nil {
					return err
				}
			}
		}
		return applyAppCatalogRevocations(tx, now)
	})
}

// appCatalogRevocations is every revoked version the trusted catalogs name.
func appCatalogRevocations(app core.App) (map[string]appCatalogRevoke, error) {
	catalogs, err := loadAppCatalogs(app)
	if err != nil {
		return nil, err
	}
	revoked := map[string]appCatalogRevoke{}
	for _, catalog := range catalogs {
		if index, ok := readAppCatalogIndex(catalog); ok {
			for _, entry := range index.Revoked {
				if entry.Reason == "" {
					entry.Reason = catalog.Name
				}
				revoked[entry.PackageHash] = entry
			}
		}
	}
	return revoked, nil
}

// applyAppCatalogRevocations stops at once every install of a revoked
// version and says why.
func applyAppCatalogRevocations(app core.App, now time.Time) error {
	return applyAppPackageRevocations(app, now)
}

func applyAppPackageRevocations(app core.App, now time.Time) error {
	revoked, err := appCatalogRevocations(app)
	if err != nil || len(revoked) == 0 {
		return err
	}
	installs, err := loadAppInstalls(app, "[[status]] != 'revoked'", dbx.Params{})
	if err != nil {
		return err
	}
	for _, install := range installs {
		entry, found := revoked[install.PackageHash]
		if !found {
			continue
		}
		// The version is revoked with its line in the app's audit, or not at all.
		if err := app.RunInTransaction(func(tx core.App) error {
			if _, err := tx.DB().Update(appInstallsTable, dbx.Params{"status": "revoked", "revoked_reason": appOneLine(entry.Reason, 300),
				"updated_by": "", "updated_at": now.UnixMilli()}, dbx.HashExp{"id": install.ID}).Execute(); err != nil {
				return err
			}
			return recordAppAudit(tx, install, "revoked", "", map[string]any{"reason": entry.Reason, "packageHash": install.PackageHash})
		}); err != nil {
			return err
		}
		// Stopped at once: what the version has running ends now, as at an
		// emergency stop, not when it has done.
		for _, cleanup := range appEmergencyCleanups {
			cleanup(install.ID)
		}
		for _, cleanup := range appStopCleanups {
			cleanup(install.ID)
		}
		notice := "app-revoked:" + install.ID + ":" + install.PackageHash
		warnAppNotice(app, install, notice, notifyAppAdministrators(app, install, notice, map[string]string{"en": entry.Reason}))
	}
	return nil
}

// appPackageRevoked reports whether a trusted catalog revoked a version.
func appPackageRevoked(app core.App, packageHash string) (bool, error) {
	revoked, err := appCatalogRevocations(app)
	if err != nil {
		return false, err
	}
	_, found := revoked[packageHash]
	return found, nil
}

// fetchAppCatalogPackage brings a catalog app's package, exactly the bytes
// its index names, and keeps it as the catalog's for review.
func fetchAppCatalogPackage(app core.App, catalogID, appID, actorID string) (string, error) {
	if catalogID == appBuiltinCatalogID {
		return fetchAppBuiltinPackage(app, appID, actorID)
	}
	catalog, err := loadAppCatalog(app, catalogID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errAppCatalogMissing
	}
	if err != nil {
		return "", err
	}
	index, ok := readAppCatalogIndex(catalog)
	if !ok {
		return "", refuse(errAppCatalogInvalid, "the catalog's kept index can't be read: read the catalog again")
	}
	var entry *appCatalogApp
	for position := range index.Apps {
		if index.Apps[position].ID == appID {
			entry = &index.Apps[position]
		}
	}
	if entry == nil {
		return "", errAppCatalogAppMissing
	}
	if revoked, err := appPackageRevoked(app, entry.Package.PackageHash); err != nil {
		return "", err
	} else if revoked {
		return "", errAppRevoked
	}
	var pkg appPackage
	if catalog.Source == "file" {
		var raw []byte
		err = app.DB().NewQuery(`SELECT [[data]] FROM {{leafen_app_catalog_packages}} WHERE [[catalog_id]] = {:catalog} AND [[sha256]] = {:digest}`).
			Bind(dbx.Params{"catalog": catalog.ID, "digest": entry.Package.SHA256}).Row(&raw)
		if err != nil {
			return "", fmt.Errorf("%w: the catalog's bundle didn't bring this app's package", errAppCatalogUnreachable)
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != entry.Package.SHA256 {
			return "", refuse(errAppCatalogInvalid, "the package isn't the file the catalog names")
		}
		if pkg, err = readAppPackage(raw); err != nil {
			return "", err
		}
	} else {
		base, _ := url.Parse(catalog.URL)
		target, err := base.Parse(entry.Package.URL)
		if err != nil {
			return "", refuse(errAppCatalogInvalid, "the catalog's package.url for %q isn't an address", entry.ID)
		}
		// A package from an address goes to disk as it comes, up to the
		// server's limit, and is read from there.
		spool, err := os.MkdirTemp("", "leafen-app-package-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(spool)
		packagePath, files := filepath.Join(spool, "package.leafenapp"), filepath.Join(spool, "files")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		limit := appPackageLimit(app)
		digest, err := fetchAppCatalogFile(ctx, target.String(), limit, packagePath)
		if err != nil {
			return "", err
		}
		if digest != entry.Package.SHA256 {
			return "", refuse(errAppCatalogInvalid, "the package isn't the file the catalog names")
		}
		if err := os.Mkdir(files, 0o700); err != nil {
			return "", err
		}
		if pkg, err = readAppPackageFile(packagePath, files, limit); err != nil {
			return "", err
		}
	}
	if pkg.Hash != entry.Package.PackageHash || pkg.Manifest.ID != entry.ID {
		return "", refuse(errAppCatalogInvalid, "the package isn't the version the catalog names")
	}
	if err := storeAppPackage(app, pkg, appTrustCatalog, actorID); err != nil {
		return "", err
	}
	// A version first uploaded by hand is the catalog's once fetched from it.
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_app_packages}} SET [[trust]] = 'catalog', [[catalog_id]] = {:catalog}
		WHERE [[hash]] = {:hash} AND [[trust]] != 'builtin'`).Bind(dbx.Params{"catalog": catalog.ID, "hash": pkg.Hash}).Execute(); err != nil {
		return "", err
	}
	return pkg.Hash, nil
}
