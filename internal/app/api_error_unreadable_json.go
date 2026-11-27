package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// A body sent as JSON that doesn't read as a JSON object or list is refused
// in one form on every API route, as Notion's API refuses it (invalid_json,
// "Error parsing JSON body."; seen 2026-11-16 with no token, a string body
// and trailing text alike): 400 LEAFEN_INVALID_JSON with Notion's words and
// no fields, where each route answered its own words, such as "Check the
// sign-in details.", naming nothing (2026-11-16, an agent calling every
// operation). A body up to apiErrorBodyBytes is read before the route, so
// its sign-in and lookups don't answer first, as Notion's don't; a longer
// one the route read is told so when the route refuses it with a 400
// (writeAPIErrorWithReference).

const errorCodeInvalidJSON = "LEAFEN_INVALID_JSON"

// refuseAPIUnreadableJSON refuses, before its route, a request whose JSON
// body up to apiErrorBodyBytes doesn't read; the route reads it again as
// sent.
func refuseAPIUnreadableJSON(event *core.RequestEvent) (bool, error) {
	request := event.Request
	rereader, rereads := request.Body.(router.Rereader)
	if !apiBodySentAsJSON(request) || !rereads || request.ContentLength <= 0 || request.ContentLength > apiErrorBodyBytes {
		return false, nil
	}
	raw, err := io.ReadAll(request.Body)
	rereader.Reread()
	if err != nil || apiJSONBodyReads(raw) {
		return false, nil
	}
	return true, apiError(event, http.StatusBadRequest, errorCodeInvalidJSON, notionInvalidJSONMessage)
}

// apiBodyUnreadableJSON says whether a request's body sent as JSON, as its
// route read it, doesn't read: whole, read again once the route read it to
// its end, or else already broken in what was kept of it (api_error_form.go
// keeps up to apiErrorBodyBytes).
func apiBodyUnreadableJSON(event *core.RequestEvent) bool {
	if event == nil || event.Request == nil {
		return false
	}
	request := event.Request
	if !strings.HasPrefix(request.URL.Path, apiRoot+"/") || !apiBodySentAsJSON(request) {
		return false
	}
	kept, keeps := event.Get(apiBodyKeptKey).(*apiBodyKept)
	switch {
	case !keeps || len(kept.read) == 0:
		return false
	case kept.ended && len(kept.read) <= apiErrorBodyBytes:
		return !apiJSONBodyReads(kept.read)
	case kept.ended:
		// Read to its end, the body starts over as it was sent.
		raw, err := io.ReadAll(request.Body)
		return err == nil && !apiJSONBodyReads(raw)
	}
	if trimmed := bytes.TrimSpace(kept.read); len(trimmed) == 0 || trimmed[0] != '{' && trimmed[0] != '[' {
		return len(trimmed) > 0
	}
	decoder := json.NewDecoder(bytes.NewReader(kept.read))
	for {
		if _, err := decoder.Token(); err != nil {
			var syntax *json.SyntaxError
			return errors.As(err, &syntax)
		}
	}
}

// apiBodySentAsJSON is a request with a body whose type is JSON in UTF-8.
func apiBodySentAsJSON(request *http.Request) bool {
	if request.Body == nil || request.Body == http.NoBody {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	charset := strings.ToLower(params["charset"])
	return err == nil && mediaType == "application/json" && (charset == "" || charset == "utf-8")
}

// apiJSONBodyReads says whether a body reads as a JSON object or list, or
// is empty.
func apiJSONBodyReads(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(trimmed)
}
