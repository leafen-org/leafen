//go:build !leafen_noapps

package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// An app's actions (docs/app-platform.md 5·10, T12·T13): the app's screen
// calls one by its key; the server checks the input against the action's
// schema and runs its worker function — as the app's account in the
// caller's organization, or with the person's own app token — passing who
// called. An action that needs approval runs only after the person approves
// it in Leafen itself: the screen, which holds only the app token, can't.

const appApprovalHeader = "X-Leafen-Approval"

var errAppApprovalInvalid = errors.New(errorCodeAppApprovalInvalid)

func appManifestActionByKey(manifest appManifest, key string) (appManifestAction, bool) {
	for _, action := range manifest.Actions {
		if action.Key == key {
			return action, true
		}
	}
	return appManifestAction{}, false
}

// validateAppActionInput checks an input against the action's schema; no
// schema takes any object.
func validateAppActionInput(action appManifestAction, input any) error {
	if len(action.Input) == 0 {
		return nil
	}
	schema := &jsonschema.Schema{}
	if err := json.Unmarshal(action.Input, schema); err != nil {
		return err
	}
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{})
	if err != nil {
		return err
	}
	return resolved.Validate(input)
}

func appApprovalTokenHash(token string) string {
	digest := sha256.Sum256([]byte("leafen-app-approval\x00" + token))
	return hex.EncodeToString(digest[:])
}

func requestAppApproval(app core.App, install storedAppInstall, action appManifestAction, personID, inputHash string, input []byte, tenancy string) (appApprovalRequest, error) {
	now := time.Now()
	request := appApprovalRequest{ApprovalID: security.RandomString(24), Action: action.Key, ActionName: action.Name, Risk: action.Risk,
		ExpiresAt: now.Add(appApprovalSeconds * time.Second).UnixMilli(), Fields: appApprovalFields(app, install.ID, action, input, tenancy, personID),
		AllowHour: appApprovalHourAllowed(app, tenancy, appApprovalChannelScreen), ExternalEffect: action.ExternalEffect}
	preview := string(input)
	if len(preview) > 600 {
		preview = preview[:600] + "…"
	}
	request.Preview = preview
	fields, _ := json.Marshal(request.Fields)
	_, err := app.DB().Insert(appApprovalsTable, dbx.Params{
		"id": request.ApprovalID, "install_id": install.ID, "action": action.Key, "person_id": personID, "input_hash": inputHash,
		"status": "pending", "created": now.UnixMilli(), "expires_at": request.ExpiresAt, "tenancy": tenancy, "fields": string(fields),
	}).Execute()
	return request, err
}

// decideAppApproval approves or declines the person's own pending request;
// an approval comes back as a token good once, for that input only.
func decideAppApproval(app core.App, installID, approvalID, personID string, approve bool) (string, error) {
	token, status, hash := "", "declined", ""
	if approve {
		token = "lap_ok_" + security.RandomString(40)
		status, hash = "approved", appApprovalTokenHash(token)
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_action_approvals}} SET [[status]] = {:status}, [[token_hash]] = {:hash}, [[decided_at]] = {:now}
		WHERE [[id]] = {:id} AND [[install_id]] = {:install} AND [[person_id]] = {:person} AND [[connection_id]] = ''
			AND [[status]] = 'pending' AND [[expires_at]] > {:now}`).
		Bind(dbx.Params{"status": status, "hash": hash, "now": time.Now().UnixMilli(), "id": approvalID, "install": installID, "person": personID}).Execute()
	if err != nil {
		return "", err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return "", errAppApprovalInvalid
	}
	return token, nil
}

// consumeAppApproval spends an approval on the call it was given for.
func consumeAppApproval(app core.App, installID, action, personID, inputHash, token string) error {
	result, err := app.DB().NewQuery(`UPDATE {{leafen_action_approvals}} SET [[status]] = 'used', [[used_at]] = {:now}
		WHERE [[token_hash]] = {:hash} AND [[install_id]] = {:install} AND [[action]] = {:action} AND [[person_id]] = {:person}
			AND [[input_hash]] = {:input} AND [[connection_id]] = '' AND [[status]] = 'approved' AND [[expires_at]] > {:now}`).Bind(dbx.Params{
		"now": time.Now().UnixMilli(), "hash": appApprovalTokenHash(token), "install": installID, "action": action,
		"person": personID, "input": inputHash,
	}).Execute()
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errAppApprovalInvalid
	}
	return nil
}

func registerAppActionRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+appPlatformActionRunPath, runAppActionRoute)
	event.Router.POST(apiRoot+appPlatformApprovalApprovePath, func(event *core.RequestEvent) error { return decideAppApprovalRoute(event, true) })
	event.Router.POST(apiRoot+appPlatformApprovalDeclinePath, func(event *core.RequestEvent) error { return decideAppApprovalRoute(event, false) })
	event.Router.POST(apiRoot+appPlatformKVPath, runAppKVRoute)
}

// runAppKVRoute runs a store command for the app whose token calls, in the
// organization the token was opened for.
func runAppKVRoute(event *core.RequestEvent) error {
	install, manifest, personID, ok, failure := appTokenInstall(event)
	if !ok {
		return failure
	}
	authority, _ := userKeyOf(event)
	request := appKVRequest{}
	raw, err := io.ReadAll(io.LimitReader(event.Request.Body, appPlatformInputMaxBytes+1))
	reason := readBodyRefusalReason(raw, err, appPlatformInputMaxBytes)
	if reason == "" {
		if err := json.Unmarshal(raw, &request); err != nil {
			reason = bodyRefusalReason("store command", err)
		}
	}
	if reason != "" {
		return apiError(event, http.StatusBadRequest, errorCodeAppKVRefused, refusalMessage("Send a store command up to 64 KB.", reason))
	}
	result, err := runAppKV(manifest, install.ID, authority.TenancyKey, personID, request, time.Now())
	switch {
	case err == nil:
		return event.JSON(http.StatusOK, appKVResult{Result: result})
	case errors.Is(err, errAppKVFull):
		return apiError(event, http.StatusInsufficientStorage, errorCodeAppKVFull, "The app's store is full.")
	case errors.Is(err, errAppRelayLimited):
		return apiError(event, http.StatusTooManyRequests, errorCodeAppRelayLimited, "Too many store commands; try again in a minute.")
	case errors.Is(err, errAppKVRefused):
		return apiError(event, http.StatusBadRequest, errorCodeAppKVRefused, appKVRefusalMessage(err))
	}
	return appPlatformFailure(event, err)
}

func runAppActionRoute(event *core.RequestEvent) error {
	install, manifest, personID, ok, failure := appTokenInstall(event)
	if !ok {
		return failure
	}
	authority, _ := userKeyOf(event)
	action, found := appManifestActionByKey(manifest, event.Request.PathValue("action"))
	if !found {
		return apiError(event, http.StatusNotFound, errorCodeAppActionInvalid, "The app has no such action.")
	}
	// An action's input is one request body, as large as the platform's
	// others (2026-10-13: it was 64 KB, so a long mail or note was refused).
	raw, err := io.ReadAll(io.LimitReader(event.Request.Body, appActionInputMaxBytes+1))
	body := appActionInput{}
	reason := readBodyRefusalReason(raw, err, appActionInputMaxBytes)
	if reason == "" && len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			reason = bodyRefusalReason("action call", err)
		}
	}
	if reason != "" {
		return apiError(event, http.StatusBadRequest, errorCodeAppActionInvalid, refusalMessage("Send the action's input as JSON up to 1 MB.", reason))
	}
	if body.Input == nil {
		body.Input = map[string]any{}
	}
	if err := validateAppActionInput(action, body.Input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppActionInvalid, "The input doesn't fit the action: "+err.Error())
	}
	canonical, _ := json.Marshal(body.Input)
	digest := sha256.Sum256(canonical)
	inputHash := hex.EncodeToString(digest[:])
	ref := action.Key
	required, err := appApprovalRequired(event.App, install, action, personID, appApprovalChannelScreen, authority.TenancyKey)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	token := event.Request.Header.Get(appApprovalHeader)
	if required && token == "" {
		request, err := requestAppApproval(event.App, install, action, personID, inputHash, canonical, authority.TenancyKey)
		if err != nil {
			return appPlatformFailure(event, err)
		}
		return event.JSON(http.StatusPreconditionRequired, map[string]any{
			"error":    map[string]string{"code": errorCodeAppApprovalRequired, "message": "The person approves this action first."},
			"approval": request,
		})
	}
	// An approval given is spent even when the hour it was given for
	// already lets the call run.
	if token != "" {
		if err := consumeAppApproval(event.App, install.ID, action.Key, personID, inputHash, token); err != nil && required {
			return apiError(event, http.StatusForbidden, errorCodeAppApprovalInvalid, "The approval isn't for this call, was used or ran out.")
		}
	}
	input, _ := json.Marshal(withAppActionCaller(event.App, map[string]any{"action": action.Key, "input": body.Input, "person": personID,
		"tenancy": authority.TenancyKey}, personID, authority.TenancyKey))
	run := appRun{Trigger: "action", Ref: ref, Subject: personID, Worker: action.Worker, Function: action.Function, Input: input,
		Tenancy: authority.TenancyKey}
	if action.RunAs == "person" {
		run.Person = &authority
	}
	output, runID, err := runAppWorkerFunction(event.Request.Context(), event.App, install, manifest, run)
	if err != nil {
		if errors.Is(err, errAppWorkerBusy) {
			return apiError(event, http.StatusServiceUnavailable, errorCodeAppActionFailed, "The app is busy; try again in a moment.")
		}
		if errors.Is(err, errAppWorkerHalted) {
			return apiError(event, http.StatusForbidden, errorCodeAppStopped, appStoppedMessage)
		}
		if errors.Is(err, errAppRunNotUndoable) {
			return apiError(event, http.StatusConflict, errorCodeAppActionInvalid, "That run didn't finish, so the app can't undo it.")
		}
		if errors.As(err, new(appWorkerPastLimit)) {
			return apiError(event, http.StatusGatewayTimeout, errorCodeAppActionTimedOut, "The action ran past its time limit.")
		}
		// The app's own reason for refusing reaches its screen.
		if reason, refused := appWorkerRefusalText(err); refused {
			return apiError(event, http.StatusUnprocessableEntity, errorCodeAppActionRefused, reason)
		}
		return apiErrorWithCause(event, http.StatusBadGateway, errorCodeAppActionFailed, "The action failed.",
			"app.action", "run", errorCodeAppActionFailed, err)
	}
	result := appActionResult{RunID: runID, Output: string(output)}
	if decoded := any(nil); json.Valid(output) && json.Unmarshal(output, &decoded) == nil {
		result.Output = decoded
	}
	return event.JSON(http.StatusOK, result)
}

// decideAppApprovalRoute is the person, in Leafen itself, answering an
// action's request: never an app or a key.
func decideAppApprovalRoute(event *core.RequestEvent, approve bool) error {
	if _, isKey := userKeyOf(event); isKey {
		return apiError(event, http.StatusForbidden, errorCodeAppApprovalInvalid, "Only the person approves, in Leafen itself.")
	}
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	token, err := decideAppApproval(event.App, event.Request.PathValue("installId"), event.Request.PathValue("approvalId"), user.Id, approve)
	if errors.Is(err, errAppApprovalInvalid) || errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeAppApprovalInvalid, "No such request is waiting.")
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if !approve {
		return event.NoContent(http.StatusNoContent)
	}
	// "For an hour" allows the action on the app's screen meanwhile.
	if appApprovalDecision(event) {
		decided := struct {
			Action  string `db:"action"`
			Tenancy string `db:"tenancy"`
		}{}
		if err := event.App.DB().NewQuery(`SELECT [[action]], [[tenancy]] FROM {{leafen_action_approvals}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": event.Request.PathValue("approvalId")}).One(&decided); err != nil {
			return appPlatformFailure(event, err)
		}
		install, err := loadAppInstall(event.App, event.Request.PathValue("installId"))
		if err != nil {
			return appPlatformFailure(event, err)
		}
		if err := allowAppActionForHour(event.App, install, event.Request.PathValue("approvalId"), decided.Action, user.Id, appApprovalChannelScreen, decided.Tenancy); err != nil {
			return appPlatformFailure(event, err)
		}
	}
	return event.JSON(http.StatusOK, appApprovalGrant{ApprovalToken: token})
}
