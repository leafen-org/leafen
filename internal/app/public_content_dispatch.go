package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerPublicContentRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/public-content", dispatchPublicContentRequest).
		Bind(apis.BodyLimit(maxPublicContentRequestBytes))
	event.Router.GET(apiRoot+"/page-login-wall/{pageId}", servePageLoginWall)
	event.Router.GET(apiRoot+"/form-login-wall/{token}", serveFormLoginWall)
}

func dispatchPublicContentRequest(event *core.RequestEvent) error {
	body, err := io.ReadAll(event.Request.Body)
	if err != nil {
		return publicContentInvalidRequest(event, refuse(errPublicContentInvalid, "the body can't be read"))
	}
	var envelope publicContentActionEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return publicContentInvalidRequest(event, bodyRefusal(errPublicContentInvalid, "public page request", err))
	}

	var response any
	switch envelope.Action {
	case publicContentActionSnapshot:
		var input publicContentSnapshotRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return publicContentInvalidRequest(event, bodyRefusal(errPublicContentInvalid, "public page request", err))
		}
		viewSecret := strings.TrimSpace(event.Request.Header.Get(viewLinkSecretHeader))
		view := strings.TrimSpace(input.SourceKind) == publicContentSourceView
		opener, now := viewLinkOpenerOf(event), time.Now().UTC()
		// A wrong secret is counted before the read, so it stays counted.
		if view {
			err = admitViewLinkSecret(event.App, strings.TrimSpace(input.Token), viewSecret, opener, now)
		}
		if err == nil {
			var snapshot publicContentSnapshotResponse
			snapshot, err = readPublicContentSnapshot(event.App, input, viewSecret)
			response = snapshot
			if err == nil {
				recordPublicContentReads(event.App, snapshot)
			}
		}
		// A view link's page opening is logged once, at its first read.
		if err == nil && view && input.PageID == "" && input.GraphCursor == "" && input.SourceRevision == nil {
			recordViewLinkOpenOf(event.App, strings.TrimSpace(input.Token), opener, now)
		}
	case publicContentActionCheck:
		var input publicContentCheckRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return publicContentInvalidRequest(event, bodyRefusal(errPublicContentInvalid, "public page request", err))
		}
		viewSecret := strings.TrimSpace(event.Request.Header.Get(viewLinkSecretHeader))
		// A wrong secret counts here as at a read, so checking is no way round it.
		if strings.TrimSpace(input.SourceKind) == publicContentSourceView {
			err = admitViewLinkSecret(event.App, strings.TrimSpace(input.Token), viewSecret, viewLinkOpenerOf(event), time.Now().UTC())
		}
		if err == nil {
			response, err = checkPublicContentSource(event.App, input, viewSecret)
		}
	case publicContentActionDiscussions:
		var input publicContentDiscussionsRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return publicContentInvalidRequest(event, bodyRefusal(errPublicContentInvalid, "public page request", err))
		}
		viewSecret := strings.TrimSpace(event.Request.Header.Get(viewLinkSecretHeader))
		// A wrong secret counts here as at a read.
		if strings.TrimSpace(input.SourceKind) == publicContentSourceView {
			err = admitViewLinkSecret(event.App, strings.TrimSpace(input.Token), viewSecret, viewLinkOpenerOf(event), time.Now().UTC())
		}
		if err == nil {
			response, err = readPublicContentDiscussions(event.App, input, viewSecret)
		}
	case publicContentActionSearchSite:
		var input publicContentSearchRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return publicContentInvalidRequest(event, bodyRefusal(errPublicContentInvalid, "public page request", err))
		}
		response, err = searchPublicSiteContent(event.App, input)
	default:
		return publicContentInvalidRequest(event, refuse(errPublicContentInvalid, "action is %s, %s, %s or %s, not %q",
			publicContentActionSnapshot, publicContentActionCheck, publicContentActionDiscussions, publicContentActionSearchSite, envelope.Action))
	}
	if errors.Is(err, errPublicContentInvalid) {
		return publicContentInvalidRequest(event, err)
	}
	if errors.Is(err, errPublicContentNotFound) || errors.Is(err, errPublicVisibilityNotFound) {
		return apiError(event, http.StatusNotFound, errorCodePublicContentNotFound, "Public page not found.")
	}
	if errors.Is(err, errViewLinkSecretRequired) {
		return apiError(event, http.StatusUnauthorized, errorCodeViewLinkSecretRequired, "Enter the view link's secret.")
	}
	if errors.Is(err, errViewLinkSecretWrong) {
		return apiError(event, http.StatusUnauthorized, errorCodeViewLinkSecretWrong, "That isn't the view link's secret.")
	}
	if errors.Is(err, errPublicContentRevisionConflict) {
		return apiError(event, http.StatusConflict, errorCodePublicContentRevisionConflict, "The public page changed. Load it again.")
	}
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodePublicContentFailed,
			"Couldn't load the public page.", "public_content.read",
			"query", errorCodePublicContentFailed, err,
		)
	}
	return event.JSON(http.StatusOK, response)
}

// publicContentInvalidRequest refuses a public page request with what to fix.
func publicContentInvalidRequest(event *core.RequestEvent, err error) error {
	return apiError(event, http.StatusBadRequest, errorCodePublicContentInvalid,
		refusalMessage("Check the public page request.", refusalReason(err)))
}
