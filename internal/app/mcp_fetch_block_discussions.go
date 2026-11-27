package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A fetch writes the words its discussions were left on in
// <span discussion-urls="…"> whether or not it is asked for the discussions
// themselves, as Notion's does (seen 2026-11-22, worker w2345, twice: a page's
// fetch and a block's without include_discussions mark them; the option adds
// the <page-discussions> alone), by the names a page's Markdown writes them
// with (mcpWriteDiscussions). A block fetched by its own ID is the page the
// fetch reads: the words of its title property, written as its line is
// (mcpPropertyMarkdown — <b>, links, code), name each discussion by its own
// ID alone, discussion://<id>, and the blocks it holds name it as their page,
// discussion://<block>/<block holding the words>/<id> (seen on a paragraph,
// a heading, a to-do, a toggle and its line inside it; a discussion on a
// block's own, marking no words, marks none).

// mcpBlockFetchDiscussions are the namers a block's fetch marks its
// discussions with: its own words by the discussion's ID, and the blocks it
// holds by addresses naming the block as their page; nil, nil for a page
// without discussions or whose comments the agent can't read.
func mcpBlockFetchDiscussions(
	app core.App, authority mcpAuthority, block storedNotionBlock,
) (own, held func(threads, blockID string) string, err error) {
	names, _, err := mcpWriteDiscussions(app, authority, block.PageID)
	if err != nil || names == nil {
		return nil, nil, err
	}
	own = func(threads, blockID string) string {
		return mcpReaddressDiscussions(names(threads, blockID), func(_, _, key string) string {
			return "discussion://" + key
		})
	}
	held = func(threads, blockID string) string {
		return mcpReaddressDiscussions(names(threads, blockID), func(_, words, key string) string {
			return "discussion://" + block.ID + "/" + words + "/" + key
		})
	}
	return own, held, nil
}

// mcpReaddressDiscussions writes each of a span's comma-joined discussion
// addresses (discussion://<page>/<block>/<id>) anew from its page, block
// and ID; one of another form stays as it is.
func mcpReaddressDiscussions(addresses string, address func(page, block, key string) string) string {
	if addresses == "" {
		return ""
	}
	parts := strings.Split(addresses, ",")
	for index, part := range parts {
		rest, named := strings.CutPrefix(part, "discussion://")
		if fields := strings.Split(rest, "/"); named && len(fields) == 3 {
			parts[index] = address(fields[0], fields[1], fields[2])
		}
	}
	return strings.Join(parts, ",")
}

// A block's fetch with include_discussions names, before what stands above
// it, the discussions on it and on the blocks it holds, as Notion's does
// (seen 2026-11-22, worker w2345, on a toggle, its line inside, a paragraph
// and a second toggle): a <page-discussions> naming the block, counting the
// open ones as a page's does — each once for every block it marks there —
// and showing three in the page's get-comments order, resolved ones after;
// each named with the block as its page (discussion://<block>/<block holding
// its words, or left on>/<id>), one left on the block itself, marking no
// words, as the page's own (context="page"), and its comments opening at the
// block (its address in place of the page's). A discussion on the page
// itself, or on other blocks, isn't one of its.

// mcpBlockFetchPageDiscussions writes the <page-discussions> of a block's
// fetch with include_discussions, with the blank line after it; "" for a
// block without open discussions on it or in it.
func mcpBlockFetchPageDiscussions(app core.App, authority mcpAuthority, block storedNotionBlock) (string, error) {
	fetched, err := mcpFetchDiscussions(app, authority, block.PageID)
	if err != nil || fetched == nil {
		return "", err
	}
	held, err := mcpBlockAndHeld(app, block)
	if err != nil {
		return "", failedBecause(errMCPToolFailed, err)
	}
	pageAddress, blockAddress := mcpPageURL(authority, block.PageID), mcpPageURL(authority, block.ID)
	count, listed := 0, []notionMCPDiscussion{}
	for _, discussion := range fetched.listed {
		in := 0
		if fetched.places.words[discussion.RootID] {
			for _, marked := range fetched.places.marked[discussion.RootID] {
				if held[marked] {
					in++
				}
			}
		} else if discussion.BlockID != "" && held[discussion.BlockID] {
			in = 1
		}
		if in == 0 {
			continue
		}
		if !discussion.Resolved {
			count += in
		}
		discussion.DiscussionID = mcpReaddressDiscussions(discussion.DiscussionID, func(_, words, key string) string {
			return "discussion://" + block.ID + "/" + words + "/" + key
		})
		if discussion.BlockID == block.ID && discussion.Context == "block" {
			discussion.Context = "page"
		}
		comments := make([]notionMCPComment, len(discussion.Comments))
		for index, comment := range discussion.Comments {
			if rest, at := strings.CutPrefix(comment.URL, pageAddress); at {
				comment.URL = blockAddress + rest
			}
			comments[index] = comment
		}
		discussion.Comments = comments
		listed = append(listed, discussion)
	}
	if count == 0 {
		return "", nil
	}
	return mcpPageDiscussionsText(block.ID, count, listed), nil
}

// mcpBlockAndHeld are a block and the blocks it holds, however deep.
func mcpBlockAndHeld(app core.App, block storedNotionBlock) (map[string]bool, error) {
	rows := []struct {
		ID       string `db:"id"`
		ParentID string `db:"parent_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]] FROM {{leafen_blocks}} WHERE [[page_id]] = {:page}
	`).Bind(dbx.Params{"page": block.PageID}).All(&rows); err != nil {
		return nil, err
	}
	children := map[string][]string{}
	for _, row := range rows {
		children[row.ParentID] = append(children[row.ParentID], row.ID)
	}
	held := map[string]bool{block.ID: true}
	for queue := []string{block.ID}; len(queue) > 0; queue = queue[1:] {
		for _, child := range children[queue[0]] {
			if !held[child] {
				held[child] = true
				queue = append(queue, child)
			}
		}
	}
	return held, nil
}
