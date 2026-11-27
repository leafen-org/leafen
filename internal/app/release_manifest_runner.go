package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	releaseManifestRunnerStoreKey = "leafenReleaseManifestRunner"
	// The address the lists' files are under (release-v1.json, sponsors-v1.json).
	releaseListURLEnv = "LEAFEN_RELEASE_LIST_URL"
	updateCheckEnv    = "LEAFEN_UPDATE_CHECK"
	// A day: a new sponsor's month starts the next day, a logo or slogan taken
	// down is gone within a day, and so is a release or security notice late
	// (decision of 2026-10-20). Each check only asks whether a list changed.
	releaseManifestInterval   = 24 * time.Hour
	releaseManifestJitter     = 2 * time.Hour
	releaseManifestIdleDelay  = 6 * time.Hour
	releaseManifestFetchLimit = 20 * time.Second
)

var releaseManifestRetryDelays = []time.Duration{6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 72 * time.Hour}

// storedReleaseManifestState holds the release notice (payload_json, key_id,
// issued_at) and the sponsor list (sponsor_…) last fetched.
type storedReleaseManifestState struct {
	PayloadJSON        string `db:"payload_json"`
	KeyID              string `db:"key_id"`
	IssuedAt           string `db:"issued_at"`
	SponsorPayloadJSON string `db:"sponsor_payload_json"`
	SponsorIssuedAt    string `db:"sponsor_issued_at"`
	FetchedAt          int64  `db:"fetched_at"`
	CheckedAt          int64  `db:"checked_at"`
	NextCheckAt        int64  `db:"next_check_at"`
	Failures           int    `db:"failures"`
}

func loadReleaseManifestState(app core.App) (storedReleaseManifestState, error) {
	state := storedReleaseManifestState{}
	err := app.DB().NewQuery(`
		SELECT [[payload_json]], [[key_id]], [[issued_at]], [[sponsor_payload_json]], [[sponsor_issued_at]],
			[[fetched_at]], [[checked_at]], [[next_check_at]], [[failures]]
		FROM {{leafen_release_manifest_state}} WHERE [[id]] = 'default'
	`).One(&state)
	return state, err
}

// currentReleaseNotice is the newest trusted release notice: the last one
// fetched when it is newer than the one this release bundles.
func currentReleaseNotice(app core.App) (releaseNotice, string, storedReleaseManifestState) {
	bundled := bundledReleaseNoticePayload()
	state, err := loadReleaseManifestState(app)
	if err != nil || state.PayloadJSON == "" {
		return bundled, "bundled", state
	}
	fetched, err := parseReleaseNotice([]byte(state.PayloadJSON), releaseListTrusted)
	if err != nil || !releaseListNotOlder(fetched.IssuedAt, bundled.IssuedAt) {
		return bundled, "bundled", state
	}
	return fetched, "fetched", state
}

// currentSponsorList is the newest trusted sponsor list, as the notice.
func currentSponsorList(app core.App) sponsorList {
	bundled := bundledSponsorListPayload()
	state, err := loadReleaseManifestState(app)
	if err != nil || state.SponsorPayloadJSON == "" {
		return bundled
	}
	fetched, err := parseSponsorList([]byte(state.SponsorPayloadJSON), releaseListTrusted)
	if err != nil || !releaseListNotOlder(fetched.IssuedAt, bundled.IssuedAt) {
		return bundled
	}
	return fetched
}

func releaseListNotOlder(candidate, current string) bool {
	candidateTime, candidateErr := time.Parse(time.RFC3339, candidate)
	currentTime, currentErr := time.Parse(time.RFC3339, current)
	return candidateErr == nil && currentErr == nil && !candidateTime.Before(currentTime)
}

func updateCheckDisabledByEnvironment() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(updateCheckEnv)), "off")
}

// releaseListURL is the address the daily check reads the lists' files
// under: https, or plain http only to a loopback address for local test lists.
func releaseListURL() string {
	raw := strings.TrimRight(strings.TrimSpace(os.Getenv(releaseListURLEnv)), "/")
	if raw == "" {
		raw = officialReleaseListURL
	}
	if !allowedReleaseManifestURL(raw) {
		return ""
	}
	return raw
}

func allowedReleaseManifestURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(parsed.Hostname())
	return parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))
}

type releaseManifestRunner struct {
	app    core.App
	client *http.Client
	wake   chan struct{}
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newReleaseManifestRunner(app core.App) *releaseManifestRunner {
	client := &http.Client{
		Timeout: releaseManifestFetchLimit,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !allowedReleaseManifestURL(request.URL.String()) {
				return errReleaseManifestInvalid
			}
			return nil
		},
	}
	return &releaseManifestRunner{app: app, client: client, wake: make(chan struct{}, 1)}
}

func (runner *releaseManifestRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done = cancel, make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *releaseManifestRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel, done := runner.cancel, runner.done
	runner.cancel, runner.done = nil, nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("release manifest shutdown: %w", ctx.Err())
	}
}

func (runner *releaseManifestRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func notifyReleaseManifestRunner(app core.App) {
	if runner, ok := app.Store().Get(releaseManifestRunnerStoreKey).(*releaseManifestRunner); ok {
		runner.notify()
	}
}

func (runner *releaseManifestRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeJobRunnerFailed, "release-manifest", err)
			wait = releaseManifestRetryDelays[0]
		}
		select {
		case <-ctx.Done():
			return
		case <-runner.wake:
		case <-time.After(wait):
		}
	}
}

// runTurn checks the lists when the check is on and due, and returns how
// long to wait before looking again. The release notice decides the next
// check: read or unchanged, the next is a day on; not, it is tried again
// sooner. A sponsor list that came is kept either way.
func (runner *releaseManifestRunner) runTurn(ctx context.Context) (time.Duration, error) {
	settings, err := loadAdministratorSettings(runner.app)
	if err != nil {
		return 0, err
	}
	source := releaseListURL()
	if !settings.UpdateCheckEnabled || updateCheckDisabledByEnvironment() || source == "" {
		return releaseManifestIdleDelay, nil
	}
	state, err := loadReleaseManifestState(runner.app)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	if due := time.UnixMilli(state.NextCheckAt); due.After(now) {
		return min(due.Sub(now), releaseManifestIdleDelay), nil
	}
	current, _, _ := currentReleaseNotice(runner.app)
	heldSponsors := currentSponsorList(runner.app)
	notice, noticeKey, noticePayload, noticeErr := runner.fetchReleaseNotice(ctx, source+"/"+releaseNoticeFile, current.IssuedAt, now)
	sponsors, sponsorKey, sponsorPayload, sponsorErr := runner.fetchSponsorList(ctx, source+"/"+sponsorListFile, heldSponsors.IssuedAt, now)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if noticeErr == nil && !releaseListNotOlder(notice.IssuedAt, current.IssuedAt) {
		noticeErr = errReleaseManifestInvalid
	}
	if sponsorErr == nil && !releaseListNotOlder(sponsors.IssuedAt, heldSponsors.IssuedAt) {
		sponsorErr = errReleaseManifestInvalid
	}
	params := dbx.Params{"now": now.UnixMilli()}
	sets := []string{"[[checked_at]] = {:now}", "[[next_check_at]] = {:next}"}
	if sponsorErr == nil {
		params["sponsorPayload"], params["sponsorKeyId"], params["sponsorIssuedAt"] = string(sponsorPayload), sponsorKey, sponsors.IssuedAt
		sets = append(sets, "[[sponsor_payload_json]] = {:sponsorPayload}", "[[sponsor_key_id]] = {:sponsorKeyId}",
			"[[sponsor_issued_at]] = {:sponsorIssuedAt}")
	}
	if noticeErr == nil || errors.Is(noticeErr, errReleaseListUnchanged) {
		jitter, err := rand.Int(rand.Reader, big.NewInt(int64(releaseManifestJitter)))
		if err != nil {
			return 0, err
		}
		params["next"] = now.Add(releaseManifestInterval + time.Duration(jitter.Int64())).UnixMilli()
		sets = append(sets, "[[fetched_at]] = {:now}", "[[failures]] = 0", "[[last_error_code]] = ''")
		if noticeErr == nil {
			params["payload"], params["keyId"], params["issuedAt"] = string(noticePayload), noticeKey, notice.IssuedAt
			sets = append(sets, "[[payload_json]] = {:payload}", "[[key_id]] = {:keyId}", "[[issued_at]] = {:issuedAt}")
		}
	} else {
		delay := releaseManifestRetryDelays[min(state.Failures, len(releaseManifestRetryDelays)-1)]
		params["next"], params["code"] = now.Add(delay).UnixMilli(), releaseManifestErrorCode(noticeErr)
		sets = append(sets, "[[failures]] = [[failures]] + 1", "[[last_error_code]] = {:code}")
	}
	query := "UPDATE {{leafen_release_manifest_state}} SET " + strings.Join(sets, ", ") + " WHERE [[id]] = 'default'"
	if _, err := runner.app.DB().NewQuery(query).Bind(params).Execute(); err != nil {
		return 0, err
	}
	return min(time.Duration(params["next"].(int64)-now.UnixMilli())*time.Millisecond, releaseManifestIdleDelay), nil
}

func (runner *releaseManifestRunner) fetchReleaseNotice(ctx context.Context, source, held string, now time.Time) (releaseNotice, string, []byte, error) {
	raw, served, err := runner.fetch(ctx, source, held)
	if served.After(now) {
		now = served
	}
	if err != nil {
		return releaseNotice{}, "", nil, err
	}
	keyID, payload, err := verifyReleaseEnvelope(raw, releaseNoticeEnvelopeFormat, releaseNoticeKeys)
	if err != nil {
		return releaseNotice{}, "", nil, err
	}
	notice, err := parseReleaseNotice(payload, now)
	return notice, keyID, payload, err
}

func (runner *releaseManifestRunner) fetchSponsorList(ctx context.Context, source, held string, now time.Time) (sponsorList, string, []byte, error) {
	raw, served, err := runner.fetch(ctx, source, held)
	if served.After(now) {
		now = served
	}
	if err != nil {
		return sponsorList{}, "", nil, err
	}
	keyID, payload, err := verifyReleaseEnvelope(raw, sponsorListEnvelopeFormat, sponsorListKeys)
	if err != nil {
		return sponsorList{}, "", nil, err
	}
	list, err := parseSponsorList(payload, now)
	return list, keyID, payload, err
}

// errReleaseListUnchanged is a list the host says hasn't changed since the
// one held was issued.
var errReleaseListUnchanged = errors.New("LEAFEN_RELEASE_LIST_UNCHANGED")

// fetch reads a list's file as a plain GET that carries nothing about this
// instance: no cookies, no query, a generic user agent. It asks for the file
// only if it changed since the list held was issued: a newer list is
// published after it, and the same one answers with no body. It also answers
// the list host's time (ReleaseListNow).
func (runner *releaseManifestRunner) fetch(ctx context.Context, source, held string) ([]byte, time.Time, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	request.Header.Set("User-Agent", "Leafen")
	request.Header.Set("Accept", "application/json")
	if issued, err := time.Parse(time.RFC3339, held); err == nil {
		request.Header.Set("If-Modified-Since", issued.UTC().Format(http.TimeFormat))
	}
	response, err := runner.client.Do(request)
	if err != nil {
		return nil, time.Time{}, errReleaseManifestInvalid
	}
	defer response.Body.Close()
	served := ReleaseListNow(time.Time{}, response)
	if response.StatusCode == http.StatusNotModified {
		return nil, served, errReleaseListUnchanged
	}
	if response.StatusCode != http.StatusOK {
		return nil, served, errReleaseManifestInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseManifestBytes+1))
	if err != nil {
		return nil, served, errReleaseManifestInvalid
	}
	return raw, served, nil
}

func releaseManifestErrorCode(err error) string {
	if errors.Is(err, errReleaseManifestSignature) {
		return errReleaseManifestSignature.Error()
	}
	return errReleaseManifestInvalid.Error()
}

// projectAdministratorUpdateSettings is what administrators alone see: the
// check's switch and, when the trusted manifest names a newer release, that
// version with its notes, upgrade link and security marker, and where this
// server runs, so the screen says how to update it there (in Docker: pull the
// image and make the container again, its volumes and settings kept).
func projectAdministratorUpdateSettings(app core.App, settings storedAdministratorSettings) administratorUpdateSettings {
	runs, _ := administratorBackupRestoreWay(app)
	manifest, source, state := currentReleaseNotice(app)
	current, currentOK := parseReleaseVersion(leafenReleaseVersion)
	latest, latestOK := parseReleaseVersion(manifest.Latest.Version)
	newer := currentOK && latestOK && compareReleaseVersions(latest, current) > 0
	checkedAt := ""
	if state.CheckedAt > 0 {
		checkedAt = time.UnixMilli(state.CheckedAt).UTC().Format(time.RFC3339)
	}
	return administratorUpdateSettings{
		Enabled: settings.UpdateCheckEnabled, EnvironmentDisabled: updateCheckDisabledByEnvironment(),
		CurrentVersion: leafenReleaseVersion, LatestVersion: manifest.Latest.Version, Newer: newer,
		Security: newer && manifest.Latest.Security, NotesURL: manifest.Latest.NotesURL,
		UpgradeURL: manifest.Latest.UpgradeURL, UpgradeRuns: runs, Source: source, CheckedAt: checkedAt,
		// What the last check left: a check that failed says nothing about the latest.
		CheckFailed: state.Failures > 0,
	}
}
