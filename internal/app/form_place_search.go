package app

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A form's place question searches by name where an administrator has given
// the server a place search provider, as the database's own place cells do;
// without one it picks on the map alone. Its respondent — a member, or
// anyone with a link to a form open on the web — searches within the form's
// own reach: the form's token and a visible place question of it, checked
// as a file sent to it is (authorizeFormQuestion), again once the provider
// answers; a few searches a minute for each respondent, on top of the
// provider's one a second for the whole server.

const (
	formPlaceSearchPath           = formPath + "/places/search"
	formPlaceSearchWindow         = time.Minute
	formPlaceSearchesPerWindow    = 20
	maxFormPlaceSearchRequestBody = 4096
)

type formPlaceSearchInput struct {
	Token      string `json:"token"`
	PropertyID string `json:"propertyId"`
	Query      string `json:"query"`
}

// formPlaceSearchPace keeps each respondent (or address on the web) of a
// form to a few searches a minute.
type formPlaceSearchPace struct {
	mu       sync.Mutex
	searches map[string][]time.Time
}

var formPlaceSearchPaces = &formPlaceSearchPace{searches: map[string][]time.Time{}}

func (pace *formPlaceSearchPace) admit(key string, now time.Time) bool {
	pace.mu.Lock()
	defer pace.mu.Unlock()
	kept := pace.searches[key][:0]
	for _, at := range pace.searches[key] {
		if now.Sub(at) < formPlaceSearchWindow {
			kept = append(kept, at)
		}
	}
	if len(kept) >= formPlaceSearchesPerWindow {
		pace.searches[key] = kept
		return false
	}
	pace.searches[key] = append(kept, now)
	if len(pace.searches) > 4096 {
		for other, times := range pace.searches {
			if len(times) == 0 || now.Sub(times[len(times)-1]) >= formPlaceSearchWindow {
				delete(pace.searches, other)
			}
		}
	}
	return true
}

func registerFormPlaceSearchRoute(event *core.ServeEvent) {
	provider := placeSearchProviderFor(event.App)
	event.Router.POST(apiRoot+formPlaceSearchPath, func(request *core.RequestEvent) error {
		return serveFormPlaceSearch(request, provider)
	}).Bind(apis.BodyLimit(maxFormPlaceSearchRequestBody))
}

func serveFormPlaceSearch(event *core.RequestEvent, provider *placeSearchProvider) error {
	input := formPlaceSearchInput{}
	if event.BindBody(&input) != nil {
		return writeFormError(event, errFormInvalid, "place-search")
	}
	input.Token, input.PropertyID, input.Query = strings.TrimSpace(input.Token), strings.TrimSpace(input.PropertyID), strings.TrimSpace(input.Query)
	if input.Query == "" {
		return refuseMissingInput(event, errorCodePlaceSearchInvalid, "query", "the words to look places up by, as a map's search box takes them.")
	}
	if !validPlaceSearchQuery(input.Query) {
		return apiError(event, http.StatusBadRequest, errorCodePlaceSearchInvalid, "Check the place search.")
	}
	grant, err := authorizeFormQuestion(event, input.Token, input.PropertyID, graphValueTypePlace)
	if err != nil {
		return writeFormError(event, err, "place-search")
	}
	if !requestLimitProfileIsDevelopment(os.Getenv(requestLimitProfileEnvironment)) &&
		!formPlaceSearchPaces.admit(grant.paceKey(event), time.Now()) {
		return placeSearchRouteError(event, errPlaceSearchRateLimited)
	}
	config, err := formPlaceSearchConfig(event.App)
	if err != nil {
		return placeSearchRouteError(event, err)
	}
	output, err := provider.search(event.Request.Context(), config, input.Query)
	if err != nil {
		return placeSearchRouteError(event, err)
	}
	// The form may have closed, or the provider changed, while it answered.
	if _, err := authorizeFormQuestion(event, input.Token, input.PropertyID, graphValueTypePlace); err != nil {
		return writeFormError(event, err, "place-search")
	}
	if current, err := formPlaceSearchConfig(event.App); err != nil || current != config {
		return placeSearchRouteError(event, failedBecause(errPlaceSearchUnavailable, err))
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, output)
}

// formPlaceSearchConfig is the provider an administrator gave the server.
func formPlaceSearchConfig(app core.App) (placeSearchConfig, error) {
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		return placeSearchConfig{}, failedBecause(errPlaceSearchUnavailable, err)
	}
	config, err := effectivePlaceSearchConfig(app, settings)
	if err != nil {
		return placeSearchConfig{}, failedBecause(errPlaceSearchUnavailable, err)
	}
	return config, nil
}
