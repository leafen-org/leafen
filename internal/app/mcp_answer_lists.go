package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// An answer to an agent says a list that grows with what the answer is about
// — a move's pages and views, a database's rows — by its groups and counts,
// and lists its items only when the call asks for that list, a window at a
// time (2026-11-21: a Notion move's status listed 310 items its agent only
// counted, as the report page folds them). A tool gives this way:
//
//   - its input takes `list` (which of its lists to go over item by item) and
//     `start_cursor`, read with mcpAnswerListingOf against the lists it has;
//   - its answer always carries each list's count and groups — the tool's own
//     words for them, or mcpAnswerGroupsOf counting by a key;
//   - only the list the call names comes item by item, as mcpAnswerWindowOf
//     cuts it, with `next_cursor` while more follow, and the answer's next
//     starts with what the window says of itself (said).
//
// What the answer's next asks the agent to act on one by one is no growing
// list: it stays in the answer.

// mcpAnswerListWindow is how many items of a list one answer gives.
const mcpAnswerListWindow = 100

// mcpAnswerListing is the list a call asks to go over item by item, and the
// item it goes on from; with no List the answer lists none.
type mcpAnswerListing struct {
	List   string
	Offset int
}

// mcpAnswerListingOf reads a call's list and start_cursor against the lists
// its tool gives; a cursor is the next_cursor a window gave, "<list>:<offset>".
func mcpAnswerListingOf(list, cursor string, lists ...string) (mcpAnswerListing, error) {
	list, cursor = strings.TrimSpace(list), strings.TrimSpace(cursor)
	if list == "" {
		if cursor != "" {
			return mcpAnswerListing{}, mcpInvalid("start_cursor goes with the list it came from: give that list too")
		}
		return mcpAnswerListing{}, nil
	}
	if !slices.Contains(lists, list) {
		named := strings.Join(lists, ", ")
		if last := len(lists) - 1; last > 0 {
			named = strings.Join(lists[:last], ", ") + " or " + lists[last]
		}
		return mcpAnswerListing{}, mcpInvalid("list is %s; left out, the answer says them by their groups and counts", named)
	}
	listing := mcpAnswerListing{List: list}
	if cursor != "" {
		named, at, found := strings.Cut(cursor, ":")
		offset, err := strconv.Atoi(at)
		if !found || named != list || err != nil || offset < 0 {
			return mcpAnswerListing{}, mcpInvalid("start_cursor is the next_cursor of the last call with the same list, as it gave it")
		}
		listing.Offset = offset
	}
	return listing, nil
}

// mcpAnswerPage is the window of one list an answer gives: its items, where
// they start in the list, how many the list has, and the cursor of the next
// window while more follow.
type mcpAnswerPage[T any] struct {
	Name       string
	Items      []T
	From       int
	Total      int
	NextCursor string
}

// mcpAnswerWindowOf is the window of the list named name that the listing
// asks for; when it asks for another list, a window of none.
func mcpAnswerWindowOf[T any](listing mcpAnswerListing, name string, items []T) mcpAnswerPage[T] {
	return mcpAnswerWindowSized(listing, name, items, mcpAnswerListWindow)
}

// mcpAnswerWindowSized is mcpAnswerWindowOf with windows of size items.
func mcpAnswerWindowSized[T any](listing mcpAnswerListing, name string, items []T, size int) mcpAnswerPage[T] {
	page := mcpAnswerPage[T]{Name: name, Total: len(items)}
	if listing.List != name {
		return page
	}
	page.From = min(listing.Offset, len(items))
	to := min(page.From+size, len(items))
	page.Items = items[page.From:to]
	if to < len(items) {
		page.NextCursor = name + ":" + strconv.Itoa(to)
	}
	return page
}

// said is what the window says of itself at the start of the answer's next.
func (page mcpAnswerPage[T]) said() string {
	switch to := page.From + len(page.Items); {
	case page.Total == 0:
		return page.Name + " has nothing to list."
	case len(page.Items) == 0:
		return fmt.Sprintf("%s has %d, all before start_cursor.", page.Name, page.Total)
	case page.NextCursor != "":
		return fmt.Sprintf("%s lists %d to %d of %d. More follow: call again with the same list and start_cursor set to next_cursor.",
			page.Name, page.From+1, to, page.Total)
	default:
		return fmt.Sprintf("%s lists %d to %d of %d.", page.Name, page.From+1, to, page.Total)
	}
}

// mcpAnswerGroup is one group of a list as an answer counts it: the key its
// items share and how many things they stand for.
type mcpAnswerGroup struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// mcpAnswerGroupsOf counts a list by a key, the groups in the order their
// first item came; weight is how many things an item stands for (nil: one).
func mcpAnswerGroupsOf[T any](items []T, key func(T) string, weight func(T) int) []mcpAnswerGroup {
	groups, at := []mcpAnswerGroup{}, map[string]int{}
	for _, item := range items {
		name, count := key(item), 1
		if weight != nil {
			count = weight(item)
		}
		index, found := at[name]
		if !found {
			index, at[name] = len(groups), len(groups)
			groups = append(groups, mcpAnswerGroup{Key: name})
		}
		groups[index].Count += count
	}
	return groups
}
