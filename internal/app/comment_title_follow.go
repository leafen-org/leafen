package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// followFirstTitleDiscussion has the person who starts a page's first
// discussion under its title follow every comment on the page, as Notion's
// (seen 2026-11-25 in Chrome at 1440, two tabs at different times, a guest
// writing on a Galaxy A90): the first comment under the title of a page whose
// "알림 받기" stood at "답글과 @멘션" turned it to "모든 댓글", on a page with no
// comments and on one with only block comments; a new one under the title of
// a page that already had another's there, a reply under the title, a block's
// comment and a reply on a block left it as it was. Its row keeps the time it
// began following (savePageNotificationLevel), so a block's discussion
// written before stays read (commentThreadFollow). A database page's title
// is not one Notion was seen doing this on, so only a page's is.
func followFirstTitleDiscussion(txApp core.App, target nativeCommentTarget, row storedComment, now time.Time) error {
	if target.Kind != commentTargetPage || row.BlockID != "" || row.ParentID != "" {
		return nil
	}
	var others int
	if err := txApp.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_comments}}
			WHERE [[page_id]]={:pageId} AND [[target_kind]]={:kind} AND [[target_id]]={:targetId}
				AND [[block_id]]='' AND [[parent_id]]='' AND [[id]]<>{:id} LIMIT 1)
	`).Bind(dbx.Params{
		"pageId": row.PageID, "kind": commentTargetPage, "targetId": row.TargetID, "id": row.ID,
	}).Row(&others); err != nil || others != 0 {
		return err
	}
	level, err := loadPageNotificationLevel(txApp, row.PageID, row.AuthorID)
	if err != nil || level == pageNotificationAllComments {
		return err
	}
	return savePageNotificationLevel(txApp, row.WorkspaceID, row.PageID, row.AuthorID, pageNotificationAllComments, now)
}
