package app

import (
	"bytes"
	"strings"
)

// The native API takes a person by the IDs the MCP tools and the
// Notion-compatible API give them, as it takes a page (native_api_id_aliases.go):
// notion-get-users and /v1/users give a person's Notion-compatible ID,
// notion-fetch a people value's user://<ID>, and an SQL query's rows
// notion_user-<ID>, while the native routes read the Leafen ID their own
// answers list (GET /workspaces/{id}/members) — seen 2026-11-26, real
// agents: a legal hold on the ID notion-get-users gave was refused three
// times and never made, and a permission group of those IDs was refused as
// "Choose members of this workspace." though both were members. One person
// is one person here as in the Notion tools (mcpDatabasePageUserID takes
// every form), so each of those forms of a Leafen-made person ID, with or
// without its dashes and in any case, reads as the Leafen ID, at the same
// boundary as the page IDs, before any route or key check reads it; a Leafen
// ID, an email, a group's or a workspace's ID, "me" and anything else are
// left as written. The person fields are known by their names:
//
//   - in a JSON body at any depth (a legal hold's scope.userIds, an
//     automation's recipientIds, a title's mention spans' userId), a string
//     or a list of strings;
//   - a query's (?userId, ?actor, ?createdBy, the people filters);
//   - a path's {userId} and {accountId}.

// nativePersonIDFields are the body fields that name people: those named
// for a person, and the plain lists that hold people where they appear — a
// permission group's members to add and remove, the guests to remove (user
// IDs, or email: keys left as written).
var nativePersonIDFields = map[string]bool{
	"userId": true, "userIds": true, "memberId": true, "memberIds": true,
	"principalId": true, "targetUserId": true, "successorUserId": true,
	"recipientIds": true, "owners": true, "actor": true,
	"add": true, "remove": true, "guests": true,
}

// nativePersonIDQueryNames are the query names that filter by a person.
var nativePersonIDQueryNames = map[string]bool{"userId": true, "actor": true, "createdBy": true}

// nativePersonIDWildcards are the route wildcards that name a person.
var nativePersonIDWildcards = map[string]bool{"userId": true, "accountId": true}

// nativePersonID is the Leafen ID of a person a value names by a
// Notion-compatible ID (bare, user://, notion_user-); a link's own ID when
// it names a Leafen ID; any other value as written.
func nativePersonID(value string) string {
	reference := notionPersonReferenceID(value)
	if internal, err := resolveNotionCompatibleUserID(nativeIDWithDashes(strings.ToLower(reference))); err == nil {
		return internal
	}
	if reference != strings.TrimSpace(value) {
		return reference
	}
	return value
}

// nativeBodyMayNameID is false for a body whose text holds no field
// resolved (nativeBodyIDAliasFields, nativePersonIDFields), which then
// passes on unscanned.
func nativeBodyMayNameID(body []byte) bool {
	for _, end := range nativeBodyIDFieldEnds {
		if bytes.Contains(body, end) {
			return true
		}
	}
	return false
}

// nativeBodyIDFieldEnds are how every field resolved ends as written.
var nativeBodyIDFieldEnds = [][]byte{
	[]byte(`Id"`), []byte(`Ids"`), []byte(`owners"`), []byte(`actor"`),
	[]byte(`"add"`), []byte(`"remove"`), []byte(`"guests"`),
}
