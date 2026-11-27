package app

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	formViewPickerDefaultLimit = 25
	formViewPickerMaximumLimit = 50
	formViewPickerScanWindow   = 200
)

type formViewPickerItem struct {
	ViewID       string `json:"viewId"`
	Name         string `json:"name"`
	DataSourceID string `json:"dataSourceId"`
	PageID       string `json:"pageId"`
	PageTitle    string `json:"pageTitle"`
	// Its page's kind, for the app to name it while untitled as it names a
	// page everywhere (page_name_kind.go).
	PageKind     string                `json:"pageKind"`
	ViewBlock    bool                  `json:"viewBlock,omitempty"`
	SourceTitles []databaseSourceTitle `json:"sourceTitles,omitempty"`
	// ContainerID is the database block the form's source is in on its
	// page, which names the database at the end of the form's place, as
	// Notion's open list ends it (seen 2026-11-24 in two tabs: "Page /
	// Database", a database of several sources by its own name).
	ContainerID string `json:"containerId,omitempty"`
}

// readFormViewPicker lists the forms of the workspace a person can read, as
// an open action offers them under "폼" with where each sits (Notion's,
// seen 2026-11-02): a view of the form kind, on a page they may read that is
// out of the trash with every page above it, its database not in the trash,
// by name.
// A form is named by its title where it has one, else its view's name, as
// Notion's open list names "회의 신청" (seen 2026-11-05).
const formViewPickerNameSQL = `COALESCE(NULLIF(trim(json_extract([[view]].[[config]], '$.leafenForm.title')), ''), [[view]].[[name]])`

func readFormViewPicker(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := strings.TrimSpace(event.Request.URL.Query().Get("q"))
	limit := formViewPickerDefaultLimit
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the form search.")
		}
		limit = parsed
	}
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength ||
		utf8.RuneCountInString(query) > databaseContainerPickerMaximumQuery || limit < 1 || limit > formViewPickerMaximumLimit {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the form search.")
	}
	workspace, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the forms.", err)
	}
	// Every form by the name is looked at, newest first, until the list is
	// full (fillWindows): those of the latest 200 the reader could open were
	// all it offered.
	judge := withAccessMemo(event.App)
	readable := map[string]*core.Record{}
	found, err := fillWindows(formViewPickerScanWindow, limit,
		func(size int, last *formViewPickerCandidate) ([]formViewPickerCandidate, error) {
			return readFormViewPickerWindow(event.App, workspace.OwnerID, workspaceID, query, size,
				windowFillAfterNewest(last, func(candidate *formViewPickerCandidate) (int64, string) {
					return candidate.Updated, candidate.ID
				}))
		},
		func(candidate *formViewPickerCandidate) (bool, error) {
			page, seen := readable[candidate.PageID]
			if !seen {
				if auth, err := authorizePage(judge, user.Id, candidate.PageID, permissionWorkspaceRead); err == nil &&
					auth.Workspace.OwnerID == workspace.OwnerID && auth.Workspace.WorkspaceID == workspaceID {
					// Not a page in the trash or under one (loadPageTrashEffects).
					_, trashed, err := loadPageTrashEffect(judge, candidate.PageID)
					if err != nil {
						return false, err
					}
					if !trashed {
						page = auth.Page
					}
				}
				readable[candidate.PageID] = page
			}
			return page != nil, nil
		})
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the forms.", err)
	}
	items := make([]formViewPickerItem, 0, len(found))
	for _, candidate := range found {
		items = append(items, formViewPickerItem{
			ViewID: candidate.ID, Name: candidate.Name, DataSourceID: candidate.DataSourceID,
			// Its page named as the person reads it (page_name_mentions.go).
			PageID: candidate.PageID, PageTitle: rowTitleAsRead(event.App, user.Id, candidate.PageID, readable[candidate.PageID].GetString("title")),
		})
	}
	pageIDs := make([]string, 0, len(items))
	for _, item := range items {
		pageIDs = append(pageIDs, item.PageID)
	}
	kinds, err := loadPageNameKinds(event.App, pageIDs)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the forms.", err)
	}
	for index := range items {
		items[index].PageKind, items[index].ViewBlock = pageKindPage, false
		if named, found := kinds[items[index].PageID]; found {
			items[index].PageKind, items[index].ViewBlock, items[index].SourceTitles = named.Kind, named.ViewBlock, named.SourceTitles
		}
		if items[index].ContainerID, err = formViewPickerContainer(event.App, workspace.OwnerID, items[index]); err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the forms.", err)
		}
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{"items": items})
}

// formViewPickerContainer is the database block on the form's page that
// holds its source — not a linked view of it — the first on the page where
// more do; "" for none.
func formViewPickerContainer(app core.App, ownerID string, item formViewPickerItem) (string, error) {
	containers := []string{}
	err := app.DB().NewQuery(`
		SELECT [[membership]].[[container_id]] FROM {{leafen_database_container_sources}} AS [[membership]]
		JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
		WHERE [[membership]].[[owner]] = {:owner} AND [[membership]].[[page_id]] = {:pageId}
			AND [[membership]].[[data_source_id]] = {:dataSourceId} AND [[container]].[[kind]] = 'source'
		ORDER BY [[container]].[[position]], [[container]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"owner": ownerID, "pageId": item.PageID, "dataSourceId": item.DataSourceID}).Column(&containers)
	if err != nil || len(containers) == 0 {
		return "", err
	}
	return containers[0], nil
}

type formViewPickerCandidate struct {
	ID           string `db:"id"`
	Name         string `db:"name"`
	DataSourceID string `db:"data_source_id"`
	PageID       string `db:"page_id"`
	Updated      int64  `db:"updated"`
}

// readFormViewPickerWindow reads a window of the workspace's form views by
// the name, their database not in the trash, newest first, after the one
// ending where after says (windowFillAfterNewestSQL).
func readFormViewPickerWindow(
	app core.App, ownerID, workspaceID, query string, size int, after dbx.Params,
) ([]formViewPickerCandidate, error) {
	params := dbx.Params{"owner": ownerID, "workspaceId": workspaceID, "query": query, "limit": size}
	for key, value := range after {
		params[key] = value
	}
	candidates := []formViewPickerCandidate{}
	err := app.DB().NewQuery(`
		SELECT [[view]].[[id]], ` + formViewPickerNameSQL + ` AS [[name]], [[view]].[[data_source_id]], [[view]].[[page_id]], [[view]].[[updated]]
		FROM {{leafen_database_views}} AS [[view]]
		WHERE [[view]].[[owner]] = {:owner} AND [[view]].[[workspace_id]] = {:workspaceId} AND [[view]].[[type]] = 'form'
			AND ({:query} = '' OR instr(lower(` + formViewPickerNameSQL + `), lower({:query})) > 0)
			AND ` + databaseSourceNotTrashedSQL("[[view]].[[data_source_id]]") + `
			AND ` + windowFillAfterNewestSQL("[[view]].[[updated]]", "[[view]].[[id]]") + `
		ORDER BY [[view]].[[updated]] DESC, [[view]].[[id]] LIMIT {:limit}
	`).Bind(params).All(&candidates)
	return candidates, err
}
