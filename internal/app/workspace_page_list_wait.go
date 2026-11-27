package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An open screen's change check may wait for its workspace's page list, or
// its sharing, to change from what they are when it comes, up to
// workspacePageListWaitLimit, and then answers as a check does:
// another person's renamed, added, moved or trashed page reaches the sidebar
// within about a second, as Notion's does (0.6 to 1 second, seen 2026-10-15),
// rather than at the next ten-second check. One reader per server looks at
// the lists the waiting screens follow, four times a second, however many
// they are; nothing looks while none waits. A new notice for the person in
// that workspace (notification_delivery.go) ends the wait at the next look,
// its write by then done, and the answer says so: their inbox shows it
// within about a second, whatever page they are on. So does a change to
// their own sidebar layout there from another screen, device or agent
// (sidebar_layout.go), which their open sidebars then read again, and a
// change to the workspace's People (notePeopleChangedAfterCommit), which an
// open People list then reads again. A guest's screen waits for its notices
// and its layout, and for the server's access to change (anyone's sharing
// anywhere), as long as its ten-second check would; what it then says is the
// guest's own (workspace_change_socket.go).
const (
	workspacePageListWaitLimit = 25 * time.Second
	workspaceGuestNoticeWait   = 10 * time.Second
	workspacePageListLookEvery = 250 * time.Millisecond
	workspacePageListLookChunk = 500
)

type workspacePageListWaiter struct {
	workspaceID string
	userID      string
	since       int64
	// follows: the page list ends the wait too, not a guest's notices alone.
	follows bool
	// access: a guest's wait ends too once the server's access revision is
	// past it (followsAccess), as a page shared with them moves it.
	access        int64
	followsAccess bool
	// notified: a notice came for the person, ending the wait at the next look.
	notified bool
	// joined: the workspaces the person belongs to changed (added to one,
	// taken out of one), ending the wait at the next look.
	joined bool
	// layout: the revision the person's sidebar layout in the workspace
	// changed to, ending the wait at the next look; 0 while it didn't.
	layout int64
	// people: the workspace's People changed (notePeopleChangedAfterCommit), ending the
	// wait at the next look.
	people bool
	// favorites: the person's favorites there changed on another screen
	// (noteFavoriteChanges), ending the wait at the next look.
	favorites bool
	// workspaces: a workspace the person is in was renamed or given another
	// icon (noteWorkspaceLookChangedAfterCommit), ending the wait at the next look.
	workspaces bool
	// watched: the pages whose content the socket's screen draws in its
	// sidebar (workspace_change_graphs.go); graphs, those of them whose
	// graph changed since it was last told, ending the wait at the next look.
	watched map[string]struct{}
	graphs  map[string]struct{}
	// held: an open change socket's, registered for as long as the socket
	// is open rather than for one wait (hold); armed while it waits.
	held  bool
	armed bool
	// ended: one of the person's sign-ins ended (endUser) while a held
	// waiter wasn't waiting, which ends its next wait at once.
	ended   bool
	changed chan struct{}
}

type workspacePageListWatch struct {
	mu      sync.Mutex
	waiters map[*workspacePageListWaiter]struct{}
	looking bool
}

// workspacePageListWatches are the servers' watches, one each: by the
// server's store, which the apps a request runs against share
// (request_write_stop.go, user_key_auth.go).
var workspacePageListWatches sync.Map

func workspacePageListWatchFor(app core.App) *workspacePageListWatch {
	watch, _ := workspacePageListWatches.LoadOrStore(app.Store(), &workspacePageListWatch{
		waiters: map[*workspacePageListWaiter]struct{}{},
	})
	return watch.(*workspacePageListWatch)
}

// What a waiting check follows: the page list's count and the sharing's
// cursor, which only grow, summed.
const workspacePageListMarkSQL = `[[revision]].[[page_list_revision]] + COALESCE((
	SELECT [[permission]].[[cursor]] FROM {{leafen_workspace_permission_state}} [[permission]]
	WHERE [[permission]].[[workspace_id]] = [[revision]].[[workspace_id]]), 0)`

// workspacePageListSince is where a member's waiting check starts; a guest's
// doesn't wait on the whole workspace, which would tell them when pages they
// can't see change.
func workspacePageListSince(app core.App, principalID, workspaceID string) (int64, bool) {
	if _, err := readWorkspaceChangeSnapshots(app, principalID, []string{workspaceID}); err != nil {
		return 0, false
	}
	var mark int64
	if err := app.DB().NewQuery(`
		SELECT ` + workspacePageListMarkSQL + ` FROM {{leafen_workspace_content_revisions}} [[revision]]
		WHERE [[revision]].[[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&mark); err != nil {
		return 0, false
	}
	return mark, true
}

// workspacePageListWaitEnd says why a wait ended besides the page list: a
// notice came for the person, their workspaces changed, or their sidebar
// layout there did (to the revision it names).
type workspacePageListWaitEnd struct {
	notified   bool
	joined     bool
	layout     int64
	people     bool
	favorites  bool
	workspaces bool
	// graphs: the pages drawn in the sidebar whose graphs changed (workspace_change_graphs.go).
	graphs []string
}

// wait returns once the workspace's page list is past since (when it
// follows the list), a notice has come for the person, their workspaces
// changed, the limit has passed or the request has ended.
func (watch *workspacePageListWatch) wait(
	ctx context.Context, app core.App, workspaceID, userID string, since int64, follows bool, limit time.Duration,
) workspacePageListWaitEnd {
	return watch.await(ctx, app, &workspacePageListWaiter{workspaceID: workspaceID, userID: userID, since: since, follows: follows}, limit)
}

func (watch *workspacePageListWatch) await(
	ctx context.Context, app core.App, waiter *workspacePageListWaiter, limit time.Duration,
) workspacePageListWaitEnd {
	watch.mu.Lock()
	// What came for a held waiter between its waits ends this one at once.
	if waiter.held && (waiter.notified || waiter.joined || waiter.layout != 0 || waiter.people || waiter.favorites || waiter.workspaces ||
		len(waiter.graphs) > 0 || waiter.ended) {
		end := waiter.take()
		watch.mu.Unlock()
		return end
	}
	waiter.changed = make(chan struct{})
	waiter.armed = true
	watch.waiters[waiter] = struct{}{}
	if !watch.looking {
		// Under the data's work (data_work.go): it ends, waited for, before
		// the data closes; with the data closing it doesn't start.
		watch.looking = dataWorkOf(app).run(app, "workspace page list watch", func(ctx context.Context) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logRecoveredPanic(app, "workspace page list watch", recovered)
					watch.mu.Lock()
					watch.looking = false
					watch.mu.Unlock()
				}
			}()
			watch.look(ctx, app)
		})
	}
	watch.mu.Unlock()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-waiter.changed:
	case <-timer.C:
	case <-ctx.Done():
	}
	watch.mu.Lock()
	waiter.armed = false
	if !waiter.held {
		delete(watch.waiters, waiter)
	}
	end := waiter.take()
	watch.mu.Unlock()
	return end
}

// take is why the waiter's wait ended, which a held one then forgets for its next.
func (waiter *workspacePageListWaiter) take() workspacePageListWaitEnd {
	end := workspacePageListWaitEnd{notified: waiter.notified, joined: waiter.joined, layout: waiter.layout, people: waiter.people,
		favorites: waiter.favorites, workspaces: waiter.workspaces, graphs: waiter.changedGraphs()}
	waiter.notified, waiter.joined, waiter.layout, waiter.people, waiter.favorites, waiter.workspaces, waiter.ended =
		false, false, 0, false, false, false, false
	return end
}

// wake ends a waiter's wait now: one wait's is let go, a held one stays for
// its next wait, and one not waiting has nothing to end.
func (watch *workspacePageListWatch) wake(waiter *workspacePageListWaiter) {
	if !waiter.armed {
		return
	}
	waiter.armed = false
	close(waiter.changed)
	if !waiter.held {
		delete(watch.waiters, waiter)
	}
}

// hold registers an open change socket's waiter (workspace_change_socket.go)
// for as long as the socket is open, not only while it waits: a notice, a
// layout, People or favorites change, a workspace joined, or one of the
// person's sign-ins ending, that comes while the socket tells the screen of
// the last one or rests, ends its next wait at once (2026-11-15: a page made
// in the second after another device's sign-in ended the person's oldest
// one, while the woken socket rested, never reached their open sidebar).
// The page list's mark it waits from is the socket's to carry (waitHeld).
func (watch *workspacePageListWatch) hold(workspaceID, userID string) *workspacePageListWaiter {
	waiter := &workspacePageListWaiter{workspaceID: workspaceID, userID: userID, held: true}
	watch.mu.Lock()
	watch.waiters[waiter] = struct{}{}
	watch.mu.Unlock()
	return waiter
}

// release lets a held waiter go as its socket closes.
func (watch *workspacePageListWatch) release(waiter *workspacePageListWaiter) {
	watch.mu.Lock()
	watch.wake(waiter)
	delete(watch.waiters, waiter)
	watch.mu.Unlock()
}

// waitHeld is a held waiter's next wait: from the page list's mark since
// (when it follows the list) and, for a guest (followsAccess), until the
// server's access revision is past accessSince, as a page shared with them
// moves it.
func (watch *workspacePageListWatch) waitHeld(
	ctx context.Context, app core.App, waiter *workspacePageListWaiter, since int64, follows bool,
	accessSince int64, followsAccess bool, limit time.Duration,
) workspacePageListWaitEnd {
	watch.mu.Lock()
	waiter.since, waiter.follows, waiter.access, waiter.followsAccess = since, follows, accessSince, followsAccess
	watch.mu.Unlock()
	return watch.await(ctx, app, waiter, limit)
}

// noteWorkspaceSetChanges marks the waits of the people whose workspaces
// changed, on every server watch, so a screen they have open lists the
// workspace they were added to, or drops the one they left, within about a
// second, as Notion's does (2026-11-11: the open menu didn't list it for
// half a minute and more; the screen's change socket only followed its own
// workspace). The writer may hold a transaction; the next look, a quarter
// second on, finds it done.
func noteWorkspaceSetChanges(userIDs ...string) {
	if len(userIDs) == 0 {
		return
	}
	people := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		people[userID] = struct{}{}
	}
	workspacePageListWatches.Range(func(_, watch any) bool {
		w := watch.(*workspacePageListWatch)
		w.mu.Lock()
		for waiter := range w.waiters {
			if _, changed := people[waiter.userID]; changed {
				waiter.joined = true
			}
		}
		w.mu.Unlock()
		return true
	})
}

// noteSidebarLayoutChanges marks the waits of a person in a workspace whose
// sidebar layout there just changed to a revision, on every server watch, so
// their other open screens read it again within about a second; the screen
// that made the change already has that revision and reads nothing.
func noteSidebarLayoutChanges(workspaceID, userID string, revision int64) {
	workspacePageListWatches.Range(func(_, watch any) bool {
		w := watch.(*workspacePageListWatch)
		w.mu.Lock()
		for waiter := range w.waiters {
			if waiter.workspaceID == workspaceID && waiter.userID == userID {
				waiter.layout = max(waiter.layout, revision)
			}
		}
		w.mu.Unlock()
		return true
	})
}

// noteFavoriteChanges marks the waits of a person in a workspace whose
// favorites there just changed, on every server watch, so their other open
// screens' sidebars take them within about a second, from the page changes
// their change check reads (bootstrap_page_changes.go).
func noteFavoriteChanges(workspaceID, userID string) {
	workspacePageListWatches.Range(func(_, watch any) bool {
		w := watch.(*workspacePageListWatch)
		w.mu.Lock()
		for waiter := range w.waiters {
			if waiter.workspaceID == workspaceID && waiter.userID == userID {
				waiter.favorites = true
			}
		}
		w.mu.Unlock()
		return true
	})
}

// notePeopleChangedAfterCommit marks, once the change commits (at once
// outside a transaction), the waits of everyone in a workspace whose People
// changed — someone joined, left, was deactivated, or their name, About or
// membership changed — on every server watch, so an open People list reads
// itself again within about a second (PeopleView.tsx; 2026-11-14: it showed
// a new member only once opened again).
func notePeopleChangedAfterCommit(app core.App, workspaceID string) {
	note := func() {
		workspacePageListWatches.Range(func(_, watch any) bool {
			w := watch.(*workspacePageListWatch)
			w.mu.Lock()
			for waiter := range w.waiters {
				if waiter.workspaceID == workspaceID {
					waiter.people = true
				}
			}
			w.mu.Unlock()
			return true
		})
		// Its People database's rows follow (people_database.go).
		syncPeopleDatabaseSoon(app, workspaceID)
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				note()
			}
			return nil
		})
		return
	}
	note()
}

// noteWorkspaceNotices marks the waits of the people notices just came for,
// each in its workspace, on every server watch — the writer may hold a
// transaction's app, which no wait is keyed by.
func noteWorkspaceNotices(workspaceUsers map[[2]string]struct{}) {
	if len(workspaceUsers) == 0 {
		return
	}
	workspacePageListWatches.Range(func(_, watch any) bool {
		watch.(*workspacePageListWatch).noteNotices(workspaceUsers)
		return true
	})
}

// noteNotices marks the waits of the people a notice just came for, in its
// workspace; the next look ends them.
func (watch *workspacePageListWatch) noteNotices(workspaceUsers map[[2]string]struct{}) {
	watch.mu.Lock()
	defer watch.mu.Unlock()
	for waiter := range watch.waiters {
		if _, noted := workspaceUsers[[2]string{waiter.workspaceID, waiter.userID}]; noted {
			waiter.notified = true
		}
	}
}

// endUser ends a person's waits now (sign_in_waits.go).
func (watch *workspacePageListWatch) endUser(userID string) {
	watch.mu.Lock()
	defer watch.mu.Unlock()
	for waiter := range watch.waiters {
		if waiter.userID == userID {
			waiter.ended = waiter.held && !waiter.armed
			watch.wake(waiter)
		}
	}
}

func (watch *workspacePageListWatch) look(ctx context.Context, app core.App) {
	ticker := time.NewTicker(workspacePageListLookEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
		watch.mu.Lock()
		// Nothing looks while none waits (a held waiter between its waits
		// doesn't), nor once the data begins to close: the desktop app starts
		// its own server again in the same process (sign_in_ends_follow.go).
		waiting := false
		for waiter := range watch.waiters {
			waiting = waiting || waiter.armed
		}
		if !waiting || ctx.Err() != nil {
			watch.looking = false
			watch.mu.Unlock()
			return
		}
		workspaceIDs := map[string]struct{}{}
		guests := false
		for waiter := range watch.waiters {
			if waiter.armed && waiter.follows {
				workspaceIDs[waiter.workspaceID] = struct{}{}
			}
			guests = guests || (waiter.armed && waiter.followsAccess)
		}
		watch.mu.Unlock()
		revisions, err := readWorkspacePageListRevisions(app, workspaceIDs)
		access, accessErr := int64(0), error(nil)
		if guests {
			access, accessErr = currentAccessRevision(app)
		}
		watch.mu.Lock()
		for waiter := range watch.waiters {
			revision, found := revisions[waiter.workspaceID]
			if waiter.armed && (waiter.notified || waiter.joined || waiter.layout != 0 || waiter.people || waiter.favorites || waiter.workspaces ||
				len(waiter.graphs) > 0 ||
				(err == nil && waiter.follows && found && revision != waiter.since) ||
				(guests && accessErr == nil && waiter.followsAccess && access != waiter.access)) {
				watch.wake(waiter)
			}
		}
		watch.mu.Unlock()
	}
}

func readWorkspacePageListRevisions(app core.App, workspaceIDs map[string]struct{}) (map[string]int64, error) {
	ids := make([]string, 0, len(workspaceIDs))
	for workspaceID := range workspaceIDs {
		ids = append(ids, workspaceID)
	}
	revisions := make(map[string]int64, len(ids))
	for start := 0; start < len(ids); start += workspacePageListLookChunk {
		end := min(start+workspacePageListLookChunk, len(ids))
		params := dbx.Params{}
		placeholders := make([]string, 0, end-start)
		for index, workspaceID := range ids[start:end] {
			key := fmt.Sprintf("workspace%d", index)
			params[key] = workspaceID
			placeholders = append(placeholders, "{:"+key+"}")
		}
		rows := []struct {
			WorkspaceID string `db:"workspace_id"`
			Mark        int64  `db:"mark"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[revision]].[[workspace_id]], ` + workspacePageListMarkSQL + ` AS [[mark]]
			FROM {{leafen_workspace_content_revisions}} [[revision]]
			WHERE [[revision]].[[workspace_id]] IN (` + strings.Join(placeholders, ", ") + `)
		`).Bind(params).All(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			revisions[row.WorkspaceID] = row.Mark
		}
	}
	return revisions, nil
}
