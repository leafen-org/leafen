package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A fetch with include_discussions names a page's discussions as Notion's
// MCP does (seen 2026-11-11 and 2026-11-22): before what stands above the
// page, a <page-discussions> with how many open discussions the page has and
// three of them in get-comments' order — those with the most comments first
// and, between as many, the page's own first, then the page's order
// (mcpPageDiscussions), resolved ones after the open ones — each with its first comments as far as they fit
// (mcpFetchShownComments; comment-count says how many are shown, as
// get-comments' total-comment-count alone counts them all;
// mcpDiscussionElement); and in its
// content, as a fetch without it does too (mcp_fetch_block_discussions.go),
// each run of words a discussion was left on — a resolved one's
// too, whose words keep its mark — in <span discussion-urls="…"> inside the
// run's marks, a run in more than one naming them all, oldest first,
// comma-joined (notionMarkdownDiscussed), each address naming the block the
// run stands in (mcp_discussion_places.go). A page without discussions has
// neither, and one with only resolved ones no <page-discussions>, as
// Notion's. A suggested edit not yet decided is counted among the open
// discussions, once for each block it changes (mcp_fetch_suggestion_count.go),
// but isn't one of the examples, and a page whose open
// discussions are all suggestions has none, its <page-discussions> empty
// (seen 2026-11-21·22 on suggestions made on Notion's screen: five
// suggestions alone discussion-count="5" with no <example-discussions>;
// two comments and five suggestions "7" with the two comments).

const mcpFetchShownDiscussions = 3

// mcpFetchedDiscussions are a page's discussions as fetch names them.
type mcpFetchedDiscussions struct {
	pageID string
	open   []notionMCPDiscussion
	// listed are the ones its examples are taken from, in get-comments'
	// order: the open ones, then the resolved ones but for suggested edits'.
	listed []notionMCPDiscussion
	// addressed are all of them, resolved ones too, by their first comments'
	// IDs.
	addressed map[string]string
	// started orders the discussions by when they were started, by their
	// first comments' IDs.
	started map[string]int
	// blocks are how many blocks each open suggested edit changes, by its
	// first comment's ID.
	blocks map[string]int
	// places are where the discussions stand, which count each once for
	// every block it marks; blockName names a block in an address as the
	// reader names it, nil by its ID.
	places    mcpDiscussionPlaces
	blockName func(string) string
}

// mcpFetchDiscussions reads a page's discussions for its fetch; none for a
// page whose comments the agent can't read.
func mcpFetchDiscussions(app core.App, authority mcpAuthority, pageID string) (*mcpFetchedDiscussions, error) {
	target, err := authorizeMCPCommentTarget(app, authority, pageID, permissionWorkspaceRead)
	if err != nil {
		return nil, nil
	}
	records, err := listAllCommentRecordsForTarget(
		app, target.Authorization.Target,
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeMCPCommentTargetCurrent(txApp, authority, target.Authorization.Target, permissionWorkspaceRead)
		},
	)
	if err != nil {
		return nil, mcpCommentError(err)
	}
	all, places, err := mcpPageDiscussions(app, authority, target, records, func(commentRecord) bool { return true }, false, false, nil)
	if err != nil {
		return nil, err
	}
	blocks, err := mcpFetchSuggestionBlocks(app, target.PageID, records)
	if err != nil {
		return nil, err
	}
	fetched := &mcpFetchedDiscussions{pageID: target.PageID, addressed: make(map[string]string, len(all)), blocks: blocks, places: places}
	suggested := map[string]bool{}
	for _, record := range records {
		if body := (suggestionThreadBody{}); record.ParentID == "" && json.Unmarshal(record.Body, &body) == nil && body.Suggestion.Kind != "" {
			suggested[record.ID] = true
		}
	}
	for _, discussion := range all {
		fetched.addressed[discussion.RootID] = discussion.DiscussionID
		if !discussion.Resolved {
			fetched.open = append(fetched.open, discussion)
		}
		if !discussion.Resolved || !suggested[discussion.RootID] {
			fetched.listed = append(fetched.listed, discussion)
		}
	}
	roots := make([]commentRecord, 0, len(all))
	for _, record := range records {
		if record.ParentID == "" {
			roots = append(roots, record)
		}
	}
	sort.Slice(roots, func(left, right int) bool {
		if roots[left].CreatedAt != roots[right].CreatedAt {
			return roots[left].CreatedAt < roots[right].CreatedAt
		}
		return roots[left].ID < roots[right].ID
	})
	fetched.started = make(map[string]int, len(roots))
	for index, root := range roots {
		fetched.started[root.ID] = index
		// One whose marks are all gone, which the list leaves out, is
		// named by the block it was left on, as an earlier version marks it.
		if _, listed := fetched.addressed[root.ID]; !listed {
			fetched.addressed[root.ID] = mcpDiscussionID(app, root.WorkspaceID, target.PageID, root.BlockID, root.ID)
		}
	}
	return fetched, nil
}

// addresses are the addresses of the page's discussions among the threads a
// run of words in a block is marked with (IDs, space-separated), oldest first
// and comma-joined, naming that block; "" for none.
func (fetched *mcpFetchedDiscussions) addresses(threads, blockID string) string {
	if blockID != "" && fetched.blockName != nil {
		blockID = fetched.blockName(blockID)
	}
	byRoot := fetched.addressed
	roots, seen := []string{}, map[string]bool{}
	for _, thread := range strings.Fields(threads) {
		if _, known := byRoot[thread]; known && !seen[thread] {
			seen[thread] = true
			roots = append(roots, thread)
		}
	}
	sort.SliceStable(roots, func(left, right int) bool { return fetched.started[roots[left]] < fetched.started[roots[right]] })
	addresses := make([]string, 0, len(roots))
	for _, root := range roots {
		addresses = append(addresses, mcpFetchAttribute(mcpDiscussionAddressAt(byRoot[root], blockID)))
	}
	return strings.Join(addresses, ",")
}

// namer is what names a page's runs of words in their discussions as its
// Markdown is read; nil for none.
func (fetched *mcpFetchedDiscussions) namer() func(threads, blockID string) string {
	if fetched == nil || len(fetched.addressed) == 0 {
		return nil
	}
	return fetched.addresses
}

// text is the <page-discussions> fetch writes before the page's ancestors,
// with the blank line after it; "" for a page without open discussions.
func (fetched *mcpFetchedDiscussions) text() string {
	if fetched == nil || len(fetched.open) == 0 {
		return ""
	}
	count := 0
	for _, discussion := range fetched.open {
		if blocks, suggested := fetched.blocks[discussion.RootID]; suggested && discussion.Kind == "suggested_edit" {
			count += blocks
		} else {
			count += fetched.places.blocks(discussion.RootID)
		}
	}
	return mcpPageDiscussionsText(fetched.pageID, count, fetched.listed)
}

// mcpPageDiscussionsText writes a <page-discussions> of what a fetch reads
// (a page, or a block, by its ID): how many open discussions it counts and
// the first three it lists — the open ones before the resolved ones, as
// get-comments orders them, a resolved one shown as resolved (seen
// 2026-11-22, worker w2345, on a page and on a block, one open and one
// resolved: discussion-count="1", both shown) — but for suggested edits;
// with the blank line after it.
func mcpPageDiscussionsText(id string, count int, listed []notionMCPDiscussion) string {
	lines := []string{
		fmt.Sprintf(`<page-discussions discussion-count="%d" url="pageDiscussions://%s">`,
			count, mcpFetchAttribute(id)),
	}
	examples := []string{}
	shown := 0
	for _, discussion := range listed {
		if shown == mcpFetchShownDiscussions {
			break
		}
		if discussion.Kind == "suggested_edit" {
			continue
		}
		shown++
		examples = append(examples, mcpDiscussionElement(discussion, mcpFetchShownComments(discussion), false)...)
	}
	if shown > 0 {
		lines = append(append(append(lines, "<example-discussions>"), examples...), "</example-discussions>")
	}
	return strings.Join(append(lines, "", "</page-discussions>", "", ""), "\n")
}
