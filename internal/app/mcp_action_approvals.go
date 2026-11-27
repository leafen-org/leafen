package app

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Before an action the key rules confirm (user_key_routes.go) — something
// deleted for good, sharing, permissions or publishing, members and guests,
// accounts and backups, an organization — an MCP token waits for its
// person's yes (decided 2026-11-10), whether its agent calls through
// Leafen's MCP (leafen_call) or sends the API request itself: a code the
// agent sends back by itself is no person's yes. The person is asked inside
// their conversation with the agent (person_ask.go, decided 2026-11-13), in
// their words (mcp_action_approval_words.go) — called one that can't be
// undone only when it can't: the agent asks them and passes the yes on with
// the request's id. A yes is for once, or for an hour on that token. The
// token's "모두 맡기기(묻지 않음)" turns the asking off: the token then
// confirms by itself as before. Every token has it off until its person
// turns it on on its page, or its agent does, asking them once
// (mcp_token_hand_over.go) — those an agent makes too. An administrator's
// safeguards ask every time either way (user_key_admin_routes.go).

const (
	errorCodeUserKeyApprovalRequired = "LEAFEN_USER_KEY_APPROVAL_REQUIRED"
	errorCodeUserKeyApprovalRefused  = "LEAFEN_USER_KEY_APPROVAL_REFUSED"
)

// userKeyMCPCall is an agent's call through Leafen's MCP: its connection.
type userKeyMCPCall struct {
	Authority mcpAuthority
}

// mcpActionApprovalPending is a request waiting for the person, as the agent
// hears of it: its id, the action's route, what kind it is, whether it can't
// be undone, and the question for the person in their words.
type mcpActionApprovalPending struct {
	ApprovalID string    `json:"approvalId"`
	ExpiresAt  int64     `json:"expiresAt"`
	Operation  string    `json:"operation"`
	Family     string    `json:"family,omitempty"`
	Request    string    `json:"request"`
	Final      bool      `json:"final"`
	Ask        personAsk `json:"ask"`
	Message    string    `json:"message"`
}

type mcpActionApprovalAnswer struct {
	Error    errorBody                `json:"error"`
	Approval mcpActionApprovalPending `json:"approval"`
}

// mcpTokenAsksAction says whether the token waits for its person before an
// action the key rules confirm.
func mcpTokenAsksAction(app core.App, connectionID string) (bool, error) {
	asks := 1
	err := app.DB().NewQuery(`SELECT [[asks_irreversible]] FROM {{leafen_mcp_connections}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": connectionID}).Row(&asks)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return asks != 0, err
}

// mcpActionFamily is what kind of action a route is, as its person reads it:
// deleting for good, members and guests, an account or the server's backups,
// an organization's administration, or what it shares and lets others do;
// "" for one none of these names.
func mcpActionFamily(operation string) string {
	method, path, _ := strings.Cut(operation, " ")
	ends := func(suffixes ...string) bool {
		return slices.ContainsFunc(suffixes, func(suffix string) bool { return strings.HasSuffix(path, suffix) })
	}
	switch {
	case strings.HasPrefix(path, "/organizations/"):
		return "organization"
	case strings.Contains(path, "permanent") || ends("/purge", "/trash/empty", "/file-cleanup", "/files/{fileId}/delete") ||
		strings.Contains(path, "/deleted-") && ends("/delete") || method == http.MethodDelete && path == "/workspaces/{workspaceId}":
		return "delete"
	case strings.HasPrefix(path, "/admin/") || ends("/move-out"):
		return "account"
	case strings.Contains(path, "/members") || strings.Contains(path, "/guests/") || strings.Contains(path, "/invitations/") ||
		ends("/owner-transfer"):
		return "members"
	case strings.Contains(path, "/access-requests/") || ends("/permissions", "/public-mutations", "/share-inheritance",
		"/app-connections", "/agent-grant-scope", "/page-rules", "/access", "/page-guest-links"):
		return "sharing"
	}
	return ""
}

// mcpActionAuthority is the token as the approval binds it: an MCP call's
// own connection, or the key a request to the API carried, at this server's
// address.
func mcpActionAuthority(event *core.RequestEvent, key userKeyAuthority) mcpAuthority {
	if key.MCP != nil {
		return key.MCP.Authority
	}
	authority := mcpAuthority{ConnectionID: key.ConnectionID, PrincipalID: key.OwnerID, ClientName: key.Name,
		Scope: key.Scope, WorkspaceIDs: key.WorkspaceIDs, AllWorkspaces: key.AllWorkspaces, Agent: key.Agent}
	if origin, err := outwardServerOrigin(event.Request); err == nil {
		authority.Resource = origin + mcpRoutePath
	}
	return authority
}

var (
	mcpActionAskDo      = mcpWords{"에이전트가 리펜에서 이 작업을 하려고 합니다: ", "An agent wants to do this in Leafen: "}
	mcpActionAskDoFinal = mcpWords{"에이전트가 리펜에서 되돌릴 수 없는 작업을 하려고 합니다: ", "An agent wants to do this in Leafen, and it can’t be undone: "}
	mcpActionAskEffect  = mcpWords{"이 MCP 토큰은 ‘모두 맡기기(묻지 않음)’가 꺼져 있어 영구 삭제, 공유·공개·권한 바꾸기, 멤버·게스트 바꾸기, 계정·백업·조직 관리를 하기 전에 묻습니다.",
		"This MCP token asks before deleting for good, changing sharing, publishing, permissions, members or guests, and managing accounts, backups or the organization, as its “Hand it all over (don’t ask)” is off."}
	mcpActionAskForHour = mcpWords{"이 연결에서 이 작업을 1시간 동안 묻지 않고 합니다.", "This connection does this action without asking for an hour."}
	// The fields a request sends as they already are, counted after what it changes.
	mcpActionAskUnchanged = mcpWords{"그대로 두는 칸: %d개", "Fields left as they are: %d"}
	// A route asked every time, however the token is set: an administrator's safeguard.
	mcpActionAskEffectEveryTime = mcpWords{"관리자 안전장치라 에이전트가 바꿀 때는 ‘모두 맡기기’를 켠 토큰이어도 언제나 사람에게 묻습니다.",
		"This is an administrator’s safeguard: an agent changing it always asks its person, even with “Hand it all over” on."}
	// What a yes lets happen, for a route asked every time that says more.
	mcpActionAskEffects = func() map[string]mcpWords {
		effects := map[string]mcpWords{"PUT " + mcpHandsAllOverPath: mcpHandsAllOverAskEffect}
		maps.Copy(effects, mcpActionOwnAccountEffects)
		maps.Copy(effects, mcpConnectionSettingsAskEffects)
		return effects
	}()
)

// mcpActionAsk is the question for an action, in the person's language:
// what it does, to what, each value it sets with what it is now.
func mcpActionAsk(app core.App, current func(path string) map[string]any, personID, operation, preview string) personAsk {
	language, err := serverTextLanguage(app, personID, nil)
	if err != nil {
		language = accountLanguageEn
	}
	words := describeMCPAction(app, current, personID, operation, preview, language)
	do := mcpActionAskDo
	if words.Final {
		do = mcpActionAskDoFinal
	}
	lines := []string{do.in(language) + mcpActionLine(words)}
	for _, change := range words.Changes {
		if change.Before != "" {
			lines = append(lines, change.Field+": "+change.Before+" → "+change.After)
		} else {
			lines = append(lines, change.Field+": "+change.After)
		}
	}
	if words.Unchanged > 0 {
		lines = append(lines, fmt.Sprintf(mcpActionAskUnchanged.in(language), words.Unchanged))
	}
	return personAsk{Question: strings.Join(lines, "\n"), Effect: mcpActionAskEffect.in(language), Input: preview,
		ForHour: mcpActionAskForHour.in(language), Language: language}
}

// requireMCPActionApproval runs the call when its person said yes — to this
// request, or to this action on the token for an hour — and otherwise
// answers that it waits for them (handled), or that they declined it.
func requireMCPActionApproval(event *core.RequestEvent, key userKeyAuthority, rule userKeyRule, inputHash string, body []byte) (bool, error) {
	app := event.App
	authority := mcpActionAuthority(event, key)
	operation := event.Request.Method + " " + rule.path
	request := event.Request.Method + " " + strings.TrimPrefix(event.Request.URL.RequestURI(), apiRoot)
	if !rule.everyTime && mcpServiceAllowedForHour(app, authority, "", operation) {
		return false, nil
	}
	// The request as the person reads it and as it is kept, its secrets hidden.
	preview := request
	if len(body) > 0 {
		preview += "\n" + mcpActionHideSecrets(string(body))
	}
	call := personAskCall{PassedOn: strings.TrimSpace(event.Request.Header.Get(userKeyConfirmationHeader))}
	// What the request's path holds now, read with the token as it asks.
	reader := key
	reader.MCP = nil
	current := func(path string) map[string]any {
		read, err := callNativeOperation(event.Request.Context(), reader, http.MethodGet, path, "", nil, "")
		found, isObject := read.Body.(map[string]any)
		if err != nil || read.Status != http.StatusOK || !isObject {
			return nil
		}
		return found
	}
	if refuse, found := mcpActionRefusedFirst[operation]; found {
		if status, code, message := refuse(current); status != 0 {
			return true, apiError(event, status, code, message)
		}
	}
	ask := mcpActionAsk(app, current, authority.owner(), operation, preview)
	if rule.everyTime {
		ask.Effect, ask.ForHour = mcpActionAskEffectEveryTime.in(ask.Language), ""
		if effect, found := mcpActionAskEffects[operation]; found {
			ask.Effect = effect.in(ask.Language)
		}
	}
	outcome, err := askPerson(mcpApprovalRequests{app: app, authority: authority, tool: operation, inputHash: inputHash, preview: preview},
		call, ask)
	switch {
	case errors.Is(err, errMCPToolInvalid):
		return true, apiError(event, http.StatusForbidden, errorCodeUserKeyApprovalRefused, strings.TrimPrefix(err.Error(), errorCodeMCPToolInvalid+": "))
	case err != nil:
		return true, apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn't ask the person.", err)
	case outcome.Proceed:
		return false, nil
	case outcome.Declined:
		return true, mcpActionDeclined(event, call, ask, operation, request, body)
	}
	// It waits for the person, asked by the agent in the conversation.
	message := personAskConversation(ask, "send the same request again with the "+userKeyConfirmationHeader+" header set to "+outcome.ID+".")
	if key.MCP != nil {
		message = personAskConversation(ask, "call leafen_call again with the same request and confirmation set to "+outcome.ID+".")
	}
	return true, event.JSON(http.StatusPreconditionRequired, mcpActionApprovalAnswer{
		Error: errorBody{Code: errorCodeUserKeyApprovalRequired, Message: message, Reference: newDiagnosticReference()},
		Approval: mcpActionApprovalPending{ApprovalID: outcome.ID, ExpiresAt: outcome.ExpiresAt, Operation: operation,
			Family: mcpActionFamily(operation), Request: request, Final: mcpActionFinal(operation, body), Ask: ask, Message: message},
	})
}
