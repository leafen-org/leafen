package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A person whose email is on a domain an organization verified is a managed
// account of that organization: it may keep their emails and name, end their
// sign-ins sooner and keep them out of other organizations' workspaces. The
// organization module decides that (ManagedAccounts); without it nobody's
// account is managed.

const (
	errorCodeProfileChangesDisabled     = "LEAFEN_PROFILE_CHANGES_DISABLED"
	errorCodeExternalWorkspacesDisabled = "LEAFEN_EXTERNAL_WORKSPACES_DISABLED"
	managedUserPolicyHoursMillis        = int64(time.Hour / time.Millisecond)
)

var (
	errProfileChangesDisabled     = errors.New(errorCodeProfileChangesDisabled)
	errExternalWorkspacesDisabled = errors.New(errorCodeExternalWorkspacesDisabled)
)

// managedUserRules are what the organizations managing a person ask of them.
type managedUserRules struct {
	Managed        bool `db:"managed"`
	ProfileChanges bool `db:"profile_changes"`
	SessionHours   int  `db:"session_hours"`
}

func emailDomain(email string) string {
	normalized, valid := normalizeEmailIdentity(email)
	separator := strings.LastIndexByte(normalized, '@')
	if !valid || separator < 1 {
		return ""
	}
	return normalized[separator+1:]
}

// loadManagedUserRules reads the strictest rules of the organizations that
// manage the person; someone they don't manage has none.
func loadManagedUserRules(app core.App, user *core.Record) (managedUserRules, error) {
	if accounts := organizationPolicy.ManagedAccounts; accounts != nil {
		return accounts.Rules(app, user)
	}
	return managedUserRules{ProfileChanges: true}, nil
}

// managedUserSessionDeadline is when a sign-in made now must end, or 0.
func managedUserSessionDeadline(app core.App, user *core.Record, now time.Time) (int64, error) {
	rules, err := loadManagedUserRules(app, user)
	if err != nil || !rules.Managed || rules.SessionHours <= 0 {
		return 0, err
	}
	return now.UnixMilli() + int64(rules.SessionHours)*managedUserPolicyHoursMillis, nil
}

// requireExternalWorkspaceJoin refuses a person a workspace of another
// organization when an organization managing them keeps them to its own.
func requireExternalWorkspaceJoin(app core.App, userID, workspaceOrganizationID string) error {
	if accounts := organizationPolicy.ManagedAccounts; accounts != nil {
		return accounts.ExternalWorkspaceJoin(app, userID, workspaceOrganizationID)
	}
	return nil
}

// requireAccountEmailChange fails with errAccountEmailManaged when an
// organization managing the person keeps their primary email.
func requireAccountEmailChange(app core.App, user *core.Record, target string) error {
	if accounts := organizationPolicy.ManagedAccounts; accounts != nil {
		return accounts.EmailChange(app, user, target)
	}
	return nil
}

// requireAccountEmailAddition fails with errAccountEmailManaged when an
// organization managing the person, or the address, refuses it as a
// secondary email.
func requireAccountEmailAddition(app core.App, user *core.Record, target string) error {
	if accounts := organizationPolicy.ManagedAccounts; accounts != nil {
		return accounts.EmailAddition(app, user, target)
	}
	return nil
}

// auditAccountEmailChange tells the organizations managing the person of a
// change to their emails.
func auditAccountEmailChange(app core.App, user *core.Record, email, action string) error {
	if accounts := organizationPolicy.ManagedAccounts; accounts != nil {
		return accounts.EmailChanged(app, user, email, action)
	}
	return nil
}

// sendDueManagedUserNotices and nextManagedUserNoticeDue run the emails the
// organization module queues for the people it manages.
func sendDueManagedUserNotices(ctx context.Context, app core.App, now time.Time) error {
	if notices := organizationPolicy.ManagedUserNotices; notices != nil {
		return notices.SendDue(ctx, app, now)
	}
	return nil
}

func nextManagedUserNoticeDue(ctx context.Context, app core.App) (int64, error) {
	if notices := organizationPolicy.ManagedUserNotices; notices != nil {
		return notices.NextDue(ctx, app)
	}
	return -1, nil
}
