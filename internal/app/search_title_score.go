package app

import "strings"

// searchTitleScore ranks a title a search's keywords find, the lower the
// sooner, as Notion's search lists what it finds by its title (its API, and
// its lists of sources to link, seen 2026-11-11): the title that is the
// search itself, then one starting with it, one holding it, one holding
// every word, and last one holding only some of them
// (workspaceSearchKeywords.match tells which). Both are folded
// (foldSearchText).
func searchTitleScore(query, title string, all bool) int {
	switch {
	case query == "" || title == query:
		return 0
	case !all:
		return 4
	case strings.HasPrefix(title, query):
		return 1
	case strings.Contains(title, query):
		return 2
	default:
		return 3
	}
}
