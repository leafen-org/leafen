package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	notionImportSecretDirectory = "secure"
	notionImportSecretFilename  = "notion-import.key"
)

type storedNotionImportConnection struct {
	ID          string `db:"id"`
	Owner       string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	RequestID   string `db:"request_id"`
	RequestHash string `db:"request_hash"`
	Label       string `db:"label"`
	TokenCipher string `db:"token_cipher"`
	Active      int    `db:"active"`
	Created     int64  `db:"created"`
}

func (r *durableJobRunner) createNotionImportConnection(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't create the connection.", err)
	}
	var input notionImportConnectionInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the connection details.")
	}
	connection, created, err := saveNotionImportConnection(event.Request.Context(), event.App, user.Id, workspaceID, input)
	switch {
	case errors.Is(err, errNotionConnectionInputInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the connection details.")
	case errors.Is(err, errNotionConnectionRequestReused):
		return apiError(event, http.StatusConflict, errorCodeJobIdentityReused, "The request ID was used for a different connection.")
	case errors.Is(err, errNotionConnectionReadFailed):
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't read the connection.", err)
	case errors.Is(err, errNotionConnectionProtectFailed):
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't protect the connection.", err)
	case isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	case errors.Is(err, errNotionConnectionSaveFailed):
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't save the connection.", err)
	case err != nil:
		return notionProviderAPIError(event, err, "Notion didn't accept this token.", false)
	case created:
		return event.JSON(http.StatusCreated, notionConnectionResponse(connection))
	}
	return event.JSON(http.StatusOK, notionConnectionResponse(connection))
}

var (
	errNotionConnectionInputInvalid  = errors.New("check the connection details")
	errNotionConnectionRequestReused = errors.New("the request ID was used for a different connection")
	errNotionConnectionReadFailed    = errors.New("couldn't read the connection")
	errNotionConnectionProtectFailed = errors.New("couldn't protect the connection")
	errNotionConnectionSaveFailed    = errors.New("couldn't save the connection")
)

// saveNotionImportConnection keeps a Notion token as a workspace's import
// connection, sealed with the server's key, once Notion takes it: what the
// import screen's hidden field sends, and what an agent hands over from its
// conversation (mcp_notion_migration_connect.go, user decision 2026-11-13).
// The same request id answers the connection it made (created false), and
// any other error is Notion's (notionProviderAPIError).
func saveNotionImportConnection(ctx context.Context, app core.App, owner, workspaceID string, input notionImportConnectionInput) (storedNotionImportConnection, bool, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Token = strings.TrimSpace(input.Token)
	input.Label = strings.TrimSpace(input.Label)
	if input.RequestID == "" || len([]rune(input.RequestID)) > 255 || input.Token == "" || len(input.Token) > 4096 || input.Label == "" || len([]rune(input.Label)) > 255 {
		return storedNotionImportConnection{}, false, errNotionConnectionInputInvalid
	}
	requestHash := notionConnectionRequestHash(workspaceID, input)
	if existing, err := loadNotionConnectionByRequest(app, owner, input.RequestID); err == nil {
		if existing.RequestHash != requestHash {
			return storedNotionImportConnection{}, false, errNotionConnectionRequestReused
		}
		return existing, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return storedNotionImportConnection{}, false, failedBecause(errNotionConnectionReadFailed, err)
	}

	validateContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// The connection is named as Notion names it; the client's label, in the
	// person's words, stands in only when Notion gives no name (2026-10-26:
	// every connection read "Leafen Notion import" in English).
	notionName, err := newNotionProviderClient(input.Token).connectionName(validateContext)
	if err != nil {
		return storedNotionImportConnection{}, false, err
	}
	label := input.Label
	if notionName != "" {
		label = notionName
	}
	key, err := notionImportEncryptionKey(app)
	if err != nil {
		return storedNotionImportConnection{}, false, failedBecause(errNotionConnectionProtectFailed, err)
	}
	cipher, err := security.Encrypt([]byte(input.Token), key)
	if err != nil {
		return storedNotionImportConnection{}, false, errNotionConnectionProtectFailed
	}

	connection := storedNotionImportConnection{
		ID: security.RandomString(15), Owner: owner, WorkspaceID: workspaceID,
		RequestID: input.RequestID, RequestHash: requestHash, Label: label,
		TokenCipher: cipher, Active: 1,
	}
	now := time.Now().UTC().UnixMilli()
	var inserted int64
	err = app.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizeWorkspace(txApp, owner, workspaceID, permissionWorkspaceWrite); err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_notion_import_connections}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]],
				 [[label]], [[token_cipher]], [[active]], [[created]], [[updated]])
			VALUES ({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash},
				{:label}, {:tokenCipher}, 1, {:now}, {:now})
			ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": connection.ID, "owner": connection.Owner, "workspaceId": connection.WorkspaceID,
			"requestId": connection.RequestID, "requestHash": connection.RequestHash,
			"label": connection.Label, "tokenCipher": connection.TokenCipher, "now": now,
		}).Execute()
		if err != nil {
			return err
		}
		inserted, err = result.RowsAffected()
		return err
	})
	if isPermissionDenied(err) {
		return storedNotionImportConnection{}, false, err
	}
	if err != nil {
		return storedNotionImportConnection{}, false, failedBecause(errNotionConnectionSaveFailed, err)
	}
	if inserted == 0 {
		existing, loadErr := loadNotionConnectionByRequest(app, owner, input.RequestID)
		if loadErr != nil || existing.RequestHash != requestHash {
			return storedNotionImportConnection{}, false, refusedUnlessStorage(errNotionConnectionRequestReused, loadErr)
		}
		return existing, false, nil
	}
	return connection, true, nil
}

func (r *durableJobRunner) readNotionImportScope(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportScopeFailed, "Couldn't read the import scope.", err)
	}
	connection, err := loadNotionConnection(event.App, strings.TrimSpace(event.Request.PathValue("connectionId")), user.Id, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeNotionImportConnectionNotFound, "Connection not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportScopeFailed, "Couldn't read the connection.", err)
	}
	token, err := decryptNotionConnection(event.App, connection)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't open the connection.", err)
	}
	requestContext, cancel := context.WithTimeout(event.Request.Context(), 30*time.Second)
	defer cancel()
	started := time.Now()
	scope, err := r.notionProviderClient(connection.ID, token).searchRoots(
		requestContext,
		strings.TrimSpace(event.Request.URL.Query().Get("cursor")),
		event.Request.URL.Query().Get("query"),
	)
	if err != nil {
		return notionProviderAPIError(event, err, "Notion no longer accepts this connection's token.",
			r.notionProviderAdmission.askedToWaitSince(connection.ID, started))
	}
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportScopeFailed, "Couldn't read the import scope.", err)
	}
	if _, err := loadNotionConnection(event.App, connection.ID, user.Id, workspaceID); errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeNotionImportConnectionNotFound, "Connection not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportScopeFailed, "Couldn't read the connection.", err)
	}
	return event.JSON(http.StatusOK, scope)
}

func loadNotionConnection(app core.App, id string, owner string, workspaceID string) (storedNotionImportConnection, error) {
	var connection storedNotionImportConnection
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]],
			[[label]], [[token_cipher]], [[active]]
		FROM {{leafen_notion_import_connections}}
		WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId} AND [[active]] = 1
	`).Bind(dbx.Params{"id": id, "owner": owner, "workspaceId": workspaceID}).One(&connection)
	return connection, err
}

func loadNotionConnectionByRequest(app core.App, owner string, requestID string) (storedNotionImportConnection, error) {
	var connection storedNotionImportConnection
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]],
			[[label]], [[token_cipher]], [[active]]
		FROM {{leafen_notion_import_connections}}
		WHERE [[owner]] = {:owner} AND [[request_id]] = {:requestId}
	`).Bind(dbx.Params{"owner": owner, "requestId": requestID}).One(&connection)
	return connection, err
}

func notionConnectionResponse(connection storedNotionImportConnection) notionImportConnection {
	return notionImportConnection{
		ID: connection.ID, WorkspaceID: connection.WorkspaceID,
		Label: connection.Label, Active: connection.Active == 1,
	}
}

func notionConnectionRequestHash(workspaceID string, input notionImportConnectionInput) string {
	digest := sha256.Sum256([]byte(workspaceID + "\x00" + input.RequestID + "\x00" + input.Token + "\x00" + input.Label))
	return hex.EncodeToString(digest[:])
}

func decryptNotionConnection(app core.App, connection storedNotionImportConnection) (string, error) {
	key, err := notionImportEncryptionKey(app)
	if err != nil {
		return "", err
	}
	plain, err := security.Decrypt(connection.TokenCipher, key)
	return string(plain), err
}

func notionImportEncryptionKey(app core.App) (string, error) {
	directory := filepath.Join(app.DataDir(), notionImportSecretDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, notionImportSecretFilename)
	if existing, err := os.ReadFile(path); err == nil {
		key := strings.TrimSpace(string(existing))
		if len(key) != 32 {
			return "", errors.New(errorCodeNotionImportConnectionFailed)
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(random)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		key := strings.TrimSpace(string(existing))
		if readErr == nil && len(key) != 32 {
			readErr = errors.New(errorCodeNotionImportConnectionFailed)
		}
		return key, readErr
	}
	if err != nil {
		return "", err
	}
	if _, err := file.WriteString(key + "\n"); err != nil {
		file.Close()
		return "", err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return "", err
	}
	return key, nil
}
