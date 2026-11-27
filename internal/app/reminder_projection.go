package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	reminderBlockWindow   = 256
	reminderPastTolerance = time.Minute
)

type storedReminderBlock struct {
	ID           string `db:"id"`
	Type         string `db:"type"`
	LastEditedBy string `db:"last_edited_by"`
	// Content is empty unless it mentions or links to something, or is a
	// to-do's with assignees or a due date's reminder.
	Content string `db:"mention_content"`
}

type storedReminderRow struct {
	ID               string `db:"id"`
	ReminderKey      string `db:"reminder_key"`
	RecipientIDsJSON string `db:"recipient_ids_json"`
	Status           string `db:"status"`
}

type projectedReminder struct {
	Key        string
	BlockID    string
	CreatorID  string
	Recipients []string
	DateValue  string
	Preview    string
	DueAt      time.Time
}

// reminderQueueSeenStoreKey marks an app whose database has the reminder
// queue.
const reminderQueueSeenStoreKey = "leafenReminderQueueSeen"

type storedReminderDirtyPage struct {
	PageID  string `db:"page_id"`
	Version int    `db:"version"`
}

func reminderID(pageID, key string) string {
	digest := sha256.Sum256([]byte("reminder\x00" + pageID + "\x00" + key))
	return hex.EncodeToString(digest[:16])
}

// markReminderPagesDirty queues pages whose content changed so their
// reminders are projected again. A database page's body is read through its
// row's own queue.
func markReminderPagesDirty(ctx context.Context, app core.App, pageIDs []string, now time.Time) error {
	if len(pageIDs) == 0 {
		return nil
	}
	// Migrations older than the reminder queue also mark pages for search;
	// once a database has the queue it keeps it, so the app asks until it
	// sees it (2026-10-17: asked at every write).
	if !app.Store().Has(reminderQueueSeenStoreKey) {
		var queues int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM [[sqlite_master]] WHERE [[type]] = 'table' AND [[name]] = {:table}
		`).WithContext(ctx).Bind(dbx.Params{"table": reminderDirtyPagesTable}).Row(&queues); err != nil || queues == 0 {
			return err
		}
		app.Store().Set(reminderQueueSeenStoreKey, true)
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_reminder_dirty_pages}} ([[page_id]], [[version]], [[enqueued_at]])
		SELECT [[page]].[[id]], 1, {:now}
		FROM json_each({:pageIds}) [[wanted]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[wanted]].[[value]]
		WHERE TRUE
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[version]] = [[leafen_reminder_dirty_pages]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]]
	`).WithContext(ctx).Bind(dbx.Params{"pageIds": string(encoded), "now": now.UnixMilli()}).Execute()
	if err == nil {
		notifyReminderRunnerAfterCommit(app)
	}
	return err
}

// projectPageReminders reads a page's block text and keeps one reminder row
// per reminder on its date mentions. A reminder keeps its setter and delivery
// while its date, time zone and offset stay the same. It reminds the people
// @mentioned in the same block, or else whoever set it. The same pass
// projects the people the page's text @mentions and the pages it mentions
// or links to.
func projectPageReminders(ctx context.Context, app core.App, dirty storedReminderDirtyPage, now time.Time) error {
	desired, mentions, pageTargets, workspaceID, title, err := collectPageReminders(ctx, app, dirty.PageID)
	if err != nil {
		return err
	}
	return app.RunInTransaction(func(txApp core.App) error {
		existing := []storedReminderRow{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]], [[reminder_key]], [[recipient_ids_json]], [[status]]
			FROM {{leafen_reminders}} WHERE [[page_id]] = {:pageId}
		`).WithContext(ctx).Bind(dbx.Params{"pageId": dirty.PageID}).All(&existing); err != nil {
			return err
		}
		known := make(map[string]storedReminderRow, len(existing))
		for _, row := range existing {
			known[row.ReminderKey] = row
			// Database date property reminders are kept by the items they are on.
			if _, keep := desired[row.ReminderKey]; keep ||
				strings.HasPrefix(row.ReminderKey, databaseDateReminderKeyPrefix) {
				continue
			}
			if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_reminders}} WHERE [[id]] = {:id}`).
				WithContext(ctx).Bind(dbx.Params{"id": row.ID}).Execute(); err != nil {
				return err
			}
		}
		for key, reminder := range desired {
			recipients, err := json.Marshal(reminder.Recipients)
			if err != nil {
				return err
			}
			if row, found := known[key]; found {
				if row.Status == "pending" {
					if _, err := txApp.DB().NewQuery(`
						UPDATE {{leafen_reminders}}
						SET [[recipient_ids_json]] = {:recipients}, [[preview]] = {:preview}, [[updated_at]] = {:now}
						WHERE [[id]] = {:id} AND ([[recipient_ids_json]] != {:recipients} OR [[preview]] != {:preview})
					`).WithContext(ctx).Bind(dbx.Params{
						"id": row.ID, "recipients": string(recipients), "preview": reminder.Preview,
						"now": now.UnixMilli(),
					}).Execute(); err != nil {
						return err
					}
				}
				continue
			}
			status := "pending"
			if reminder.DueAt.Before(now.Add(-reminderPastTolerance)) {
				status = "past"
			}
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_reminders}} (
					[[id]], [[workspace_id]], [[page_id]], [[block_id]], [[reminder_key]], [[creator_id]],
					[[recipient_ids_json]], [[date_value]], [[preview]], [[due_at]], [[status]],
					[[created_at]], [[updated_at]]
				) VALUES (
					{:id}, {:workspaceId}, {:pageId}, {:blockId}, {:key}, {:creatorId},
					{:recipients}, {:dateValue}, {:preview}, {:dueAt}, {:status}, {:now}, {:now}
				)
			`).WithContext(ctx).Bind(dbx.Params{
				"id": reminderID(dirty.PageID, key), "workspaceId": workspaceID, "pageId": dirty.PageID,
				"blockId": reminder.BlockID, "key": key, "creatorId": reminder.CreatorID,
				"recipients": string(recipients), "dateValue": reminder.DateValue, "preview": reminder.Preview,
				"dueAt": max(reminder.DueAt.UnixMilli(), 0), "status": status, "now": now.UnixMilli(),
			}).Execute(); err != nil {
				return err
			}
		}
		if workspaceID != "" {
			if err := projectPagePersonMentions(ctx, txApp, dirty.PageID, workspaceID, title, mentions, now); err != nil {
				return err
			}
			if err := projectPageTextReferences(ctx, txApp, dirty.PageID, pageTargets); err != nil {
				return err
			}
		}
		_, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_reminder_dirty_pages}} WHERE [[page_id]] = {:pageId} AND [[version]] = {:version}
		`).WithContext(ctx).Bind(dbx.Params{"pageId": dirty.PageID, "version": dirty.Version}).Execute()
		if err == nil && len(desired) > 0 {
			notifyReminderRunnerAfterCommit(txApp)
		}
		return err
	})
}

func collectPageReminders(
	ctx context.Context, app core.App, pageID string,
) (map[string]projectedReminder, map[string]blockPersonMentions, map[string][]string, string, string, error) {
	desired := map[string]projectedReminder{}
	mentions := map[string]blockPersonMentions{}
	pageTargets := map[string][]string{}
	var page struct {
		WorkspaceID  string `db:"workspace"`
		Title        string `db:"title"`
		LastEditedBy string `db:"last_edited_by"`
		Template     bool   `db:"database_template"`
	}
	err := app.DB().NewQuery(`
		SELECT [[workspace]], [[title]], [[last_edited_by]], [[database_template]] FROM {{leafen_pages}} WHERE [[id]] = {:pageId}
	`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID}).One(&page)
	if errors.Is(err, sql.ErrNoRows) {
		return desired, mentions, pageTargets, "", "", nil
	}
	if err != nil {
		return nil, nil, nil, "", "", err
	}
	// A template's body reminds and mentions nobody until a page is made
	// from it.
	if page.Template {
		return desired, mentions, pageTargets, page.WorkspaceID, page.Title, nil
	}
	doc, found, err := loadYgoDoc(app, pageID)
	if err != nil || !found {
		return desired, mentions, pageTargets, page.WorkspaceID, page.Title, err
	}
	// A reminder without a time zone of its own fires in its setter's, read
	// only for a block with such a reminder (2026-10-17: read for every block,
	// it was a seventh of a block write's statements).
	zones := map[string]string{}
	zoneOf := func(userID string) func() string {
		return func() string {
			zone, known := zones[userID]
			if !known {
				zone = loadAccountTimeZone(app, userID)
				zones[userID] = zone
			}
			return zone
		}
	}
	cursor := ""
	for {
		blocks := []storedReminderBlock{}
		if err := app.DB().NewQuery(`
			SELECT [[id]], [[type]], [[last_edited_by]],
				CASE WHEN instr([[content]], '"mention"') > 0 OR instr([[content]], '"link"') > 0
					OR ([[type]] = 'to_do' AND (instr([[content]], '"dateReminder"') > 0 OR instr([[content]], '"assignees"') > 0))
					THEN [[content]] ELSE '' END AS [[mention_content]]
			FROM {{leafen_blocks}}
			WHERE [[page_id]] = {:pageId} AND [[id]] > {:cursor}
			ORDER BY [[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"pageId": pageID, "cursor": cursor, "limit": reminderBlockWindow,
		}).All(&blocks); err != nil {
			return nil, nil, nil, "", "", err
		}
		// A block's mentions and reminders are from whoever last edited it —
		// its text or its structure, as its block menu names them — not only
		// its row's last structural change (2026-10-26: a mention typed into
		// someone else's block was taken for theirs). Asked only for the blocks
		// that mention someone or a date.
		texts := make([]*crdt.YText, len(blocks))
		assigned := make([][]string, len(blocks))
		mentioning := []string{}
		for index, block := range blocks {
			texts[index] = blockMentionText(doc, block)
			assigned[index] = todoBlockAssignees(block)
			if blockTextMentions(texts[index]) || len(assigned[index]) > 0 {
				mentioning = append(mentioning, block.ID)
			}
		}
		edits, err := pageBlockLastEdits(app, pageID, mentioning)
		if err != nil {
			return nil, nil, nil, "", "", err
		}
		for index, block := range blocks {
			creator := edits[block.ID].ActorID
			// The integration that made that edit, if one did, names its
			// mentions' notices (noticeConnection).
			connection := noticeConnection(edits[block.ID].Connection)
			if creator == "" {
				creator = block.LastEditedBy
			}
			if creator == "" {
				creator = page.LastEditedBy
			}
			text := texts[index]
			for _, reminder := range blockTextReminders(text, block.ID, creator, zoneOf(creator)) {
				desired[reminder.Key] = reminder
			}
			// A to-do's due date reminds; turned into another block, it keeps the date but shows and reminds of none.
			if block.Type == graphBlockTypeToDo {
				for _, reminder := range todoDueReminders(block.Content, text, block.ID, creator, zoneOf(creator)) {
					desired[reminder.Key] = reminder
				}
			}
			if targets := blockTextPageTargets(text); len(targets) > 0 {
				pageTargets[block.ID] = targets
			}
			// A to-do's assignees are told as its mentions are, as Notion's
			// "〈이름〉 다음에서 나를 멘션함" for a to-do assigned by its button (seen 2026-11-09).
			if people := sortedUnion(blockTextPeople(text), assigned[index]); len(people) > 0 {
				mentions[block.ID] = blockPersonMentions{
					Editor: creator, Users: people, Connection: connection,
					Preview: notificationEmailLine(textAsItStands(text), notificationEmailPreviewRunes),
				}
			}
		}
		if len(blocks) < reminderBlockWindow {
			return desired, mentions, pageTargets, page.WorkspaceID, page.Title, nil
		}
		cursor = blocks[len(blocks)-1].ID
	}
}

// blockMentionText is the text a block's reminders, mentions and links are
// read from: its own, and after it the table cells and caption its content
// keeps, which remind, mention and link just the same, as Notion's (seen
// 2026-11-05: a reminder picked in a cell took the cell's whole write with it).
func blockMentionText(doc *crdt.Doc, block storedReminderBlock) *crdt.YText {
	text := doc.GetText(ygoBlockTextRootPrefix + block.ID)
	if block.Content == "" {
		return text
	}
	content := struct {
		Cells   [][]graphTextSpan `json:"cells"`
		Caption []graphTextSpan   `json:"caption"`
	}{}
	if json.Unmarshal([]byte(block.Content), &content) != nil {
		return text
	}
	spans := graphTextSpansFromYText(text)
	for _, words := range append(content.Cells, content.Caption) {
		if len(spans) > 0 && len(words) > 0 {
			spans = append(spans, graphTextSpan{Text: " "})
		}
		spans = append(spans, words...)
	}
	scratch := crdt.New()
	replaceNotionBlockResponseText(scratch, block.ID, spans)
	return scratch.GetText(ygoBlockTextRootPrefix + block.ID)
}

// blockTextMentions says whether a block's text @mentions a person or a date.
func blockTextMentions(text *crdt.YText) bool {
	if text == nil {
		return false
	}
	for _, delta := range textDeltaAsItStands(text.ToDelta(), false) {
		if delta.Op != crdt.DeltaOpInsert {
			continue
		}
		if mention, _ := delta.Attributes["mention"].(string); mention == "person" || mention == "date" {
			return true
		}
	}
	return false
}

// blockTextReminders finds the reminders on one block's date mentions and
// the people mentioned alongside them; a mention without a time zone is
// placed in the fallback one, asked for only then.
func blockTextReminders(text *crdt.YText, blockID, creatorID string, fallbackZone func() string) []projectedReminder {
	if text == nil {
		return nil
	}
	people := map[string]struct{}{}
	type found struct{ date, timeZone, reminder string }
	dates := []found{}
	for _, delta := range textDeltaAsItStands(text.ToDelta(), false) {
		if delta.Op != crdt.DeltaOpInsert {
			continue
		}
		mention, _ := delta.Attributes["mention"].(string)
		switch mention {
		case "person":
			if userID, ok := delta.Attributes["userId"].(string); ok && validCommentIdentifier(userID) {
				people[userID] = struct{}{}
			}
		case "date":
			date, _ := delta.Attributes["date"].(string)
			timeZone, _ := delta.Attributes["dateTimeZone"].(string)
			reminder, _ := delta.Attributes["dateReminder"].(string)
			if reminder != "" && validDateMentionReminder(date, reminder) {
				dates = append(dates, found{date: date, timeZone: timeZone, reminder: reminder})
			}
		}
	}
	recipients := make([]string, 0, len(people))
	for userID := range people {
		recipients = append(recipients, userID)
	}
	sort.Strings(recipients)
	if len(recipients) == 0 && creatorID != "" {
		recipients = []string{creatorID}
	}
	preview := notificationEmailLine(textAsItStands(text), notificationEmailPreviewRunes)
	occurrences := map[string]int{}
	reminders := make([]projectedReminder, 0, len(dates))
	for _, date := range dates {
		zone := date.timeZone
		if zone == "" {
			zone = fallbackZone()
		}
		due, ok := dateMentionReminderDue(date.date, zone, date.reminder)
		if !ok || len(recipients) == 0 {
			continue
		}
		base := strings.Join([]string{blockID, date.date, zone, date.reminder}, "|")
		occurrences[base]++
		reminders = append(reminders, projectedReminder{
			Key: base + "|" + strconv.Itoa(occurrences[base]), BlockID: blockID, CreatorID: creatorID,
			Recipients: recipients, DateValue: date.date, Preview: preview, DueAt: due,
		})
	}
	return reminders
}
