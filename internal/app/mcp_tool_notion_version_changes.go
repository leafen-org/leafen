package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// get-version-changes (Notion's, seen 2026-11-13): each block or property
// that changed in the range listed once, as it was at the start (before) and
// at the end (after), added, edited, moved or removed, with who changed it and when
// first and last, the latest edited first; a database block as its database
// tag, and the page's inline databases, whose own changes are read by their
// urls, named after them. Leafen compares the page's versions at the two
// times and names each change's people from the history's changes between
// them, an agent's change by its connection (source mcp). A database's
// changes are its data sources', views' and rows'
// (mcp_tool_notion_version_database_changes.go).

type notionMCPVersionChangesInput struct {
	Page      string   `json:"page" jsonschema:"The page or database to read: its address or ID, or a data source's collection:// URL"`
	StartTime string   `json:"start_time,omitempty" jsonschema:"Start of the range, not included, as an ISO 8601 time with a time zone; defaults to 24 hours before end_time"`
	EndTime   string   `json:"end_time,omitempty" jsonschema:"End of the range, included, as an ISO 8601 time with a time zone; defaults to now"`
	Authors   []string `json:"authors,omitempty" jsonschema:"Only records these people changed: user:// addresses"`
	Sources   []string `json:"sources,omitempty" jsonschema:"Only records changed through these: mcp (an agent's connection); Leafen has no built-in AI or automations' page edits"`
}

// notionMCPVersionContributor is who changed a record: a person, an agent
// connected as them (source mcp), or an integration named alone, without a
// person, as Notion's names its bot (seen 2026-11-14: {"source_name":"…"}).
type notionMCPVersionContributor struct {
	Author     string `json:"author,omitempty"`
	Source     string `json:"source,omitempty"`
	SourceName string `json:"source_name,omitempty"`
}

type notionMCPVersionChange struct {
	Contributors  []notionMCPVersionContributor `json:"contributors"`
	FirstEditTime string                        `json:"first_edit_time"`
	LastEditTime  string                        `json:"last_edit_time"`
	Type          string                        `json:"type"`
	URL           string                        `json:"url,omitempty"`
	Action        string                        `json:"action"`
	Title         string                        `json:"title,omitempty"`
	DataSourceURL string                        `json:"data_source_url,omitempty"`
	Before        any                           `json:"before,omitempty"`
	After         any                           `json:"after,omitempty"`
	// Truncated marks a state written short (mcpVersionSource.sourceWritten).
	Truncated bool `json:"truncated,omitempty"`
	// record is the ID of what changed — the page for its title, a block, a
	// data source, a view or a row — which orders changes made at one time.
	record string
}

type notionMCPVersionAuthor struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

type notionMCPVersionChangesOutput struct {
	Type      string                   `json:"type"`
	URL       string                   `json:"url"`
	StartTime string                   `json:"start_time"`
	EndTime   string                   `json:"end_time"`
	Authors   []notionMCPVersionAuthor `json:"authors"`
	Changes   []notionMCPVersionChange `json:"changes"`
	// InlineDatabases are a page's inline databases, by their urls.
	InlineDatabases []string `json:"inline_databases,omitempty"`
	// OlderChangesEndTime is where the changes go on when not all of them
	// fit in one answer (mcpVersionChangesWindow).
	OlderChangesEndTime string `json:"older_changes_end_time,omitempty"`
	Message             string `json:"message,omitempty"`
}

func listMCPPageVersionChanges(ctx context.Context, app core.App, input notionMCPVersionChangesInput) (notionMCPVersionChangesOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return notionMCPVersionChangesOutput{}, err
	}
	target, err := mcpHistoryTargetOf(app, authority, input.Page)
	if err != nil {
		return notionMCPVersionChangesOutput{}, err
	}
	pageID, address := target.PageID, mcpPageAddress(app, authority, target.PageID)
	if target.Database != nil {
		address = target.Database.URL
	}
	end, start, message := time.Now().UTC(), time.Time{}, ""
	if input.EndTime != "" {
		if end, err = mcpHistoryTime("end_time", input.EndTime); err != nil {
			return notionMCPVersionChangesOutput{}, err
		}
	}
	if input.StartTime != "" {
		if start, err = mcpHistoryTime("start_time", input.StartTime); err != nil {
			return notionMCPVersionChangesOutput{}, err
		}
	} else {
		start = end.Add(-24 * time.Hour)
		message = "Without start_time this covers only the 24 hours before end_time: say so to the person, and give start_time to look further back."
	}
	if !end.After(start) {
		return notionMCPVersionChangesOutput{}, mcpInvalid("end_time comes after start_time.")
	}
	for _, source := range input.Sources {
		if !slices.Contains([]string{"notion_ai", "custom_agent", "mcp", "database_automation"}, source) {
			return notionMCPVersionChangesOutput{}, mcpInvalid("sources holds notion_ai, custom_agent, mcp or database_automation")
		}
	}
	output := notionMCPVersionChangesOutput{
		Type: "changes", URL: address, StartTime: personClockMillis(start, authority.clock()),
		EndTime: personClockMillis(end, authority.clock()), Authors: []notionMCPVersionAuthor{}, Changes: []notionMCPVersionChange{}, Message: message,
	}
	atEnd, err := readMCPHistoryVersions(ctx, pageID, end)
	if err != nil || atEnd.At == nil {
		return output, err
	}
	// A page's inline databases are named whether or not it changed, as
	// Notion's (seen 2026-11-13).
	after := pageHistorySnapshotResponse{}
	if target.Database == nil {
		if after, err = readMCPHistorySnapshot(ctx, pageID, atEnd.At.ID); err != nil {
			return output, err
		}
		output.InlineDatabases = mcpVersionInlineDatabases(app, authority, pageID, after)
	}
	atStart, err := readMCPHistoryVersions(ctx, pageID, start)
	if err != nil {
		return output, err
	}
	// The versions in the range, the latest first: those after the start
	// and at or before the end.
	inRange := slices.DeleteFunc(slices.Clone(atStart.InRange), func(version pageHistoryVersionWire) bool {
		return version.CreatedAt > end.UnixMilli()
	})
	if atStart.At == nil && atEnd.At != nil && !slices.ContainsFunc(inRange, func(version pageHistoryVersionWire) bool { return version.ID == atEnd.At.ID }) {
		inRange = append(inRange, *atEnd.At)
	}
	if len(inRange) == 0 {
		return output, nil
	}
	since := inRange[len(inRange)-1].ID
	if atStart.At != nil {
		since = atStart.At.ID
	}
	edits, err := readMCPVersionEdits(ctx, app, pageID, since, atEnd.At.ID, inRange)
	if err != nil {
		return output, err
	}
	if target.Database == nil && atStart.At == nil {
		// Made in the range: the records it was made with were first
		// edited then (mcpVersionMadeWith).
		made, creation := inRange[len(inRange)-1], after
		if made.ID != atEnd.At.ID {
			if creation, err = readMCPHistorySnapshot(ctx, pageID, made.ID); err != nil {
				return output, err
			}
		}
		mcpVersionMadeWith(edits, made, creation)
	}
	if target.Database != nil {
		if output.Changes, err = listMCPDatabaseVersionChanges(
			ctx, app, authority, target, atStart.At, *atEnd.At, start.UnixMilli(), end.UnixMilli(), edits, inRange,
		); err != nil {
			return output, err
		}
	} else {
		before := pageHistorySnapshotResponse{}
		if atStart.At != nil {
			if before, err = readMCPHistorySnapshot(ctx, pageID, atStart.At.ID); err != nil {
				return output, err
			}
		}
		// A database row page's values are its properties
		// (mcp_tool_notion_version_row_page.go).
		row, err := readMCPVersionRowPage(ctx, app, authority, pageID, atStart.At != nil, start, end)
		if err != nil {
			return output, err
		}
		if row != nil {
			if err := mcpVersionRowPageEdits(app, pageID, edits, inRange); err != nil {
				return output, failedBecause(errMCPToolFailed, err)
			}
		}
		mcpVersionBlockListEdits(edits, before, after)
		// The discussions started on what changed count too
		// (mcp_version_discussion_edits.go).
		if err := mcpVersionDiscussionEdits(app, authority, pageID, start.UnixMilli(), end.UnixMilli(), edits); err != nil {
			return output, failedBecause(errMCPToolFailed, err)
		}
		// So do the writes that edited a block without changing it (mcp_version_touch_edits.go).
		if err := mcpVersionTouchEdits(app, pageID, start.UnixMilli(), end.UnixMilli(), edits); err != nil {
			return output, failedBecause(errMCPToolFailed, err)
		}
		text, failed, err := mcpVersionBlockText(
			app, authority, target.Page, append(slices.Clone(after.Databases), before.Databases...), after.Blocks, before.Blocks,
		)
		if err != nil {
			return output, err
		}
		output.Changes = mcpVersionChanges(before, after, atStart.At == nil, edits, inRange, text)
		if err := failed(); err != nil {
			return output, err
		}
		// Its title as it was spelled then (mcp_tool_notion_version_titles.go);
		// an untitled page's change names it a new page, as Notion's
		// ("title":"새 페이지", after {"title":""}; seen 2026-11-14).
		if row != nil {
			output.Changes = row.properties(app, pageID, atStart.At == nil, output.Changes,
				mcpVersionStamper(edits, mcpVersionPageRange(edits, inRange)))
		} else {
			mcpVersionTitleChanges(app, authority, target.Page, before, after, output.Changes)
		}
	}
	// The latest edited first, and those last edited at one time by the
	// IDs of what changed, as Notion's (seen 2026-11-14: a page's blocks,
	// its title, a database's data source, block and view, and its rows, in
	// their IDs' order whichever was made or edited first). Leafen's IDs are
	// its own, so the order is Notion's rule over them.
	slices.SortStableFunc(output.Changes, func(left, right notionMCPVersionChange) int {
		if order := strings.Compare(right.LastEditTime, left.LastEditTime); order != 0 {
			return order
		}
		return strings.Compare(left.record, right.record)
	})
	output.Changes = slices.DeleteFunc(output.Changes, func(change notionMCPVersionChange) bool {
		return !mcpVersionChangeWanted(change, input.Authors, input.Sources)
	})
	// Their times on the clock (person_clock.go), once in their order.
	for index := range output.Changes {
		change := &output.Changes[index]
		change.FirstEditTime = personClockMillisOf(change.FirstEditTime, authority.clock())
		change.LastEditTime = personClockMillisOf(change.LastEditTime, authority.clock())
	}
	if output.Authors, err = mcpVersionAuthors(app, output.Changes); err != nil {
		return output, err
	}
	if stopped, err := mcpVersionChangesWindow(&output); err != nil {
		return output, failedBecause(errMCPToolFailed, err)
	} else if stopped {
		if output.Authors, err = mcpVersionAuthors(app, output.Changes); err != nil {
			return output, err
		}
	}
	return output, nil
}

// mcpVersionContributor is who made a version, as Notion names them: a
// person by the address the tools name them by (notionPersonAddress), a
// moved workspace's person without an account here by the ID the move kept.
func mcpVersionContributor(version pageHistoryVersionWire) notionMCPVersionContributor {
	if version.ConnectionKind == "integration" {
		return notionMCPVersionContributor{SourceName: version.ConnectionName}
	}
	contributor := notionMCPVersionContributor{Author: notionPersonAddress(version.ActorID)}
	if version.ConnectionKind == "agent" {
		contributor.Source, contributor.SourceName = "mcp", version.ConnectionName
	}
	return contributor
}

// mcpVersionChanges compares a page's two versions: its title, then its
// blocks in the later version's order, then those it no longer has, each
// block as text writes it; a record no change names is said to be the
// range's people's. A block under another parent than at the start, its
// text the same, was moved, listed as it is after; one whose text changed
// too was edited; one only put in another place under the same parent isn't
// listed; one the page no longer has was removed, as Notion's (seen
// 2026-11-14: a paragraph indented under another or moved into a list
// {"action":"moved","after":…}, one indented and retyped "edited", one
// moved up among its siblings not listed, a deleted one "removed").
func mcpVersionChanges(
	before, after pageHistorySnapshotResponse, created bool, edits map[string]*mcpVersionEdits, inRange []pageHistoryVersionWire,
	text func(pageHistoryBlockWire) string,
) []notionMCPVersionChange {
	stamper := mcpVersionStamper(edits, mcpVersionPageRange(edits, inRange))
	stamp := func(change notionMCPVersionChange, key string) notionMCPVersionChange {
		change = stamper(change, key)
		if key == "title" {
			change.record = after.PageID
		}
		return change
	}
	changes := []notionMCPVersionChange{}
	switch {
	case created:
		changes = append(changes, stamp(notionMCPVersionChange{Type: "properties", Action: "added", Title: after.Title,
			After: map[string]any{"title": after.Title}}, "title"))
	case before.Title != after.Title:
		changes = append(changes, stamp(notionMCPVersionChange{Type: "properties", Action: "edited", Title: after.Title,
			Before: map[string]any{"title": before.Title}, After: map[string]any{"title": after.Title}}, "title"))
	}
	// A sub-page's block, or a full-page database's, isn't listed: what
	// it holds is its own page's, as Notion's (seen 2026-11-13).
	own := func(block pageHistoryBlockWire) bool {
		return block.Type != graphBlockTypeChildPage && block.Type != graphBlockTypeChildDatabase
	}
	earlier := map[string]pageHistoryBlockWire{}
	for _, block := range before.Blocks {
		earlier[block.ID] = block
	}
	later := map[string]bool{}
	for _, block := range after.Blocks {
		later[block.ID] = true
		if !own(block) {
			continue
		}
		was, had := earlier[block.ID]
		switch {
		case !had:
			changes = append(changes, stamp(notionMCPVersionChange{Type: "block", Action: "added", After: text(block)}, block.ID))
		case was.Type != block.Type || was.PlainText != block.PlainText || mcpVersionContentAsCompared(was.Content) != mcpVersionContentAsCompared(block.Content):
			changes = append(changes, stamp(notionMCPVersionChange{Type: "block", Action: "edited", Before: text(was), After: text(block)}, block.ID))
		case was.ParentID != block.ParentID:
			changes = append(changes, stamp(notionMCPVersionChange{Type: "block", Action: "moved", After: text(block)}, block.ID))
		}
	}
	for _, block := range before.Blocks {
		if !later[block.ID] && own(block) {
			changes = append(changes, stamp(notionMCPVersionChange{Type: "block", Action: "removed", Before: text(block)}, block.ID))
		}
	}
	return changes
}

// mcpVersionBlockText writes a version's block as get-version-changes lists
// it, as Notion's (seen 2026-11-14): alone, as fetch writes it at a page's
// top, without the blocks it holds, which are listed apart — "- 목록 아래
// <code>코드</code>", "<toggle>\n토글 머리\n</toggle>" — but a page's mention
// by its address alone: "첫 줄 <mention url=\"…\" /> 보통 <b>굵게</b> 글
// [링크](https://example.com) <date start=\"2026-11-20\" /> <mention
// url=\"user://…\">이름</mention><br />둘째 줄". A database block is its tag
// as fetch writes it (seen 2026-11-13: "<database url=… data-source-url=…
// inline=\"true\">\nName\n</database>"). failed is what stopped a block's
// writing, if anything did.
func mcpVersionBlockText(
	app core.App, authority mcpAuthority, page *core.Record, databases []pageHistoryDatabaseWire, versions ...[]pageHistoryBlockWire,
) (text func(pageHistoryBlockWire) string, failed func() error, err error) {
	names := map[string]string{}
	for _, database := range databases {
		if _, named := names[database.BlockID]; !named {
			names[database.BlockID] = database.Title
		}
	}
	every := []workspaceExportMarkdownBlock{}
	for _, blocks := range versions {
		every = append(every, mcpHistoryBlocks(blocks)...)
	}
	writer, err := newMCPHistoryWriter(app, authority, page, every)
	if err != nil {
		return nil, nil, err
	}
	writer.mentions.PageTitle = nil
	var stopped error
	text = func(block pageHistoryBlockWire) string {
		if tag, ok := mcpVersionDatabaseTag(app, authority, page.Id, block, names[block.ID]); ok {
			return tag
		}
		alone := mcpHistoryBlocks([]pageHistoryBlockWire{block})
		alone[0].ParentID = ""
		written, err := writer.markdown(alone)
		if err != nil && stopped == nil {
			stopped = err
		}
		return written
	}
	return text, func() error { return stopped }, nil
}

// mcpVersionDatabaseTag is an inline database block's tag; false for
// another block.
func mcpVersionDatabaseTag(app core.App, authority mcpAuthority, pageID string, block pageHistoryBlockWire, name string) (string, bool) {
	if block.Type != graphBlockTypeInlineDatabase {
		return "", false
	}
	content := map[string]any{}
	_ = json.Unmarshal(block.Content, &content)
	tag := notionMarkdownReference{Tag: "database", Inline: true, Title: mcpDatabaseShownTitle(app, authority.PrincipalID, name)}
	tag.DataSourceID, _ = content["dataSourceId"].(string)
	tag.URL = mcpDatabaseAddress(authority, mcpBlockDatabaseContainer(app, pageID, block.ID))
	return tag.line(), true
}

// mcpBlockDatabaseContainer is the database a page's block shows.
func mcpBlockDatabaseContainer(app core.App, pageID, blockID string) string {
	containerID := blockID
	_ = app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_containers}} WHERE [[page_id]] = {:pageId} AND [[block_id]] = {:blockId}
		ORDER BY [[id]] LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "blockId": blockID}).Row(&containerID)
	return containerID
}

// mcpVersionInlineDatabases are the inline databases a page's version
// holds, by their urls, as Notion's get-version-changes names them after a
// page's changes.
func mcpVersionInlineDatabases(app core.App, authority mcpAuthority, pageID string, version pageHistorySnapshotResponse) []string {
	addresses := []string{}
	for _, block := range version.Blocks {
		if block.Type == graphBlockTypeInlineDatabase {
			addresses = append(addresses, mcpDatabaseAddress(authority, mcpBlockDatabaseContainer(app, pageID, block.ID)))
		}
	}
	return addresses
}

// mcpVersionChangeWanted keeps a record its filters name.
func mcpVersionChangeWanted(change notionMCPVersionChange, authors, sources []string) bool {
	if len(authors) == 0 && len(sources) == 0 {
		return true
	}
	for _, contributor := range change.Contributors {
		if (len(authors) == 0 || slices.Contains(authors, contributor.Author)) &&
			(len(sources) == 0 || slices.Contains(sources, contributor.Source)) {
			return true
		}
	}
	return false
}

// mcpCanonicalJSON writes a block's content with its keys in order, to tell
// a change from the same content written another way.
func mcpCanonicalJSON(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
