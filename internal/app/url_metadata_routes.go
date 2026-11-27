package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerURLMetadataRoutes(event *core.ServeEvent, service *urlMetadataService) {
	event.Router.POST(apiRoot+urlMetadataPath, func(request *core.RequestEvent) error {
		return readURLMetadata(request, service)
	}).Bind(apis.BodyLimit(urlMetadataMaxRequestBodyBytes))
	event.Router.POST(apiRoot+urlMetadataPdfPath, readLinkedPdf).Bind(apis.BodyLimit(urlMetadataMaxRequestBodyBytes))
	registerOutsideImageRoutes(event)
}

func readURLMetadata(event *core.RequestEvent, service *urlMetadataService) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := urlMetadataRequest{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeURLMetadataInvalidURL,
			refusalMessage("Check the public HTTP(S) URL.", bodyRefusalReason("URL to read", err)))
	}
	if strings.TrimSpace(input.URL) == "" {
		return refuseMissingInput(event, errorCodeURLMetadataInvalidURL, "url", "the public http or https address to read.")
	}
	release, refused, admitted := admitOutboundFetch(event, user.Id)
	if !admitted {
		return refused
	}
	defer release()
	read := service.Fetch
	if input.VideoSize {
		read = service.FetchVideoSize
	}
	metadata, err := read(event.Request.Context(), input.URL)
	if err != nil {
		return urlMetadataRouteError(event, err)
	}
	return event.JSON(http.StatusOK, urlMetadataResponse{Metadata: metadata})
}

func urlMetadataRouteError(event *core.RequestEvent, err error) error {
	status := http.StatusBadGateway
	code := errorCodeURLMetadataFetchFailed
	message := "Couldn't read the URL metadata."
	switch {
	case errors.Is(err, errURLMetadataInvalidURL):
		status, code, message = http.StatusBadRequest, errorCodeURLMetadataInvalidURL, refusalMessage("Check the public HTTP(S) URL.", refusalReason(err))
	case errors.Is(err, errURLMetadataUnsafeDestination):
		status, code, message = http.StatusBadRequest, errorCodeURLMetadataUnsafeDestination, refusalMessage("This URL can't be requested safely.", refusalReason(err))
	case errors.Is(err, errURLMetadataRedirectLimit):
		code, message = errorCodeURLMetadataRedirectLimit, "The URL redirected too many times."
	case errors.Is(err, errURLMetadataResponseTooLarge):
		code, message = errorCodeURLMetadataResponseTooLarge, "The URL response exceeded the allowed size."
	case errors.Is(err, errURLMetadataNotFound):
		code, message = errorCodeURLMetadataNotFound, "Nothing was found at the URL."
	case errors.Is(err, errURLMetadataParserLimit):
		code, message = errorCodeURLMetadataParserLimit, "Couldn't safely analyze the URL document."
	case errors.Is(err, errURLMetadataTimeout):
		status, code, message = http.StatusGatewayTimeout, errorCodeURLMetadataTimeout, "The URL request timed out."
	case errors.Is(err, context.Canceled):
		status = http.StatusServiceUnavailable
	}
	return apiErrorWithCause(event, status, code, message, "url-metadata", "fetch", code, err)
}
