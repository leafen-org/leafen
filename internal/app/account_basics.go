package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// signedInPerson is the person signed in for something done signed in
// alone, never with a key; ok false when it answered the refusal instead,
// err then that answer's write (scripts/check-written-answers).
func signedInPerson(event *core.RequestEvent) (*core.Record, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	if _, keyed := userKeyOf(event); keyed {
		return nil, false, apiError(event, http.StatusForbidden, errorCodeUserKeyClosed, "A key can't do this; it's done logged in.")
	}
	return user, true, nil
}

// A deleted account keeps no name and this address, which is no reader's to
// see: each reader says of it what it says of a person no longer here.
const deletedAccountEmailPrefix, deletedAccountEmailSuffix = "deleted+", "@invalid.leafen"

// deletedAccountOrganizationName names the empty organization a deleted
// account leaves behind, which no reader is shown.
const deletedAccountOrganizationName = "Deleted organization"

func deletedAccountEmail(userID string) string {
	return deletedAccountEmailPrefix + strings.ToLower(userID) + deletedAccountEmailSuffix
}

func visibleAccountEmail(email string) string {
	if strings.HasPrefix(email, deletedAccountEmailPrefix) && strings.HasSuffix(email, deletedAccountEmailSuffix) {
		return ""
	}
	return email
}

// accountEmailSQL is the account's address for a reader, and accountNameSQL
// its name, or that address without one.
func accountEmailSQL(alias string) string {
	return `CASE WHEN [[` + alias + `]].[[email]] LIKE '` + deletedAccountEmailPrefix + `%` + deletedAccountEmailSuffix +
		`' THEN '' ELSE [[` + alias + `]].[[email]] END`
}

func accountNameSQL(alias string) string {
	return `COALESCE(NULLIF([[` + alias + `]].[[name]], ''), ` + accountEmailSQL(alias) + `)`
}

// userDisplayName is an account's name, or its email without one.
func userDisplayName(app core.App, userID string) string {
	if record, err := app.FindRecordById("users", userID); err == nil {
		if name := record.GetString("name"); name != "" {
			return name
		}
		return visibleAccountEmail(record.GetString("email"))
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
