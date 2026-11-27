package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"
)

const errorCodeYgoRoomInvalidationFailed = "LEAFEN_YGO_ROOM_INVALIDATION_FAILED"

type workspaceRoomInvalidator interface {
	InvalidateWorkspace(workspaceID string) error
}

type ygoRoomAuthorization struct {
	app        core.App
	server     *ygowebsocket.Server
	settlement *ygoPersistenceSettlementOwner

	mu                     sync.Mutex
	changed                chan struct{}
	workspaceDrains        map[string]*ygoWorkspaceDrain
	roomBindings           map[string]ygoRoomBinding
	roomRequestCounts      map[string]int
	workspaceRequestCounts map[string]int
	requestBindings        map[*http.Request]ygoRequestBinding
	activeRooms            map[string]map[string]struct{}
	personSockets          map[string]int
	refusals               collaborationRefusalLog
	recheck                *ygoAccessRecheck
	// dlpPolicies are the DLP policies read for workspaces, by workspace
	// (ygo_dlp_write_check.go).
	dlpPolicies sync.Map
}

func (a *ygoRoomAuthorization) bindPersistenceSettlement(owner *ygoPersistenceSettlementOwner) {
	a.settlement = owner
}

type ygoRoomBinding struct {
	workspaceID string
}

type ygoRequestBinding struct {
	workspaceID string
	roomID      string
}

type ygoWorkspaceDrain struct {
	done chan struct{}
	err  error
}

func newYgoRoomAuthorization(app core.App, server *ygowebsocket.Server) *ygoRoomAuthorization {
	return &ygoRoomAuthorization{
		app:                    app,
		server:                 server,
		changed:                make(chan struct{}),
		workspaceDrains:        map[string]*ygoWorkspaceDrain{},
		roomBindings:           map[string]ygoRoomBinding{},
		roomRequestCounts:      map[string]int{},
		workspaceRequestCounts: map[string]int{},
		requestBindings:        map[*http.Request]ygoRequestBinding{},
		activeRooms:            map[string]map[string]struct{}{},
		personSockets:          map[string]int{},
		recheck:                newYgoAccessRecheck(),
	}
}

func (a *ygoRoomAuthorization) Authorize(request *http.Request) (ygowebsocket.ConnectionConfig, bool) {
	token := bearerToken(request)
	if token == "" {
		token = request.URL.Query().Get("token")
	}
	user, connection, err := a.socketPerson(token)
	if err != nil {
		a.refusals.note(a.app, request, "", http.StatusUnauthorized, errorCodeUnauthorized, "session")
		return ygowebsocket.ConnectionConfig{}, false
	}
	// The socket carries its token in the query, outside the HTTP session
	// middleware, so it admits the token through the same open-session read.
	room := strings.TrimSpace(request.PathValue("room"))
	// The revision is read before the access, so a change committed between
	// the two is checked again at the socket's first write.
	revision, err := currentAccessRevision(a.app)
	if err != nil {
		a.refusals.note(a.app, request, user.Id, http.StatusInternalServerError, errorCodeUnauthorized, "access_revision")
		return ygowebsocket.ConnectionConfig{}, false
	}
	access, ok := a.evaluateRoomAccess(user.Id, token, room)
	if !ok {
		a.refusals.note(a.app, request, user.Id, http.StatusUnauthorized, errorCodePermissionDenied, "room_access")
		return ygowebsocket.ConnectionConfig{}, false
	}
	if !a.trackAuthorizedRequest(request, room, access.WorkspaceID) {
		a.refusals.note(a.app, request, user.Id, http.StatusUnauthorized, errorCodePermissionDenied, "room_closing")
		return ygowebsocket.ConnectionConfig{}, false
	}
	a.admitSocket(request, token, room, access, revision)
	// Opening a page or an item is a read for the read log (read_log.go).
	kind := "page"
	if access.RowPage {
		kind = "row"
	}
	via := contentReadViaApp
	if connection.ID != "" {
		via = contentReadViaAPI
	}
	recordContentReads(a.app, access.WorkspaceID, contentReader{UserID: user.Id, Via: via},
		[]contentRead{{Kind: kind, ID: room, How: contentReadOpen}})
	// Opening a page themselves reads the discussions they have now been in (comment_discussions_seen.go).
	if connection.ID == "" && !strings.HasPrefix(access.SessionID, "key:") {
		noteDiscussionsSeenAfterOpen(a.app, user.Id, access.WorkspaceID, room, access.RowPage)
	}
	// Someone a lock or the trash holds off editing is still among the people
	// on the page, though it refuses their writes; someone who may only read
	// isn't, as in Notion (seen 2026-11-05): the others learn only that someone
	// is here (collaboration_reader_presence.go).
	// The document reaches them as they may read it (collaboration_reader_mentions.go).
	config := ygowebsocket.ConnectionConfig{
		ReadOnly:            access.ReadOnly,
		ReadOnlyPresence:    access.ReadOnly,
		PersistenceMetadata: ygoPersistenceActor{ActorID: user.Id, Connection: connection},
		OutboundUpdate:      collaborationReaderUpdates(a.app, user.Id),
	}
	if access.ViewOnly {
		config.AwarenessState = readerPresenceState
	}
	if !access.ReadOnly {
		config.WriteGate = a.writeGate(request)
	}
	return config, true
}

func (a *ygoRoomAuthorization) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	// The packaged client's own page opens rooms as the server's own does.
	if !socketOriginAllowed(request, packagedClientOrigins) {
		a.refusals.note(a.app, request, "", http.StatusForbidden, errorCodePermissionDenied, "origin")
		http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	releasePerson, person, admitted := a.admitPersonSocket(request)
	if !admitted {
		a.refusals.note(a.app, request, person, http.StatusTooManyRequests, errorCodeRateLimited, "person_limit")
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Retry-After", collaborationSocketRetryAfter)
		response.WriteHeader(http.StatusTooManyRequests)
		_, _ = response.Write([]byte(`{"error":{"code":"` + errorCodeRateLimited + `","message":"Too many live page connections for one person. Close some tabs, or try again shortly."}}`))
		return
	}
	defer releasePerson()
	request, closeSocket := a.openSocket(request)
	defer closeSocket()
	defer a.finishRequest(request)
	a.server.ServeHTTP(response, request)
}

// collaborationSocketsPerPerson is how many live page connections one person
// may hold at once: each open page or item holds one, so three devices with
// ten tabs of two or three rooms each stay well inside it, while one person
// can't hold the server's connections, goroutines and buffers by opening
// sockets without end (2026-10-18). Over it, a new socket is refused and
// those already open go on.
const (
	collaborationSocketsPerPerson = 100
	collaborationSocketRetryAfter = "30"
)

// admitPersonSocket counts a socket against its person until it closes. A
// socket whose person can't be read goes on, to be refused by Authorize.
func (a *ygoRoomAuthorization) admitPersonSocket(request *http.Request) (func(), string, bool) {
	token := bearerToken(request)
	if token == "" {
		token = request.URL.Query().Get("token")
	}
	user, _, err := a.socketPerson(token)
	if err != nil || user == nil {
		return func() {}, "", true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.personSockets[user.Id] >= collaborationSocketsPerPerson {
		return nil, user.Id, false
	}
	a.personSockets[user.Id]++
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.personSockets[user.Id]--; a.personSockets[user.Id] <= 0 {
			delete(a.personSockets, user.Id)
		}
	}, user.Id, true
}

func (a *ygoRoomAuthorization) trackAuthorizedRequest(
	request *http.Request,
	roomID string,
	workspaceID string,
) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.trackAuthorizedRoomLocked(roomID, workspaceID) {
		return false
	}
	if _, exists := a.requestBindings[request]; exists {
		return false
	}
	a.requestBindings[request] = ygoRequestBinding{workspaceID: workspaceID, roomID: roomID}
	a.roomRequestCounts[roomID]++
	a.workspaceRequestCounts[workspaceID]++
	a.signalChangedLocked()
	return true
}

func (a *ygoRoomAuthorization) trackAuthorizedRoom(roomID string, workspaceID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.trackAuthorizedRoomLocked(roomID, workspaceID)
}

func (a *ygoRoomAuthorization) trackAuthorizedRoomLocked(roomID string, workspaceID string) bool {
	if _, draining := a.workspaceDrains[workspaceID]; draining {
		return false
	}
	if binding, exists := a.roomBindings[roomID]; exists && binding.workspaceID != workspaceID {
		return false
	}
	a.roomBindings[roomID] = ygoRoomBinding{workspaceID: workspaceID}
	return true
}

func (a *ygoRoomAuthorization) finishRequest(request *http.Request) {
	a.mu.Lock()
	binding, exists := a.requestBindings[request]
	if !exists {
		a.mu.Unlock()
		return
	}
	delete(a.requestBindings, request)
	a.roomRequestCounts[binding.roomID]--
	if a.roomRequestCounts[binding.roomID] == 0 {
		delete(a.roomRequestCounts, binding.roomID)
		a.removeActiveRoomLocked(binding.workspaceID, binding.roomID)
		delete(a.roomBindings, binding.roomID)
	}
	a.workspaceRequestCounts[binding.workspaceID]--
	if a.workspaceRequestCounts[binding.workspaceID] == 0 {
		delete(a.workspaceRequestCounts, binding.workspaceID)
	}
	a.signalChangedLocked()
	a.mu.Unlock()
}

func (a *ygoRoomAuthorization) OnFirstPeer(_ context.Context, roomID string) {
	a.mu.Lock()
	binding, exists := a.roomBindings[roomID]
	if exists {
		rooms := a.activeRooms[binding.workspaceID]
		if rooms == nil {
			rooms = map[string]struct{}{}
			a.activeRooms[binding.workspaceID] = rooms
		}
		rooms[roomID] = struct{}{}
		a.signalChangedLocked()
	}
	a.mu.Unlock()
	// Before the room sends its peer the states it holds, so the peer hears the
	// position with them (ygo_room_publication.go).
	if exists {
		announceGraphCursorOnRoomOpen(a.app, binding.workspaceID, roomID)
	}
}

func (a *ygoRoomAuthorization) OnLastPeer(_ context.Context, roomID string) {
	a.mu.Lock()
	binding, exists := a.roomBindings[roomID]
	if exists && a.roomRequestCounts[roomID] == 0 {
		a.removeActiveRoomLocked(binding.workspaceID, roomID)
		delete(a.roomBindings, roomID)
	}
	a.signalChangedLocked()
	a.mu.Unlock()
}

func (a *ygoRoomAuthorization) InvalidateWorkspace(workspaceID string) error {
	a.mu.Lock()
	if drain, exists := a.workspaceDrains[workspaceID]; exists {
		a.mu.Unlock()
		<-drain.done
		return drain.err
	}
	drain := &ygoWorkspaceDrain{done: make(chan struct{})}
	a.workspaceDrains[workspaceID] = drain
	a.signalChangedLocked()
	a.mu.Unlock()

	err := a.drainWorkspace(workspaceID)
	if err != nil {
		// Callers may go on: the rooms' sockets were cut, and a socket whose
		// access was lowered is closed by the access recheck in any case.
		a.app.Logger().Warn("leafen collaboration rooms not closed; their sockets were cut", "workspace", workspaceID, "error", err)
	}
	a.mu.Lock()
	drain.err = err
	delete(a.workspaceDrains, workspaceID)
	close(drain.done)
	a.signalChangedLocked()
	a.mu.Unlock()
	return err
}

// drainWorkspace closes each open room of the workspace; a room the server
// can't close has its sockets cut, and doesn't keep the others open
// (2026-10-13: the first failure left the rest).
func (a *ygoRoomAuthorization) drainWorkspace(workspaceID string) error {
	var failures error
	// Requests still being let into a room are waited for only so long
	// (2026-10-17: a request that never finished held a member removal open);
	// past that the drain ends with a failure, as a room that couldn't close
	// does, and the access recheck closes what joins late.
	deadline := time.Now().Add(ygoWorkspaceDrainLimit)
	for {
		a.mu.Lock()
		roomID, hasRoom := a.nextActiveRoomLocked(workspaceID)
		if !hasRoom && a.workspaceRequestCounts[workspaceID] == 0 {
			a.mu.Unlock()
			if failures != nil {
				return fmt.Errorf("%s: %w", errorCodeYgoRoomInvalidationFailed, failures)
			}
			return nil
		}
		changed := a.changed
		a.mu.Unlock()

		if !hasRoom {
			select {
			case <-changed:
			case <-time.After(time.Until(deadline)):
				failures = errors.Join(failures, fmt.Errorf("room requests still open after %s", ygoWorkspaceDrainLimit))
				return fmt.Errorf("%s: %w", errorCodeYgoRoomInvalidationFailed, failures)
			}
			continue
		}
		if err := a.server.CloseRoom(roomID, true); err != nil && !errors.Is(err, ygowebsocket.ErrRoomNotFound) {
			failures = errors.Join(failures, fmt.Errorf("room %s: %w", roomID, err))
			a.closeRoomSockets(roomID)
		}
		a.mu.Lock()
		a.removeActiveRoomLocked(workspaceID, roomID)
		if a.roomRequestCounts[roomID] == 0 {
			delete(a.roomBindings, roomID)
		}
		a.signalChangedLocked()
		a.mu.Unlock()
	}
}

var ygoWorkspaceDrainLimit = 10 * time.Second

// closeRoomSockets cuts the sockets of a room the server couldn't close, as
// the access recheck cuts one; their editors reconnect under their access.
func (a *ygoRoomAuthorization) closeRoomSockets(roomID string) {
	a.mu.Lock()
	requests := []*http.Request{}
	for request, binding := range a.requestBindings {
		if binding.roomID == roomID {
			requests = append(requests, request)
		}
	}
	a.mu.Unlock()
	a.recheck.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(requests))
	for _, request := range requests {
		if socket := a.recheck.sockets[request]; socket != nil {
			cancels = append(cancels, socket.cancel)
		}
	}
	a.recheck.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (a *ygoRoomAuthorization) nextActiveRoomLocked(workspaceID string) (string, bool) {
	for roomID := range a.activeRooms[workspaceID] {
		return roomID, true
	}
	return "", false
}

func (a *ygoRoomAuthorization) removeActiveRoomLocked(workspaceID string, roomID string) {
	rooms := a.activeRooms[workspaceID]
	delete(rooms, roomID)
	if len(rooms) == 0 {
		delete(a.activeRooms, workspaceID)
	}
}

func (a *ygoRoomAuthorization) signalChangedLocked() {
	close(a.changed)
	a.changed = make(chan struct{})
}

// socketPerson is who a socket's token signs in, and the connection a key's
// socket writes through.
func (a *ygoRoomAuthorization) socketPerson(token string) (*core.Record, pageHistoryConnection, error) {
	if !strings.HasPrefix(token, userKeyPrefix) {
		user, err := a.app.FindAuthRecordByToken(token, core.TokenTypeAuth)
		return user, pageHistoryConnection{}, err
	}
	key, found, err := findUserKey(a.app, token)
	if err == nil && !found {
		err = errAuthSessionInvalid
	}
	if err != nil {
		return nil, pageHistoryConnection{}, err
	}
	user, err := a.app.FindRecordById("users", key.OwnerID)
	return user, pageHistoryConnection{Name: pageHistoryConnectionLabel(key.Name, "API"), ID: "mcp:" + key.ConnectionID}, err
}
