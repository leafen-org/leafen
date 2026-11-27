package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// notionCommentDeleteAnswer is what DELETE /v1/comments/{id} answers, as
// Notion's (measured 2026-11-21 on a synthetic page, twice, a comment and a
// reply): the comment as a GET finds it once deleted
// (serveNotionDeletedCommentRetrieve) — its parent, discussion, creation and
// author — with its words gone: rich_text [] and original_content_deleted
// true, last edited when deleted (a GET after a delete, seen three times the
// same day). Its files go with its words. A connection that may not read
// comments is told its id alone, as Notion's was the same day with 댓글 읽기
// off. It is made before the comment is deleted, while its row can still be
// read.
func notionCommentDeleteAnswer(
	event *core.RequestEvent, target notionCommentTarget, row storedComment, publicCommentID, deletedAt string,
) (any, error) {
	if !notionOAuthHasScope(target.Identity, "comments:read") {
		return map[string]any{"object": "comment", "id": strings.TrimSpace(publicCommentID)}, nil
	}
	answer, err := notionCommentObject(
		event.App, row, newNotionBlockFileCapabilityIssuer(event.Request, target.Identity), target.Identity,
		&notionCommentOriginals{},
	)
	if err != nil {
		return nil, err
	}
	answer.RichText, answer.Attachments = []map[string]any{}, nil
	answer.OriginalContentDeleted, answer.HasPendingAttachments = true, false
	answer.LastEditedTime = deletedAt
	return answer, nil
}
