package app

import (
	"github.com/reearth/ygo/crdt"
)

// A database page's body is its page's body: its blocks move from the row
// block table into the page block table under the row's page record, their
// text into the page's collaborative document (room = row ID), their page
// mentions into the page text references, and their file references become
// page block references. Their Notion API IDs stay. The row block table and
// the old row body documents stay behind, empty and unused, until the row
// page authority is retired.

// rowBodyStoredTextKeys are where the row block table kept a block's text;
// a page block keeps its text in the page's document instead.
var rowBodyStoredTextKeys = []string{"text", "plainText", "rich", "notionRichTextSpans"}

func docHasText(doc *crdt.Doc, blockIDs []string) bool {
	for _, blockID := range blockIDs {
		if doc.GetText(ygoBlockTextRootPrefix+blockID).Len() > 0 {
			return true
		}
	}
	return false
}
