//go:build !leafen_noapps

package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Some endpoint pages aren't in their app's package: a published build kept
// apart from Leafen's binary, under its own license. The Vault
// app's web vault is Bitwarden's, under the GPL; the server fetches its
// pinned release the first time a page of it is asked for, checks it by its
// SHA-256, and keeps it in the data folder, each file compressed as the
// endpoint sends it and without its source maps — as the release is, never
// changed. Until it is there the page says so and comes back by itself. A
// server without the internet takes the release's archive put in that folder
// by hand, as <data>/app-page-packs/<sha256>.tar.gz.

type appPagePack struct {
	AppID    string
	Endpoint string
	Version  string
	URL      string
	SHA256   string
	// The archive's folder that holds the pages.
	Root string
	// The license's text, served beside the pages as leafen/COPYING.txt (the
	// package keeps leafen/SOURCE.md, which says where they come from).
	License []byte
}

const (
	appPagePacksFolder   = "app-page-packs"
	appPagePackMaxBytes  = 128 << 20
	appPagePackRetry     = time.Minute
	appPagePackComplete  = ".complete"
	appPagePackFetchTime = 10 * time.Minute
)

//go:embed apppagepacks/gpl-3.0.txt
var appPagePackGPLText []byte

var appPagePacks = []appPagePack{{
	AppID: "app.leafen.vault", Endpoint: "bw", Version: "2026.7.0",
	URL:     "https://github.com/dani-garcia/bw_web_builds/releases/download/v2026.7.0/bw_web_v2026.7.0.tar.gz",
	SHA256:  "002e972bf0d0487ec0324b06d916de33e29de4c29bffd92ee3b843084c300570",
	Root:    "web-vault/",
	License: appPagePackGPLText,
}}

func appPagePackFor(appID, endpoint string) (appPagePack, bool) {
	for _, pack := range appPagePacks {
		if pack.AppID == appID && pack.Endpoint == endpoint {
			return pack, true
		}
	}
	return appPagePack{}, false
}

// appPagePackState is where each pack's fetch is: going, or failed when.
var appPagePackState = struct {
	sync.Mutex
	fetching map[string]bool
	failed   map[string]time.Time
}{fetching: map[string]bool{}, failed: map[string]time.Time{}}

func appPagePackFolder(app core.App, pack appPagePack) string {
	return filepath.Join(app.DataDir(), appPagePacksFolder, pack.SHA256)
}

// appEndpointServePackPage sends a page from its pack, starting the pack's
// fetch when it isn't kept yet.
func appEndpointServePackPage(event *core.RequestEvent, pack appPagePack, name string, status int) error {
	folder := appPagePackFolder(event.App, pack)
	if _, err := os.Stat(filepath.Join(folder, appPagePackComplete)); err != nil {
		startAppPagePackFetch(event.App, pack)
		event.Response.Header().Set("Retry-After", "5")
		event.Response.Header().Set("Refresh", "5")
		return appEndpointPlain(event, http.StatusServiceUnavailable,
			"Leafen is getting these pages ready the first time they're opened. This page reloads in a moment.")
	}
	data, err := os.ReadFile(filepath.Join(folder, filepath.FromSlash(name)+".gz"))
	if errors.Is(err, os.ErrNotExist) {
		return appEndpointPlain(event, http.StatusNotFound, "Not found.")
	}
	if err != nil {
		return appEndpointFailure(event, "Leafen couldn't read the app.", err)
	}
	return appEndpointWritePage(event, name, data, true, status)
}

func startAppPagePackFetch(app core.App, pack appPagePack) {
	appPagePackState.Lock()
	defer appPagePackState.Unlock()
	if appPagePackState.fetching[pack.SHA256] || time.Since(appPagePackState.failed[pack.SHA256]) < appPagePackRetry {
		return
	}
	appPagePackState.fetching[pack.SHA256] = true
	// The fetch runs under the app platform's owner, which the server's
	// shutdown cancels and waits for (app_platform_background.go).
	started := appPlatformBackgroundOf(app).run(func(background context.Context) {
		ctx, cancel := context.WithTimeout(background, appPagePackFetchTime)
		defer cancel()
		err := fetchAppPagePack(ctx, app, pack)
		appPagePackState.Lock()
		defer appPagePackState.Unlock()
		delete(appPagePackState.fetching, pack.SHA256)
		if err != nil {
			appPagePackState.failed[pack.SHA256] = time.Now()
			app.Logger().Warn("app pages couldn't be fetched", "app", pack.AppID, "version", pack.Version, "error", err.Error())
			return
		}
		delete(appPagePackState.failed, pack.SHA256)
	})
	if !started {
		delete(appPagePackState.fetching, pack.SHA256)
	}
}

// fetchAppPagePack fetches a pack's release (or takes it from the data
// folder), checks it, and keeps its pages.
func fetchAppPagePack(ctx context.Context, app core.App, pack appPagePack) error {
	packs := filepath.Join(app.DataDir(), appPagePacksFolder)
	if err := os.MkdirAll(packs, 0o700); err != nil {
		return err
	}
	archive := filepath.Join(packs, pack.SHA256+".tar.gz")
	if _, err := os.Stat(archive); err != nil {
		download := archive + ".part"
		_ = os.Remove(download)
		digest, err := fetchAppCatalogFile(ctx, pack.URL, appPagePackMaxBytes, download)
		if err != nil {
			_ = os.Remove(download)
			return err
		}
		if digest != pack.SHA256 {
			_ = os.Remove(download)
			return errors.New("the release's SHA-256 isn't the pinned one")
		}
		if err := os.Rename(download, archive); err != nil {
			return err
		}
	} else if digest, err := appPagePackFileDigest(archive); err != nil || digest != pack.SHA256 {
		if err == nil {
			err = errors.New("the archive in the data folder isn't the pinned release")
		}
		return err
	}
	work, err := os.MkdirTemp(packs, pack.SHA256+".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := unpackAppPagePack(archive, pack.Root, work); err != nil {
		return err
	}
	if err := writeAppPagePackFile(work, "leafen/COPYING.txt", bytes.NewReader(pack.License)); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, appPagePackComplete), []byte(pack.Version+"\n"), 0o600); err != nil {
		return err
	}
	final := filepath.Join(packs, pack.SHA256)
	if err := os.RemoveAll(final); err != nil {
		return err
	}
	return os.Rename(work, final)
}

func appPagePackFileDigest(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, appPagePackMaxBytes+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// unpackAppPagePack keeps each file under the archive's root, compressed,
// leaving out source maps.
func unpackAppPagePack(archive, root, target string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	unzipped, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer unzipped.Close()
	reader := tar.NewReader(unzipped)
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(path.Clean("/" + header.Name)[1:], "./")
		if !strings.HasPrefix(name, root) || strings.HasSuffix(name, ".map") {
			continue
		}
		relative := strings.TrimPrefix(name, root)
		if relative == "" || strings.HasPrefix(relative, "leafen/") {
			continue
		}
		if total += header.Size; total > 4*appPagePackMaxBytes {
			return errors.New("the release unpacks larger than a page pack may")
		}
		if err := writeAppPagePackFile(target, relative, io.LimitReader(reader, header.Size)); err != nil {
			return err
		}
	}
}

func writeAppPagePackFile(target, name string, content io.Reader) error {
	full := filepath.Join(target, filepath.FromSlash(name)+".gz")
	if !strings.HasPrefix(full, filepath.Clean(target)+string(filepath.Separator)) {
		return errors.New("a page's name leaves its pack")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	compressor, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err == nil {
		_, err = io.Copy(compressor, content)
		if closeErr := compressor.Close(); err == nil {
			err = closeErr
		}
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}
