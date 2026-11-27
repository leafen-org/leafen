package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A database update changes what the editor changes. The title, icon and
// description are the first source's, and a full-page database's page takes
// the title and icon too; the title and icon of one of several sources of its
// own are its own (updateNotionDatabaseSource); a linked database's title and icon are its block's
// own, and its description is its source's, which the request leaves alone,
// as Notion's (seen 2026-10-30). Trash and moves go through the database
// block, or through the page of a full-page database, which also holds the
// cover and lock. is_inline turns the database (notion_database_turn.go).

type notionDatabaseUpdateRequest struct {
	Title       json.RawMessage          `json:"title,omitempty"`
	Description json.RawMessage          `json:"description,omitempty"`
	Icon        json.RawMessage          `json:"icon,omitempty"`
	Cover       json.RawMessage          `json:"cover,omitempty"`
	Parent      *notionPageParentRequest `json:"parent,omitempty"`
	IsInline    *bool                    `json:"is_inline,omitempty"`
	IsLocked    *bool                    `json:"is_locked,omitempty"`
	InTrash     *bool                    `json:"in_trash,omitempty"`
	// Properties are taken and left alone, as Notion's API does since its data
	// sources (2025-09-03 and 2026-03-11, seen 2026-11-08): a schema changes
	// through its data source.
	Properties json.RawMessage `json:"properties,omitempty"`
}

func (input notionDatabaseUpdateRequest) empty() bool {
	return input.Title == nil && input.Description == nil && input.Icon == nil && input.Cover == nil &&
		input.Parent == nil && input.IsInline == nil && input.IsLocked == nil && input.InTrash == nil && input.Properties == nil
}

func serveNotionDatabaseUpdate(event *core.RequestEvent, ygo *pocketBaseYgoPersistence, publicID string) error {
	if ygo == nil {
		return notionRequestNotCompleted(event, nil)
	}
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
	if err != nil || !ok {
		return err
	}
	identity, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDatabase, publicID)
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && identity.Workspace != workspace.Identity.WorkspaceID {
		return notionPageNotFound(event)
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	input := notionDatabaseUpdateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	container, page, err := loadNotionDatabaseContainer(event.App, workspace.Identity, identity.InternalID, permissionWorkspaceWrite)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	// A database in the trash takes only being taken out, one under a page
	// in the trash nothing — the same trash again and an empty change
	// neither (notion_trash_refusal.go); the trash goes beside the rest
	// (notion_database_trash_turn.go).
	restoring := input.InTrash != nil && !*input.InTrash
	if message, err := notionTrashedDatabaseEditMessage(event.App, container, restoring); err != nil {
		return notionDataSourceMutationError(event, err)
	} else if message != "" {
		return notionPageValidationError(event, message)
	}
	// A body that changes nothing answers with the database as it is, as
	// Notion's does (seen 2026-11-23).
	if input.empty() {
		return serveNotionDatabaseObject(event, workspace.Identity, container.ID)
	}
	err = applyNotionDatabaseUpdate(event, ygo, workspace.Identity, container, page, input)
	if err == nil {
		err = recordConnectionStoredMark(event.App, workspace.Identity, connectionAuthoredDatabase, container.ID, false,
			notionBlockEditMark(container.Updated))
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	return serveNotionDatabaseObject(event, workspace.Identity, container.ID)
}

// updateNotionDatabase applies every change except trash: the source title
// and icon first, then the full-page database's page, then an inline move.
func updateNotionDatabase(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, page pageAuthorization, input notionDatabaseUpdateRequest,
) error {
	// Notion's API takes no null icon for a database (seen 2026-10-30).
	if string(input.Icon) == "null" {
		return notionDatabaseRowPageRequestInvalid("%s", notionBodyFault("body.icon", "an object or `undefined`", "null"))
	}
	fullPage := container.fullPage()
	if input.IsInline != nil && *input.IsInline == fullPage {
		// The rest of the request applies to the database as turned.
		if err := turnNotionDatabase(event.Request.Context(), event.App, ygo, identity, container, page, *input.IsInline); err != nil {
			return err
		}
		rest := input
		rest.IsInline = nil
		if rest.empty() {
			return nil
		}
		turned, turnedPage, err := loadNotionDatabaseContainer(event.App, identity, container.ID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		return updateNotionDatabase(event, ygo, identity, turned, turnedPage, rest)
	}
	if !fullPage && input.Cover != nil {
		return notionDatabaseRowPageRequestInvalid("An inline database has no cover; only a database on a page of its own does.")
	}
	// None, or only spaces, leaves it unnamed (notionDataSourceName), as
	// Notion's (seen 2026-11-13: [], "" and "   " each read back as "New
	// database", its source's title []), and takes a linked one back to its
	// source's name. The name's and description's mentions are named as its
	// page's blocks' are.
	mentions := notionNameMentions(event.App, identity, page)
	title, titleSpans := "", []graphTextSpan(nil)
	if input.Title != nil {
		var err error
		if title, titleSpans, err = notionDataSourceName(input.Title, mentions); err != nil {
			if requestError := (&notionDatabaseRowPageRequestError{}); errors.As(err, &requestError) {
				err = notionDatabaseRowPageRequestInvalid("%s", strings.Replace(err.Error(), "data source", "database", 1))
			}
			return err
		}
	}
	var description *string
	var descriptionSpans []graphTextSpan
	if input.Description != nil {
		text, spans, err := notionDatabaseDescription(input.Description, mentions)
		if err != nil {
			return err
		}
		description, descriptionSpans = &text, spans
	}
	presentation, err := notionPagePresentation(input.Icon, input.Cover)
	if err != nil {
		return notionDatabaseRowPageRequestInvalid("%s", err.Error())
	}
	parentID, moves := "", false
	if input.Parent != nil {
		if parentID, err = authorizeNotionDatabaseParent(event.App, identity, *input.Parent, fullPage); err != nil {
			return err
		}
		if fullPage {
			moves = parentID != strings.TrimSpace(page.Page.GetString("parent_id"))
		} else {
			moves = parentID != container.PageID
		}
	}
	if input.Title != nil || input.Icon != nil || description != nil {
		if err := updateNotionDatabaseSource(event, ygo, identity, container, input, title, titleSpans, description, descriptionSpans); err != nil {
			return err
		}
	}
	// One whose page keeps its title apart from its one source's name has
	// that source alone renamed (fullPageDatabaseTitleApart).
	if fullPage && input.Title != nil {
		apart, err := fullPageDatabaseTitleApart(event.App, container.Owner, container.PageID)
		if err != nil {
			return err
		}
		if apart {
			input.Title = nil
		}
	}
	if fullPage && (input.Title != nil || input.Icon != nil || input.Cover != nil || input.IsLocked != nil || moves) {
		metadata := func(current *core.Record) pageMetadataInput {
			value := pageMetadataInput{
				Title: current.GetString("title"), ParentID: current.GetString("parent_id"), InTrash: current.GetBool("in_trash"),
				IsLocked:   input.IsLocked,
				MutationID: "notion-public-database-page:" + newDiagnosticReference(), BaseCursor: current.GetInt("cursor"),
			}
			if input.Title != nil {
				value.Title = title
			}
			if moves {
				value.ParentID = parentID
			}
			return value
		}
		// The connection's change — a lock too — names it in the page's history and updates.
		named := userKeyApp{App: event.App, ctx: event.Request.Context()}
		if err := updateNotionDatabasePage(named, identity, container.PageID, presentation, metadata); err != nil {
			return err
		}
	}
	if !fullPage && input.IsLocked != nil {
		if err := setNotionInlineDatabaseLock(event.Request.Context(), event.App, ygo, identity, container, *input.IsLocked); err != nil {
			return err
		}
	}
	if !fullPage && moves {
		return moveNotionInlineDatabase(event.Request.Context(), event.App, identity, container, parentID)
	}
	return nil
}

func emptyNotionRichText(raw json.RawMessage) bool {
	var items []json.RawMessage
	return string(raw) == "null" || json.Unmarshal(raw, &items) == nil && len(items) == 0
}

// authorizeNotionDatabaseParent resolves a requested parent to a writable
// page, or to the workspace top level for a full-page database.
func authorizeNotionDatabaseParent(
	app core.App, identity notionOAuthBearerIdentity, parent notionPageParentRequest, fullPage bool,
) (string, error) {
	switch notionPageParentKind(parent) {
	case "workspace":
		if !fullPage {
			return "", notionDatabaseRowPageRequestInvalid("An inline database must stay in a page.")
		}
		if len(identity.ResourceIDs) > 0 {
			return "", errDatabaseRowPageNotFound
		}
		return "", nil
	case "page":
		// A database page holds databases as a page does (notion_page_parent.go).
		target, err := authorizeNotionParentPageRecord(app, identity, parent.PageID)
		if errors.Is(err, errNotionParentInTrash) {
			message, err := notionTrashedEditMessage(app, target.Page.Id, true, false, notionTrashedEdit)
			if err != nil {
				return "", err
			}
			return "", notionDatabaseRowPageRequestInvalid("%s", message)
		}
		if isPermissionDenied(err) {
			return "", errDatabaseRowPageNotFound
		}
		if err != nil {
			return "", err
		}
		return target.Page.Id, nil
	}
	return "", notionDatabaseRowPageRequestInvalid("A database parent is a page or the workspace.")
}

// updateNotionDatabaseSource writes the title, icon and description to the
// database's first source; a linked database's title and icon go to its own
// block, and its source's description stays.
func updateNotionDatabaseSource(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, input notionDatabaseUpdateRequest, title string, titleSpans []graphTextSpan,
	description *string, descriptionSpans []graphTextSpan,
) error {
	first := struct {
		DataSourceID      string `db:"data_source_id"`
		SourceContainerID string `db:"source_container_id"`
	}{}
	err := event.App.DB().NewQuery(`
		SELECT [[data_source_id]], [[source_container_id]] FROM {{leafen_database_container_sources}}
		WHERE [[container_id]] = {:id} ORDER BY [[position]], [[data_source_id]] LIMIT 1
	`).Bind(dbx.Params{"id": container.ID}).One(&first)
	if errors.Is(err, sql.ErrNoRows) {
		return errDatabaseRowPageNotFound
	}
	if err != nil {
		return err
	}
	if first.SourceContainerID != "" {
		if input.Title == nil && input.Icon == nil {
			return nil
		}
		return updateNotionLinkedDatabaseName(event, ygo, identity, container, input, title, titleSpans)
	}
	// A database of two or more sources of its own has a name apart from
	// theirs, which renaming it gives it, its sources keeping theirs, as
	// Notion's (seen 2026-11-08 and 2026-11-11): a full-page one's is its
	// page's, its formatting kept on its block (database_page_name_spans.go);
	// an inline one's is kept there whole, plain words as one piece
	// (notionInlineDatabaseName). Unnamed, it goes by its sources' names
	// listed (seen 2026-11-13). With one source, renaming it renames that
	// source, as Notion's does.
	own, err := databaseOwnSourceCount(event.App, container.ID)
	if err != nil {
		return err
	}
	if input.Title != nil && own > 1 {
		kept := titleSpans
		if !container.fullPage() && len(kept) == 0 && title != "" {
			kept = []graphTextSpan{{Text: title}}
		}
		if err := updateNotionDatabasePageNameSpans(event, ygo, identity, container, kept); err != nil {
			return err
		}
		input.Title = nil
	}
	// Its icon is its own too, its sources keeping theirs (database_own_icon.go):
	// a full-page one's page takes it (serveNotionDatabaseUpdate), an inline one's block.
	if input.Icon != nil && own > 1 {
		if !container.fullPage() {
			if err := setNotionInlineDatabaseOwnIcon(event, ygo, identity, container, input.Icon); err != nil {
				return err
			}
		}
		input.Icon = nil
	}
	if input.Title == nil && input.Icon == nil && description == nil {
		return nil
	}
	// Its first source kept in the trash where it stands takes the name too,
	// as Notion's (seen 2026-11-11).
	inPlace, err := databaseSourceTrashedInPlace(event.App, first.DataSourceID)
	if err != nil {
		return err
	}
	authorization, err := authorizeNotionDataSourceParentReach(event.App, identity, first.DataSourceID, permissionWorkspaceWrite, inPlace)
	if err != nil {
		return err
	}
	if authorization.Container.ContainerID != container.ID {
		return errDatabaseRowPageConflict
	}
	request := notionDataSourceUpdateRequest{Title: input.Title, Icon: input.Icon, description: description, descriptionSpans: descriptionSpans,
		titleSpans: titleSpans}
	// Its icon given is the database's own, not its source's alone (full_page_database_icon.go).
	ctx := event.Request.Context()
	if input.Icon != nil {
		ctx = withDatabaseOwnIcon(ctx)
	}
	return applyNotionDataSourceMutationReach(
		ctx, ygo, authorization, "notion-public-database-update:"+newDiagnosticReference(),
		func(txApp core.App, current notionDatabaseRowPageAuthorization, _ *crdt.Doc) ([]graphOperation, error) {
			if current.Container.ContainerID != container.ID {
				return nil, errDatabaseRowPageConflict
			}
			return planNotionDataSourceUpdate(txApp, current, request, title)
		},
		nil, inPlace,
	)
}

// updateNotionDatabasePage changes the page of a full-page database from its
// current state.
func updateNotionDatabasePage(
	app core.App, identity notionOAuthBearerIdentity, pageID string, presentation notionPagePresentationRequest,
	metadata func(current *core.Record) pageMetadataInput,
) error {
	status := pageMetadataApplied
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		if err := validateNotionPagePresentationReferences(
			txApp, identity.PrincipalID, identity.WorkspaceID, &presentation,
		); err != nil {
			return err
		}
		input := metadata(current)
		input.Icon, input.IconType, input.Cover = presentation.Icon, presentation.IconType, presentation.Cover
		_, status, err = applyPageMetadataTransaction(
			txApp, identity.PrincipalID, pageID, input, pageMetadataRequestHash(pageID, input),
		)
		return err
	})
	switch {
	case err != nil:
		return err
	case status == pageMetadataPageMissing:
		return errDatabaseRowPageNotFound
	case status != pageMetadataApplied:
		return graphConflictError(errorCodeGraphRevisionConflict, "The database page could not be changed in its current state.")
	}
	return nil
}

// moveNotionInlineDatabase moves the database block to the end of another
// page, as the editor's block move does, and waits a bounded time for it.
func moveNotionInlineDatabase(
	ctx context.Context, app core.App, identity notionOAuthBearerIdentity, container notionDatabaseContainer, pageID string,
) error {
	admission, err := admitBlockMove(app, identity.PrincipalID, container.PageID, container.BlockID, blockMoveInput{
		RequestID:         "notion-public-database-move:" + newDiagnosticReference(),
		DestinationPageID: pageID, Operation: blockMoveOperationMove,
	})
	switch {
	case errors.Is(err, errBlockMoveNotFound), isPermissionDenied(err):
		return errDatabaseRowPageNotFound
	case errors.Is(err, errBlockMoveConflict):
		return graphConflictError(errorCodeGraphRevisionConflict, "The database is already moving. Retry the request later.")
	case err != nil:
		return err
	}
	phase, err := driveDataSourceMove(ctx, app, admission.Job.ID, notionDataSourceMoveWait)
	switch {
	case err != nil:
		return err
	case phase == blockMovePhaseCompleted:
		return nil
	case phase == blockMovePhaseFailed:
		return graphConflictError(errorCodeGraphRevisionConflict, "The database could not be moved. Its page changed during the move.")
	}
	if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		runner.notify()
	}
	return graphConflictError(errorCodeGraphRevisionConflict, "The database is still moving. Retry the request to see the result.")
}

// changeNotionDatabaseTrash trashes or restores a full-page database's page,
// or an inline database's block through recoverable block trash.
func changeNotionDatabaseTrash(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, page pageAuthorization, inTrash bool,
) error {
	pageTrashed := page.Page.GetBool("in_trash")
	if container.fullPage() && !container.Trashed {
		metadata := func(current *core.Record) pageMetadataInput {
			return pageMetadataInput{
				Title: current.GetString("title"), ParentID: current.GetString("parent_id"), InTrash: inTrash,
				MutationID: "notion-public-database-trash:" + newDiagnosticReference(), BaseCursor: current.GetInt("cursor"),
			}
		}
		return updateNotionDatabasePage(event.App, identity, container.PageID, notionPagePresentationRequest{}, metadata)
	}
	if pageTrashed {
		return notionDatabaseRowPageRequestInvalid("The database is in trash with its page. Restore the page instead.")
	}
	// A deleted database comes back as the trash restores it.
	if container.Deleted {
		deleted, found, err := loadNotionDeletedDatabase(event.App, identity.WorkspaceID, container.ID)
		if err != nil || !found {
			return errors.Join(errDatabaseRowPageNotFound, err)
		}
		_, err = restoreNotionDeletedDatabase(event, ygo, identity, deleted)
		return err
	}
	if inTrash {
		_, err := ygo.applyContentGraphMutationContext(
			event.Request.Context(), identity.PrincipalID, container.PageID,
			graphMutationInput{
				MutationID: "notion-public-database-trash:" + newDiagnosticReference(),
				Operations: []graphOperation{{
					Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: container.BlockID},
				}},
			},
			contentGraphMutationHooks{
				Authorize:             authorizeNotionPageMutation(identity, container.PageID),
				BeforeApplyOperations: captureRecoverableDatabaseTrash(container.BlockID, identity.PrincipalID),
			},
		)
		return err
	}
	var rootBlockID string
	if err := event.App.DB().NewQuery(`
		SELECT [[root_block_id]] FROM {{leafen_database_container_trash}} WHERE [[container_id]] = {:id}
	`).Bind(dbx.Params{"id": container.ID}).Row(&rootBlockID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errDatabaseRowPageConflict
		}
		return err
	}
	if rootBlockID != container.BlockID {
		return notionDatabaseRowPageRequestInvalid(
			"The database was deleted with the block that holds it. Restore that block instead.")
	}
	tombstone, found, err := loadRecoverableBlockTrash(event.App, rootBlockID)
	if err != nil {
		return err
	}
	if !found || tombstone.PageID != container.PageID || tombstone.WorkspaceID != identity.WorkspaceID {
		return errDatabaseRowPageNotFound
	}
	// Last in the parent it left, as Notion's (seen 2026-11-13).
	return restoreRecoverableBlockTrashTree(
		event.Request.Context(), ygo, identity, tombstone,
		"notion-public-database-restore:"+newDiagnosticReference(), restoreAtParentEnd,
	)
}
