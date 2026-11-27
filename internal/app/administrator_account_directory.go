package app

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedAdministratorDirectoryAccount struct {
	ID            string `db:"id"`
	Email         string `db:"email"`
	Name          string `db:"name"`
	Avatar        string `db:"avatar"`
	Status        string `db:"status"`
	Verified      bool   `db:"verified"`
	Administrator bool   `db:"administrator"`
	// An organization suspended the account, which can't sign in meanwhile.
	OrganizationSuspended bool   `db:"organization_suspended"`
	WorkspaceCount        int64  `db:"workspace_count"`
	OrganizationCount     int64  `db:"organization_count"`
	Revision              int    `db:"revision"`
	CleanupJobID          string `db:"cleanup_job_id"`
	CreatedAt             string `db:"created_at"`
	UpdatedAt             string `db:"updated_at"`
}

type administratorAccountDirectoryCursor struct {
	Email string
	ID    string
}

func registerAdministratorAccountDirectoryRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/admin/accounts", listAdministratorAccounts)
}

func listAdministratorAccounts(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	search := strings.ToLower(strings.TrimSpace(event.Request.URL.Query().Get("query")))
	if utf8.RuneCountInString(search) > administratorAccountSearchRunes {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the search query.")
	}
	cursor, err := decodeAdministratorAccountDirectoryCursor(strings.TrimSpace(event.Request.URL.Query().Get("cursor")))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the list position.")
	}
	expectedRevision := int64(0)
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("authorityRevision")); raw != "" {
		expectedRevision, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || expectedRevision < 1 {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the list version.")
		}
	}
	if cursor.ID != "" && expectedRevision == 0 {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "The next list needs a version.")
	}
	authorityRevision, err := currentAdministratorAccountDirectoryRevision(event.App)
	if err != nil {
		return administratorAccountMutationError(event, err, "directory-authority")
	}
	if expectedRevision > 0 && expectedRevision != authorityRevision {
		return administratorAccountMutationError(event, errAdministratorAccountRevision, "directory-revision")
	}

	rows, err := queryAdministratorAccounts(event.App, search, cursor, administratorAccountDirectoryLimit+1, "")
	if err != nil {
		return administratorAccountMutationError(event, err, "directory-read")
	}
	verifiedRevision, err := currentAdministratorAccountDirectoryRevision(event.App)
	if err != nil {
		return administratorAccountMutationError(event, err, "directory-authority-recheck")
	}
	if verifiedRevision != authorityRevision {
		return administratorAccountMutationError(event, errAdministratorAccountRevision, "directory-mixed-snapshot")
	}
	var nextCursor *string
	if len(rows) > administratorAccountDirectoryLimit {
		last := rows[administratorAccountDirectoryLimit-1]
		encoded := encodeAdministratorAccountDirectoryCursor(strings.ToLower(last.Email), last.ID)
		nextCursor = &encoded
		rows = rows[:administratorAccountDirectoryLimit]
	}
	items := make([]administratorManagedAccount, 0, len(rows))
	for _, row := range rows {
		items = append(items, projectAdministratorDirectoryAccount(event.App, row))
	}
	if err := addAdministratorAccountPlaces(event.App, items); err != nil {
		return administratorAccountMutationError(event, err, "directory-places")
	}
	return event.JSON(http.StatusOK, administratorAccountDirectory{
		Items: items, AuthorityRevision: authorityRevision, NextCursor: nextCursor,
	})
}

func loadManagedAdministratorAccount(app core.App, userID string) (administratorManagedAccount, error) {
	rows, err := queryAdministratorAccounts(app, "", administratorAccountDirectoryCursor{}, 1, userID)
	if err != nil {
		return administratorManagedAccount{}, err
	}
	if len(rows) != 1 {
		return administratorManagedAccount{}, sql.ErrNoRows
	}
	accounts := []administratorManagedAccount{projectAdministratorDirectoryAccount(app, rows[0])}
	if err := addAdministratorAccountPlaces(app, accounts); err != nil {
		return administratorManagedAccount{}, err
	}
	return accounts[0], nil
}

func queryAdministratorAccounts(
	app core.App,
	search string,
	cursor administratorAccountDirectoryCursor,
	limit int,
	targetID string,
) ([]storedAdministratorDirectoryAccount, error) {
	// App and integration accounts aren't people: they never sign in.
	filter := `[[state]].[[status]] <> 'deleted' AND lower([[user]].[[email]]) NOT LIKE '%@` + integrationEmailHost + `'`
	params := dbx.Params{"limit": limit}
	if targetID != "" {
		filter += ` AND [[user]].[[id]]={:targetId}`
		params["targetId"] = targetID
	}
	if search != "" {
		filter += ` AND (lower([[user]].[[email]]) LIKE {:search} ESCAPE '\' OR lower([[user]].[[name]]) LIKE {:search} ESCAPE '\')`
		params["search"] = "%" + escapeAdministratorAccountSearch(search) + "%"
	}
	if cursor.ID != "" {
		filter += ` AND (lower([[user]].[[email]])>{:afterEmail} OR (lower([[user]].[[email]])={:afterEmail} AND [[user]].[[id]]>{:afterId}))`
		params["afterEmail"] = cursor.Email
		params["afterId"] = cursor.ID
	}
	rows := []storedAdministratorDirectoryAccount{}
	err := app.DB().NewQuery(`
		SELECT [[user]].[[id]],[[user]].[[email]],[[user]].[[name]],[[user]].[[profile_avatar]] AS [[avatar]],[[state]].[[status]],
			(` + provenAccountEmailSQL("[[user]].") + `) AS [[verified]],EXISTS(SELECT 1 FROM {{leafen_administrators}} [[admin]] WHERE [[admin]].[[user_id]]=[[user]].[[id]]) AS [[administrator]],
			` + organizationSuspendedSQL("[[user]].[[id]]") + ` AS [[organization_suspended]],
			(SELECT COUNT(*) FROM {{leafen_workspaces}} [[workspace]] WHERE [[workspace]].[[workspace_owner]]=[[user]].[[id]]) AS [[workspace_count]],
			(SELECT COUNT(*) FROM {{leafen_organizations}} [[organization]] WHERE [[organization]].[[owner]]=[[user]].[[id]]) AS [[organization_count]],
			[[state]].[[revision]],[[state]].[[cleanup_job_id]],
			[[user]].[[created]] AS [[created_at]],[[user]].[[updated]] AS [[updated_at]]
		FROM {{users}} [[user]]
		JOIN {{leafen_administrator_account_states}} [[state]] ON [[state]].[[user_id]]=[[user]].[[id]]
		WHERE ` + filter + `
		ORDER BY lower([[user]].[[email]]),[[user]].[[id]] LIMIT {:limit}
	`).Bind(params).All(&rows)
	return rows, err
}

func currentAdministratorAccountDirectoryRevision(app core.App) (int64, error) {
	var revision int64
	err := app.DB().NewQuery(`SELECT [[revision]] FROM {{leafen_administrator_account_directory_authority}}
		WHERE [[id]]='default'`).Row(&revision)
	return revision, err
}

func projectAdministratorDirectoryAccount(app core.App, row storedAdministratorDirectoryAccount) administratorManagedAccount {
	account := administratorManagedAccount{
		ID: row.ID, Email: row.Email, Name: row.Name, Avatar: row.Avatar, Status: row.Status,
		Verified: row.Verified, Administrator: row.Administrator, OrganizationSuspended: row.OrganizationSuspended,
		// The old names count what it owns, as the new ones say.
		WorkspaceCount: row.WorkspaceCount, OrganizationCount: row.OrganizationCount,
		OwnedWorkspaceCount: row.WorkspaceCount, OwnedOrganizationCount: row.OrganizationCount,
		JoinedWorkspaces: []administratorAccountJoinedWorkspace{}, GuestWorkspaces: []administratorAccountGuestWorkspace{},
		Revision: row.Revision, CleanupJobID: row.CleanupJobID,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	return account
}

func encodeAdministratorAccountDirectoryCursor(email, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(email + "\x00" + id))
}

func decodeAdministratorAccountDirectoryCursor(value string) (administratorAccountDirectoryCursor, error) {
	if value == "" {
		return administratorAccountDirectoryCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return administratorAccountDirectoryCursor{}, err
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return administratorAccountDirectoryCursor{}, errors.New(errorCodeInvalidInput)
	}
	return administratorAccountDirectoryCursor{Email: parts[0], ID: parts[1]}, nil
}

func escapeAdministratorAccountSearch(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "%", "\\%")
	return strings.ReplaceAll(value, "_", "\\_")
}
