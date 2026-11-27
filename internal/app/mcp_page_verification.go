package app

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// verifyMCPPageItself verifies a page by its own verification or takes it
// away, as notion-update-page's update_verification asks of a page not
// verified by its database's Verification property (page_verification_way.go):
// the connection's person verifies it as the page head's "인증 추가" does
// (page_verification.go), for the days or until the day given, or with no
// end. Taking away a verification the page doesn't have changes nothing.
func verifyMCPPageItself(app core.App, principalID, pageID string, verification mcpPageVerification) error {
	now := time.Now()
	end, err := mcpVerificationEnd(app, principalID, verification, now)
	if err != nil {
		return err
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		target := verificationTarget{PageID: pageID}
		stored, _, err := loadStoredVerification(txApp, target)
		if err != nil {
			return err
		}
		input := pageVerificationInput{Operation: pageVerificationVerify, ExpectedRevision: stored.Revision}
		switch {
		case verification.Status == "unverified" && stored.VerifiedAt == "":
			return nil
		case verification.Status == "unverified":
			input.Operation = pageVerificationUnverify
		case end != nil:
			input.ExpiresAt = end.Format(time.RFC3339Nano)
		}
		return applyVerification(txApp, principalID, target, input, now.UTC())
	})
	if err == nil {
		return nil
	}
	if reason := refusalReason(err); reason != "" {
		return mcpInvalid("%s", reason)
	}
	return mcpReasoned(err)
}
