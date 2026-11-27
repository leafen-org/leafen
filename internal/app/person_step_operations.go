package app

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The operations a person step holds (person_steps.go), each with the
// fields of its body only the person types, where on their screen they type
// them, and what Leafen makes for them to keep or shows them alone. An
// operation not listed here takes its secrets from whoever calls it, as
// before; the agent's own words say where else such a step stays the
// person's (docs/guide/agents.md).

type personStepOperation struct {
	// The settings section the inbox notice opens, by the fields asked
	// where they differ (one operation saving several sections' settings).
	section func(fields []string) string
	// The way to it as the person's screen names it (mcp_screen_names.go).
	screen func(fields []string) []string
	// What the step sets up, as the person reads it; by section where
	// the operation saves several (sectionTitles).
	title         mcpWords
	sectionTitles map[string]mcpWords
	// personal: the section is the person's own settings', not the server's.
	personal bool
	// The body's fields, by dotted path, that only the person types.
	secrets []string
	// fieldsFor is the secret fields a body leaves to the person when its
	// agent says secretsFromPerson: true; nil is every secret it leaves out.
	fieldsFor func(app core.App, body map[string]any) []string
	// keepFor is what Leafen makes for the person to keep before the
	// operation runs, for this body and the fields asked; "" for nothing.
	keepFor func(app core.App, body map[string]any, fields []string) string
	// keepOnly: what is kept is the whole operation, which then doesn't run.
	keepOnly bool
	// shows are the fields of the operation's answer shown to the person
	// alone: it runs when they open it, and it is done.
	shows []string
	// made are the secret fields Leafen makes when they are left out
	// (agentSecretOperations' Made): a step asking true leaves them out, so
	// the person sees what was made, in shows, instead of typing one; a list
	// naming them has the person type them.
	made []string
	// place names, without a secret, where the operation acts, for the
	// person's screen; placeAt by what its path names.
	place   func(body map[string]any, path string) string
	placeAt func(app core.App, path string) string
}

// An operation that makes a value nobody holds yet when it is left out (a
// temporary password, a person's new one) shows it to the person alone
// where its agent leaves it to them (user 2026-11-23: an agent with
// Leafen's tools alone hands a new password over without it entering the
// conversation), from the one list of what is made (agent_secret_notes.go).
func init() {
	for id, operation := range personStepOperations {
		made, out := agentSecretMade(agentSecretOperations[id])
		if len(made) == 0 {
			continue
		}
		operation.made, operation.shows = made, out
		personStepOperations[id] = operation
	}
}

// personStepAccountPlace is the account an administrator's step acts on, by
// its email or name, so the person knows whose password they see.
func personStepAccountPlace(app core.App, path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "admin" || parts[1] != "accounts" {
		return ""
	}
	account, err := app.FindRecordById("users", parts[2])
	if err != nil || account == nil {
		return ""
	}
	return firstNonempty(account.Email(), account.GetString("name"))
}

const personStepKeepRecoveryCode = "recovery-code"

func personStepSection(section string) func([]string) string {
	return func([]string) string { return section }
}

func personStepScreen(names ...string) func([]string) []string {
	return func([]string) []string { return names }
}

var personStepOperations = map[string]personStepOperation{
	"PUT /admin/backups/schedule": {
		section: personStepSection("data"), screen: personStepScreen("settings", "server", "serverBackups"),
		title:   mcpWords{"예약 백업", "Scheduled backups"},
		secrets: []string{"s3AccessKey", "s3SecretKey", "webdavPassword", "password"}, fieldsFor: scheduleStepFields,
		keepFor: scheduleStepKeep, place: scheduleStepPlace,
	},
	"POST /admin/backups/protection/code": {
		section: personStepSection("data"), screen: personStepScreen("settings", "server", "serverBackups"),
		title:   mcpWords{"백업 복구 코드", "Backup recovery code"},
		keepFor: func(core.App, map[string]any, []string) string { return personStepKeepRecoveryCode }, keepOnly: true,
	},
	"PUT /admin/backups/protection/password": {
		section: personStepSection("data"), screen: personStepScreen("settings", "server", "serverBackups"),
		title: mcpWords{"백업 비밀번호", "Backup password"}, secrets: []string{"password"},
	},
	"POST /admin/backups/standby/code": {
		section: personStepSection("standby"), screen: personStepScreen("settings", "server", "serverStandby"),
		title: mcpWords{"예비 서버 코드", "Standby server code"}, shows: []string{"code"},
	},
	"PUT /admin/backups/standby/join": {
		section: personStepSection("standby"), screen: personStepScreen("settings", "server", "serverStandby", "standbyJoin"),
		title: mcpWords{"예비 서버가 되기", "Become a standby server"}, secrets: []string{"code"},
	},
	"PATCH /admin/settings": {
		section: settingsStepSection, screen: func(fields []string) []string {
			return []string{"settings", "server", settingsStepScreen[settingsStepSection(fields)]}
		},
		title: mcpWords{"서버 설정", "Server settings"}, sectionTitles: map[string]mcpWords{
			"email": {"이메일 전송", "Email delivery"}, "providers": {"가입 및 로그인", "Sign-up and login"}, "map": {"지도", "Map"},
		},
		secrets: []string{"smtp.password", "providers.google.clientSecret", "providers.oidc.clientSecret",
			"placeSearch.apiKey", "placeSearch.apiKeyId"},
		fieldsFor: settingsStepFields,
		place: func(body map[string]any, _ string) string {
			return strings.Trim(personStepString(body, "smtp.host")+" · "+personStepString(body, "placeSearch.endpoint"), " ·")
		},
	},
	"PUT /admin/network-proxy": {
		section: personStepSection("tunnel"), screen: personStepScreen("settings", "server", "serverRemoteAccess"),
		title: mcpWords{"프록시", "Proxy"}, secrets: []string{"password"}, fieldsFor: proxyStepFields,
		place: func(body map[string]any, _ string) string { return personStepString(body, "url") },
	},
	"PUT /admin/tunnel": {
		section: personStepSection("tunnel"), screen: personStepScreen("settings", "server", "serverRemoteAccess"),
		title: mcpWords{"외부에서 접속", "Access from outside"}, secrets: []string{"token"}, fieldsFor: tunnelStepFields,
		place: func(body map[string]any, _ string) string { return personStepString(body, "address") },
	},
	"POST /admin/backups/protection/open": {
		section: personStepSection("restore"), screen: personStepScreen("settings", "server", "serverRestore"),
		title: mcpWords{"백업 열기", "Open the backups"}, secrets: []string{"secret"},
	},
	"POST /admin/accounts": {
		section: personStepSection("users"), screen: personStepScreen("settings", "server", "serverUsers"),
		title: mcpWords{"새 계정", "New account"}, secrets: []string{"password"},
		place: func(body map[string]any, _ string) string { return personStepString(body, "email") },
	},
	"PUT /admin/accounts/{accountId}/password": {
		section: personStepSection("users"), screen: personStepScreen("settings", "server", "serverUsers"),
		title: mcpWords{"계정 비밀번호", "Account password"}, secrets: []string{"password"}, placeAt: personStepAccountPlace,
	},
	"POST /mcp-services": {
		section: personStepSection("integrations"), screen: personStepScreen("settings", "connections"), personal: true,
		title: mcpWords{"외부 서비스 연결", "Outside service"}, secrets: []string{"token", "auth.token", "auth.password", "auth.value"},
		fieldsFor: serviceStepFields,
		place: func(body map[string]any, _ string) string {
			return strings.Trim(personStepString(body, "name")+" · "+personStepString(body, "url"), " ·")
		},
	},
	"PUT /admin/apps/installs/{installId}/secrets/{name}": {
		section: personStepSection("apps"), screen: personStepScreen("settings", "server", "serverApps"),
		title: mcpWords{"앱 비밀값", "App secret"}, secrets: []string{"value"},
		place: func(_ map[string]any, path string) string {
			name, _ := url.PathUnescape(path[strings.LastIndex(path, "/")+1:])
			return name
		},
	},
}

// settingsStepFields asks the secret of each part of the server's settings
// the body turns on and leaves without one: the mail server's password where
// it signs in with a user name, a sign-in provider's client secret, the
// place search's key.
func settingsStepFields(_ core.App, body map[string]any) []string {
	fields := []string{}
	on := func(path string) bool { enabled, _ := personStepValue(body, path).(bool); return enabled }
	for _, part := range []struct{ enabled, needs, field string }{
		{"smtp.enabled", "smtp.username", "smtp.password"},
		{"providers.google.enabled", "", "providers.google.clientSecret"},
		{"providers.oidc.enabled", "", "providers.oidc.clientSecret"},
		{"placeSearch.enabled", "", "placeSearch.apiKey"},
	} {
		if on(part.enabled) && (part.needs == "" || personStepString(body, part.needs) != "") && !personStepGiven(body, part.field) {
			fields = append(fields, part.field)
		}
	}
	return fields
}

// The section PATCH /admin/settings's fields are saved in, and its name.
func settingsStepSection(fields []string) string {
	switch {
	case slices.ContainsFunc(fields, func(field string) bool { return strings.HasPrefix(field, "providers.") }):
		return "providers"
	case slices.ContainsFunc(fields, func(field string) bool { return strings.HasPrefix(field, "placeSearch.") }):
		return "map"
	}
	return "email"
}

var settingsStepScreen = map[string]string{"providers": "serverSignIn", "map": "serverMap", "email": "serverEmail"}

// personStepSectionOperations are, by settings section, the operations
// whose steps can show there: each operation's section for every set of its
// secret fields a step may ask, so a section's list is read by operation in
// the database while the section itself is still decided here alone.
var personStepSectionOperations = func() map[string][]string {
	bySection := map[string][]string{}
	for id, operation := range personStepOperations {
		sections := map[string]bool{}
		for set := range 1 << len(operation.secrets) {
			fields := []string{}
			for index, field := range operation.secrets {
				if set&(1<<index) != 0 {
					fields = append(fields, field)
				}
			}
			sections[operation.section(fields)] = true
		}
		for section := range sections {
			bySection[section] = append(bySection[section], id)
		}
	}
	for _, ids := range bySection {
		slices.Sort(ids)
	}
	return bySection
}()

// personStepPatterns are the operations' own paths, as a call's path is
// matched to them.
var personStepPatterns = func() map[string]*regexp.Regexp {
	patterns := map[string]*regexp.Regexp{}
	for id := range personStepOperations {
		method, path, _ := strings.Cut(id, " ")
		patterns[id] = userKeyRoute(method, path, userKeyScopeRead, userKeyPlacePerson, "").pattern
	}
	return patterns
}()

// personStepOperationOf is the listed operation a call's method and path
// (below apiRoot) are, its id and what it is; "" when none.
func personStepOperationOf(method, path string) (string, personStepOperation) {
	for id, pattern := range personStepPatterns {
		if strings.HasPrefix(id, method+" ") && pattern.MatchString(path) {
			return id, personStepOperations[id]
		}
	}
	return "", personStepOperation{}
}

// The scheduled backup's step asks the keys of the place its body names.
func scheduleStepFields(_ core.App, body map[string]any) []string {
	fields := []string{}
	ask := func(field string) {
		if personStepString(body, field) == "" {
			fields = append(fields, field)
		}
	}
	switch personStepString(body, "target") {
	case scheduledBackupTargetS3:
		ask("s3AccessKey")
		ask("s3SecretKey")
	case scheduledBackupTargetWebDAV:
		ask("webdavPassword")
	}
	return fields
}

// scheduleStepKeep gives the person a recovery code to keep where the body
// turns on a place off this computer the backups have no way to open yet,
// and no password is asked or given: the first backup needs one.
func scheduleStepKeep(app core.App, body map[string]any, fields []string) string {
	enabled, _ := body["enabled"].(bool)
	if !enabled || personStepString(body, "password") != "" || slices.Contains(fields, "password") {
		return ""
	}
	wanted := storedScheduledBackup{Target: personStepString(body, "target"), Folder: personStepString(body, "folder")}
	if !backupProtectionRequired(wanted) {
		return ""
	}
	current, err := loadScheduledBackup(app)
	if err != nil {
		return ""
	}
	if code, password := backupProtectionWays(current); code || password {
		return ""
	}
	return personStepKeepRecoveryCode
}

func scheduleStepPlace(body map[string]any, _ string) string {
	switch personStepString(body, "target") {
	case scheduledBackupTargetS3:
		return strings.Trim(personStepString(body, "s3Endpoint")+" · "+personStepString(body, "s3Bucket"), " ·")
	case scheduledBackupTargetWebDAV:
		return personStepString(body, "webdavUrl")
	}
	return personStepString(body, "folder")
}

// personStepString is a body's text at a dotted path, "" where there is none.
func personStepString(body map[string]any, path string) string {
	value, _ := personStepValue(body, path).(string)
	return strings.TrimSpace(value)
}

func personStepValue(body map[string]any, path string) any {
	var current any = body
	for part := range strings.SplitSeq(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

// setPersonStepValue sets a body's value at a dotted path, making the
// objects on the way.
func setPersonStepValue(body map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := body
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

// titleOf is what a step with these fields sets up, as the person reads it.
func (operation personStepOperation) titleOf(fields []string) mcpWords {
	if title, named := operation.sectionTitles[operation.section(fields)]; named {
		return title
	}
	return operation.title
}

// A proxy's password is asked where the body signs in to it with a user name.
func proxyStepFields(_ core.App, body map[string]any) []string {
	if personStepString(body, "username") == "" || personStepGiven(body, "password") {
		return nil
	}
	return []string{"password"}
}

// A tunnel's token is asked for the way that runs on Cloudflare's token.
func tunnelStepFields(_ core.App, body map[string]any) []string {
	if personStepString(body, "mode") != serverTunnelToken || personStepGiven(body, "token") {
		return nil
	}
	return []string{"token"}
}

// serviceStepFields asks the one secret of the way the body signs in to the
// service (mcp_service_headers.go): its token — the body's own or under auth
// — a basic sign-in's password, or an API key's value.
func serviceStepFields(_ core.App, body map[string]any) []string {
	ask := func(field string) []string {
		if personStepGiven(body, field) {
			return nil
		}
		return []string{field}
	}
	switch personStepString(body, "auth.type") {
	case "":
		if personStepGiven(body, "auth.token") {
			return nil
		}
		return ask("token")
	case mcpServiceHeaderAuthBearer:
		return ask("auth.token")
	case mcpServiceHeaderAuthBasic:
		return ask("auth.password")
	case mcpServiceHeaderAuthAPIKey:
		return ask("auth.value")
	}
	return nil
}
