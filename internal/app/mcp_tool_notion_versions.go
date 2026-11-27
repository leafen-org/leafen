package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's page versions (seen 2026-11-13): view-version reads a page as it
// was at a time, get-version-changes lists what changed between two times
// and who changed it, and restore-version puts a page back as it was,
// listing what it would do until user_confirmed. Leafen reads them from the
// page's version history as its screen does (/pages/{id}/history): the
// version at a time is the last one saved at or before it, a time before the
// page was made is refused as Notion's refuses it, and a restore is the
// history's own restore, its databases' parts kept or not as Notion's
// database_options say; an inline database's restores it alone, leaving the
// page it is in (page_history_database_alone_restore.go). The history's
// restore leaves comments as they are, and its confirmation says so: Notion's
// says the ones added after the time go (seen 2026-11-13), which Leafen's
// restore doesn't do.

type notionMCPViewVersionInput struct {
	Page   string `json:"page" jsonschema:"The page or database to read: its address or ID, or a data source's collection:// URL"`
	Time   string `json:"time" jsonschema:"The moment to read, as an ISO 8601 time with a time zone offset, such as 2026-10-10T14:05:02-04:00"`
	Cursor string `json:"cursor,omitempty" jsonschema:"A database's next_cursor, to read its next rows; pass the same page and time as the call that gave it"`
}

// A page's version gives its properties, content and truncated; a
// database's its configuration, rows and total_rows (mcp_tool_notion_version_database.go).
type notionMCPViewVersionOutput struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	// Properties are a page's title, or a database row's values
	// (mcpVersionRowProperties).
	Properties    any                    `json:"properties,omitempty"`
	Content       *string                `json:"content,omitempty"`
	Truncated     *bool                  `json:"truncated,omitempty"`
	Configuration string                 `json:"configuration,omitempty"`
	Rows          *[]notionMCPVersionRow `json:"rows,omitempty"`
	TotalRows     *int                   `json:"total_rows,omitempty"`
	NextCursor    string                 `json:"next_cursor,omitempty"`
}

type notionMCPRestoreDatabaseOptions struct {
	Properties        *bool `json:"properties,omitempty" jsonschema:"false leaves the database's properties (its schema) as they are now; left out, they are restored"`
	RowPropertyValues *bool `json:"row_property_values,omitempty" jsonschema:"false leaves each row's property values as they are now; left out, they are restored"`
	RowTitles         *bool `json:"row_titles,omitempty" jsonschema:"false leaves each row's title as it is now; left out, titles are restored"`
}

type notionMCPRestoreVersionInput struct {
	Page                   string                           `json:"page" jsonschema:"The page or database to restore: its address or ID, or a data source's collection:// URL"`
	Time                   string                           `json:"time" jsonschema:"The past moment to restore to, as an ISO 8601 time with a time zone; the page keeps every edit made at or before it"`
	UserConfirmed          bool                             `json:"user_confirmed,omitempty" jsonschema:"Set to true only after the person has seen what this restore changes and said yes; without it nothing is restored and the call says what it would do"`
	IncludeInlineDatabases bool                             `json:"include_inline_databases,omitempty" jsonschema:"For a page: true restores the databases inside it as well; left out, they stay as they are"`
	DatabaseOptions        *notionMCPRestoreDatabaseOptions `json:"database_options,omitempty" jsonschema:"For a database: which parts to restore; left out, all"`
}

type notionMCPRestoreVersionOutput struct {
	Status    string `json:"status"`
	StartedAt string `json:"started_at"`
	Message   string `json:"message"`
}

func registerMCPVersionTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-view-version", Title: "Read a Leafen page as it was",
		Description: "Read a page or database as it looked at a past time, from its version history. The open page and other reads show only what it is now, so use this for what a page said at an earlier time. A page returns its title and its content as Markdown, as fetch writes it; each inline database is a database tag, whose url you can pass to this tool to read the database at the same time. A database returns its data sources, views and rows; a linked database leaves out the data sources and rows that belong to another database.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPViewVersionInput) (*mcp.CallToolResult, notionMCPViewVersionOutput, error) {
		output, err := viewMCPPageVersion(ctx, app, input)
		if err != nil {
			return nil, output, err
		}
		// A row's values keep their table's order (mcp_answer_order.go).
		answer, err := mcpAnswerInOrder(output)
		return answer, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-version-changes", Title: "List changes to a Leafen page",
		Description: "List what changed in a page or database between two times and who changed it, with whether through an agent (mcp). Each block, property, row, data source or view that changed is listed once, as it was at the start and at the end of the range, the latest edited first; the range defaults to the last 24 hours. Filter by authors (user:// addresses) or sources. Comments aren’t listed, nor changes inside a page’s inline databases: pass an inline database’s url to read them. Times carry the offset of the person’s time zone.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPVersionChangesInput) (*mcp.CallToolResult, notionMCPVersionChangesOutput, error) {
		output, err := listMCPPageVersionChanges(ctx, app, input)
		if err != nil {
			return nil, output, err
		}
		// A row's values keep their schema's order (mcp_answer_order.go).
		answer, err := mcpAnswerInOrder(output)
		return answer, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-restore-version", Title: "Restore a Leafen page",
		Description: "Restore a whole page or database to how it looked at a past time, from its version history, as restoring a version from the page's history does: every edit made after that time is undone, other people's too; comments stay. To undo one change and keep later edits, edit the page instead. First list what will be undone with notion-get-version-changes from that time to now and show it to the person. Without user_confirmed nothing is restored and the call says what it would do. Databases inside a page are restored only with include_inline_databases. To undo a restore, restore the page to its started_at time.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPRestoreVersionInput) (*mcp.CallToolResult, notionMCPRestoreVersionOutput, error) {
		return restoreMCPPageVersion(ctx, app, input)
	})
}

// mcpHistoryPage is the page a version tool reads: a page, or a full-page
// database's page, the person can open in the connection's workspaces.
func mcpHistoryPage(app core.App, authority mcpAuthority, raw string) (string, *core.Record, error) {
	pageID := mcpLocalID(app, raw)
	if containerID, found := resolveMCPDatabaseID(app, pageID); found {
		if location, err := loadMCPDatabaseContainerLocation(app, containerID); err == nil && mcpFullPageDatabaseContainer(app, location.PageID) == containerID {
			pageID = location.PageID
		}
	}
	_, page, readable := mcpListedPage(app, authority, pageID)
	if !readable {
		return "", nil, mcpNotFound("page", raw)
	}
	return pageID, page, nil
}

// mcpHistoryTime reads a time given with its zone.
func mcpHistoryTime(name, raw string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, mcpInvalid("%s is an ISO 8601 time with a time zone, such as 2026-10-10T18:05:02Z", name)
	}
	return at, nil
}

// mcpHistoryVersions reads a page's versions, the latest first, down to the
// first saved at or before a time, and the version history's frontier.
type mcpHistoryVersions struct {
	Frontier int
	// InRange are the versions after the time, the latest first; At the one
	// the page was as at the time, nil before the page was made.
	InRange []pageHistoryVersionWire
	At      *pageHistoryVersionWire
}

func readMCPHistoryVersions(ctx context.Context, pageID string, at time.Time) (mcpHistoryVersions, error) {
	found := mcpHistoryVersions{}
	var creation *pageHistoryVersionWire
	cursor := ""
	for {
		query := url.Values{"limit": {strconv.Itoa(pageHistoryPageSize)}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		result, err := mcpNativeCall(ctx, "GET", "/pages/"+pageID+"/history", query.Encode(), nil)
		if err != nil {
			return found, err
		}
		if err := mcpNativeRefusal(result); err != nil {
			return found, err
		}
		listed := pageHistoryListResponse{}
		if encoded, err := json.Marshal(result.Body); err != nil || json.Unmarshal(encoded, &listed) != nil {
			return found, failedBecause(errMCPToolFailed, err)
		}
		if cursor == "" {
			found.Frontier, creation = listed.SnapshotFrontier, listed.Creation
		}
		for index := range listed.Versions {
			if listed.Versions[index].CreatedAt <= at.UnixMilli() {
				found.At = &listed.Versions[index]
				return found, nil
			}
			found.InRange = append(found.InRange, listed.Versions[index])
		}
		if !listed.HasMore || listed.NextCursor == nil || *listed.NextCursor == "" {
			break
		}
		cursor = *listed.NextCursor
	}
	if creation != nil && creation.CreatedAt <= at.UnixMilli() {
		found.At = creation
	}
	return found, nil
}

// readMCPHistorySnapshot reads a version of a page whole, its suggestions
// written in (mcpHistoryWithSuggestions).
func readMCPHistorySnapshot(ctx context.Context, pageID, versionID string) (pageHistorySnapshotResponse, error) {
	whole := pageHistorySnapshotResponse{}
	cursor := ""
	for first := true; ; first = false {
		query := url.Values{"limit": {strconv.Itoa(pageHistoryBlockPageSize)}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		result, err := mcpNativeCall(ctx, "GET", "/pages/"+pageID+"/history/"+url.PathEscape(versionID), query.Encode(), nil)
		if err != nil {
			return whole, err
		}
		if err := mcpNativeRefusal(result); err != nil {
			return whole, err
		}
		part := pageHistorySnapshotResponse{}
		if encoded, err := json.Marshal(result.Body); err != nil || json.Unmarshal(encoded, &part) != nil {
			return whole, failedBecause(errMCPToolFailed, err)
		}
		if first {
			blocks := whole.Blocks
			whole = part
			whole.Blocks = blocks
		}
		whole.Blocks = append(whole.Blocks, part.Blocks...)
		if !part.HasMore || part.NextCursor == nil || *part.NextCursor == "" {
			mcpHistoryWithSuggestions(&whole)
			return whole, nil
		}
		cursor = *part.NextCursor
	}
}

func viewMCPPageVersion(ctx context.Context, app core.App, input notionMCPViewVersionInput) (notionMCPViewVersionOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return notionMCPViewVersionOutput{}, err
	}
	target, err := mcpHistoryTargetOf(app, authority, input.Page)
	if err != nil {
		return notionMCPViewVersionOutput{}, err
	}
	pageID, page := target.PageID, target.Page
	at, err := mcpHistoryTime("time", input.Time)
	if err != nil {
		return notionMCPViewVersionOutput{}, err
	}
	offset, readable := mcpVersionRowsCursor(input.Cursor)
	if target.Database == nil && input.Cursor != "" {
		return notionMCPViewVersionOutput{}, mcpInvalid("Leafen reads a page's version whole, without a cursor: leave it out")
	}
	if !readable {
		return notionMCPViewVersionOutput{}, mcpInvalid("cursor is a next_cursor this tool gave for the same database and time")
	}
	versions, err := readMCPHistoryVersions(ctx, pageID, at)
	if err != nil {
		return notionMCPViewVersionOutput{}, err
	}
	if versions.At == nil && target.Database != nil {
		return notionMCPViewVersionOutput{}, errMCPVersionBeforeDatabase
	}
	if versions.At == nil {
		return notionMCPViewVersionOutput{}, mcpVersionBeforeMade("page")
	}
	if target.Database != nil {
		output, err := viewMCPDatabaseVersion(ctx, app, authority, target, versions.At.VersionCursor, at.UnixMilli(), offset)
		return output, err
	}
	// The words its discussions marked by then, named as fetch names them
	// (mcp_tool_notion_version_discussions.go).
	snapshot, err := readMCPHistorySnapshot(ctx, pageID, mcpVersionMarkedAt(app, pageID, versions, at))
	if err != nil {
		return notionMCPViewVersionOutput{}, err
	}
	discussions, err := mcpVersionDiscussionNamer(app, authority, pageID, at, snapshot.Blocks)
	if err != nil {
		return notionMCPViewVersionOutput{}, err
	}
	markdown, err := mcpHistoryMarkdown(app, authority, page, snapshot.Blocks, discussions)
	if err != nil {
		return notionMCPViewVersionOutput{}, failedBecause(errMCPToolFailed, err)
	}
	// Its title as it was spelled then (mcp_tool_notion_version_titles.go).
	var properties any = map[string]any{"title": mcpVersionTitleMarkdown(
		app, authority, page.GetString("workspace"), snapshot.Title, mcpVersionTitleSpans(app, pageID, snapshot),
	)}
	if values, row, err := mcpVersionRowProperties(ctx, app, authority, pageID, at); err != nil {
		return notionMCPViewVersionOutput{}, err
	} else if row {
		properties = values
	}
	truncated := false
	return notionMCPViewVersionOutput{
		Type: "page", URL: mcpPageURL(authority, pageID), Properties: properties, Content: &markdown, Truncated: &truncated,
	}, nil
}

// mcpHistoryMarkdown writes a version's blocks as fetch writes a page's,
// its words in the discussions left on them that discussions names (nil for
// none).
func mcpHistoryMarkdown(
	app core.App, authority mcpAuthority, page *core.Record, versionBlocks []pageHistoryBlockWire, discussions func(threads, blockID string) string,
) (string, error) {
	blocks := mcpHistoryBlocks(versionBlocks)
	writer, err := newMCPHistoryWriter(app, authority, page, blocks)
	if err != nil {
		return "", err
	}
	writer.mentions.Discussions = discussions
	return writer.markdown(blocks)
}

// mcpHistoryBlocks are a version's blocks as Markdown is written from them.
func mcpHistoryBlocks(versionBlocks []pageHistoryBlockWire) []workspaceExportMarkdownBlock {
	blocks := make([]workspaceExportMarkdownBlock, 0, len(versionBlocks))
	for _, block := range versionBlocks {
		content := map[string]any{}
		_ = json.Unmarshal(block.Content, &content)
		if content == nil {
			content = map[string]any{}
		}
		if _, has := content["text"]; !has && block.PlainText != "" {
			content["text"] = block.PlainText
		}
		blocks = append(blocks, workspaceExportMarkdownBlock{
			ID: block.ID, ParentID: block.ParentID, Position: block.Position, Type: block.Type,
			Content: content, Spans: workspaceExportMarkdownRichSpans(content),
		})
	}
	return blocks
}

// mcpHistoryWriter writes a page's version blocks as fetch writes them for
// its reader, with what the blocks name.
type mcpHistoryWriter struct {
	pageID     string
	references map[string]notionMarkdownReference
	mentions   notionMarkdownMentions
	sources    bool
	failed     *error
}

func newMCPHistoryWriter(app core.App, authority mcpAuthority, page *core.Record, blocks []workspaceExportMarkdownBlock) (mcpHistoryWriter, error) {
	workspaceID := page.GetString("workspace")
	owner, err := mcpPageWorkspaceOwner(app, authority, page.Id)
	if err != nil {
		return mcpHistoryWriter{}, err
	}
	reader := mcpMarkdownReader(authority)
	// A version's file may be on a block gone since, which no stable
	// address finds: it is named by a download address of the reader's.
	reader.StableFiles = false
	references, err := loadNotionMarkdownReferences(app, owner, workspaceID, page.Id, blocks, reader)
	if err != nil {
		return mcpHistoryWriter{}, err
	}
	writer := mcpHistoryWriter{pageID: page.Id, references: references, sources: reader.Sources, failed: new(error)}
	writer.mentions = reader.mentions(app, workspaceID, writer.failed)
	writer.mentions.PageID = page.Id
	return writer, nil
}

func (writer mcpHistoryWriter) markdown(blocks []workspaceExportMarkdownBlock) (string, error) {
	projection := renderNotionPageMarkdown(
		blocks, writer.pageID, false, func(id string) string { return id }, writer.references, writer.mentions, writer.sources,
	)
	return projection.Markdown, *writer.failed
}

// mcpPageWorkspaceOwner is the owner of the workspace a page is in, whose
// content graph holds it.
func mcpPageWorkspaceOwner(app core.App, authority mcpAuthority, pageID string) (string, error) {
	current, err := authorizePage(app, authority.PrincipalID, pageID, permissionWorkspaceRead)
	if err != nil {
		return "", err
	}
	return current.Workspace.OwnerID, nil
}

func restoreMCPPageVersion(ctx context.Context, app core.App, input notionMCPRestoreVersionInput) (*mcp.CallToolResult, notionMCPRestoreVersionOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPRestoreVersionOutput{}, err
	}
	// A page, or a database as the other version tools read it: a full-page
	// one restores its page, an inline one itself alone.
	target, err := mcpHistoryTargetOf(app, authority, input.Page)
	if err != nil {
		return nil, notionMCPRestoreVersionOutput{}, err
	}
	pageID, page := target.PageID, target.Page
	at, err := mcpHistoryTime("time", input.Time)
	if err != nil {
		return nil, notionMCPRestoreVersionOutput{}, err
	}
	versions, err := readMCPHistoryVersions(ctx, pageID, at)
	if err != nil {
		return nil, notionMCPRestoreVersionOutput{}, err
	}
	if versions.At == nil && target.Database != nil {
		return nil, notionMCPRestoreVersionOutput{}, errMCPVersionBeforeDatabase
	}
	if versions.At == nil {
		return nil, notionMCPRestoreVersionOutput{}, mcpVersionBeforeMade("page")
	}
	inline := target.Database != nil && target.Database.Inline
	// An untitled one as fetch calls it, which its person reads in the question (mcp_untitled_names.go).
	title := mcpNamedOrCalled(page.GetString("title"), func() string { return mcpUntitledPageName(app, authority.PrincipalID, page) })
	address, kind := mcpPageAddress(app, authority, pageID), "page"
	if inline {
		if held, err := readMCPVersionDatabase(app, authority, target, versions.At.VersionCursor); err != nil {
			return nil, notionMCPRestoreVersionOutput{}, failedBecause(errMCPToolFailed, err)
		} else if len(held.Sources) == 0 && len(held.Views) == 0 {
			return nil, notionMCPRestoreVersionOutput{}, errMCPVersionBeforeDatabase
		}
		if title, err = notionDatabaseTurnTitle(app, notionDatabaseContainer{ID: target.Database.ContainerID}); err != nil {
			return nil, notionMCPRestoreVersionOutput{}, failedBecause(errMCPToolFailed, err)
		}
		title, address, kind = mcpDatabaseShownTitle(app, authority.PrincipalID, title), target.Database.URL, "database"
	}
	database := target.Database != nil || isFullPageDatabase(page)
	if database {
		kind = "database"
	}
	if !input.UserConfirmed {
		what := "The databases inside the page stay as they are; include_inline_databases true restores them too, should the person want that."
		switch {
		case inline && len(target.Database.SourceIDs) == 0:
			what = "Its views are restored; the database it shows and the page it is in are left as they are."
		case inline:
			what = "Its properties, rows' values and rows' titles are restored, unless database_options leaves some out; the page it is in is left as it is."
		case database:
			what = "Its properties, rows' values and rows' titles are restored, unless database_options leaves some out."
		case input.IncludeInlineDatabases:
			what = "The databases inside the page are restored too."
		}
		return nil, notionMCPRestoreVersionOutput{}, mcpConfirmationNeeded("Not restored yet: with user_confirmed: true, %q (%s) goes back to how it was at %s, and every edit after that time is undone, other people's too. %s Comments stay, those added after that time too; one on words the restore undoes is no longer marked in the text. "+mcpConfirmFirst,
			title, address, mcpTimestamp(versions.At.CreatedAt, authority.clock()), what)
	}
	body := pageHistoryRestoreRequest{RequestID: "mcp-restore:" + newDiagnosticReference(), BaseRevision: versions.Frontier,
		RestoreDatabases: database || input.IncludeInlineDatabases}
	if inline {
		body.RestoreDatabases, body.DatabaseID = false, target.Database.ContainerID
	}
	if options := input.DatabaseOptions; options != nil {
		body.KeepDatabaseProperties = options.Properties != nil && !*options.Properties
		body.KeepRowValues = options.RowPropertyValues != nil && !*options.RowPropertyValues
		body.KeepRowNames = options.RowTitles != nil && !*options.RowTitles
	}
	startedAt := time.Now().UTC()
	result, err := mcpNativeCall(ctx, "POST", "/pages/"+pageID+"/history/"+url.PathEscape(versions.At.ID)+"/restore", "", body)
	if err != nil {
		return nil, notionMCPRestoreVersionOutput{}, err
	}
	if err := mcpNativeRefusal(result); err != nil {
		return nil, notionMCPRestoreVersionOutput{}, err
	}
	return nil, notionMCPRestoreVersionOutput{
		Status: "restored", StartedAt: mcpTimestamp(startedAt.UnixMilli(), authority.clock()),
		Message: fmt.Sprintf("The %s is back as it was at that time; restoring the %s to its started_at time undoes this. Left to do: that to the person.", kind, kind),
	}, nil
}
