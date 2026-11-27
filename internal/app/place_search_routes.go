package app

import (
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
)

var errPlaceSearchInvalid = errors.New(errorCodePlaceSearchInvalid)

func registerPlaceSearchRoutes(event *core.ServeEvent) {
	provider := placeSearchProviderFor(event.App)
	event.Router.POST(apiRoot+placeSearchPath, func(request *core.RequestEvent) error {
		var input placeSearchInput
		return servePlaceLookup(request, func() error {
			if request.BindBody(&input) != nil {
				return errPlaceSearchInvalid
			}
			if input.Query = strings.TrimSpace(input.Query); input.Query == "" {
				return refusedMissingInput(errorCodePlaceSearchInvalid, "query", "the words to look places up by, as a map's search box takes them.")
			}
			if !validPlaceSearchQuery(input.Query) {
				return errPlaceSearchInvalid
			}
			return nil
		}, func(config placeSearchConfig) (any, error) {
			return provider.search(request.Request.Context(), config, input.Query)
		})
	})
	// The name of a point, as a map's pin names the page it makes (Notion's, seen 2026-11-02).
	event.Router.POST(apiRoot+placeReversePath, func(request *core.RequestEvent) error {
		var input placeReverseInput
		return servePlaceLookup(request, func() error {
			if request.BindBody(&input) != nil || !validPlacePoint(input.Lat, input.Lon) {
				return errPlaceSearchInvalid
			}
			return nil
		}, func(config placeSearchConfig) (any, error) {
			return provider.reverse(request.Request.Context(), config, input.Lat, input.Lon, request.Request.Header.Get("Accept-Language"))
		})
	})
}

// placeSearchProviderFor is the server's one provider client, which a page's
// search and a form's share: one pace and one cache for every searcher.
func placeSearchProviderFor(app core.App) *placeSearchProvider {
	provider, _ := app.Store().GetOrSet(placeSearchProviderStoreKey, func() any {
		return newPlaceSearchProvider()
	}).(*placeSearchProvider)
	return provider
}

const placeSearchProviderStoreKey = "leafenPlaceSearchProvider"

// validPlaceSearchQuery is a search's words: some, at most 200 characters, no control characters.
func validPlaceSearchQuery(query string) bool {
	return query != "" && len([]rune(query)) <= 200 && strings.IndexFunc(query, unicode.IsControl) < 0
}

// servePlaceLookup asks the configured provider for someone who may edit the
// page, and answers only while they still may and the provider is unchanged.
func servePlaceLookup(request *core.RequestEvent, bind func() error, lookup func(placeSearchConfig) (any, error)) error {
	user, ok := authenticatedUser(request)
	if !ok {
		return apiError(request, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	auth, err := authorizePageBody(request.App, user.Id, request.Request.PathValue("pageId"), permissionWorkspaceWrite)
	if err != nil {
		return placeSearchAccessError(request, err)
	}
	locked, err := pageLockedFor(request.App, auth.Page, user.Id)
	if err != nil {
		return placeSearchAccessError(request, err)
	}
	if locked {
		return placeSearchAccessError(request, pageLockedError())
	}
	request.Request.Body = http.MaxBytesReader(request.Response, request.Request.Body, 4096)
	// One left out is named (api_missing_input.go), as itself.
	if err := bind(); err != nil {
		return apiErrorFrom(request, http.StatusBadRequest, errorCodePlaceSearchInvalid, "Check the place search.", err)
	}
	settings, err := loadAdministratorSettings(request.App)
	if err != nil {
		return placeSearchRouteError(request, failedBecause(errPlaceSearchUnavailable, err))
	}
	config, err := effectivePlaceSearchConfig(request.App, settings)
	if err != nil {
		return placeSearchRouteError(request, failedBecause(errPlaceSearchUnavailable, err))
	}
	output, err := lookup(config)
	if err != nil {
		return placeSearchRouteError(request, err)
	}
	// Access may have been revoked while the provider answered.
	auth, err = authorizePageBody(request.App, user.Id, auth.Page.Id, permissionWorkspaceWrite)
	if err != nil {
		return placeSearchAccessError(request, err)
	}
	locked, err = pageLockedFor(request.App, auth.Page, user.Id)
	if err != nil || locked {
		if err == nil {
			err = pageLockedError()
		}
		return placeSearchAccessError(request, err)
	}
	settings, err = loadAdministratorSettings(request.App)
	if err != nil {
		return placeSearchRouteError(request, failedBecause(errPlaceSearchUnavailable, err))
	}
	current, err := effectivePlaceSearchConfig(request.App, settings)
	if err != nil || current != config {
		return placeSearchRouteError(request, failedBecause(errPlaceSearchUnavailable, err))
	}
	request.Response.Header().Set("Cache-Control", "no-store")
	return request.JSON(http.StatusOK, output)
}

func placeSearchRouteError(event *core.RequestEvent, err error) error {
	status, code, message := http.StatusBadGateway, errorCodePlaceSearchFailed, "Couldn't search places. Try again."
	// Its settings unread because the database stayed busy, it is the
	// server's failure, which the error writer tells busy (api_error_busy.go),
	// not a search left unconfigured.
	if isSQLiteBusyError(err) {
		return apiErrorFrom(event, http.StatusInternalServerError, code, message, err)
	}
	if errors.Is(err, errPlaceSearchUnavailable) {
		status, code, message = http.StatusServiceUnavailable, errorCodePlaceSearchUnavailable, "Place search isn't configured on this server."
	}
	if errors.Is(err, errPlaceSearchRateLimited) {
		status, code, message = http.StatusTooManyRequests, errorCodePlaceSearchRateLimited, "Wait a moment before searching again."
	}
	return apiError(event, status, code, message)
}

func placeSearchAccessError(event *core.RequestEvent, err error) error {
	if isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "This page isn't available for editing.")
	}
	return writeGraphError(event, err, "place-search-access", errorCodePlaceSearchFailed, "Couldn’t read this page.")
}
