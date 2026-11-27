package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const (
	formStorageUnavailableCauseCode = "FORM_STORAGE_UNAVAILABLE"
	formDatabaseBusyCauseCode       = "FORM_DATABASE_BUSY"
)

func registerFormRoutes(event *core.ServeEvent, persistence *pocketBaseYgoPersistence) {
	event.Router.Route(formConfigureOperation, apiRoot+formPath, dispatchFormRequest(persistence)).
		Bind(apis.BodyLimit(maxFormRequestBytes))
	registerFormFileUploadRoute(event)
	registerFormPlaceSearchRoute(event)
}

func dispatchFormRequest(persistence *pocketBaseYgoPersistence) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		body, err := io.ReadAll(event.Request.Body)
		if err != nil {
			return writeFormError(event, errFormInvalid, "request")
		}
		envelope := formActionEnvelope{}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return writeFormError(event, formBodyProblem("request", err), "request")
		}
		switch envelope.Action {
		case formActionConfigure:
			user, ok := authenticatedUser(event)
			if !ok {
				return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
			}
			input := formConfigureRequest{}
			if err := json.Unmarshal(body, &input); err != nil {
				return writeFormError(event, formBodyProblem(formActionConfigure, err), "configure")
			}
			response, err := configureForm(persistence, event.App, user.Id, input)
			if err != nil {
				return writeFormError(event, err, "configure")
			}
			status := http.StatusCreated
			if response.Replayed {
				status = http.StatusOK
			}
			return event.JSON(status, response)
		case formActionDefinition:
			input := formDefinitionRequest{}
			if err := json.Unmarshal(body, &input); err != nil {
				return writeFormError(event, formBodyProblem(formActionDefinition, err), "definition")
			}
			principalID := ""
			if user, ok := authenticatedUser(event); ok {
				principalID = user.Id
			}
			response, err := readFormDefinition(event.App, principalID, input.Token)
			if err != nil {
				return writeFormError(event, err, "definition")
			}
			return event.JSON(http.StatusOK, response)
		case formActionOptions:
			user, ok := authenticatedUser(event)
			if !ok {
				return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
			}
			input := formOptionsRequest{}
			if err := json.Unmarshal(body, &input); err != nil {
				return writeFormError(event, formBodyProblem(formActionOptions, err), "options")
			}
			response, err := readFormOptions(event.App, user.Id, input)
			if err != nil {
				return writeFormError(event, err, "options")
			}
			return event.JSON(http.StatusOK, response)
		case formActionSubmit:
			input := formSubmitRequest{}
			if err := json.Unmarshal(body, &input); err != nil {
				return writeFormError(event, formBodyProblem(formActionSubmit, err), "submit")
			}
			input, err = normalizeFormSubmitRequest(input)
			if err != nil {
				return writeFormError(event, err, "submit")
			}
			admitted, err := admitFormSubmission(event.App, event.RealIP(), input)
			if err != nil {
				return writeFormError(event, err, "admission")
			}
			if !admitted {
				return writeFormError(event, errFormRateLimit, "admission")
			}
			principalID := ""
			if user, ok := authenticatedUser(event); ok {
				principalID = user.Id
			}
			response, err := submitFormResponse(persistence, event.App, principalID, input)
			if err != nil {
				return writeFormError(event, err, "submit")
			}
			status := http.StatusCreated
			if response.Replayed {
				status = http.StatusOK
			}
			return event.JSON(status, response)
		case formActionResponseCopy:
			user, ok := authenticatedUser(event)
			if !ok {
				return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
			}
			input := formResponseCopyRequest{}
			if err := json.Unmarshal(body, &input); err != nil {
				return writeFormError(event, formBodyProblem(formActionResponseCopy, err), "response_copy")
			}
			response, err := sendFormResponseCopy(event.Request.Context(), event.App, user.Id, input)
			if err != nil {
				return writeFormError(event, err, "response_copy")
			}
			return event.JSON(http.StatusOK, response)
		default:
			return writeFormError(event, formInvalid("action is %s, %s, %s, %s or %s", formActionConfigure, formActionDefinition,
				formActionOptions, formActionSubmit, formActionResponseCopy), "request")
		}
	}
}

// formBodyProblem is why a body doesn't read as its action's request: the
// field that holds the wrong kind of value, or that it isn't JSON.
func formBodyProblem(action string, err error) error {
	return bodyRefusal(errFormInvalid, action+" request", err)
}

func writeFormError(event *core.RequestEvent, err error, stage string) error {
	if errors.Is(err, errFormRateLimit) {
		event.Response.Header().Set(
			"Retry-After", strconv.Itoa(formSubmissionAdmissionWindowSeconds),
		)
		return apiErrorWithCause(
			event, http.StatusTooManyRequests, errorCodeRateLimited,
			"Too many requests. Try again shortly.",
			"form", stage, formSubmissionAdmissionCause, err,
		)
	}
	if errors.Is(err, errFormAuthenticationRequired) {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	// What to fix follows, where the refusal names it: the field and why; a
	// form not found says why only to the one configuring it.
	reason := refusalReason(err)
	if errors.Is(err, errFormInvalid) {
		return apiError(event, http.StatusBadRequest, errorCodeFormInvalid, refusalMessage("Check the form details.", reason))
	}
	if errors.Is(err, errFormNotFound) {
		if reason != "" && stage == "configure" {
			return apiError(event, http.StatusNotFound, errorCodeFormNotFound, "Form not found: "+reason+".")
		}
		return apiError(event, http.StatusNotFound, errorCodeFormNotFound, "Form not found.")
	}
	if errors.Is(err, errFormForbidden) {
		return apiError(event, http.StatusForbidden, errorCodeFormForbidden, "The form can't be accessed.")
	}
	if errors.Is(err, errFormConflict) {
		return apiError(event, http.StatusConflict, errorCodeFormConflict, "The form changed. Load it again.")
	}
	if storageError, ok := formStorageAPIError(event, err, stage); ok {
		return storageError
	}
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "form-"+stage)
	}
	if errors.Is(err, errOrganizationSharingSecurityBlocked) {
		return writeSharingSecurityBlockedError(event)
	}
	var graphError *graphRequestError
	if errors.As(err, &graphError) {
		switch graphError.code {
		case errorCodeGraphMutationIDReused:
			return apiError(event, http.StatusConflict, errorCodeFormMutationIDReused, "This ID was used for a different form change.")
		case errorCodePageNotFound:
			return apiError(event, http.StatusNotFound, errorCodeFormNotFound, "Form not found.")
		default:
			// The graph's own refusal is kept as the cause, for the log to say why.
			return apiErrorWithCause(event, graphError.status, errorCodeFormConflict, "The form changed. Load it again.",
				"form", stage, graphError.code, err)
		}
	}
	return apiErrorWithCause(
		event, http.StatusInternalServerError, errorCodeFormFailed,
		"Couldn't process the form request.", "form", stage, errorCodeFormFailed, err,
	)
}

func formStorageAPIError(event *core.RequestEvent, err error, stage string) (error, bool) {
	switch sqliteOperationalErrorClass(err) {
	case sqliteOperationalBusy:
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeFormDatabaseBusy,
			"The form store is locked by another operation. Try again shortly.",
			"form", stage, formDatabaseBusyCauseCode, err,
		), true
	case sqliteOperationalUnavailable:
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeFormStorageUnavailable,
			"The form store is temporarily unavailable. Try again shortly.",
			"form", stage, formStorageUnavailableCauseCode, err,
		), true
	default:
		return nil, false
	}
}
