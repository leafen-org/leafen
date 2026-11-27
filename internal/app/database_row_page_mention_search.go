package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The @ mention menu finds database row pages by title, as Notion's does —
// a row holding only some of the words typed too, after those holding them
// all, by the rank pages go by (mention_name_match.go), and within it the
// row edited last first, the order the menu gives pages and rows alike
// (mentionMenuItems.ts); each candidate is one the reader may open, through its database's page or
// by its own shares, and names that page to those who may read it; the rows
// of a database in the trash, or under a page there, aren't, as search finds
// none of them. The databases rows hold in their bodies are found with them
// by their names (database_row_body_mention_search.go). A workspace's People rows are its
// people, which the menu offers as people, not pages: Notion's "[[" and "@"
// find People itself (database_row_page_mention_people.go) but none of its rows (seen 2026-11-21 in Chrome, a
// member's and the owner's name in a free workspace's "[[" and a
// Business-trial one's "[[" and "@").

const (
	databaseRowMentionSearchLimit = 8
	// A list that shows more found pages than the menu asks for more rows:
	// a button's "열기" list, which shows twenty at a time (Notion's, seen
	// 2026-11-24 in two tabs).
	databaseRowMentionMaximumLimit   = 50
	databaseRowMentionScanWindow     = 100
	databaseRowMentionMaximumQuery   = 128
	databaseRowMentionSearchPathName = "/workspaces/{workspaceId}/row-page-mentions"
)

type databaseRowMentionCandidate struct {
	RowID          string `json:"rowId" db:"row_id"`
	Title          string `json:"title" db:"title"`
	Icon           string `json:"icon" db:"icon"`
	IconType       string `json:"iconType" db:"icon_type"`
	ContainerPage  string `json:"containerPageId" db:"container_page_id"`
	ContainerTitle string `json:"containerTitle" db:"-"`
	DataSourceID   string `json:"-" db:"data_source_id"`
	// Updated is when it was edited last, in milliseconds, which orders the
	// menu's candidates answering as well (mentionMenuItems.ts).
	Updated int64 `json:"editedAt,omitzero" db:"updated"`
	// InlineDatabase and DatabaseTitle name the inline database between the
	// container and the row in its path, as Notion's @ menu shows it.
	InlineDatabase bool   `json:"inlineDatabase,omitzero" db:"-"`
	DatabaseTitle  string `json:"databaseTitle,omitempty" db:"-"`
	// UntitledName is what the row is called while it has no title.
	UntitledName string `json:"untitledName,omitempty" db:"-"`
	// DatabaseID marks the database the row holds in its body, named by
	// Title, its row by RowTitle (database_row_body_mention_search.go).
	DatabaseID string `json:"databaseId,omitempty" db:"-"`
	RowTitle   string `json:"rowTitle,omitempty" db:"-"`
	// Archived says the row is archived itself or with a page above it,
	// which the menu marks as Notion's "@" marks an archived page.
	Archived bool `json:"archived,omitempty" db:"-"`
	// PeopleDatabase marks People itself, DatabaseID its database (database_row_page_mention_people.go).
	PeopleDatabase bool `json:"peopleDatabase,omitzero" db:"-"`
	// SourceID marks one source of a database holding several, named by
	// Title, its database's sources named by SourceTitles in their order
	// (database_source_mention_search.go).
	SourceID     string                `json:"dataSourceId,omitempty" db:"-"`
	SourceTitles []databaseSourceTitle `json:"sourceTitles,omitempty" db:"-"`
}

func registerDatabaseRowMentionSearchRoute(event *core.ServeEvent) {
	event.Router.GET(apiRoot+databaseRowMentionSearchPathName, readDatabaseRowMentionCandidates)
}

func readDatabaseRowMentionCandidates(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := strings.TrimSpace(event.Request.URL.Query().Get("q"))
	limit := databaseRowMentionSearchLimit
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > databaseRowMentionMaximumLimit {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the search query.")
		}
		limit = parsed
	}
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength ||
		utf8.RuneCountInString(query) > databaseRowMentionMaximumQuery {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the search query.")
	}
	workspace, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't find the page.", err)
	}
	// Every match is looked at, a tier at a time (mentionSearchTiers) and
	// newest first in each, until the menu's rows are found: the hundred
	// latest read and then those the reader may not open left out, a member
	// who could open none of them was offered nothing (fillWindows). Each
	// decision on a database's page is asked once.
	judge := withAccessMemo(event.App)
	containers := map[string]databaseRowMentionContainer{}
	locations := newInlineDatabaseLocations(event.App, user.Id, workspaceID).reading(event.Request.Context())
	items, err := fillMentionTiers(query, limit, func(tier mentionSearchTier, need int) ([]databaseRowMentionCandidate, error) {
		return fillWindows(databaseRowMentionScanWindow, need,
			func(size int, last *databaseRowMentionCandidate) ([]databaseRowMentionCandidate, error) {
				window, err := readDatabaseRowMentionWindow(event.App, workspace.OwnerID, workspaceID, query, tier, size,
					windowFillAfterNewest(last, databaseRowMentionWindowKey))
				if err != nil {
					return nil, err
				}
				// The window's row records and the roles their shares give the
				// reader are read at once, not row by row.
				ids := make([]string, 0, len(window))
				for _, candidate := range window {
					ids = append(ids, candidate.RowID)
				}
				if err := primeRecordsRemembered(judge, pagesCollection, ids); err != nil {
					return nil, err
				}
				return window, primePageShareRoles(judge, user.Id, ids)
			},
			func(candidate *databaseRowMentionCandidate) (bool, error) {
				// The index's lower case holds a number inside another, which the menu doesn't.
				if !mentionNameInTier(candidate.Title, query, tier) {
					return false, nil
				}
				if _, err := authorizePage(judge, user.Id, candidate.RowID, permissionWorkspaceRead); err != nil {
					if isPermissionDenied(err) {
						return false, nil
					}
					return false, err
				}
				container, checked := containers[candidate.ContainerPage]
				if !checked {
					var err error
					container, err = loadDatabaseRowMentionContainer(judge, user.Id, workspaceID, candidate.ContainerPage)
					if err != nil {
						return false, err
					}
					containers[candidate.ContainerPage] = container
				}
				if container.trashed {
					return false, nil
				}
				// A title naming a page closed to the reader (content_graph_reader_mentions.go).
				candidate.Title = rowTitleAsRead(event.App, user.Id, candidate.RowID, databaseRowPageTitle(event.App, candidate.RowID))
				candidate.ContainerTitle = container.title
				location, err := locations.of(candidate.ContainerPage, candidate.DataSourceID)
				if err != nil {
					return false, err
				}
				candidate.InlineDatabase, candidate.DatabaseTitle = location.Inline, location.Title
				candidate.UntitledName = location.UntitledRow
				return true, nil
			})
	})
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't find the page.", err)
	}
	// The databases rows hold in their bodies come before the rows, as databases among the pages, People first.
	databases, err := readDatabaseRowBodyMentionCandidates(event.Request.Context(), event.App, user.Id, workspaceID, workspace.OwnerID, query, databaseRowMentionSearchLimit)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't find the page.", err)
	}
	people, err := readPeopleDatabaseMentionCandidate(event.App, user.Id, workspaceID, query)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't find the page.", err)
	}
	// Each source of a database holding several, after the databases, before the rows.
	sources, err := readDatabaseSourceMentionCandidates(event.App, user.Id, workspaceID, workspace.OwnerID, query, databaseRowMentionSearchLimit)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't find the page.", err)
	}
	found := append(append(append(people, databases...), sources...), items...)
	if err := markArchivedRowMentionCandidates(event.App, found); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't find the page.", err)
	}
	return event.JSON(http.StatusOK, map[string]any{"items": found})
}

// databaseRowMentionWindowKey is where a window of row pages ends.
func databaseRowMentionWindowKey(candidate *databaseRowMentionCandidate) (int64, string) {
	return candidate.Updated, candidate.RowID
}

// readDatabaseRowMentionWindow reads a window of the workspace's row pages
// whose title may answer the query within a tier (mentionTierSQL), newest
// first, after the one ending where after says (windowFillAfterNewestSQL):
// rows of a database in the trash, of one deleted or of People are none.
func readDatabaseRowMentionWindow(
	app core.App, ownerID, workspaceID, query string, tier mentionSearchTier, size int, after dbx.Params,
) ([]databaseRowMentionCandidate, error) {
	candidates := []databaseRowMentionCandidate{}
	params := dbx.Params{"owner": ownerID, "workspaceId": workspaceID, "limit": size}
	for key, value := range after {
		params[key] = value
	}
	// Each row's title is read once, below, whatever the words asked of it.
	err := app.DB().NewQuery(`
		SELECT * FROM (
			SELECT [[page]].[[row_id]], [[page]].[[container_page_id]], [[page]].[[data_source_id]], [[page]].[[updated]],
				[[row_record]].[[icon]], [[row_record]].[[icon_type]],
				COALESCE((SELECT json_extract([[value]].[[value]], '$') FROM {{leafen_database_row_values}} AS [[value]]
					WHERE [[value]].[[row_id]] = [[page]].[[row_id]] AND [[value]].[[type]] = 'title' LIMIT 1), '') AS [[title]]
			FROM {{leafen_database_row_pages}} AS [[page]]
			JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]] = [[page]].[[row_id]]
			WHERE [[page]].[[owner]] = {:owner} AND [[page]].[[workspace_id]] = {:workspaceId}
				AND ` + databaseSourceNotTrashedSQL("[[page]].[[data_source_id]]") + `
				AND ` + searchSourceNotDeletedSQL("[[page]].[[workspace_id]]", "[[page]].[[data_source_id]]") + `
				AND ` + databaseRowNotTrashedSQL("[[page]].[[row_id]]") + `
				AND [[page]].[[data_source_id]] NOT IN (SELECT [[data_source_id]] FROM {{leafen_workspace_people_databases}})
				AND ` + windowFillAfterNewestSQL("[[page]].[[updated]]", "[[page]].[[row_id]]") + `
		) AS [[found]]
		WHERE ` + mentionTierSQL("[[found]].[[title]]", query, tier, params) + `
		ORDER BY [[found]].[[updated]] DESC, [[found]].[[row_id]] LIMIT {:limit}
	`).Bind(params).All(&candidates)
	return candidates, err
}

// markArchivedRowMentionCandidates marks the candidates whose row is
// archived, itself or with a page above it; a database a row holds in its
// body goes by that row. Archived rows stay found, as Notion's "@" finds an
// archived page and the pages under it (seen 2026-11-19).
func markArchivedRowMentionCandidates(app core.App, candidates []databaseRowMentionCandidate) error {
	rowIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		rowIDs = append(rowIDs, candidate.RowID)
	}
	effects, err := loadPageArchiveEffects(app, rowIDs)
	if err != nil {
		return err
	}
	for index := range candidates {
		_, candidates[index].Archived = effects[candidates[index].RowID]
	}
	return nil
}

// databaseRowMentionContainer is the page that holds a candidate's database;
// its title is empty to those who may not read it. It is trashed while it is
// in the trash or under a page there (loadPageTrashEffects): the rows of a
// database under a trashed page went to the trash with it, as search finds
// none of them.
type databaseRowMentionContainer struct {
	title   string
	trashed bool
}

func loadDatabaseRowMentionContainer(
	app core.App, principalID, workspaceID, pageID string,
) (databaseRowMentionContainer, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseRowMentionContainer{trashed: true}, nil
	}
	if err != nil {
		return databaseRowMentionContainer{}, err
	}
	_, trashed, err := loadPageTrashEffect(app, pageID)
	if err != nil {
		return databaseRowMentionContainer{}, err
	}
	container := databaseRowMentionContainer{trashed: trashed}
	readable, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
	if err != nil && !isPermissionDenied(err) {
		return databaseRowMentionContainer{}, err
	}
	if err == nil && readable.Workspace.WorkspaceID == workspaceID {
		container.title = page.GetString("title")
	}
	return container, nil
}
