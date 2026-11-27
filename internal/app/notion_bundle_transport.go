package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A bundle another tool made comes in as a workspace of its own, which the
// migration engine reads as it reads Notion — through Leafen's own Notion
// API, in this process. The engine asks in Notion's 2022-06-28 shapes;
// Leafen serves its current version. This transport sends each request to
// Leafen's router at the current version and gives the engine a database
// as it was in 2022: its first data source's properties on the database,
// its rows queried through that source, and a row's parent the database.
//
// Leafen's Notion IDs for databases, data sources, rows, row blocks, views
// and comments carry their workspace, so the tool's next bundle, in a new
// workspace, would show new ones; the transport shows the engine IDs made
// from the migration's connection instead, which stay, and turns them back
// as the engine asks. Files the engine fetches come from the router too.

// The bundle's own address, on a name no one can hold (2026-10-19: its
// former name could have been registered one day).
const notionBundleOrigin = "http://leafen-bundle.invalid"

// notionBundleFormerOrigin is the address the files of a migration begun
// before then carry; they still come through the router.
const notionBundleFormerOrigin = "http://leafen.bundle"

var (
	notionBundleDatabasePath = regexp.MustCompile(`^/v1/databases/([^/]+)$`)
	notionBundleQueryPath    = regexp.MustCompile(`^/v1/databases/([^/]+)/query$`)
)

type notionBundleTransport struct {
	ids notionBundleIDs
}

type notionBundleIDs struct {
	app          core.App
	connectionID string
	stagingID    string
}

func (transport notionBundleTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	legacy := request.Header.Get("Notion-Version") == "2022-06-28"
	var body []byte
	if request.Body != nil {
		read, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = read
	}
	ids := transport.ids
	path, query, body, err := ids.inbound(request.URL.Path, request.URL.Query(), body)
	if err != nil {
		return nil, err
	}
	status, header, answer, err := notionBundleServe(request, legacy, path, query, body)
	if err != nil {
		return nil, err
	}
	if status < 300 {
		if legacy {
			answer = notionBundleLegacyParents(answer)
			// A staged workspace's roots are its top pages; the rest come
			// with them.
			if request.Method == http.MethodPost && path == "/v1/search" {
				answer = notionBundleTopResults(answer)
			}
		}
		if answer, err = ids.outbound(answer); err != nil {
			return nil, err
		}
	}
	return notionBundleResponse(request, status, header, answer), nil
}

func notionBundleServe(request *http.Request, legacy bool, path, query string, body []byte) (int, http.Header, []byte, error) {
	if legacy && request.Method == http.MethodGet {
		if match := notionBundleDatabasePath.FindStringSubmatch(path); match != nil {
			return notionBundleLegacyDatabase(request, match[1])
		}
	}
	if legacy && request.Method == http.MethodPost {
		if match := notionBundleQueryPath.FindStringSubmatch(path); match != nil {
			source, status, header, answer, err := notionBundleFirstSource(request, match[1])
			if source == "" || err != nil {
				return status, header, answer, err
			}
			path = "/v1/data_sources/" + source + "/query"
		}
	}
	return notionBundleCall(request, request.Method, path, query, body)
}

// notionBundleCall answers one request from Leafen's router at the
// version it serves.
func notionBundleCall(original *http.Request, method, path, query string, body []byte) (int, http.Header, []byte, error) {
	mux, _, err := nativeOperationRuntime()
	if err != nil {
		return 0, nil, nil, err
	}
	target := path
	if query != "" {
		target += "?" + query
	}
	inner := notionBundleInnerRequest(original.Context(), method, target, body)
	inner.Header.Set("Authorization", original.Header.Get("Authorization"))
	inner.Header.Set("Notion-Version", notionCurrentAPIVersion)
	if len(body) > 0 {
		inner.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, inner)
	return recorder.Code, recorder.Header(), recorder.Body.Bytes(), nil
}

// notionBundleInnerRequest is the server's own reading from its router
// (request_limits_inner.go), as if at the bundle's own origin, its target
// escaped as every request to it from this process is (inner_request_target.go).
func notionBundleInnerRequest(ctx context.Context, method, target string, body []byte) *http.Request {
	inner := httptest.NewRequestWithContext(withServerOwnRead(withServerInnerRequest(ctx)), method, innerRequestTarget(target), bytes.NewReader(body))
	inner.RemoteAddr = "127.0.0.1:0"
	inner.Host = strings.TrimPrefix(notionBundleOrigin, "http://")
	return inner
}

func notionBundleResponse(request *http.Request, status int, header http.Header, body []byte) *http.Response {
	if header == nil {
		header = http.Header{"Content-Type": {"application/json"}}
	}
	header.Del("Content-Length")
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: header, Body: io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)), Request: request, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1}
}

// notionBundleFirstSource is a database's first data source, or the answer
// that says why there is none.
func notionBundleFirstSource(request *http.Request, databaseID string) (string, int, http.Header, []byte, error) {
	status, header, answer, err := notionBundleCall(request, http.MethodGet, "/v1/databases/"+databaseID, "", nil)
	if err != nil || status >= 300 {
		return "", status, header, answer, err
	}
	database := struct {
		DataSources []struct {
			ID string `json:"id"`
		} `json:"data_sources"`
	}{}
	if json.Unmarshal(answer, &database) != nil || len(database.DataSources) == 0 {
		return "", http.StatusNotFound, nil,
			[]byte(`{"object":"error","status":404,"code":"object_not_found","message":"The database has no data source."}`), nil
	}
	return database.DataSources[0].ID, 0, nil, nil, nil
}

// notionBundleLegacyDatabase is a database as Notion gave it in 2022: the
// database with its first data source's properties.
func notionBundleLegacyDatabase(request *http.Request, databaseID string) (int, http.Header, []byte, error) {
	status, header, answer, err := notionBundleCall(request, http.MethodGet, "/v1/databases/"+databaseID, "", nil)
	if err != nil || status >= 300 {
		return status, header, answer, err
	}
	database := map[string]any{}
	if err := json.Unmarshal(answer, &database); err != nil {
		return 0, nil, nil, err
	}
	source, status, header, failure, err := notionBundleFirstSource(request, databaseID)
	if source == "" || err != nil {
		return status, header, failure, err
	}
	status, header, sourceAnswer, err := notionBundleCall(request, http.MethodGet, "/v1/data_sources/"+source, "", nil)
	if err != nil || status >= 300 {
		return status, header, sourceAnswer, err
	}
	dataSource := map[string]any{}
	if err := json.Unmarshal(sourceAnswer, &dataSource); err != nil {
		return 0, nil, nil, err
	}
	database["object"] = "database"
	database["properties"] = dataSource["properties"]
	delete(database, "data_sources")
	encoded, err := json.Marshal(notionBundleLegacyValue(database))
	if err != nil {
		return 0, nil, nil, err
	}
	return http.StatusOK, nil, encoded, nil
}

// notionBundleLegacyParents writes parents and objects as 2022 named them.
func notionBundleLegacyParents(answer []byte) []byte {
	var value any
	if json.Unmarshal(answer, &value) != nil {
		return answer
	}
	encoded, err := json.Marshal(notionBundleLegacyValue(value))
	if err != nil {
		return answer
	}
	return encoded
}

func notionBundleLegacyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		if typed["type"] == "data_source_id" {
			if databaseID, ok := typed["database_id"].(string); ok && databaseID != "" {
				return map[string]any{"type": "database_id", "database_id": databaseID}
			}
		}
		for key, child := range typed {
			typed[key] = notionBundleLegacyValue(child)
		}
		// A page's or database's properties keep Notion's order when written
		// again (notion_property_order.go).
		if properties, ok := typed["properties"].(map[string]any); ok {
			switch typed["object"] {
			case "page", "database", "data_source":
				typed["properties"] = notionPropertyObjects(properties)
			}
		}
		return typed
	case []any:
		for index, child := range typed {
			typed[index] = notionBundleLegacyValue(child)
		}
		return typed
	}
	return value
}

// notionBundleTopResults keeps the search results at a workspace's top.
func notionBundleTopResults(answer []byte) []byte {
	page := map[string]any{}
	if json.Unmarshal(answer, &page) != nil {
		return answer
	}
	results, _ := page["results"].([]any)
	kept := []any{}
	for _, result := range results {
		object, _ := result.(map[string]any)
		parent, _ := object["parent"].(map[string]any)
		if object["object"] == "page" && parent["type"] == "workspace" {
			kept = append(kept, object)
		}
	}
	page["results"] = kept
	encoded, err := json.Marshal(page)
	if err != nil {
		return answer
	}
	return encoded
}

// inbound turns the IDs in a request the engine makes into the staged
// workspace's.
func (ids notionBundleIDs) inbound(path string, query url.Values, body []byte) (string, string, []byte, error) {
	var value any
	hasBody := len(body) > 0 && json.Unmarshal(body, &value) == nil
	candidates := []string{}
	segments := strings.Split(path, "/")
	candidates = append(candidates, segments...)
	for _, values := range query {
		candidates = append(candidates, values...)
	}
	if hasBody {
		notionBundleStrings(value, func(text string) string { candidates = append(candidates, text); return text })
	}
	staged, err := ids.stagedIDs(candidates)
	if err != nil || len(staged) == 0 {
		return path, query.Encode(), body, err
	}
	swap := func(text string) string {
		if normalized, ok := normalizeNotionUUIDRequest(text); ok {
			if public, found := staged[normalized]; found {
				return public
			}
		}
		return text
	}
	for index, segment := range segments {
		segments[index] = swap(segment)
	}
	for key, values := range query {
		for index, value := range values {
			values[index] = swap(value)
		}
		query[key] = values
	}
	if hasBody {
		if body, err = json.Marshal(notionBundleStrings(value, swap)); err != nil {
			return "", "", nil, err
		}
	}
	return strings.Join(segments, "/"), query.Encode(), body, nil
}

// outbound turns the staged workspace's IDs in an answer into the ones the
// engine keeps, and Leafen's file addresses into the bundle's.
func (ids notionBundleIDs) outbound(answer []byte) ([]byte, error) {
	var value any
	if json.Unmarshal(answer, &value) != nil {
		return answer, nil
	}
	candidates := []string{}
	notionBundleStrings(value, func(text string) string { candidates = append(candidates, text); return text })
	shown, err := ids.shownIDs(candidates)
	if err != nil {
		return nil, err
	}
	own, _ := publicServerOrigin(notionBundleInnerRequest(context.Background(), http.MethodGet, "/", nil))
	files := own + apiRoot + strings.Replace(fileTransferDownloadCapabilityPath, "{token}", "", 1)
	value = notionBundleStrings(value, func(text string) string {
		if normalized, ok := normalizeNotionUUIDRequest(text); ok {
			if presented, found := shown[normalized]; found {
				return presented
			}
		}
		if own != notionBundleOrigin && strings.HasPrefix(text, files) {
			return notionBundleOrigin + strings.TrimPrefix(text, own)
		}
		return text
	})
	if err := ids.versions(value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// notionBundleStrings gives each string in a JSON value to change.
func notionBundleStrings(value any, change func(string) string) any {
	switch typed := value.(type) {
	case string:
		return change(typed)
	case map[string]any:
		for key, child := range typed {
			typed[key] = notionBundleStrings(child, change)
		}
		return typed
	case []any:
		for index, child := range typed {
			typed[index] = notionBundleStrings(child, change)
		}
		return typed
	}
	return value
}

func notionBundleUUIDs(candidates []string) []string {
	seen, found := map[string]bool{}, []string{}
	for _, candidate := range candidates {
		if normalized, ok := normalizeNotionUUIDRequest(candidate); ok && !seen[normalized] {
			seen[normalized] = true
			found = append(found, normalized)
		}
	}
	return found
}

// notionBundleIDBatch runs a query over IDs, a few hundred at a time.
func notionBundleIDBatch(ids []string, run func(placeholders string, params dbx.Params) error) error {
	for start := 0; start < len(ids); start += 400 {
		end := min(start+400, len(ids))
		params, names := dbx.Params{}, make([]string, 0, end-start)
		for index, id := range ids[start:end] {
			name := "id" + strconv.Itoa(index)
			params[name] = id
			names = append(names, "{:"+name+"}")
		}
		if err := run(strings.Join(names, ","), params); err != nil {
			return err
		}
	}
	return nil
}

// shownIDs are the IDs the engine keeps for the staged workspace's own,
// by the staged one.
func (ids notionBundleIDs) shownIDs(candidates []string) (map[string]string, error) {
	named := []notionObjectIdentity{}
	err := notionBundleIDBatch(notionBundleUUIDs(candidates), func(placeholders string, params dbx.Params) error {
		rows := []notionObjectIdentity{}
		params["workspace"] = ids.stagingID
		if err := ids.app.DB().NewQuery(`
			SELECT [[public_id]], [[object_kind]], [[workspace_id]], [[internal_id]] FROM {{leafen_notion_object_identities}}
			WHERE [[workspace_id]] = {:workspace} AND [[public_id]] IN (` + placeholders + `)
		`).Bind(params).All(&rows); err != nil {
			return err
		}
		named = append(named, rows...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	rowPages, err := ids.stagedRowPageIdentities(candidates)
	if err != nil {
		return nil, err
	}
	owned, err := ids.stagedOwnIdentities(candidates)
	if err != nil {
		return nil, err
	}
	return ids.presented(append(append(named, rowPages...), owned...))
}

// stagedIDs are the staged workspace's IDs for the ones the engine keeps,
// by the kept one.
func (ids notionBundleIDs) stagedIDs(candidates []string) (map[string]string, error) {
	staged := map[string]string{}
	err := notionBundleIDBatch(notionBundleUUIDs(candidates), func(placeholders string, params dbx.Params) error {
		rows := []struct {
			Presented  string `db:"presented_id"`
			ObjectKind string `db:"object_kind"`
			InternalID string `db:"internal_id"`
		}{}
		params["connection"] = ids.connectionID
		if err := ids.app.DB().NewQuery(`
			SELECT [[presented_id]], [[object_kind]], [[internal_id]] FROM {{leafen_bundle_import_ids}}
			WHERE [[connection_id]] = {:connection} AND [[presented_id]] IN (` + placeholders + `)
		`).Bind(params).All(&rows); err != nil {
			return err
		}
		for _, row := range rows {
			public, err := ensureNotionObjectIdentity(ids.app, row.ObjectKind, ids.stagingID, row.InternalID)
			if err != nil {
				return err
			}
			staged[row.Presented] = public
		}
		return nil
	})
	return staged, err
}

// notionBundleClient reads a staged bundle workspace with a token Leafen
// made for it.
func notionBundleClient(ids notionBundleIDs, token string) notionProviderClient {
	client := newNotionProviderClient(token)
	client.baseURL = notionBundleOrigin
	client.client = &http.Client{Transport: notionBundleTransport{ids: ids}}
	return client
}

// notionSourceClient reads a migration connection's source: Notion, or a
// bundle staged in a workspace of its own. Whoever reads Notion, the import
// screen, an agent's tool or a job, goes at the connection's one pace
// (Notion's three requests a second); a bundle, at its own (paced).
func notionSourceClient(app core.App, connectionID, token string) notionProviderClient {
	client := newNotionProviderClient(token)
	var staging string
	err := app.DB().NewQuery(`SELECT [[staging_workspace_id]] FROM {{leafen_bundle_import_sources}} WHERE [[connection_id]] = {:id}`).
		Bind(dbx.Params{"id": connectionID}).Row(&staging)
	if err == nil {
		client = notionBundleClient(notionBundleIDs{app: app, connectionID: connectionID, stagingID: staging}, token)
	}
	if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok && runner != nil {
		client.admission, client.connectionID = runner.notionProviderAdmission, connectionID
	}
	return client
}

// notionBundleOwnURL says whether a file's address is the bundle's own,
// fetched through the router rather than the network.
func notionBundleOwnURL(raw string) bool {
	return strings.HasPrefix(raw, notionBundleOrigin+"/") || strings.HasPrefix(raw, notionBundleFormerOrigin+"/")
}

// notionBundleFetch fetches a file at the bundle's own address, and the
// name it was uploaded with.
func notionBundleFetch(ctx context.Context, raw string) (*http.Response, string, error) {
	mux, _, err := nativeOperationRuntime()
	if err != nil {
		return nil, "", err
	}
	recorder := httptest.NewRecorder()
	target := strings.TrimPrefix(strings.TrimPrefix(raw, notionBundleOrigin), notionBundleFormerOrigin)
	mux.ServeHTTP(recorder, notionBundleInnerRequest(ctx, http.MethodGet, target, nil))
	response := recorder.Result()
	name := ""
	if _, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition")); err == nil {
		name = params["filename"]
	}
	return response, name, nil
}
