package app

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An open page shows rows from databases on other pages: the sources its
// linked databases show and the ones its relations point to. Those rows
// change on their own pages, so the page asks for the latest change of each
// such page the reader can still read, and whether it is in the trash, and
// reads its graph again when one moves, goes to the trash or comes back, or a
// page leaves the list: a linked database of a page in the trash shows its
// rows to read only (content_graph_window.go).

type contentGraphExternalRevision struct {
	PageID   string `json:"pageId" db:"page_id"`
	Revision int64  `json:"revision" db:"revision"`
	InTrash  bool   `json:"inTrash,omitempty" db:"-"`
}

type contentGraphExternalRevisions struct {
	Pages []contentGraphExternalRevision `json:"pages"`
}

func readContentGraphExternalRevisions(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	response, err := loadContentGraphExternalRevisions(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("id")))
	if err != nil {
		return writeGraphError(event, err, "read", errorCodeGraphReadFailed, "Couldn't load the content.")
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func loadContentGraphExternalRevisions(app core.App, principalID, pageID string) (contentGraphExternalRevisions, error) {
	response := contentGraphExternalRevisions{Pages: []contentGraphExternalRevision{}}
	current, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return response, graphNotFoundError()
	}
	if err != nil {
		return response, err
	}
	scope, err := authorizeLinkedDatabaseReadScope(app, principalID, current, pageID)
	if err != nil {
		return response, err
	}
	pages := map[string]struct{}{}
	for _, page := range scope.sourceContainerPages {
		pages[page] = struct{}{}
	}
	for _, page := range scope.relationSourcePages {
		pages[page] = struct{}{}
	}
	delete(pages, pageID)
	if len(pages) == 0 {
		return response, nil
	}
	ids := make([]string, 0, len(pages))
	for page := range pages {
		ids = append(ids, page)
	}
	sort.Strings(ids)
	encoded, _ := json.Marshal(ids)
	if err := app.DB().NewQuery(`
		SELECT [[value]] AS [[page_id]], COALESCE((
			SELECT MAX([[mutation]].[[cursor]]) FROM {{leafen_graph_mutations}} AS [[mutation]]
			WHERE [[mutation]].[[owner]] = {:owner} AND [[mutation]].[[page_id]] = [[pages]].[[value]]
		), 0) AS [[revision]]
		FROM json_each({:pages}) AS [[pages]] ORDER BY [[value]]
	`).Bind(dbx.Params{"owner": current.Workspace.OwnerID, "pages": string(encoded)}).All(&response.Pages); err != nil {
		return response, err
	}
	trashed, err := loadPageTrashEffects(app, ids)
	for index, page := range response.Pages {
		_, response.Pages[index].InTrash = trashed[page.PageID]
	}
	return response, err
}
