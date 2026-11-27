//go:build !leafen_noapps

package app

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"

	"leafen.org/leafen/internal/netproxy"
)

// App catalogs (docs/app-platform.md 16, T5): an administrator trusts a
// catalog by its address and list key, or, on a closed network, by a bundle
// file of its index, signature and packages. An index is read only when its
// list key signed it; it names each app's package by address, file digest
// and fingerprint, so a fetched package is installed only as exactly those
// bytes, and it lists the versions its keeper revoked: an install of one
// stops at once and its administrators see why.

const appCatalogSignatureMessage = "leafen-catalog-v1\n"

// The official catalog's index address and list key come with the release
// keys (release_keys_production.go): a development build names none.

// appOfficialCatalogID is the official catalog's row on every server.
const appOfficialCatalogID = "official"

var (
	errAppCatalogInvalid     = errors.New(errorCodeAppCatalogInvalid)
	errAppCatalogUnreachable = errors.New(errorCodeAppCatalogUnreachable)
	errAppRevoked            = errors.New(errorCodeAppRevoked)
	appDigestPattern         = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// An address a catalog or its package isn't read from: no request goes to it.
	errAppCatalogAddressRefused = refuse(errors.New(errorCodeAppCatalogAddressRefused), "the address is https, or http only on this machine")
)

// appCatalogNameRefusal is why a catalog's name isn't one, or nil.
func appCatalogNameRefusal(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return refuse(errAppInputInvalid, "name is empty")
	}
	if len([]rune(name)) > 80 {
		return refuse(errAppInputInvalid, "name is at most 80 characters")
	}
	return nil
}

type appCatalogIndex struct {
	Schema   int                `json:"schema"`
	Catalog  string             `json:"catalog"`
	Featured []string           `json:"featured,omitempty"`
	Apps     []appCatalogApp    `json:"apps"`
	Revoked  []appCatalogRevoke `json:"revoked,omitempty"`
}

type appCatalogRevoke struct {
	ID          string `json:"id"`
	PackageHash string `json:"packageHash"`
	Reason      string `json:"reason"`
}

type storedAppCatalog struct {
	ID        string `db:"id"`
	Name      string `db:"name"`
	Kind      string `db:"kind"`
	Source    string `db:"source"`
	URL       string `db:"url"`
	PublicKey string `db:"public_key"`
	IndexJSON string `db:"index_json"`
	FetchedAt int64  `db:"fetched_at"`
	LastError string `db:"last_error"`
}

func parseAppCatalogKey(text string) (ed25519.PublicKey, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, refuse(errAppCatalogInvalid, "publicKey is the list key: an ed25519 public key in base64")
	}
	return ed25519.PublicKey(key), nil
}

func appCatalogKeyFingerprint(text string) string {
	key, err := parseAppCatalogKey(text)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(key)
	return hex.EncodeToString(digest[:8])
}

// verifyAppCatalog reads an index its list key signed.
func verifyAppCatalog(keyText string, index, signature []byte) (appCatalogIndex, error) {
	key, err := parseAppCatalogKey(keyText)
	if err != nil {
		return appCatalogIndex{}, err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil || !ed25519.Verify(key, append([]byte(appCatalogSignatureMessage), index...), sig) {
		return appCatalogIndex{}, refuse(errAppCatalogInvalid, "the signature isn't the list key's for this index")
	}
	parsed := appCatalogIndex{}
	if err := json.Unmarshal(index, &parsed); err != nil {
		return appCatalogIndex{}, refuse(errAppCatalogInvalid, "the index isn't a catalog in JSON")
	}
	switch {
	case parsed.Schema != 1:
		return appCatalogIndex{}, refuse(errAppCatalogInvalid, "the index's schema is 1, not %d", parsed.Schema)
	case len(parsed.Apps) > 5000:
		return appCatalogIndex{}, refuse(errAppCatalogInvalid, "the index lists %d apps, over 5000", len(parsed.Apps))
	case len(parsed.Revoked) > 5000:
		return appCatalogIndex{}, refuse(errAppCatalogInvalid, "the index revokes %d versions, over 5000", len(parsed.Revoked))
	}
	for position, entry := range parsed.Apps {
		if problem := appCatalogEntryProblem(entry); problem != "" {
			return appCatalogIndex{}, refuse(errAppCatalogInvalid, "apps[%d] (%q) %s", position, entry.ID, problem)
		}
	}
	for position, revoked := range parsed.Revoked {
		if !appDigestPattern.MatchString(revoked.PackageHash) {
			return appCatalogIndex{}, refuse(errAppCatalogInvalid, "revoked[%d] (%q) packageHash isn't a SHA-256 digest", position, revoked.ID)
		}
	}
	return parsed, nil
}

// appCatalogURLAllowed takes https addresses, and plain http only on this
// machine's own loopback for a developer's local catalog.
func appCatalogURLAllowed(address *url.URL) bool {
	if address == nil || address.User != nil || address.Host == "" {
		return false
	}
	if address.Scheme == "https" {
		return true
	}
	if address.Scheme != "http" {
		return false
	}
	host := address.Hostname()
	ip := net.ParseIP(host)
	return host == "localhost" || ip != nil && ip.IsLoopback()
}

var appCatalogClient = &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
	if len(via) >= 3 || !appCatalogURLAllowed(request.URL) {
		return errAppCatalogUnreachable
	}
	return nil
}}

// fetchAppCatalogBytes reads an address up to a size.
func fetchAppCatalogBytes(ctx context.Context, address string, limit int64) ([]byte, error) {
	response, err := openAppCatalogURL(ctx, address)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: too large or cut short", errAppCatalogUnreachable)
	}
	return data, nil
}

func openAppCatalogURL(ctx context.Context, address string) (*http.Response, error) {
	parsed, err := url.Parse(address)
	if err != nil || !appCatalogURLAllowed(parsed) {
		return nil, errAppCatalogAddressRefused
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, errAppCatalogUnreachable
	}
	response, err := appCatalogClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errAppCatalogUnreachable, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("%w: %s answered %d", errAppCatalogUnreachable, parsed.Host, response.StatusCode)
	}
	return response, nil
}

// A package download may take long: it has no limit on the whole request,
// only on silence — a minute without a byte ends an attempt — and a cut
// attempt goes on from where it stopped when the address takes ranges, as
// release hosts do, else starts over; three tries in all.
const (
	appCatalogDownloadStall    = time.Minute
	appCatalogDownloadAttempts = 3
)

var appCatalogDownloadClient = &http.Client{
	Transport:     &http.Transport{Proxy: netproxy.ForRequest, ResponseHeaderTimeout: time.Minute, TLSHandshakeTimeout: 20 * time.Second},
	CheckRedirect: appCatalogClient.CheckRedirect,
}

// fetchAppCatalogFile writes what an address answers to a new file, at most
// limit bytes, and returns its SHA-256.
func fetchAppCatalogFile(ctx context.Context, address string, limit int64, target string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil || !appCatalogURLAllowed(parsed) {
		return "", errAppCatalogAddressRefused
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return "", err
	}
	defer out.Close()
	var written int64
	var last error
	for attempt := 0; attempt < appCatalogDownloadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("%w: %v", errAppCatalogUnreachable, err)
		}
		done, more, err := appCatalogDownloadAttempt(ctx, parsed.String(), out, written, limit)
		written, last = more, err
		if done {
			break
		}
		if errors.Is(err, errAppCatalogTooLarge) {
			return "", fmt.Errorf("%w: too large", errAppCatalogUnreachable)
		}
	}
	if last != nil {
		return "", fmt.Errorf("%w: cut short: %v", errAppCatalogUnreachable, last)
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, out); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

var errAppCatalogTooLarge = errors.New("larger than the limit")

// appCatalogDownloadAttempt asks for what is still missing after written
// bytes and appends it, answering whether the file is whole and how much of
// it there is.
func appCatalogDownloadAttempt(parent context.Context, address string, out *os.File, written, limit int64) (bool, int64, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return false, written, err
	}
	if written > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", written))
	}
	response, err := appCatalogDownloadClient.Do(request)
	if err != nil {
		return false, written, err
	}
	defer response.Body.Close()
	switch {
	case written > 0 && response.StatusCode == http.StatusPartialContent &&
		strings.HasPrefix(response.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", written)):
	case response.StatusCode == http.StatusOK:
		// Not a range: start over.
		if err := out.Truncate(0); err != nil {
			return false, 0, err
		}
		if _, err := out.Seek(0, io.SeekStart); err != nil {
			return false, 0, err
		}
		written = 0
	default:
		return false, written, fmt.Errorf("answered %d", response.StatusCode)
	}
	if _, err := out.Seek(written, io.SeekStart); err != nil {
		return false, written, err
	}
	stall := time.AfterFunc(appCatalogDownloadStall, cancel)
	defer stall.Stop()
	buffer := make([]byte, 256<<10)
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			stall.Reset(appCatalogDownloadStall)
			if written+int64(n) > limit {
				return false, written, errAppCatalogTooLarge
			}
			if _, err := out.Write(buffer[:n]); err != nil {
				return false, written, err
			}
			written += int64(n)
		}
		if readErr == io.EOF {
			if response.ContentLength >= 0 && response.StatusCode == http.StatusOK && written != response.ContentLength {
				return false, written, io.ErrUnexpectedEOF
			}
			return true, written, nil
		}
		if readErr != nil {
			return false, written, readErr
		}
	}
}

func loadAppCatalogs(app core.App) ([]storedAppCatalog, error) {
	catalogs := []storedAppCatalog{}
	err := app.DB().NewQuery(`SELECT [[id]], [[name]], [[kind]], [[source]], [[url]], [[public_key]], [[index_json]], [[fetched_at]], [[last_error]]
		FROM {{leafen_app_catalogs}} ORDER BY [[kind]] DESC, [[name]], [[id]]`).All(&catalogs)
	return catalogs, err
}

func loadAppCatalog(app core.App, id string) (storedAppCatalog, error) {
	catalog := storedAppCatalog{}
	err := app.DB().NewQuery(`SELECT [[id]], [[name]], [[kind]], [[source]], [[url]], [[public_key]], [[index_json]], [[fetched_at]], [[last_error]]
		FROM {{leafen_app_catalogs}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": id}).One(&catalog)
	return catalog, err
}

// readAppCatalogIndex reads a catalog's index as fetched and verified.
func readAppCatalogIndex(catalog storedAppCatalog) (appCatalogIndex, bool) {
	index := appCatalogIndex{}
	if catalog.IndexJSON == "" || json.Unmarshal([]byte(catalog.IndexJSON), &index) != nil {
		return index, false
	}
	return index, true
}

// refreshAppCatalog reads an address catalog again; a failure keeps the
// index last read and says why.
func refreshAppCatalog(app core.App, catalog storedAppCatalog, now time.Time) error {
	if catalog.Source != "url" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	index, _, err := fetchVerifiedAppCatalog(ctx, catalog.URL, catalog.PublicKey)
	if err != nil {
		_, _ = app.DB().Update(appCatalogsTable, dbx.Params{"last_error": appOneLine(err.Error(), 300), "fetched_at": now.UnixMilli()},
			dbx.HashExp{"id": catalog.ID}).Execute()
		return err
	}
	if _, err := app.DB().Update(appCatalogsTable, dbx.Params{"index_json": string(index), "fetched_at": now.UnixMilli(), "last_error": ""},
		dbx.HashExp{"id": catalog.ID}).Execute(); err != nil {
		return err
	}
	return applyAppCatalogRevocations(app, now)
}

// ensureOfficialAppCatalog trusts the official catalog on a server whose
// build names one, as it lists the built-in apps: Leafen's apps that aren't
// built in, as the Office app, come from it, and an administrator chooses
// which to install. A build that names another address or key reads it anew.
func ensureOfficialAppCatalog(app core.App, now time.Time) error {
	if appOfficialCatalogURL == "" || appOfficialCatalogKey == "" {
		return nil
	}
	catalog, err := loadAppCatalog(app, appOfficialCatalogID)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = app.DB().Insert(appCatalogsTable, dbx.Params{
			"id": appOfficialCatalogID, "name": "Leafen", "kind": "official", "source": "url", "url": appOfficialCatalogURL,
			"public_key": appOfficialCatalogKey, "index_json": "", "fetched_at": 0, "created_by": "", "created": now.UnixMilli(),
		}).Execute()
		return err
	}
	if err != nil || (catalog.URL == appOfficialCatalogURL && catalog.PublicKey == appOfficialCatalogKey) {
		return err
	}
	_, err = app.DB().Update(appCatalogsTable, dbx.Params{"url": appOfficialCatalogURL, "public_key": appOfficialCatalogKey, "fetched_at": 0},
		dbx.HashExp{"id": appOfficialCatalogID}).Execute()
	return err
}

// refreshDueAppCatalogs reads again the catalogs not read for a while, the
// official one first put in place.
func refreshDueAppCatalogs(app core.App, now time.Time) error {
	if err := ensureOfficialAppCatalog(app, now); err != nil {
		return err
	}
	catalogs, err := loadAppCatalogs(app)
	if err != nil {
		return err
	}
	for _, catalog := range catalogs {
		if catalog.Source == "url" && now.Sub(time.UnixMilli(catalog.FetchedAt)) >= appCatalogRefreshHours*time.Hour {
			_ = refreshAppCatalog(app, catalog, now)
		}
	}
	return applyAppCatalogRevocations(app, now)
}

// addAppCatalog trusts an address catalog once its first index reads.
func addAppCatalog(app core.App, input appCatalogInput, actorID string, now time.Time) error {
	if err := appCatalogNameRefusal(input.Name); err != nil {
		return err
	}
	name := strings.TrimSpace(input.Name)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	index, _, err := fetchVerifiedAppCatalog(ctx, strings.TrimSpace(input.URL), input.PublicKey)
	if err != nil {
		return err
	}
	if _, err := app.DB().Insert(appCatalogsTable, dbx.Params{
		"id": security.RandomString(15), "name": name, "kind": "partner", "source": "url", "url": strings.TrimSpace(input.URL),
		"public_key": strings.TrimSpace(input.PublicKey), "index_json": string(index), "fetched_at": now.UnixMilli(),
		"created_by": actorID, "created": now.UnixMilli(),
	}).Execute(); err != nil {
		return err
	}
	return applyAppCatalogRevocations(app, now)
}

// browseAppCatalogs is every trusted catalog's apps and the built-in ones,
// with which are installed and which versions were revoked.
func browseAppCatalogs(app core.App) (appCatalogBrowseWire, error) {
	browse := appCatalogBrowseWire{Catalogs: []appCatalogSourceWire{}, Apps: []appCatalogEntryWire{}, Categories: appCategories}
	catalogs, err := loadAppCatalogs(app)
	if err != nil {
		return browse, err
	}
	installs, err := loadAppInstalls(app, "", dbx.Params{})
	if err != nil {
		return browse, err
	}
	installed := map[string]string{}
	for _, install := range installs {
		if _, manifest, err := loadAppPackage(app, install.PackageHash); err == nil {
			installed[install.AppID] = manifest.Version
		}
	}
	revoked, err := appCatalogRevocations(app)
	if err != nil {
		return browse, err
	}
	for _, catalog := range catalogs {
		index, ok := readAppCatalogIndex(catalog)
		browse.Catalogs = append(browse.Catalogs, appCatalogSourceWire{ID: catalog.ID, Name: catalog.Name, Kind: catalog.Kind, Source: catalog.Source,
			URL: catalog.URL, KeyFingerprint: appCatalogKeyFingerprint(catalog.PublicKey), FetchedAt: catalog.FetchedAt, LastError: catalog.LastError,
			AppCount: len(index.Apps)})
		if !ok {
			continue
		}
		featured := map[string]bool{}
		for _, id := range index.Featured {
			featured[id] = true
		}
		for _, entry := range index.Apps {
			_, isRevoked := revoked[entry.Package.PackageHash]
			wire := appCatalogEntryWire{IconURL: appCatalogEntryIcon(entry.Icon), CatalogID: catalog.ID, CatalogName: catalog.Name, CatalogKind: catalog.Kind, ID: entry.ID,
				Slug: entry.Slug, Name: entry.Name, Summary: appTextOrEmpty(entry.Summary), Developer: entry.Developer, Category: entry.Category, Tags: entry.Tags,
				Version: entry.Version, Reviewed: entry.Reviewed, Score: entry.Score, Downloads: entry.Downloads, Added: entry.Added, Updated: entry.Updated,
				Languages: entry.Languages, Paid: entry.Paid, Network: appOneOfOr(entry.Network, []string{"none", "relay", "direct"}, "direct"),
				Risk: appOneOfOr(entry.Risk, []string{"low", "medium", "high"}, "medium"), Featured: featured[entry.ID], Example: entry.Example,
				InstalledVersion: installed[entry.ID], InstalledChange: appInstalledChange(installed[entry.ID], entry.Version), Revoked: isRevoked}
			if wire.Tags == nil {
				wire.Tags = []string{}
			}
			if wire.Languages == nil {
				wire.Languages = []string{}
			}
			browse.Apps = append(browse.Apps, wire)
		}
	}
	browse.Apps = append(browse.Apps, appBuiltinEntries(installed, revoked)...)
	return browse, nil
}

// appInstalledChange is what installing a listed version does to the
// installed one, or nothing for an app not installed.
func appInstalledChange(installed, listed string) string {
	if installed == "" {
		return ""
	}
	return appVersionChange(installed, listed)
}

func appOneOfOr(value string, allowed []string, fallback string) string {
	if appOneOf(value, allowed) {
		return value
	}
	return fallback
}
