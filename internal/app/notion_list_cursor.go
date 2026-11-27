package app

// A list of Leafen's Notion API names, as next_cursor, the first item its
// page left out, and a start_cursor reads from that item on, as Notion's
// lists do (seen 2026-11-21, Notion-Version 2026-03-11, synthetic pages
// under 한지테스트페이지: a block's comments and the file uploads list read
// one at a time — each next_cursor was the item the next read gave first —
// and a block's children, notion_page_body_cursor.go). A start_cursor that
// names an item the list doesn't hold is refused, as Notion's 400 "The
// start_cursor provided is invalid" for a deleted comment, another block's
// comment and an upload of another status. A cursor Leafen gave before,
// naming the last item a read gave, reads that item again. The custom emoji
// list is read by the same rule (Notion's own not read: its emoji are the
// real workspace's).

// notionListPage cuts a read of up to one more than a page into the page and
// the item the next read starts at, none when the read held no more.
func notionListPage[T any](items []T, pageSize int) ([]T, *T) {
	if len(items) <= pageSize {
		return items, nil
	}
	return items[:pageSize], &items[pageSize]
}
