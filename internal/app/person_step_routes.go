package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A person step's routes (person_steps.go): its person's screen lists the
// steps waiting for them and types or keeps what each asks
// (person_step_apply.go); the agent that made one, or any of the person's
// keys, reads where it is and may cancel it, and never what was typed or
// shown.

// personStepView is a step as its person's screen and agents read it.
type personStepView struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Operation string `json:"operation"`
	// What it sets up and where it acts, in the person's words, without a secret.
	Title string `json:"title"`
	Place string `json:"place,omitempty"`
	// The settings section the person finds it in.
	Section string `json:"section"`
	// The fields the person types, by the body's dotted names.
	Fields []string `json:"fields"`
	// What Leafen makes for the person to keep first, and whether they have.
	Keep string `json:"keep,omitempty"`
	Kept bool   `json:"kept,omitempty"`
	// The operation's answer is shown to the person alone.
	Shows bool `json:"shows,omitzero"`
	// The agent's connection that asked.
	Agent       string `json:"agent,omitempty"`
	FailureCode string `json:"failureCode,omitempty"`
	Failure     string `json:"failure,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	ExpiresAt   int64  `json:"expiresAt"`
	// For an agent: the way to it on the person's screen, and what to do next.
	Where string `json:"where,omitempty"`
	Next  string `json:"next,omitempty"`
}

func personStepViewOf(app core.App, step storedPersonStep, operation personStepOperation, language string, now time.Time, forAgent bool) personStepView {
	fields := step.fields()
	view := personStepView{ID: step.ID, State: step.state(now), Operation: step.Operation, Title: operation.titleOf(fields).in(language),
		Section: operation.section(fields), Fields: fields, Keep: step.Keep, Kept: step.KeptAt > 0, Shows: personStepShows(operation, fields),
		Agent: step.ConnectionName, FailureCode: step.FailureCode, Failure: step.Failure, CreatedAt: step.CreatedAt, ExpiresAt: step.ExpiresAt}
	switch {
	case operation.place != nil:
		if body, err := personStepBody(step); err == nil {
			view.Place = appOneLine(operation.place(body, step.Path), 200)
		}
	case operation.placeAt != nil:
		view.Place = appOneLine(operation.placeAt(app, step.Path), 200)
	}
	if forAgent {
		view.Where = mcpScreen(language, operation.screen(fields)...)
		view.Next = personStepNext(view)
	}
	return view
}

// personStepShows says a step shows its operation's answer to the person
// alone: one that asks them to type no field.
func personStepShows(operation personStepOperation, fields []string) bool {
	return len(operation.shows) > 0 && len(fields) == 0
}

// personStepNext is what the agent does next, by the step's state.
func personStepNext(view personStepView) string {
	follow := " Follow it with GET /person-steps/" + view.ID + " (leafen_call): its state turns done once it went through, " +
		"and failure says why a try of theirs didn’t — they try again there."
	switch view.State {
	case personStepDone:
		return "Done: Leafen ran " + view.Operation + " as the person with what only they gave. Check its result as usual."
	case personStepCanceled:
		return "Canceled, or replaced by a newer step for the same operation."
	case personStepExpired:
		return "Nobody finished it in time. Send the call again with secretsFromPerson to make a new step."
	}
	asks := []string{}
	if len(view.Fields) > 0 {
		asks = append(asks, "type "+strings.Join(view.Fields, " and ")+" themselves")
	}
	if view.Keep == personStepKeepRecoveryCode && !view.Kept {
		asks = append(asks, "keep the recovery code Leafen shows them somewhere other than this server and press that they saved it")
	}
	if view.Shows {
		asks = append(asks, "open it to see what it makes — it shows only to them")
	}
	return "Leafen holds " + view.Operation + " until the person, logged in, goes to " + view.Where +
		" (their inbox has it too) and does what only they can: " + strings.Join(asks, ", and ") +
		". You never see it, so it isn’t asked for in the conversation. Left to do: where, to them, in their words." + follow
}

func personStepBody(step storedPersonStep) (map[string]any, error) {
	body := map[string]any{}
	if strings.TrimSpace(step.Body) == "" {
		return body, nil
	}
	return body, json.Unmarshal([]byte(step.Body), &body)
}

// personStepOf is the step a route names, when it is the caller's person's.
func personStepOf(event *core.RequestEvent) (storedPersonStep, personStepOperation, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return storedPersonStep{}, personStepOperation{}, false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	step, err := loadPersonStep(event.App, event.Request.PathValue("stepId"))
	if errors.Is(err, errPersonStepMissing) || err == nil && step.OwnerID != user.Id {
		return step, personStepOperation{}, false, apiError(event, http.StatusNotFound, errorCodePersonStepNotFound, "No such step.")
	}
	if err != nil {
		return step, personStepOperation{}, false, apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t read the step.", err)
	}
	operation, listed := personStepOperations[step.Operation]
	if !listed {
		return step, personStepOperation{}, false, apiError(event, http.StatusNotFound, errorCodePersonStepNotFound, "No such step.")
	}
	return step, operation, true, nil
}

func personStepLanguage(event *core.RequestEvent, userID string) (string, bool) {
	_, keyed := userKeyOf(event)
	return mcpScreenLanguageIn(event.Request.Context(), event.App, userID), keyed
}

type personStepList struct {
	Steps []personStepView `json:"steps"`
	// NextCursor reads the steps after these, while HasMore says some wait.
	NextCursor *string `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
}

// personStepPageSize is how many steps a list answers at most, and when
// its limit says none.
const personStepPageSize = 50

// listPersonSteps lists what waits for the person, newest first, a page at
// a time: every one of them, or a settings section's, read by its
// operations in the database (personStepSectionOperations), so no section
// is cut by another's and none past the first page is left unsaid.
func listPersonSteps(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	query := event.Request.URL.Query()
	limit := apiQueryLimit(query)
	if limit == 0 || limit > personStepPageSize {
		limit = personStepPageSize
	}
	after, valid := decodePersonStepCursor(query.Get("cursor"))
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodePersonStepInvalid, "The cursor isn't one this list gave: leave it out to read from the start.")
	}
	section := query.Get("section")
	var operations []string
	if section != "" {
		operations = append([]string{}, personStepSectionOperations[section]...)
	}
	now := time.Now()
	steps, more, err := waitingPersonSteps(event.App, user.Id, now, operations, after, limit)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t read the steps.", err)
	}
	language, keyed := personStepLanguage(event, user.Id)
	answer := personStepList{Steps: []personStepView{}, HasMore: more}
	for _, step := range steps {
		operation, listed := personStepOperations[step.Operation]
		if !listed {
			continue
		}
		// An operation saving several sections' settings is read for each
		// it may be in; its step shows in the one its fields are.
		view := personStepViewOf(event.App, step, operation, language, now, keyed)
		if section == "" || view.Section == section {
			answer.Steps = append(answer.Steps, view)
		}
	}
	if more {
		cursor := encodePersonStepCursor(steps[len(steps)-1])
		answer.NextCursor = &cursor
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, answer)
}

const personStepCursorPrefix = "psc_"

func encodePersonStepCursor(step storedPersonStep) string {
	return personStepCursorPrefix + base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(step.CreatedAt, 10)+":"+step.ID))
}

// decodePersonStepCursor is a list's cursor, nil for none; false for one
// no list gave.
func decodePersonStepCursor(raw string) (*personStepCursor, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	encoded, found := strings.CutPrefix(raw, personStepCursorPrefix)
	if !found || len(raw) > 128 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false
	}
	at, id, found := strings.Cut(string(payload), ":")
	createdAt, err := strconv.ParseInt(at, 10, 64)
	if !found || err != nil || createdAt < 1 || !strings.HasPrefix(id, "lps_") {
		return nil, false
	}
	return &personStepCursor{CreatedAt: createdAt, ID: id}, true
}

func readPersonStep(event *core.RequestEvent) error {
	step, operation, ok, err := personStepOf(event)
	if !ok {
		return err
	}
	language, keyed := personStepLanguage(event, step.OwnerID)
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, personStepAnswer{PersonStep: personStepViewOf(event.App, step, operation, language, time.Now(), keyed)})
}

// cancelPersonStep ends a waiting step: its agent changed its mind, or the
// person doesn't want it.
func cancelPersonStep(event *core.RequestEvent) error {
	step, operation, ok, err := personStepOf(event)
	if !ok {
		return err
	}
	now := time.Now()
	if step.state(now) == personStepWaiting {
		if err := endPersonStep(event.App, step.ID, personStepCanceled, now); err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t cancel the step.", err)
		}
		step.State = personStepCanceled
	}
	language, keyed := personStepLanguage(event, step.OwnerID)
	return event.JSON(http.StatusOK, personStepAnswer{PersonStep: personStepViewOf(event.App, step, operation, language, now, keyed)})
}

// personStepWaitingOr is whether the step still waits, or false when it
// refused one that no longer does, saying so; err is then that answer's write.
func personStepWaitingOr(event *core.RequestEvent, step storedPersonStep) (bool, error) {
	if state := step.state(time.Now()); state != personStepWaiting {
		return false, apiError(event, http.StatusConflict, errorCodePersonStepEnded, fmt.Sprintf("This step is %s.", state))
	}
	return true, nil
}
