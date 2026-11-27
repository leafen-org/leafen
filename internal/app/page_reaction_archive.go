package app

import (
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's or a database row's own reactions travel in a workspace archive
// with the page or the row, as a comment's travel with the comment: each
// person's emoji in the order used, the person kept by ID. They come back
// as the archive's other references to people and emoji do
// (workspaceImportArchiveReactions).

// workspaceExportPageReactionsPatch is a JSON patch for a page's or a row's
// archive value that adds its reactions, and nothing when it has none.
func workspaceExportPageReactionsPatch(targetKind, idExpression string) string {
	// Resolved reactions stay behind: they show nowhere.
	where := `[[target_kind]]='` + targetKind + `' AND [[target_id]]=` + idExpression + ` AND [[resolved_at]]=''`
	return `CASE WHEN EXISTS (SELECT 1 FROM {{leafen_page_reactions}} WHERE ` + where + `)
		THEN json_object('reactions', json((SELECT json_group_array(json_object('emoji', [[reaction]].[[emoji]], 'userId', [[reaction]].[[user_id]]))
			FROM (SELECT [[emoji]], [[user_id]] FROM {{leafen_page_reactions}} WHERE ` + where + `
				ORDER BY [[created_at]], [[user_id]], [[emoji]]) AS [[reaction]])))
		ELSE '{}' END`
}

// withWorkspaceExportPageReactions adds a page's or a row's reactions to the
// archive value valueSQL makes.
func withWorkspaceExportPageReactions(valueSQL, targetKind, idExpression string) string {
	return `json_patch(` + valueSQL + `, ` + workspaceExportPageReactionsPatch(targetKind, idExpression) + `)`
}

// insertWorkspaceImportPageReactions puts back an imported page's or row's
// own reactions, in the order they were used.
func insertWorkspaceImportPageReactions(
	app core.App, workspaceID, pageID, targetKind, targetID string, reactions []workspaceImportArchiveReaction,
) error {
	base := time.Now().UTC()
	for index, reaction := range reactions {
		if _, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_page_reactions}}
				([[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],[[user_id]],[[emoji]],[[created_at]])
			VALUES ({:workspaceId},{:pageId},{:kind},{:targetId},{:userId},{:emoji},{:at})
		`).Bind(dbx.Params{
			"workspaceId": workspaceID, "pageId": pageID, "kind": targetKind, "targetId": targetID,
			"userId": reaction.UserID, "emoji": reaction.Emoji,
			"at": base.Add(time.Duration(index) * time.Microsecond).Format("2006-01-02T15:04:05.000000000Z07:00"),
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// workspaceImportArchiveReactions are an archived page's, database page's or
// comment's reactions here: each by the account its person was paired with
// (workspace_import_archive_people.go), and a workspace emoji the archive
// brought named by its ID here, as text that mentions it names it. An emoji
// it didn't bring keeps the ID it had, as such a mention does.
func workspaceImportArchiveReactions(
	db workspaceImportArchiveQueryer, reactions []workspaceImportArchiveReaction,
) error {
	for index, reaction := range reactions {
		account, err := workspaceImportArchiveAccount(db, reaction.UserID)
		if err != nil {
			return err
		}
		if account != "" {
			reactions[index].UserID = account
		}
		emojiID, custom := strings.CutPrefix(reaction.Emoji, commentReactionCustomEmojiPrefix)
		if !custom || emojiID == "" {
			continue
		}
		target, err := workspaceImportArchiveHeldReference(db, []string{workspaceExportItemCustomEmoji}, emojiID)
		if err != nil {
			return err
		}
		reactions[index].Emoji = commentReactionCustomEmojiPrefix + target
	}
	return nil
}
