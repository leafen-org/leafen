package app

import (
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// mcpWriteDiscussions are what an MCP write of a page's Markdown names its
// discussions with and reads them back by, as Notion's MCP writes read a
// page (notion_page_markdown_discussion_marks.go): the Markdown its
// update_content, insert_content and replace_content read holds each run of
// words a discussion was left on in <span discussion-urls="…">, by the
// addresses fetch with include_discussions writes (mcpFetchDiscussions), and
// the Markdown it writes marks the words such a span holds with the page's
// discussions it names, by their addresses' own IDs (mcpDiscussionKey). Only
// the discussions' first comments are read, not what they say. None for a
// page without discussions, or whose comments the agent can't read.
func mcpWriteDiscussions(app core.App, authority mcpAuthority, pageID string) (names func(threads, blockID string) string, threads func(string) string, err error) {
	target, err := authorizeMCPCommentTarget(app, authority, pageID, permissionWorkspaceRead)
	if err != nil {
		return nil, nil, nil
	}
	records, err := listAllCommentRecordsForTarget(
		app, target.Authorization.Target,
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeMCPCommentTargetCurrent(txApp, authority, target.Authorization.Target, permissionWorkspaceRead)
		},
	)
	if err != nil {
		return nil, nil, mcpCommentError(err)
	}
	names, threads = pageMarkdownDiscussions(records, func(root commentRecord) string {
		return mcpDiscussionID(app, root.WorkspaceID, target.PageID, root.BlockID, root.ID)
	}, nil)
	return names, threads, nil
}

// pageMarkdownDiscussions name a page's discussions, from its comments, in
// its Markdown, by the address each one's first comment gives (address),
// naming the block a span stands in as blockName does (nil by its ID), and
// read a span's names back as the threads they name, by their addresses' own
// IDs (mcpDiscussionKey), whatever page and block they name; none for a page
// without discussions.
func pageMarkdownDiscussions(
	records []commentRecord, address func(root commentRecord) string, blockName func(string) string,
) (names func(threads, blockID string) string, threads func(string) string) {
	roots := mcpDiscussionRoots(records)
	if len(roots) == 0 {
		return nil, nil
	}
	named := &mcpFetchedDiscussions{addressed: map[string]string{}, started: map[string]int{}, blockName: blockName}
	byKey := map[string]string{}
	for index, root := range roots {
		named.addressed[root.ID], named.started[root.ID] = address(root), index
		byKey[mcpDiscussionAddressKey(named.addressed[root.ID])] = root.ID
	}
	return named.namer(), func(addresses string) string {
		marked := ""
		for _, address := range strings.Split(addresses, ",") {
			if root := byKey[mcpDiscussionAddressKey(address)]; root != "" {
				marked = commentThreadsWith(marked, root)
			}
		}
		return marked
	}
}

// mcpDiscussionRoots are the first comments of a page's discussions, in the order
// they were started, as fetch orders them (mcpFetchDiscussions).
func mcpDiscussionRoots(records []commentRecord) []commentRecord {
	roots := make([]commentRecord, 0, len(records))
	for _, record := range records {
		if record.ParentID == "" {
			roots = append(roots, record)
		}
	}
	sort.SliceStable(roots, func(left, right int) bool {
		if roots[left].CreatedAt != roots[right].CreatedAt {
			return roots[left].CreatedAt < roots[right].CreatedAt
		}
		return roots[left].ID < roots[right].ID
	})
	return roots
}

// mcpDiscussionAddressKey is the discussion's own ID a discussion:// address
// ends with, or an ID given alone, without its dashes and in lower case.
func mcpDiscussionAddressKey(address string) string {
	address = strings.TrimSpace(address)
	if index := strings.LastIndex(address, "/"); index >= 0 {
		address = address[index+1:]
	}
	return strings.ToLower(strings.ReplaceAll(address, "-", ""))
}
