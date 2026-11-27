//go:build !leafen_noapps

package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// An app's endpoints (document apps decision of 2026-10-10, 8): a program
// outside Leafen — a file manager on WebDAV, a password manager's extension —
// reaches the app at /app-endpoints/<slug>/<key>/ on Leafen's own address, so
// nobody sets up a name or certificate for it. It signs in with the person's
// app password (HTTP Basic with their email, or as a bearer), and every path
// and method below goes to the app's worker, which runs as the person through
// the app and answers the status, headers and body. Nothing it answers can
// act as a page of Leafen's: the answer is sandboxed, never sniffed, and sets
// no cookie, so a page it serves can't read Leafen's sign-in or pose as it.

const (
	appEndpointBodyMaxBytes      = 1 << 20
	appEndpointAnswerMaxBytes    = 16 << 20
	appPasswordFailureWindow     = 15 * time.Minute
	appPasswordFailuresPermitted = 10
	appPasswordUsedEvery         = time.Minute
)

var (
	appPasswordMisses = &appPasswordFailures{seen: map[string]*appRelayWindow{}}

	// The headers a worker's answer may carry; Leafen sets its own others.
	appEndpointAnswerHeaders = []string{"Accept-Ranges", "Allow", "Cache-Control", "Content-Disposition", "Content-Language",
		"Content-Range", "Content-Type", "DAV", "ETag", "Expires", "Last-Modified", "Location", "Lock-Token", "MS-Author-Via",
		"Retry-After", "Vary", "X-OC-Mtime"}
)

// appPasswordFailures counts wrong passwords by where they come from and
// the name they give; past the limit that pair waits out the window.
type appPasswordFailures struct {
	mu   sync.Mutex
	seen map[string]*appRelayWindow
}

func (failures *appPasswordFailures) key(request *http.Request, username string) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	return host + "\x00" + strings.ToLower(username)
}

func (failures *appPasswordFailures) blocked(key string, now time.Time) bool {
	failures.mu.Lock()
	defer failures.mu.Unlock()
	window := failures.seen[key]
	return window != nil && now.Sub(window.started) < appPasswordFailureWindow && window.count >= appPasswordFailuresPermitted
}

func (failures *appPasswordFailures) miss(key string, now time.Time) {
	failures.mu.Lock()
	defer failures.mu.Unlock()
	window := failures.seen[key]
	if window == nil || now.Sub(window.started) >= appPasswordFailureWindow {
		window = &appRelayWindow{started: now}
		failures.seen[key] = window
	}
	window.count++
	if len(failures.seen) > 10000 {
		for name, stale := range failures.seen {
			if now.Sub(stale.started) >= appPasswordFailureWindow {
				delete(failures.seen, name)
			}
		}
	}
}

// registerAppEndpointRoutes takes each method an endpoint may name — a
// pattern for every method would clash with the app's own GET of every
// path; GET's also takes HEAD.
func registerAppEndpointRoutes(event *core.ServeEvent) {
	for _, method := range appEndpointMethods {
		if method == http.MethodHead {
			continue
		}
		// Programs, not pages, call these: no CORS answers OPTIONS for the
		// app, and no other origin's page reads what it answers.
		// A body's size is decided below, up to what Leafen keeps of a file.
		event.Router.Route(method, appEndpointAddressPrefix+"{slug}/{key}/{path...}", serveAppEndpoint).Unbind(apis.DefaultCorsMiddlewareId, apis.DefaultBodyLimitMiddlewareId)
		event.Router.Route(method, appEndpointAddressPrefix+"{slug}/{key}", serveAppEndpoint).Unbind(apis.DefaultCorsMiddlewareId, apis.DefaultBodyLimitMiddlewareId)
	}
}

// appEndpointPerson is who an app password names, while it may still use
// the app in that organization.
type appEndpointPerson struct {
	PasswordID, UserID, TenancyKey string
}

// appEndpointSignIn reads the request's app password for the install:
// Basic with the person's email (or the password's ID) as the name, or a
// bearer. An unknown password, a name that isn't its person's, or a person
// no longer granted the app are all the same refusal.
func appEndpointSignIn(app core.App, request *http.Request, install storedAppInstall, now time.Time) (appEndpointPerson, string, bool) {
	username, secret, basic := request.BasicAuth()
	if !basic {
		bearer, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !found {
			return appEndpointPerson{}, "", false
		}
		secret = strings.TrimSpace(bearer)
	}
	if secret == "" {
		return appEndpointPerson{}, username, false
	}
	stored := struct {
		ID         string `db:"id"`
		UserID     string `db:"user_id"`
		TenancyKey string `db:"tenancy_key"`
		LastUsedAt int64  `db:"last_used_at"`
		Email      string `db:"email"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[password]].[[id]], [[password]].[[user_id]], [[password]].[[tenancy_key]],
			[[password]].[[last_used_at]], [[user]].[[email]]
		FROM {{leafen_app_passwords}} [[password]] JOIN {{users}} [[user]] ON [[user]].[[id]] = [[password]].[[user_id]]
		WHERE [[password]].[[secret_hash]] = {:hash} AND [[password]].[[install_id]] = {:install}`).
		Bind(dbx.Params{"hash": appPasswordHash(secret), "install": install.ID}).One(&stored); err != nil {
		return appEndpointPerson{}, username, false
	}
	// An account with no email signs in with the password's ID; an empty name never matches.
	if basic && (stored.Email == "" || !strings.EqualFold(strings.TrimSpace(username), stored.Email)) && username != stored.ID {
		return appEndpointPerson{}, username, false
	}
	if active, err := accountMayAct(app, stored.UserID); err != nil || !active {
		return appEndpointPerson{}, username, false
	}
	if granted, err := appGranted(app, install.ID, stored.TenancyKey, stored.UserID); err != nil || !granted {
		return appEndpointPerson{}, username, false
	}
	if workspaces, err := appPersonOrdinaryWorkspaces(app, stored.UserID, stored.TenancyKey); err != nil || len(workspaces) == 0 {
		return appEndpointPerson{}, username, false
	}
	if now.Sub(time.UnixMilli(stored.LastUsedAt)) >= appPasswordUsedEvery {
		_, _ = app.DB().Update(appPasswordsTable, dbx.Params{"last_used_at": now.UnixMilli()}, dbx.HashExp{"id": stored.ID}).Execute()
	}
	return appEndpointPerson{PasswordID: stored.ID, UserID: stored.UserID, TenancyKey: stored.TenancyKey}, username, true
}

// appEndpointAuthority is what the worker may do as the person: their app
// connection, made as opening the app makes it when they haven't yet.
func appEndpointAuthority(app core.App, install storedAppInstall, manifest appManifest, person appEndpointPerson) (*userKeyAuthority, error) {
	if person.UserID == "" || manifest.PersonAccess == "" || manifest.PersonAccess == appOpenedFileAccess {
		return nil, nil
	}
	usable := usableApp{Install: install, Manifest: manifest, TenancyKey: person.TenancyKey}
	// The person's place in the app's workspace and their own data, as their
	// first opening of the app makes them.
	if _, err := appWorkspaceForPerson(app, usable, person.UserID); err != nil {
		return nil, err
	}
	readyAppPersonData(app, usable, person.UserID)
	if authority, err := appPersonAuthority(app, install, manifest, person.TenancyKey, person.UserID); err == nil {
		return authority, nil
	}
	if _, err := prepareAppForPerson(app, usable, person.UserID); err != nil {
		return nil, err
	}
	workspaces, err := appPersonWorkspaces(app, person.UserID, person.TenancyKey)
	if err != nil {
		return nil, err
	}
	if _, err := appConnection(app, install, manifest, person.TenancyKey, person.UserID, workspaces); err != nil {
		return nil, err
	}
	return appPersonAuthority(app, install, manifest, person.TenancyKey, person.UserID)
}

// appEndpointAnswer is what the worker returns: a status, headers, and a
// body as text or base64.
type appEndpointAnswer struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	BodyBase64 string            `json:"bodyBase64"`
	// File is a stored file Leafen sends instead of a body, with ranges.
	File string `json:"file"`
	// PackageFile is one of the package's site/ files, by its name below
	// site/, that an endpoint serving pages sends as a page.
	PackageFile string `json:"packageFile"`
}

// appEndpointPlain answers a refusal as protocol clients read it: a status
// and a line of text, sandboxed like everything served here.
func appEndpointPlain(event *core.RequestEvent, status int, message string) error {
	appEndpointHeaders(event.Response.Header())
	event.Response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	return event.String(status, message+"\n")
}

func appEndpointHeaders(header http.Header) {
	header.Set("Content-Security-Policy", "sandbox; default-src 'none'; frame-ancestors 'none'")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Cache-Control", "private, no-store")
}

func serveAppEndpoint(event *core.RequestEvent) error {
	now := time.Now()
	installs, err := loadAppInstalls(event.App, "[[slug]] = {:slug}", dbx.Params{"slug": event.Request.PathValue("slug")})
	if err != nil {
		return appEndpointFailure(event, "Leafen couldn't read the app.", err)
	}
	if len(installs) != 1 || installs[0].RemovedAt != 0 {
		return appEndpointPlain(event, http.StatusNotFound, "Not found.")
	}
	install := installs[0]
	_, manifest, err := loadAppPackage(event.App, install.PackageHash)
	if err != nil {
		return appEndpointFailure(event, "Leafen couldn't read the app.", err)
	}
	key := event.Request.PathValue("key")
	index := slices.IndexFunc(manifest.Endpoints, func(endpoint appManifestEndpoint) bool { return endpoint.Key == key })
	if index < 0 {
		return appEndpointPlain(event, http.StatusNotFound, "Not found.")
	}
	endpoint := manifest.Endpoints[index]
	if install.Status != "active" {
		return appEndpointPlain(event, http.StatusServiceUnavailable, "The app is stopped.")
	}
	// A page's requests carry the origin "null"; their preflight ends here.
	if answered, err := appEndpointPageCORS(event, endpoint); answered {
		return err
	}
	method := event.Request.Method
	if len(endpoint.Methods) > 0 && !slices.Contains(endpoint.Methods, method) {
		event.Response.Header().Set("Allow", strings.Join(endpoint.Methods, ", "))
		return appEndpointPlain(event, http.StatusMethodNotAllowed, "The app doesn't take "+method+" here.")
	}
	// A public endpoint, once an administrator opened it, answers for no one
	// in particular and leaves signing in to the app: its Authorization goes
	// to the worker, as a Bitwarden client's token does. Until then it is a
	// person's like any other.
	person, limitKey := appEndpointPerson{}, ""
	public := appEndpointOpened(event.App, install, endpoint)
	if public {
		person.TenancyKey = appEndpointPublicTenancy(event.App, install, event.Request)
		limitKey = "public:" + appPasswordMisses.key(event.Request, "")
	} else {
		username, _, _ := event.Request.BasicAuth()
		misses := appPasswordMisses.key(event.Request, username)
		if appPasswordMisses.blocked(misses, now) {
			event.Response.Header().Set("Retry-After", strconv.Itoa(int(appPasswordFailureWindow.Seconds())))
			return appEndpointPlain(event, http.StatusTooManyRequests, "Too many wrong passwords; try again later.")
		}
		signedIn, _, ok := appEndpointSignIn(event.App, event.Request, install, now)
		if !ok {
			if event.Request.Header.Get("Authorization") != "" {
				appPasswordMisses.miss(misses, now)
			}
			realm := strings.NewReplacer(`"`, "", `\`, "").Replace(appDisplayName(manifest))
			event.Response.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`", charset="UTF-8"`)
			return appEndpointPlain(event, http.StatusUnauthorized, "Log in with an app password from Leafen's Apps page.")
		}
		person, limitKey = signedIn, signedIn.UserID
	}
	limits := loadAppEndpointLimits(event.App, install.ID)
	ticket := uint64(0)
	defer appEndpointGates.leave(install.ID, &ticket)
	release, refusal := appEndpointGates.admit(install.ID, limitKey, limits, now, &ticket)
	// Busy for a moment: a browser opens several requests at once.
	for deadline := now.Add(appEndpointGateWait); refusal == http.StatusServiceUnavailable && time.Now().Before(deadline); {
		if ended := requestPause(event, 20*time.Millisecond); ended && requestClientLeft(event) {
			return nil
		} else if ended {
			break // a stopping server ended it: told busy below (server_stopping.go)
		}
		release, refusal = appEndpointGates.admit(install.ID, limitKey, limits, time.Now(), &ticket)
	}
	switch refusal {
	case http.StatusTooManyRequests:
		countAppEndpoint(install.ID, now, appEndpointCount{refused: 1})
		event.Response.Header().Set("Retry-After", "60")
		return appEndpointPlain(event, refusal, "Too many requests to the app this minute; try again shortly.")
	case http.StatusServiceUnavailable:
		countAppEndpoint(install.ID, now, appEndpointCount{refused: 1})
		event.Response.Header().Set("Retry-After", "5")
		return appEndpointPlain(event, refusal, "The app is busy; try again in a moment.")
	}
	defer release()
	authority, err := appEndpointAuthority(event.App, install, manifest, person)
	if err != nil {
		event.App.Logger().Warn("leafen app endpoint person failed", "install", install.ID, "error", err)
		return appEndpointPlain(event, http.StatusForbidden, "The app can't reach your data here.")
	}
	actor := func() (appEndpointActor, error) {
		return appEndpointActorFor(event.App, install, manifest, person.TenancyKey, person.UserID, authority)
	}
	// A size declared past what Leafen keeps, or past what the person may
	// keep through the app, is refused before any of it is read.
	// Some endpoints take every upload as a file, however small.
	asFile := slices.Contains(endpoint.FileBodies, method)
	if declared := event.Request.ContentLength; declared > fileTransferMaxFileBytes {
		return appEndpointPlain(event, http.StatusRequestEntityTooLarge, "The request is larger than Leafen keeps.")
	} else if declared > appEndpointBodyMaxBytes || (asFile && declared > 0) {
		if left, err := appEndpointStorageLeft(event.App, install.ID, person.UserID, limits, declared); err != nil || !left {
			return appEndpointPlain(event, http.StatusInsufficientStorage,
				"You keep as much through this app as its administrator allows; remove files you don't need.")
		}
	}
	body, err := io.ReadAll(io.LimitReader(event.Request.Body, appEndpointBodyMaxBytes+1))
	if err != nil {
		return appEndpointPlain(event, http.StatusBadRequest, "Leafen couldn't read the request.")
	}
	// A body past a megabyte is kept as a file the worker gets instead.
	var bodyFile *fileTransferFile
	if len(body) > appEndpointBodyMaxBytes || (asFile && len(body) > 0) {
		// A body read to its end is whole: the request's reader would only
		// start over (PocketBase lets a body be read again).
		rest := io.Reader(event.Request.Body)
		if len(body) <= appEndpointBodyMaxBytes {
			rest = strings.NewReader("")
		}
		stored, kept, err := appEndpointKeepBody(event, actor, body, rest, install.ID, limits)
		if !kept {
			return err
		}
		body, bodyFile = nil, &stored
	}
	// A body the run refused, or didn't answer, goes at once
	// (app_platform_endpoint_files.go).
	bodyKept := false
	if bodyFile != nil {
		defer func() {
			if !bodyKept {
				releaseAppEndpointBody(event.App, bodyFile.ID)
			}
		}()
	}
	received := int64(len(body))
	if bodyFile != nil {
		received = bodyFile.Size
	}
	countAppEndpoint(install.ID, now, appEndpointCount{requests: 1, received: received})
	headers := map[string]string{}
	for name, values := range event.Request.Header {
		switch lower := strings.ToLower(name); {
		case lower == "cookie" || lower == "proxy-authorization":
		case lower == "authorization" && !public:
		default:
			headers[lower] = strings.Join(values, ", ")
		}
	}
	path := "/" + event.Request.PathValue("path")
	fields := map[string]any{
		"endpoint": endpoint.Key, "method": method, "path": path, "query": event.Request.URL.RawQuery, "headers": headers,
		"body": base64.StdEncoding.EncodeToString(body), "person": person.UserID, "tenancy": person.TenancyKey,
	}
	if bodyFile != nil {
		fields["bodyFile"] = bodyFile
	}
	// The endpoint's address as people are shown it, for an app that names
	// its own addresses to the programs it answers.
	if origin, err := outwardServerOrigin(event.Request); err == nil {
		fields["address"] = appEndpointAddress(origin, install.Slug, endpoint.Key)
	}
	input, _ := json.Marshal(fields)
	run := appRun{Trigger: "endpoint", Ref: endpoint.Key + " " + method, Subject: person.UserID, Worker: endpoint.Worker,
		Function: endpoint.Function, Input: input, Tenancy: person.TenancyKey, Person: authority}
	ctx, refusals := withAppRunRefusals(event.Request.Context())
	output, _, err := runAppWorkerFunction(ctx, event.App, install, manifest, run)
	if err != nil {
		if errors.Is(err, errAppWorkerBusy) {
			event.Response.Header().Set("Retry-After", "5")
			return appEndpointPlain(event, http.StatusServiceUnavailable, "The app is busy; try again in a moment.")
		}
		if answered, err := appEndpointRunFailed(event, install.ID, now, refusals); answered {
			return err
		}
		return appEndpointPlain(event, http.StatusBadGateway, "The app couldn't answer.")
	}
	answer := appEndpointAnswer{}
	if json.Unmarshal(output, &answer) != nil {
		return appEndpointPlain(event, http.StatusBadGateway, "The app's answer isn't one Leafen reads.")
	}
	if answer.Status == 0 {
		answer.Status = http.StatusOK
	}
	content := []byte(answer.Body)
	if answer.BodyBase64 != "" {
		if content, err = base64.StdEncoding.DecodeString(answer.BodyBase64); err != nil {
			return appEndpointPlain(event, http.StatusBadGateway, "The app's answer isn't one Leafen reads.")
		}
	}
	if answer.Status < 200 || answer.Status > 599 {
		return appEndpointPlain(event, http.StatusBadGateway, "The app's answer isn't one Leafen reads.")
	}
	// A failure under a stop, or after a call refused for a moment, says so (app_platform_endpoint_refusals.go).
	if answer.Status >= http.StatusBadRequest && appStoppedSince(install.ID, now) {
		return appEndpointPlain(event, http.StatusServiceUnavailable, "The app is stopped.")
	}
	wait, refused := refusals.failedWait(answer.Status)
	if refused {
		answer.Status = http.StatusServiceUnavailable
	}
	bodyKept = answer.Status < http.StatusBadRequest
	header := event.Response.Header()
	for name, value := range answer.Headers {
		canonical := http.CanonicalHeaderKey(name)
		allowed := slices.ContainsFunc(appEndpointAnswerHeaders, func(each string) bool { return strings.EqualFold(each, canonical) })
		if !allowed || strings.ContainsAny(value, "\r\n") {
			continue
		}
		// A move elsewhere stays within the endpoint, on Leafen's address.
		if strings.EqualFold(canonical, "Location") && !appEndpointLocationAllowed(value, appEndpointAddressPrefix+install.Slug+"/"+endpoint.Key+"/") {
			continue
		}
		header.Set(canonical, value)
	}
	if refused && header.Get("Retry-After") == "" {
		header.Set("Retry-After", strconv.Itoa(wait))
	}
	cacheControl := header.Get("Cache-Control")
	appEndpointHeaders(header)
	if cacheControl != "" {
		header.Set("Cache-Control", cacheControl)
	}
	if answer.PackageFile != "" {
		return appEndpointServePage(event, install, endpoint, answer.PackageFile, answer.Status)
	}
	if answer.File != "" {
		err := appEndpointServeFile(event, actor, answer.File)
		sent, _ := strconv.ParseInt(header.Get("Content-Length"), 10, 64)
		countAppEndpoint(install.ID, time.Now(), appEndpointCount{sent: sent})
		return err
	}
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/octet-stream")
	}
	event.Response.WriteHeader(answer.Status)
	if method != http.MethodHead {
		_, err = event.Response.Write(content)
		countAppEndpoint(install.ID, time.Now(), appEndpointCount{sent: int64(len(content))})
	}
	return err
}

// appEndpointLocationAllowed keeps a redirect on the endpoint's own paths.
func appEndpointLocationAllowed(location, prefix string) bool {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil {
		return false
	}
	return strings.HasPrefix(parsed.Path, prefix) && !strings.Contains(parsed.Path, "/../") && !strings.HasSuffix(parsed.Path, "/..")
}

// appEndpointKeepBody stores a large body in the app's workspace and
// answers the stored file; when it can't, it writes the refusal and
// answers kept false, the request done.
func appEndpointKeepBody(event *core.RequestEvent, actor func() (appEndpointActor, error), head []byte, rest io.Reader, installID string,
	limits appEndpointLimitsWire) (fileTransferFile, bool, error) {
	refuse := func(status int, message string) (fileTransferFile, bool, error) {
		return fileTransferFile{}, false, appEndpointPlain(event, status, message)
	}
	who, err := actor()
	if err != nil {
		return refuse(http.StatusForbidden, "The app can't keep files here.")
	}
	spool, size, err := spoolAppEndpointBody(event.App, head, rest)
	if errors.Is(err, errFileUploadInvalid) {
		return refuse(http.StatusRequestEntityTooLarge, "The request is larger than Leafen keeps.")
	}
	if errors.Is(err, errServerDiskLow) {
		return refuse(http.StatusInsufficientStorage, "The server's disk is almost full; ask its administrator to free space.")
	}
	if err != nil {
		return refuse(http.StatusBadRequest, "Leafen couldn't read the request.")
	}
	defer func() {
		spool.Close()
		os.Remove(spool.Name())
	}()
	if left, err := appEndpointStorageLeft(event.App, installID, who.personID(), limits, size); err != nil || !left {
		return refuse(http.StatusInsufficientStorage, "You keep as much through this app as its administrator allows; remove files you don't need.")
	}
	contentType := event.Request.Header.Get("Content-Type")
	stored, err := stageAppEndpointFile(event.App, who, spool, size, appEndpointBodyName(event.Request.PathValue("path")), contentType)
	if err != nil {
		if isDLPPolicyDecisionError(err) {
			return refuse(http.StatusForbidden, "The organization's data rules keep this file out.")
		}
		// A refusal isn't a full disk: a WebDAV client shows 507 as one.
		if isPermissionDenied(err) {
			return refuse(http.StatusForbidden, "You can't keep files in this app's space.")
		}
		if errors.Is(err, errServerDiskLow) {
			return refuse(http.StatusInsufficientStorage, "The server's disk is almost full; ask its administrator to free space.")
		}
		event.App.Logger().Warn("leafen app endpoint body failed", "error", err)
		return fileTransferFile{}, false, appEndpointFailure(event, "Leafen couldn't keep the request's body.", err)
	}
	recordAppEndpointFile(event.App, installID, who.personID(), stored.ID)
	// A sync tool's own time for the file stays with it (product_file_client_modified.go).
	if modified, ok := fileClientModifiedHeader(event.Request.Header.Get("X-OC-Mtime")); ok {
		if err := setFileClientModified(event.App, stored.ID, modified); err != nil {
			event.App.Logger().Warn("leafen app endpoint body time not kept", "error", err)
		}
	}
	return stored, true, nil
}

// appEndpointServeFile sends a stored file the run may read, with ranges.
func appEndpointServeFile(event *core.RequestEvent, actor func() (appEndpointActor, error), fileID string) error {
	who, err := actor()
	if err != nil {
		return appEndpointPlain(event, http.StatusBadGateway, "The app's answer isn't one Leafen sends.")
	}
	read, err := authorizeAppEndpointFile(event.App, who, fileID)
	if err != nil {
		return appEndpointPlain(event, http.StatusBadGateway, "The app's answer isn't one Leafen sends.")
	}
	defer read.Filesystem.Close()
	if event.Response.Header().Get("Content-Type") == "" {
		event.Response.Header().Set("Content-Type", read.Record.GetString("content_type"))
	}
	event.Response.Header().Set("Content-Disposition", "attachment")
	return serveNamedFile(read.Filesystem, event.Response, event.Request, read.Record.BaseFilesPath()+"/"+read.Record.GetString("blob"),
		read.Record.GetString("original_name"))
}

// appEndpointPublicTenancy is the organization an unsigned request names
// with ?org=, when the app has a workspace there; otherwise none.
func appEndpointPublicTenancy(app core.App, install storedAppInstall, request *http.Request) string {
	organization := request.URL.Query().Get("org")
	if organization == "" || !appWorkspaceTenancyKnown(app, install.ID, "org:"+organization) {
		return ""
	}
	return "org:" + organization
}
