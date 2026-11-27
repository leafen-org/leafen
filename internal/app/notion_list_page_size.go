package app

// notionListPageSize reads a list's page_size as Notion's API does (seen
// 2026-11-04): more than a page holds is a full page, 0 is the list's own size,
// and a negative one is an empty page that says more follow.
func notionListPageSize(size, fallback int) int {
	switch {
	case size < 0:
		return 0
	case size == 0:
		return fallback
	case size > notionBlockMaximumPageSize:
		return notionBlockMaximumPageSize
	}
	return size
}
