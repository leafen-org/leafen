package app

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Taking a guest invite link (page_guest_links.go): the account — made by
// the link a moment before, or signed in — comes into the workspace's
// organization as a guest where it isn't in it yet, and each of the link's
// pages is shared with it, with the link's role, as the link's maker would
// share it now (executePublicMutationTransaction): a page no longer there
// (deleted, or in the trash), one its maker no longer may share, or one its
// organization's or teamspace's rules now keep guests from, is left out, and
// a page the account already reaches as widely keeps its access. A link that
// leaves out every page lets no one in, and says it has none left to share.
// A link for one person is spent first, so two taking it at once don't both
// get in. One of whose pages an operation holds for a while isn't taken until
// it is let go (page_guest_link_held.go).

const pageGuestLinkAdmittedVia = "page_guest_link"

// takePageGuestLink lets the account into the link's pages and answers the
// first one, which opens next.
func takePageGuestLink(ctx context.Context, txApp core.App, userID, token string, now time.Time) (invitationTakeResponse, error) {
	link, err := loadUsablePageGuestLink(txApp, token, now)
	if err != nil {
		return invitationTakeResponse{}, err
	}
	spent, err := txApp.DB().NewQuery(`UPDATE {{leafen_page_guest_links}}
		SET [[uses]] = [[uses]] + 1, [[last_used_by]] = {:user}, [[last_used_at]] = {:now}
		WHERE [[id]] = {:id} AND ` + pageGuestLinkUsable).
		Bind(dbx.Params{"id": link.ID, "user": userID, "now": now.UnixMilli()}).Execute()
	if err != nil {
		return invitationTakeResponse{}, err
	}
	if taken, err := spent.RowsAffected(); err != nil || taken == 0 {
		if err == nil {
			// Taken, turned off or ended a moment ago: said as it is now.
			err = pageGuestLinkEndedFor(txApp, pageGuestLinkTokenHash(strings.TrimSpace(token)), now)
		}
		return invitationTakeResponse{}, err
	}
	workspace, err := txApp.FindRecordById(workspacesCollection, link.WorkspaceID)
	if err != nil {
		return invitationTakeResponse{}, err
	}
	if err := admitOrganizationGuestVia(txApp, workspace.GetString("organization"), link.CreatedBy, userID, pageGuestLinkAdmittedVia); err != nil {
		return invitationTakeResponse{}, err
	}
	wanted := accessRequestRoleFromName(link.Role)
	first := ""
	for _, pageID := range link.Left {
		had, err := pageAccessRoleOf(txApp, userID, pageID)
		if err != nil {
			return invitationTakeResponse{}, err
		}
		if had < wanted {
			granted, err := grantPageGuestLinkPage(ctx, txApp, link, pageID, userID)
			if err != nil {
				return invitationTakeResponse{}, err
			}
			if !granted {
				continue
			}
		}
		if first == "" {
			first = pageID
		}
	}
	if first == "" {
		return invitationTakeResponse{}, invitationEnded{pageGuestLinkNoPages}
	}
	return invitationTakeResponse{WorkspaceID: link.WorkspaceID, PageID: first}, nil
}

// grantPageGuestLinkPage shares one page with the account as the link's
// maker, false where it is left out.
func grantPageGuestLinkPage(ctx context.Context, txApp core.App, link storedPageGuestLink, pageID, userID string) (bool, error) {
	state, err := loadStoredPublicMutationState(txApp, pageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	_, _, err = executePublicMutationTransaction(ctx, txApp, link.CreatedBy, pageID, publicMutationInput{
		RequestID: "guest-link:" + link.ID + ":" + userID + ":" + pageID, Action: publicMutationActionGrantPermission,
		PrincipalType: publicMutationPrincipalUser, PrincipalID: userID, Role: link.Role, ExpectedRevision: state.Revision,
	})
	switch {
	case err == nil:
		return true, nil
	case pageAccessHeld(err) != "":
		return false, guestLinkPageHeld{pageID: pageID, busy: pageAccessHeld(err)}
	case errors.Is(err, errPublicMutationNotFound), isPermissionDenied(err), errors.Is(err, errPublicMutationInTrash),
		errors.Is(err, errPublicMutationInvalid), errors.Is(err, errOrganizationSharingSecurityBlocked),
		errors.Is(err, errGuestInviteRequiresApproval), isDLPPolicyDecisionError(err),
		errors.Is(err, errPersonPageRestrictedInvite), errors.Is(err, errPersonPageShareLinked):
		return false, nil
	}
	return false, err
}

// lockPageGuestLinkPages holds the sharing lock of each page a guest invite
// link names, in one order, for a take's transaction, which shares them
// (executePublicMutation holds it the same way); nothing for another token.
func lockPageGuestLinkPages(app core.App, token string) func() {
	if !strings.HasPrefix(strings.TrimSpace(token), pageGuestLinkTokenPrefix) {
		return func() {}
	}
	link, err := loadUsablePageGuestLink(app, token, time.Now())
	if err != nil {
		return func() {}
	}
	pages := link.pages()
	slices.Sort(pages)
	releases := make([]func(), 0, len(pages))
	for _, pageID := range pages {
		releases = append(releases, acquireProductOperationLock(publicMutationLockNamespace, link.WorkspaceID+"\x00"+pageID))
	}
	return func() {
		for index := len(releases) - 1; index >= 0; index-- {
			releases[index]()
		}
	}
}
