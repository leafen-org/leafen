package app

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An invitation that lets no one in says why (2026-11-23: a used guest
// invite link, a turned-off workspace link and a withdrawn invitation by
// name all read the email invitation's "for another address … older than
// 7 days"), as the error's common cause beside the invitation's code
// (api_error_because.go), for the screen to say in its language. Each kind
// says what is true of it:
//   - a guest invite link (page_guest_links.go): made for one person, who
//     used it; past its end; turned off — by its maker, or by its
//     organization keeping guests out or its workspace going; or no page of
//     it left to share (each in the trash or deleted, or none its maker may
//     still share);
//   - an invitation by name (workspace_name_invitations.go): used, withdrawn,
//     past its seven days, or its workspace being deleted;
//   - a workspace's or teamspace's invite link (workspace_invite_links.go,
//     teamspace_invite_links.go), which has no end: turned off (its
//     teamspace archived or gone, or whoever turned it on no longer an
//     owner, among them), or its workspace being deleted;
//   - an invitation by name or an invite link where the server no longer
//     lets workspaces add members.
//
// A code no invitation of its kind has says only that, in its kind's words
// (its holder may have copied it short; a workspace's link made anew keeps
// no trace of its old key): the code's own shape tells its kind. An email
// invitation keeps its one line of every way it may have ended. Only
// someone holding a code hears why: a code is 24 random bytes or more,
// never guessed, so the cause tells nothing its holder didn't have.
const (
	pageGuestLinkUsed           = "guestLinkUsed"
	pageGuestLinkExpired        = "guestLinkExpired"
	pageGuestLinkOff            = "guestLinkOff"
	pageGuestLinkUnknown        = "guestLinkUnknown"
	pageGuestLinkNoPages        = "guestLinkNoPages"
	nameInvitationUsed          = "nameInvitationUsed"
	nameInvitationWithdrawn     = "nameInvitationWithdrawn"
	nameInvitationExpired       = "nameInvitationExpired"
	nameInvitationUnknown       = "nameInvitationUnknown"
	inviteLinkOff               = "inviteLinkOff"
	inviteLinkUnknown           = "inviteLinkUnknown"
	invitationWorkspaceDeleted  = "invitationWorkspaceDeleted"
	invitationMemberAddsStopped = "invitationMemberAddsStopped"
)

// invitationEndedSaid is each cause in the error's own words.
var invitationEndedSaid = map[string]string{
	pageGuestLinkUsed:           "This guest invite link was for one person and has been used: ask whoever sent it for a new one.",
	pageGuestLinkExpired:        "This guest invite link has expired: ask whoever sent it for a new one.",
	pageGuestLinkOff:            "This guest invite link was turned off: ask whoever sent it for a new one.",
	pageGuestLinkUnknown:        "No guest invite link has this code: check the whole link was copied, or ask whoever sent it for a new one.",
	pageGuestLinkNoPages:        "This guest invite link has no page left to share: tell whoever sent it.",
	nameInvitationUsed:          "This invitation has been used: ask whoever invited you for a new one.",
	nameInvitationWithdrawn:     "This invitation was withdrawn: ask whoever invited you for a new one.",
	nameInvitationExpired:       "This invitation is past its 7 days: ask whoever invited you for a new one.",
	nameInvitationUnknown:       "No invitation has this code: check the whole link was copied, or ask whoever invited you for a new one.",
	inviteLinkOff:               "This invite link was turned off: ask whoever sent it for a new one.",
	inviteLinkUnknown:           "No invite link has this code; it may have been replaced by a new one: ask whoever sent it for the current link.",
	invitationWorkspaceDeleted:  "This invitation's workspace has been deleted.",
	invitationMemberAddsStopped: "This server doesn't let workspaces add members now, so this invitation can't be used: ask its administrator.",
}

// invitationEnded is such an invitation: one that can't be used, as any
// other, with why.
type invitationEnded struct{ cause string }

func (ended invitationEnded) Error() string {
	return errorCodeSignupInvitationInvalid + ": " + ended.cause
}

// Is reads it as an invitation's refusal, and as a guest invite link's
// where it is one's.
func (ended invitationEnded) Is(target error) bool {
	return target == errSignupInvitationInvalid ||
		target == errPageGuestLinkInvalid && strings.HasPrefix(ended.cause, "guestLink")
}

// invitationEndedCause is why an invitation ended, where it says; "" otherwise.
func invitationEndedCause(err error) string {
	var ended invitationEnded
	if errors.As(err, &ended) {
		return ended.cause
	}
	return ""
}

// nameInvitationEndedFor is why the invitation by name a code opens lets no
// one in; the refusal without why where it still waits.
func nameInvitationEndedFor(app core.App, token string, now time.Time) error {
	stored := struct {
		Status    string `db:"status"`
		ExpiresAt int64  `db:"expires_at"`
		Deletion  string `db:"deletion_job_id"`
	}{}
	err := app.DB().NewQuery(`SELECT [[invitation]].[[status]], [[invitation]].[[expires_at]], [[workspace]].[[deletion_job_id]]
		FROM {{leafen_workspace_name_invitations}} [[invitation]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[invitation]].[[workspace_id]]
		WHERE [[invitation]].[[token]] = {:token}`).Bind(dbx.Params{"token": strings.TrimSpace(token)}).One(&stored)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return invitationEnded{nameInvitationUnknown}
	case err != nil:
		return err
	case stored.Status == "joined":
		return invitationEnded{nameInvitationUsed}
	case stored.Status == "cancelled":
		return invitationEnded{nameInvitationWithdrawn}
	case stored.Deletion != "":
		return invitationEnded{invitationWorkspaceDeleted}
	case stored.ExpiresAt <= now.UnixMilli():
		return invitationEnded{nameInvitationExpired}
	}
	return memberAddsStoppedOr(app, errSignupInvitationInvalid)
}

// inviteLinkEndedFor is why the workspace's or teamspace's invite link a
// code opens lets no one in: one that is on and still lets workspaces add
// members has been turned off for its person all the same (whoever turned
// it on is no longer an owner).
func inviteLinkEndedFor(app core.App, token string) error {
	stored := struct {
		Enabled  bool   `db:"enabled"`
		Gone     bool   `db:"gone"`
		Deletion string `db:"deletion_job_id"`
	}{}
	token = strings.TrimSpace(token)
	var err error
	if key, teamspace := strings.CutPrefix(token, teamspaceInviteTokenPrefix); teamspace {
		err = app.DB().NewQuery(`SELECT [[link]].[[enabled]], ([[teamspace]].[[deleted_at]] <> 0 OR [[teamspace]].[[archived_at]] <> 0) AS [[gone]],
				[[workspace]].[[deletion_job_id]]
			FROM {{leafen_teamspace_invite_links}} [[link]]
			JOIN {{leafen_teamspaces}} [[teamspace]] ON [[teamspace]].[[id]] = [[link]].[[teamspace_id]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[link]].[[workspace_id]]
			WHERE [[link]].[[token]] = {:token}`).Bind(dbx.Params{"token": key}).One(&stored)
	} else {
		err = app.DB().NewQuery(`SELECT [[link]].[[enabled]], 0 AS [[gone]], [[workspace]].[[deletion_job_id]]
			FROM {{leafen_workspace_invite_links}} [[link]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[link]].[[workspace_id]]
			WHERE [[link]].[[token]] = {:token}`).Bind(dbx.Params{"token": token}).One(&stored)
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return invitationEnded{inviteLinkUnknown}
	case err != nil:
		return err
	case stored.Deletion != "":
		return invitationEnded{invitationWorkspaceDeleted}
	case !stored.Enabled || stored.Gone:
		return invitationEnded{inviteLinkOff}
	}
	return memberAddsStoppedOr(app, invitationEnded{inviteLinkOff})
}

// memberAddsStoppedOr is why an invitation that adds a member lets no one
// in where the server no longer lets workspaces add members; ended otherwise.
func memberAddsStoppedOr(app core.App, ended error) error {
	policy, err := loadWorkspaceMemberAddPolicy(app)
	if err != nil {
		return err
	}
	if policy != administratorMemberAddPolicyEnabled {
		return invitationEnded{invitationMemberAddsStopped}
	}
	return ended
}
