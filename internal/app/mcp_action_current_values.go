package app

import (
	"fmt"
	"strings"
)

// What a setting holds now, for the question that asks to change it
// (mcp_action_approval_words.go), keyed as the change's body is: most
// routes read it at their own path, as the asking token reads it; these
// read it where it is kept — an organization's app safeguards and the
// server's app policies in the apps' list, an app's running and people in
// its install, an app's open address in its usage, a workspace's guest
// 2-step in the organization's list, and an organization's single sign-on
// in its provider (2026-11-13: the oversight's question showed the new
// value alone).

type mcpActionCurrentRead func(read func(path string) map[string]any, values map[string]string, body map[string]any) map[string]any

var mcpActionCurrentReads = map[string]mcpActionCurrentRead{
	"PUT /admin/apps/oversight": mcpActionAppTenancyNow,
	"PUT /admin/apps/strict":    mcpActionAppTenancyNow,
	"PUT /admin/apps/settings": func(read func(string) map[string]any, _ map[string]string, _ map[string]any) map[string]any {
		settings, _ := read("/admin/apps")["settings"].(map[string]any)
		return settings
	},
	"PUT /admin/apps/allowlist": func(read func(string) map[string]any, _ map[string]string, _ map[string]any) map[string]any {
		if allowed, found := read("/admin/apps")["allowlist"]; found {
			return map[string]any{"apps": allowed}
		}
		return nil
	},
	"PATCH /admin/apps/installs/{installId}/status": mcpActionAppInstallNow,
	"PUT /admin/apps/installs/{installId}/grants":   mcpActionAppInstallNow,
	"PUT /admin/apps/installs/{installId}/endpoint-public": func(read func(string) map[string]any, values map[string]string, body map[string]any) map[string]any {
		endpoint := mcpActionListItem(read("/admin/apps/installs/" + values["installId"] + "/usage")["endpoints"], "key", body["key"])
		if endpoint == nil {
			return nil
		}
		return map[string]any{"open": endpoint["open"]}
	},
	"PUT /organizations/{organizationId}/guest-mfa": func(read func(string) map[string]any, values map[string]string, body map[string]any) map[string]any {
		workspace := mcpActionListItem(read("/organizations/" + values["organizationId"] + "/guest-mfa")["workspaces"], "id", body["workspaceId"])
		if workspace == nil {
			return nil
		}
		return map[string]any{"required": workspace["required"]}
	},
	"PUT /organizations/{organizationId}/saml": func(read func(string) map[string]any, values map[string]string, _ map[string]any) map[string]any {
		provider, _ := read("/organizations/" + values["organizationId"] + "/saml")["provider"].(map[string]any)
		return provider
	},
}

// mcpActionNow is what the change's own fields hold now, or nothing when it
// can't be read; current reads a path as the asking token does.
func mcpActionNow(current func(path string) map[string]any, operation, path string, pathValues [][2]string, body map[string]any) map[string]any {
	method, _, _ := strings.Cut(operation, " ")
	if current == nil || method != "PUT" && method != "PATCH" {
		return nil
	}
	read, found := mcpActionCurrentReads[operation]
	if !found {
		return current(path)
	}
	values := map[string]string{}
	for _, value := range pathValues {
		values[value[0]] = value[1]
	}
	return read(current, values, body)
}

// mcpActionAppTenancyNow is an organization's app safeguards — the oversight
// and strict connecting — in the apps' list.
func mcpActionAppTenancyNow(read func(string) map[string]any, _ map[string]string, body map[string]any) map[string]any {
	return mcpActionListItem(read("/admin/apps")["tenancies"], "key", body["tenancyKey"])
}

// mcpActionAppInstallNow is an installed app as the apps' list has it: its
// running and its people.
func mcpActionAppInstallNow(read func(string) map[string]any, values map[string]string, _ map[string]any) map[string]any {
	return mcpActionListItem(read("/admin/apps")["installs"], "installId", values["installId"])
}

// mcpActionListItem is the object of a list whose field is the value.
func mcpActionListItem(list any, field string, value any) map[string]any {
	items, _ := list.([]any)
	for _, item := range items {
		if object, isObject := item.(map[string]any); isObject && value != nil && object[field] == value {
			return object
		}
	}
	return nil
}

// mcpActionTakenAway are what a request loosening the person's own sign-in
// or deleting their account takes away that its body doesn't name, as it is
// now → after it: their 2-step verification, the recovery codes they saved,
// their sign-in emails, and the workspaces their account's deletion deletes
// and leaves (user_key_account_routes.go).
var mcpActionTakenAway = map[string]func(read func(string) map[string]any, body map[string]any, language string) []mcpActionChange{
	"DELETE /account/mfa/totp": func(read func(string) map[string]any, _ map[string]any, language string) []mcpActionChange {
		change := mcpActionChange{Field: mcpWordsTwoStep.in(language), After: mcpWordsOff.in(language)}
		if factors, known := read("/account/mfa")["factors"].([]any); known {
			change.Before = mcpWordsOff.in(language)
			for _, factor := range factors {
				if found, _ := factor.(map[string]any); found["type"] == "totp" && found["verified"] == true {
					change.Before = mcpWordsOn.in(language)
				}
			}
		}
		return []mcpActionChange{change}
	},
	"POST /account/mfa/recovery-codes": func(read func(string) map[string]any, _ map[string]any, language string) []mcpActionChange {
		change := mcpActionChange{Field: mcpWordsRecoveryCodes.in(language), After: mcpWordsNewRecoveryCodes.in(language)}
		if remaining, known := read("/account/mfa")["recoveryCodesRemaining"].(float64); known {
			change.Before = fmt.Sprintf(mcpWordsCount.in(language), int(remaining))
		}
		return []mcpActionChange{change}
	},
	"DELETE /account/emails": func(read func(string) map[string]any, body map[string]any, language string) []mcpActionChange {
		email, _ := body["email"].(string)
		return mcpActionOneFewer(read("/account/emails")["emails"], "email", strings.ToLower(strings.TrimSpace(email)), mcpWordsSignInEmails, language)
	},
	"POST /account/deletion": func(read func(string) map[string]any, _ map[string]any, language string) []mcpActionChange {
		preview := read("/account/deletion")
		if preview == nil {
			return nil
		}
		return []mcpActionChange{
			{Field: mcpWordsDeletedWorkspaces.in(language), After: mcpActionWorkspaceList(preview["deletedWorkspaces"], preview["deletedWorkspaceCount"], true, language)},
			{Field: mcpWordsLeftWorkspaces.in(language), After: mcpActionWorkspaceList(preview["leftWorkspaces"], preview["leftWorkspaceCount"], false, language)},
		}
	},
}

// mcpActionRefusedFirst refuse, before the person is asked, a request its
// handler can't carry out as things stand, for the reason their own screen
// gives, read as the asking token reads it: an account deletion the only
// administrator, a legal hold or an organization others' workspaces are in
// keeps from going ahead (account_deletion.go). Their yes couldn't make it
// happen, so they aren't asked (2026-11-13: the only administrator's agent
// asked them, and the deletion was refused after their yes).
var mcpActionRefusedFirst = map[string]func(read func(string) map[string]any) (status int, code, message string){
	"POST /account/deletion": func(read func(string) map[string]any) (int, string, string) {
		blocker, _ := read("/account/deletion")["blocker"].(string)
		return accountDeletionRefusal(blocker)
	},
}

// mcpActionWorkspaceListNamed is how many workspaces a list names before it counts the rest.
const mcpActionWorkspaceListNamed = 5

// mcpActionWorkspaceList names the workspaces a list holds, with their
// members where they go with it, and counts those past the first few.
func mcpActionWorkspaceList(list any, total any, members bool, language string) string {
	items, _ := list.([]any)
	count, known := total.(float64)
	if !known {
		count = float64(len(items))
	}
	if count == 0 {
		return mcpWordsNothing.in(language)
	}
	names := []string{}
	for _, item := range items {
		workspace, _ := item.(map[string]any)
		if workspace == nil || len(names) == mcpActionWorkspaceListNamed {
			break
		}
		name, _ := workspace["name"].(string)
		if name = strings.TrimSpace(name); name == "" {
			name = mcpWordsUntitled.in(language)
		}
		if memberCount, _ := workspace["memberCount"].(float64); members && memberCount > 1 {
			name = fmt.Sprintf(mcpWordsMembers.in(language), name, int(memberCount))
		}
		names = append(names, name)
	}
	said := strings.Join(names, ", ")
	if rest := int(count) - len(names); rest > 0 {
		said = strings.TrimSpace(said + " " + fmt.Sprintf(mcpWordsMore.in(language), rest))
	}
	return said
}

// mcpActionOneFewer is a list one of whose items a request takes away, by
// how many it holds now and after; nothing where the item isn't in it.
func mcpActionOneFewer(list any, field string, value any, words mcpWords, language string) []mcpActionChange {
	items, _ := list.([]any)
	if mcpActionListItem(list, field, value) == nil {
		return nil
	}
	return []mcpActionChange{{Field: words.in(language), Before: fmt.Sprintf(mcpWordsCount.in(language), len(items)),
		After: fmt.Sprintf(mcpWordsCount.in(language), len(items)-1)}}
}
