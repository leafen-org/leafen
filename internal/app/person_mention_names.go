package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/reearth/ygo/crdt"
)

// A person mention reads "@" and the person's name now, which follows the
// person's renames, as Notion's mention does (2026-10-20): wherever the
// server writes a page's text out — a public page, an agent's read, the
// search index, an export. The name it was written with stands for a person
// who isn't the workspace's anymore or has no name.

const personMentionNamesBatch = 100

// personMentionNames are the names of those of userIDs who are the
// workspace's owner, members or guests, or the bots of its connections and
// personal access tokens (notion_people_value_bots.go), by ID: a bot by its
// connection's or token's name now, as Notion's mention of an integration
// reads (seen 2026-11-12: "@" and the integration's name in its export).
func personMentionNames(app core.App, workspaceID string, userIDs []string) (map[string]string, error) {
	names := map[string]string{}
	seen := map[string]bool{}
	unique := []string{}
	for _, id := range userIDs {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	for start := 0; start < len(unique); start += personMentionNamesBatch {
		chunk := unique[start:min(start+personMentionNamesBatch, len(unique))]
		params := dbx.Params{"workspaceId": workspaceID}
		placeholders := make([]string, 0, len(chunk))
		for index, id := range chunk {
			key := fmt.Sprintf("user%d", index)
			params[key] = id
			placeholders = append(placeholders, "{:"+key+"}")
		}
		rows := []struct {
			ID   string `db:"id"`
			Name string `db:"name"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[user]].[[id]], [[user]].[[name]] FROM {{users}} [[user]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = {:workspaceId}
			WHERE [[user]].[[id]] IN (` + strings.Join(placeholders, ", ") + `)
				AND ([[user]].[[id]] = [[workspace]].[[workspace_owner]] OR EXISTS (
					SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
					WHERE [[membership]].[[workspace_id]] = [[workspace]].[[id]] AND [[membership]].[[user_id]] = [[user]].[[id]]
				))
		`).Bind(params).All(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if name := strings.TrimSpace(row.Name); name != "" {
				names[row.ID] = name
			}
		}
	}
	bots, err := loadPeopleValueBots(app, workspaceID, unique)
	if err != nil {
		return nil, err
	}
	for id, bot := range bots {
		if name := strings.TrimSpace(bot.Name); name != "" {
			names[id] = name
		}
	}
	return names, nil
}

// personMentionText is a person mention's text now.
func personMentionText(names map[string]string, userID, written string) string {
	if name := names[strings.TrimSpace(userID)]; name != "" {
		return "@" + name
	}
	return written
}

// Search's words are a page's words without its page and database mentions:
// Notion's search finds no page by the title of a page or database it
// mentions, nor shows that title in a result's line or highlights, whoever
// searches — the mentioned page's reader as well (seen 2026-11-09, "Ref line
// end" for "Ref line @page end"). A mention keeps its target's title as
// written, which would name to every reader of the page a page they may not
// read (mcp_reader_mentions.go).

// searchMentionHasNoWords reports a page or database mention, whose words
// search leaves out.
func searchMentionHasNoWords(mention string) bool {
	return mention == "page" || mention == "database"
}

// searchMentionCloser says how what a page or database mention names stands
// to a reader (mention "page" or "database", and its target's ID), or, for a
// sub-page's line of its page, the sub-page (mention "page"): open to them,
// closed, or missing (mcp_reader_mentions.go).
type searchMentionCloser func(mention, targetID string) mentionReach

// searchMentionWords are a page or database mention's words for a reader
// through an API or MCP: "Untitled" for a target they can't open
// (unnamedMentionWords).
func searchMentionWords(closed searchMentionCloser, mention, pageID, databaseID, written string) string {
	if reach := searchMentionReach(closed, mention, pageID, databaseID); reach != mentionOpens {
		return unnamedMentionWords(reach, written, false)
	}
	return written
}

// spansAsRead is spans' words as a reader reads them, and whether a mention
// among them was closed to it.
func spansAsRead(closed searchMentionCloser, spans []graphTextSpan) (string, bool) {
	hid := false
	var words strings.Builder
	for _, span := range spans {
		read := searchMentionWords(closed, span.Mention, span.PageID, span.DatabaseID, span.Text)
		hid = hid || read != span.Text
		words.WriteString(read)
	}
	return words.String(), hid
}

// searchMentionClosed reports a page or database mention its reader can't
// open: closed to them, or missing.
func searchMentionClosed(closed searchMentionCloser, mention, pageID, databaseID string) bool {
	return searchMentionReach(closed, mention, pageID, databaseID) != mentionOpens
}

// searchMentionReach is how what a page or database mention names stands to
// a reader; any other mention opens.
func searchMentionReach(closed searchMentionCloser, mention, pageID, databaseID string) mentionReach {
	switch {
	case closed == nil:
		return mentionOpens
	case mention == "page" && pageID != "":
		return closed(mention, pageID)
	case mention == "database" && databaseID != "":
		return closed(mention, databaseID)
	}
	return mentionOpens
}

// searchTextReader reads a block's text as search keeps it: as it stands, a
// person mention with the person's name now, a page or database mention
// without its words, a custom emoji the workspace never had without its
// ":?:", as Notion's search line and preview say nothing for it (seen
// 2026-11-11: "w284 lineprobe ", that emoji and " omega" reads "w284
// lineprobe  omega").
func searchTextReader(app core.App, workspaceID string) func(*crdt.YText) string {
	return searchTextReaderSaying(app, workspaceID, nil)
}

// searchTextReaderSaying is searchTextReader with each date mention said by
// date, as its words where it says nothing; nil keeps their words.
func searchTextReaderSaying(app core.App, workspaceID string, date func(graphTextSpan) string) func(*crdt.YText) string {
	names := map[string]string{}
	return func(text *crdt.YText) string {
		deltas := textDeltaAsItStands(text.ToDelta(), false)
		missing := []string{}
		for _, delta := range deltas {
			if userID := personMentionDeltaUser(delta); userID != "" {
				if _, known := names[userID]; !known {
					missing = append(missing, userID)
				}
			}
		}
		if len(missing) > 0 {
			found, err := personMentionNames(app, workspaceID, missing)
			for _, userID := range missing {
				if err == nil {
					names[userID] = found[userID]
				}
			}
		}
		var result strings.Builder
		for _, delta := range deltas {
			content, ok := delta.Insert.(string)
			if !ok {
				continue
			}
			if mention, _ := delta.Attributes["mention"].(string); searchMentionHasNoWords(mention) {
				continue
			}
			if customEmojiDeltaNeverHad(delta, content) {
				continue
			}
			if userID := personMentionDeltaUser(delta); userID != "" {
				content = personMentionText(names, userID, content)
			}
			if mention, _ := delta.Attributes["mention"].(string); mention == "date" && date != nil {
				if spans := graphTextSpansFromDelta([]crdt.Delta{delta}); len(spans) == 1 && date(spans[0]) != "" {
					content = date(spans[0])
				}
			}
			result.WriteString(content)
		}
		return result.String()
	}
}

// searchSpansText is spans' words as search keeps them, a person mention
// with the person's name now, a page or database mention without its words,
// a custom emoji the workspace never had without its ":?:", a date mention in
// full in English (workspace_search_date_words.go);
// and shown, the words as a database page's title shows them in a result, a
// page or database mention's as written, or "Untitled" for one closed to the
// reader (closed; nil for the index). names keeps the names read.
func searchSpansText(
	app core.App, workspaceID string, spans []graphTextSpan, names map[string]string, closed searchMentionCloser,
) (searched, shown string) {
	missing := []string{}
	for _, span := range spans {
		if _, known := names[span.UserID]; span.Mention == "person" && span.UserID != "" && !known {
			missing = append(missing, span.UserID)
		}
	}
	if len(missing) > 0 {
		if found, err := personMentionNames(app, workspaceID, missing); err == nil {
			for _, userID := range missing {
				names[userID] = found[userID]
			}
		}
	}
	var words, read strings.Builder
	for _, span := range spans {
		text := span.Text
		if span.Mention == "person" {
			text = personMentionText(names, span.UserID, text)
		}
		read.WriteString(searchMentionWords(closed, span.Mention, span.PageID, span.DatabaseID, text))
		if !searchMentionHasNoWords(span.Mention) && !customEmojiSpanNeverHad(span) {
			words.WriteString(searchDateMentionSpanWords(span, text))
		}
	}
	return words.String(), read.String()
}

// customEmojiDeltaNeverHad reports text of a custom emoji mention of one the
// workspace never had (customEmojiSpanNeverHad).
func customEmojiDeltaNeverHad(delta crdt.Delta, content string) bool {
	if mention, _ := delta.Attributes["mention"].(string); mention != "custom_emoji" {
		return false
	}
	id, _ := delta.Attributes["customEmojiId"].(string)
	return customEmojiSpanNeverHad(graphTextSpan{Text: content, Mention: "custom_emoji", CustomEmojiID: id})
}

func personMentionDeltaUser(delta crdt.Delta) string {
	if mention, _ := delta.Attributes["mention"].(string); mention != "person" {
		return ""
	}
	userID, _ := delta.Attributes["userId"].(string)
	return strings.TrimSpace(userID)
}

// registerPersonRenameHooks makes every change of a person's name one
// rename, once it is saved, whatever made it: their settings, the
// organization's managed users or SCIM, an administrator, the account's
// deletion (personRenamed).
func registerPersonRenameHooks(app core.App) {
	app.OnRecordAfterUpdateSuccess("users").Bind(&hook.Handler[*core.RecordEvent]{
		Id: "leafen-person-rename",
		Func: func(event *core.RecordEvent) error {
			if formerName := event.Record.Original().GetString("name"); formerName != event.Record.GetString("name") {
				personRenamed(event.App, event.Record, formerName)
			}
			return event.Next()
		},
	})
}

// personRenamed follows a person's rename from formerName: People's
// automations hear of it (admitPeopleRenamedSoon) — a deleted account's
// keeps the one it had — and the search index reads again what mentions
// them, and the pages whose words hold the name they had.
func personRenamed(app core.App, user *core.Record, formerName string) {
	if user.Email() != deletedAccountEmail(user.Id) {
		admitPeopleRenamedSoon(app, user.Id)
	}
	refreshPersonMentionsSoon(app, user.Id, formerName)
}

// refreshPersonMentionsSoon has the search index read again what mentions a
// person who was renamed from formerName, in the workspaces they are in
// (refreshPersonMentions).
func refreshPersonMentionsSoon(app core.App, userID, formerName string) {
	input := afterworkInput(userID, formerName)
	productAfterworkOf(app).run(afterworkPersonMentionNames, input, func(ctx context.Context) error {
		err := refreshPersonMentions(ctx, app, userID, formerName, "")
		if err != nil && ctx.Err() == nil {
			app.Logger().Warn("leafen search keeps a renamed person's old name for now", "user", userID, "error", err)
		}
		return err
	})
}

// refreshBotMentionsSoon is the same for a bot of a workspace's connection
// renamed from formerName, whose mentions are that workspace's.
func refreshBotMentionsSoon(app core.App, workspaceID, botID, formerName string) {
	input := afterworkInput(workspaceID, botID, formerName)
	productAfterworkOf(app).run(afterworkBotMentionNames, input, func(ctx context.Context) error {
		err := refreshPersonMentions(ctx, app, botID, formerName, workspaceID)
		if err != nil && ctx.Err() == nil {
			app.Logger().Warn("leafen search keeps a renamed connection's old name for now", "bot", botID, "error", err)
		}
		return err
	})
}

// refreshPersonMentions queues for the search index the pages' blocks, database
// pages' bodies among them, that mention a renamed person; the database items
// whose text cells mention them; and the pages whose indexed words still hold
// their mention by the name they had, "@" and formerName, in workspaceID or,
// with none, every workspace they are in. The last finds the mentions the
// first doesn't keep: a bot's, and one of someone who couldn't open the page
// when it was written (projectPagePersonMentions keeps only those it tells).
func refreshPersonMentions(ctx context.Context, app core.App, userID, formerName, workspaceID string) error {
	mentions := []struct {
		PageID  string `db:"page_id"`
		BlockID string `db:"block_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[block_id]] FROM {{leafen_page_person_mentions}} WHERE [[user_id]] = {:userId}
		ORDER BY [[page_id]], [[block_id]]
	`).WithContext(ctx).Bind(dbx.Params{"userId": userID}).All(&mentions); err != nil {
		return err
	}
	blocks := map[string][]string{}
	pages := []string{}
	for _, mention := range mentions {
		if _, seen := blocks[mention.PageID]; !seen {
			pages = append(pages, mention.PageID)
		}
		blocks[mention.PageID] = append(blocks[mention.PageID], mention.BlockID)
	}
	for _, pageID := range pages {
		if err := markSearchBlocksDirtyContext(ctx, app, pageID, blocks[pageID]); err != nil {
			return err
		}
	}
	// A database item whose text cells mention them is read again.
	rows := []struct {
		WorkspaceID string `db:"workspace_id"`
		RowID       string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[entry]].[[workspace_id]], [[mention]].[[page_id]]
		FROM {{leafen_search_cell_person_mentions}} [[mention]]
		JOIN {{leafen_search_pages}} [[entry]] ON [[entry]].[[page_id]] = [[mention]].[[page_id]]
		WHERE [[mention]].[[user_id]] = {:userId} ORDER BY [[entry]].[[workspace_id]], [[mention]].[[page_id]]
	`).WithContext(ctx).Bind(dbx.Params{"userId": userID}).All(&rows); err != nil {
		return err
	}
	byWorkspace := map[string][]string{}
	workspaces := []string{}
	for _, row := range rows {
		if _, seen := byWorkspace[row.WorkspaceID]; !seen {
			workspaces = append(workspaces, row.WorkspaceID)
		}
		byWorkspace[row.WorkspaceID] = append(byWorkspace[row.WorkspaceID], row.RowID)
	}
	for _, workspaceID := range workspaces {
		if err := markDatabaseRowsSearchDirty(ctx, app, workspaceID, byWorkspace[workspaceID]); err != nil {
			return err
		}
	}
	return refreshFormerMentionWords(ctx, app, userID, formerName, workspaceID)
}

// refreshFormerMentionWords queues the pages whose indexed title or words hold
// "@" and formerName, as a mention by that name was indexed.
func refreshFormerMentionWords(ctx context.Context, app core.App, userID, formerName, workspaceID string) error {
	formerName = strings.TrimSpace(formerName)
	if formerName == "" {
		return nil
	}
	scope := `SELECT {:workspaceId} WHERE {:workspaceId} != ''`
	if workspaceID == "" {
		scope = `SELECT [[workspace_id]] FROM {{leafen_workspace_memberships}} WHERE [[user_id]] = {:userId}
			UNION SELECT [[id]] FROM {{leafen_workspaces}} WHERE [[workspace_owner]] = {:userId}`
	}
	found := []struct {
		PageID      string `db:"page_id"`
		WorkspaceID string `db:"workspace_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[workspace_id]] FROM {{leafen_search_chunks}}
		WHERE [[workspace_id]] IN (` + scope + `) AND instr([[body]], {:words}) > 0
		UNION SELECT [[page_id]], [[workspace_id]] FROM {{leafen_search_pages}}
		WHERE [[workspace_id]] IN (` + scope + `) AND (instr([[title]], {:words}) > 0 OR instr([[body]], {:words}) > 0)
		ORDER BY 1
	`).WithContext(ctx).Bind(dbx.Params{
		"userId": userID, "workspaceId": workspaceID, "words": "@" + formerName,
	}).All(&found); err != nil {
		return err
	}
	targets := make([]searchDirtyTarget, 0, len(found))
	for _, page := range found {
		targets = append(targets, searchDirtyTarget{PageID: page.PageID, WorkspaceID: page.WorkspaceID})
	}
	for start := 0; start < len(targets); start += personMentionNamesBatch {
		if err := markSearchDirtyTargets(ctx, app, targets[start:min(start+personMentionNamesBatch, len(targets))]); err != nil {
			return err
		}
	}
	return nil
}
