package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// How a request to Notion failed, and what the person and an agent are told
// of it: Notion refusing the token, Notion asking to wait (its request limit,
// 429), no way through to Notion at all (this server off the internet, a
// name that doesn't resolve, a refused connection), or Notion answering with
// a failure or not in time. Only the third is the internet's (2026-11-13: a
// scope read Notion kept asking to wait said to check that the server reaches
// the internet).
type notionProviderFailure int

const (
	notionProviderAnswerFailed notionProviderFailure = iota
	notionProviderTokenRefused
	notionProviderAskedToWait
	notionProviderOutOfReach
)

// notionProviderFailureOf tells how err failed. askedToWait is whether
// Notion asked the connection to wait while the request went on: a read
// that waits that out (the import scope) and runs out of time then failed
// because of the wait, not because Notion was slow.
func notionProviderFailureOf(err error, askedToWait bool) notionProviderFailure {
	if notionProviderTokenRejected(err) {
		return notionProviderTokenRefused
	}
	var limited *notionProviderRateLimitError
	var status *notionProviderStatusError
	if errors.As(err, &limited) || (errors.As(err, &status) && status.status == http.StatusTooManyRequests) {
		return notionProviderAskedToWait
	}
	var timedOut *notionProviderTimeoutError
	if askedToWait && (errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timedOut)) {
		return notionProviderAskedToWait
	}
	if notionProviderOutOfReachError(err) {
		return notionProviderOutOfReach
	}
	return notionProviderAnswerFailed
}

// notionProviderOutOfReachError is a request that found no way through to
// Notion: its name didn't resolve, no connection could be made, or the
// connection wasn't Notion's (a certificate that isn't).
func notionProviderOutOfReachError(err error) bool {
	var name *net.DNSError
	var operation *net.OpError
	var certificate *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	return errors.As(err, &name) || (errors.As(err, &operation) && operation.Op == "dial") ||
		errors.As(err, &certificate) || errors.As(err, &authority) || errors.As(err, &hostname)
}

// askedToWaitSince is whether Notion asked the connection to wait (a 429)
// at or after since.
func (owner *notionProviderAdmissionOwner) askedToWaitSince(connectionID string, since time.Time) bool {
	if owner == nil {
		return false
	}
	owner.mu.Lock()
	lane := owner.lanes[connectionID]
	owner.mu.Unlock()
	if lane == nil {
		return false
	}
	lane.mu.Lock()
	defer lane.mu.Unlock()
	return !lane.limitedAt.IsZero() && !lane.limitedAt.Before(since)
}

// notionProviderAPIError answers a Notion request that failed as
// notionProviderFailureOf tells it: a token Notion refuses, with what to say
// (2026-10-25: a made-up token read only as "Notion 연결을 저장하지
// 못했습니다"), Notion asking to wait, Notion out of reach, or Notion
// failing to answer — each its own code, so the window says which.
func notionProviderAPIError(event *core.RequestEvent, err error, rejected string, askedToWait bool) error {
	switch notionProviderFailureOf(err, askedToWait) {
	case notionProviderTokenRefused:
		return apiError(event, http.StatusUnprocessableEntity, errorCodeNotionImportTokenRejected, rejected)
	case notionProviderAskedToWait:
		return apiError(event, http.StatusBadGateway, errorCodeNotionImportProviderLimited, "Notion asked to wait: too many requests for now.")
	case notionProviderOutOfReach:
		return apiError(event, http.StatusBadGateway, errorCodeNotionImportProviderUnreachable, "Couldn't reach Notion.")
	}
	return apiError(event, http.StatusBadGateway, errorCodeNotionImportProviderFailed, "Notion didn't answer the request.")
}

// notionProviderFailureReason says to an agent, for the person, why a
// request to Notion failed and what to do, as the window says it.
func notionProviderFailureReason(err error, askedToWait bool) string {
	switch notionProviderFailureOf(err, askedToWait) {
	case notionProviderTokenRefused:
		return notionImportFailureReason(errorCodeNotionImportTokenRejected, "")
	case notionProviderAskedToWait:
		return "Notion asked Leafen to wait (too many requests for now); try again in a minute"
	case notionProviderOutOfReach:
		return "Leafen couldn't reach Notion: check that the Leafen server reaches the internet, then try again"
	}
	return "Notion didn't answer the request; try again in a moment"
}

// notionProviderTimeoutError is a request to Notion that ran past its own
// time while its caller was still waiting: Notion slow for a moment, which
// may pass (2026-11-05: one answer over 30 seconds ended a run of a real
// workspace at its first attempt, the client's timeout read as the caller's).
type notionProviderTimeoutError struct{ err error }

func (e *notionProviderTimeoutError) Error() string { return e.err.Error() }
func (e *notionProviderTimeoutError) Unwrap() error { return e.err }

// notionProviderTimedOut marks a request's own timeout; the caller's
// context ending stays as it is.
func notionProviderTimedOut(ctx context.Context, err error) error {
	if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		return &notionProviderTimeoutError{err: err}
	}
	return err
}

// notionProviderMayPass reports a failure that may pass on its own: Notion
// asking to wait, busy, slow or down for a moment (409, 429, 5xx, a request
// past its own time), or the answer lost on the way. Notion refusing the
// request or the token, and the caller's own time running out, are not.
func notionProviderMayPass(err error) bool {
	var timedOut *notionProviderTimeoutError
	if errors.As(err, &timedOut) {
		return true
	}
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// A file Notion keeps that didn't come this time is tried again with the
	// rest, as Notion not answering is (2026-11-06: one file's download
	// failing under load ended a whole real migration as "Leafen stopped").
	var file *notionFileFetchFailure
	if errors.As(err, &file) {
		return file.kind == "transient"
	}
	var limited *notionProviderRateLimitError
	var status *notionProviderStatusError
	switch {
	case errors.As(err, &limited):
		return true
	case errors.As(err, &status):
		return status.status == http.StatusConflict || status.status == http.StatusTooManyRequests || status.status >= 500
	}
	var transport *url.Error
	var network net.Error
	return errors.As(err, &transport) || errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF)
}
