package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// notionPageBodyCursor is where a read of a page's body starts: at the
// item a start_cursor names, a block or a sub-page, as an append's answer
// starts at the first block it added (notion_block_append_answer.go).
type notionPageBodyCursor struct {
	Kind string
	ID   string
}

// notionCursorComparison is how a read's items compare with where it starts:
// from an item a cursor names, or after one it leaves out.
func notionCursorComparison(including bool) string {
	if including {
		return ">="
	}
	return ">"
}

func parseNotionPageBodyCursor(app core.App, raw string) (notionPageBodyCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return notionPageBodyCursor{}, nil
	}
	if pageID, err := resolveNotionPageRecordID(app, raw); err == nil {
		return notionPageBodyCursor{Kind: "page", ID: pageID}, nil
	}
	blockID, ok := normalizeNotionUUIDRequest(raw)
	if !ok {
		return notionPageBodyCursor{}, errNotionBlockCursorInvalid
	}
	return notionPageBodyCursor{Kind: "block", ID: blockID}, nil
}

// notionPageBodyNextCursor names the next item a read of a page's body would
// give, the start_cursor that reads on from it, as Notion's API names it: a
// block's ID, or a sub-page's (seen 2026-11-21, Notion-Version 2026-03-11,
// synthetic pages under 한지테스트페이지, page_size 2 over blocks and a
// sub-page; a negative page_size names the first item). A cursor that names
// the last item a read gave, as Leafen's named it before, reads it again.
func notionPageBodyNextCursor(app, txApp core.App, current pageAuthorization, next notionPageBodyItemReference) (string, error) {
	if next.Kind == "page" {
		return notionPagePublicID(app, next.ID)
	}
	return notionBlockPublicID(txApp, current.Workspace.WorkspaceID, next.ID)
}
