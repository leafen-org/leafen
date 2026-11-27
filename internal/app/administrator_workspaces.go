package app

import (
	"encoding/base64"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Server administrators see every workspace on the server by name, with its
// owner, organization, members, pages out of trash, and stored files, and
// find one by its name or its owner's name or email. The counts are read
// for the window shown; `id` reads one workspace's, as its own page asks
// again after a file or page there is removed.

const administratorWorkspaceCursorPrefix = "hws1."

type storedAdministratorServerWorkspace struct {
	ID             string `db:"id"`
	Name           string `db:"name"`
	SortName       string `db:"sort_name"`
	Domain         string `db:"domain"`
	OwnerID        string `db:"owner_id"`
	OwnerName      string `db:"owner_name"`
	OwnerEmail     string `db:"owner_email"`
	OrganizationID string `db:"organization_id"`
	MemberCount    int64  `db:"member_count"`
	PageCount      int64  `db:"page_count"`
	FileCount      int64  `db:"file_count"`
	StorageBytes   int64  `db:"storage_bytes"`
	Created        string `db:"created"`
}

func registerAdministratorWorkspaceRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/admin/workspaces", listAdministratorWorkspaces)
}

func listAdministratorWorkspaces(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	query := event.Request.URL.Query()
	search := strings.ToLower(strings.TrimSpace(query.Get("query")))
	if utf8.RuneCountInString(search) > administratorAccountSearchRunes {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the search query.")
	}
	afterName, afterID, ok := decodeAdministratorWorkspaceCursor(strings.TrimSpace(query.Get("cursor")))
	if !ok {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the list position.")
	}
	filter := `TRUE`
	params := dbx.Params{"limit": administratorWorkspaceListLimit + 1}
	if id := strings.TrimSpace(query.Get("id")); id != "" {
		filter += ` AND [[workspace]].[[id]] = {:id}`
		params["id"] = id
	}
	if search != "" {
		filter += ` AND (lower([[workspace]].[[name]]) LIKE {:search} ESCAPE '\'
			OR lower(COALESCE([[owner]].[[email]], '')) LIKE {:search} ESCAPE '\'
			OR lower(COALESCE([[owner]].[[name]], '')) LIKE {:search} ESCAPE '\')`
		params["search"] = "%" + escapeAdministratorAccountSearch(search) + "%"
	}
	if afterID != "" {
		filter += ` AND (lower([[workspace]].[[name]]) > {:afterName}
			OR (lower([[workspace]].[[name]]) = {:afterName} AND [[workspace]].[[id]] > {:afterId}))`
		params["afterName"], params["afterId"] = afterName, afterID
	}
	rows := []storedAdministratorServerWorkspace{}
	if err := event.App.DB().NewQuery(`
		SELECT [[workspace]].[[id]], [[workspace]].[[name]], lower([[workspace]].[[name]]) AS [[sort_name]], COALESCE([[workspace]].[[domain]], '') AS [[domain]],
			COALESCE([[workspace]].[[workspace_owner]], '') AS [[owner_id]],
			COALESCE([[owner]].[[name]], '') AS [[owner_name]], COALESCE([[owner]].[[email]], '') AS [[owner_email]],
			COALESCE([[workspace]].[[organization]], '') AS [[organization_id]], [[workspace]].[[created]],
			` + workspaceMemberCountSQL("[[workspace]].[[id]]") + ` AS [[member_count]],
			(SELECT COUNT(*) FROM {{leafen_pages}} [[page]]
				WHERE [[page]].[[workspace]] = [[workspace]].[[id]] AND [[page]].[[owner]] = [[workspace]].[[owner]]
					AND [[page]].[[notion_import_staging]] = FALSE AND [[page]].[[in_trash]] = FALSE
					AND [[page]].[[data_source_id]] = '' AND ` + workspaceArchiveTargetPageSQL("[[page]]") + `) AS [[page_count]],
			(SELECT COUNT(*) FROM {{leafen_files}} [[file]] WHERE [[file]].[[workspace]] = [[workspace]].[[id]]) AS [[file_count]],
			(SELECT COALESCE(SUM([[file]].[[size]]), 0) FROM {{leafen_files}} [[file]]
				WHERE [[file]].[[workspace]] = [[workspace]].[[id]]) AS [[storage_bytes]]
		FROM {{leafen_workspaces}} [[workspace]]
		LEFT JOIN {{users}} [[owner]] ON [[owner]].[[id]] = [[workspace]].[[workspace_owner]]
		WHERE ` + filter + `
		ORDER BY lower([[workspace]].[[name]]), [[workspace]].[[id]] LIMIT {:limit}
	`).Bind(params).All(&rows); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed,
			"Couldn't read the workspace list.", "administrator-workspaces", "read", errorCodeAdministratorReadFailed, err)
	}
	list := administratorServerWorkspaceList{Items: []administratorServerWorkspace{}}
	if len(rows) > administratorWorkspaceListLimit {
		rows = rows[:administratorWorkspaceListLimit]
		last := rows[len(rows)-1]
		cursor := administratorWorkspaceCursorPrefix + base64.RawURLEncoding.EncodeToString(
			[]byte(last.SortName+"\x00"+last.ID))
		list.NextCursor = &cursor
	}
	for _, row := range rows {
		item := administratorServerWorkspace{
			ID: row.ID, Name: row.Name, Domain: row.Domain, OwnerID: row.OwnerID, OwnerName: row.OwnerName,
			OwnerEmail: row.OwnerEmail, OrganizationID: row.OrganizationID, MemberCount: row.MemberCount,
			PageCount: row.PageCount, FileCount: row.FileCount, StorageBytes: row.StorageBytes, CreatedAt: row.Created,
		}
		list.Items = append(list.Items, item)
	}
	return event.JSON(http.StatusOK, list)
}

func decodeAdministratorWorkspaceCursor(raw string) (string, string, bool) {
	if raw == "" {
		return "", "", true
	}
	encoded, found := strings.CutPrefix(raw, administratorWorkspaceCursorPrefix)
	if !found || len(encoded) > 2048 {
		return "", "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", false
	}
	name, id, found := strings.Cut(string(decoded), "\x00")
	return name, id, found && id != ""
}
