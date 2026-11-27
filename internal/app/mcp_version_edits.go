package app

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// What a page's versions changed, as the notion-get-version-changes tool
// counts it (mcp_tool_notion_version_changes.go): each block's and the
// page's edits by contributor and time, read from its history's changes,
// its blocks' lists and the version that made it.

// mcpVersionEdits are the people and times of one record's changes, and
// the versions that made them. The people are in the order they first
// changed it, as Notion's contributors (seen 2026-11-14, three of them in
// two orders on two blocks: an integration, the person on screen and their
// agent, each block's in the order of its first edits), since[i] being when
// contributors[i] first did.
type mcpVersionEdits struct {
	contributors []notionMCPVersionContributor
	since        []int64
	first, last  int64
	versionIDs   []string
}

func (edits *mcpVersionEdits) add(contributor notionMCPVersionContributor, at int64) {
	edits.count(contributor, at)
	if edits.first == 0 || at < edits.first {
		edits.first = at
	}
	edits.last = max(edits.last, at)
}

// count places a contributor by their first change, those first at one
// time in the order they're counted.
func (edits *mcpVersionEdits) count(contributor notionMCPVersionContributor, at int64) {
	if index := slices.Index(edits.contributors, contributor); index >= 0 {
		if at >= edits.since[index] {
			return
		}
		edits.contributors = slices.Delete(edits.contributors, index, index+1)
		edits.since = slices.Delete(edits.since, index, index+1)
	}
	place := len(edits.since)
	for place > 0 && edits.since[place-1] > at {
		place--
	}
	edits.contributors = slices.Insert(edits.contributors, place, contributor)
	edits.since = slices.Insert(edits.since, place, at)
}

// merge adds another record's people and times.
func (edits *mcpVersionEdits) merge(other *mcpVersionEdits) {
	if other == nil {
		return
	}
	for index, contributor := range other.contributors {
		edits.count(contributor, other.since[index])
	}
	if other.first != 0 && (edits.first == 0 || other.first < edits.first) {
		edits.first = other.first
	}
	edits.last = max(edits.last, other.last)
}

// readMCPVersionEdits reads who changed each block, and the title, between
// two versions.
func readMCPVersionEdits(
	ctx context.Context, app core.App, pageID, since, versionID string, inRange []pageHistoryVersionWire,
) (map[string]*mcpVersionEdits, error) {
	versions := map[string]pageHistoryVersionWire{}
	for _, version := range inRange {
		versions[version.ID] = version
	}
	edits := map[string]*mcpVersionEdits{}
	cursor := ""
	for {
		// Every change, not a person's runs as their latest: each one's
		// time and connection counts (page_history_changes.go).
		query := url.Values{"since": {since}, "limit": {strconv.Itoa(pageHistoryChangePageSize)}, "coalesce": {"false"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		result, err := mcpNativeCall(ctx, "GET", "/pages/"+pageID+"/history/"+url.PathEscape(versionID)+"/changes", query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if err := mcpNativeRefusal(result); err != nil {
			return nil, err
		}
		listed := pageHistoryChangesResponse{}
		if encoded, err := json.Marshal(result.Body); err != nil || json.Unmarshal(encoded, &listed) != nil {
			return nil, failedBecause(errMCPToolFailed, err)
		}
		for _, change := range listed.Changes {
			key := mcpVersionEditKey(change)
			if key == "" {
				continue
			}
			// Only the range's versions count: the one it starts from is the
			// page as it stood then, whose changes came before it, as
			// Notion's ranges start (seen 2026-11-22, worker w2345: a title an
			// integration renamed seconds before the start and the agent
			// after it lists the agent's rename alone, first edited then, and
			// the other way round the integration's).
			version, known := versions[change.VersionID]
			if !known {
				continue
			}
			if edits[key] == nil {
				edits[key] = &mcpVersionEdits{}
			}
			edits[key].add(mcpVersionContributor(version), change.CreatedAt)
			if !slices.Contains(edits[key].versionIDs, change.VersionID) {
				edits[key].versionIDs = append(edits[key].versionIDs, change.VersionID)
			}
			if change.BlockID != "" && key == change.BlockID {
				mcpVersionNoteBlockPlace(edits, change, mcpVersionContributor(version))
			}
		}
		if !listed.HasMore || listed.NextCursor == nil || *listed.NextCursor == "" {
			return edits, nil
		}
		cursor = *listed.NextCursor
	}
}

// mcpVersionNoteBlockPlace keeps, apart from a block's edits, its earliest
// change in the range ("made:", its making when the block is new), its
// removal ("removed:") and its moves ("moved:"), for mcpVersionBlockListEdits.
func mcpVersionNoteBlockPlace(edits map[string]*mcpVersionEdits, change pageHistoryChangeWire, contributor notionMCPVersionContributor) {
	note := func(key string) {
		if edits[key] == nil {
			edits[key] = &mcpVersionEdits{}
		}
		edits[key].add(contributor, change.CreatedAt)
	}
	switch {
	case change.Deleted:
		note("removed:" + change.BlockID)
	case change.Kind == pageHistoryChangeMoved:
		note("moved:" + change.BlockID)
	}
	if made := edits["made:"+change.BlockID]; made == nil || change.CreatedAt < made.first {
		edits["made:"+change.BlockID] = nil
		note("made:" + change.BlockID)
	}
}

// mcpVersionBlockListEdits counts a change to the page's own list of blocks
// — a block made at its top, removed from its top, or moved among, into or
// out of its top blocks — as an edit of its title, as Notion's does (seen
// 2026-11-14: the title's last edit was a block added at the page's top,
// removed from it or moved among its blocks or out of them; not a block's
// text, nor a block added or moved inside another).
func mcpVersionBlockListEdits(edits map[string]*mcpVersionEdits, before, after pageHistorySnapshotResponse) {
	parents := func(blocks []pageHistoryBlockWire) map[string]string {
		parent := make(map[string]string, len(blocks))
		for _, block := range blocks {
			parent[block.ID] = block.ParentID
		}
		return parent
	}
	was, is := parents(before.Blocks), parents(after.Blocks)
	count := func(record *mcpVersionEdits) {
		if record == nil {
			return
		}
		if edits["title"] == nil {
			edits["title"] = &mcpVersionEdits{}
		}
		edits["title"].merge(record)
	}
	for _, block := range after.Blocks {
		parent, had := was[block.ID]
		if !had && block.ParentID == "" {
			count(edits["made:"+block.ID])
		}
		if block.ParentID == "" || had && parent == "" {
			count(edits["moved:"+block.ID])
		}
	}
	for _, block := range before.Blocks {
		if _, has := is[block.ID]; !has && block.ParentID == "" {
			count(edits["removed:"+block.ID])
		}
	}
}

// mcpVersionMadeWith counts a page's making, the version made, as an edit
// of its title and of each block it was made with, as Notion's does (seen
// 2026-11-14: a block made with the page and edited later was first edited
// when the page was made, one not edited since then last too); the history
// names no block in the version that made the page. A block made with the
// page was made with its title, so its first change in the range — a later
// edit of its words — is no making of a block in the page's list
// (mcpVersionBlockListEdits): Notion's title stays at the making and its
// contributors leave that edit's maker out (seen 2026-11-22, worker w2345,
// twice: an agent's later edits of a line an integration made the page with).
func mcpVersionMadeWith(edits map[string]*mcpVersionEdits, made pageHistoryVersionWire, creation pageHistorySnapshotResponse) {
	mark := func(key string) {
		record := &mcpVersionEdits{versionIDs: []string{made.ID}}
		record.add(mcpVersionContributor(made), made.CreatedAt)
		if later := edits[key]; later != nil {
			record.merge(later)
			for _, id := range later.versionIDs {
				if !slices.Contains(record.versionIDs, id) {
					record.versionIDs = append(record.versionIDs, id)
				}
			}
		}
		edits[key] = record
	}
	mark("title")
	for _, block := range creation.Blocks {
		mark(block.ID)
		delete(edits, "made:"+block.ID)
	}
}

// mcpVersionEditKey is the record a history change names: a block, the
// page's title, its own state (mcpVersionPageStateKey), a row, a view, or a
// database's data source.
func mcpVersionEditKey(change pageHistoryChangeWire) string {
	switch change.Kind {
	case pageHistoryChangeRenamed:
		return "title"
	case pageHistoryChangeRowAdded, pageHistoryChangeRowEdited, pageHistoryChangeRowRemoved:
		return "row:" + change.ItemID
	case pageHistoryChangeViewCreated:
		return "view:" + change.ItemID
	case pageHistoryChangePropertyChanged, pageHistoryChangeDatabaseEdited:
		return "data_source"
	case pageHistoryChangePageEdited:
		if change.BlockID == "" {
			return mcpVersionPageStateKey
		}
	}
	return change.BlockID
}

// mcpVersionPageRange are the versions in a range that changed the page
// itself, not only its inline databases, which a page's records no change
// names are said to be made by: those databases' changes are their own
// (Notion's, seen 2026-11-13: a page's title last edited when its database
// was made, not when a row was added to it). None such leaves them all.
func mcpVersionPageRange(edits map[string]*mcpVersionEdits, inRange []pageHistoryVersionWire) []pageHistoryVersionWire {
	databaseOnly := map[string]bool{}
	for key, record := range edits {
		if key == "data_source" || strings.HasPrefix(key, "row:") || strings.HasPrefix(key, "view:") {
			for _, id := range record.versionIDs {
				databaseOnly[id] = true
			}
		}
	}
	for key, record := range edits {
		if key != "data_source" && !strings.HasPrefix(key, "row:") && !strings.HasPrefix(key, "view:") {
			for _, id := range record.versionIDs {
				delete(databaseOnly, id)
			}
		}
	}
	own := slices.DeleteFunc(slices.Clone(inRange), func(version pageHistoryVersionWire) bool { return databaseOnly[version.ID] })
	if len(own) == 0 {
		return inRange
	}
	return own
}
