package app

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	authAuditIDPrefix      = "laa_"
	authAuditCursorPrefix  = "haa1."
	authAuditReadOperation = "auth_audit.read"
)

type authAuditAttempt struct {
	OwnerID     string
	TargetEmail string
	Method      string
	Phase       string
	Outcome     string
	ReasonCode  string
	// The answer's status with a failure's code, which together say it in
	// the words its screen had.
	ReasonStatus        int
	DiagnosticReference string
	Metadata            browserSessionMetadata
}

type storedAuthAuditEvent struct {
	ID                  string `db:"id"`
	Method              string `db:"method"`
	Phase               string `db:"phase"`
	Outcome             string `db:"outcome"`
	ReasonCode          string `db:"reason_code"`
	ReasonStatus        int    `db:"reason_status"`
	DiagnosticReference string `db:"diagnostic_reference"`
	UserAgent           string `db:"user_agent"`
	IP                  string `db:"ip"`
	OccurredAt          int64  `db:"occurred_at"`
}

type authAuditCursor struct {
	OccurredAt int64
	ID         string
}

func registerAuthAuditRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/auth-audit", readOwnAuthAuditHistory)
}

func recordAuthAttemptInTransaction(app core.App, attempt authAuditAttempt) (string, error) {
	if !validAuthAuditMethod(attempt.Method) || !validAuthAuditPhase(attempt.Phase) ||
		!validAuthAuditOutcome(attempt.Outcome) {
		return "", errors.New(errorCodeAuthAuditUnavailable)
	}
	reasonCode := strings.TrimSpace(attempt.ReasonCode)
	if reasonCode != "" && safeDiagnosticToken(reasonCode) != reasonCode {
		return "", errors.New(errorCodeAuthAuditUnavailable)
	}
	reasonStatus := attempt.ReasonStatus
	if reasonStatus < 0 || reasonStatus > 599 {
		reasonStatus = 0
	}
	reference := attempt.DiagnosticReference
	if reference == "" {
		reference = newDiagnosticReference()
	}
	if !validDiagnosticReference(reference) {
		return "", errors.New(errorCodeAuthAuditUnavailable)
	}
	targetEmail, _ := normalizeEmailIdentity(attempt.TargetEmail)
	ownerID := strings.TrimSpace(attempt.OwnerID)
	var storedOwner any
	if ownerID != "" {
		storedOwner = ownerID
	}
	_, err := app.DB().Insert(authAuditEventsTable, dbx.Params{
		"id":       authAuditIDPrefix + security.RandomString(24),
		"owner_id": storedOwner, "target_email": targetEmail,
		"method": attempt.Method, "phase": attempt.Phase, "outcome": attempt.Outcome,
		"reason_code": reasonCode, "reason_status": reasonStatus, "diagnostic_reference": reference,
		"user_agent":  boundedAuthAuditMetadata(attempt.Metadata.UserAgent, maxAuthSessionUserAgentLength),
		"ip":          boundedAuthAuditMetadata(attempt.Metadata.IP, maxAuthSessionIPLength),
		"occurred_at": time.Now().UTC().UnixNano(),
	}).Execute()
	if err != nil {
		return "", err
	}
	return reference, nil
}

func recordAuthAttempt(app core.App, attempt authAuditAttempt) (string, error) {
	reference := ""
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		reference, err = recordAuthAttemptInTransaction(txApp, attempt)
		return err
	})
	return reference, err
}

// signInStepPhase is the phase a sign-in's right first step is recorded in:
// "request" while 2-step verification still asks for its code, which records
// the sign-in itself, and "verify" where the step signed in (2026-10-27: a
// right password that stopped at the code read "로그인했습니다").
func signInStepPhase(secondStepAsked bool) string {
	if secondStepAsked {
		return authAuditPhaseRequest
	}
	return authAuditPhaseVerify
}

func recordAuthAttemptInTransactionSuccess(
	app core.App,
	event *core.RequestEvent,
	ownerID string,
	targetEmail string,
	method string,
	phase string,
) error {
	return recordAuthAttemptInTransactionSuccessWithMetadata(
		app, ownerID, targetEmail, method, phase, browserSessionMetadataFromEvent(event),
	)
}

func recordAuthAttemptInTransactionSuccessWithMetadata(
	app core.App,
	ownerID string,
	targetEmail string,
	method string,
	phase string,
	metadata browserSessionMetadata,
) error {
	_, err := recordAuthAttemptInTransaction(app, authAuditAttempt{
		OwnerID: ownerID, TargetEmail: targetEmail, Method: method, Phase: phase,
		Outcome: authAuditOutcomeSuccess, Metadata: metadata,
	})
	return err
}

// recordSuccessfulAuthAttempt keeps the attempt in the security records:
// recorded, or false when it couldn't and answered so, err then that
// answer's write.
func recordSuccessfulAuthAttempt(
	event *core.RequestEvent,
	ownerID string,
	targetEmail string,
	method string,
	phase string,
) (bool, error) {
	reference, err := recordAuthAttempt(event.App, authAuditAttempt{
		OwnerID: ownerID, TargetEmail: targetEmail, Method: method, Phase: phase,
		Outcome: authAuditOutcomeSuccess, Metadata: browserSessionMetadataFromEvent(event),
	})
	if err == nil {
		return true, nil
	}
	if !validDiagnosticReference(reference) {
		reference = newDiagnosticReference()
	}
	return false, writeAPIErrorWithReference(
		event, http.StatusServiceUnavailable, errorCodeAuthAuditUnavailable,
		"Couldn't save the authentication security records.", reference,
		"auth_audit.record", "storage-write", "AUTH_AUDIT_WRITE_FAILED", err,
		diagnosticSourcePosition(1),
	)
}

func recordRejectedAuthAttempt(
	event *core.RequestEvent,
	ownerID string,
	targetEmail string,
	method string,
	phase string,
	status int,
	code string,
	message string,
) error {
	return recordRejectedAuthAttemptWithCause(
		event, ownerID, targetEmail, method, phase, status, code, message,
		diagnosticOperation(event), "response", code, nil,
	)
}

func recordRejectedAuthAttemptWithCause(
	event *core.RequestEvent,
	ownerID string,
	targetEmail string,
	method string,
	phase string,
	status int,
	code string,
	message string,
	operation string,
	stage string,
	causeCode string,
	cause error,
) error {
	reference := newDiagnosticReference()
	return recordRejectedAuthAttemptWithReference(
		event, ownerID, targetEmail, method, phase, status, code, message,
		operation, stage, causeCode, cause, reference,
	)
}

func recordRejectedAuthAttemptWithReference(
	event *core.RequestEvent,
	ownerID string,
	targetEmail string,
	method string,
	phase string,
	status int,
	code string,
	message string,
	operation string,
	stage string,
	causeCode string,
	cause error,
	reference string,
) error {
	if !validDiagnosticReference(reference) {
		reference = newDiagnosticReference()
	}
	primaryFailure, primaryClassified := classifyAuthStorageError(cause)
	if primaryClassified {
		status = primaryFailure.Status
		code = primaryFailure.Code
		message = primaryFailure.Message
		stage = primaryFailure.Stage
		causeCode = primaryFailure.CauseCode
	}
	_, auditErr := recordAuthAttempt(event.App, authAuditAttempt{
		OwnerID: ownerID, TargetEmail: targetEmail, Method: method, Phase: phase,
		Outcome: authAuditOutcomeFailure, ReasonCode: code, ReasonStatus: status,
		DiagnosticReference: reference, Metadata: browserSessionMetadataFromEvent(event),
	})
	if auditErr != nil {
		auditCauseCode := "AUTH_AUDIT_WRITE_FAILED"
		if auditFailure, ok := classifyAuthStorageError(auditErr); ok {
			auditCauseCode = auditFailure.CauseCode
		}
		if primaryClassified {
			logAPIDiagnostic(
				event, http.StatusServiceUnavailable, errorCodeAuthAuditUnavailable,
				reference, "auth_audit.record", "storage-write", auditCauseCode,
				auditErr, diagnosticSourcePosition(1),
			)
			return writeAPIErrorWithReference(
				event, status, code, message, reference, operation, stage, causeCode,
				cause, diagnosticSourcePosition(1),
			)
		}
		return writeAPIErrorWithReference(
			event, http.StatusServiceUnavailable, errorCodeAuthAuditUnavailable,
			"Couldn't save the authentication security records.", reference,
			"auth_audit.record", "storage-write", auditCauseCode, auditErr,
			diagnosticSourcePosition(1),
		)
	}
	return writeAPIErrorWithReference(
		event, status, code, message, reference, operation, stage, causeCode, cause,
		diagnosticSourcePosition(1),
	)
}

func readOwnAuthAuditHistory(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	limit := authAuditPageLimit
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > authAuditPageLimit {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the audit log range.")
		}
		limit = parsed
	}
	cursor, err := decodeAuthAuditCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAuthAuditCursorInvalid, "Check the audit log position.")
	}

	rows := []storedAuthAuditEvent{}
	params := dbx.Params{"ownerId": user.Id, "limit": limit + 1}
	query := `
		SELECT [[id]], [[method]], [[phase]], [[outcome]], [[reason_code]], [[reason_status]],
			[[diagnostic_reference]], [[user_agent]], [[ip]], [[occurred_at]]
		FROM {{leafen_auth_audit_events}}
		WHERE [[owner_id]] = {:ownerId}
	`
	if cursor != nil {
		query += ` AND ([[occurred_at]] < {:occurredAt} OR ([[occurred_at]] = {:occurredAt} AND [[id]] < {:cursorId}))`
		params["occurredAt"] = cursor.OccurredAt
		params["cursorId"] = cursor.ID
	}
	query += ` ORDER BY [[occurred_at]] DESC, [[id]] DESC LIMIT {:limit}`
	if err := event.App.DB().NewQuery(query).Bind(params).All(&rows); err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAuthAuditReadFailed,
			"Couldn't load the authentication security records.", authAuditReadOperation,
			"storage-read", "AUTH_AUDIT_READ_FAILED", err,
		)
	}

	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	response := authAuditHistoryResponse{Events: make([]authAuditEventResponse, 0, len(rows))}
	for _, row := range rows {
		var reasonCode *string
		if row.ReasonCode != "" {
			value := row.ReasonCode
			reasonCode = &value
		}
		response.Events = append(response.Events, authAuditEventResponse{
			ID: row.ID, Method: row.Method, Phase: row.Phase, Outcome: row.Outcome,
			ReasonCode: reasonCode, ReasonStatus: row.ReasonStatus, Reference: row.DiagnosticReference,
			UserAgent: row.UserAgent, IP: row.IP,
			OccurredAt: time.Unix(0, row.OccurredAt).UTC().Format(time.RFC3339Nano),
		})
	}
	if more && len(rows) > 0 {
		value := encodeAuthAuditCursor(rows[len(rows)-1])
		response.NextCursor = &value
	}
	return event.JSON(http.StatusOK, response)
}

func encodeAuthAuditCursor(row storedAuthAuditEvent) string {
	payload := strconv.FormatInt(row.OccurredAt, 10) + ":" + row.ID
	return authAuditCursorPrefix + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeAuthAuditCursor(raw string) (*authAuditCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if !strings.HasPrefix(raw, authAuditCursorPrefix) || len(raw) > 128 {
		return nil, errors.New(errorCodeAuthAuditCursorInvalid)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, authAuditCursorPrefix))
	if err != nil {
		return nil, errors.New(errorCodeAuthAuditCursorInvalid)
	}
	parts := strings.SplitN(string(payload), ":", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[1], authAuditIDPrefix) || len(parts[1]) != 28 {
		return nil, errors.New(errorCodeAuthAuditCursorInvalid)
	}
	occurredAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || occurredAt < 1 {
		return nil, errors.New(errorCodeAuthAuditCursorInvalid)
	}
	return &authAuditCursor{OccurredAt: occurredAt, ID: parts[1]}, nil
}

func validAuthAuditMethod(value string) bool {
	switch value {
	case authAuditMethodPasswordSignup, authAuditMethodPasswordSignin,
		authAuditMethodOAuthSignin, authAuditMethodMFATOTP, authAuditMethodMFARecovery,
		authAuditMethodSAMLSignin, authAuditMethodPasskeySignin, authAuditMethodDeviceCode, authAuditMethodInvitationJoin,
		authAuditMethodSetup:
		return true
	default:
		return false
	}
}

func validAuthAuditPhase(value string) bool {
	return value == authAuditPhaseRequest || value == authAuditPhaseVerify
}

func validAuthAuditOutcome(value string) bool {
	return value == authAuditOutcomeSuccess || value == authAuditOutcomeFailure
}

func boundedAuthAuditMetadata(value string, maximum int) string {
	value = strings.Map(func(current rune) rune {
		if unicode.IsControl(current) {
			return -1
		}
		return current
	}, value)
	return boundedAuthSessionMetadata(value, maximum)
}

func validDiagnosticReference(value string) bool {
	return len(value) == 22 && strings.HasPrefix(value, diagnosticReferencePrefix) && value[15] == '-'
}
