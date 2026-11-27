package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// The native API takes an object by the IDs the MCP tools and the
// Notion-compatible API give it, as one object has one ID there (Notion's
// database, its page and its block share one; seen 2026-11-16: the ID
// notion-fetch gave a full-page database reached no native route):
//
//   - a UUID without its dashes, as every address names one (/p/<32 hex>,
//     notionMentionID), is the UUID with them;
//   - a database's own ID (databaseOwnPublicID), or the identity made for the
//     API it answered with before, names the page it is on in a /pages/{id}
//     route: a full-page database's page for every route, as that page is
//     the database, and the page holding a database in a page for its
//     /pages/{id}/databases/… routes — the rest of that page's routes act on
//     the whole page, which the database's ID doesn't name;
//   - a data source's or a view's identity made for the API is its own ID;
//     a data source's place takes every ID the tools give for it, a
//     database's that shows one data source too
//     (native_api_data_source_id_aliases.go).
//
// A path naming a page by its own ID is left as it is, and the key and
// session checks after this see the ID the route takes. A JSON body's own
// fields of the same names are resolved the same way
// (native_api_body_id_aliases.go), and a person's ID in the path, the query
// or the body as the Leafen ID (native_api_person_id_aliases.go).

// nativeIDAliasWildcards are the route wildcards that name an object by an
// ID an MCP tool or the Notion-compatible API gives.
var nativeIDAliasWildcards = map[string]string{
	"id": "", "pageId": "", "sourcePageId": "", "blockId": "", "rowId": "",
	"databaseId": notionObjectIdentityDatabase, "containerId": notionObjectIdentityDatabase,
	"dataSourceId": notionObjectIdentityDataSource, "viewId": notionObjectIdentityView,
}

func registerNativeIDAliases(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenNativeIDAliases", Priority: apis.DefaultLoadAuthTokenMiddlewarePriority, Func: resolveNativeIDAliases,
	})
}

func resolveNativeIDAliases(event *core.RequestEvent) error {
	request := event.Request
	pattern := request.Pattern
	if _, path, method := strings.Cut(pattern, " "); method {
		pattern = path
	}
	if !strings.HasPrefix(request.URL.Path, apiRoot+"/") || !strings.HasPrefix(pattern, apiRoot+"/") {
		return event.Next()
	}
	// And in the query and the body (native_api_body_id_aliases.go).
	resolveNativeQueryIDAliases(event)
	resolveNativeBodyIDAliases(event)
	patternSegments := strings.Split(strings.Trim(pattern, "/"), "/")
	pathSegments := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(patternSegments) != len(pathSegments) {
		return event.Next()
	}
	// apiRoot's own segments come first: api, leafen, v1.
	rootSegments := len(strings.Split(strings.Trim(apiRoot, "/"), "/"))
	changed := false
	for index, segment := range patternSegments {
		name, wildcard := strings.CutPrefix(segment, "{")
		name = strings.TrimSuffix(name, "}")
		kind, aliased := nativeIDAliasWildcards[name]
		person := nativePersonIDWildcards[name]
		if !wildcard || !aliased && !person {
			continue
		}
		value := nativeIDWithDashes(pathSegments[index])
		switch {
		case person:
			value = nativePersonID(pathSegments[index])
		case index == rootSegments+1 && patternSegments[rootSegments] == "pages":
			underDatabases := index+1 < len(patternSegments) && patternSegments[index+1] == "databases"
			value = nativeDatabasePage(event.App, value, underDatabases)
		case nativeDataSourceIDWildcards[name]:
			value = nativeDataSourceID(event.App, pathSegments[index])
		case kind != "":
			value = nativeOwnID(event.App, kind, value)
		}
		if value != pathSegments[index] {
			request.SetPathValue(name, value)
			pathSegments[index] = value
			changed = true
		}
	}
	if changed {
		request.URL.Path = "/" + strings.Join(pathSegments, "/")
		request.URL.RawPath = ""
	}
	return event.Next()
}

// nativeIDWithDashes is a UUID written without its dashes, as an address
// names one, with them; any other ID as it is.
func nativeIDWithDashes(value string) string {
	if len(value) != 32 {
		return value
	}
	dashed, uuid := normalizeNotionUUIDRequest(strings.ToLower(value))
	if !uuid {
		return value
	}
	return dashed
}

// nativeDatabasePage is the page a /pages/{id} route takes for an ID: the
// page itself, or the page a database its ID names is on (see above).
func nativeDatabasePage(app core.App, id string, underDatabases bool) string {
	if _, uuid := normalizeNotionUUIDRequest(id); !uuid {
		return id
	}
	containerID := nativeOwnID(app, notionObjectIdentityDatabase, id)
	holder := struct {
		PageID string `db:"page_id"`
		Kind   string `db:"kind"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[page_id]], [[page]].[[kind]]
		FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[container]].[[page_id]]
		WHERE [[container]].[[id]] = {:container}
			AND NOT EXISTS (SELECT 1 FROM {{leafen_pages}} WHERE [[id]] = {:id})
	`).Bind(dbx.Params{"container": containerID, "id": id}).One(&holder)
	if err != nil || holder.PageID == "" {
		// A data source names the page it is on for the page's database
		// routes (native_api_data_source_id_aliases.go).
		if underDatabases {
			if _, err := app.FindRecordById(pagesCollection, id); err != nil {
				if pageID := nativeDataSourcePage(app, id); pageID != "" {
					return pageID
				}
			}
		}
		return id
	}
	if holder.Kind == pageKindDatabase || underDatabases {
		return holder.PageID
	}
	return id
}

// nativeOwnID is the object's own ID for an identity made for the API
// (UUIDv8) that names one of a kind; any other ID as it is.
func nativeOwnID(app core.App, kind, id string) string {
	if len(id) != 36 || id[14] != '8' {
		return id
	}
	identity, err := resolveNotionObjectIdentity(app, kind, id)
	if err != nil || identity.InternalID == "" {
		return id
	}
	return identity.InternalID
}
