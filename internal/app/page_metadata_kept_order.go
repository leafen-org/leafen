package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page moved by hand — dragged in the sidebar or the library, put at the
// top of its section — shows it last edited now, by the one who moved it, as
// Notion's does (seen 2026-11-16 at 1440, twice, each in its own tab: a
// private page "1주 전" and one "1일 전" put at the top by the library's
// "개인 페이지 상단으로 이동" each read "지금", their menu's foot "오늘 오전
// 2:33"). A section turned to its order by hand keeps the order it showed,
// and the pages that take new positions only to keep it (the screen sends
// them with keepLastEdit) are not edited: Notion's pages kept their times
// as its private pages turned to "수동", by the library's question and by
// the sidebar section's "정렬 › 수동" (the same day, in two tabs: "1시간
// 전", "1일 전" and "1주 전" stayed, in the order shown). Such a write keeps
// who edited the page last and when, and is no version of the page.

// pageMetadataKeepsLastEdit reports whether a change only puts the page at
// another position, sent to keep an order shown: nothing else of the page
// changes, so it is not the page's edit.
func pageMetadataKeepsLastEdit(page *core.Record, input pageMetadataInput) bool {
	if !input.KeepLastEdit || input.Position == nil {
		return false
	}
	return input.Title == page.GetString("title") && input.TitleSpans == nil && !input.TitleEdited &&
		input.ParentID == page.GetString("parent_id") && input.TeamspaceID == nil && input.InTrash == page.GetBool("in_trash") &&
		input.IsLocked == nil && input.IsArchived == nil && input.Icon == nil && input.IconType == nil &&
		input.Cover == nil && input.CoverPosition == nil && input.Font == nil && input.SmallText == nil &&
		input.FullWidth == nil && input.BacklinksDisplay == nil && input.PageCommentsDisplay == nil &&
		input.TableOfContentsDisplay == nil && input.InlineCommentsDisplay == nil
}

// restorePageLastEdit puts back who edited a page last and when, after a
// save that only kept an order shown wrote its time anew.
func restorePageLastEdit(app core.App, page *core.Record, editedAt string, editedBy string) error {
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[updated]] = {:editedAt}, [[last_edited_by]] = {:editedBy} WHERE [[id]] = {:pageId}
	`).Bind(dbx.Params{"editedAt": editedAt, "editedBy": editedBy, "pageId": page.Id}).Execute(); err != nil {
		return err
	}
	page.Set("updated", editedAt)
	page.Set("last_edited_by", editedBy)
	return nil
}
