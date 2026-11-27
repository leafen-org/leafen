package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A picture on another site, read for the person copying it to the clipboard
// (web lib/copyImage.ts). A site that gives other sites no leave to read its
// pictures (no CORS) still shows them on a page, but the page can't read one
// to copy it, so it asks here, as Notion's web client asks its own server
// (seen 2026-11-15: its "이미지 복사" of a google.com picture with no such
// leave fetched app.notion.com/image/<address> and was copied). Only a person
// signed in asks; the address goes through the same public-only guards as a
// link read (url_metadata_fetch.go, outbound_public_network_policy.go): no
// private, loopback, link-local or shared address, checked again at each
// connection and each redirect, the person's reads held to a few at once
// (outbound_fetch_gate.go). The answer is a picture's bytes alone, at most
// 20 MB, sent as the kind they sniff as; an SVG, which can hold scripts, and
// anything that is no picture are refused.

const (
	outsideImagePath     = "/outside-image"
	outsideImageMaxBytes = 20 << 20
	outsideImageTimeout  = 30 * time.Second
)

func registerOutsideImageRoutes(event *core.ServeEvent) {
	client := newURLMetadataHTTPClient()
	client.Timeout = outsideImageTimeout
	event.Router.GET(apiRoot+outsideImagePath, func(request *core.RequestEvent) error {
		return readOutsideImage(request, client)
	})
}

func readOutsideImage(event *core.RequestEvent, client *http.Client) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	current, err := normalizeURLMetadataURL(event.Request.URL.Query().Get("url"))
	if err != nil {
		return urlMetadataRouteError(event, err)
	}
	release, refused, admitted := admitOutboundFetch(event, user.Id)
	if !admitted {
		return refused
	}
	defer release()
	ctx, cancel := context.WithTimeout(event.Request.Context(), outsideImageTimeout)
	defer cancel()
	for redirects := 0; ; redirects++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return urlMetadataRouteError(event, errURLMetadataInvalidURL)
		}
		request.Header.Set("Accept", "image/*")
		request.Header.Set("User-Agent", "LeafenBot/1.0")
		response, err := client.Do(request)
		if err != nil {
			return urlMetadataRouteError(event, classifyURLMetadataFetchError(ctx, err))
		}
		if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
			location := strings.TrimSpace(response.Header.Get("Location"))
			_ = response.Body.Close()
			if redirects >= urlMetadataMaxRedirects {
				return urlMetadataRouteError(event, errURLMetadataRedirectLimit)
			}
			if current, err = resolveURLMetadataRedirect(current, location); err != nil || location == "" {
				return urlMetadataRouteError(event, errURLMetadataFetchFailed)
			}
			continue
		}
		picture, kind, err := readOutsidePicture(response)
		if err != nil {
			return urlMetadataRouteError(event, err)
		}
		header := event.Response.Header()
		header.Set("Cache-Control", "private, no-store")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		return event.Blob(http.StatusOK, kind, picture)
	}
}

// readOutsidePicture reads an answer's picture and the kind it sniffs as.
func readOutsidePicture(response *http.Response) ([]byte, string, error) {
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return nil, "", errURLMetadataNotFound
	}
	if response.StatusCode != http.StatusOK {
		return nil, "", errURLMetadataFetchFailed
	}
	if response.ContentLength > outsideImageMaxBytes {
		return nil, "", errURLMetadataResponseTooLarge
	}
	picture, err := io.ReadAll(io.LimitReader(response.Body, outsideImageMaxBytes+1))
	if err != nil {
		return nil, "", errURLMetadataFetchFailed
	}
	if len(picture) > outsideImageMaxBytes {
		return nil, "", errURLMetadataResponseTooLarge
	}
	kind := http.DetectContentType(picture)
	if !strings.HasPrefix(kind, "image/") || strings.HasPrefix(kind, "image/svg") {
		return nil, "", refuse(errURLMetadataFetchFailed, "the address holds no picture")
	}
	return picture, kind, nil
}
