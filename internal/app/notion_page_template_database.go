package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page made under a page from a database named as its template becomes a
// copy of that database, as Notion's does (seen 2026-11-14 through its API,
// 2026-03-11, on synthetic pages): the copy has the database's name,
// description, sources, views and rows with their bodies, the request's icon
// or else the database's, and stays inline when the database is, standing
// where the page was made; the page's ID gives way to the copy's. The copy
// keeps the database's name, as a page's copy carried by a template does. Notion
// answers with the page it made, before it copies: Leafen answers with the
// same page named by the copy's ID, once the copy is done or has gone on for
// notionDataSourceMoveWait, as a database turned through the API is waited
// for (notion_database_turn.go). A copy still going on then lands in the
// page, as an inline database. Under a data source, a database named gives
// nothing (notionPageTemplateSource.copies).

// copyNotionPageTemplateDatabase makes the page a copy of the database
// source names, with the request's icon, and returns the copy's ID.
func copyNotionPageTemplateDatabase(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	source notionPageTemplateSource, pageID string, icon json.RawMessage,
) (string, error) {
	ctx, app := event.Request.Context(), event.App
	container, err := loadNotionDatabaseContainerRecord(app, identity.WorkspaceID, source.ContainerID)
	if err != nil {
		return "", err
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return "", err
	}
	parentID := page.GetString("parent_id")
	inline := !container.fullPage() && parentID != ""
	input := blockMoveInput{RequestID: "notion-template-database:" + newDiagnosticReference(), DestinationPageID: pageID}
	if inline {
		// An inline database's copy stands where the page's block does.
		placeParent := ""
		err := app.DB().NewQuery(`SELECT [[parent_id]] FROM {{leafen_blocks}} WHERE [[id]] = {:id} AND [[page_id]] = {:parentId}`).
			Bind(dbx.Params{"id": pageID, "parentId": parentID}).Row(&placeParent)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		input.DestinationPageID, input.PlaceParent, input.PlaceAfter = parentID, placeParent, pageID
	}
	admission, err := admitDatabaseTemplateCopy(app, identity.PrincipalID, container.PageID, container.BlockID, input)
	if errors.Is(err, errBlockMoveConflict) {
		return "", errDatabaseRowPageConflict
	}
	if err != nil {
		return "", err
	}
	databaseID, err := ensureNotionObjectIdentity(
		app, notionObjectIdentityDatabase, identity.WorkspaceID, admission.Job.TargetRootBlockID,
	)
	if err != nil {
		return "", err
	}
	phase, err := driveDataSourceMove(ctx, app, admission.Job.ID, notionDataSourceMoveWait)
	if err != nil {
		return "", err
	}
	if phase != blockMovePhaseCompleted {
		if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
			runner.notify()
		}
		if phase == blockMovePhaseFailed {
			return "", errDatabaseRowPageConflict
		}
		return databaseID, nil
	}
	copied, err := loadNotionDatabaseContainerRecord(app, identity.WorkspaceID, admission.Job.TargetRootBlockID)
	if err != nil {
		return "", err
	}
	if len(icon) > 0 {
		if err := updateNotionDatabaseSource(event, ygo, identity, copied, notionDatabaseUpdateRequest{Icon: icon}, "", nil, nil, nil); err != nil {
			return "", err
		}
	}
	if inline {
		// The page made way for the copy, as a database turned inline leaves
		// its page.
		return databaseID, trashNotionTurnPage(app, identity, pageID)
	}
	title, err := notionDatabaseTurnTitle(app, copied)
	if err != nil {
		return "", err
	}
	// The database's icon and cover, where the request gave none.
	host, err := app.FindRecordById(pagesCollection, container.PageID)
	if err != nil {
		return "", err
	}
	presentation := notionPagePresentationRequest{}
	if icon, iconType := host.GetString("icon"), host.GetString("icon_type"); page.GetString("icon") == "" && icon != "" {
		if _, managed := productFilePresentationID(icon); !managed || iconType != "image" {
			presentation.Icon, presentation.IconType = &icon, &iconType
		}
	}
	if cover := host.GetString("cover"); page.GetString("cover") == "" && cover != "" {
		if _, managed := productFilePresentationID(cover); !managed {
			presentation.Cover = &cover
		}
	}
	spans := []graphTextSpan{}
	if err := updateNotionDatabasePage(app, identity, pageID, presentation, func(current *core.Record) pageMetadataInput {
		return pageMetadataInput{
			Title: title, TitleSpans: &spans, ParentID: current.GetString("parent_id"),
			MutationID: "notion-public-template-database:" + newDiagnosticReference(), BaseCursor: current.GetInt("cursor"),
		}
	}); err != nil {
		return "", err
	}
	return databaseID, setNotionTurnPageKind(ctx, ygo, identity, pageID, pageKindDatabase)
}
