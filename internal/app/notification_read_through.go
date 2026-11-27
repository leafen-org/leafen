package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A read of all the inbox through one notice takes that notice by the time
// any of Leafen's answers gives it: the inbox list writes a notice's time to
// the millisecond in UTC, leafen_inbox to the second on the person's clock
// (seen 2026-11-26: an agent passed leafen_inbox's newest id with
// "2026-11-26T11:18:06+09:00" for a notice stored at 02:18:06.269Z, nothing
// was read and the answer said complete). A time written coarser stands for
// the whole span it covers (a second, or the tenth or hundredth of one its
// digits give); when the notice through names is stored within that span,
// the read goes through that notice as it is stored. A notice whose time
// moved past the span — a newer comment on it since — stays unread, as one
// the person hasn't seen; an ID naming none of the person's notices leaves
// the time as written.
//
// A read of all that leaves unread notices says so: how many are left and
// the newest of them, as the inbox list gives its boundary, to name as
// through to read those too (notificationReadAnswer).

// notificationReadThroughSpan is how many milliseconds a time as written
// covers: a second without a fraction, a tenth or a hundredth of one with
// one or two digits of it, a millisecond with three or more.
func notificationReadThroughSpan(written string) int64 {
	written = strings.TrimSpace(written)
	clock := strings.IndexAny(written, "Tt ")
	if clock < 0 {
		return 1
	}
	dot := strings.IndexByte(written[clock:], '.')
	if dot < 0 {
		return 1000
	}
	digits := 0
	for _, c := range written[clock+dot+1:] {
		if c < '0' || c > '9' {
			break
		}
		digits++
	}
	switch digits {
	case 0:
		return 1000
	case 1:
		return 100
	case 2:
		return 10
	}
	return 1
}

// notificationReadThroughAsStored moves a read's through time to the stored
// time of the notice it names, when that lies within the span the time as
// written covers.
func notificationReadThroughAsStored(
	app core.App,
	actorID string,
	workspaceID string,
	input *normalizedNotificationReadInput,
) error {
	if input.Through == nil || input.ThroughSpan <= 1 {
		return nil
	}
	var stored int64
	err := app.DB().NewQuery(`
		SELECT [[occurred_at]] FROM {{leafen_notifications}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId}
	`).Bind(dbx.Params{
		"id": input.Through.ID, "workspaceId": workspaceID, "userId": actorID,
	}).Row(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if stored > input.ThroughOccurredAt && stored < input.ThroughOccurredAt+input.ThroughSpan {
		input.ThroughOccurredAt = stored
		input.Through.OccurredAt = notificationInboxTime(stored)
	}
	return nil
}

// renderNotificationReadAnswer is a settlement as it is answered: a read of
// all done says what it left unread — how many, and the newest of them by
// its id and occurredAt as the inbox list gives them.
func renderNotificationReadAnswer(
	app core.App,
	row storedNotificationReadSettlement,
) (notificationReadSettlement, error) {
	settlement := renderNotificationReadSettlement(row)
	if row.Mode != notificationReadModeAll || row.Status != notificationReadStatusComplete {
		return settlement, nil
	}
	inbox, err := readNotificationInboxFilteredSnapshotInTransaction(
		app, row.ActorID, row.WorkspaceID, 1, false, notificationInboxStateUnread, notificationInboxCursor{},
	)
	if err != nil {
		// One the person can no longer read says nothing of it.
		if isPermissionDenied(err) {
			return settlement, nil
		}
		return settlement, err
	}
	if inbox.UnreadCount > 0 && len(inbox.Items) > 0 {
		settlement.UnreadLeft = inbox.UnreadCount
		settlement.NewestUnread = &notificationInboxBoundary{OccurredAt: inbox.Items[0].OccurredAt, ID: inbox.Items[0].ID}
	}
	return settlement, nil
}
