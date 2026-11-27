package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionImportReadApp struct {
	core.App
	database dbx.Builder
}

func (app notionImportReadApp) DB() dbx.Builder {
	return app.database
}

func (app notionImportReadApp) ConcurrentDB() dbx.Builder {
	return app.database
}

func (app notionImportReadApp) NonconcurrentDB() dbx.Builder {
	return app.database
}

func withAuthorizedNotionImportRead(
	ctx context.Context,
	app core.App,
	userID, workspaceID string,
	read func(core.App) error,
) error {
	if _, err := authorizeWorkspace(app, userID, workspaceID, permissionWorkspaceWrite); err != nil {
		return err
	}
	database, ok := app.ConcurrentDB().(*dbx.DB)
	if !ok {
		return errors.New("notion import read requires the concurrent database pool")
	}
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err := read(notionImportReadApp{App: app, database: transaction}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	return authorizeNotionImportPublication(app, userID, workspaceID)
}

func authorizeNotionImportPublication(app core.App, userID, workspaceID string) error {
	return authorizeWorkspaceSet(
		app, userID, []string{workspaceID}, permissionWorkspaceWrite,
	)
}

func authorizeNotionImportTurn(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
) (workspaceAuthorization, error) {
	current, err := loadStoredJob(app, job.ID, "")
	if err != nil {
		return workspaceAuthorization{}, err
	}
	if current.Kind != notionImportJobKind || current.Owner != job.Owner ||
		current.WorkspaceID != job.WorkspaceID || current.Status != job.Status ||
		current.Updated != job.Updated {
		return workspaceAuthorization{}, errors.New(errorCodeNotionImportNotFound)
	}
	authorization, err := authorizeWorkspace(
		app, job.Owner, job.WorkspaceID, permissionWorkspaceWrite,
	)
	if err != nil {
		return workspaceAuthorization{}, err
	}
	if authorization.OwnerID != payload.AuthorityOwnerID {
		return workspaceAuthorization{}, permissionDenied()
	}
	if _, err := loadNotionConnection(app, payload.ConnectionID, job.Owner, job.WorkspaceID); errors.Is(err, sql.ErrNoRows) {
		return workspaceAuthorization{}, errors.New(errorCodeNotionImportConnectionNotFound)
	} else if err != nil {
		return workspaceAuthorization{}, err
	}
	return authorization, nil
}
