package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// appPermissions lists what an administrator approves, most telling first,
// and appPermissionsHash fingerprints exactly the manifest parts that list
// comes from, so any change to what the app may reach asks again. A line
// carries what the screen says it with in the reader's language — names in
// every language the app gave, a schedule's cron, the types it names — and
// detail keeps the line's own words for what is said as it is, such as an
// address (2026-10-16: the lines showed keys, cron and English names).
func appPermissions(manifest appManifest) []appPermissionWire {
	list := []appPermissionWire{}
	add := func(kind, detail, risk string) *appPermissionWire {
		list = append(list, appPermissionWire{Kind: kind, Detail: detail, Risk: risk})
		return &list[len(list)-1]
	}
	credentialName := func(key string) map[string]string {
		for _, credential := range manifest.Credentials {
			if credential.Key == key && len(credential.Name) > 0 {
				return credential.Name
			}
		}
		return map[string]string{"en": key}
	}
	settingName := func(key string) map[string]string {
		for _, setting := range manifest.Settings {
			if setting.Key == key && len(setting.Name) > 0 {
				return setting.Name
			}
		}
		return map[string]string{"en": key}
	}
	actionName := func(key string) map[string]string {
		for _, action := range manifest.Actions {
			if action.Key == key && len(action.Name) > 0 {
				return action.Name
			}
		}
		return map[string]string{"en": key}
	}
	switch manifest.PersonAccess {
	case "admin":
		add("personAccess", "admin", "high")
	case "structure", "content":
		add("personAccess", manifest.PersonAccess, "medium")
	case "read":
		add("personAccess", "read", "low")
	case appOpenedFileAccess:
		add("personAccess", appOpenedFileAccess, "low")
	}
	for _, direct := range manifest.Network.Direct {
		add("directAddress", direct.URL+" ("+strings.Join(direct.Kinds, ", ")+")", "medium").Types = direct.Kinds
	}
	for _, relay := range manifest.Network.Relay {
		risk := "medium"
		if relay.PrivateNetwork {
			risk = "high"
			add("privateNetwork", relay.URL, risk)
		} else {
			add("relayAddress", relay.URL, risk)
		}
		for _, secret := range relay.Secrets {
			add("secret", secret, "medium")
		}
		for _, key := range relay.OAuth {
			add("outsideSignInUse", key+" → "+relay.URL, "medium")
		}
		for _, credential := range relay.Credentials {
			add("credentialUse", credential+" → "+relay.URL, "high").Name = credentialName(credential)
		}
	}
	for _, socket := range manifest.Network.Sockets {
		risk := "medium"
		if socket.PrivateNetwork {
			risk = "high"
		}
		where := socket.Host + ":" + strconv.Itoa(socket.Port)
		setting, named := appSettingName(socket.Host)
		if named {
			// The administrator sets the address; the line names the setting.
			add("settingSocket", setting, risk).Name = settingName(setting)
			where = "setting:" + setting
		} else {
			add("socket", where, risk)
		}
		for _, credential := range socket.Credentials {
			line := add("credentialUse", credential+" → "+where, "high")
			line.Name = credentialName(credential)
			if named {
				line.Target = settingName(setting)
			}
		}
	}
	for _, credential := range manifest.Credentials {
		add("personCredential", credential.Key, "high").Name = credentialName(credential.Key)
	}
	for _, provider := range manifest.Network.OAuth {
		add("outsideSignIn", provider.Key+" "+provider.AuthorizeURL, "medium")
	}
	for _, device := range manifest.Device {
		add("device", device, "medium")
	}
	for _, opener := range manifest.Surfaces.FileOpeners {
		if opener.Edit {
			add("fileEditor", strings.Join(opener.Types, ", "), "medium").Types = opener.Types
		} else {
			add("fileOpener", strings.Join(opener.Types, ", "), "low").Types = opener.Types
		}
	}
	for _, source := range manifest.Surfaces.ImportSources {
		add("importSource", appPermissionName(source.Name), "low").Name = source.Name
	}
	if manifest.WebAssembly {
		add("webAssembly", "", "low")
	}
	if manifest.ScriptEval {
		add("scriptEval", "", "high")
	}
	if realtime := manifest.Realtime; realtime != nil {
		add("realtime", strconv.Itoa(realtime.MaxParticipants), "low")
		if realtime.WebRTC {
			add("webrtc", "", "medium")
		}
		if realtime.Loop {
			add("roomLoop", realtime.Function, "medium")
		}
	}
	if guests := manifest.Guests; guests != nil {
		add("guests", strings.Join(guests.Rooms, ", "), "medium").Name = appGuestRoomsNamed(guests)
	}
	for _, worker := range manifest.Workers {
		if worker.LongRunning {
			add("longRunningWorker", worker.Name, "medium")
		}
		if worker.Resident {
			add("residentWorker", worker.Name, "low")
		}
	}
	for _, schedule := range manifest.Schedules {
		detail := schedule.Key + " " + schedule.Cron
		if schedule.PerPerson {
			detail += " (per person)"
		}
		line := add("schedule", detail, "low")
		line.Name, line.Cron, line.PerPerson = schedule.Name, schedule.Cron, schedule.PerPerson
	}
	for _, webhook := range manifest.Webhooks {
		add("webhook", webhook.Key, "low").Name = webhook.Name
	}
	// A program outside Leafen reaches what the person may through the app.
	for _, endpoint := range manifest.Endpoints {
		detail := appPermissionName(endpoint.Name) + " (" + endpoint.Key + ")"
		if len(endpoint.Methods) > 0 {
			detail += " " + strings.Join(endpoint.Methods, ", ")
		}
		kind, risk := "endpoint", "medium"
		if endpoint.Public {
			kind, risk = "publicEndpoint", "high"
		}
		add(kind, detail, risk).Name = endpoint.Name
		if endpoint.Pages {
			add("endpointPages", appPermissionName(endpoint.Name)+" ("+endpoint.Key+")", "high").Name = endpoint.Name
		}
	}
	for _, event := range manifest.Events {
		add("dataEvent", strings.Join(event.Types, ", "), "low").Types = event.Types
	}
	for _, action := range manifest.Actions {
		if action.offeredToAgents() && !action.RequiresApproval && action.Risk != "high" {
			add("toolWithoutApproval", action.Key, action.Risk).Name = actionName(action.Key)
		}
	}
	// Lines in people's menus, named as they will read.
	for _, item := range manifest.SlashItems {
		target := "block " + item.Block
		if item.Action != "" {
			target = "action " + item.Action
		}
		line := add("slashItem", appPermissionName(item.Name)+" → "+target, "low")
		line.Name = item.Name
		if item.Action != "" {
			line.Target, line.TargetKind = actionName(item.Action), "action"
		} else if block, found := appManifestBlockByKey(manifest, item.Block); found {
			line.Target, line.TargetKind = block.Name, "block"
		}
	}
	for _, menu := range manifest.MenuActions {
		line := add("menuAction", appPermissionName(menu.Name)+" ("+strings.Join(menu.On, ", ")+") → action "+menu.Action, "low")
		line.Name, line.Target, line.TargetKind, line.Types = menu.Name, actionName(menu.Action), "action", menu.On
	}
	for _, editor := range manifest.PropertyEditors {
		line := add("propertyEditor", appPermissionName(editor.Name)+" ("+strings.Join(editor.Types, ", ")+")", "low")
		line.Name, line.Types = editor.Name, editor.Types
	}
	if agent := manifest.Agent; agent != nil && (len(agent.Instructions) > 0 || len(agent.Prompts) > 0) {
		add("agentInstructions", "", "medium")
	}
	if kv := manifest.KV; kv != nil {
		add("storage", strconv.FormatInt(kv.MaxBytes, 10), "low")
	}
	if manifest.Notifications {
		add("notifications", "", "low")
	}
	if manifest.Groups {
		add("groups", "", "medium")
	}
	for _, data := range manifest.Data {
		if data.Locked {
			add("lockedData", data.Key, "low")
		}
	}
	return list
}

// appPackagePermissions are a kept package's permission lines, its data
// named as people find it once installed: what the entry's setup step
// makes ("Sites"), in each language its file names, the key staying the
// line's detail, so a line keeps its identity across versions and the
// approval its hash (2026-11-08: the review showed "(sites)"). An entry
// whose setup file can't be read keeps its key.
func appPackagePermissions(app core.App, packageHash string, manifest appManifest) []appPermissionWire {
	lines := appPermissions(manifest)
	steps := map[string]appDataStep{}
	read := map[string]bool{}
	for _, data := range manifest.Data {
		if !data.Locked || read[data.File] {
			continue
		}
		read[data.File] = true
		raw, err := loadAppPackageFile(app, packageHash, data.File)
		if err != nil {
			continue
		}
		if setup, err := parseAppDataSetup(raw); err == nil {
			for _, step := range setup.Steps {
				steps[step.Key] = step
			}
		}
	}
	for index := range lines {
		if step, found := steps[lines[index].Detail]; found && lines[index].Kind == "lockedData" {
			lines[index].Name = appDataStepNames(step)
		}
	}
	return lines
}

func appManifestBlockByKey(manifest appManifest, key string) (appManifestBlock, bool) {
	for _, block := range manifest.Surfaces.Blocks {
		if block.Key == key {
			return block, true
		}
	}
	return appManifestBlock{}, false
}

// appPermissionName is a menu line's name in English, or else in the
// first language it has.
func appPermissionName(name map[string]string) string {
	if text := name["en"]; text != "" {
		return text
	}
	languages := make([]string, 0, len(name))
	for language := range name {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	if len(languages) == 0 {
		return ""
	}
	return name[languages[0]]
}

func appPermissionsHash(manifest appManifest) string {
	actions := make([]map[string]any, 0, len(manifest.Actions))
	for _, action := range manifest.Actions {
		actions = append(actions, map[string]any{
			"key": action.Key, "runAs": action.RunAs, "risk": action.Risk, "requiresApproval": action.RequiresApproval,
			"externalEffect": action.ExternalEffect, "tool": action.offeredToAgents(), "description": action.Description, "input": action.Input,
		})
	}
	approved := map[string]any{
		"personAccess": manifest.PersonAccess, "data": manifest.Data, "network": manifest.Network, "device": manifest.Device,
		"realtime": manifest.Realtime, "kv": manifest.KV, "workers": manifest.Workers, "schedules": manifest.Schedules,
		"webhooks": appWebhooksApproved(manifest.Webhooks), "actions": actions, "agent": manifest.Agent, "guests": appGuestsApproved(manifest.Guests),
	}
	// Added later: only an app that has them hashes them, so others keep their approval.
	if len(manifest.Events) > 0 {
		approved["events"] = manifest.Events
	}
	if manifest.Notifications {
		approved["notifications"] = true
	}
	if manifest.Groups {
		approved["groups"] = true
	}
	if len(manifest.Endpoints) > 0 {
		approved["endpoints"] = manifest.Endpoints
	}
	if len(manifest.SlashItems) > 0 {
		approved["slashItems"] = manifest.SlashItems
	}
	if len(manifest.MenuActions) > 0 {
		approved["menuActions"] = manifest.MenuActions
	}
	if len(manifest.PropertyEditors) > 0 {
		approved["propertyEditors"] = manifest.PropertyEditors
	}
	if len(manifest.Surfaces.ImportSources) > 0 {
		approved["importSources"] = manifest.Surfaces.ImportSources
	}
	encoded, _ := json.Marshal(approved)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
