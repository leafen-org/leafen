package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where sign-up is closed, an invitation still lets its person make an
// account (2026-10-18): an email invitation — to a workspace as a member,
// or to a page as a guest — carries a sign-up token in its link, sent by
// mail or copied by the inviter where the server sends no mail; a
// workspace's or teamspace's invite link lets anyone who has it sign up and
// ask to join. An email invitation's token proves its address: only that
// address may use it, only while the invitation waits, and within
// signupInvitationLifetime. An account made from an invite link keeps an
// unproven address (account_email_proof.go).
const (
	signupInvitationTokenPrefix = "inv_"
	// A member's invitation lasts a short time (the multi-server decision of
	// 2026-10-20), as an invitation by name does (workspace_name_invitations.go).
	signupInvitationLifetime  = 7 * 24 * time.Hour
	signupInvitationWorkspace = "workspace"
	signupInvitationPage      = "page"
)

var errSignupInvitationInvalid = errors.New(errorCodeSignupInvitationInvalid)

// ensureSignupInvitation keeps one sign-up token for an email invitation and
// returns it.
func ensureSignupInvitation(app core.App, kind, sourceID, email string, now time.Time) (string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_signup_invitations}} ([[token]], [[kind]], [[source_id]], [[email]], [[created_at]])
		VALUES ({:token}, {:kind}, {:source}, {:email}, {:now})
		ON CONFLICT ([[kind]], [[source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"token": signupInvitationTokenPrefix + base64.RawURLEncoding.EncodeToString(random),
		"kind":  kind, "source": sourceID, "email": normalizePageShareEmail(email), "now": now.UnixMilli(),
	}).Execute(); err != nil {
		return "", err
	}
	return signupInvitationToken(app, kind, sourceID)
}

// signupInvitationToken is an email invitation's sign-up token, or "" where
// it has none.
func signupInvitationToken(app core.App, kind, sourceID string) (string, error) {
	token := ""
	err := app.DB().NewQuery(`SELECT [[token]] FROM {{leafen_signup_invitations}}
		WHERE [[kind]] = {:kind} AND [[source_id]] = {:source}`).
		Bind(dbx.Params{"kind": kind, "source": sourceID}).Row(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return token, err
}

// signupAdmission is what lets an account be made where sign-up is closed.
type signupAdmission struct {
	// Invited is true for a valid invitation of either kind.
	Invited bool
	// ProvesEmail is true for an email invitation's token, which proves the
	// address it was sent to.
	ProvesEmail bool
	// NeedsProof is true for an address admitted by a waiting invitation or
	// an allowed domain without a token: the account waits for its
	// confirmation mail, whatever the server's own setting.
	NeedsProof bool
	// Join is the invitation that admitted it, kept with the account
	// (account_joins.go).
	Join accountJoin
}

// resolveSignupAdmission reads an invitation given at sign-up: an email
// invitation's token, which must be for email and still waiting, or a
// workspace's or teamspace's invite link that is on. No token admits
// nothing; a token that admits nothing is refused.
func resolveSignupAdmission(app core.App, token, email string, now time.Time) (signupAdmission, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return signupAdmission{}, nil
	}
	if strings.HasPrefix(token, signupInvitationTokenPrefix) {
		stored := struct {
			Kind      string `db:"kind"`
			SourceID  string `db:"source_id"`
			Email     string `db:"email"`
			CreatedAt int64  `db:"created_at"`
		}{}
		err := app.DB().NewQuery(`SELECT [[kind]], [[source_id]], [[email]], [[created_at]] FROM {{leafen_signup_invitations}}
			WHERE [[token]] = {:token}`).Bind(dbx.Params{"token": token}).One(&stored)
		if errors.Is(err, sql.ErrNoRows) {
			return signupAdmission{}, errSignupInvitationInvalid
		}
		if err != nil {
			return signupAdmission{}, err
		}
		if stored.Email != normalizePageShareEmail(email) ||
			now.Sub(time.UnixMilli(stored.CreatedAt)) > signupInvitationLifetime {
			return signupAdmission{}, errSignupInvitationInvalid
		}
		join := accountJoin{}
		switch stored.Kind {
		case signupInvitationWorkspace:
			join.Kind = accountJoinWorkspaceEmail
			err = app.DB().NewQuery(`SELECT [[workspace_id]], [[inviter_id]] FROM {{leafen_workspace_invitations}}
				WHERE [[id]] = {:id} AND [[email]] = {:email} AND [[status]] = 'pending'`).
				Bind(dbx.Params{"id": stored.SourceID, "email": stored.Email}).Row(&join.WorkspaceID, &join.InviterID)
		case signupInvitationPage:
			join.Kind = accountJoinPageEmail
			err = app.DB().NewQuery(`SELECT [[workspace_id]], [[created_by]] FROM {{leafen_page_permissions}}
				WHERE [[id]] = {:id} AND [[principal_type]] = 'email' AND [[principal_id]] = {:email}`).
				Bind(dbx.Params{"id": stored.SourceID, "email": stored.Email}).Row(&join.WorkspaceID, &join.InviterID)
		default:
			err = sql.ErrNoRows
		}
		if errors.Is(err, sql.ErrNoRows) {
			return signupAdmission{}, errSignupInvitationInvalid
		}
		if err != nil {
			return signupAdmission{}, err
		}
		return signupAdmission{Invited: true, ProvesEmail: true, Join: join}, nil
	}
	if strings.HasPrefix(token, pageGuestLinkTokenPrefix) {
		// A guest invite link lets in whoever holds it (page_guest_links.go).
		link, err := loadUsablePageGuestLink(app, token, now)
		if err != nil {
			return signupAdmission{}, asSignupInvitationInvalid(err)
		}
		return signupAdmission{Invited: true, Join: accountJoin{Kind: accountJoinPageLink, WorkspaceID: link.WorkspaceID, InviterID: link.CreatedBy}}, nil
	}
	if strings.HasPrefix(token, nameInvitationTokenPrefix) {
		// One that can't be used says why (invitation_ended.go).
		invitation, err := loadWaitingNameInvitation(app, token, now)
		if errors.Is(err, errNameInvitationInvalid) {
			err = nameInvitationEndedFor(app, token, now)
		}
		if err != nil {
			return signupAdmission{}, err
		}
		if err := memberAddsStoppedOr(app, nil); err != nil {
			return signupAdmission{}, err
		}
		return signupAdmission{Invited: true, Join: accountJoin{
			Kind: accountJoinWorkspaceName, WorkspaceID: invitation.WorkspaceID, InviterID: invitation.InviterID,
		}}, nil
	}
	join := accountJoin{}
	var err error
	if key, teamspace := strings.CutPrefix(token, teamspaceInviteTokenPrefix); teamspace {
		join.Kind = accountJoinTeamspaceLink
		err = app.DB().NewQuery(`SELECT [[link]].[[workspace_id]], [[link]].[[updated_by]] FROM {{leafen_teamspace_invite_links}} [[link]]
			JOIN {{leafen_teamspaces}} [[teamspace]] ON [[teamspace]].[[id]] = [[link]].[[teamspace_id]]
			WHERE [[link]].[[token]] = {:token} AND [[link]].[[enabled]] = 1
				AND [[teamspace]].[[deleted_at]] = 0 AND [[teamspace]].[[archived_at]] = 0`).
			Bind(dbx.Params{"token": key}).Row(&join.WorkspaceID, &join.InviterID)
	} else {
		join.Kind = accountJoinWorkspaceLink
		err = app.DB().NewQuery(`SELECT [[link]].[[workspace_id]], [[link]].[[updated_by]] FROM {{leafen_workspace_invite_links}} [[link]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[link]].[[workspace_id]]
			WHERE [[link]].[[token]] = {:token} AND [[link]].[[enabled]] = 1 AND [[workspace]].[[deletion_job_id]] = ''`).
			Bind(dbx.Params{"token": token}).Row(&join.WorkspaceID, &join.InviterID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return signupAdmission{}, inviteLinkEndedFor(app, token)
	}
	if err != nil {
		return signupAdmission{}, err
	}
	// A link opens the way only while workspaces may add members, as the
	// link's own join does (workspace_invite_links.go).
	if err := memberAddsStoppedOr(app, nil); err != nil {
		return signupAdmission{}, err
	}
	return signupAdmission{Invited: true, Join: join}, nil
}

// signupAddressAdmitted says whether, where sign-up is closed, an address
// may make its own account once the server proves it: an invitation waits
// for it — to a workspace or by email to a page — or a workspace allows its
// domain. The caller proves the address — a confirmation mail, or the sign-in
// provider — before trusting it; without a way to prove it the policy's
// notice stands.
func signupAddressAdmitted(app core.App, email string) (bool, error) {
	email = normalizePageShareEmail(email)
	domain := emailDomain(email)
	admitted := 0
	err := app.DB().NewQuery(`SELECT
		EXISTS (SELECT 1 FROM {{leafen_workspace_invitations}} WHERE [[email]] = {:email} AND [[status]] = 'pending')
		OR EXISTS (SELECT 1 FROM {{leafen_page_permissions}} WHERE [[principal_type]] = 'email' AND [[principal_id]] = {:email})
		OR ({:domain} <> '' AND EXISTS (SELECT 1 FROM {{leafen_workspace_allowed_email_domains}} [[allowed]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[allowed]].[[workspace_id]]
			WHERE [[allowed]].[[domain]] = {:domain} AND [[workspace]].[[deletion_job_id]] = ''))`).
		Bind(dbx.Params{"email": email, "domain": domain}).Row(&admitted)
	return admitted == 1, err
}

// signupAddressAdmission admits a password sign-up without a token where an
// invitation or an allowed domain admits its address and the server can
// send the confirmation that proves it.
func signupAddressAdmission(app core.App, email string) (signupAdmission, error) {
	admitted, err := signupAddressAdmitted(app, email)
	if err != nil || !admitted {
		return signupAdmission{}, err
	}
	settings, err := loadAdministratorSettings(app)
	if err != nil || !notificationEmailConfigured(settings) {
		return signupAdmission{}, err
	}
	join, err := addressAccountJoin(app, normalizePageShareEmail(email))
	if err != nil {
		return signupAdmission{}, err
	}
	return signupAdmission{Invited: true, NeedsProof: true, Join: join}, nil
}

// administratorSignupContactRunes is how long the sign-up contact may be.
const administratorSignupContactRunes = 300

// describeSignupInvitation is what an invitation's mail says (Notion's
// names who invited, to what, and their note), on its first screen for whoever
// holds its link, so a server without mail loses none of it. A link anyone may
// have been given names only its workspace: no one in particular invited them.
func describeSignupInvitation(app core.App, token string, join accountJoin, response *signupInvitationResponse) error {
	if join.WorkspaceID == "" {
		return nil
	}
	// A teamspace's link lets someone new ask to join, not join
	// (teamspace_invite_links.go): its first screen says so.
	response.AsksToJoin = join.Kind == accountJoinTeamspaceLink
	workspace, err := app.FindRecordById(workspacesCollection, join.WorkspaceID)
	if err != nil {
		return err
	}
	response.WorkspaceName = workspace.GetString("name")
	if join.Kind != accountJoinWorkspaceEmail && join.Kind != accountJoinPageEmail && join.Kind != accountJoinWorkspaceName &&
		join.Kind != accountJoinPageLink {
		return nil
	}
	if join.InviterID != "" {
		inviter, err := app.FindRecordById("users", join.InviterID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			response.InviterName = strings.TrimSpace(inviter.GetString("name"))
		}
	}
	sourceSQL := `SELECT [[source_id]] FROM {{leafen_signup_invitations}} WHERE [[token]] = {:token}`
	switch join.Kind {
	case accountJoinPageLink:
		return describePageGuestLink(app, token, response)
	case accountJoinWorkspaceEmail:
		return app.DB().NewQuery(`SELECT [[invite_message]] FROM {{leafen_workspace_invitations}} WHERE [[id]] = (` + sourceSQL + `)`).
			Bind(dbx.Params{"token": token}).Row(&response.Message)
	case accountJoinPageEmail:
		return app.DB().NewQuery(`SELECT [[share]].[[invite_message]], [[page]].[[title]] FROM {{leafen_page_permissions}} [[share]]
			JOIN {{`+pagesCollection+`}} [[page]] ON [[page]].[[id]] = [[share]].[[page_id]]
			WHERE [[share]].[[id]] = (`+sourceSQL+`)`).
			Bind(dbx.Params{"token": token}).Row(&response.Message, &response.PageTitle)
	}
	return nil
}

// readSignupInvitation is what the sign-up page shows for an invitation in
// its address, as Notion's invitation link opens sign-up for the address it
// was sent to: an email invitation's address, which the token already
// carries, or nothing for an invite link. One that admits nothing reads as
// such.
func readSignupInvitation(event *core.RequestEvent) error {
	input := signupInvitationInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the invitation.")
	}
	token := strings.TrimSpace(input.Token)
	response := signupInvitationResponse{}
	admission := signupAdmission{}
	if strings.HasPrefix(token, signupInvitationTokenPrefix) {
		err := event.App.DB().NewQuery(`SELECT [[email]] FROM {{leafen_signup_invitations}} WHERE [[token]] = {:token}`).
			Bind(dbx.Params{"token": token}).Row(&response.Email)
		if err == nil {
			admission, err = resolveSignupAdmission(event.App, token, response.Email, time.Now())
		}
		if err != nil {
			response.Email = ""
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errSignupInvitationInvalid) {
				return signupPolicyAPIError(event, errSignupInvitationInvalid)
			}
			return signupPolicyAPIError(event, errSignupPolicyUnavailable)
		}
	} else {
		var err error
		admission, err = resolveSignupAdmission(event.App, token, "", time.Now())
		if err == nil && strings.HasPrefix(token, nameInvitationTokenPrefix) {
			// The name the inviter wrote, to start with.
			var invitation waitingNameInvitation
			if invitation, err = loadWaitingNameInvitation(event.App, token, time.Now()); errors.Is(err, errNameInvitationInvalid) {
				err = nameInvitationEndedFor(event.App, token, time.Now())
			}
			response.Name = invitation.Name
		}
		if err != nil {
			// One that can't be used keeps why (invitation_ended.go).
			if errors.Is(err, errSignupInvitationInvalid) {
				return signupPolicyAPIError(event, err)
			}
			return signupPolicyAPIError(event, errSignupPolicyUnavailable)
		}
	}
	if err := describeSignupInvitation(event.App, token, admission.Join, &response); err != nil {
		return signupPolicyAPIError(event, errSignupPolicyUnavailable)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}
