package app

import (
	"context"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// settleDatabasePageLeft finishes a database page's turn into a page
// (convertDatabaseRowToPage), whichever road took it out — the app's row
// move, the public API's and MCP's moves, a restore from a data source in
// the trash. A wiki's page leaving it as a page takes its marked link with
// it, and one under the page holding its database takes its block there,
// done again as the server starts when its stop cut them
// (ygo_persistence_follow_ups.go). Then one put under a wiki, or under a
// page of one, becomes the wiki's page with the pages under it, owned by
// the person who moved it, as every page put there does
// (wiki_page_adoption.go) and as Notion's API makes a database page moved
// there (seen 2026-11-19: moved to a wiki, to a page of one, or to its data
// source, the wiki's page with the wiki's properties, the mover its owner).
func (p *pocketBaseYgoPersistence) settleDatabasePageLeft(
	ctx context.Context, request databaseRowToPageRequest, authority databaseRowPageAuthority,
) error {
	left := databasePageLeft{
		ownerID: authority.OwnerID, workspaceID: authority.WorkspaceID, actorID: request.PrincipalID, pageID: authority.RowID,
	}
	if request.ParentID == request.ContainerPageID {
		left.holderID = request.ParentID
	}
	if err := p.runKeptFollowUp(left.kept(), left.write); err != nil {
		return err
	}
	_, err := p.adoptPageTreeIntoWiki(ctx, request.PrincipalID, authority.RowID)
	return err
}

// movePageIntoDatabase moves a page into a database as its page
// (convertPageToDatabaseRow) — the app's, the public API's and MCP's moves;
// one moved into a wiki's data source takes the live pages under it into the
// wiki (adoptPageTreeIntoWiki). An import's page turned into a row is the
// import's to place (workspace_import_database_place.go).
func (p *pocketBaseYgoPersistence) movePageIntoDatabase(
	ctx context.Context, request pageToDatabaseRowRequest,
) (graphMutationReceipt, error) {
	receipt, err := p.convertPageToDatabaseRow(ctx, request)
	if err != nil {
		return receipt, err
	}
	_, err = p.adoptPageTreeIntoWiki(ctx, request.PrincipalID, request.PageID)
	return receipt, err
}

// applyDatabaseRowPageMoveAcross moves a database's page to a data source
// on another page (applyDatabaseRowPageMoveAcrossRecord); one moved into a
// wiki's takes the live pages under it into the wiki, its owner the mover
// (prepareWikiRowValues).
func (p *pocketBaseYgoPersistence) applyDatabaseRowPageMoveAcross(
	ctx context.Context, request databaseRowPageMoveAcrossRequest, access databaseRowPageMoveAccess,
) (graphMutationReceipt, error) {
	receipt, err := p.applyDatabaseRowPageMoveAcrossRecord(ctx, request, access)
	if err != nil {
		return receipt, err
	}
	_, err = p.adoptPageTreeIntoWiki(ctx, request.PrincipalID, request.RowID)
	return receipt, err
}

// ownRowsMovedIntoWiki names whoever moves database pages into a wiki's data
// source, or another whose pages are verified, as their owner where no value
// carried with them names one: a page new to such a database is owned by
// whoever puts it there (prepareWikiRowValues), but a move from another
// database's page takes the rows out of their source before that rule reads
// them, so the move names it.
func ownRowsMovedIntoWiki(app core.App, dataSourceID, principalID string, operations []graphOperation) error {
	properties, named, err := loadVerificationSourceProperties(app, dataSourceID)
	if err != nil || !named || properties.Owner == "" {
		return err
	}
	owner, err := json.Marshal([]string{principalID})
	if err != nil {
		return err
	}
	for _, operation := range operations {
		row := operation.Row
		if operation.Kind != graphOperationKindUpsertRow || row == nil || row.DataSourceID != dataSourceID {
			continue
		}
		if _, carried := row.Values[properties.Owner]; carried {
			continue
		}
		if row.Values == nil {
			row.Values = map[string]graphRowValue{}
		}
		row.Values[properties.Owner] = graphRowValue{Type: graphValueTypePerson, Value: owner}
	}
	return nil
}

// moveWikiPageWithinWiki moves a wiki's page put under its own wiki's page,
// or under another page of its wiki, within the wiki (changeWikiPage): it
// stays the wiki's page with its values, last under that page, as the app's
// Move to keeps it (useDatabasePageMove.ts) and Notion's does (seen
// 2026-11-11). within is false for any other move, which takes the database
// page out of its database.
func (p *pocketBaseYgoPersistence) moveWikiPageWithinWiki(
	ctx context.Context, request databaseRowToPageRequest,
) (receipt graphMutationReceipt, within bool, err error) {
	if request.ParentID == "" {
		return receipt, false, nil
	}
	sourceID := ""
	err = p.app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": request.RowID}).Row(&sourceID)
	if err != nil || sourceID == "" {
		return receipt, false, ignoreNoRows(err)
	}
	place, inWiki, err := loadWikiPlace(p.app, request.ParentID)
	if err != nil || !inWiki || place.SourceID != sourceID {
		return receipt, false, err
	}
	parentID := request.ParentID
	response, err := p.changeWikiPage(ctx, request.PrincipalID, request.RowID, databaseRowPageWikiChangeInput{
		RequestID: request.MutationID, ParentID: &parentID,
	})
	return graphMutationReceipt{MutationID: request.MutationID, Duplicate: response.Duplicate}, true, err
}
