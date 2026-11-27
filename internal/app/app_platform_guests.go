//go:build !leafen_noapps

package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Public guests (docs/app-platform.md 9, T9): when an administrator lets an
// app take them, a person who uses it makes a link to one of its guest
// rooms that runs out; the token rides after "#" so no server or log sees
// it. A guest opens the app with no account and reaches only that room and
// the organization's volatile store — no Leafen data, actions or outside
// addresses. A room takes the app's declared number of guests, a guest
// session lasts the link's time or two hours, and revoking a link ends its
// guests at once.

const (
	appGuestHeader      = "X-Leafen-Guest"
	appGuestSessionsMax = 10000
)

var (
	errAppGuestRefused = errors.New(errorCodeAppGuestRefused)
	errAppGuestBlocked = errors.New("the guest's address was blocked from the link")
)

type appGuestSession struct {
	LinkID, InstallID, Tenancy, Room string
	Expires                          time.Time
	// AddressHash names the guest's address for its link, to keep a
	// blocked guest out of it.
	AddressHash string
}

type appGuestSessions struct {
	mu       sync.Mutex
	sessions map[string]appGuestSession
	opening  *appRelayLimits
}

var appGuests = &appGuestSessions{sessions: map[string]appGuestSession{}, opening: newAppRelayLimits(30, 1000)}

func appGuestTokenHash(token string) string {
	digest := sha256.Sum256([]byte("leafen-app-guest\x00" + token))
	return hex.EncodeToString(digest[:])
}

func appGuestsEnabled(app core.App, installID string) bool {
	var enabled int
	err := app.DB().NewQuery(`SELECT [[enabled]] FROM {{leafen_app_guest_settings}} WHERE [[install_id]] = {:install}`).
		Bind(dbx.Params{"install": installID}).Row(&enabled)
	return err == nil && enabled == 1
}

// makeAppGuestLink makes a link to one of the app's guest rooms in the
// organization the person's token was opened for.
func makeAppGuestLink(app core.App, install storedAppInstall, manifest appManifest, tenancy, personID, room string, minutes int, origin string) (appGuestLinkMade, error) {
	switch {
	case manifest.Guests == nil:
		return appGuestLinkMade{}, refuse(errAppGuestRefused, "the app's manifest takes no guests")
	case !appGuestsEnabled(app, install.ID):
		return appGuestLinkMade{}, refuse(errAppGuestRefused, "guests aren't on for this app: an administrator turns them on")
	case !appOneOf(room, manifest.Guests.Rooms):
		return appGuestLinkMade{}, refuse(errAppGuestRefused, "room is one guests may join (%s), not %q", strings.Join(manifest.Guests.Rooms, ", "), room)
	case minutes < appGuestLinkMinMinutes || minutes > appGuestLinkMaxMinutes:
		return appGuestLinkMade{}, refuse(errAppGuestRefused, "minutes is from %d to %d, not %d", appGuestLinkMinMinutes, appGuestLinkMaxMinutes, minutes)
	}
	token, linkID := "lgl_"+security.RandomString(40), security.RandomString(16)
	now := time.Now()
	expires := now.Add(time.Duration(minutes) * time.Minute)
	if err := app.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().Insert("leafen_app_guest_links", dbx.Params{
			"id": linkID, "install_id": install.ID, "tenancy": tenancy, "room": room, "token_hash": appGuestTokenHash(token),
			"created_by": personID, "created": now.UnixMilli(), "expires_at": expires.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "guest-link", personID, map[string]any{"link": linkID, "room": room, "minutes": minutes})
	}); err != nil {
		return appGuestLinkMade{}, err
	}
	return appGuestLinkMade{URL: origin + "/app-guest#token=" + token, ExpiresAt: expires.UnixMilli()}, nil
}

// openAppGuestSession turns a link's token into a guest session, unless the
// guest's address was blocked from the link.
func openAppGuestSession(app core.App, token, address string, now time.Time) (appGuestSessionWire, error) {
	link := struct {
		ID        string `db:"id"`
		InstallID string `db:"install_id"`
		Tenancy   string `db:"tenancy"`
		Room      string `db:"room"`
		ExpiresAt int64  `db:"expires_at"`
		RevokedAt int64  `db:"revoked_at"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[install_id]], [[tenancy]], [[room]], [[expires_at]], [[revoked_at]] FROM {{leafen_app_guest_links}}
		WHERE [[token_hash]] = {:hash}`).Bind(dbx.Params{"hash": appGuestTokenHash(token)}).One(&link); err != nil ||
		link.RevokedAt != 0 || link.ExpiresAt <= now.UnixMilli() {
		return appGuestSessionWire{}, refusedUnlessStorage(errAppGuestRefused, err)
	}
	install, err := loadAppInstall(app, link.InstallID)
	if err != nil || install.Status != "active" || install.RemovedAt != 0 || !appGuestsEnabled(app, install.ID) {
		return appGuestSessionWire{}, refusedUnlessStorage(errAppGuestRefused, err)
	}
	addressHash := appGuestAddressHash(link.ID, address)
	if appGuestBlocked(app, install.ID, link.ID, addressHash) {
		return appGuestSessionWire{}, errAppGuestBlocked
	}
	_, manifest, err := loadAppPackage(app, install.PackageHash)
	if err != nil || manifest.Guests == nil {
		return appGuestSessionWire{}, refusedUnlessStorage(errAppGuestRefused, err)
	}
	expires := now.Add(appGuestSessionSeconds * time.Second)
	if linkEnds := time.UnixMilli(link.ExpiresAt); linkEnds.Before(expires) {
		expires = linkEnds
	}
	guestToken := "lgs_" + security.RandomString(40)
	appGuests.mu.Lock()
	for key, session := range appGuests.sessions {
		if !session.Expires.After(now) {
			delete(appGuests.sessions, key)
		}
	}
	if len(appGuests.sessions) >= appGuestSessionsMax {
		appGuests.mu.Unlock()
		return appGuestSessionWire{}, errAppRelayLimited
	}
	appGuests.sessions[guestToken] = appGuestSession{LinkID: link.ID, InstallID: install.ID, Tenancy: link.Tenancy, Room: link.Room, Expires: expires,
		AddressHash: addressHash}
	appGuests.mu.Unlock()
	ticket, err := appFrameTicketFor(app, install.ID, install.PackageHash, expires.Unix(), appStrictFor(app, link.Tenancy))
	if err != nil {
		return appGuestSessionWire{}, err
	}
	entry := "ui/index.html"
	if manifest.Surfaces.FullPage != nil {
		entry = manifest.Surfaces.FullPage.Entry
	}
	return appGuestSessionWire{Name: manifest.Name, Slug: install.Slug, Version: manifest.Version, Developer: appDeveloper(manifest),
		Builtin:  appInstallBuiltin(app, install),
		FrameURL: appFramePrefix + ticket + "/" + entry, GuestToken: guestToken, Room: link.Room, ExpiresAt: expires.UnixMilli()}, nil
}

func appGuestFromRequest(request *http.Request) (appGuestSession, string, bool) {
	token := request.Header.Get(appGuestHeader)
	appGuests.mu.Lock()
	defer appGuests.mu.Unlock()
	session, ok := appGuests.sessions[token]
	if !ok || !session.Expires.After(time.Now()) {
		return appGuestSession{}, "", false
	}
	return session, token, true
}

// endAppGuests ends the guest sessions of a link, or of every link of an app.
func endAppGuests(installID, linkID string) {
	appGuests.mu.Lock()
	defer appGuests.mu.Unlock()
	for token, session := range appGuests.sessions {
		if session.InstallID == installID && (linkID == "" || session.LinkID == linkID) {
			delete(appGuests.sessions, token)
		}
	}
}

// loadAppGuests is the app's guest settings with the first window of its open
// links and of the reports about its guests.
func loadAppGuests(app core.App, install storedAppInstall, manifest appManifest) (appGuestsWire, error) {
	guests := appGuestsWire{Declared: manifest.Guests != nil, Enabled: appGuestsEnabled(app, install.ID), Rooms: []string{}}
	if manifest.Guests != nil {
		guests.Rooms, guests.MaxPerRoom, guests.RoomNames = manifest.Guests.Rooms, manifest.Guests.MaxPerRoom, manifest.Guests.Names
	}
	var err error
	if guests.Links, guests.LinksNextCursor, err = loadAppGuestLinks(app, install.ID, timeIDCursor{}); err != nil {
		return guests, err
	}
	guests.Reports, guests.ReportsNextCursor, err = loadAppGuestReports(app, install.ID, timeIDCursor{})
	return guests, err
}

// loadAppGuestLinks is a window of the app's links that are still open — not
// run out or revoked — newest first, from after on.
func loadAppGuestLinks(app core.App, installID string, after timeIDCursor) ([]appGuestLinkWire, string, error) {
	rows := []struct {
		ID        string `db:"id"`
		Room      string `db:"room"`
		CreatedBy string `db:"name"`
		Created   int64  `db:"created"`
		ExpiresAt int64  `db:"expires_at"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[link]].[[id]], [[link]].[[room]], COALESCE([[user]].[[name]], '') AS [[name]], [[link]].[[created]],
			[[link]].[[expires_at]]
		FROM {{leafen_app_guest_links}} [[link]] LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[link]].[[created_by]]
		WHERE [[link]].[[install_id]] = {:install} AND [[link]].[[expires_at]] > {:now} AND [[link]].[[revoked_at]] = 0
			AND ({:afterId} = '' OR [[link]].[[created]] < {:afterAt} OR ([[link]].[[created]] = {:afterAt} AND [[link]].[[id]] > {:afterId}))
		ORDER BY [[link]].[[created]] DESC, [[link]].[[id]] LIMIT {:limit}`).
		Bind(dbx.Params{"install": installID, "now": time.Now().UnixMilli(), "afterAt": after.At, "afterId": after.ID,
			"limit": appGuestLinksPerWindow + 1}).All(&rows); err != nil {
		return nil, "", err
	}
	links := make([]appGuestLinkWire, 0, len(rows))
	for _, row := range rows {
		links = append(links, appGuestLinkWire{ID: row.ID, Room: row.Room, CreatedBy: row.CreatedBy, Created: row.Created, ExpiresAt: row.ExpiresAt})
	}
	links, next := appWindowOf(links, appGuestLinksPerWindow, func(link appGuestLinkWire) (int64, string) { return link.Created, link.ID })
	return links, next, nil
}

func registerAppGuestRoutes(event *core.ServeEvent) {
	appStopCleanups = append(appStopCleanups, func(installID string) { endAppGuests(installID, "") })
	appRemovalCleanups = append(appRemovalCleanups, func(installID string) { endAppGuests(installID, "") })
	limit := apis.BodyLimit(appPlatformInputMaxBytes)
	event.Router.GET(apiRoot+appPlatformGuestsReadPath, guestsAdminRoute(false))
	event.Router.PUT(apiRoot+appPlatformGuestsSetPath, guestsAdminRoute(true)).Bind(limit)
	event.Router.POST(apiRoot+appPlatformGuestLinkRevokePath, revokeAppGuestLinkRoute).Bind(limit)
	event.Router.POST(apiRoot+appPlatformGuestLinkMakePath, makeAppGuestLinkRoute).Bind(limit)
	event.Router.POST(apiRoot+appPlatformGuestReportPath, reportAppGuestRoute).Bind(limit)
	event.Router.POST(apiRoot+appPlatformGuestSessionPath, openAppGuestSessionRoute).Bind(apis.BodyLimit(4096))
	event.Router.POST(apiRoot+appPlatformGuestRoomTicketPath, guestRoomTicketRoute).Bind(limit)
	event.Router.POST(apiRoot+appPlatformGuestKVPath, guestKVRoute).Bind(limit)
	registerAppAdminWindowRoutes(event)
}

func guestsAdminRoute(change bool) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		if !authorizeAdministratorRequest(event) {
			return nil
		}
		user, _ := authenticatedUser(event)
		install, manifest, err := adminAppInstall(event)
		if err != nil {
			return appPlatformFailure(event, err)
		}
		if change {
			input := appGuestsInput{}
			if manifest.Guests == nil {
				return appPlatformFailure(event, refuse(errAppInputInvalid, "the app takes no guests"))
			}
			if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
				return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "guests choice", err))
			}
			// A key's call asks its person once the app has guests
			// (user_key_acting_ask.go).
			if asked, err := askBeforeActing(event); asked {
				return err
			}
			enabled := 0
			if input.Enabled {
				enabled = 1
			}
			if err := event.App.RunInTransaction(func(tx core.App) error {
				if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_guest_settings}} ([[install_id]], [[enabled]], [[updated_by]], [[updated_at]])
					VALUES ({:install}, {:enabled}, {:actor}, {:now}) ON CONFLICT ([[install_id]]) DO UPDATE SET [[enabled]] = excluded.[[enabled]],
					[[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]`).Bind(dbx.Params{
					"install": install.ID, "enabled": enabled, "actor": user.Id, "now": time.Now().UnixMilli(),
				}).Execute(); err != nil {
					return err
				}
				return recordAppAudit(tx, install, "guests", user.Id, map[string]any{"enabled": input.Enabled})
			}); err != nil {
				return appPlatformFailure(event, err)
			}
			if !input.Enabled {
				endAppGuests(install.ID, "")
				appRoomRegistry.closeGuests(install.ID, "")
			}
		}
		guests, err := loadAppGuests(event.App, install, manifest)
		if err != nil {
			return appPlatformFailure(event, err)
		}
		return event.JSON(http.StatusOK, guests)
	}
}

func revokeAppGuestLinkRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	linkID := event.Request.PathValue("linkId")
	revoked := false
	err = event.App.RunInTransaction(func(tx core.App) error {
		result, err := tx.DB().NewQuery(`UPDATE {{leafen_app_guest_links}} SET [[revoked_at]] = {:now}
			WHERE [[id]] = {:id} AND [[install_id]] = {:install} AND [[revoked_at]] = 0`).
			Bind(dbx.Params{"now": time.Now().UnixMilli(), "id": linkID, "install": install.ID}).Execute()
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return nil
		}
		revoked = true
		return recordAppAudit(tx, install, "guest-link-revoke", user.Id, map[string]any{"link": linkID})
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if revoked {
		endAppGuests(install.ID, linkID)
		appRoomRegistry.closeGuests(install.ID, linkID)
	}
	guests, err := loadAppGuests(event.App, install, manifest)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, guests)
}

func makeAppGuestLinkRoute(event *core.RequestEvent) error {
	install, manifest, personID, ok, failure := appTokenInstall(event)
	if !ok {
		return failure
	}
	authority, _ := userKeyOf(event)
	input := appGuestLinkInput{}
	if err := json.NewDecoder(io.LimitReader(event.Request.Body, appPlatformInputMaxBytes)).Decode(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppGuestRefused,
			refusalMessage("Name a room and how many minutes the link lasts.", bodyRefusalReason("guest link request", err)))
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	made, err := makeAppGuestLink(event.App, install, manifest, authority.TenancyKey, personID, input.Room, input.Minutes, origin)
	if errors.Is(err, errAppGuestRefused) {
		return apiError(event, http.StatusForbidden, errorCodeAppGuestRefused,
			refusalMessage("The guest link can't be made.", refusalReason(err)))
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, made)
}

func openAppGuestSessionRoute(event *core.RequestEvent) error {
	release, admitted := appGuests.opening.admit("guest-open", event.RealIP(), time.Now())
	if !admitted {
		return apiError(event, http.StatusTooManyRequests, errorCodeAppRelayLimited, "Too many tries; wait a minute.")
	}
	release()
	input := appGuestSessionInput{}
	if err := json.NewDecoder(io.LimitReader(event.Request.Body, 4096)).Decode(&input); err != nil || !strings.HasPrefix(input.Token, "lgl_") {
		return apiError(event, http.StatusNotFound, errorCodeAppGuestRefused, "This link isn't valid.")
	}
	session, err := openAppGuestSession(event.App, input.Token, event.RealIP(), time.Now())
	if errors.Is(err, errAppGuestBlocked) {
		return apiError(event, http.StatusForbidden, errorCodeAppGuestRefused, "You can't join by this link.")
	}
	if errors.Is(err, errAppGuestRefused) || errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeAppGuestRefused, "This link ran out, was revoked or the app isn't taking guests.")
	}
	if errors.Is(err, errAppRelayLimited) {
		return apiError(event, http.StatusServiceUnavailable, errorCodeAppRelayLimited, "The server has too many guests right now; try again later.")
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, session)
}

func guestRoomTicketRoute(event *core.RequestEvent) error {
	guest, guestToken, ok := appGuestFromRequest(event.Request)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeAppGuestRefused, "The guest session ended.")
	}
	install, err := loadAppInstall(event.App, guest.InstallID)
	if err != nil || install.Status != "active" {
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
	}
	// The room's socket is at the address the page came in on (framingOrigins).
	origin, err := requestArrivalOrigin(event.Request)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if _, manifest, err := loadAppPackage(event.App, install.PackageHash); err == nil && manifest.Realtime != nil {
		if code, reason := appRoomRegistry.refusal(appRoomName(install.ID, guest.Tenancy, guest.Room), install.ID, manifest, true); code != "" {
			return apiError(event, http.StatusServiceUnavailable, code, reason)
		}
	}
	ticket, expires := appRoomRegistry.guestTicket(install, guest, guestToken, time.Now())
	socket := strings.Replace(strings.Replace(origin, "https://", "wss://", 1), "http://", "ws://", 1)
	return event.JSON(http.StatusOK, appRoomTicket{ServerURL: socket + apiRoot + appRoomsPath,
		RoomName: appRoomName(install.ID, guest.Tenancy, guest.Room), Ticket: ticket, ExpiresAt: expires.UnixMilli()})
}

func guestKVRoute(event *core.RequestEvent) error {
	guest, guestToken, ok := appGuestFromRequest(event.Request)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeAppGuestRefused, "The guest session ended.")
	}
	install, err := loadAppInstall(event.App, guest.InstallID)
	if err != nil || install.Status != "active" {
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
	}
	_, manifest, err := loadAppPackage(event.App, install.PackageHash)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	request := appKVRequest{}
	if err := json.NewDecoder(io.LimitReader(event.Request.Body, appPlatformInputMaxBytes)).Decode(&request); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppKVRefused, refusalMessage("Send a store command.", bodyRefusalReason("store command", err)))
	}
	result, err := runAppKV(manifest, install.ID, guest.Tenancy, "guest:"+appGuestTokenHash(guestToken)[:16], request, time.Now())
	switch {
	case err == nil:
		return event.JSON(http.StatusOK, appKVResult{Result: result})
	case errors.Is(err, errAppKVFull):
		return apiError(event, http.StatusInsufficientStorage, errorCodeAppKVFull, "The app's store is full.")
	case errors.Is(err, errAppRelayLimited):
		return apiError(event, http.StatusTooManyRequests, errorCodeAppRelayLimited, "Too many store commands; try again in a minute.")
	case errors.Is(err, errAppKVRefused):
		return apiError(event, http.StatusBadRequest, errorCodeAppKVRefused, appKVRefusalMessage(err))
	}
	return appPlatformFailure(event, err)
}
