package app

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// An open collaboration socket keeps the access it was admitted with only
// while that access holds. Each socket is checked again by the same rules
// that admitted it: when a session is revoked, when a page moves to or out
// of the trash, as soon as the access revision shows a permission, a
// membership or a page's place changed, and every few seconds for anything
// else. A socket whose session no longer holds, whose page it may no longer
// open, or whose page changed between editable and read-only is closed; the
// editor reconnects under its current access. A writable socket's every write
// is also gated: once the access revision moved past what the socket was last
// checked at, it is checked again before the write is applied, so a write
// that reaches the server after a permission change is committed is refused.

const (
	ygoRoomAuthorizationStoreKey = "leafenYgoRoomAuthorization"
	ygoAccessRecheckInterval     = 10 * time.Second
	// How often open sockets look at the access revision; one row is read.
	ygoAccessRevisionPollInterval = 500 * time.Millisecond
)

// ygoRoomAccess is what a socket was admitted with.
type ygoRoomAccess struct {
	UserID      string
	SessionID   string
	WorkspaceID string
	ReadOnly    bool
	// ViewOnly is someone who may only view or comment: as in Notion (seen
	// 2026-11-05), they aren't among the people on the page. Someone a lock or
	// the trash holds off editing still is.
	ViewOnly bool
	// RowPage is a database item's body rather than a page.
	RowPage bool
}

// ygoOpenSocket is one collaboration request, admitted or not yet.
type ygoOpenSocket struct {
	cancel   context.CancelFunc
	token    string
	room     string
	access   ygoRoomAccess
	admitted bool
	// The access revision the socket's access was last found to hold at.
	checkedRevision int64
}

type ygoAccessRecheck struct {
	mu      sync.Mutex
	sockets map[*http.Request]*ygoOpenSocket
	wake    chan struct{}
	cancel  context.CancelFunc
	done    chan struct{}
}

func newYgoAccessRecheck() *ygoAccessRecheck {
	return &ygoAccessRecheck{sockets: map[*http.Request]*ygoOpenSocket{}, wake: make(chan struct{}, 1)}
}

// evaluateRoomAccess applies the admission rules to a signed-in person's
// token for a room.
func (a *ygoRoomAuthorization) evaluateRoomAccess(userID, token, room string) (ygoRoomAccess, bool) {
	// A person's key opens the room too (agent full-permission decision of
	// 2026-10-16, 1), within its workspaces and what its scope allows; the
	// socket is checked again by the key as by a session.
	if strings.HasPrefix(token, userKeyPrefix) {
		key, found, err := findUserKey(a.app, token)
		if err != nil || !found || key.OwnerID != userID {
			return ygoRoomAccess{}, false
		}
		access, ok := a.evaluateRoomAccessAs(userID, "key:"+key.ConnectionID, room)
		if !ok || !slices.Contains(key.WorkspaceIDs, access.WorkspaceID) {
			return ygoRoomAccess{}, false
		}
		scope, err := userKeyEffectiveScope(a.app, key, []string{access.WorkspaceID})
		if err != nil || scope < userKeyScopeRead {
			return ygoRoomAccess{}, false
		}
		access.ReadOnly = access.ReadOnly || scope < userKeyScopeContent
		return access, true
	}
	sessionID, err := activeBrowserSessionID(a.app, token, userID)
	if err != nil {
		return ygoRoomAccess{}, false
	}
	return a.evaluateRoomAccessAs(userID, sessionID, room)
}

// evaluateRoomAccessAs is what a sign-in, a session or a key, may do in the room.
func (a *ygoRoomAuthorization) evaluateRoomAccessAs(userID, sessionID, room string) (ygoRoomAccess, bool) {
	if room == "" || len(room) > maxWorkspacePermissionIdentifierLength {
		return ygoRoomAccess{}, false
	}
	if a.settlement != nil && !a.settlement.allowsRoom(room) {
		return ygoRoomAccess{}, false
	}
	access := ygoRoomAccess{UserID: userID, SessionID: sessionID}
	if rowPage, err := isDatabaseRowPageRoom(a.app, room); err != nil {
		return ygoRoomAccess{}, false
	} else if rowPage {
		access.RowPage = true
		access.WorkspaceID, access.ReadOnly, err = authorizeDatabaseRowBodyRoom(a.app, userID, room)
		access.ViewOnly = access.ReadOnly
		return access, err == nil
	}
	// Someone who can only view or comment on the page reads it through a
	// socket that takes no edits.
	viewOnly := false
	authorization, err := authorizePage(a.app, userID, room, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		viewOnly = true
		authorization, err = authorizePage(a.app, userID, room, permissionWorkspaceRead)
	}
	if err != nil {
		return ygoRoomAccess{}, false
	}
	if leased, leaseErr := workspaceMarkdownReplacePageLeased(a.app, room, ""); leaseErr != nil || leased {
		return ygoRoomAccess{}, false
	}
	// A page in the trash, or under one, stays readable but takes no edits
	// until it is restored; so does a locked page, unless this person has
	// unlocked it for themselves.
	trashed, err := pageInTrashPath(a.app, room)
	if err != nil {
		return ygoRoomAccess{}, false
	}
	locked, err := pageLockedFor(a.app, authorization.Page, userID)
	if err != nil {
		return ygoRoomAccess{}, false
	}
	access.WorkspaceID, access.ReadOnly, access.ViewOnly = authorization.Workspace.WorkspaceID, trashed || locked || viewOnly, viewOnly
	return access, true
}

// openSocket registers a collaboration request so its socket can be closed
// by cancelling the request.
func (a *ygoRoomAuthorization) openSocket(request *http.Request) (*http.Request, func()) {
	ctx, cancel := context.WithCancel(request.Context())
	request = request.WithContext(ctx)
	a.recheck.mu.Lock()
	a.recheck.sockets[request] = &ygoOpenSocket{cancel: cancel}
	a.recheck.mu.Unlock()
	return request, func() {
		a.recheck.mu.Lock()
		delete(a.recheck.sockets, request)
		a.recheck.mu.Unlock()
		cancel()
	}
}

func (a *ygoRoomAuthorization) admitSocket(request *http.Request, token, room string, access ygoRoomAccess, revision int64) {
	a.recheck.mu.Lock()
	defer a.recheck.mu.Unlock()
	if socket := a.recheck.sockets[request]; socket != nil {
		socket.token, socket.room, socket.access, socket.admitted = token, room, access, true
		socket.checkedRevision = revision
	}
}

// accessStillHolds tells whether a socket's current access is the one it
// was admitted with.
func accessStillHolds(admitted, current ygoRoomAccess, ok bool) bool {
	return ok && current.SessionID == admitted.SessionID &&
		current.WorkspaceID == admitted.WorkspaceID && current.ReadOnly == admitted.ReadOnly &&
		current.ViewOnly == admitted.ViewOnly
}

// writeGate admits a writable socket's write only while its access holds.
// The access revision is read with each write; when it moved since the
// socket was last checked, the socket is checked again first, and one that
// may no longer write is closed and the write dropped.
func (a *ygoRoomAuthorization) writeGate(request *http.Request) func() bool {
	return func() bool {
		// A server whose data moved to another takes no live edit either.
		if dataMovedAway(a.app) {
			return false
		}
		// Copy/move fences are durable jobs, outside the access revision.
		// An already admitted socket must observe the fence on every write.
		a.recheck.mu.Lock()
		held := a.recheck.sockets[request]
		room, workspaceID := "", ""
		if held != nil {
			room, workspaceID = held.room, held.access.WorkspaceID
		}
		a.recheck.mu.Unlock()
		if room == "" {
			return false
		}
		// Nor one whose workspace is moving or moved to another server.
		if _, moved := workspaceMovedTo(a.app, workspaceID); moved {
			return false
		}
		fenced, fenceErr := blockMovePageFenceActive(a.app, room)
		if fenceErr != nil || fenced {
			// What a fence drops stays in the writer's copy, so the socket
			// closes with it: the writer opens the room again once the page
			// takes edits (it isn't let in while fenced), and its sync sends
			// the write then. Kept open, the socket would never send it again.
			if fenced && held != nil {
				held.cancel()
			}
			return false
		}
		revision, err := currentAccessRevision(a.app)
		if err != nil {
			return false
		}
		a.recheck.mu.Lock()
		socket := a.recheck.sockets[request]
		if socket == nil || !socket.admitted {
			a.recheck.mu.Unlock()
			return false
		}
		if socket.checkedRevision == revision {
			a.recheck.mu.Unlock()
			return true
		}
		token, room, access := socket.token, socket.room, socket.access
		a.recheck.mu.Unlock()
		current, ok := a.evaluateRoomAccess(access.UserID, token, room)
		if !accessStillHolds(access, current, ok) {
			socket.cancel()
			return false
		}
		a.recheck.mu.Lock()
		if socket.checkedRevision < revision {
			socket.checkedRevision = revision
		}
		a.recheck.mu.Unlock()
		return true
	}
}

// recheckOpenSockets closes the sockets whose access no longer holds as
// admitted.
func (a *ygoRoomAuthorization) recheckOpenSockets() {
	a.recheck.mu.Lock()
	sockets := make([]*ygoOpenSocket, 0, len(a.recheck.sockets))
	for _, socket := range a.recheck.sockets {
		if socket.admitted {
			sockets = append(sockets, socket)
		}
	}
	a.recheck.mu.Unlock()
	for _, socket := range sockets {
		current, ok := a.evaluateRoomAccess(socket.access.UserID, socket.token, socket.room)
		if !accessStillHolds(socket.access, current, ok) {
			socket.cancel()
		}
	}
}

func (a *ygoRoomAuthorization) hasAdmittedSockets() bool {
	a.recheck.mu.Lock()
	defer a.recheck.mu.Unlock()
	for _, socket := range a.recheck.sockets {
		if socket.admitted {
			return true
		}
	}
	return false
}

func (a *ygoRoomAuthorization) wakeAccessRecheck() {
	select {
	case a.recheck.wake <- struct{}{}:
	default:
	}
}

// StartAccessRecheck checks open sockets whenever woken and every few
// seconds.
func (a *ygoRoomAuthorization) StartAccessRecheck() {
	a.recheck.mu.Lock()
	defer a.recheck.mu.Unlock()
	if a.recheck.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.recheck.cancel, a.recheck.done = cancel, make(chan struct{})
	go func(done chan struct{}) {
		defer close(done)
		ticker := time.NewTicker(ygoAccessRecheckInterval)
		defer ticker.Stop()
		poll := time.NewTicker(ygoAccessRevisionPollInterval)
		defer poll.Stop()
		seen, _ := currentAccessRevision(a.app)
		for {
			select {
			case <-ctx.Done():
				return
			case <-a.recheck.wake:
			case <-ticker.C:
			case <-poll.C:
				if !a.hasAdmittedSockets() {
					continue
				}
				revision, err := currentAccessRevision(a.app)
				if err != nil || revision == seen {
					continue
				}
				seen = revision
			}
			a.recheckOpenSockets()
		}
	}(a.recheck.done)
}

func (a *ygoRoomAuthorization) ShutdownAccessRecheck(ctx context.Context) error {
	a.recheck.mu.Lock()
	cancel, done := a.recheck.cancel, a.recheck.done
	a.recheck.cancel, a.recheck.done = nil, nil
	a.recheck.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("collaboration access recheck shutdown: %w", ctx.Err())
	}
}

// recheckCollaborationAccessAfterCommit checks open sockets once the
// current change is committed: a revoked session or a page that moved to
// or out of the trash.
func recheckCollaborationAccessAfterCommit(app core.App) {
	wake := func() {
		if authorization, ok := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization); ok {
			authorization.wakeAccessRecheck()
		}
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				wake()
			}
			return nil
		})
		return
	}
	wake()
}
