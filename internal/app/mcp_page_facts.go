package app

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Notion MCP's fetch gives a page, beside its content, its verification and
// when it was last edited (seen 2026-09-30); a page no one verified is
// {"state": "unverified"}. Its path comes from what stands above it
// (mcp_fetch_ancestors.go).

type mcpPageFacts struct {
	Verification map[string]any
	LastEditedAt string
}

// loadMCPPageFacts reads a page's verification, its times on the clock
// (person_clock.go), and its last edit.
func loadMCPPageFacts(app core.App, page *core.Record, now time.Time, clock *time.Location) (mcpPageFacts, error) {
	facts := mcpPageFacts{}
	verification, err := mcpPageVerificationFact(app, page.Id, now, clock)
	if err != nil {
		return facts, err
	}
	facts.Verification = verification
	// A database page's is its row page's, the one time the app and the
	// Public API read (row_page_body_edit_time.go).
	if isDatabaseRowPageRecord(page) {
		if edited := databaseRowPageEditedAt(app, page.Id); edited != "" {
			facts.LastEditedAt = edited
			return facts, nil
		}
	}
	facts.LastEditedAt, err = notionPageTime(page.GetString("updated"))
	return facts, err
}

// mcpPageVerificationFact is a page's verification as the Public API writes a
// verification value: unverified, or verified by someone from a time until
// another or none, expired once that time has passed.
func mcpPageVerificationFact(app core.App, pageID string, now time.Time, clock *time.Location) (map[string]any, error) {
	return mcpVerificationFact(app, verificationTarget{PageID: pageID}, now, clock)
}

// mcpVerificationFact is a page's or a database's verification, a database's
// kept on its data source, written as a page's is, its times on the clock.
func mcpVerificationFact(app core.App, target verificationTarget, now time.Time, clock *time.Location) (map[string]any, error) {
	stored, _, err := loadStoredVerification(app, target)
	if err != nil {
		return nil, err
	}
	if stored.VerifiedAt == "" {
		return map[string]any{"state": "unverified"}, nil
	}
	state := "verified"
	var end any
	if stored.ExpiresAt != "" {
		end = personClockMillisOf(stored.ExpiresAt, clock)
		if expires, err := time.Parse(time.RFC3339Nano, stored.ExpiresAt); err == nil && !now.Before(expires) {
			state = "expired"
		}
	}
	fact := map[string]any{"state": state, "date": map[string]any{"start": personClockMillisOf(stored.VerifiedAt, clock), "end": end}}
	if stored.VerifiedBy != "" {
		verifiedBy, err := notionDatabaseRowPagePropertyUser(stored.VerifiedBy)
		if err != nil {
			return nil, err
		}
		fact["verified_by"] = verifiedBy
	}
	return fact, nil
}
