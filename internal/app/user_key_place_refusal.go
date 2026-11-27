package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A key's call whose place isn't in the workspaces the key reaches says
// what it named and didn't find there — the page, row, file or form, by its
// ID — not that a workspace wasn't found; and a call that names no place at
// all, or two, says which field to give (2026-11-16, an agent calling every
// operation: a missing file or row, a body sent without its workspaceId and
// one naming pageIds for pageId all answered "LEAFEN_WORKSPACE_NOT_FOUND …
// in another workspace, or it doesn't exist").

// userKeyPlaceKinds are what each kind of place names, with the code of not
// finding one.
var userKeyPlaceKinds = map[userKeyPlace]struct{ kind, code string }{
	userKeyPlaceWorkspaceParam:    {"workspace", errorCodeWorkspaceNotFound},
	userKeyPlacePageParam:         {"page", errorCodePageNotFound},
	userKeyPlaceRowParam:          {"database row", errorCodeDatabaseRowPageNotFound},
	userKeyPlaceContainerParam:    {"database", errorCodeDatabaseNotFound},
	userKeyPlaceDataSourceParam:   {"data source", errorCodeDatabaseDataSourceNotFound},
	userKeyPlaceJobParam:          {"job", errorCodeJobNotFound},
	userKeyPlaceOrganizationParam: {"organization", errorCodeOrganizationNotFound},
	userKeyPlaceFileParam:         {"file", errorCodeFileNotFound},
	userKeyPlaceBodyWorkspace:     {"workspace", errorCodeWorkspaceNotFound},
	userKeyPlaceBodyPage:          {"page", errorCodePageNotFound},
	userKeyPlaceForm:              {"form", errorCodeFormNotFound},
	// Its route answers a link that isn't there so (page_guest_links.go).
	userKeyPlaceGuestLinkParam: {"guest invite link", errorCodePublicMutationNotFound},
}

// refuseUserKeyPlace answers a call whose place the key doesn't reach.
func refuseUserKeyPlace(event *core.RequestEvent, rule userKeyRule, body userKeyBody) error {
	place, known := userKeyPlaceKinds[rule.place]
	if !known {
		place = userKeyPlaceKinds[userKeyPlaceWorkspaceParam]
	}
	id, field := userKeyPathValue(event.Request.URL.Path), ""
	switch rule.place {
	case userKeyPlaceBodyWorkspace, userKeyPlaceBodyPage:
		field = rule.param
	case userKeyPlaceForm:
		field = "token"
		if pageID, _ := body.one("pageId"); pageID != "" {
			field = "pageId"
		}
	}
	if field != "" {
		value, single := body.one(field)
		switch {
		case !single:
			apiErrorAbout(event, nil, apiErrorField{Name: field, Problem: "given twice with different values"})
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput,
				"Give "+field+" once: the "+place.kind+" this acts on.")
		case value == "":
			apiErrorAbout(event, nil, apiErrorField{Name: field, Problem: apiErrorFieldMissing})
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput,
				"Give "+field+": the "+place.kind+" this acts on.")
		}
		id = value
	}
	apiErrorAbout(event, &apiErrorTarget{Kind: place.kind, ID: id})
	return apiError(event, http.StatusNotFound, place.code,
		strings.ToUpper(place.kind[:1])+place.kind[1:]+" "+id+" isn't in the workspaces this key reaches, or doesn't exist.")
}
