package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// What the person does on their screen for a step (person_steps.go), signed
// in — never with a key, so no agent types or reads it: they open what it
// shows them alone or the recovery code to keep, and enter the fields it
// asks, and Leafen runs the agent's operation as them with those fields,
// through the same router, checks, saving and history as their own screen's
// request. A refusal is answered as the operation gave it, kept on the step
// for the agent to read, and the person tries again in the same place.

const maxPersonStepValue = 4096

// personStepShownKeeper holds the recovery code each step showed its person
// until they keep it: in memory only, so nothing is kept of it once kept,
// and a restart before then shows a new one.
type personStepShownKeeper struct {
	mu    sync.Mutex
	shown map[string]string
}

func personStepShownOf(app core.App) *personStepShownKeeper {
	return app.Store().GetOrSet("leafen.personStepShown", func() any {
		return &personStepShownKeeper{shown: map[string]string{}}
	}).(*personStepShownKeeper)
}

func forgetPersonStepShown(app core.App, id string) {
	keeper := personStepShownOf(app)
	keeper.mu.Lock()
	delete(keeper.shown, id)
	keeper.mu.Unlock()
}

// recoveryCode is the code the step shows, made the first time it is asked.
func (keeper *personStepShownKeeper) recoveryCode(id string, create bool) (string, error) {
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	if code := keeper.shown[id]; code != "" || !create {
		return code, nil
	}
	code, _, err := newScheduledBackupCode()
	if err == nil {
		keeper.shown[id] = code
	}
	return code, err
}

type personStepRevealAnswer struct {
	PersonStep personStepView `json:"personStep"`
	// What the person alone sees: the recovery code to keep, or the fields
	// of the operation's answer it shows.
	Shown map[string]string `json:"shown"`
}

// revealPersonStep shows the person what the step makes for them alone.
func revealPersonStep(event *core.RequestEvent) error {
	user, ok, err := signedInPerson(event)
	if !ok {
		return err
	}
	step, operation, ok, err := personStepOf(event)
	if !ok {
		return err
	}
	if waiting, err := personStepWaitingOr(event, step); !waiting {
		return err
	}
	if strings.HasPrefix(step.Path, "/admin/") && !authorizeAdministratorRequest(event) {
		return nil
	}
	now := time.Now()
	language := mcpScreenLanguageIn(event.Request.Context(), event.App, user.Id)
	event.Response.Header().Set("Cache-Control", "no-store")
	switch {
	case step.Keep == personStepKeepRecoveryCode && step.KeptAt == 0:
		code, err := personStepShownOf(event.App).recoveryCode(step.ID, true)
		if err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t make the recovery code.", err)
		}
		return event.JSON(http.StatusOK, personStepRevealAnswer{PersonStep: personStepViewOf(event.App, step, operation, language, now, false),
			Shown: map[string]string{"recoveryCode": code}})
	case personStepShows(operation, step.fields()):
		body, err := personStepBody(step)
		if err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t read the step.", err)
		}
		status, answer, err := runPersonStepOperation(event, step, body)
		if err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t run the step.", err)
		}
		if status < 200 || status >= 300 {
			return refusePersonStepTry(event, step, status, answer)
		}
		fields := map[string]any{}
		_ = json.Unmarshal(answer, &fields)
		shown := map[string]string{}
		for _, name := range operation.shows {
			if value, ok := fields[name].(string); ok {
				shown[name] = value
			}
		}
		if err := endPersonStep(event.App, step.ID, personStepDone, now); err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t save the step.", err)
		}
		step.State = personStepDone
		return event.JSON(http.StatusOK, personStepRevealAnswer{PersonStep: personStepViewOf(event.App, step, operation, language, now, false), Shown: shown})
	}
	return apiError(event, http.StatusBadRequest, errorCodePersonStepInvalid, "This step shows nothing: it asks for fields.")
}

type personStepEntry struct {
	// The fields the step asks, as the person typed them.
	Values map[string]string `json:"values"`
	// The person kept the recovery code the step showed them.
	Kept bool `json:"kept"`
}

type personStepEntered struct {
	PersonStep personStepView `json:"personStep"`
	// The operation's own answer, as their screen's request gets it.
	Answer json.RawMessage `json:"answer,omitempty"`
}

// enterPersonStep takes what the person typed and kept, and runs the
// agent's operation as them with it.
func enterPersonStep(event *core.RequestEvent) error {
	user, ok, err := signedInPerson(event)
	if !ok {
		return err
	}
	step, operation, ok, err := personStepOf(event)
	if !ok {
		return err
	}
	if waiting, err := personStepWaitingOr(event, step); !waiting {
		return err
	}
	input := personStepEntry{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePersonStepInvalid, "Send the fields the step asks as values.")
	}
	fields := step.fields()
	for name, value := range input.Values {
		if !slices.Contains(fields, name) || len(value) > maxPersonStepValue {
			return apiError(event, http.StatusBadRequest, errorCodePersonStepInvalid, "Send only the fields the step asks.")
		}
	}
	for _, name := range fields {
		if strings.TrimSpace(input.Values[name]) == "" {
			return apiError(event, http.StatusBadRequest, errorCodePersonStepInvalid, "Fill in every field the step asks.")
		}
	}
	body, err := personStepBody(step)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t read the step.", err)
	}
	now := time.Now()
	language := mcpScreenLanguageIn(event.Request.Context(), event.App, user.Id)
	if step.Keep == personStepKeepRecoveryCode && step.KeptAt == 0 {
		if handled, err := keepPersonStepRecoveryCode(event, step, input.Kept, body); handled {
			return err
		}
		step.KeptAt = now.UnixMilli()
		if operation.keepOnly {
			if err := endPersonStep(event.App, step.ID, personStepDone, now); err != nil {
				return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t save the step.", err)
			}
			step.State = personStepDone
			return event.JSON(http.StatusOK, personStepEntered{PersonStep: personStepViewOf(event.App, step, operation, language, now, false)})
		}
	}
	for name, value := range input.Values {
		setPersonStepValue(body, name, value)
	}
	status, answer, err := runPersonStepOperation(event, step, body)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t run the step.", err)
	}
	if status < 200 || status >= 300 {
		return refusePersonStepTry(event, step, status, answer)
	}
	if err := endPersonStep(event.App, step.ID, personStepDone, now); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t save the step.", err)
	}
	step.State, step.FailureCode, step.Failure = personStepDone, "", ""
	entered := personStepEntered{PersonStep: personStepViewOf(event.App, step, operation, language, now, false)}
	if json.Valid(answer) {
		entered.Answer = answer
	}
	return event.JSON(http.StatusOK, entered)
}

// keepPersonStepRecoveryCode turns on the recovery code the step showed,
// once the person says they kept it; handled when it answered instead.
func keepPersonStepRecoveryCode(event *core.RequestEvent, step storedPersonStep, kept bool, body map[string]any) (bool, error) {
	if !kept {
		return true, apiError(event, http.StatusBadRequest, errorCodePersonStepInvalid,
			"Keep the recovery code somewhere other than this server first, then say you saved it.")
	}
	if !authorizeAdministratorRequest(event) {
		return true, nil
	}
	code, err := personStepShownOf(event.App).recoveryCode(step.ID, false)
	if err != nil || code == "" {
		return true, apiError(event, http.StatusConflict, errorCodePersonStepEnded,
			"Leafen started again since the recovery code was shown, so it was made again: keep the new one.")
	}
	before, err := loadScheduledBackup(event.App)
	if err != nil {
		return true, apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	if before.SetByEnvironment {
		return true, apiError(event, http.StatusConflict, errorCodeScheduledBackupSetByEnvironment, "The server's environment sets the backup schedule.")
	}
	if err := rewrapScheduledBackupKey(event.App, scheduledBackupWrapCode, code); err != nil {
		return true, backupProtectionError(event, err)
	}
	publishBackupKeyRecord(event.Request.Context(), event.App)
	notifyScheduledBackupRunner(event.App)
	now := time.Now()
	if err := markPersonStepKept(event.App, step.ID, now); err != nil {
		return true, apiErrorFrom(event, http.StatusInternalServerError, errorCodePersonStepInvalid, "Couldn’t save the step.", err)
	}
	forgetPersonStepShown(event.App, step.ID)
	// The wrap moved the schedule's revision: a body made on the one
	// before follows it.
	if revision, ok := body["revision"].(float64); ok && int(revision) == before.Revision {
		if after, err := loadScheduledBackup(event.App); err == nil {
			body["revision"] = after.Revision
		}
	}
	return false, nil
}

// runPersonStepOperation runs the step's operation as the person whose
// request this is, with their own sign-in.
func runPersonStepOperation(event *core.RequestEvent, step storedPersonStep, body map[string]any) (int, []byte, error) {
	mux, _, err := nativeOperationRuntime()
	if err != nil {
		return 0, nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	method, _, _ := strings.Cut(step.Operation, " ")
	base, cancel := context.WithCancel(withAppRunID(context.Background(), appRunIDOf(event.Request.Context())))
	if arrival, err := requestArrivalOrigin(event.Request); err == nil {
		base = withInnerRequestArrival(base, arrival)
	}
	defer cancel()
	defer context.AfterFunc(event.Request.Context(), cancel)()
	request, err := newInnerRequest(base, method, apiRoot+step.Path, encoded)
	if err != nil {
		return 0, nil, err
	}
	request.RemoteAddr = event.Request.RemoteAddr
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", event.Request.Header.Get("Authorization"))
	request.Header.Set("User-Agent", event.Request.UserAgent())
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.Bytes(), nil
}

// refusePersonStepTry keeps why the person's try didn't go through and
// answers it as the operation did.
func refusePersonStepTry(event *core.RequestEvent, step storedPersonStep, status int, answer []byte) error {
	refusal := errorResponse{}
	_ = json.Unmarshal(answer, &refusal)
	if err := failPersonStep(event.App, step.ID, refusal.Error.Code, refusal.Error.Message, time.Now()); err != nil {
		event.App.Logger().Warn("leafen person step failure not kept", "step", step.ID, "error", err)
	}
	if !json.Valid(answer) {
		return apiError(event, status, firstNonempty(refusal.Error.Code, errorCodePersonStepInvalid), "The operation refused it.")
	}
	return event.Blob(status, "application/json", answer)
}
