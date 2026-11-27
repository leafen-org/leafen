package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// notionBlockAppendLimit is the most blocks one append adds.
const notionBlockAppendLimit = 100

// serveNotionBlockAppendCount answers an append its target or the number of
// blocks it adds settles, once its place is checked: one under a block that
// takes none is refused, an empty one too (notionBlockAppendTargetRefusal);
// more than notionBlockAppendLimit are refused, and none adds nothing and
// answers the block's or page's children as a read of them lists them, their
// first 100 with has_more and next_cursor, as Notion's does (seen
// 2026-11-21: a page, a toggle and a paragraph, with no position, at the
// start, at the end, after one of its blocks, and at Notion-Version
// 2025-09-03 after one; a block after_block names that isn't there is not
// found first) — an append that adds some answers from the first of them
// (serveNotionBlockAppendAnswer). answered is false for an append that goes on.
func serveNotionBlockAppendCount(event *core.RequestEvent, target notionBlockAppendTarget, count int) (answered bool, err error) {
	if refusal := notionBlockAppendTargetRefusal(target); refusal != "" {
		return true, notionPageValidationError(event, refusal)
	}
	if count > notionBlockAppendLimit {
		return true, notionPageValidationError(event, "An append adds at most 100 blocks.")
	}
	if count > 0 {
		return false, nil
	}
	path := strings.TrimSuffix(strings.TrimPrefix(event.Request.URL.Path, notionAPIBlocksPath+"/"), "/children")
	return true, serveNotionBlockChildren(event, notionBlockIDForDatabase(event.App, path))
}
