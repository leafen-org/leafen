package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A full-page database's page title and its data source's name are one value
// while one source of its own alone names it: Notion's page, its link,
// path and window and its API's database title all show that source's name,
// and its title typed on the page or renamed through the API (a database's
// or a data source's) renames the source, which they all then show (seen
// 2026-10-01, 2026-11-08 and 2026-11-11). Once it holds a second, each keeps
// its own (seen 2026-11-08: the database's name and its first source's go
// apart), and so does one that shows a source from another database beside
// its one, which counts among those naming it (databaseNamingSources): seen
// 2026-11-25 in Chrome at 1440 at two times, its source of its own renamed
// "F2" through the API left its page and window "F", the API's database
// title and child_database "F2", and its title typed "F3" on its page left
// the source "F2". Notion keeps a database's own name apart from its one source's
// all along, unseen until a second source is added, when its page takes
// that name again — the one it was made with, renames since being its
// source's (seen 2026-11-11, the source added through the API or the
// screen); here the page keeps the name it shows, but for one only its
// source was renamed since, through the API or MCP, which takes its own name
// again as Notion's does (full_page_database_own_name.go). The name is that of the
// one source its body's first database shows. A graph change to either
// carries the other in the same change, a change that gives the page its
// database names the source after the page, and a change to the page's
// details names the source after it. The editor writes the name to the
// source alone (nativeDatabaseViewProductAdapter changeTitle), the page
// following it here.

// databaseNamingSource is the one own source a database block's content
// shows that alone names it, or "" for a linked database, one of several
// sources or one showing a source from another database beside its own
// (databaseShowsLinkedSources), as the sources naming it count them
// (databaseNamingSources). A source sent to the trash with its views
// (databaseTrashedWithViewsField) no longer counts, as it doesn't for an
// inline database's name (inline_database_own_name.go): one of several left
// with one so goes by that one's name, its title typed renaming it, as
// Notion's (seen 2026-11-19: an untitled database of three with two so sent
// titled by the third in white, a letter typed renaming it; a titled one of
// two with one so sent titled by the other, its path, window and API title
// alike).
func databaseNamingSource(content []byte) string {
	own, live := databaseOwnSources(content)
	switch {
	case databaseShowsLinkedSources(content):
		return ""
	case len(own) == 1:
		return own[0]
	case len(live) == 1:
		return live[0]
	}
	return ""
}

// databaseShowsLinkedSources reports whether a database block shows a
// source from another database beside its own (linkedMembers, as
// 데이터 소스 관리 › 기존 데이터 소스 연결하기 adds one).
func databaseShowsLinkedSources(content []byte) bool {
	return len(databaseLinkedMemberIDs(content)) > 0
}

// databaseLinkedMemberIDs are the sources a database block shows from other
// databases beside its own, in its order.
func databaseLinkedMemberIDs(content []byte) []string {
	shown := struct {
		Members []struct {
			DataSourceID      string `json:"dataSourceId"`
			SourceContainerID string `json:"sourceContainerId"`
		} `json:"linkedMembers"`
	}{}
	if json.Unmarshal(content, &shown) != nil {
		return nil
	}
	ids := []string{}
	for _, member := range shown.Members {
		if member.DataSourceID != "" && member.SourceContainerID != "" {
			ids = append(ids, member.DataSourceID)
		}
	}
	return ids
}

// fullPageDatabaseTitleApart reports whether a full-page database's page
// keeps its title apart from its one source of its own's name while that one
// names it through the API: it shows a source from another database beside
// it. Renaming the database through the API then renames the source alone,
// as Notion's (seen 2026-11-25: "F4" given to such a database renamed its
// source, its page staying "F3").
func fullPageDatabaseTitleApart(app core.App, ownerID, pageID string) (bool, error) {
	stored, found, err := loadDatabasePageBlock(app, ownerID, pageID)
	if err != nil || !found {
		return false, err
	}
	own, _ := databaseOwnSources([]byte(stored.Content))
	return len(own) == 1 && databaseShowsLinkedSources([]byte(stored.Content)), nil
}

// databaseOwnSources is a database block's own sources in order, none for a
// linked one, and those of them not in the trash with their views.
func databaseOwnSources(content []byte) (own, live []string) {
	shown := struct {
		DataSourceID string   `json:"dataSourceId"`
		Additional   []string `json:"additionalDataSourceIds"`
		Linked       bool     `json:"linkedDatabaseSource"`
		Gone         []string `json:"trashedWithViewsDataSourceIds"`
	}{}
	if json.Unmarshal(content, &shown) != nil || shown.Linked || shown.DataSourceID == "" {
		return nil, nil
	}
	own = []string{shown.DataSourceID}
	for _, id := range shown.Additional {
		if id != "" {
			own = append(own, id)
		}
	}
	live = slices.DeleteFunc(slices.Clone(own), func(id string) bool { return slices.Contains(shown.Gone, id) })
	return own, live
}

// fullPageDatabaseTitleSource reads the source a full-page database is named
// by: the one source of the first database at the top of its body, or none.
func fullPageDatabaseTitleSource(app core.App, ownerID, pageID string) (*graphDataSource, error) {
	var block struct {
		Content string `db:"content"`
	}
	err := app.DB().NewQuery(`
		SELECT [[content]] FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_page_order]]
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:pageId} AND [[parent_id]]='' AND [[type]]='inline_database'
		ORDER BY [[position]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"ownerId": ownerID, "pageId": pageID}).One(&block)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sourceID := databaseNamingSource([]byte(block.Content))
	if sourceID == "" {
		return nil, nil
	}
	var stored struct {
		ID       string `db:"id"`
		PageID   string `db:"page_id"`
		Title    string `db:"title"`
		Spans    string `db:"title_spans"`
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
	}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[page_id]], [[title]], [[title_spans]], [[icon]], [[icon_type]] FROM {{leafen_data_sources}}
		WHERE [[owner]]={:ownerId} AND [[id]]={:id}
	`).Bind(dbx.Params{"ownerId": ownerID, "id": sourceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	source := &graphDataSource{ID: stored.ID, PageID: stored.PageID, Title: stored.Title}
	// Its name's formatting, while it spells the name, which a rename keeps
	// on the words it keeps (renamedDataSource).
	if spans := decodeStoredTextSpans(stored.Spans); len(spans) > 0 && spelledName(spans) == stored.Title {
		source.TitleSpans = spans
	}
	if stored.IconType != "" && stored.IconType != "none" {
		icon, iconType := stored.Icon, stored.IconType
		source.Icon, source.IconType = &icon, &iconType
	}
	return source, nil
}

// alignFullPageDatabaseName makes a graph change to a full-page database's
// title or its one source's name carry the other: a page title names the
// source too, and renaming the source titles its page. A change that makes a
// page a full-page database, or puts a database in place of its own, as a
// template chosen for it does, names the new source after the page.
func alignFullPageDatabaseName(
	ctx context.Context, app core.App, ownerID string, authorization pageAuthorization, actorID string, input graphMutationInput,
) (graphMutationInput, error) {
	page := authorization.Page
	aligned, err := alignFullPageDatabaseTitleAndSource(app, ownerID, page, input)
	if err == nil {
		// Its one source's icon written is its page's too, and the icon it kept
		// of its own its page's again with a second source (full_page_database_icon.go).
		aligned, err = carrySourceIconToPage(ctx, app, ownerID, authorization, actorID, aligned)
	}
	if err == nil {
		aligned, err = restoreFullPageDatabaseOwnIcon(app, ownerID, authorization, actorID, aligned)
	}
	if err != nil {
		return aligned, err
	}
	// Renamed, it keeps the name it went by on its block (database_block_title.go).
	return keepRenamedDatabaseBlockTitle(app, ownerID, page, aligned)
}

func alignFullPageDatabaseTitleAndSource(app core.App, ownerID string, page *core.Record, input graphMutationInput) (graphMutationInput, error) {
	if input.PageKind != nil {
		if *input.PageKind == pageKindDatabase && input.PageTitle != nil {
			input.Operations = nameGivenDatabaseSource(input.Operations, *input.PageTitle)
		}
		// A linked database's name goes with it between a page of its own
		// and inline (database_turn_linked_name.go); another turned inline
		// keeps the name it went by on its block (database_block_title.go).
		turned, err := turnLinkedDatabaseName(app, ownerID, page, input)
		if err == nil {
			turned, err = keepTurnedDatabaseBlockTitle(app, ownerID, page, turned)
		}
		if err != nil {
			return turned, err
		}
		// So does the icon of one of several sources of its own (database_own_icon.go).
		return turnDatabaseOwnIcon(app, ownerID, page, turned)
	}
	if !isFullPageDatabase(page) {
		return input, nil
	}
	if input.PageTitle != nil {
		var err error
		input.Operations, err = clearGeneratedDatabaseTitle(app, ownerID, page.Id, input.Operations, true, *input.PageTitle)
		if err != nil {
			return input, err
		}
	}
	// Left named by another source than before — one of several left with
	// one by the trash — it takes that one's name (full_page_database_own_name.go).
	if named, renamed, err := nameDatabaseByLeftSource(app, ownerID, page, input); err != nil || renamed {
		return named, err
	}
	source, err := fullPageDatabaseTitleSource(app, ownerID, page.Id)
	if err != nil {
		return input, err
	}
	if source == nil {
		// A database its page names takes a formatted name from its block.
		return alignDatabasePageName(app, ownerID, page, input)
	}
	// Given a second source, it takes again the name it kept of its own
	// (full_page_database_own_name.go).
	named, restored, err := restoreFullPageDatabaseOwnName(app, ownerID, page, input)
	if err != nil || restored {
		return named, err
	}
	input = named
	if input.Operations, err = carrySourceNameToPage(app, ownerID, page, source.ID, input.Operations); err != nil {
		return input, err
	}
	for _, operation := range input.Operations {
		if operation.Kind == graphOperationKindDeleteDataSource && operation.DataSourceDeletion != nil &&
			operation.DataSourceDeletion.ID == source.ID {
			title := page.GetString("title")
			if input.PageTitle != nil {
				title = *input.PageTitle
			}
			input.Operations = nameGivenDatabaseSource(input.Operations, title)
			return input, nil
		}
	}
	// A write to the source that leaves its name out (its description, icon
	// or cover) renames nothing: the page keeps its title.
	written, renamed := -1, -1
	for index, operation := range input.Operations {
		if operation.Kind == graphOperationKindUpsertDataSource && operation.DataSource != nil &&
			operation.DataSource.ID == source.ID {
			written = index
			if !operation.DataSource.TitleOmitted {
				renamed = index
			}
		}
	}
	if input.PageTitle != nil {
		if renamed >= 0 {
			if input.Operations[renamed].DataSource.Title == *input.PageTitle {
				return input, nil
			}
			// A new title names the database; a write that sends the title
			// unchanged, as a body write does, takes the database's new name.
			if *input.PageTitle == page.GetString("title") {
				title := input.Operations[renamed].DataSource.Title
				input.PageTitle = &title
				return input, nil
			}
			operations := append([]graphOperation(nil), input.Operations...)
			operations[renamed].DataSource = renamedDataSource(*input.Operations[renamed].DataSource, *input.PageTitle)
			input.Operations = operations
		} else if source.Title != *input.PageTitle {
			name := renamedDataSource(*source, *input.PageTitle)
			operations := append([]graphOperation(nil), input.Operations...)
			if written >= 0 {
				// The write that leaves it out takes the new name with it.
				change := *operations[written].DataSource
				change.Title, change.TitleSpans, change.TitleOmitted = name.Title, name.TitleSpans, false
				operations[written].DataSource = &change
			} else {
				operations = append(operations, graphOperation{Kind: graphOperationKindUpsertDataSource, DataSource: name})
			}
			input.Operations = operations
		}
		return input, nil
	}
	if renamed >= 0 && input.Operations[renamed].DataSource.Title != page.GetString("title") {
		title := input.Operations[renamed].DataSource.Title
		input.PageTitle = &title
	}
	return input, nil
}

// nameGivenDatabaseSource names the one source of the database a change puts
// at the top of a page after the page's title, where the change writes that
// source: a template's source is named by the page it is applied to.
func nameGivenDatabaseSource(operations []graphOperation, title string) []graphOperation {
	sourceID := ""
	for _, operation := range operations {
		block := operation.Block
		if operation.Kind == graphOperationKindUpsertBlock && block != nil && block.Type == graphBlockTypeInlineDatabase &&
			(block.ParentID == nil || *block.ParentID == "") {
			sourceID = databaseNamingSource(block.Content)
			break
		}
	}
	for index, operation := range operations {
		if sourceID == "" || operation.Kind != graphOperationKindUpsertDataSource || operation.DataSource == nil ||
			operation.DataSource.ID != sourceID || operation.DataSource.Title == title {
			continue
		}
		next := append([]graphOperation(nil), operations...)
		next[index].DataSource = renamedDataSource(*operation.DataSource, title)
		return next
	}
	return operations
}

// nameFullPageDatabase names a full-page database after its page's title,
// once the page's details changed it, and gives its one source its page's
// icon when they gave one.
func (p *pocketBaseYgoPersistence) nameFullPageDatabase(ctx context.Context, principalID, pageID string, titleEdited, iconGiven bool) error {
	page, err := p.app.FindRecordById(pagesCollection, pageID)
	// A database in the trash keeps its name until it comes back, when this
	// names it again; moving it there isn't a failure to name it.
	if err != nil || !isFullPageDatabase(page) || page.GetBool("in_trash") {
		return err
	}
	authorization, err := authorizePage(p.app, principalID, pageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return nil
	}
	if err != nil {
		return err
	}
	operations, err := clearGeneratedDatabaseTitle(p.app, authorization.Workspace.OwnerID, pageID, nil, titleEdited, page.GetString("title"))
	if err != nil {
		return err
	}
	if len(operations) > 0 {
		_, err = p.applyContentGraphMutationContext(ctx, principalID, pageID, graphMutationInput{MutationID: "database-title-written:" + newDiagnosticReference()}, contentGraphMutationHooks{
			Prepare: func(app core.App, auth pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, err := clearGeneratedDatabaseTitle(app, auth.Workspace.OwnerID, pageID, nil, titleEdited, auth.Page.GetString("title"))
				return contentGraphMutationPreparation{Operations: current}, err
			},
		})
		if err != nil {
			return err
		}
	}
	source, err := fullPageDatabaseTitleSource(p.app, authorization.Workspace.OwnerID, pageID)
	if err != nil || source == nil {
		return err
	}
	// Its page's icon given is its one source's too (full_page_database_icon.go).
	written := (*graphDataSource)(nil)
	if iconGiven {
		written = sourceIconFollowingPage(page, source)
	}
	if source.Title != page.GetString("title") {
		renamed := renamedDataSource(*source, page.GetString("title"))
		if written != nil {
			renamed.Icon, renamed.IconType = written.Icon, written.IconType
		}
		written = renamed
	}
	if written == nil {
		return nil
	}
	_, err = p.applyContentGraphMutationContext(ctx, principalID, pageID, graphMutationInput{
		MutationID: "full-page-database-name:" + newDiagnosticReference(),
		Operations: []graphOperation{{Kind: graphOperationKindUpsertDataSource, DataSource: written}},
	}, contentGraphMutationHooks{})
	return err
}
