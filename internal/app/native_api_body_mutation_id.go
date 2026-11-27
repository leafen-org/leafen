package app

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A write whose body takes a mutationId — the id that makes the same change
// sent twice made once — takes one left out too: the server makes it at the
// API's edge, where the body's IDs are resolved (native_api_body_id_aliases.go),
// before any route reads the body, so no route refuses a change for want of
// one (2026-11-26: an agent writing a row's files through leafen_call didn't
// know the write needed one and was refused). One given is kept and dedupes
// as before; one made is new each call, so a call sent again without one is
// made again. Every route whose body's own record holds a mutationId field
// takes it so — a page's change, its metadata, its content graph, a row's
// values, a database's mutations, automations (deleted too) and starter, an
// organization's exports and legal holds —, sent with a body or none; a
// record only an action of the body reads (POST /forms) keeps asking for it;
// an empty one ("", spaces) is one left out. Nothing says it's needed:
// leafen_operations marks none of them required (native_operation_describe.go),
// and a refusal's fields and words don't name one left out as missing
// (api_error_details.go) — they did, from the body as sent, and an agent's
// row values written with no mutationId over a value changed meanwhile were
// told "Missing what this operation needs: mutationId" in place of the
// change (2026-11-27).

const (
	nativeMadeMutationIDPrefix   = "server-"
	nativeMadeMutationIDLength   = 24
	nativeMadeMutationIDAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// nativeMutationIDField is the field, as every contract and handler names it.
const nativeMutationIDField = "mutationId"

// nativeBodyMadeMutationID is the mutationId the server gives a request's
// body where the body leaves it out: a new one for a route whose body takes
// one, "" for any other.
func nativeBodyMadeMutationID(request *http.Request) string {
	if !nativeBodyTakesMutationID(request) {
		return ""
	}
	return nativeMadeMutationIDPrefix + security.RandomStringWithAlphabet(nativeMadeMutationIDLength, nativeMadeMutationIDAlphabet)
}

// nativeBodyTakesMutationID is whether the route a request matched takes a
// mutationId in its body's own record, by its handler's generated input
// (api_error_details.go).
func nativeBodyTakesMutationID(request *http.Request) bool {
	input, known := apiOperationInputOf(request)
	if !known {
		return false
	}
	for _, field := range input.Records[input.Record] {
		if field.Name == nativeMutationIDField {
			return true
		}
	}
	return false
}

// nativeMutationIDKey is whether a body's key names its mutationId, as the
// body is read: in any case (BindBody matches names so).
func nativeMutationIDKey(key string) bool {
	return strings.EqualFold(key, nativeMutationIDField)
}

// nativeOperationInputsMutationIDOptional marks each body's own mutationId,
// in what its handler reads, as a call needn't send it, as the edge makes
// one left out.
func nativeOperationInputsMutationIDOptional(inputs map[string]nativeOperationInput) {
	for _, input := range inputs {
		fields := input.Records[input.Record]
		for index := range fields {
			if fields[index].Name == nativeMutationIDField {
				fields[index].Required = false
			}
		}
	}
}

// nativeEmptyBodyMadeMutationID gives a write sent with no body, whose route
// takes a mutationId, a JSON body holding the one made, as one that leaves
// its own out is given it: a change with nothing more to say isn't refused
// for want of one, and one that lacks more is refused for what it lacks
// (2026-11-27: a row's values sent with no body were told "mutationId is a
// new id of yours"). One sent as another kind of body than JSON is left as
// it came.
func nativeEmptyBodyMadeMutationID(request *http.Request) {
	if kind := request.Header.Get("Content-Type"); kind != "" {
		if media, _, err := mime.ParseMediaType(kind); err != nil || media != "application/json" {
			return
		}
	}
	made := nativeBodyMadeMutationID(request)
	if made == "" {
		return
	}
	body := []byte(`{"` + nativeMutationIDField + `":"` + made + `"}`)
	request.Body = &router.RereadableReadCloser{ReadCloser: io.NopCloser(bytes.NewReader(body))}
	request.ContentLength = int64(len(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Length", strconv.Itoa(len(body)))
}
