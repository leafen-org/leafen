package app

import (
	"context"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// listDatabaseRowTrash is listDatabaseRowTrashFor with no request of the
// reader's: its untitled rows are named as with none (serverTextLanguageIn).
func listDatabaseRowTrash(app core.App, principalID, workspaceID, cursor string) ([]databaseRowTrashItem, string, error) {
	return listDatabaseRowTrashFor(context.Background(), app, principalID, workspaceID, cursor)
}

// databaseRowTrashRefused reports a write refused because its row is in trash.
func databaseRowTrashRefused(err error) bool {
	requestError := &graphRequestError{}
	return errors.Is(err, errDatabaseRowPageInTrash) ||
		errors.As(err, &requestError) && requestError.code == errorCodeDatabaseRowPageInTrash
}
