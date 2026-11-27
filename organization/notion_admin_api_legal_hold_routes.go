package organization

import (
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

// serveNotionAdminLegalHoldAPI serves the Admin API's legal hold endpoints;
// it answers whether the request was one.
func serveNotionAdminLegalHoldAPI(event *core.RequestEvent, jobs *leafen.DurableJobRunner) (bool, error) {
	if event.Request.URL.Path == leafen.NotionAdminAPILegalHoldsPath {
		switch event.Request.Method {
		case http.MethodGet:
			return true, serveNotionAdminLegalHoldList(event)
		case http.MethodPost:
			return true, serveNotionAdminLegalHoldCreate(event)
		}
	}
	if legalHoldID, ok := notionAdminLegalHoldID(event.Request.URL.Path); ok {
		switch event.Request.Method {
		case http.MethodGet:
			return true, serveNotionAdminLegalHoldRetrieve(event, legalHoldID)
		case http.MethodPatch:
			return true, serveNotionAdminLegalHoldUpdate(event, legalHoldID)
		}
	}
	if legalHoldID, ok := notionAdminLegalHoldUsersID(event.Request.URL.Path); ok {
		if event.Request.Method == http.MethodGet {
			return true, serveNotionAdminLegalHoldUsersList(event, legalHoldID)
		}
		if event.Request.Method == http.MethodPost {
			return true, serveNotionAdminLegalHoldUsersAdd(event, legalHoldID)
		}
	}
	if legalHoldID, ok := notionAdminLegalHoldWorkspacesID(event.Request.URL.Path); ok &&
		event.Request.Method == http.MethodGet {
		return true, serveNotionAdminLegalHoldWorkspacesList(event, legalHoldID)
	}
	if legalHoldID, workspaceID, ok := notionAdminLegalHoldPagesIDs(event.Request.URL.Path); ok &&
		event.Request.Method == http.MethodGet {
		return true, serveNotionAdminLegalHoldPagesList(event, legalHoldID, workspaceID)
	}
	if legalHoldID, ok := notionAdminLegalHoldExportID(event.Request.URL.Path); ok &&
		event.Request.Method == http.MethodPost {
		return true, serveNotionAdminLegalHoldExport(event, jobs, legalHoldID)
	}
	if legalHoldID, userID, ok := notionAdminLegalHoldUserIDs(event.Request.URL.Path); ok &&
		event.Request.Method == http.MethodDelete {
		return true, serveNotionAdminLegalHoldUserRemove(event, legalHoldID, userID)
	}
	if legalHoldID, ok := notionAdminLegalHoldReleaseID(event.Request.URL.Path); ok && event.Request.Method == http.MethodPost {
		return true, serveNotionAdminLegalHoldRelease(event, legalHoldID)
	}
	return false, nil
}

func notionAdminLegalHoldID(path string) (string, bool) {
	value, found := strings.CutPrefix(path, leafen.NotionAdminAPILegalHoldsPath+"/")
	return value, found && value != "" && !strings.Contains(value, "/")
}

func notionAdminLegalHoldReleaseID(path string) (string, bool) {
	value, found := strings.CutPrefix(path, leafen.NotionAdminAPILegalHoldsPath+"/")
	legalHoldID, suffix, separated := strings.Cut(value, "/")
	return legalHoldID, found && separated && legalHoldID != "" && suffix == "release"
}

func notionAdminLegalHoldUsersID(path string) (string, bool) {
	value, found := strings.CutPrefix(path, leafen.NotionAdminAPILegalHoldsPath+"/")
	legalHoldID, suffix, separated := strings.Cut(value, "/")
	return legalHoldID, found && separated && legalHoldID != "" && suffix == "users"
}

func notionAdminLegalHoldWorkspacesID(path string) (string, bool) {
	value, found := strings.CutPrefix(path, leafen.NotionAdminAPILegalHoldsPath+"/")
	legalHoldID, suffix, separated := strings.Cut(value, "/")
	return legalHoldID, found && separated && legalHoldID != "" && suffix == "workspaces"
}

func notionAdminLegalHoldPagesIDs(path string) (string, string, bool) {
	value, found := strings.CutPrefix(path, leafen.NotionAdminAPILegalHoldsPath+"/")
	parts := strings.Split(value, "/")
	if !found || len(parts) != 4 || parts[0] == "" || parts[1] != "spaces" ||
		parts[2] == "" || parts[3] != "pages" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

func notionAdminLegalHoldExportID(path string) (string, bool) {
	value, found := strings.CutPrefix(path, leafen.NotionAdminAPILegalHoldsPath+"/")
	legalHoldID, suffix, separated := strings.Cut(value, "/")
	return legalHoldID, found && separated && legalHoldID != "" && suffix == "export"
}

func notionAdminLegalHoldUserIDs(path string) (string, string, bool) {
	value, found := strings.CutPrefix(path, leafen.NotionAdminAPILegalHoldsPath+"/")
	parts := strings.Split(value, "/")
	if !found || len(parts) != 3 || parts[0] == "" || parts[1] != "users" || parts[2] == "" {
		return "", "", false
	}
	return parts[0], parts[2], true
}
