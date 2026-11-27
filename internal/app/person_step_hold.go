package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// An agent that may not handle secrets leaves them to its person (user
// decision 2026-11-22, 10:2x: only for such an agent — one that may puts a
// secret in itself with `leafen mcp secret`, never seeing it,
// mcp_secret_command.go). Its call to an operation a person step holds
// (person_step_operations.go), sent with its key or through leafen_call and
// saying "secretsFromPerson",
// doesn't run: Leafen keeps it as a step for the key's person, who types
// what the call left out on their own screen. true leaves them the secret
// fields the body doesn't give (and a recovery code to keep where the
// backups have no way to open yet) — one Leafen makes when it is left out, a
// new password, it makes as they open the step and shows them alone; a list
// names the fields. The key's own
// checks — its scope, its workspaces — have passed by then, so a step holds
// only what the key could have done itself.

const personStepMarker = "secretsFromPerson"

func registerPersonStepRoutes(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenPersonStepHold", Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 3, Func: holdPersonStep,
	})
	event.Router.GET(personStepsPath, listPersonSteps)
	event.Router.GET(personStepsPath+"/{stepId}", readPersonStep)
	event.Router.DELETE(personStepsPath+"/{stepId}", cancelPersonStep)
	event.Router.POST(personStepsPath+"/{stepId}/reveal", revealPersonStep)
	event.Router.POST(personStepsPath+"/{stepId}/enter", enterPersonStep)
}

func holdPersonStep(event *core.RequestEvent) error {
	path := event.Request.URL.Path
	if event.Request.Method == http.MethodGet || !strings.HasPrefix(path, apiRoot+"/") {
		return event.Next()
	}
	key, keyed := userKeyOf(event)
	if !keyed || key.AppInstallID != "" {
		return event.Next()
	}
	id, operation := personStepOperationOf(event.Request.Method, strings.TrimPrefix(path, apiRoot))
	if id == "" {
		return event.Next()
	}
	raw, err := readUserKeyBody(event.Request)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePersonStepInvalid, "Couldn’t read the request.")
	}
	body := map[string]any{}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && json.Unmarshal(trimmed, &body) != nil {
		// The operation refuses it in its own words.
		return event.Next()
	}
	marker, asked := body[personStepMarker]
	if !asked {
		return event.Next()
	}
	delete(body, personStepMarker)
	if marker == false || marker == nil {
		encoded, _ := json.Marshal(body)
		event.Request.Body = userKeyReadBody{Reader: bytes.NewReader(encoded), Closer: event.Request.Body}
		event.Request.ContentLength = int64(len(encoded))
		return event.Next()
	}
	fields, refusal := personStepFieldsAsked(event.App, operation, body, marker)
	if refusal != "" {
		return apiError(event, http.StatusBadRequest, errorCodePersonStepInvalid, refusal)
	}
	keep := ""
	if operation.keepFor != nil {
		keep = operation.keepFor(event.App, body, fields)
	}
	if len(fields) == 0 && keep == "" && len(operation.shows) == 0 {
		return apiError(event, http.StatusBadRequest, errorCodePersonStepInvalid,
			"This call leaves nothing to the person: send it without secretsFromPerson, or name the fields they type.")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t keep the step.", err)
	}
	now := time.Now()
	step := storedPersonStep{ID: newPersonStepID(), OwnerID: key.OwnerID, ConnectionID: key.ConnectionID,
		ConnectionName: appOneLine(key.Name, 80), Operation: id, Path: strings.TrimPrefix(path, apiRoot), Body: string(encoded),
		Fields: personStepFieldsJSON(fields), Keep: keep, State: personStepWaiting,
		CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(personStepLifetime).UnixMilli()}
	if len(encoded) > maxPersonStepBody {
		return apiError(event, http.StatusBadRequest, errorCodePersonStepInvalid, "The request is too long to keep as a step.")
	}
	if err := savePersonStep(event.App, step); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t keep the step.", err)
	}
	// The person's own press on their screen is the yes an action asked
	// where it acts waits for: nothing acted unasked here.
	if pending, held := event.Request.Context().Value(userKeyPendingAskKey{}).(*userKeyPendingAsk); held && pending != nil {
		pending.asked = true
	}
	notifyPersonStep(event.App, step, operation)
	language := mcpScreenLanguage(event.App, key.OwnerID)
	return event.JSON(http.StatusAccepted, personStepAnswer{PersonStep: personStepViewOf(event.App, step, operation, language, now, true)})
}

// The body a step keeps is the agent's request less what the person types.
const maxPersonStepBody = 64 << 10

// personStepFieldsAsked is the fields a call leaves to the person: true for
// those its operation picks, or a list of its secret fields, none of them
// given in the body; else why not.
func personStepFieldsAsked(app core.App, operation personStepOperation, body map[string]any, marker any) ([]string, string) {
	fields := []string{}
	switch value := marker.(type) {
	case bool:
		switch {
		case operation.fieldsFor != nil:
			fields = operation.fieldsFor(app, body)
		default:
			for _, field := range operation.secrets {
				if personStepGiven(body, field) {
					continue
				}
				fields = append(fields, field)
			}
		}
		// What Leafen makes when it is left out isn't typed: the person
		// sees what it made.
		fields = slices.DeleteFunc(fields, func(field string) bool { return slices.Contains(operation.made, field) })
	case []any:
		for _, item := range value {
			field, ok := item.(string)
			if !ok || !slices.Contains(operation.secrets, field) {
				return nil, "secretsFromPerson takes true or a list of the fields the person types: " + strings.Join(operation.secrets, ", ") + "."
			}
			fields = append(fields, field)
		}
	default:
		return nil, "secretsFromPerson takes true or a list of the fields the person types."
	}
	for _, field := range fields {
		if personStepGiven(body, field) {
			return nil, "Leave " + field + " out of the body: the person types it."
		}
	}
	return fields, ""
}

// personStepGiven says a body gives a field: a value that isn't empty.
func personStepGiven(body map[string]any, field string) bool {
	switch value := personStepValue(body, field).(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	}
	return true
}

type personStepAnswer struct {
	PersonStep personStepView `json:"personStep"`
}

var (
	personStepNoticeTitle  = mcpWords{"%s: 직접 넣을 것이 있습니다", "%s: something only you can enter"}
	personStepNoticeKeep   = mcpWords{"%s: 보관할 코드가 있습니다", "%s: a code for you to keep"}
	personStepNoticeShow   = mcpWords{"%s: 나에게만 보일 값이 있습니다", "%s: something shown to you alone"}
	personStepNoticeAgent  = mcpWords{"%s가 준비했습니다. 설정에서 직접 넣고 저장하면 켜집니다.", "%s prepared it. Enter it yourself in Settings and save to turn it on."}
	personStepNoticeShown  = mcpWords{"%s가 요청했습니다. 설정에서 열면 나에게만 보입니다.", "%s asked for it. It shows only to you when you open it in Settings."}
	personStepAgentUnnamed = mcpWords{"에이전트", "An agent"}
	// A named agent, as the Korean sentence's subject.
	personStepAgentNamed = mcpWords{"%s(에이전트)", "%s"}
)

// notifyPersonStep puts the step in its person's inbox — and so in their
// mail and push as their settings say —, opening the settings place where
// they type or keep what it asks: one notice for each operation, which a
// newer step for it takes over as it takes the older step's place.
func notifyPersonStep(app core.App, step storedPersonStep, operation personStepOperation) {
	workspaceID, err := personNoticeWorkspace(app, step.OwnerID, "")
	if err != nil || workspaceID == "" {
		return
	}
	language, err := serverTextLanguage(app, step.OwnerID, nil)
	if err != nil {
		language = accountLanguageEn
	}
	agent := personStepAgentUnnamed.in(language)
	if step.ConnectionName != "" {
		agent = fmt.Sprintf(personStepAgentNamed.in(language), step.ConnectionName)
	}
	title, preview := personStepNoticeTitle, personStepNoticeAgent
	if len(step.fields()) == 0 {
		title = personStepNoticeKeep
		if len(operation.shows) > 0 {
			title, preview = personStepNoticeShow, personStepNoticeShown
		}
	}
	fields := step.fields()
	if _, err := upsertProductNotifications(context.Background(), app, []productNotificationProjection{{
		ID: appNoticeID("person-step", step.Operation, step.Path, step.OwnerID), EventID: appNoticeID("person-step-event", step.ID),
		WorkspaceID: workspaceID, UserID: step.OwnerID, ActivityKey: "person-step:" + step.Operation + " " + step.Path, Kind: "system",
		PageID: agentNoticePagePrefix + "person-step", Title: fmt.Sprintf(title.in(language), operation.titleOf(fields).in(language)),
		Preview: appOneLine(fmt.Sprintf(preview.in(language), agent), 300),
		Target:  personStepTarget(operation, fields), OccurredAt: step.CreatedAt, Personal: true,
	}}, step.CreatedAt); err != nil {
		app.Logger().Warn("leafen person step notice failed", "step", step.ID, "error", err)
	}
}

// personStepTarget is the settings place a step's notice opens.
func personStepTarget(operation personStepOperation, fields []string) string {
	if operation.personal {
		return "/settings?section=" + operation.section(fields)
	}
	return "/admin/settings?section=" + operation.section(fields)
}
