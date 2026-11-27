package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A linked database in Notion is a block with views of its own over another
// database's data source. The older API version refuses it, and the view API
// gives its views. The import stages the block where it stood, as an empty
// paragraph, and its views; the link phase, when every database exists, turns
// the paragraph into a linked database of the imported data source. When that
// data source did not come over, the block is reported and goes, by why
// (2026-11-23: a move's report said 674 times "a database Notion didn't give,
// or whose source didn't come", once for each row a template had made with a
// linked database of a database outside the move): one showing a database
// outside what the import read is named by that database, so each such
// database is one line (linked_source_outside); one whose source Notion won't
// give the connection says so (linked_source_unavailable), and of a linked
// database whose other views came, that view alone; one whose source the
// import read and didn't make says that, as before (database_unavailable).

// notionUnnamedLinkedDatabaseTitle is what Notion's API calls a linked
// database without a name of its own.
const notionUnnamedLinkedDatabaseTitle = "Untitled"

// notionImportLinkedSourceKey finds what the import calls a data source a
// linked view shows: its database, for a database's first, or the key of its
// others. It reports false when Notion won't say.
func notionImportLinkedSourceKey(
	ctx context.Context, client notionProviderClient, payload *notionImportJobPayload, dataSourceID string,
) (string, bool, error) {
	if key, known := payload.LinkedSourceKeys[dataSourceID]; known {
		return key, key != "", nil
	}
	remember := func(key string) (string, bool, error) {
		if payload.LinkedSourceKeys == nil {
			payload.LinkedSourceKeys = map[string]string{}
		}
		payload.LinkedSourceKeys[dataSourceID] = key
		return key, key != "", nil
	}
	source, err := client.dataSource(ctx, dataSourceID)
	if notionProviderRefused(err) {
		return remember("")
	}
	if err != nil {
		return "", false, err
	}
	databaseID := source.Parent.DatabaseID
	if source.Parent.Type != "database_id" || databaseID == "" {
		return remember("")
	}
	named := remember
	remember = func(key string) (string, bool, error) {
		if payload.LinkedSourceNames == nil {
			payload.LinkedSourceNames = map[string]string{}
		}
		payload.LinkedSourceNames[key] = notionObjectTitle(source)
		return named(key)
	}
	listed := struct {
		DataSources []notionProviderOtherDataSource `json:"data_sources"`
	}{}
	err = client.requestVersion(
		ctx, notionProviderDataSourceAPIVersion, http.MethodGet, "/v1/databases/"+url.PathEscape(databaseID), nil, &listed,
	)
	if notionProviderRefused(err) {
		return remember("")
	}
	if err != nil {
		return "", false, err
	}
	if len(listed.DataSources) > 0 && !notionImportSameID(listed.DataSources[0].ID, dataSourceID) {
		return remember(notionImportDataSourceKey(databaseID, dataSourceID))
	}
	return remember(databaseID)
}

// notionImportLinkedViewSources reads the views of a database block Notion
// refused and stages them, or nothing when it is no linked database the import
// can follow; with them, each one whose source Notion won't give, reported,
// and unavailable when it had only such views.
func notionImportLinkedViewSources(
	ctx context.Context, client notionProviderClient, payload *notionImportJobPayload, databaseID string,
) (sources []stagedNotionSource, unavailable bool, err error) {
	ids, cursor := []string{}, ""
	for {
		page, err := client.databaseViews(ctx, databaseID, cursor)
		if notionProviderRefused(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		ids = append(ids, page.IDs...)
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}
	views, withheld := []stagedNotionSource{}, []stagedNotionSource{}
	for _, id := range ids {
		view, err := client.view(ctx, id)
		if notionProviderRefused(err) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if view.DataSourceID == nil || *view.DataSourceID == "" {
			continue
		}
		key, known, err := notionImportLinkedSourceKey(ctx, client, payload, *view.DataSourceID)
		if err != nil {
			return nil, false, err
		}
		name := notionImportViewName(view.Name, view.Type)
		if !known {
			withheld = append(withheld, notionImportViewIssueSource(
				databaseID, notionImportViewSourceID(databaseID, view.ID), name, notionImportIssueLinkedSourceUnavailable, len(withheld),
			))
			continue
		}
		if notionMigrationSourceKey(key) == notionMigrationSourceKey(databaseID) {
			continue
		}
		plan := notionImportViewPlan{
			ID: view.ID, Name: notionImportViewGivenName(view.Name), Type: view.Type, DataSourceKey: key, LinkedBlock: databaseID,
			Filter: notionImportPresentJSON(view.Filter), Sorts: notionImportPresentJSON(view.Sorts),
			QuickFilters:  notionImportPresentJSON(view.QuickFilters),
			Configuration: notionImportPresentJSON(view.Configuration),
		}
		encoded, err := json.Marshal(plan)
		if err != nil {
			return nil, false, err
		}
		if len(encoded) > notionImportViewPlanBytes {
			plan.Filter, plan.Sorts, plan.QuickFilters, plan.Configuration, plan.Truncated = nil, nil, nil, nil, true
			if encoded, err = json.Marshal(plan); err != nil {
				return nil, false, err
			}
		}
		views = append(views, stagedNotionSource{
			SourceID: notionImportViewSourceID(databaseID, view.ID), Kind: "view", ParentSourceID: databaseID,
			Position: len(views), Title: name, Text: string(encoded),
		})
	}
	if len(views) == 0 {
		return nil, len(withheld) > 0, nil
	}
	return append(views, withheld...), false, nil
}

// stageNotionImportLinkedBlock turns a staged database into the block a linked
// database holds its place with.
func stageNotionImportLinkedBlock(app dbxExecutor, jobID, databaseID string) error {
	encoded, err := encodeNotionParagraphSourcePlan(notionParagraphSourcePlan{
		Spans: []notionParagraphSpan{}, BlockType: graphBlockTypeParagraph, LinkedDatabase: true,
	})
	if err != nil {
		return err
	}
	_, err = app.NewQuery(`
		UPDATE {{leafen_notion_import_sources}} SET [[kind]]='block', [[text]]={:text}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]='database'
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": databaseID, "text": encoded}).Execute()
	return err
}

type dbxExecutor interface {
	NewQuery(string) *dbx.Query
}

// linkedViewPlace is where a linked database's view goes: its block, on the
// page that holds it, of the data source it shows. Gone is a block an
// earlier run made that isn't there any more (goneLinkedBlock).
type linkedViewPlace struct {
	block        storedNotionBlock
	gone         bool
	title        string
	dataSourceID string
	reference    linkedDatabaseReferenceRow
}

// linkedViewPlace reads a linked view's place, or nil when its block did not
// come over.
func (linker *notionImportLinker) linkedViewPlace(plan notionImportViewPlan) (*linkedViewPlace, error) {
	blockID, err := linker.lookup(plan.LinkedBlock, "block")
	if err != nil || blockID == "" {
		return nil, err
	}
	block, found, err := loadStoredNotionBlock(linker.app, blockID)
	if err != nil {
		return nil, err
	}
	titles := []string{}
	if err := linker.app.DB().NewQuery(`
		SELECT [[title]] FROM {{leafen_notion_import_sources}} WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId}
	`).Bind(dbx.Params{"jobId": linker.job.ID, "sourceId": plan.LinkedBlock}).Column(&titles); err != nil {
		return nil, err
	}
	place := &linkedViewPlace{block: block, gone: !found}
	if len(titles) == 1 {
		place.title = titles[0]
	}
	dataSourceID, err := linker.lookup(plan.DataSourceKey, "data_source")
	if err != nil {
		return nil, err
	}
	if dataSourceID != "" {
		place.reference, err = loadLinkedDatabaseReference(linker.app, linker.payload.AuthorityOwnerID, linker.job.WorkspaceID, "", dataSourceID)
		if errors.Is(err, sql.ErrNoRows) {
			dataSourceID, err = "", nil
		}
		if err != nil {
			return nil, err
		}
	}
	place.dataSourceID = dataSourceID
	return place, nil
}

// dropLinkedBlock takes a linked database's empty place away and reports it,
// when its data source did not come over and nothing else fills it; a view
// of one its other views fill whose source didn't come is reported alone —
// one before them too, staged at sequence: a later view whose source came
// makes the linked database (before 2026-11-24 the first view's source
// outside the move took the block away, the later views of the move's
// sources with it).
func (linker *notionImportLinker) dropLinkedBlock(plan notionImportViewPlan, place *linkedViewPlace, sequence int64) error {
	reason, subject, err := linker.linkedSourceMissing(plan, place)
	if err != nil {
		return err
	}
	filled := place.block.Type != graphBlockTypeParagraph
	if !filled {
		if filled, err = linker.laterLinkedViewFills(plan.LinkedBlock, sequence); err != nil {
			return err
		}
	}
	if filled {
		if place.dataSourceID != "" {
			return nil
		}
		return recordNotionImportLinkedViewIssue(
			linker.app, linker.job.ID, notionImportViewSourceID(plan.LinkedBlock, plan.ID), subject, reason,
		)
	}
	batch, err := loadStructuralMutationBatch(linker.app, place.block.PageID, linker.documents)
	if err != nil {
		return err
	}
	if err := applyNotionGraphOperationsWith(
		linker.app, batch, linker.payload.AuthorityOwnerID, linker.job.WorkspaceID, place.block.PageID, linker.job.Owner,
		[]graphOperation{{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: place.block.ID}}},
		notionGraphApplyReferences{},
	); err != nil {
		return err
	}
	return recordNotionImportPublicationIssue(linker.app, linker.job.ID, plan.LinkedBlock, subject, reason)
}

// goneLinkedBlock says again, in a run again or a resumed run, why a linked
// database an earlier run made the place of and took away (dropLinkedBlock)
// isn't there: its block's line, as that run said it, once, by the first of
// its views — by why its source didn't come, or, when it came, by what the
// migration still names of the block (2026-11-24: a run again's list of
// what didn't come whole lost the line its first run gave, the views' lines
// staying). One a person deleted in Leafen since, whose source came, says
// nothing.
func (linker *notionImportLinker) goneLinkedBlock(plan notionImportViewPlan, place *linkedViewPlace) error {
	params := dbx.Params{
		"jobId": linker.job.ID, "migrationId": linker.payload.MigrationID, "prefix": plan.LinkedBlock + "#",
		"viewParent": notionImportViewIssueParentKind,
	}
	var said int
	if err := linker.app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='issue' AND substr([[source_id]], 1, length({:prefix}))={:prefix}
	`).Bind(params).Row(&said); err != nil || said > 0 {
		return err
	}
	if place.dataSourceID == "" {
		reason, subject, err := linker.linkedSourceMissing(plan, place)
		if err != nil {
			return err
		}
		return recordNotionImportPublicationIssue(linker.app, linker.job.ID, plan.LinkedBlock, subject, reason)
	}
	standing := struct {
		Subject string `db:"subject"`
		Reason  string `db:"reason"`
	}{}
	err := linker.app.DB().NewQuery(`
		SELECT [[subject]], [[reason]] FROM {{leafen_notion_migration_issues}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]<>{:viewParent} AND substr([[issue_id]], 1, length({:prefix}))={:prefix}
		ORDER BY [[issue_id]] LIMIT 1
	`).Bind(params).One(&standing)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return recordNotionImportPublicationIssue(linker.app, linker.job.ID, plan.LinkedBlock, standing.Subject, standing.Reason)
}

// laterLinkedViewFills says whether a view of the linked database staged
// after sequence shows a data source the import made, which makes the
// linked database of it.
func (linker *notionImportLinker) laterLinkedViewFills(linkedBlock string, sequence int64) (bool, error) {
	texts := []string{}
	if err := linker.app.DB().NewQuery(`
		SELECT [[text]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='view' AND [[parent_source_id]]={:block} AND [[sequence]]>{:sequence}
		ORDER BY [[sequence]]
	`).Bind(dbx.Params{"jobId": linker.job.ID, "block": linkedBlock, "sequence": sequence}).Column(&texts); err != nil {
		return false, err
	}
	for _, text := range texts {
		later := notionImportViewPlan{}
		if json.Unmarshal([]byte(text), &later) != nil || later.LinkedBlock != linkedBlock || later.DataSourceKey == "" {
			continue
		}
		place, err := linker.linkedViewPlace(later)
		if err != nil {
			return false, err
		}
		if place != nil && !place.gone && place.dataSourceID != "" {
			return true, nil
		}
	}
	return false, nil
}

// linkedSourceMissing is why a linked view's source isn't there, and what the
// report names it by: one the import didn't read, outside it, by its own
// name; one the import read and didn't make, by the linked database's.
func (linker *notionImportLinker) linkedSourceMissing(plan notionImportViewPlan, place *linkedViewPlace) (string, string, error) {
	if place.dataSourceID != "" {
		return notionImportIssueDatabaseUnavailable, place.title, nil
	}
	database, _, _ := strings.Cut(plan.DataSourceKey, notionImportDataSourceKeyMarker)
	var read int
	if err := linker.app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[source_id]] IN ({:key}, {:database})
	`).Bind(dbx.Params{"jobId": linker.job.ID, "key": plan.DataSourceKey, "database": database}).Row(&read); err != nil || read > 0 {
		return notionImportIssueDatabaseUnavailable, place.title, err
	}
	name := strings.TrimSpace(linker.payload.LinkedSourceNames[plan.DataSourceKey])
	return notionImportIssueLinkedSourceOutside, cmpOr(name, place.title), nil
}

// recordNotionImportLinkedViewIssue reports one view of a linked database,
// under it, named by subject.
func recordNotionImportLinkedViewIssue(app core.App, jobID, viewSourceID, subject, reason string) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_sources}}
			([[job_id]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]],
			 [[text]], [[blueprint]], [[discovery_status]], [[discovery_cursor]], [[discovery_position]], [[created]])
		SELECT [[job_id]], [[source_id]] || '#' || {:reason}, 'issue', [[parent_source_id]], {:parentKind},
			[[position]], {:subject}, {:reason}, 0, 'complete', '', 0, {:created}
		FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]='view'
		ON CONFLICT ([[job_id]], [[source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "sourceId": viewSourceID, "reason": reason, "subject": subject,
		"parentKind": notionImportViewIssueParentKind, "created": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

// blockOperation turns a linked database's place into the block showing
// the view, the first time one of its views is made.
func (place *linkedViewPlace) blockOperation(viewID string, sourceTitle string) (graphOperation, bool) {
	if place.block.Type != graphBlockTypeParagraph {
		return graphOperation{}, false
	}
	content := map[string]any{
		"dataSourceId": place.dataSourceID, "viewId": viewID,
		"linkedDatabaseSource": true, "sourceContainerId": place.reference.SourceContainerID,
	}
	// It keeps a name of its own when Notion gave it one apart from its
	// source's. Notion's API reads one never named as "Untitled" (its
	// child_database title and its database title, seen 2026-11-21), where
	// Notion's screen heads it with its source's name and its Markdown leaves
	// its tag empty: it comes over unnamed, as one made in Leafen is. One
	// named "Untitled" by hand reads the same through the API, so it comes
	// over unnamed too, as a view so named does (notionImportViewOwnName).
	if title := place.title; strings.TrimSpace(title) != "" && title != sourceTitle && title != notionUnnamedLinkedDatabaseTitle {
		content["linkedTitle"] = title
	}
	block := &graphBlock{
		ID: place.block.ID, Position: place.block.Position, Type: graphBlockTypeInlineDatabase, Content: notionImportJSON(content),
	}
	if place.block.ParentID != "" {
		parentID := place.block.ParentID
		block.ParentID = &parentID
	}
	return graphOperation{Kind: graphOperationKindUpsertBlock, Block: block}, true
}

// memberOperation makes the data source a later view of a linked database
// shows one of its members, when it is none yet: Notion's linked database
// holds views of several sources, and Leafen's holds each source after its
// first as a linked member, naming the database that holds it
// (database_container_linked_members.go), as one made in Leafen does
// (2026-11-24: a template's linked database with a view of each of two
// databases the move brought came with the first view alone, said nowhere
// but in the run's check). It gives the block's write with the sources of
// all its members, which their views may name; nil when the source is one.
func (place *linkedViewPlace) memberOperation(
	app core.App, owner, workspaceID string,
) (*graphOperation, map[string]graphEntityReference, error) {
	content, err := parseDatabaseContainerBlockContent(json.RawMessage(place.block.Content))
	if err != nil || !content.LinkedDatabaseSource || slices.Contains(content.memberDataSourceIDs(), place.dataSourceID) {
		return nil, nil, err
	}
	stored := map[string]any{}
	if err := json.Unmarshal([]byte(place.block.Content), &stored); err != nil {
		return nil, nil, err
	}
	stored["linkedMembers"] = append(content.LinkedMembers, databaseContainerLinkedMember{
		DataSourceID: place.dataSourceID, SourceContainerID: place.reference.SourceContainerID,
	})
	members := map[string]graphEntityReference{}
	for _, member := range content.linkedMemberList() {
		row, err := loadLinkedDatabaseReference(app, owner, workspaceID, member.SourceContainerID, member.DataSourceID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		members[row.DataSourceID] = graphEntityReference{ID: row.DataSourceID, Owner: row.OwnerID, PageID: row.SourcePageID}
	}
	block := &graphBlock{
		ID: place.block.ID, Position: place.block.Position, Type: graphBlockTypeInlineDatabase, Content: notionImportJSON(stored),
	}
	if place.block.ParentID != "" {
		parentID := place.block.ParentID
		block.ParentID = &parentID
	}
	return &graphOperation{Kind: graphOperationKindUpsertBlock, Block: block}, members, nil
}

// references are what a linked view may name on its source's page.
func (place *linkedViewPlace) references(app core.App, view *graphDatabaseView) (notionGraphApplyReferences, error) {
	row := place.reference
	sources := map[string]graphEntityReference{row.DataSourceID: {ID: row.DataSourceID, Owner: row.OwnerID, PageID: row.SourcePageID}}
	referenced, err := loadGraphReferences(app, "leafen_database_properties", linkedGraphViewPropertyIDs(view))
	if err != nil {
		return notionGraphApplyReferences{}, err
	}
	properties := map[string]graphEntityReference{}
	for id, property := range referenced {
		if property.DataSourceID == row.DataSourceID && property.PageID == row.SourcePageID {
			properties[id] = property
		}
	}
	return notionGraphApplyReferences{sources: sources, properties: properties}, nil
}

// sourceTitle reads a data source's title.
func (linker *notionImportLinker) sourceTitle(dataSourceID string) (string, error) {
	titles := []string{}
	err := linker.app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": dataSourceID}).Column(&titles)
	if err != nil || len(titles) != 1 {
		return "", err
	}
	return titles[0], nil
}
