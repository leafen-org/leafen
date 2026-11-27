package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pocketbase/pocketbase/core"
)

// The change check's wait goes over a socket (measured 2026-10-30, approved
// the same day): over plain HTTP/1.1 a browser keeps six connections to a
// server, so six tabs each holding a waiting check took them all and every
// other request of those tabs waited the 25 seconds out; a socket doesn't
// count toward them. The server says {"changed":true} when the person's page
// list or sharing moved, {"notified":true} when a notice came,
// {"layout":<revision>} when their sidebar layout changed and
// {"people":true} when the workspace's People did, {"workspaces":true} when
// a workspace of the person's was renamed or given another icon
// (workspace_look_follow.go), and {"graphs":[…]} when a page whose content
// its sidebar draws changed (workspace_change_graphs.go), and the screen then
// asks its short check as before, reads the layout again unless it has that
// revision, reads its workspaces again, or reads those pages again. Like a
// page's socket it carries
// its token in the query, opens only from the app's own pages, and counts
// toward the person's live sockets (collaborationSocketsPerPerson).

const workspaceChangeSocketPath = "/workspaces/{workspaceId}/change-socket"

// Its origin is checked before it opens, as a page socket's is.
var workspaceChangeSocketUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func registerWorkspaceChangeSocketRoute(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspaceChangeSocketPath, serveWorkspaceChangeSocket)
}

func serveWorkspaceChangeSocket(event *core.RequestEvent) error {
	if !socketOriginAllowed(event.Request, packagedClientOrigins) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "This page can't open the workspace's changes.")
	}
	rooms, _ := event.App.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization)
	if rooms == nil {
		return apiError(event, http.StatusInternalServerError, errorCodeWorkspaceChangeProbeFailed, "Couldn't follow the workspace's changes.")
	}
	token := bearerToken(event.Request)
	if token == "" {
		token = event.Request.URL.Query().Get("token")
	}
	user, _, err := rooms.socketPerson(token)
	if err != nil || user == nil || user.Collection().Name != "users" {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, member := workspacePageListSince(event.App, user.Id, workspaceID); !member {
		if _, err := readGuestWorkspaceChangeSnapshot(event.App, user.Id, workspaceID, true); err != nil {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
	}
	release, _, admitted := rooms.admitPersonSocket(event.Request)
	if !admitted {
		event.Response.Header().Set("Retry-After", collaborationSocketRetryAfter)
		return apiError(event, http.StatusTooManyRequests, errorCodeRateLimited,
			"Too many live connections for one person. Close some tabs, or try again shortly.")
	}
	defer release()
	conn, err := workspaceChangeSocketUpgrader.Upgrade(event.Response, event.Request, nil)
	if err != nil {
		return nil
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(event.Request.Context())
	defer cancel()
	watch := workspacePageListWatchFor(event.App)
	// Its waiter stays registered while it is open, so what comes between
	// two waits ends the next (workspacePageListWatch.hold).
	waiter := watch.hold(workspaceID, user.Id)
	defer watch.release(waiter)
	// The screen says only which pages' content its sidebar draws
	// (workspace_change_graphs.go); a read ends when it closes.
	conn.SetReadLimit(workspaceChangeSocketReadLimit)
	go func() {
		defer cancel()
		for {
			kind, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.TextMessage {
				watch.takeWatchedGraphsWord(event.App, waiter, message)
			}
		}
	}()
	// What of the guest's the screen was last told of, read at its first wait
	// and kept from one to the next so a change between them isn't missed;
	// a member's page list mark likewise, read as the socket opens and then
	// as each wait ends (2026-11-15: read again as each wait began, a page
	// made while the socket told of the last change or rested was taken as
	// already told of, and never reached the open sidebar).
	var seenByGuest *guestSeen
	since, member := workspacePageListSince(event.App, user.Id, workspaceID)
	for ctx.Err() == nil {
		started := time.Now()
		var end workspacePageListWaitEnd
		guestChanged := false
		if member {
			end = watch.waitHeld(ctx, event.App, waiter, since, true, 0, false, workspacePageListWaitLimit)
		} else {
			if seenByGuest == nil {
				first := readGuestSeen(event.App, user.Id, workspaceID)
				seenByGuest = &first
			}
			end, guestChanged = waitGuestWorkspaceChange(ctx, event.App, watch, waiter, seenByGuest)
		}
		if ctx.Err() != nil {
			return nil
		}
		notified, layout := end.notified, end.layout
		after, stillMember := workspacePageListSince(event.App, user.Id, workspaceID)
		// The person's workspaces changing is a change too: the screen asks,
		// and its menu lists them as they are now.
		changed := member != stillMember || (member && after != since) || guestChanged || end.joined || end.favorites
		// People changed: an open People list reads itself again (a guest's has none).
		people := end.people && stillMember
		since, member = after, stillMember
		if changed || notified || layout != 0 || people || end.workspaces || len(end.graphs) > 0 {
			said := map[string]any{"changed": changed, "notified": notified}
			if len(end.graphs) > 0 {
				said["graphs"] = end.graphs
			}
			if layout != 0 {
				said["layout"] = layout
			}
			if people {
				said["people"] = true
			}
			if end.workspaces {
				said["workspaces"] = true
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(said); err != nil {
				return nil
			}
		} else if time.Since(started) < time.Second {
			// A wait ended early with nothing to tell: a sign-in of the
			// person's ended. If it is this socket's own, the screen is told
			// to look again, finds the sign-in gone and shows the sign-in
			// screen, and the socket closes (2026-11-05: an administrator's
			// "로그아웃" left the person's open screen as it was). Otherwise
			// rest a moment rather than spin.
			if current, _, err := rooms.socketPerson(token); err != nil || current == nil || current.Id != user.Id {
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				_ = conn.WriteJSON(map[string]bool{"changed": true, "notified": false})
				return nil
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}
	return nil
}

// waitGuestWorkspaceChange is a guest screen's wait: its notices and layout,
// and what it may see there — the pages shared with it, and the workspace's
// pages as its ten-second check followed them (its change snapshot). It ends
// early once anyone's sharing changes on the server, so a page newly shared
// with the guest, or taken back, reaches their open sidebar within about a
// second, as Notion's does; it says changed only when the guest's own
// snapshot moved from what was seen before (seen), at once when it already
// had (2026-11-11: with the socket open the guest's screen never asked, and
// newly shared pages showed only after a reload).
func waitGuestWorkspaceChange(
	ctx context.Context, app core.App, watch *workspacePageListWatch, waiter *workspacePageListWaiter, seen *guestSeen,
) (workspacePageListWaitEnd, bool) {
	workspaceID, userID := waiter.workspaceID, waiter.userID
	var end workspacePageListWaitEnd
	// The access revision is read first: a change after it ends the wait,
	// and one before it the snapshot read after it already holds.
	access, accessErr := currentAccessRevision(app)
	if seen.moved(readGuestSeen(app, userID, workspaceID)) {
		return end, true
	}
	end = watch.waitHeld(ctx, app, waiter, 0, false, access, accessErr == nil, workspaceGuestNoticeWait)
	if ctx.Err() != nil {
		return end, false
	}
	return end, seen.moved(readGuestSeen(app, userID, workspaceID))
}

// guestSeen is a guest's change snapshot as last read, or that it couldn't be.
type guestSeen struct {
	snapshot workspaceChangeSnapshot
	readable bool
}

func readGuestSeen(app core.App, userID, workspaceID string) guestSeen {
	snapshot, err := readGuestWorkspaceChangeSnapshot(app, userID, workspaceID, true)
	// The server's access revision moves for sharing anywhere; the rest of
	// the snapshot says what of the guest's did.
	snapshot.AccessRevision = 0
	return guestSeen{snapshot: snapshot, readable: err == nil}
}

// moved says whether now differs from what was seen, which it then becomes.
func (seen *guestSeen) moved(now guestSeen) bool {
	if now == *seen {
		return false
	}
	*seen = now
	return true
}
