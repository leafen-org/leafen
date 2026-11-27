package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A data source moves to another database the way the official API describes:
// the destination database owns it and gets a new table view, and the original
// database keeps its existing views of it as linked views of the moved source.
// Both databases change in one graph mutation when they share a page; a move
// to another page runs as a durable job that the request drives for a while.

const notionDataSourceMoveWait = 20 * time.Second

type notionDataSourceParentRequest struct {
	Type       string `json:"type,omitempty"`
	DatabaseID string `json:"database_id"`
}

type notionDataSourceMoveTarget struct {
	ContainerID string `db:"id"`
	OwnerID     string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
}

func notionDataSourceMoveInvalid(message string) error {
	return graphInputError(errorCodeGraphReferenceInvalid, message)
}

// authorizeNotionDataSourceMoveTarget resolves the destination database and
// requires write access to the page that holds it.
func authorizeNotionDataSourceMoveTarget(
	app core.App, identity notionOAuthBearerIdentity, parent notionDataSourceParentRequest,
) (notionDataSourceMoveTarget, error) {
	if parent.Type != "" && parent.Type != "database_id" || strings.TrimSpace(parent.DatabaseID) == "" {
		return notionDataSourceMoveTarget{}, notionDataSourceMoveInvalid("A data source can only move to a database.")
	}
	database, err := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, parent.DatabaseID)
	if errors.Is(err, errInvalidNotionObjectIdentity) || errors.Is(err, sql.ErrNoRows) ||
		err == nil && database.Workspace != identity.WorkspaceID {
		return notionDataSourceMoveTarget{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return notionDataSourceMoveTarget{}, err
	}
	return authorizeDataSourceMoveContainer(app, identity, database.InternalID)
}

// authorizeDataSourceMoveContainer requires the destination database to be
// live and its page writable.
func authorizeDataSourceMoveContainer(
	app core.App, identity notionOAuthBearerIdentity, containerID string,
) (notionDataSourceMoveTarget, error) {
	target := notionDataSourceMoveTarget{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], [[container]].[[owner]], [[container]].[[workspace_id]], [[container]].[[page_id]]
		FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_blocks}} AS [[block]]
			ON [[block]].[[id]] = [[container]].[[block_id]] AND [[block]].[[page_id]] = [[container]].[[page_id]]
			AND [[block]].[[type]] = 'inline_database'
		WHERE [[container]].[[id]] = {:id} AND [[container]].[[workspace_id]] = {:workspaceId}
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] = [[container]].[[id]]
			)
	`).Bind(dbx.Params{"id": containerID, "workspaceId": identity.WorkspaceID}).One(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return notionDataSourceMoveTarget{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return notionDataSourceMoveTarget{}, err
	}
	page, err := authorizePageBody(app, identity.PrincipalID, target.PageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) || err == nil && (page.Page.GetBool("in_trash") ||
		page.Workspace.WorkspaceID != identity.WorkspaceID || page.Workspace.OwnerID != target.OwnerID ||
		!notionOAuthIdentityAllowsPage(app, identity, page)) {
		return notionDataSourceMoveTarget{}, errDatabaseRowPageNotFound
	}
	return target, err
}

// planNotionDataSourceMove rewrites both databases and the moved source's
// views on one page. Moving to the database that already owns the source
// changes nothing.
func planNotionDataSourceMove(
	app core.App, doc *crdt.Doc, current notionDatabaseRowPageAuthorization,
	target notionDataSourceMoveTarget, spec dataSourceMoveSpec,
) ([]graphOperation, error) {
	sourceID, originID, pageID := current.Container.DataSource, current.Container.ContainerID, current.Container.PageID
	if spec.TargetID == originID && !spec.NewDatabase {
		return nil, nil
	}
	if target.PageID != pageID {
		return nil, errDatabaseRowPageConflict
	}
	linkers, err := dataSourceMoveLinkers(app, originID, sourceID, spec.TargetID)
	if err != nil {
		return nil, err
	}
	origin, positions, err := dataSourceMoveOriginOperations(
		app, doc, pageID, current.Container.OwnerID, originID, sourceID, spec,
	)
	if err != nil {
		return nil, err
	}
	var destination []graphOperation
	if spec.NewDatabase {
		destination, err = dataSourceMoveNewDatabaseOperations(
			app, pageID, target.OwnerID, target.WorkspaceID, sourceID, spec, positions,
		)
	} else {
		destination, err = dataSourceMoveTargetOperations(app, doc, pageID, target.OwnerID, sourceID, spec, positions)
	}
	if err != nil {
		return nil, err
	}
	// Databases here that link the source through the original follow it; the
	// ones on other pages follow right after this mutation.
	followers, err := dataSourceMoveLinkerOperations(app, doc, pageID, target.OwnerID, linkers, sourceID, spec.TargetID)
	if err != nil {
		return nil, err
	}
	// The original database's block goes first so the source leaves it before
	// the destination takes it.
	return append(append(origin, destination...), followers...), nil
}

// applyNotionDataSourceMutation applies one mutation on the source's page
// whose operations plan builds from the current state; after, when given,
// runs in the same transaction once they apply and says whether it changed
// the page's graph (StepsChangeGraph).
func applyNotionDataSourceMutation(
	ctx context.Context, ygo *pocketBaseYgoPersistence, authorization notionDatabaseRowPageAuthorization,
	mutationID string,
	plan func(txApp core.App, current notionDatabaseRowPageAuthorization, doc *crdt.Doc) ([]graphOperation, error),
	after func(txApp core.App) (bool, error),
) error {
	return applyNotionDataSourceMutationReach(ctx, ygo, authorization, mutationID, plan, after, false)
}

// applyNotionDataSourceMutationReach with anyTrash also changes a source in
// the trash, as a database renames its first source kept in the trash where
// it stands (notion_database_update.go).
func applyNotionDataSourceMutationReach(
	ctx context.Context, ygo *pocketBaseYgoPersistence, authorization notionDatabaseRowPageAuthorization,
	mutationID string,
	plan func(txApp core.App, current notionDatabaseRowPageAuthorization, doc *crdt.Doc) ([]graphOperation, error),
	after func(txApp core.App) (bool, error),
	anyTrash bool,
) error {
	current := func(txApp core.App) (notionDatabaseRowPageAuthorization, error) {
		current, err := authorizeNotionDataSourceParentReach(
			txApp, authorization.Identity, authorization.Container.DataSource, permissionWorkspaceWrite, anyTrash,
		)
		if err == nil && current.Container.PageID != authorization.Container.PageID {
			err = errDatabaseRowPageNotFound
		}
		return current, err
	}
	// The change says what its operations changed unless its step after
	// them changed the page's graph too (a property sent to Deleted properties).
	changed := false
	hooks := contentGraphMutationHooks{
		LinkVerificationProperties: true, SettingsAsAsked: true, CheckOperations: databaseOptionChangesChecked(true),
		StepsChangeGraph: func() bool { return changed },
	}
	if after != nil {
		hooks.AfterApply = func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (err error) {
			changed, err = after(txApp)
			return err
		}
	}
	hooks.Authorize = func(txApp core.App) (pageAuthorization, error) {
		current, err := current(txApp)
		if err != nil {
			return pageAuthorization{}, refusedUnlessStorage(errDatabaseRowPageNotFound, err)
		}
		return current.Page, nil
	}
	hooks.Prepare = func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
		current, err := current(txApp)
		if err != nil {
			return contentGraphMutationPreparation{}, err
		}
		operations, err := plan(txApp, current, doc)
		return contentGraphMutationPreparation{Operations: operations}, err
	}
	_, err := ygo.applyContentGraphMutationContext(
		ctx, authorization.Identity.PrincipalID, authorization.Container.PageID,
		graphMutationInput{MutationID: mutationID}, hooks,
	)
	return err
}

// dataSourceMoveRequest names an existing database or a page that gets a new
// database, and whether the source's views go along.
type dataSourceMoveRequest struct {
	DatabaseID string
	PageID     string
	MoveViews  bool
	// NamedAfterSource names the table view the move makes after the source,
	// as a move made in the app does (dataSourceMoveSpec.ViewName).
	NamedAfterSource bool
}

// resolveDataSourceMoveTarget authorizes where a move goes: an existing
// database, or a new database at the end of a writable page.
func resolveDataSourceMoveTarget(
	app core.App, identity notionOAuthBearerIdentity, request dataSourceMoveRequest,
) (notionDataSourceMoveTarget, dataSourceMoveSpec, error) {
	if (request.DatabaseID == "") == (request.PageID == "") {
		return notionDataSourceMoveTarget{}, dataSourceMoveSpec{},
			notionDataSourceMoveInvalid("Choose a database or a page to move the data source to.")
	}
	viewID := newDatabaseEntityID()
	// A database chosen in the page picker comes by its ID where a page's
	// would: an inline one's is its database's (inline databases are found
	// among pages, as Notion's "다음으로 이동:" finds them, seen 2026-11-11).
	if request.PageID != "" && dataSourceMoveContainerNamed(app, request.PageID) {
		request.DatabaseID, request.PageID = request.PageID, ""
	}
	if request.DatabaseID != "" {
		target, err := authorizeDataSourceMoveContainer(app, identity, request.DatabaseID)
		return target, dataSourceMoveSpec{TargetID: target.ContainerID, MoveViews: request.MoveViews, ViewID: viewID}, err
	}
	page, err := authorizePageBody(app, identity.PrincipalID, request.PageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) || err == nil && (page.Page.GetBool("in_trash") ||
		page.Workspace.WorkspaceID != identity.WorkspaceID || !notionOAuthIdentityAllowsPage(app, identity, page)) {
		return notionDataSourceMoveTarget{}, dataSourceMoveSpec{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return notionDataSourceMoveTarget{}, dataSourceMoveSpec{}, err
	}
	// A full-page database's page chosen takes the source into its database,
	// as Notion's "다음으로 이동:" does with a database chosen (seen 2026-11-11).
	if isFullPageDatabase(page.Page) {
		held, found, err := loadDatabasePageBlock(app, page.Workspace.OwnerID, request.PageID)
		if err != nil {
			return notionDataSourceMoveTarget{}, dataSourceMoveSpec{}, err
		}
		if found {
			target, err := authorizeDataSourceMoveContainer(app, identity, held.ID)
			return target, dataSourceMoveSpec{TargetID: target.ContainerID, MoveViews: request.MoveViews, ViewID: viewID}, err
		}
	}
	blockID := uuid.NewString()
	return notionDataSourceMoveTarget{
		ContainerID: blockID, OwnerID: page.Workspace.OwnerID, WorkspaceID: identity.WorkspaceID, PageID: request.PageID,
	}, dataSourceMoveSpec{
		TargetID: blockID, NewDatabase: true, MoveViews: request.MoveViews, ViewID: viewID,
	}, nil
}

// dataSourceMoveViewName is the name a move made in the app gives the table
// view it makes: the source's title within a view name's length, or none for
// a source without one.
func dataSourceMoveViewName(title string) string {
	if strings.TrimSpace(title) == "" {
		return ""
	}
	if runes := []rune(title); len(runes) > maxGraphIdentifierLength {
		return string(runes[:maxGraphIdentifierLength])
	}
	return title
}

// dataSourceMoveContainerNamed is whether an ID names a database's own
// container, not a page.
func dataSourceMoveContainerNamed(app core.App, id string) bool {
	found := 0
	_ = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_containers}} WHERE [[id]]={:id} AND [[kind]]='source'`).
		Bind(dbx.Params{"id": id}).Row(&found)
	return found > 0
}

// startDataSourceCrossPageMove queues moving the source to another page after
// checking both ends again, and returns where it goes.
func startDataSourceCrossPageMove(
	app core.App, authorization notionDatabaseRowPageAuthorization, request dataSourceMoveRequest,
) (string, dataSourceMoveSpec, error) {
	jobID, moved := "", dataSourceMoveSpec{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := authorizeNotionDataSourceParentCurrent(
			txApp, authorization.Identity, authorization.Container.DataSource, permissionWorkspaceWrite,
		)
		if err != nil {
			return err
		}
		target, spec, err := resolveDataSourceMoveTarget(txApp, current.Identity, request)
		if err != nil {
			return err
		}
		if target.PageID == current.Container.PageID {
			return errDatabaseRowPageConflict
		}
		if request.NamedAfterSource {
			spec.ViewName = dataSourceMoveViewName(current.Container.Title)
		}
		moved = spec
		jobID, err = admitDataSourceMove(txApp, current.Identity.PrincipalID, current, target, spec)
		return err
	})
	return jobID, moved, err
}

// driveDataSourceMove advances the move until it finishes or the wait ends and
// returns its phase.
func driveDataSourceMove(ctx context.Context, app core.App, jobID string, wait time.Duration) (string, error) {
	deadline := time.Now().Add(wait)
	for {
		if err := advanceBlockMove(ctx, app, jobID); err != nil {
			return blockMovePhaseMoving, nil
		}
		job, err := loadBlockMoveJobByID(app, jobID, "")
		if err != nil {
			return "", err
		}
		if job.Phase == blockMovePhaseCompleted || job.Phase == blockMovePhaseFailed || !time.Now().Before(deadline) {
			return job.Phase, nil
		}
	}
}

// serveNotionDataSourceCrossPageMove queues the move and drives it until it
// finishes or the wait ends; a retry after the wait reports the result.
func serveNotionDataSourceCrossPageMove(
	event *core.RequestEvent, authorization notionDatabaseRowPageAuthorization, target notionDataSourceMoveTarget,
) error {
	jobID, _, err := startDataSourceCrossPageMove(event.App, authorization, dataSourceMoveRequest{DatabaseID: target.ContainerID})
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	phase, err := driveDataSourceMove(event.Request.Context(), event.App, jobID, notionDataSourceMoveWait)
	switch {
	case err != nil:
		return notionDataSourceMutationError(event, err)
	case phase == blockMovePhaseCompleted:
		return serveNotionDataSourceUpdated(event, authorization)
	case phase == blockMovePhaseFailed:
		return notionCompatibilityError(event, http.StatusConflict, "conflict_error",
			"The data source could not be moved. Its databases changed during the move.")
	}
	return notionCompatibilityError(event, http.StatusConflict, "conflict_error",
		"The data source is still moving. Retry the request to see the result.")
}
