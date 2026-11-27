package app

import (
	"context"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// update-data-source's is_inline turns the database holding the data source
// as Notion's does (seen 2026-11-13: a full-page database under a page given
// is_inline true is inline in that page, its address and data source kept),
// the way the Public API's is_inline and the block menu's "인라인으로 전환"
// and "페이지로 전환" turn one (notion_database_turn.go): true moves a
// full-page database into the page above it, where its page was; false moves
// an inline one into a new page right after it, named after it. Asked for
// what it already is, nothing changes.

// turnMCPDataSourceDatabase turns the database that holds a data source
// itself inline or into a full page.
func turnMCPDataSourceDatabase(
	ctx context.Context, app core.App, persistence *pocketBaseYgoPersistence, authority mcpAuthority,
	location mcpDataSourceLocation, dataSourceID string, inline bool,
) error {
	identity := mcpNotionIdentity(authority, location.WorkspaceID)
	container, page, turns, err := mcpDataSourceTurnTarget(app, identity, dataSourceID, inline)
	if err != nil || !turns {
		return err
	}
	return mcpDataSourceTurnError(turnNotionDatabase(ctx, app, persistence, identity, container, page, inline))
}

// mcpDataSourceTurnRefusal is what is_inline would refuse for what the
// database is and where it stands, asked before the call saves anything
// (mcpDataSourceUpdateFits).
func mcpDataSourceTurnRefusal(app core.App, authority mcpAuthority, location mcpDataSourceLocation, dataSourceID string, inline bool) error {
	container, page, turns, err := mcpDataSourceTurnTarget(app, mcpNotionIdentity(authority, location.WorkspaceID), dataSourceID, inline)
	if err != nil || !turns {
		return err
	}
	return mcpDataSourceTurnError(notionDatabaseTurnRefusal(container, page, inline))
}

// mcpDataSourceTurnTarget is the database holding a data source, and whether
// is_inline turns it: asked for what it is already, it doesn't.
func mcpDataSourceTurnTarget(
	app core.App, identity notionOAuthBearerIdentity, dataSourceID string, inline bool,
) (notionDatabaseContainer, pageAuthorization, bool, error) {
	containerID := mcpOwningDatabaseContainer(app, dataSourceID)
	if containerID == "" {
		return notionDatabaseContainer{}, pageAuthorization{}, false, errMCPInlineWithoutDatabase
	}
	container, page, err := loadNotionDatabaseContainer(app, identity, containerID, permissionWorkspaceWrite)
	if err != nil {
		return container, page, false, mcpDataSourceTurnError(err)
	}
	return container, page, container.fullPage() == inline, nil
}

// mcpDataSourceTurnError says what stopped a turn in the Public API's words.
func mcpDataSourceTurnError(err error) error {
	requestError := &notionDatabaseRowPageRequestError{}
	var graphError *graphRequestError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &requestError):
		return mcpInvalid("%s", requestError.message)
	case errors.Is(err, errDatabaseRowPageNotFound), isPermissionDenied(err):
		return errMCPCantChange
	case errors.As(err, &graphError):
		return mcpGraphReason(errMCPToolInvalid, err)
	}
	return mcpReasoned(err)
}
