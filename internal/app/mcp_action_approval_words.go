package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What an agent's action waiting for its person reads as — the question
// their agent's program shows, or the agent puts to them in the conversation
// (person_ask.go) — in their words (mcp_action_approval_names.go): what it
// does, what it does it to by name, each value it sets beside what it is now
// where the same path reads it — only those it changes, the rest counted
// (an object's fields one by one, a secret hidden: mcp_action_body_fields.go)
// — and whether it can't be undone. A name is
// told only of what the person reaches themselves (a server's
// administrator, on its administration, everything); anything else stays in
// the request as sent, which follows the words (2026-11-13: a request line
// and JSON alone told the person nothing of what they allowed).

type mcpActionWords struct {
	Action  string            `json:"action"`
	Final   bool              `json:"final"`
	Targets []mcpActionTarget `json:"targets,omitempty"`
	Changes []mcpActionChange `json:"changes,omitempty"`
	// How many fields the request sends as they already are: not listed, so
	// the question shows only what changes (2026-11-13).
	Unchanged int `json:"unchanged,omitzero"`
}

type mcpActionTarget struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type mcpActionChange struct {
	Field  string `json:"field"`
	Before string `json:"before,omitempty"`
	After  string `json:"after"`
}

// Routes whose effect stays once done, beside deleting for good: a session
// ended, a token, an internal connection, a group, an app's secret or the
// credentials given it deleted, the server's data replaced or sent away, an account's cleanup
// finished or the person's own account deleted, a workspace moved out, a
// private page opened to its workspace's owner. Sharing, permissions, members and settings are set back as they
// were set, and the words don't call them final.
var mcpActionsFinal = map[string]bool{
	"POST /admin/accounts/{accountId}/sessions/revoke": true, "POST /admin/accounts/{accountId}/sessions/{sessionId}/revoke": true,
	"POST /admin/accounts/{accountId}/cleanup/{jobId}/commit": true, "POST /admin/backups/replica/restore": true,
	"PUT /admin/backups/standby/join": true, "POST /admin/backups/standby/takeover": true, "POST /admin/server-move/out": true,
	"POST /admin/workspaces/{workspaceId}/pages/{pageId}/take-over": true, "POST /workspaces/{workspaceId}/move-out": true,
	"POST /organizations/{organizationId}/managed-users/logout": true, "POST /organizations/{organizationId}/managed-users/{userId}/logout": true,
	"POST /account/deletion":                              true,
	"DELETE /admin/apps/installs/{installId}/credentials": true, "DELETE /admin/apps/installs/{installId}/secrets/{name}": true,
	"POST /workspaces/{workspaceId}/personal-access-tokens/{tokenId}/revoke": true, "POST /organizations/{organizationId}/notion-admin-tokens/{tokenId}/revoke": true,
	"DELETE /workspaces/{workspaceId}/groups/{groupId}": true, "DELETE /workspaces/{workspaceId}/internal-connections/{connectionId}": true,
}

// mcpActionFinalBodyActions are the body actions of a route that can't be
// undone: an organization's group deleted.
var mcpActionFinalBodyActions = map[string][]string{
	"POST /organizations/{organizationId}/settings-mutations": {"group_delete"},
}

var mcpActionFamilyNames = map[string]mcpWords{
	"delete": {"영구 삭제", "Delete for good"}, "sharing": {"공유·공개·권한 바꾸기", "Change sharing, publishing or permissions"},
	"members": {"멤버·게스트 바꾸기", "Change members or guests"}, "account": {"계정·백업 관리", "Manage accounts or backups"},
	"organization": {"조직 관리", "Manage the organization"},
}

// Fields a body carries for the request itself, not for what it changes.
var mcpActionQuietFields = map[string]bool{"action": true, "expectedRevision": true, "requestId": true, "revision": true,
	"baseRevision": true, "version": true, "expectedPolicyVersion": true, "policyVersion": true}

const mcpActionChangesMax = 12

// mcpActionBodyAction is the action a body picks, or "".
func mcpActionBodyAction(body []byte) string {
	picked := struct {
		Action string `json:"action"`
	}{}
	_ = json.Unmarshal(body, &picked)
	return picked.Action
}

// mcpActionFinal says the action can't be undone once done.
func mcpActionFinal(operation string, body []byte) bool {
	if mcpActionFamily(operation) == "delete" || mcpActionsFinal[operation] {
		return true
	}
	actions, picks := mcpActionFinalBodyActions[operation]
	return picks && slices.Contains(actions, mcpActionBodyAction(body))
}

// mcpActionName is what the action does, in the language.
func mcpActionName(operation string, body []byte, language string) string {
	if words, found := mcpActionBodyNames[operation][mcpActionBodyAction(body)]; found {
		return words.in(language)
	}
	if words, found := mcpActionNames[operation]; found {
		return words.in(language)
	}
	if words, found := mcpActionFamilyNames[mcpActionFamily(operation)]; found {
		return words.in(language)
	}
	return operation
}

// mcpActionRequestParts splits a kept request ("METHOD /path?query" and its
// body on the lines after) into its path and body.
func mcpActionRequestParts(preview string) (string, []byte) {
	line, body, _ := strings.Cut(preview, "\n")
	_, target, _ := strings.Cut(line, " ")
	path, _, _ := strings.Cut(target, "?")
	return path, []byte(body)
}

// mcpActionPathValues reads a route's path values from the path it was
// called with, in the route's order.
func mcpActionPathValues(operation, path string) [][2]string {
	_, template, _ := strings.Cut(operation, " ")
	names, values := strings.Split(strings.Trim(template, "/"), "/"), strings.Split(strings.Trim(path, "/"), "/")
	if len(names) != len(values) {
		return nil
	}
	found := [][2]string{}
	for index, name := range names {
		if strings.HasPrefix(name, "{") && strings.HasSuffix(name, "}") {
			found = append(found, [2]string{strings.Trim(name, "{}"), values[index]})
		}
	}
	return found
}

// describeMCPAction words a request waiting for the person in the
// language; current, when there is one, reads a path as the asking token
// does, for what a change replaces (mcp_action_current_values.go).
func describeMCPAction(app core.App, current func(path string) map[string]any, personID, operation, preview, language string) mcpActionWords {
	path, body := mcpActionRequestParts(preview)
	words := mcpActionWords{Action: mcpActionName(operation, body, language), Final: mcpActionFinal(operation, body)}
	namer := newMCPActionNamer(app, personID, operation, language)
	values := mcpActionPathValues(operation, path)
	// Done to the person themself, the action is what that does.
	if self, named := mcpActionSelfNames[operation]; named && slices.Contains(values, [2]string{"userId", personID}) {
		words.Action = self.in(language)
	}
	for _, value := range values {
		if kind, name, found := namer.pathValue(value[0], value[1], values); found {
			words.Targets = append(words.Targets, mcpActionTarget{Kind: mcpActionKindNames[kind].in(language), Name: name})
		}
	}
	fields := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&fields) != nil {
		return words
	}
	if takenAway, found := mcpActionTakenAway[operation]; found && current != nil {
		words.Changes = append(words.Changes, takenAway(current, fields, language)...)
	}
	now := mcpActionFlatten("", mcpActionNow(current, operation, path, values, fields), map[string]any{})
	for _, field := range mcpActionBodyFields(body) {
		key := field.Key
		if mcpActionQuietFields[key] {
			continue
		}
		if mcpActionAsksNothing(key, field.Value) {
			words.Unchanged++
			continue
		}
		named := key
		if alias, found := mcpActionFieldAliases[operation][key]; found {
			named = alias
		}
		change := mcpActionChange{Field: key, After: namer.value(named, field.Value, fields, values, language)}
		if label, found := mcpActionFieldNames[named]; found {
			change.Field = label.in(language)
		}
		if mcpActionSecret(key) {
			change.After = mcpWordsHidden.in(language)
		} else if before, found := now[key]; found {
			said := namer.value(named, before, now, values, language)
			if said == change.After {
				words.Unchanged++
				continue
			}
			change.Before = said
		}
		if words.Changes = append(words.Changes, change); len(words.Changes) == mcpActionChangesMax {
			break
		}
	}
	return words
}

// mcpActionNamer names what the person reaches, once each.
type mcpActionNamer struct {
	app      core.App
	personID string
	language string
	admin    bool
	names    map[string]string
}

func newMCPActionNamer(app core.App, personID, operation, language string) *mcpActionNamer {
	namer := &mcpActionNamer{app: app, personID: personID, language: language, names: map[string]string{}}
	if _, path, _ := strings.Cut(operation, " "); strings.HasPrefix(path, "/admin/") {
		namer.admin, _ = isAdministrator(app, personID)
	}
	return namer
}

func (namer *mcpActionNamer) remembered(kind, id string, name func() (string, bool)) (string, bool) {
	key := kind + "\x00" + id
	if said, found := namer.names[key]; found {
		return said, said != ""
	}
	said, found := name()
	if !found {
		said = ""
	}
	namer.names[key] = said
	return said, said != ""
}

// pathValue names one of the route's path values: its kind and name.
func (namer *mcpActionNamer) pathValue(name, id string, values [][2]string) (string, string, bool) {
	kind := map[string]string{"workspaceId": "workspace", "pageId": "page", "rowId": "page", "dataSourceId": "database",
		"blockId": "database", "propertyId": "property", "userId": "person", "accountId": "person", "organizationId": "organization",
		"invitationId": "invitation", "fileId": "file", "legalHoldId": "legalHold", "installId": "app"}[name]
	if kind == "" {
		return "", "", false
	}
	said, found := namer.named(name, id, values)
	return kind, said, found
}

// named is the name of what a path value or a body field names, when the
// person reaches it.
func (namer *mcpActionNamer) named(field, id string, values [][2]string) (string, bool) {
	app, person := namer.app, namer.personID
	if id == "" || len(id) > 64 {
		return "", false
	}
	switch field {
	case "workspaceId", "workspaceIds":
		return namer.remembered("workspace", id, func() (string, bool) {
			if role, _ := workspaceRoleOf(app, person, id); role == "" && !namer.admin {
				return "", false
			}
			return namer.column(`SELECT [[name]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`, id)
		})
	case "pageId", "pageIds", "navigationPageIds":
		return namer.remembered("page", id, func() (string, bool) {
			page, err := app.FindRecordById(pagesCollection, id)
			if err != nil || !namer.admin && !namer.opens(id) {
				return "", false
			}
			// One without a title as fetch calls it, in the person's language (mcp_untitled_names.go).
			return mcpNamedOrCalled(page.GetString("title"), func() string { return mcpUntitledPageNameIn(app, namer.language, page) }), true
		})
	case "rowId":
		return namer.remembered("row", id, func() (string, bool) {
			pageID, found := namer.column(`SELECT [[source]].[[page_id]] FROM {{leafen_database_rows}} [[row]]
				JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[row]].[[data_source_id]] WHERE [[row]].[[id]] = {:id}`, id)
			if !found || !namer.opens(pageID) {
				return "", false
			}
			return mcpNamedOrCalled(databaseRowPageTitle(app, id), func() string {
				row, _ := app.FindRecordById(pagesCollection, id)
				return mcpUntitledPageNameIn(app, namer.language, row)
			}), true
		})
	case "dataSourceId", "blockId":
		return namer.remembered("database", id, func() (string, bool) {
			pageID, found := namer.column(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`, id)
			if !found || !namer.opens(pageID) {
				return "", false
			}
			title, _ := namer.column(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`, id)
			return mcpNamedOrCalled(title, func() string {
				location, _ := loadMCPDataSourceLocationToRead(app, id)
				return mcpUntitledDataSourceNameIn(app, namer.language, location)
			}), true
		})
	case "propertyId":
		return namer.remembered("property", id, func() (string, bool) {
			sourceID, found := namer.column(`SELECT [[data_source_id]] FROM {{leafen_database_properties}} WHERE [[id]] = {:id}`, id)
			if !found {
				return "", false
			}
			if _, reaches := namer.named("dataSourceId", sourceID, values); !reaches {
				return "", false
			}
			return namer.column(`SELECT [[name]] FROM {{leafen_database_properties}} WHERE [[id]] = {:id}`, id)
		})
	case "userId", "userIds", "accountId", "principalId":
		return namer.remembered("person", id, func() (string, bool) {
			shared := 0
			_ = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspace_memberships}} [[them]]
				JOIN {{leafen_workspace_memberships}} [[me]] ON [[me]].[[workspace_id]] = [[them]].[[workspace_id]]
				WHERE [[them]].[[user_id]] = {:id} AND [[me]].[[user_id]] = {:person}`).
				Bind(dbx.Params{"id": id, "person": person}).Row(&shared)
			if shared == 0 && !namer.admin && !namer.sharesOrganization(id) {
				return "", false
			}
			var name, email string
			if err := app.DB().NewQuery(`SELECT [[name]], [[email]] FROM {{users}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": id}).Row(&name, &email); err != nil {
				return "", false
			}
			return firstNonempty(strings.TrimSpace(name), email), true
		})
	case "groupId":
		return namer.remembered("group", id, func() (string, bool) {
			workspaceID, found := namer.column(`SELECT [[workspace_id]] FROM {{leafen_workspace_permission_groups}} WHERE [[id]] = {:id}`, id)
			if !found {
				return "", false
			}
			if role, _ := workspaceRoleOf(app, person, workspaceID); role == "" {
				return "", false
			}
			return namer.column(`SELECT [[name]] FROM {{leafen_workspace_permission_groups}} WHERE [[id]] = {:id}`, id)
		})
	case "organizationId":
		return namer.remembered("organization", id, func() (string, bool) {
			if !namer.memberOfOrganization(id) {
				return "", false
			}
			return namer.column(`SELECT [[name]] FROM {{leafen_organizations}} WHERE [[id]] = {:id}`, id)
		})
	case "tenancyKey":
		// Where an app's data and people live apart: an organization, or the
		// workspaces outside any (app_platform_store.go).
		if id == appTenancyServer {
			return mcpWordsOutsideOrganizations.in(namer.language), true
		}
		if organizationID, isOrganization := strings.CutPrefix(id, "org:"); isOrganization {
			return namer.named("organizationId", organizationID, values)
		}
	case "legalHoldId":
		return namer.remembered("legalHold", id, func() (string, bool) {
			organizationID, found := namer.column(`SELECT [[organization_id]] FROM {{leafen_organization_legal_holds}} WHERE [[id]] = {:id}`, id)
			if !found || !namer.memberOfOrganization(organizationID) {
				return "", false
			}
			return namer.column(`SELECT [[name]] FROM {{leafen_organization_legal_holds}} WHERE [[id]] = {:id}`, id)
		})
	case "invitationId":
		return namer.remembered("invitation", id, func() (string, bool) {
			workspaceID, found := namer.column(`SELECT [[workspace_id]] FROM {{leafen_workspace_invitations}} WHERE [[id]] = {:id}`, id)
			if !found {
				return "", false
			}
			if role, _ := workspaceRoleOf(app, person, workspaceID); role == "" {
				return "", false
			}
			return namer.column(`SELECT [[email]] FROM {{leafen_workspace_invitations}} WHERE [[id]] = {:id}`, id)
		})
	case "installId":
		// An app installed on the server, by its address name, for its administrator.
		if !namer.admin {
			return "", false
		}
		return namer.remembered("app", id, func() (string, bool) {
			return namer.column(`SELECT [[slug]] FROM {{leafen_app_installs}} WHERE [[id]] = {:id}`, id)
		})
	case "fileId":
		if !namer.admin {
			return "", false
		}
		return namer.remembered("file", id, func() (string, bool) {
			return namer.column(`SELECT [[original_name]] FROM {{leafen_files}} WHERE [[id]] = {:id}`, id)
		})
	}
	return "", false
}

func (namer *mcpActionNamer) column(query, id string) (string, bool) {
	value := ""
	if err := namer.app.DB().NewQuery(query).Bind(dbx.Params{"id": id}).Row(&value); err != nil {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// opens says the person reads the page.
func (namer *mcpActionNamer) opens(pageID string) bool {
	_, err := authorizePageAccessRole(namer.app, namer.personID, pageID, pageAccessRoleView)
	return err == nil
}

func (namer *mcpActionNamer) memberOfOrganization(organizationID string) bool {
	count := 0
	_ = namer.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_memberships}} WHERE [[organization]] = {:organization}
		AND [[user]] = {:person}`).Bind(dbx.Params{"organization": organizationID, "person": namer.personID}).Row(&count)
	return count > 0 || namer.admin
}

func (namer *mcpActionNamer) sharesOrganization(userID string) bool {
	count := 0
	_ = namer.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_memberships}} [[them]]
		JOIN {{leafen_organization_memberships}} [[me]] ON [[me]].[[organization]] = [[them]].[[organization]]
		WHERE [[them]].[[user]] = {:id} AND [[me]].[[user]] = {:person}`).Bind(dbx.Params{"id": userID, "person": namer.personID}).Row(&count)
	return count > 0
}

// value words one value a body sets, or the route reads now: a word for
// what the field takes, the name of what an ID names, or the value as sent.
func (namer *mcpActionNamer) value(field string, value any, fields map[string]any, values [][2]string, language string) string {
	switch typed := value.(type) {
	case nil:
		return mcpWordsNothing.in(language)
	case bool:
		if typed {
			return mcpWordsYes.in(language)
		}
		return mcpWordsNo.in(language)
	case json.Number:
		return typed.String()
	case string:
		if strings.TrimSpace(typed) == "" {
			return mcpWordsNothing.in(language)
		}
		if words, found := mcpActionValueNames[field][typed]; found {
			return words.in(language)
		}
		lookup := field
		if field == "principalId" {
			// A share's principal is named as its kind says; an email or a
			// connection reads as sent.
			kind, _ := fields["principalType"].(string)
			lookup = map[string]string{"": field, publicMutationPrincipalUser: field, publicMutationPrincipalGroup: "groupId",
				publicMutationPrincipalWorkspace: "workspaceId"}[kind]
		}
		if name, found := namer.named(lookup, typed, values); found {
			return name
		}
		return appOneLine(typed, 120)
	case []any:
		if len(typed) == 0 {
			return mcpWordsNothing.in(language)
		}
		said := []string{}
		for _, item := range typed {
			if _, object := item.(map[string]any); object {
				return fmt.Sprintf(mcpWordsCount.in(language), len(typed))
			}
			if len(said) == 5 {
				return strings.Join(said, ", ") + " " + fmt.Sprintf(mcpWordsMore.in(language), len(typed)-len(said))
			}
			said = append(said, namer.value(field, item, fields, values, language))
		}
		return strings.Join(said, ", ")
	}
	encoded, _ := json.Marshal(value)
	return appOneLine(string(encoded), 120)
}

// mcpActionLine is the action in one line: what it does, to what.
func mcpActionLine(words mcpActionWords) string {
	names := make([]string, 0, len(words.Targets))
	for _, target := range words.Targets {
		names = append(names, "‘"+target.Name+"’")
	}
	if len(names) == 0 {
		return words.Action
	}
	return words.Action + " · " + strings.Join(names, " › ")
}
