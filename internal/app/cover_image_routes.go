package app

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func registerCoverImageRoutes(event *core.ServeEvent) {
	provider := newUnsplashCoverProvider()
	event.Router.GET(apiRoot+coverImageSearchPath, func(request *core.RequestEvent) error {
		if _, ok := authenticatedUser(request); !ok {
			return apiError(request, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		text := strings.TrimSpace(request.Request.URL.Query().Get("query"))
		page := 1
		if raw := request.Request.URL.Query().Get("page"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed == math.MaxInt {
				return apiError(request, http.StatusBadRequest, errorCodeCoverImageInvalid, "Check the image search.")
			}
			page = parsed
		}
		if len([]rune(text)) > coverImageSearchQueryRunes {
			return apiError(request, http.StatusBadRequest, errorCodeCoverImageInvalid, "Check the image search.")
		}
		output, err := provider.search(request.Request.Context(), text, page)
		if err != nil {
			return coverImageRouteError(request, err)
		}
		request.Response.Header().Set("Cache-Control", "no-store")
		return request.JSON(http.StatusOK, output)
	})
	event.Router.POST(apiRoot+coverImageSelectionPath, func(request *core.RequestEvent) error {
		if _, ok := authenticatedUser(request); !ok {
			return apiError(request, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		id := request.Request.PathValue("photoId")
		if !unsplashPhotoID.MatchString(id) {
			return apiError(request, http.StatusBadRequest, errorCodeCoverImageInvalid, "Check the photo.")
		}
		output, err := provider.selectPhoto(request.Request.Context(), id)
		if err != nil {
			return coverImageRouteError(request, err)
		}
		request.Response.Header().Set("Cache-Control", "no-store")
		return request.JSON(http.StatusOK, output)
	})
}

func coverImageRouteError(event *core.RequestEvent, err error) error {
	status, code, message := http.StatusBadGateway, errorCodeCoverImageFailed, "Couldn't load the photos. Try again."
	if errors.Is(err, errCoverProviderUnavailable) {
		status, code, message = http.StatusServiceUnavailable, errorCodeCoverImageUnavailable, "Image search isn't configured on this server."
	} else if errors.Is(err, errCoverProviderRateLimited) {
		status, code, message = http.StatusTooManyRequests, errorCodeCoverImageRateLimited, "Image search reached its rate limit. Try again later."
	}
	return apiError(event, status, code, message)
}
