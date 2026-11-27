//go:build !leafen_noapps

package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A person's app routes: the apps they may use, and opening one, which gives
// the host page a ticket for the app's files and a token for its calls. The
// files themselves load in a frame with no origin of its own, under a policy
// that lets their scripts come only from the app's own files and reach only
// the addresses an administrator approved (docs/app-platform.md 4, T1·T5·T7).

const appFramePrefix = "/apps-frame/"

// originlessFrameRoute is where an app's frame loads its files and opens its
// rooms (localOwnerAppFrameRequest); a build without apps has none.
func originlessFrameRoute(path string) bool {
	return strings.HasPrefix(path, appFramePrefix) || strings.HasPrefix(path, apiRoot+appRoomsPath+"/")
}

func registerAppFrameRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+installedAppsListPath, listUsableAppsRoute)
	event.Router.POST(apiRoot+installedAppSessionPath, openAppSessionRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.GET(appFramePrefix+"{ticket}/{path...}", serveAppFrameFile)
}

func listUsableAppsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	apps, err := usableApps(event.App, user.Id)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	response := usableAppListResponse{Apps: []usableAppWire{}}
	for _, usable := range apps {
		// The blocks a page can hold and whether a page can open the app beside it.
		blocks := []usableAppBlockWire{}
		for _, block := range usable.Manifest.Surfaces.Blocks {
			blocks = append(blocks, usableAppBlockWire{Key: block.Key, Name: block.Name, DefaultHeight: block.DefaultHeight})
		}
		// And the lines it adds to Leafen's menus.
		slashItems := []usableAppSlashItemWire{}
		for _, item := range usable.Manifest.SlashItems {
			slashItems = append(slashItems, usableAppSlashItemWire{Key: item.Key, Name: item.Name, Keywords: item.Keywords, Action: item.Action, Block: item.Block})
		}
		menuActions := []usableAppMenuActionWire{}
		for _, line := range usable.Manifest.MenuActions {
			menuActions = append(menuActions, usableAppMenuActionWire{Key: line.Key, Name: line.Name, On: line.On, Action: line.Action})
		}
		openers := []usableAppFileOpenerWire{}
		for _, opener := range usable.Manifest.Surfaces.FileOpeners {
			// And the blank files it starts, for the library's "새 문서".
			var made []usableAppNewFileWire
			for _, newFile := range opener.NewFiles {
				made = append(made, usableAppNewFileWire{Type: newFile.Type, Name: newFile.Name})
			}
			openers = append(openers, usableAppFileOpenerWire{Key: opener.Key, Name: opener.Name, Types: opener.Types, Edit: opener.Edit, NewFiles: made})
		}
		// And the tools it moves content from, for the import screen.
		var importSources []usableAppImportSourceWire
		for _, source := range usable.Manifest.Surfaces.ImportSources {
			importSources = append(importSources, usableAppImportSourceWire{Key: source.Key, Name: source.Name, Description: source.Description})
		}
		editors := []usableAppPropertyEditorWire{}
		for _, editor := range usable.Manifest.PropertyEditors {
			editors = append(editors, usableAppPropertyEditorWire{Key: editor.Key, Name: editor.Name, Types: editor.Types, Height: editor.Height})
		}
		response.Apps = append(response.Apps, usableAppWire{
			InstallID: usable.Install.ID, AppID: usable.Install.AppID, Slug: usable.Install.Slug, Name: usable.Manifest.Name,
			Description: appTextOrEmpty(usable.Manifest.Description), TenancyKey: usable.TenancyKey, TenancyName: usable.TenancyName,
			Blocks: blocks, FullPage: usable.Manifest.Surfaces.FullPage != nil, SidePanel: usable.Manifest.Surfaces.SidePanel != nil, SlashItems: slashItems, MenuActions: menuActions,
			PropertyEditors: editors, FileOpeners: openers, ImportSources: importSources, Automations: usableAppAutomations(usable.Manifest),
		})
	}
	return event.JSON(http.StatusOK, response)
}

func openAppSessionRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := appSessionInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
			refusalMessage("Choose where to open the app.", bodyRefusalReason("app session request", err)))
	}
	// Someone who added the app themselves agrees again once what it may do changed.
	if install, err := loadAppInstall(event.App, event.Request.PathValue("installId")); err == nil {
		needed, err := appConsentNeeded(event.App, install, user.Id)
		if err != nil {
			return appPlatformFailure(event, err)
		}
		if needed != nil {
			return apiError(event, http.StatusConflict, errorCodeAppConsentNeeded, "Agree to what the app may do now.")
		}
	}
	// Only a screen opened on a file, from its block, is held to that file.
	opened := appOpenedFile{}
	if input.Surface == "fileOpener" {
		opened = appOpenedFile{PageID: strings.TrimSpace(input.PageID), BlockID: strings.TrimSpace(input.FileBlockID)}
	}
	session, err := mintAppSession(event.App, user.Id, event.Request.PathValue("installId"), input.TenancyKey, opened)
	if errors.Is(err, errAppNotUsable) {
		if appStoppedFor(event.App, user.Id, event.Request.PathValue("installId")) {
			return apiError(event, http.StatusForbidden, errorCodeAppStopped, appStoppedMessage)
		}
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	entry, found := appSurfaceEntry(session.App.Manifest, input.Surface, input.Block, input.Editor, input.Opener, input.ImportSource)
	if !found {
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "The app has no such place to open.")
	}
	// The app's own address for the links and addresses it gives people and
	// their programs (a Bitwarden app, Windows Explorer, a clipper): the
	// outward one — written, else the way from outside the administrator
	// chose, else the request's — as its endpoints are told and listed, not
	// the 127.0.0.1 the person happens to have opened Leafen at (2026-11-13:
	// with Tailscale on, the vault still gave its http address).
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	// Leafen's own apps learn whether the person administers the server, so
	// a note about what only they turn on (an outside address) gives them the
	// way there and tells anyone else who can; an app of someone else's never
	// learns it (decision principle 3).
	builtin := appInstallBuiltin(event.App, session.App.Install)
	administrator := false
	if builtin {
		administrator, _ = isAdministrator(event.App, user.Id)
	}
	manifestURL, err := appHomeScreenManifestURL(event.App, session.App.Install)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	// An app that keeps no shared data has nothing to see in its workspace.
	workspaceID := ""
	for _, data := range session.App.Manifest.Data {
		if !data.PerPerson && !data.Hidden {
			workspaceID = session.Workspace.WorkspaceID
		}
	}
	return event.JSON(http.StatusOK, appSessionWire{
		InstallID: session.App.Install.ID, Slug: session.App.Install.Slug, Version: session.App.Manifest.Version,
		Name: session.App.Manifest.Name, Developer: appDeveloper(session.App.Manifest), Builtin: builtin,
		TenancyKey: session.App.TenancyKey,
		FrameURL:   appFramePrefix + session.Ticket + "/" + entry, Device: append([]string{}, session.App.Manifest.Device...),
		Token: session.Token, ExpiresAt: session.ExpiresAt, WorkspaceID: workspaceID,
		HomeURL: origin + "/apps/" + session.App.Install.Slug, ManifestURL: manifestURL, Administrator: administrator,
	})
}

// appSurfaceEntry is the file an app opens with in a place: its own page,
// one of its blocks in a page, beside a page, in a database cell, on a file
// or in the import screen's window for one of its tools.
func appSurfaceEntry(manifest appManifest, surface, block, editor, opener, source string) (string, bool) {
	switch surface {
	case "", "page":
		if manifest.Surfaces.FullPage != nil {
			return manifest.Surfaces.FullPage.Entry, true
		}
		return "ui/index.html", true
	case "sidePanel":
		if manifest.Surfaces.SidePanel != nil {
			return manifest.Surfaces.SidePanel.Entry, true
		}
	case "block":
		for _, declared := range manifest.Surfaces.Blocks {
			if declared.Key == block {
				return declared.Entry, true
			}
		}
	case "propertyEditor":
		for _, declared := range manifest.PropertyEditors {
			if declared.Key == editor {
				return declared.Entry, true
			}
		}
	case "fileOpener":
		for _, declared := range manifest.Surfaces.FileOpeners {
			if declared.Key == opener {
				return declared.Entry, true
			}
		}
	case "importSource":
		for _, declared := range manifest.Surfaces.ImportSources {
			if declared.Key == source {
				return declared.Entry, true
			}
		}
	}
	return "", false
}

// serveAppFrameFile serves one of an installed version's ui/ files under a
// valid ticket, never as the server's own origin.
func serveAppFrameFile(event *core.RequestEvent) error {
	ticket := event.Request.PathValue("ticket")
	filePath := event.Request.PathValue("path")
	install, strict, err := readAppFrameTicketStrict(event.App, ticket)
	if err != nil || !strings.HasPrefix(filePath, appPackageTopFolderUI) {
		return event.NoContent(http.StatusNotFound)
	}
	_, manifest, err := loadAppPackage(event.App, install.PackageHash)
	if err != nil {
		return event.NoContent(http.StatusNotFound)
	}
	webrtc := manifest.Realtime != nil && manifest.Realtime.WebRTC
	file := storedAppPackageFile{}
	// A large file streams from the file storage, ranges and all.
	var stream io.ReadSeekCloser
	if filePath == appFrameNoWebRTCPath {
		// Leafen's own script, never the package's file of that name.
		file.ContentType, file.Data = "text/javascript; charset=utf-8", appFrameNoWebRTCScript
	} else {
		file, err = findAppPackageFile(event.App, install.PackageHash, filePath)
		if errors.Is(err, sql.ErrNoRows) {
			return event.NoContent(http.StatusNotFound)
		}
		if err != nil {
			return event.NoContent(failedStatus(event, http.StatusInternalServerError, errorCodeAppPlatformFailed, err))
		}
		if appFrameHTML(file.ContentType) {
			if file.Data, err = loadAppPackageFile(event.App, install.PackageHash, filePath); err != nil {
				return event.NoContent(failedStatus(event, http.StatusInternalServerError, errorCodeAppPlatformFailed, err))
			}
			if !webrtc {
				file.Data = appFrameWithoutWebRTC(file.Data, ticket)
			}
		} else if !file.inRow() {
			fsys, reader, err := openAppPackageFile(event.App, file)
			if err != nil {
				return event.NoContent(failedStatus(event, http.StatusInternalServerError, errorCodeAppPlatformFailed, err))
			}
			defer fsys.Close()
			defer reader.Close()
			stream = reader
		}
	}
	origins, err := framingOrigins(event.Request)
	if err != nil {
		return event.NoContent(failedStatus(event, http.StatusInternalServerError, errorCodeAppPlatformFailed, err))
	}
	header := event.Response.Header()
	header.Set("Content-Type", file.ContentType)
	// Strict mode — the server's, or the organization's the ticket carries —
	// sends every outside call through the relay.
	if settings, err := loadAppPlatformSettings(event.App); err != nil || settings.StrictNetwork || strict {
		manifest.Network.Direct = nil
	}
	inner := appFrameHTML(file.ContentType) && !appScreenEntry(manifest, filePath)
	header.Set("Content-Security-Policy", appFramePolicy(origins, ticket, manifest, inner))
	// The policy says who frames it (frameAncestorsDirective, or anyone for an
	// inner page); the server's SAMEORIGIN would refuse the app's page.
	header.Del("X-Frame-Options")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	// The frame has no origin, so its module scripts and fonts load as
	// cross-origin requests; the ticket, not the origin, guards the files.
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("Cross-Origin-Resource-Policy", "cross-origin")
	header.Set("Cache-Control", "private, max-age=3600")
	if stream != nil {
		http.ServeContent(event.Response, event.Request, "", time.Time{}, stream)
		return nil
	}
	event.Response.WriteHeader(http.StatusOK)
	_, err = event.Response.Write(file.Data)
	return err
}

// appFramePolicy is the frame's content security policy: no origin of its
// own (sandbox), scripts and other files only from this version's ticket,
// and outside addresses only of the kinds the administrator approved; only
// Leafen frames it. A page the screen frames itself (inner) has a parent with
// no origin, which no policy can name, so it says nothing of who frames it;
// it has no origin either, and nothing of Leafen's. Origins are the server's
// addresses its pages are opened at (framingOrigins).
func appFramePolicy(origins []string, ticket string, manifest appManifest, inner bool) string {
	ticketed := []string{}
	rooms := []string{}
	for _, origin := range origins {
		ticketed = append(ticketed, origin+appFramePrefix+ticket+"/")
		rooms = append(rooms, appRoomsSource(origin, manifest)...)
	}
	own := strings.Join(ticketed, " ")
	// The screen reads its own files too — a WebAssembly module, an engine's
	// data, blobs and data: addresses it made — which reach no one but Leafen,
	// as its scripts already do.
	connect := append(rooms, own, "blob:", "data:")
	direct := map[string][]string{}
	for _, entry := range manifest.Network.Direct {
		source := appCSPSource(entry.URL)
		if source == "" {
			continue
		}
		for _, kind := range entry.Kinds {
			direct[kind] = append(direct[kind], source)
		}
	}
	sources := func(kind string, base ...string) string {
		list := append(base, direct[kind]...)
		if len(list) == 0 {
			return "'none'"
		}
		return strings.Join(list, " ")
	}
	directives := []string{
		"sandbox allow-scripts allow-forms",
		"default-src 'none'",
		"script-src " + own + map[bool]string{true: " 'wasm-unsafe-eval'"}[manifest.WebAssembly] +
			// Scripts the screen makes, when the app declares it.
			map[bool]string{true: " 'unsafe-eval' 'unsafe-inline'"}[manifest.ScriptEval],
		"style-src " + own + " 'unsafe-inline'",
		"img-src " + sources("img", own, "data:", "blob:"),
		"font-src " + sources("font", own, "data:"),
		"media-src " + sources("media", own, "blob:"),
		"connect-src " + sources("connect", connect...),
		"frame-src " + sources("frame", own),
		// A frame with no origin starts workers only from its own blob: copies
		// (classic ones; browsers refuse it module workers).
		"worker-src " + own + " blob:",
		"manifest-src 'none'",
		"object-src 'none'",
		"form-action 'none'",
		"base-uri 'none'",
	}
	if !inner {
		directives = append(directives, frameAncestorsDirective(origins))
	}
	return strings.Join(directives, "; ")
}

// appScreenEntry says a file is a page Leafen opens one of the app's screens
// at, rather than one the screen frames itself.
func appScreenEntry(manifest appManifest, path string) bool {
	surfaces := manifest.Surfaces
	entries := []string{"ui/index.html"}
	if surfaces.FullPage != nil {
		entries = append(entries, surfaces.FullPage.Entry)
	}
	if surfaces.SidePanel != nil {
		entries = append(entries, surfaces.SidePanel.Entry)
	}
	for _, block := range surfaces.Blocks {
		entries = append(entries, block.Entry)
	}
	for _, opener := range surfaces.FileOpeners {
		entries = append(entries, opener.Entry)
	}
	for _, source := range surfaces.ImportSources {
		entries = append(entries, source.Entry)
	}
	for _, editor := range manifest.PropertyEditors {
		entries = append(entries, editor.Entry)
	}
	return slices.Contains(entries, path)
}

// appCSPSource writes an approved https address as a policy source, or
// nothing for anything that could widen the policy.
func appCSPSource(raw string) string {
	wildcard := strings.Contains(raw, "://*.")
	parsed, err := url.Parse(strings.Replace(raw, "://*.", "://", 1))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || strings.ContainsAny(raw, " ;,'\"") {
		return ""
	}
	host := parsed.Host
	if wildcard {
		host = "*." + host
	}
	return "https://" + host + parsed.EscapedPath()
}

// appRoomsSource opens the frame's connections to the rooms socket, for an
// app that declares realtime rooms.
func appRoomsSource(origin string, manifest appManifest) []string {
	if manifest.Realtime == nil {
		return nil
	}
	socket := strings.Replace(strings.Replace(origin, "https://", "wss://", 1), "http://", "ws://", 1)
	return []string{socket + apiRoot + "/app-rooms/"}
}
