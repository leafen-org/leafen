package app

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// A route that takes an object in its JSON body takes it by the same IDs as
// a route that names it in its path (native_api_id_aliases.go): an agent
// gave POST /page-analytics a database's address ID that GET /pages/{id}
// took, and was told the page wasn't found (2026-11-22). The body's own
// top-level fields named as the path's wildcards are resolved by the same
// rules, at the same boundary, before any route reads them: pageId and
// sourcePageId as a /pages/{id}, a row's or block's ID with its dashes, and
// a database's, data source's or view's identity made for the API as its own
// ID — a data source's by every ID the tools give for it, and a view's
// written view://<ID> too (native_api_data_source_id_aliases.go). Those
// fields deeper in the body, and bodies that aren't JSON, are left
// as they are; a person's fields are read at any depth
// (native_api_person_id_aliases.go); a body of any size is read so
// (native_api_body_id_alias_stream.go). A write's mutationId left out, its
// whole body with it, is made there too (native_api_body_mutation_id.go).

// The query's own parameters of the same names are resolved the same way
// (?pageId=, ?dataSourceId=). A person's ID is read in the body's fields
// at any depth and in the query's person filters
// (native_api_person_id_aliases.go).

// resolveNativeQueryIDAliases resolves the IDs a request's query names.
func resolveNativeQueryIDAliases(event *core.RequestEvent) {
	request := event.Request
	if request.URL.RawQuery == "" {
		return
	}
	query := request.URL.Query()
	changed := false
	resolve := func(name string, resolved func(string) string) {
		values := query[name]
		for index, value := range values {
			if value == "" {
				continue
			}
			if id := resolved(value); id != value {
				values[index], changed = id, true
			}
		}
	}
	for name := range nativeBodyIDAliasFields {
		resolve(name, func(value string) string { return nativeAliasFieldID(event.App, name, value) })
	}
	for name := range nativePersonIDQueryNames {
		resolve(name, nativePersonID)
	}
	if changed {
		request.URL.RawQuery = query.Encode()
	}
}

// nativeAliasFieldID is an ID a body field or a query parameter of that name
// gives, as the route takes it.
func nativeAliasFieldID(app core.App, name, value string) string {
	if nativePersonIDFields[name] {
		return nativePersonID(value)
	}
	if name == "dataSourceId" {
		return nativeDataSourceID(app, value)
	}
	if name == "viewId" {
		value = nativeViewID(value)
	}
	resolved := nativeIDWithDashes(value)
	switch kind := nativeBodyIDAliasFields[name]; {
	case name == "pageId" || name == "sourcePageId":
		return nativeDatabasePage(app, resolved, false)
	case kind != "":
		return nativeOwnID(app, kind, resolved)
	}
	return resolved
}

// nativeBodyIDAliasReadAtOnceBytes bounds the bodies read whole here, which
// go on with their length as rewritten; a larger one, or one that doesn't
// say its length, is rewritten as its reader reads it, keeping the length
// it was sent with, which its route's bound judges.
const nativeBodyIDAliasReadAtOnceBytes = 256 << 10

// nativeBodyIDAliasFields are the body fields resolved, with the kind of
// identity each names ("" for a page's or a row's own).
var nativeBodyIDAliasFields = map[string]string{
	"pageId": "", "sourcePageId": "", "rowId": "", "blockId": "",
	"databaseId": notionObjectIdentityDatabase, "containerId": notionObjectIdentityDatabase,
	"dataSourceId": notionObjectIdentityDataSource, "viewId": notionObjectIdentityView,
}

// resolveNativeBodyIDAliases resolves the IDs a request's JSON body names at
// its top level, and the people it names at any depth, putting the body back
// as it reads again.
func resolveNativeBodyIDAliases(event *core.RequestEvent) {
	request := event.Request
	// A write's body: a DELETE's too, which some take (an automation's
	// mutationId and revision).
	if request.Method != http.MethodPost && request.Method != http.MethodPut && request.Method != http.MethodPatch &&
		request.Method != http.MethodDelete {
		return
	}
	if request.Body == nil || request.Body == http.NoBody || request.ContentLength == 0 {
		nativeEmptyBodyMadeMutationID(request)
		return
	}
	if media, _, err := mime.ParseMediaType(request.Header.Get("Content-Type")); err != nil || media != "application/json" {
		return
	}
	scan := nativeBodyAliasScanner{
		resolve:    func(name, value string) string { return nativeAliasFieldID(event.App, name, value) },
		mutationID: nativeBodyMadeMutationID(request),
	}
	if request.ContentLength < 0 || request.ContentLength > nativeBodyIDAliasReadAtOnceBytes {
		reader := &nativeBodyAliasReader{scan: scan}
		if rereadable, ok := request.Body.(*router.RereadableReadCloser); ok {
			reader.source, rereadable.ReadCloser = rereadable.ReadCloser, reader
		} else {
			reader.source, request.Body = request.Body, reader
		}
		return
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, nativeBodyIDAliasReadAtOnceBytes+1))
	_ = request.Body.Close()
	body := raw
	if err == nil && (nativeBodyMayNameID(raw) || scan.mutationID != "") {
		body = scan.flush(scan.feed(raw, make([]byte, 0, len(raw)+64)))
	}
	request.Body = &router.RereadableReadCloser{ReadCloser: io.NopCloser(bytes.NewReader(body))}
	request.ContentLength = int64(len(body))
	request.Header.Set("Content-Length", strconv.Itoa(len(body)))
}
