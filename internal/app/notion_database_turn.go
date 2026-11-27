package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// is_inline turns a database as the block menu's "페이지로 전환" and
// "인라인으로 전환" do, and as Notion's API does (seen 2026-10-30, the
// database keeping its ID): an inline database moves into a new page right
// after it, named after it, which becomes a full-page database; a full-page
// database's page becomes a page again and its database moves into the page
// above, where the page was, which goes to the trash. The move is the
// editor's block move, waited for as a database move is. A linked
// database's own name, its formatting and icon go with it as the page-kind
// change gives them (database_turn_linked_name.go), as the editor's do.
// MCP's update-data-source is_inline turns a database the same way.

func turnNotionDatabase(
	ctx context.Context, app core.App, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, page pageAuthorization, inline bool,
) error {
	if err := notionDatabaseTurnRefusal(container, page, inline); err != nil {
		return err
	}
	if inline {
		return turnNotionDatabaseInline(ctx, app, ygo, identity, container, page)
	}
	return turnNotionDatabaseIntoPage(ctx, app, ygo, identity, container)
}

// notionDatabaseTurnRefusal is what a turn refuses for what the database is
// and where it stands, before anything moves; a caller that saves other
// changes first asks it before them (mcpDataSourceUpdateFits).
func notionDatabaseTurnRefusal(container notionDatabaseContainer, page pageAuthorization, inline bool) error {
	if container.Wiki {
		return notionDatabaseRowPageRequestInvalid("A wiki's database stays a full page.")
	}
	if inline && strings.TrimSpace(page.Page.GetString("parent_id")) == "" {
		return notionDatabaseRowPageRequestInvalid("An inline database must stay in a page.")
	}
	return nil
}

func turnNotionDatabaseIntoPage(
	ctx context.Context, app core.App, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer,
) error {
	title, err := notionDatabaseTurnTitle(app, container)
	if err != nil {
		return err
	}
	pageID := ""
	err = app.RunInTransaction(func(txApp core.App) error {
		created, status, err := admitPageCreationTransactionWithLock(txApp, identity.PrincipalID, pageCreationAdmissionInput{
			RequestID: "notion-public-database-turn:" + newDiagnosticReference(), WorkspaceID: identity.WorkspaceID,
			ParentID: container.PageID, Title: title, ContentInBody: true,
			BodyPlacement: &pageBodyPlacement{Type: "after", AfterKind: pageBodyItemBlock, AfterID: container.BlockID},
		}, pageLockIgnored)
		if err != nil {
			return err
		}
		if status != pageCreationApplied {
			return graphConflictError(errorCodeGraphPageCursorConflict, "The page the database turns into could not be made.")
		}
		pageID = created.Page.ID
		return nil
	})
	if err != nil {
		return err
	}
	if err := moveNotionInlineDatabase(ctx, app, identity, container, pageID); err != nil {
		// A page the database couldn't move into goes, as the editor's; one it
		// is still moving into stays.
		var request *graphRequestError
		if !errors.As(err, &request) || !strings.Contains(request.message, "still moving") {
			_ = trashNotionTurnPage(app, identity, pageID)
		}
		return err
	}
	return setNotionTurnPageKind(ctx, ygo, identity, pageID, pageKindDatabase)
}

func turnNotionDatabaseInline(
	ctx context.Context, app core.App, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, page pageAuthorization,
) error {
	parentID := strings.TrimSpace(page.Page.GetString("parent_id"))
	target, err := authorizePageBody(app, identity.PrincipalID, parentID, permissionWorkspaceWrite)
	if isPermissionDenied(err) || err == nil && (target.Workspace.WorkspaceID != identity.WorkspaceID ||
		target.Page.GetBool("in_trash") || !notionOAuthIdentityAllowsPage(app, identity, target)) {
		return errDatabaseRowPageNotFound
	}
	if err != nil {
		return err
	}
	turn := notionDatabaseTurnOf(identity)
	if err := turn.setKind(ctx, ygo, container.PageID, pageKindPage); err != nil {
		return err
	}
	if err := moveNotionInlineDatabase(ctx, app, identity, container, parentID); err != nil {
		return err
	}
	// The database goes where the page's block is, and the emptied page goes
	// to the trash (databaseTurn.placeInline).
	return turn.placeInline(ctx, app, ygo, parentID, container.PageID, container.BlockID)
}

// notionDatabaseTurnTitle names the page an inline database becomes: its
// own name for a linked database, else its first source's title.
func notionDatabaseTurnTitle(app core.App, container notionDatabaseContainer) (string, error) {
	var title, linked string
	err := app.DB().NewQuery(`
		SELECT [[source]].[[title]], COALESCE(json_extract([[block]].[[content]], '$.linkedTitle'), '')
		FROM {{leafen_database_container_sources}} AS [[membership]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[membership]].[[data_source_id]]
		JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]] = [[membership]].[[container_id]]
		WHERE [[membership]].[[container_id]] = {:id}
		ORDER BY [[membership]].[[position]], [[membership]].[[data_source_id]] LIMIT 1
	`).Bind(dbx.Params{"id": container.ID}).Row(&title, &linked)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errDatabaseRowPageNotFound
	}
	if err != nil {
		return "", err
	}
	if linked != "" {
		return linked, nil
	}
	return title, nil
}

func setNotionTurnPageKind(
	ctx context.Context, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity, pageID, kind string,
) error {
	return notionDatabaseTurnOf(identity).setKind(ctx, ygo, pageID, kind)
}

// trashNotionTurnPage puts the page a database left in the trash, as a
// page's trash does.
func trashNotionTurnPage(app core.App, identity notionOAuthBearerIdentity, pageID string) error {
	return notionDatabaseTurnOf(identity).trash(app, pageID)
}
