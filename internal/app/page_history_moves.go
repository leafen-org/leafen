package app

import "github.com/pocketbase/pocketbase/core"

// A block the editor sends moved only because blocks were added or deleted
// before it keeps its place among the blocks that were there: it isn't a
// move in the page's history or its updates (2026-10-19: a line typed above
// a sub-page put "블록을 이동했습니다" in the workspace updates; the editor
// moves the block after a new one, as a new block can't be placed first
// otherwise).

// Nor is a block moved because the one before it went under another block
// or came out from under one: the editor sends the block after it moved too,
// to stay after the same block, and its next sibling showed "moved" in the
// history and in its first edit time (2026-11-14: a line indented with Tab).

// pageHistoryMoveReader tells a real move from such a shift, reading the
// deleted blocks' places the first time one is needed.
type pageHistoryMoveReader struct {
	app          core.App
	pageID       string
	desiredAfter map[string]string
	open         map[string]pageHistoryOpenBlockSpan
	// current are the blocks as the change leaves them, among them every
	// block it put somewhere.
	current map[string]pageHistoryProjectionBlock
	deleted map[string]struct{}
	gone    map[string]pageHistoryOpenBlockSpan
}

// stays says a block the change leaves under the parent it was under: one it
// didn't touch, or one it changed in place.
func (reader *pageHistoryMoveReader) stays(id string) bool {
	after, changed := reader.current[id]
	if !changed {
		return true
	}
	was, existed := reader.open[id]
	return existed && was.ParentID == after.ParentID
}

// moved says the block's parent changed, or the block before it among those
// that were there before the change and are there after it, each under the
// parent it was under.
func (reader *pageHistoryMoveReader) moved(id string, block pageHistoryProjectionBlock) (bool, error) {
	previous, existed := reader.open[id]
	if !existed || previous.ParentID != block.ParentID {
		return true, nil
	}
	// Before it now: past the blocks this change made or brought in from
	// under another parent.
	now, seen := reader.desiredAfter[id], map[string]bool{}
	for now != "" && !seen[now] {
		if reader.stays(now) {
			break
		}
		seen[now] = true
		now = reader.desiredAfter[now]
	}
	// Before it then: past the blocks this change deleted or took to
	// another parent.
	then := previous.AfterID
	for then != "" && !seen[then] {
		if _, deleted := reader.deleted[then]; !deleted && !reader.stays(then) {
			seen[then] = true
			then = reader.open[then].AfterID
			continue
		}
		if _, deleted := reader.deleted[then]; !deleted {
			break
		}
		if reader.gone == nil {
			blocks := make(map[string]pageHistoryProjectionBlock, len(reader.deleted))
			for deletedID := range reader.deleted {
				blocks[deletedID] = pageHistoryProjectionBlock{}
			}
			gone, err := loadPageHistoryOpenBlockSpans(reader.app, reader.pageID, blocks)
			if err != nil {
				return false, err
			}
			reader.gone = gone
		}
		seen[then] = true
		then = reader.gone[then].AfterID
	}
	return now != then, nil
}
