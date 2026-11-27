package app

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Emoji reactions on comments, as Notion's: a person reacts to a comment
// with an emoji once, and reacting with it again takes it back. A comment
// shows each emoji with the people who used it, in the order each emoji was
// first used. Anyone who can comment on the page can react; a reaction
// doesn't change its comment's revision, so it never refuses an edit.

type storedCommentReaction struct {
	CommentID string `db:"comment_id"`
	UserID    string `db:"user_id"`
	Emoji     string `db:"emoji"`
}

func registerCommentReactionRoutes(event *core.ServeEvent) {
	event.Router.Route(commentReactionSetMethod, apiRoot+commentReactionSetPath, commentsOfRowPage(
		refuseCommentWritesInTrash(setCommentReactionRoute), refuseCommentWritesInTrash(setDatabaseRowCommentReactionRoute),
	)).Bind(apis.BodyLimit(1024))
	event.Router.Route(databaseRowCommentReactionSetMethod, apiRoot+databaseRowCommentReactionSetPath, refuseCommentWritesInTrash(setDatabaseRowCommentReactionRoute)).
		Bind(apis.BodyLimit(1024))
	registerPageReactionRoutes(event)
}

func setCommentReactionRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentReactionInput{}
	if err := event.BindBody(&input); err != nil {
		return commentAPIError(event, commentBodyRefused("reaction request", err), persistentWriteComment)
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	response, err := setCommentReactionForTarget(
		event.App, user.Id, nativePageCommentTarget(pageID),
		event.Request.PathValue("commentId"), input,
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeCommentPage(txApp, user.Id, pageID, permissionWorkspaceWrite, "")
		},
	)
	if err != nil {
		return commentAPIError(event, err, persistentWriteComment)
	}
	return event.JSON(http.StatusOK, response)
}

func setDatabaseRowCommentReactionRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentReactionInput{}
	if err := event.BindBody(&input); err != nil {
		return databaseRowCommentAPIError(event, commentBodyRefused("reaction request", err), persistentWriteComment)
	}
	target, authorize, ok, err := databaseRowCommentMutationTarget(event, user.Id, strings.TrimSpace(event.Request.PathValue("rowId")))
	if !ok {
		return err
	}
	response, err := setCommentReactionForTarget(
		event.App, user.Id, target, event.Request.PathValue("commentId"), input, authorize,
	)
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	return event.JSON(http.StatusOK, response)
}

// setCommentReactionForTarget adds or takes back the actor's emoji on a
// comment and answers with the comment and all its reactions. Asking for
// what is already so changes nothing and answers as a duplicate.
func setCommentReactionForTarget(
	app core.App,
	actorID string,
	target nativeCommentTarget,
	commentID string,
	input commentReactionInput,
	authorize func(core.App) (commentAuthorization, error),
) (commentMutationResponse, error) {
	commentID = strings.TrimSpace(commentID)
	if !validCommentIdentifier(commentID) || !validCommentIdentifier(target.ID) || !validCommentIdentifier(target.StoragePageID) {
		return commentMutationResponse{}, errCommentInvalid
	}
	if !validCommentReactionEmoji(input.Emoji) {
		return commentMutationResponse{}, commentInvalid("emoji is one emoji, or %q and a custom emoji's id", commentReactionCustomEmojiPrefix)
	}
	response := commentMutationResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorize(txApp)
		if err != nil {
			return err
		}
		if authorization.Target != target {
			return permissionDenied()
		}
		row, err := loadCommentForTarget(txApp, target, commentID)
		if err != nil {
			return err
		}
		changed, err := storeCommentReaction(txApp, row.ID, row.WorkspaceID, actorID, input)
		if err != nil {
			return err
		}
		if changed {
			if err := advanceCommentPageRevision(txApp, target.StoragePageID); err != nil {
				return err
			}
		}
		record, err := renderComment(row)
		if err != nil {
			return err
		}
		records := []commentRecord{record}
		if err := attachCommentDetails(txApp, actorID, records); err != nil {
			return err
		}
		response = commentMutationResponse{Comment: records[0], Duplicate: !changed}
		return nil
	})
	return response, err
}

// storeCommentReaction writes one person's emoji on a comment, or takes it
// out, and says whether anything changed. A workspace emoji reacts only while
// it is the comment's workspace's; taking one back needs no emoji.
func storeCommentReaction(app core.App, commentID, workspaceID, actorID string, input commentReactionInput) (bool, error) {
	params := dbx.Params{"commentId": commentID, "userId": actorID, "emoji": input.Emoji}
	if !input.Reacted {
		result, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_comment_reactions}}
			WHERE [[comment_id]]={:commentId} AND [[user_id]]={:userId} AND [[emoji]]={:emoji}
		`).Bind(params).Execute()
		if err != nil {
			return false, err
		}
		removed, err := result.RowsAffected()
		return removed > 0, err
	}
	var own struct {
		Exists int `db:"existing"`
	}
	if err := app.DB().NewQuery(`
		SELECT COALESCE(SUM([[emoji]]={:emoji}),0) AS [[existing]]
		FROM {{leafen_comment_reactions}}
		WHERE [[comment_id]]={:commentId} AND [[user_id]]={:userId}
	`).Bind(params).One(&own); err != nil {
		return false, err
	}
	if own.Exists != 0 {
		return false, nil
	}
	if emojiID, custom := strings.CutPrefix(input.Emoji, commentReactionCustomEmojiPrefix); custom {
		var found int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_custom_emojis}} WHERE [[id]]={:emojiId} AND [[workspace_id]]={:workspaceId}
		`).Bind(dbx.Params{"emojiId": emojiID, "workspaceId": workspaceID}).Row(&found); err != nil {
			return false, err
		}
		if found == 0 {
			return false, commentInvalid("emoji %q names no custom emoji of this workspace", input.Emoji)
		}
	}
	_, err := app.DB().Insert(commentReactionsTable, dbx.Params{
		"comment_id": commentID, "user_id": actorID, "emoji": input.Emoji,
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
	}).Execute()
	return err == nil, err
}

// commentReactionsBatch is how many comments' reactions one query reads: a
// page's every comment, read for an agent, would pass SQLite's limit on a
// query's values (32,766) in one.
const commentReactionsBatch = 500

// attachCommentReactions sets the reactions of comment records, read
// commentReactionsBatch comments at a time: each emoji with its people in
// the order they reacted, the emoji in the order each was first used.
func attachCommentReactions(app core.App, records []commentRecord) error {
	rows := []storedCommentReaction{}
	for start := 0; start < len(records); start += commentReactionsBatch {
		batch := records[start:min(start+commentReactionsBatch, len(records))]
		params := dbx.Params{}
		placeholders := make([]string, len(batch))
		for index, record := range batch {
			key := fmt.Sprintf("comment%d", index)
			params[key], placeholders[index] = record.ID, "{:"+key+"}"
		}
		read := []storedCommentReaction{}
		if err := app.DB().NewQuery(`
			SELECT [[comment_id]],[[user_id]],[[emoji]] FROM {{leafen_comment_reactions}}
			WHERE [[comment_id]] IN (` + strings.Join(placeholders, ",") + `)
			ORDER BY [[comment_id]],[[created_at]],[[user_id]],[[emoji]]
		`).Bind(params).All(&read); err != nil {
			return err
		}
		rows = append(rows, read...)
	}
	byComment := map[string][]commentReaction{}
	for _, row := range rows {
		reactions := byComment[row.CommentID]
		at := -1
		for index := range reactions {
			if reactions[index].Emoji == row.Emoji {
				at = index
				break
			}
		}
		if at < 0 {
			reactions = append(reactions, commentReaction{Emoji: row.Emoji, UserIDs: []string{}})
			at = len(reactions) - 1
		}
		reactions[at].UserIDs = append(reactions[at].UserIDs, row.UserID)
		byComment[row.CommentID] = reactions
	}
	for index := range records {
		records[index].Reactions = byComment[records[index].ID]
	}
	return nil
}

// A reaction with one of the workspace's own emoji names it by its ID, as a
// page icon does ("custom_emoji:<id>"), as Notion's reaction picker offers
// the workspace's emoji (seen 2026-10-01).
const commentReactionCustomEmojiPrefix = "custom_emoji:"

// validCommentReactionEmoji accepts one emoji: a pictograph or keycap with
// its skin tone, joiners, variation selectors and tags, a flag, or a
// workspace emoji by its ID.
func validCommentReactionEmoji(value string) bool {
	if value == "" || len(value) > commentReactionEmojiBytes || !utf8.ValidString(value) {
		return false
	}
	if emojiID, custom := strings.CutPrefix(value, commentReactionCustomEmojiPrefix); custom {
		return emojiID != "" && strings.Trim(emojiID, "abcdefghijklmnopqrstuvwxyz0123456789-") == ""
	}
	pictograph := false
	for _, char := range value {
		switch {
		case char == '#' || char == '*' || (char >= '0' && char <= '9'):
		case char == 0x200D || char == 0xFE0E || char == 0xFE0F || (char >= 0xE0020 && char <= 0xE007F):
		case char == 0x00A9 || char == 0x00AE || char == 0x203C || char == 0x2049 || char == 0x20E3 ||
			char == 0x2122 || char == 0x2139 || (char >= 0x2194 && char <= 0x21AA) || char == 0x24C2 ||
			(char >= 0x231A && char <= 0x23FF) || (char >= 0x25AA && char <= 0x27BF) ||
			(char >= 0x2934 && char <= 0x2935) || (char >= 0x2B05 && char <= 0x2B55) ||
			char == 0x3030 || char == 0x303D || char == 0x3297 || char == 0x3299 ||
			(char >= 0x1F000 && char <= 0x1FAFF):
			pictograph = true
		default:
			return false
		}
	}
	return pictograph
}
