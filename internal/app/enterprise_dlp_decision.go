package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// An organization's DLP policy may block content holding sensitive terms and
// some outbound actions (public and external sharing, file downloads,
// exports). The organization module decides that (DLP); without it nothing
// is blocked. The core's paths check through these and answer a block alike.

const (
	dlpDatabaseBusyCauseCode       = "DLP_DATABASE_BUSY"
	dlpStorageUnavailableCauseCode = "DLP_STORAGE_UNAVAILABLE"
)

var (
	errDLPPolicyBlocked     = errors.New(errorCodeDLPPolicyBlocked)
	errDLPPolicyUnavailable = errors.New(errorCodeDLPUnavailable)
)

func isDLPPolicyDecisionError(err error) bool {
	return errors.Is(err, errDLPPolicyBlocked) || errors.Is(err, errDLPPolicyUnavailable)
}

// organizationDLPContentScanner checks content against an organization's DLP
// policy as read once, for a run of checks.
type organizationDLPContentScanner struct {
	organizationID string
	check          OrganizationDLPContentScanner
}

func newOrganizationDLPContentScanner(app core.App, organizationID string) (organizationDLPContentScanner, error) {
	if dlp := organizationPolicy.DLP; dlp != nil {
		check, err := dlp.ContentScanner(app, organizationID)
		return organizationDLPContentScanner{organizationID: organizationID, check: check}, err
	}
	return organizationDLPContentScanner{organizationID: organizationID}, nil
}

// revision is the revision of the policy the scanner read; 0 without one.
func (scanner organizationDLPContentScanner) revision() int {
	if scanner.check == nil {
		return 0
	}
	return scanner.check.Revision()
}

// blocks tells whether the policy blocks content holding sensitive terms.
func (scanner organizationDLPContentScanner) blocks() bool {
	return scanner.check != nil && scanner.check.Blocks()
}

// enforce fails with errDLPPolicyBlocked when a value holds a sensitive term.
func (scanner organizationDLPContentScanner) enforce(values ...any) error {
	if scanner.check == nil {
		return nil
	}
	return scanner.check.Enforce(values...)
}

// requireCurrentPolicy fails with errDLPPolicyUnavailable when the policy
// changed since the scanner read it.
func (scanner organizationDLPContentScanner) requireCurrentPolicy(app core.App, organizationID string) error {
	if scanner.check == nil {
		return nil
	}
	return scanner.check.RequireCurrentPolicy(app, organizationID)
}

func enforceOrganizationDLPContent(app core.App, organizationID string, values ...any) error {
	scanner, err := newOrganizationDLPContentScanner(app, organizationID)
	if err != nil {
		return err
	}
	return scanner.enforce(values...)
}

// enforceOrganizationDLPOutbound fails with errDLPPolicyBlocked when the
// policy blocks the outbound action.
func enforceOrganizationDLPOutbound(app core.App, organizationID, action string) error {
	if dlp := organizationPolicy.DLP; dlp != nil {
		return dlp.Outbound(app, organizationID, action)
	}
	return nil
}

const (
	dlpPolicyBlockedReason     = "The organization's data loss prevention (DLP) policy blocks this content."
	dlpPolicyUnavailableReason = "Couldn't check the organization's data loss prevention (DLP) policy. Try again."
)

// notionDLPPolicyError answers a /v1 request the organization's DLP policy
// refused, saying so instead of reading as a token permission
// (2026-10-16: a blocked block write said the token was not authorized).
func notionDLPPolicyError(event *core.RequestEvent, err error) error {
	if errors.Is(err, errDLPPolicyBlocked) {
		return notionCompatibilityError(event, http.StatusForbidden, notionErrorRestrictedResource, dlpPolicyBlockedReason)
	}
	return notionCompatibilityError(event, http.StatusServiceUnavailable, "service_unavailable", dlpPolicyUnavailableReason)
}

func writeDLPPolicyError(event *core.RequestEvent, err error, stage string) error {
	if storageError, ok := dlpStorageAPIError(event, err, stage); ok {
		return storageError
	}
	if errors.Is(err, errDLPPolicyBlocked) {
		return apiErrorWithCause(
			event, http.StatusBadRequest, errorCodeDLPPolicyBlocked,
			"The organization DLP policy blocked this operation.", "enterprise_dlp", stage,
			errorCodeDLPPolicyBlocked, err,
		)
	}
	return apiErrorWithCause(
		event, http.StatusServiceUnavailable, errorCodeDLPUnavailable,
		"Couldn't check the DLP policy.", "enterprise_dlp", stage,
		errorCodeDLPUnavailable, err,
	)
}

func dlpStorageAPIError(event *core.RequestEvent, err error, stage string) (error, bool) {
	if isSQLiteBusyError(err) {
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeDLPDatabaseBusy,
			"The DLP policy store is busy. Try again.", "enterprise_dlp", stage,
			dlpDatabaseBusyCauseCode, err,
		), true
	}
	if !isSQLiteStorageUnavailableError(err) {
		return nil, false
	}
	return apiErrorWithCause(
		event, http.StatusServiceUnavailable, errorCodeDLPStorageUnavailable,
		"The DLP policy store is temporarily unavailable.", "enterprise_dlp", stage,
		dlpStorageUnavailableCauseCode, err,
	), true
}
