package app

import (
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// view-version writes each run of words a discussion was left on in
// <span discussion-urls="…">, as fetch with include_discussions does, as
// Notion's view-version does (seen 2026-11-22 through Notion's MCP, worker
// w2328, twice): the discussions marking the page's words at that time — one
// left after the page's last edit before it too, a resolved one, and one
// deleted since, by the address it had — and not one deleted by then, whose
// words lost their mark, nor one left after. get-version-changes writes none,
// and a discussion's mark alone is no change of a block
// (mcpVersionContentAsCompared): Notion's lists none for one.

// mcpVersionMarkedAt is the version a page's words stood in at a time with
// their discussions' marks: the version at the time, or a later one that
// only marked words with a discussion left at or before the time
// (pageHistoryVersionCommentMarks), which the history's list leaves out as
// no edit.
func mcpVersionMarkedAt(app core.App, pageID string, versions mcpHistoryVersions, at time.Time) string {
	marked := ""
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_page_history_versions}}
		WHERE [[page_id]]={:pageId} AND [[source]]={:source} AND [[version_cursor]]>{:after}
			AND [[version_cursor]]<={:frontier} AND [[created_at]]<={:at}
		ORDER BY [[version_cursor]] DESC LIMIT 1
	`).Bind(dbx.Params{
		"pageId": pageID, "source": pageHistoryVersionCommentMarks, "after": versions.At.VersionCursor,
		"frontier": versions.Frontier, "at": at.UnixMilli(),
	}).Row(&marked); err != nil || marked == "" {
		return versions.At.ID
	}
	return marked
}

// mcpVersionDiscussionNamer names the discussions a page's version marks
// its words with: those the page has, resolved ones too, as fetch names
// them, and a deleted first comment's (leafen_comment_tombstones) by the
// discussion it was — the one a reply took over and still stands, or, deleted
// after the time, the address it had. nil when the agent can't read the
// page's comments.
func mcpVersionDiscussionNamer(
	app core.App, authority mcpAuthority, pageID string, at time.Time, blocks []pageHistoryBlockWire,
) (func(threads, blockID string) string, error) {
	fetched, err := mcpFetchDiscussions(app, authority, pageID)
	if err != nil || fetched == nil {
		return nil, err
	}
	addressed := make(map[string]string, len(fetched.addressed))
	byKey := map[string]string{}
	for root, address := range fetched.addressed {
		addressed[root] = address
		byKey[address[strings.LastIndex(address, "/")+1:]] = address
	}
	for _, thread := range mcpVersionMarkedThreads(blocks) {
		if _, known := addressed[thread]; known {
			continue
		}
		gone := struct {
			WorkspaceID        string `db:"workspace_id"`
			BlockID            string `db:"block_id"`
			NotionDiscussionID string `db:"notion_discussion_id"`
			DeletedAt          string `db:"deleted_at"`
		}{}
		if app.DB().NewQuery(`
			SELECT [[workspace_id]],[[block_id]],[[notion_discussion_id]],[[deleted_at]]
			FROM {{leafen_comment_tombstones}} WHERE [[id]]={:id} AND [[page_id]]={:pageId}
		`).Bind(dbx.Params{"id": thread, "pageId": pageID}).One(&gone) != nil {
			continue
		}
		key := gone.NotionDiscussionID
		if key == "" {
			key = thread
			if derived, err := notionRootCommentDiscussionID(gone.WorkspaceID, thread); err == nil {
				key = derived
			}
		}
		deleted, err := types.ParseDateTime(gone.DeletedAt)
		switch {
		case byKey[key] != "":
			addressed[thread] = byKey[key]
		case err == nil && deleted.Time().After(at):
			addressed[thread] = mcpDiscussionAddress(fetched.pageID, gone.BlockID, key)
		}
	}
	named := &mcpFetchedDiscussions{pageID: fetched.pageID, addressed: addressed, started: mcpVersionThreadsStarted(app, pageID, addressed)}
	return named.namer(), nil
}

// mcpVersionMarkedThreads are the threads a version's words are marked with,
// in its text and its captions.
func mcpVersionMarkedThreads(blocks []pageHistoryBlockWire) []string {
	threads, seen := []string{}, map[string]bool{}
	for _, block := range blocks {
		content := struct {
			Rich []struct {
				CommentID string `json:"commentId"`
			} `json:"rich"`
			Caption []struct {
				CommentID string `json:"commentId"`
			} `json:"caption"`
		}{}
		if json.Unmarshal(block.Content, &content) != nil {
			continue
		}
		for _, run := range append(content.Rich, content.Caption...) {
			for _, thread := range strings.Fields(run.CommentID) {
				if !seen[thread] {
					seen[thread] = true
					threads = append(threads, thread)
				}
			}
		}
	}
	return threads
}

// mcpVersionThreadsStarted orders the threads named by when their first
// comments were made, a deleted one's too, as fetch orders a run's
// discussions.
func mcpVersionThreadsStarted(app core.App, pageID string, addressed map[string]string) map[string]int {
	type thread struct {
		ID        string `db:"id"`
		CreatedAt string `db:"created_at"`
	}
	made := []thread{}
	_ = app.DB().NewQuery(`
		SELECT [[id]],[[created_at]] FROM {{leafen_comments}} WHERE [[page_id]]={:pageId} AND [[parent_id]]=''
		UNION ALL SELECT [[id]],[[created_at]] FROM {{leafen_comment_tombstones}} WHERE [[page_id]]={:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).All(&made)
	made = slices.DeleteFunc(made, func(one thread) bool {
		_, named := addressed[one.ID]
		return !named
	})
	sort.SliceStable(made, func(left, right int) bool {
		if made[left].CreatedAt != made[right].CreatedAt {
			return made[left].CreatedAt < made[right].CreatedAt
		}
		return made[left].ID < made[right].ID
	})
	started := make(map[string]int, len(made))
	for index, one := range made {
		if _, placed := started[one.ID]; !placed {
			started[one.ID] = index
		}
	}
	return started
}

// mcpVersionContentAsCompared is a version's block content as
// get-version-changes compares two versions of a block: without the marks
// of the discussions left on its words, in its text and its captions, its
// runs alike apart from them joined.
func mcpVersionContentAsCompared(raw json.RawMessage) string {
	content := map[string]any{}
	if json.Unmarshal(raw, &content) != nil || content == nil {
		return mcpCanonicalJSON(raw)
	}
	for _, key := range []string{"rich", "caption"} {
		runs, listed := content[key].([]any)
		if !listed {
			continue
		}
		joined := make([]any, 0, len(runs))
		for _, value := range runs {
			run, isRun := value.(map[string]any)
			if !isRun {
				joined = append(joined, value)
				continue
			}
			delete(run, "commentId")
			if last := len(joined) - 1; last >= 0 && mcpVersionRunsAlike(joined[last], run) {
				previous := joined[last].(map[string]any)
				previous["text"] = previous["text"].(string) + run["text"].(string)
				continue
			}
			joined = append(joined, run)
		}
		content[key] = joined
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return mcpCanonicalJSON(raw)
	}
	return string(encoded)
}

// mcpVersionRunsAlike is whether two runs of words are one text apart from
// where they part: both words alone, with the same formatting.
func mcpVersionRunsAlike(left any, right map[string]any) bool {
	previous, isRun := left.(map[string]any)
	if !isRun {
		return false
	}
	for _, run := range []map[string]any{previous, right} {
		if _, words := run["text"].(string); !words || run["mention"] != nil || run["equation"] != nil || run["emptyPiece"] != nil {
			return false
		}
	}
	strip := func(run map[string]any) map[string]any {
		rest := make(map[string]any, len(run))
		for key, value := range run {
			if key != "text" {
				rest[key] = value
			}
		}
		return rest
	}
	return reflect.DeepEqual(strip(previous), strip(right))
}
