//go:build !leafen_noapps

package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An agent's call to an app's tool that needs approval asks its person
// inside their conversation with the agent (person_ask.go, decided
// 2026-11-13), never through a Leafen page: the agent asks them and passes
// the yes on with the request's id. The question says which tool of which
// app, the input as the app's fields, and whether it changes things outside
// Leafen. Where the administrator oversees the organization's apps every
// tool asks, answered the same way (decided 2026-11-21: the setting that
// took only the program's own form answer went with the forms).

// appApprovalRequests are one call's requests in leafen_action_approvals,
// as the asking boundary keeps them (personAskRequests): bound to the
// connection, the person, the app, the tool and that input.
type appApprovalRequests struct {
	app       core.App
	usable    usableApp
	action    appManifestAction
	authority mcpAuthority
	inputHash string
	input     []byte
}

func (requests appApprovalRequests) params(extra dbx.Params) dbx.Params {
	params := dbx.Params{"install": requests.usable.Install.ID, "action": requests.action.Key, "person": requests.authority.owner(),
		"connection": requests.authority.ConnectionID, "input": requests.inputHash, "now": time.Now().UnixMilli()}
	for key, value := range extra {
		params[key] = value
	}
	return params
}

const appApprovalRequestMatch = `[[install_id]] = {:install} AND [[action]] = {:action} AND [[person_id]] = {:person}
	AND [[connection_id]] = {:connection} AND [[input_hash]] = {:input}`

func (requests appApprovalRequests) waiting() (string, int64, error) {
	found := struct {
		ID        string `db:"id"`
		ExpiresAt int64  `db:"expires_at"`
	}{}
	err := requests.app.DB().NewQuery(`SELECT [[id]], [[expires_at]] FROM {{leafen_action_approvals}}
		WHERE ` + appApprovalRequestMatch + ` AND [[status]] = 'pending' AND [[expires_at]] > {:now}
		ORDER BY [[created]] DESC LIMIT 1`).Bind(requests.params(nil)).One(&found)
	if err == nil {
		return found.ID, found.ExpiresAt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", 0, err
	}
	pending, err := requestAgentApproval(requests.app, requests.usable, requests.action, requests.authority, requests.inputHash, requests.input)
	return pending.ApprovalID, pending.ExpiresAt, err
}

func (requests appApprovalRequests) status(id string) string {
	found := struct {
		Status    string `db:"status"`
		ExpiresAt int64  `db:"expires_at"`
	}{}
	if err := requests.app.DB().NewQuery(`SELECT [[status]], [[expires_at]] FROM {{leafen_action_approvals}}
		WHERE [[id]] = {:id} AND ` + appApprovalRequestMatch).Bind(requests.params(dbx.Params{"id": id})).One(&found); err != nil {
		return ""
	}
	switch found.Status {
	case "declined", "used":
		return found.Status
	case "pending", "approved":
		// A yes not spent within the day runs out with the request.
		if found.ExpiresAt > time.Now().UnixMilli() {
			return found.Status
		}
	}
	return ""
}

// decide keeps the yes the agent passed on from the conversation.
func (requests appApprovalRequests) decide(id string, yes, forHour, _ bool) error {
	_, err := decideAgentApproval(requests.app, id, requests.authority.owner(), yes, forHour, "conversation")
	if errors.Is(err, errAgentApprovalNotWaiting) {
		return fmt.Errorf("%w: the request ran out; call without approval to ask again", errMCPToolInvalid)
	}
	return err
}

func (requests appApprovalRequests) spend(id string) error {
	result, err := requests.app.DB().NewQuery(`UPDATE {{leafen_action_approvals}} SET [[status]] = 'used', [[used_at]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = 'approved' AND ` + appApprovalRequestMatch).Bind(requests.params(dbx.Params{"id": id})).Execute()
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("%w: this yes was used; call without approval to ask again", errMCPToolInvalid)
	}
	return nil
}

var (
	appAgentAskRun      = mcpWords{"에이전트가 %s 앱의 “%s” 도구를 쓰려고 합니다.", "An agent wants to use “%[2]s” of the %[1]s app."}
	appAgentAskAsks     = mcpWords{"이 도구는 실행하기 전에 묻도록 되어 있습니다.", "This tool asks before it runs."}
	appAgentAskHigh     = mcpWords{"앱이 이 도구를 신중히 허용할 작업으로 표시했습니다.", "The app marks this tool as one to allow with care."}
	appAgentAskOutside  = mcpWords{"리펜 밖의 것을 바꿉니다.", "It changes things outside Leafen."}
	appAgentAskOverseen = mcpWords{"이 조직의 관리자가 앱의 모든 작업을 사람이 허용하도록 했습니다.",
		"This organization’s administrator has a person allow every action of its apps."}
	appAgentAskForHour = mcpWords{"이 연결에서 이 도구를 1시간 동안 묻지 않고 씁니다.", "This connection uses this tool without asking for an hour."}
)

// appAgentAsk is the question for an app's tool, in the person's language:
// which tool of which app, its input as the app's fields, and what a yes
// lets happen.
func appAgentAsk(app core.App, usable usableApp, action appManifestAction, authority mcpAuthority, input []byte) personAsk {
	overseen := appOverseen(app, usable.TenancyKey)
	language, err := serverTextLanguage(app, authority.owner(), nil)
	if err != nil {
		language = accountLanguageEn
	}
	inLanguage := func(text map[string]string, fallback string) string {
		if value := strings.TrimSpace(text[language]); value != "" {
			return appOneLine(value, 80)
		}
		return appOneLine(fallback, 80)
	}
	lines := []string{fmt.Sprintf(appAgentAskRun.in(language), inLanguage(usable.Manifest.Name, appAgentName(usable.Manifest.Name, usable.Install.Slug)),
		inLanguage(action.Name, appAgentName(action.Name, action.Key)))}
	fields := appApprovalFields(app, usable.Install.ID, action, input, usable.TenancyKey, authority.owner())
	for _, field := range fields {
		value := field.Value
		if field.Kind == "page" {
			value = appApprovalPageWords(value, language)
		}
		lines = append(lines, inLanguage(field.Labels, field.Label)+": "+appOneLine(value, 300))
	}
	effects := []string{}
	if overseen {
		effects = append(effects, appAgentAskOverseen.in(language))
	} else if action.Risk == "high" {
		effects = append(effects, appAgentAskHigh.in(language))
	}
	if action.ExternalEffect {
		effects = append(effects, appAgentAskOutside.in(language))
	}
	if len(effects) == 0 {
		effects = append(effects, appAgentAskAsks.in(language))
	}
	ask := personAsk{Question: strings.Join(lines, "\n"), Effect: strings.Join(effects, " "), Language: language}
	// The fields read the input; the input as sent goes only without them.
	if len(fields) == 0 {
		ask.Input = string(input)
	}
	if appApprovalHourAllowed(app, usable.TenancyKey, authority.ConnectionID) {
		ask.ForHour = appAgentAskForHour.in(language)
	}
	return ask
}

// appAgentName is an app's or an action's name for a person, in English
// where the maker gave it.
func appAgentName(name map[string]string, key string) string {
	if text := appEnglish(name); text != "" {
		return text
	}
	return key
}
