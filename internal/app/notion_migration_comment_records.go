package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration's Notion comments, written as Leafen comments: each marked with
// who wrote it in Notion and when, each kept in the migration's ledger so a
// run again doesn't move it twice.

// createNotionMigrationComments writes one page's Notion comments, each
// discussion a thread in the order it was written. A comment the migration
// moved before — by an earlier run, or this one before a restart — isn't
// moved again, even if it was deleted in Leafen since; the ledger keeps each.
func createNotionMigrationComments(
	app core.App, job storedJob, migrationID, pageID, blockID string, row bool, comments []notionProviderComment, people func(string) string,
	members map[string]string, fromBundle bool,
) (int, error) {
	sort.SliceStable(comments, func(i, j int) bool { return comments[i].CreatedTime < comments[j].CreatedTime })
	moved, err := notionMigrationMovedComments(app, migrationID, comments)
	if err != nil {
		return 0, err
	}
	threads, started, created := map[string]string{}, map[string]bool{}, 0
	location := personLocation(app, job.Owner)
	resolver := &notionImportReferenceResolver{app: app, jobID: job.ID, resolved: map[string]string{}}
	for _, comment := range comments {
		root := !started[comment.DiscussionID]
		started[comment.DiscussionID] = true
		if native, done := moved[comment.ID]; done {
			// A thread whose first comment is still in Leafen goes on under it.
			if root {
				var present int
				if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_comments}} WHERE [[id]]={:id}`).
					Bind(dbx.Params{"id": native}).Row(&present); err != nil {
					return created, err
				}
				if present > 0 {
					threads[comment.DiscussionID] = native
				}
			}
			continue
		}
		// A bundle's comment says who wrote it in its tool already, in its
		// first line; the source's Leafen author is whoever imported it.
		first := ""
		if !fromBundle {
			first = notionMigrationCommentText(comment, people, "", location)
		}
		spans, err := notionMigrationCommentSpans(resolver, members, first, comment.RichText)
		if err != nil {
			return created, err
		}
		// The body as a comment's own: its text runs under "rich" (2026-10-13:
		// a list of runs, which nothing reads, showed every moved comment
		// empty), each with its formatting (notion_migration_comment_spans.go).
		body, err := json.Marshal(map[string]any{"rich": spans})
		if err != nil {
			return created, err
		}
		digest := sha256.Sum256([]byte("notion-migration-comment\x00" + migrationID + "\x00" + comment.ID))
		id := "notion-" + hex.EncodeToString(digest[:12])
		input := commentCreateInput{ID: id, RequestID: id, Body: body, ParentID: threads[comment.DiscussionID]}
		// A thread's first comment stands on its block; its replies follow it.
		if input.ParentID == "" {
			input.BlockID = blockID
		}
		var made int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_comments}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": id}).Row(&made); err != nil {
			return created, err
		}
		switch {
		case made > 0:
			// One made before the ledger could keep it is kept now, not made
			// again.
		case row:
			authorization, _, authErr := authorizeDatabaseRowCommentTarget(app, job.Owner, pageID, permissionWorkspaceWrite)
			if authErr != nil {
				return created, authErr
			}
			_, err = createCommentRecordForTarget(app, job.Owner, authorization.Target, input,
				func(txApp core.App) (commentAuthorization, error) {
					authorization, _, err := authorizeDatabaseRowCommentTarget(txApp, job.Owner, pageID, permissionWorkspaceWrite)
					return authorization, err
				})
		default:
			_, err = createCommentRecord(app, job.Owner, pageID, input)
		}
		if err != nil {
			return created, err
		}
		if _, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_notion_migration_ledger}}
				([[migration_id]],[[workspace_id]],[[source_id]],[[kind]],[[native_id]])
			VALUES ({:migrationId},{:workspaceId},{:sourceId},{:kind},{:nativeId})
		`).Bind(dbx.Params{
			"migrationId": migrationID, "workspaceId": job.WorkspaceID, "sourceId": comment.ID,
			"kind": notionMigrationLedgerComment, "nativeId": id,
		}).Execute(); err != nil {
			return created, err
		}
		if threads[comment.DiscussionID] == "" {
			threads[comment.DiscussionID] = id
		}
		created++
	}
	return created, nil
}

// notionMigrationMovedComments finds which of a page's comments the
// migration moved before, and what each became.
func notionMigrationMovedComments(app core.App, migrationID string, comments []notionProviderComment) (map[string]string, error) {
	ids := make([]string, 0, len(comments))
	for _, comment := range comments {
		ids = append(ids, comment.ID)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		SourceID string `db:"source_id"`
		NativeID string `db:"native_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]],[[native_id]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]={:kind}
			AND [[source_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"migrationId": migrationID, "kind": notionMigrationLedgerComment, "ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	moved := make(map[string]string, len(rows))
	for _, row := range rows {
		moved[row.SourceID] = row.NativeID
	}
	return moved, nil
}

// notionMigrationCommentText marks a comment with who wrote it in Notion
// and when, since its Leafen author is the person who moved it.
func notionMigrationCommentText(comment notionProviderComment, people func(string) string, text string, location *time.Location) string {
	author := people(comment.CreatedBy.ID)
	if author == "" && comment.CreatedBy.Name != nil {
		author = *comment.CreatedBy.Name
	}
	if author == "" {
		author = comment.DisplayName.ResolvedName
	}
	if author == "" {
		author = "Notion"
	}
	return commentAuthorLine(author, comment.CreatedTime, location) + "\n" + text
}

// commentAuthorLine names who wrote a comment outside Leafen — in Notion, in
// another tool — and when, to the minute on the clock of whoever moved it in
// (person_clock.go).
func commentAuthorLine(author, written string, location *time.Location) string {
	when := written
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.000Z"} {
		if parsed, err := time.Parse(layout, written); err == nil {
			when = personClockText(parsed, location)
			break
		}
	}
	return author + " · " + when
}
