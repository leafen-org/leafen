package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const commentNotificationPreviewRunes = 500

type commentNotificationProjection struct {
	MentionRecipientIDs []string
	Preview             string
	// Empty is a body with no text, mention or attachment: a thread begun
	// by a reaction to text.
	Empty bool
}

func commentNotificationEventID(workspaceID, sourceID string, revision int) string {
	digest := sha256.Sum256([]byte(strings.Join(
		[]string{"comment-notification", workspaceID, sourceID, strconv.Itoa(revision)}, "\x00",
	)))
	return hex.EncodeToString(digest[:16])
}

func commentNotificationBodyProjection(body json.RawMessage) (commentNotificationProjection, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return commentNotificationProjection{}, errCommentInvalid
	}
	stack := []any{root}
	mentions := map[string]struct{}{}
	texts := []string{}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch value := current.(type) {
		case []any:
			for index := len(value) - 1; index >= 0; index-- {
				stack = append(stack, value[index])
			}
		case map[string]any:
			if value["mention"] == "person" {
				if userID, ok := value["userId"].(string); ok && validCommentIdentifier(userID) {
					mentions[userID] = struct{}{}
				}
			}
			// The spaces between two mentions are spans of their own; the
			// preview reads as written (2026-11-05: "@가@나").
			if text, ok := value["text"].(string); ok {
				texts = append(texts, text)
			}
			keys := make([]string, 0, len(value))
			for key := range value {
				if key != "text" && key != "mention" && key != "userId" {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			for index := len(keys) - 1; index >= 0; index-- {
				stack = append(stack, value[keys[index]])
			}
		}
	}
	mentionIDs := make([]string, 0, len(mentions))
	for userID := range mentions {
		mentionIDs = append(mentionIDs, userID)
	}
	sort.Strings(mentionIDs)
	previewRunes := []rune(strings.TrimSpace(strings.Join(texts, "")))
	if len(previewRunes) > commentNotificationPreviewRunes {
		previewRunes = previewRunes[:commentNotificationPreviewRunes]
	}
	preview := string(previewRunes)
	// A suggestion's thread has its suggestion rather than text.
	suggestion := false
	if object, ok := root.(map[string]any); ok {
		_, suggestion = object["suggestion"]
	}
	empty := preview == "" && len(mentionIDs) == 0 && !suggestion
	if preview == "" {
		preview = "New comment"
	}
	if empty {
		attachments, err := commentAttachmentIDs(body)
		if err != nil {
			return commentNotificationProjection{}, err
		}
		empty = len(attachments) == 0
	}
	return commentNotificationProjection{MentionRecipientIDs: mentionIDs, Preview: preview, Empty: empty}, nil
}

func commentNotificationRecipients(actorID string, values ...string) []string {
	unique := map[string]struct{}{}
	for _, value := range values {
		if value != actorID && validCommentIdentifier(value) {
			unique[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// commentNotificationRecipientBytes is the most a notification's recipient
// list holds, as its table takes (comment_notification_schema.go).
const commentNotificationRecipientBytes = 65536

func notificationRecipientJSON(values []string) (string, error) {
	payload, err := json.Marshal(values)
	if err != nil || len(payload) > commentNotificationRecipientBytes {
		return "", failedBecause(errCommentFailed, err)
	}
	return string(payload), nil
}

func insertCommentNotificationEvent(
	app core.App,
	authorization commentAuthorization,
	row storedComment,
	audienceAt int64,
) error {
	projection, err := commentNotificationBodyProjection(json.RawMessage(row.BodyJSON))
	if err != nil {
		return err
	}
	// A reaction to text begins its thread with an empty comment, which
	// notifies no one, as reactions don't.
	if projection.Empty && row.ParentID == "" {
		return nil
	}
	parentAuthorID := ""
	if row.ParentID != "" {
		parent, err := loadCommentForTarget(app, nativeCommentTarget{
			Kind: row.TargetKind, ID: row.TargetID, StoragePageID: row.PageID,
		}, row.ParentID)
		if err != nil {
			return err
		}
		parentAuthorID = parent.AuthorID
	}
	// Thread participants and the page's followers get a delivery even when
	// they are not workspace members, every one of them: materialization
	// reads them after the fixed recipients (commentNotificationFollowersAfter).
	// Delivery decides what is personal.
	fixedRecipientIDs := commentNotificationRecipients(
		row.AuthorID,
		authorization.Page.Workspace.OwnerID,
		authorization.Page.Page.GetString("created_by"),
		authorization.Page.Page.GetString("last_edited_by"),
		parentAuthorID,
	)
	return admitCommentNotificationEvent(
		app, authorization, row, row.AuthorID, projection.Preview,
		projection.MentionRecipientIDs, fixedRecipientIDs, true, audienceAt, row.CreatedAt,
	)
}

// insertCommentEditMentionEvent tells the people an edit newly mentions, as
// a new comment's mention does (2026-10-29: an added mention told no one
// while a removed one was taken back). Whom the comment named before, or
// was ever told of as a mention, is not told again: a mention taken out and
// put back shows its first notification again (notification_inbox.go).
// Neither the thread's followers nor the page's audience hear of an edit.
func insertCommentEditMentionEvent(
	app core.App,
	authorization commentAuthorization,
	row storedComment,
	actorID, previousBody string,
	editedAt int64,
) error {
	before, err := commentNotificationBodyProjection(json.RawMessage(previousBody))
	if err != nil {
		return err
	}
	after, err := commentNotificationBodyProjection(json.RawMessage(row.BodyJSON))
	if err != nil {
		return err
	}
	told := map[string]struct{}{}
	for _, userID := range before.MentionRecipientIDs {
		told[userID] = struct{}{}
	}
	added := func() []string {
		result := []string{}
		for _, userID := range after.MentionRecipientIDs {
			if _, ok := told[userID]; !ok && userID != actorID {
				result = append(result, userID)
			}
		}
		return result
	}
	if len(added()) == 0 {
		return nil
	}
	earlier := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[mention_recipient_ids_json]] FROM {{leafen_notification_events}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[source_kind]] = 'comment' AND [[source_id]] = {:commentId}
	`).Bind(dbx.Params{"workspaceId": row.WorkspaceID, "commentId": row.ID}).Column(&earlier); err != nil {
		return err
	}
	for _, payload := range earlier {
		values := []string{}
		if err := json.Unmarshal([]byte(payload), &values); err != nil {
			return err
		}
		for _, userID := range values {
			told[userID] = struct{}{}
		}
	}
	// Events are pruned a week after delivery; the notifications they made
	// are kept.
	notified := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[user_id]] FROM {{leafen_notifications}}
		WHERE [[page_id]] = {:pageId} AND [[comment_id]] = {:commentId}
			AND [[activity_key]] LIKE 'mention:comment:%'
	`).Bind(dbx.Params{"pageId": row.PageID, "commentId": row.ID}).Column(&notified); err != nil {
		return err
	}
	for _, userID := range notified {
		told[userID] = struct{}{}
	}
	recipients := added()
	if len(recipients) == 0 {
		return nil
	}
	return admitCommentNotificationEvent(
		app, authorization, row, actorID, after.Preview, recipients, []string{}, false, editedAt, row.UpdatedAt,
	)
}

func admitCommentNotificationEvent(
	app core.App,
	authorization commentAuthorization,
	row storedComment,
	actorID, preview string,
	mentionRecipientIDs, fixedRecipientIDs []string,
	includePageAudience bool,
	audienceAt int64,
	createdAt string,
) error {
	mentionJSON, err := notificationRecipientJSON(mentionRecipientIDs)
	if err != nil {
		return err
	}
	fixedJSON, err := notificationRecipientJSON(fixedRecipientIDs)
	if err != nil {
		return err
	}
	// An untitled page stays so, named in each reader's language where shown.
	// A comment on a database item names the item, as Notion's does (seen
	// 2026-11-09; notification_item_now.go).
	title := strings.TrimSpace(authorization.Page.Page.GetString("title"))
	if row.TargetKind == "database_row" && row.TargetID != "" {
		title = databaseRowPageTitle(app, row.TargetID)
	}
	logicalPageID := row.TargetID
	if logicalPageID == "" {
		logicalPageID = row.PageID
	}
	target := "/p/" + url.PathEscape(logicalPageID) + "#comment-" + url.PathEscape(row.ID)
	_, err = app.DB().Insert(notificationEventsTable, dbx.Params{
		"id":           commentNotificationEventID(row.WorkspaceID, row.ID, row.Revision),
		"workspace_id": row.WorkspaceID, "source_kind": "comment",
		"source_id": row.ID, "source_revision": row.Revision, "actor_id": actorID,
		"page_id": row.PageID, "block_id": row.BlockID, "parent_comment_id": row.ParentID,
		"title": notificationEventTitle(title), "preview": preview, "target": target,
		"mention_recipient_ids_json": mentionJSON, "fixed_recipient_ids_json": fixedJSON,
		"include_page_audience": includePageAudience, "audience_at": audienceAt, "status": "pending",
		"created_at": createdAt, "updated_at": createdAt,
	}).Execute()
	if err == nil {
		notifyCommentNotificationMaterializationAfterCommit(app)
	}
	return err
}
