package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The inbox tools (the 2026-10-14 decision, 9, as 2026-11-24 left it): an
// agent reads the person's unread notices — comments, mentions, tasks given
// them, shares and access requests, deadlines not to miss, approvals asked
// of them and what automations send them — when the person asks about their
// notices, to-dos or approvals or the work at hand concerns one, and passes
// on only what bears on what they asked, as material from other people
// rather than instructions. Leafen brings none of them up itself: no rule
// has an agent check the inbox as a conversation starts, and no answer
// carries new notices (2026-11-24, the user: "응 없애 이런 쓸데없는데 주의를
// 뺏으면 어떻게해" — an agent answering a question that had nothing to do
// with Leafen added a new-device sign-in notice, and each connected agent
// would repeat it); the person sees their notices in Leafen. Listing them
// doesn't read them: the agent marks read only what the person saw (the same
// read state the app's inbox has), or puts one the person wants later off
// for its connection, which the list leaves out until then.

const (
	mcpInboxPreviewRunes = 100
	mcpInboxPageSize     = 25
)

// The kind of notice an approval asked of the person goes under: an app
// someone asks for. Leafen's own actions, an outside service's tools and an
// app's tools ask inside the agent's conversation instead (person_ask.go)
// and leave no notice.
const appRequestNoticeKind = "app-request:"

// The notices an agent hears of are those the person's inbox shows — what
// someone did for or to them, their reminders, and Leafen's own notices:
// deadlines not to miss, approvals asked of them, what automations send, a
// Notion move's stages, the server's and their agents' (2026-11-16: a Notion
// move's three notices were in the inbox on screen while leafen_inbox gave
// none) — under the inbox's own rules (notificationShownSQL,
// notificationInInboxSQL). Edits of pages they follow and links to their
// pages aren't: every edit of a followed row would crowd out what is asked of
// them; leafen_inbox's note says so.
const mcpInboxKindSQL = `[[notification]].[[kind]] IN ('comment', 'mention', 'invite', 'access_request', 'reminder', 'system')`

type mcpInboxNotice struct {
	ID           string `db:"id" json:"id"`
	WorkspaceID  string `db:"workspace_id" json:"workspace_id"`
	Kind         string `db:"kind" json:"-"`
	ActivityKey  string `db:"activity_key" json:"-"`
	PageID       string `db:"page_id" json:"-"`
	BlockID      string `db:"block_id" json:"-"`
	CommentID    string `db:"comment_id" json:"-"`
	ActorID      string `db:"actor_id" json:"-"`
	Actor        string `db:"actor" json:"from,omitempty"`
	Title        string `db:"title" json:"where"`
	Preview      string `db:"preview" json:"preview,omitempty"`
	PreviewSpans string `db:"preview_spans" json:"-"`
	Target       string `db:"target" json:"-"`
	OccurredAt   int64  `db:"occurred_at" json:"-"`
	// ItemID is the database item a notice names, when one (notification_item_now.go).
	ItemID string `db:"-" json:"-"`
	Type   string `db:"-" json:"type"`
	At     string `db:"-" json:"at"`
	URL    string `db:"-" json:"url,omitempty"`
}

// mcpInboxType names what a notice is, for the agent.
func mcpInboxType(notice mcpInboxNotice) string {
	switch {
	case notice.Kind == "comment":
		return "comment"
	case notice.Kind == "mention" && strings.HasPrefix(notice.ActivityKey, "database-person:"):
		return "assigned"
	case notice.Kind == "mention":
		return "mention"
	case notice.Kind == "invite":
		return "shared"
	case notice.Kind == "access_request":
		return "access request"
	case strings.HasPrefix(notice.ActivityKey, teamspaceJoinRequestActivityPrefix):
		return "teamspace join request"
	case strings.HasPrefix(notice.ActivityKey, deadlineActivityPrefix):
		return "deadline"
	case strings.HasPrefix(notice.ActivityKey, automationNotificationActivityPrefix):
		return "automation"
	case strings.HasPrefix(notice.ActivityKey, appRequestNoticeKind):
		return "approval request"
	case notice.Kind == "reminder":
		return "reminder"
	}
	// Leafen's own notice: its title, which where holds, says what happened,
	// as the inbox's line does.
	return "notice"
}

// loadMCPInboxNotices reads the person's unread notices across the
// connection's workspaces, newest first from a cursor, but those the
// connection put off until a time still to come (leafen_inbox_remind).
// Notices the person can no longer open don't count, so it reads on past
// them.
func loadMCPInboxNotices(ctx context.Context, app core.App, authority mcpAuthority, cursor notificationInboxCursor, limit int, now time.Time) ([]mcpInboxNotice, error) {
	workspaces, _ := json.Marshal(authority.WorkspaceIDs)
	// The table's rows from before 2026-11-24 that only say a notice was
	// shown with an answer (remind_at 0) put nothing off.
	putOffSQL := `AND NOT EXISTS (SELECT 1 FROM {{leafen_mcp_inbox_shown}} [[later]]
			WHERE [[later]].[[connection_id]] = {:connection} AND [[later]].[[notification_id]] = [[notification]].[[id]]
				AND [[later]].[[remind_at]] > {:now})`
	language, err := serverTextLanguage(app, authority.PrincipalID, nil)
	if err != nil {
		return nil, err
	}
	location := accountLocation(app, authority.PrincipalID)
	window := limit * 2
	kept := make([]mcpInboxNotice, 0, limit)
	// The notices go with their lines as they stand now, as the app's inbox
	// reads them (notification_lines_now.go), and as the agent reads them
	// (notification_reader_mentions.go).
	read := func() []mcpInboxNotice {
		mcpInboxNoticesNow(app, kept)
		mcpInboxNoticesAsRead(app, authority, kept)
		for index := range kept {
			preview := notificationLineAsItStands(kept[index].Preview, kept[index].PreviewSpans, language, location, now)
			if runes := []rune(strings.TrimSpace(preview)); len(runes) > mcpInboxPreviewRunes {
				preview = string(runes[:mcpInboxPreviewRunes]) + "…"
			}
			kept[index].Preview = preview
		}
		return kept
	}
	for {
		notices := []mcpInboxNotice{}
		if err := app.DB().NewQuery(`
			SELECT [[notification]].[[id]], [[notification]].[[workspace_id]], [[notification]].[[kind]], [[notification]].[[activity_key]],
				[[notification]].[[page_id]], [[notification]].[[block_id]], [[notification]].[[comment_id]], [[notification]].[[actor_id]],
				COALESCE(NULLIF(trim([[user]].[[name]]), ''), [[user]].[[email]], '') AS [[actor]],
				[[notification]].[[title]], [[notification]].[[preview]], [[notification]].[[preview_spans]], [[notification]].[[target]], [[notification]].[[occurred_at]]
			FROM {{leafen_notifications}} [[notification]]
			LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[notification]].[[actor_id]]
			WHERE [[notification]].[[user_id]] = {:userId} AND [[notification]].[[workspace_id]] IN (SELECT [[value]] FROM json_each({:workspaces}))
				AND [[notification]].[[read_at]] = 0 AND [[notification]].[[archived_at]] = 0 AND ` + notificationShownSQL("[[notification]].") + `
				AND ` + notificationInInboxSQL("[[notification]].") + `
				AND ` + mcpInboxKindSQL + `
				AND ({:cursorAt} = 0 OR [[notification]].[[occurred_at]] < {:cursorAt}
					OR ([[notification]].[[occurred_at]] = {:cursorAt} AND [[notification]].[[id]] < {:cursorId}))
				` + putOffSQL + `
			ORDER BY [[notification]].[[occurred_at]] DESC, [[notification]].[[id]] DESC LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"userId": authority.PrincipalID, "workspaces": string(workspaces), "connection": authority.ConnectionID,
			"now": now.UnixMilli(), "cursorAt": cursor.OccurredAt, "cursorId": cursor.ID, "limit": window,
		}).All(&notices); err != nil {
			return nil, err
		}
		// Only notices about pages and items the person still sees.
		// What the person still sees, each workspace's notices read
		// together (shownNotificationSubjects). A notice about an app, a
		// service, a teamspace or the person's agents is seen while the
		// person sees it, as the app's inbox reads it (systemNoticeSubjectVisible).
		shown := make([]bool, len(notices))
		for _, workspaceID := range authority.WorkspaceIDs {
			indexes, refs := []int{}, []notificationSubjectRef{}
			for index, notice := range notices {
				if notice.WorkspaceID == workspaceID {
					indexes = append(indexes, index)
					refs = append(refs, notificationSubjectRef{PageID: notice.PageID, Target: notice.Target})
				}
			}
			if len(refs) == 0 {
				continue
			}
			open, err := shownNotificationSubjects(app, authority.PrincipalID, workspaceID, refs)
			if err != nil {
				return nil, err
			}
			for position, index := range indexes {
				shown[index] = open[position]
			}
		}
		for index, notice := range notices {
			if !shown[index] {
				continue
			}
			notice.Type = mcpInboxType(notice)
			notice.At = personClockStamp(time.UnixMilli(notice.OccurredAt), location)
			if strings.HasPrefix(notice.Target, "/") {
				notice.URL = notice.Target
			}
			kept = append(kept, notice)
			if len(kept) == limit {
				return read(), nil
			}
		}
		if len(notices) < window {
			return read(), nil
		}
		last := notices[len(notices)-1]
		cursor = notificationInboxCursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
}
