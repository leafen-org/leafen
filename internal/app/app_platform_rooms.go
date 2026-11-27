//go:build !leafen_noapps

package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"
	"golang.org/x/time/rate"
)

// Apps' realtime rooms (docs/app-platform.md 9): a shared Y.Doc, who is
// there and stateless signals, on a ygo server of their own that keeps
// nothing — a room is gone once it empties. An app's screen asks for a
// ticket bound to the app, the organization, the room and the person, and
// joins with it; the frame has no origin, so the ticket, never a cookie,
// lets it in, and the request log never keeps it. A room holds at most the
// people the app declared, closes when the app stops, and passes messages
// addressed to the server to the app's realtime worker, whose answer goes
// back to the whole room. A guest's socket (9, public guests) also counts
// against the app's guests per room and is cut alone when its session runs
// out or its link is revoked, while the people in the room stay.

const appRoomsPath = "/app-rooms"

// appRoomsOpenMax is how many rooms an app may have open at once.
func appRoomsOpenMax(realtime *appManifestRealtime) int {
	if realtime.MaxRooms > 0 {
		return realtime.MaxRooms
	}
	return appRoomsPerAppDefault
}

type appRoomGrant struct {
	InstallID, Tenancy, Room, PersonID string
	Expires                            time.Time
	// GuestLink is the link a guest came by; SessionEnds is when the guest's
	// session runs out.
	GuestLink   string
	SessionEnds time.Time
}

type appRooms struct {
	mu      sync.Mutex
	server  *ygowebsocket.Server
	tickets map[string]appRoomGrant
	peers   map[string]int
	guests  map[string]int
	owners  map[string]appRoomGrant
	sockets map[*appRoomSocket]appRoomGrant
}

// appRoomSocket keeps the connection a guest's socket took over so it can be
// cut on its own.
type appRoomSocket struct {
	http.ResponseWriter
	mu   sync.Mutex
	conn net.Conn
	cut  bool
}

func (socket *appRoomSocket) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(socket.ResponseWriter).Hijack()
	if err != nil {
		return conn, buffer, err
	}
	socket.mu.Lock()
	defer socket.mu.Unlock()
	socket.conn = conn
	if socket.cut {
		_ = conn.Close()
	}
	return conn, buffer, nil
}

func (socket *appRoomSocket) close() {
	socket.mu.Lock()
	defer socket.mu.Unlock()
	socket.cut = true
	if socket.conn != nil {
		_ = socket.conn.Close()
	}
}

var appRoomRegistry *appRooms

// appRoomOrigins are where an app room's socket may come from beside the
// server's own page: the app's frame, which has no origin ("null"), and the
// packaged client's page.
var appRoomOrigins = append([]string{"null"}, packagedClientOrigins...)

func newAppRooms() *appRooms {
	server := ygowebsocket.NewServer()
	server.MaxRooms = 2000
	server.MaxPeersPerRoom = 200
	server.MaxMessageBytes = 256 << 10
	server.MessageRateLimit = rate.Limit(50)
	server.MessageRateBurst = 100
	server.MaxAwarenessClientsPerRoom = 400
	server.MaxAwarenessBytesPerRoom = 4 << 20
	server.AwarenessExpiry = 30 * time.Second
	// Where a socket may come from is asked as it comes in (serve).
	server.AllowedOrigins = []string{"*"}
	rooms := &appRooms{server: server, tickets: map[string]appRoomGrant{}, peers: map[string]int{}, guests: map[string]int{},
		owners: map[string]appRoomGrant{}, sockets: map[*appRoomSocket]appRoomGrant{}}
	server.Authorize = func(request *http.Request) (ygowebsocket.ConnectionConfig, bool) {
		sender, ok := request.Context().Value(appRoomGrantKey{}).(appRoomSender)
		return ygowebsocket.ConnectionConfig{Identity: sender}, ok
	}
	return rooms
}

type appRoomGrantKey struct{}

// appRoomSender is who sent a room's message, as the server knows them:
// the person whose ticket the socket came with, or the guest's session.
type appRoomSender struct {
	Person string `json:"person,omitempty"`
	Guest  string `json:"guest,omitempty"`
	Name   string `json:"name"`
}

// appRoomMessage is a message a room sent the server, with its sender.
type appRoomMessage struct {
	text string
	from appRoomSender
}

// appRoomSenderOf names the holder of a room grant.
func appRoomSenderOf(app core.App, grant appRoomGrant) appRoomSender {
	if guest, ok := strings.CutPrefix(grant.PersonID, "guest:"); ok {
		return appRoomSender{Guest: guest}
	}
	sender := appRoomSender{Person: grant.PersonID}
	var name, email string
	_ = app.DB().NewQuery(`SELECT [[name]], [[email]] FROM {{users}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": grant.PersonID}).Row(&name, &email)
	if sender.Name = strings.TrimSpace(name); sender.Name == "" {
		sender.Name = email
	}
	return sender
}

// appRoomName is the room's name on the rooms server: the app, a digest of
// the organization and the app's own name for it.
func appRoomName(installID, tenancy, room string) string {
	digest := sha256.Sum256([]byte(tenancy))
	return "app." + installID + "." + hex.EncodeToString(digest[:8]) + "." + room
}

func (rooms *appRooms) ticket(install storedAppInstall, tenancy, room, personID string, now time.Time) (string, time.Time) {
	return rooms.issue(appRoomGrant{InstallID: install.ID, Tenancy: tenancy, Room: room, PersonID: personID}, now)
}

// guestTicket gives a guest a ticket to its link's room that ends no later
// than its session.
func (rooms *appRooms) guestTicket(install storedAppInstall, guest appGuestSession, guestToken string, now time.Time) (string, time.Time) {
	return rooms.issue(appRoomGrant{InstallID: install.ID, Tenancy: guest.Tenancy, Room: guest.Room,
		PersonID: "guest:" + appGuestTokenHash(guestToken)[:16], GuestLink: guest.LinkID, SessionEnds: guest.Expires}, now)
}

func (rooms *appRooms) issue(grant appRoomGrant, now time.Time) (string, time.Time) {
	rooms.mu.Lock()
	defer rooms.mu.Unlock()
	for key, kept := range rooms.tickets {
		if !kept.Expires.After(now) {
			delete(rooms.tickets, key)
		}
	}
	ticket := "lrt_" + security.RandomString(40)
	grant.Expires = now.Add(appRoomTicketSeconds * time.Second)
	if grant.GuestLink != "" && grant.SessionEnds.Before(grant.Expires) {
		grant.Expires = grant.SessionEnds
	}
	rooms.tickets[ticket] = grant
	return ticket, grant.Expires
}

// serve admits a socket by its ticket into the room it names, within the
// app's declared number of people.
func (rooms *appRooms) serve(app core.App) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		request := event.Request
		// The app's frame has no origin; the ticket in the address is what admits it.
		if !socketOriginAllowed(request, appRoomOrigins) {
			return apiError(event, http.StatusForbidden, errorCodeAppRoomRefused, "The room can't be opened from that page.")
		}
		rooms.mu.Lock()
		grant, ok := rooms.tickets[request.URL.Query().Get("ticket")]
		rooms.mu.Unlock()
		if !ok || !grant.Expires.After(time.Now()) {
			return apiError(event, http.StatusUnauthorized, errorCodeAppRoomRefused, "The room ticket isn't valid.")
		}
		install, err := loadAppInstall(app, grant.InstallID)
		if err != nil || install.Status != "active" || install.RemovedAt != 0 {
			return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
		}
		_, manifest, err := loadAppPackage(app, install.PackageHash)
		if err != nil || manifest.Realtime == nil {
			return apiError(event, http.StatusForbidden, errorCodeAppRoomRefused, "The app has no rooms.")
		}
		name := appRoomName(grant.InstallID, grant.Tenancy, grant.Room)
		if requested := request.PathValue("room"); requested != "" && requested != name {
			return apiError(event, http.StatusForbidden, errorCodeAppRoomRefused, "The ticket is for another room.")
		}
		guest := grant.GuestLink != ""
		if guest && (manifest.Guests == nil || !appGuestsEnabled(app, install.ID)) {
			return apiError(event, http.StatusForbidden, errorCodeAppGuestRefused, "The app isn't taking guests.")
		}
		rooms.mu.Lock()
		if code, reason := rooms.refusalLocked(name, install.ID, manifest, guest); code != "" {
			rooms.mu.Unlock()
			return apiError(event, http.StatusServiceUnavailable, code, reason)
		}
		rooms.peers[name]++
		first := rooms.peers[name] == 1
		socket := &appRoomSocket{ResponseWriter: event.Response}
		if guest {
			rooms.guests[name]++
			rooms.sockets[socket] = grant
		}
		rooms.owners[name] = grant
		rooms.mu.Unlock()
		// The room's loop starts with its first person.
		if first {
			rooms.startAppRoomLoop(app, install, manifest, name, grant)
		}
		if guest {
			ends := time.AfterFunc(time.Until(grant.SessionEnds), socket.close)
			defer ends.Stop()
		}
		defer func() {
			rooms.mu.Lock()
			rooms.peers[name]--
			if guest {
				rooms.guests[name]--
				delete(rooms.sockets, socket)
				if rooms.guests[name] <= 0 {
					delete(rooms.guests, name)
				}
			}
			empty := rooms.peers[name] <= 0
			if empty {
				delete(rooms.peers, name)
				delete(rooms.owners, name)
			}
			rooms.mu.Unlock()
			if empty {
				endAppRoomLoop(name)
			}
		}()
		request.SetPathValue("room", name)
		rooms.server.ServeHTTP(socket, request.WithContext(context.WithValue(request.Context(), appRoomGrantKey{}, appRoomSenderOf(app, grant))))
		return nil
	}
}

// closeApp closes every room an app has open, and says which it couldn't.
func (rooms *appRooms) closeApp(installID string) error {
	rooms.mu.Lock()
	names := []string{}
	for name, grant := range rooms.owners {
		if grant.InstallID == installID {
			names = append(names, name)
		}
	}
	for ticket, grant := range rooms.tickets {
		if grant.InstallID == installID {
			delete(rooms.tickets, ticket)
		}
	}
	rooms.mu.Unlock()
	var failures error
	for _, name := range names {
		if err := rooms.server.CloseRoom(name, true); err != nil && !errors.Is(err, ygowebsocket.ErrRoomNotFound) {
			failures = errors.Join(failures, fmt.Errorf("room %s: %w", name, err))
		}
	}
	if failures != nil {
		// A room the server couldn't close still loses the app's sockets.
		rooms.mu.Lock()
		cut := []*appRoomSocket{}
		for socket, grant := range rooms.sockets {
			if grant.InstallID == installID {
				cut = append(cut, socket)
			}
		}
		rooms.mu.Unlock()
		for _, socket := range cut {
			socket.close()
		}
	}
	return failures
}

// closeGuests cuts the guest sockets and drops the guest tickets of one link,
// or of every link of an app; the people in the rooms stay.
func (rooms *appRooms) closeGuests(installID, linkID string) {
	rooms.mu.Lock()
	cut := []*appRoomSocket{}
	for socket, grant := range rooms.sockets {
		if grant.InstallID == installID && (linkID == "" || grant.GuestLink == linkID) {
			cut = append(cut, socket)
		}
	}
	for ticket, grant := range rooms.tickets {
		if grant.InstallID == installID && grant.GuestLink != "" && (linkID == "" || grant.GuestLink == linkID) {
			delete(rooms.tickets, ticket)
		}
	}
	rooms.mu.Unlock()
	for _, socket := range cut {
		socket.close()
	}
}

// closeGuest cuts one guest's sockets in an app's rooms and drops its
// tickets; everyone else stays.
func (rooms *appRooms) closeGuest(installID, holder string) {
	rooms.mu.Lock()
	cut := []*appRoomSocket{}
	for socket, grant := range rooms.sockets {
		if grant.InstallID == installID && grant.PersonID == holder {
			cut = append(cut, socket)
		}
	}
	for ticket, grant := range rooms.tickets {
		if grant.InstallID == installID && grant.PersonID == holder {
			delete(rooms.tickets, ticket)
		}
	}
	rooms.mu.Unlock()
	for _, socket := range cut {
		socket.close()
	}
}

// answerFromWorker passes a message a room sent the server to the app's
// realtime worker and sends its answer to the whole room.
func (rooms *appRooms) answerFromWorker(app core.App) ygowebsocket.StatelessHook {
	return func(info ygowebsocket.StatelessInfo) {
		if info.IsBroadcast {
			return
		}
		rooms.mu.Lock()
		grant, ok := rooms.owners[info.Room]
		rooms.mu.Unlock()
		sender, _ := info.Identity.(appRoomSender)
		if !ok || queueAppRoomLoop(info.Room, appRoomMessage{text: info.Payload, from: sender}) {
			return
		}
		go runGuarded(app, "app room message", func() {
			install, err := loadAppInstall(app, grant.InstallID)
			if err != nil || install.Status != "active" {
				return
			}
			_, manifest, err := loadAppPackage(app, install.PackageHash)
			// A loop app's messages go only to its room's loop.
			if err != nil || manifest.Realtime == nil || manifest.Realtime.Worker == "" || manifest.Realtime.Loop {
				return
			}
			input, _ := json.Marshal(map[string]any{"room": grant.Room, "tenancy": grant.Tenancy, "message": info.Payload, "from": sender})
			output, _, err := runAppWorkerFunction(context.Background(), app, install, manifest, appRun{Trigger: "realtime", Ref: grant.Room,
				Worker: manifest.Realtime.Worker, Function: manifest.Realtime.Function, Input: input, Tenancy: grant.Tenancy})
			if err != nil || len(output) == 0 {
				return
			}
			_ = rooms.server.BroadcastStateless(info.Room, string(output))
		})
	}
}

// refusalLocked says why one more person can't join a room now, or nothing
// when they can: a room seats as many as the app's manifest says and takes as
// many guests, and the app keeps as many rooms open as it may. The ticket
// routes ask it too, so the screen hears the reason the socket can't carry.
func (rooms *appRooms) refusalLocked(name, installID string, manifest appManifest, guest bool) (string, string) {
	if rooms.peers[name] >= manifest.Realtime.MaxParticipants {
		return errorCodeAppRoomFull, fmt.Sprintf("The room is full: it seats %d.", manifest.Realtime.MaxParticipants)
	}
	if guest && manifest.Guests != nil && rooms.guests[name] >= manifest.Guests.MaxPerRoom {
		return errorCodeAppRoomFull, fmt.Sprintf("The room takes no more guests: it takes %d.", manifest.Guests.MaxPerRoom)
	}
	if rooms.peers[name] == 0 {
		open := 0
		for _, owner := range rooms.owners {
			if owner.InstallID == installID {
				open++
			}
		}
		if open >= appRoomsOpenMax(manifest.Realtime) {
			return errorCodeAppRoomRefused, "The app has as many rooms open as it may."
		}
	}
	return "", ""
}

// refusal is refusalLocked for a ticket route.
func (rooms *appRooms) refusal(name, installID string, manifest appManifest, guest bool) (string, string) {
	rooms.mu.Lock()
	defer rooms.mu.Unlock()
	return rooms.refusalLocked(name, installID, manifest, guest)
}

func registerAppRoomRoutes(event *core.ServeEvent) {
	appRoomRegistry = newAppRooms()
	appRoomRegistry.server.OnStateless = appRoomRegistry.answerFromWorker(event.App)
	closeApp := func(installID string) {
		if err := appRoomRegistry.closeApp(installID); err != nil {
			event.App.Logger().Warn("leafen app rooms not closed; their sockets were cut", "install", installID, "error", err)
		}
	}
	appStopCleanups = append(appStopCleanups, closeApp)
	appRemovalCleanups = append(appRemovalCleanups, closeApp)
	event.Router.GET(apiRoot+appRoomsPath+"/{room}", appRoomRegistry.serve(event.App))
	event.Router.POST(apiRoot+appPlatformRoomTicketPath, roomTicketRoute)
}

// roomTicketRoute gives the app's own token a ticket to one of its rooms in
// the organization the token was opened for.
func roomTicketRoute(event *core.RequestEvent) error {
	install, manifest, personID, ok, failure := appTokenInstall(event)
	if !ok {
		return failure
	}
	authority, _ := userKeyOf(event)
	room := event.Request.PathValue("room")
	if manifest.Realtime == nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppRoomRefused, "The app has no rooms: its manifest declares no realtime.")
	}
	if !appKeyPattern.MatchString(room) {
		return apiError(event, http.StatusBadRequest, errorCodeAppRoomRefused,
			fmt.Sprintf("The room's name is a key: a lowercase letter, then up to 47 letters, digits, - or _, not %q.", room))
	}
	// The room's socket is at the address the page came in on (framingOrigins).
	origin, err := requestArrivalOrigin(event.Request)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if code, reason := appRoomRegistry.refusal(appRoomName(install.ID, authority.TenancyKey, room), install.ID, manifest, false); code != "" {
		return apiError(event, http.StatusServiceUnavailable, code, reason)
	}
	ticket, expires := appRoomRegistry.ticket(install, authority.TenancyKey, room, personID, time.Now())
	socket := strings.Replace(strings.Replace(origin, "https://", "wss://", 1), "http://", "ws://", 1)
	return event.JSON(http.StatusOK, appRoomTicket{ServerURL: socket + apiRoot + appRoomsPath,
		RoomName: appRoomName(install.ID, authority.TenancyKey, room), Ticket: ticket, ExpiresAt: expires.UnixMilli()})
}
