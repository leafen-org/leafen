package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person a page is shared with hears of it in their inbox, as someone
// Notion invites to a page does: the notification names who shared it and
// the page, and becomes an email as their other personal notifications do.
// Sharing again with someone the page is already shared with, or with
// oneself, tells no one. A person has one invitation to a page in their
// inbox, as Notion's: shared with again after the share was taken away, with
// another message, their invitation was one line, its time "지금" and its
// message the new one (seen once, 2026-11-21 in the second account's inbox,
// a synthetic page under the test page) — so a new share's invitation takes
// the place of the person's earlier one to the page (replaceEarlierPageInvite).

// pageShareStoredPrincipal is how the page's share with a principal names
// them, or "" while the page isn't shared with them. An address matches in
// any case.
func pageShareStoredPrincipal(app core.App, pageID, principalType, principalID string) (string, error) {
	condition := "[[principal_id]]={:principalId}"
	if principalType == publicMutationPrincipalEmail {
		condition = "lower(trim([[principal_id]]))={:principalId}"
		principalID = normalizePageShareEmail(principalID)
	}
	stored := []string{}
	err := app.DB().NewQuery(`
		SELECT [[principal_id]] FROM {{leafen_page_permissions}}
		WHERE [[page_id]]={:pageId} AND [[principal_type]]={:principalType} AND ` + condition + `
		ORDER BY [[created_at]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "principalType": principalType, "principalId": principalID}).Column(&stored)
	if err != nil || len(stored) == 0 {
		return "", err
	}
	return stored[0], nil
}

// pageShareEmailAccount is the account that verified an address a page is
// shared with, or "" while no one has.
func pageShareEmailAccount(app core.App, email string) (string, error) {
	accounts := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]] FROM {{leafen_account_emails}}
		WHERE [[email]]={:email} AND ` + provenAccountEmailSQL("") + `
		LIMIT 2
	`).Bind(dbx.Params{"email": normalizePageShareEmail(email)}).Column(&accounts); err != nil {
		return "", err
	}
	if len(accounts) != 1 {
		return "", nil
	}
	return accounts[0], nil
}

// notifyPageShareInvite tells the account a page was newly shared with, by
// the share it was given.
func notifyPageShareInvite(app core.App, workspaceID, pageID, actorID, userID, shareID string, now time.Time, messages ...string) error {
	if userID == "" || userID == actorID {
		return nil
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	message := ""
	if len(messages) > 0 {
		message = messages[0]
	}
	// A member of the workspace has its permission line too, among the workspace's updates (page_share_permission_notices.go).
	if err := tellShareGivenToMember(app, workspaceID, page, actorID, userID, shareID, now, message); err != nil {
		return err
	}
	target, err := pageNotificationTarget(app, pageID)
	if err != nil {
		return err
	}
	stableID := func(parts ...string) string {
		digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
		return hex.EncodeToString(digest[:16])
	}
	activityKey := pageInviteActivityPrefix + shareID
	if err := replaceEarlierPageInvite(app, workspaceID, pageID, userID, activityKey); err != nil {
		return err
	}
	_, err = upsertProductNotifications(context.Background(), app, []productNotificationProjection{{
		ID:          stableID("page-share-invite", pageID, shareID, userID),
		EventID:     stableID("page-share-invite-event", pageID, shareID),
		WorkspaceID: workspaceID, UserID: userID, ActivityKey: activityKey,
		Kind: "invite", PageID: pageID, ActorID: actorID, Title: page.GetString("title"),
		Preview: message, Target: target, OccurredAt: now.UnixMilli(), Personal: true,
	}}, now.UnixMilli())
	return err
}

// inviteMessageQuoted is an invitation's message as a notice shows it under
// its sentence — the inbox, a push, the desktop app's notice, its email —
// the inviter's own words between “ and ”, as Notion's inbox quotes them
// (seen once, 2026-11-21); another notice's line, or none, as it is.
func inviteMessageQuoted(kind, line string) string {
	if kind != "invite" || strings.TrimSpace(line) == "" {
		return line
	}
	return "“" + line + "”"
}

// pageInviteActivityPrefix leads an invitation's key, the share's id after it.
const pageInviteActivityPrefix = "page-invite:"

// replaceEarlierPageInvite takes away the person's invitation to the page
// from another share than the one keyed — a share taken away and given
// again, or given by a group or an address they proved — so the new one,
// unread with its own message, time, email and push, is their one line of
// it. The same share told again keeps its line.
func replaceEarlierPageInvite(app core.App, workspaceID, pageID, userID, activityKey string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notifications}}
		WHERE [[workspace_id]]={:workspace} AND [[user_id]]={:user} AND [[kind]]='invite' AND [[page_id]]={:page}
			AND [[activity_key]] LIKE 'page-invite:%' AND [[activity_key]]!={:key}
	`).Bind(dbx.Params{"workspace": workspaceID, "user": userID, "page": pageID, "key": activityKey}).Execute()
	return err
}

// maxEmailShareNotificationsOnAdmission bounds what one address hears at once.
const maxEmailShareNotificationsOnAdmission = 200

// notifyEmailSharesOnAdmission tells an account that proved an address of the
// pages shared with that address, with the sharer's note, as an account shared
// with hears at once. Mail is only another way to hear it, and a server without
// mail has none (2026-11-03: an invitation's note reached no one there).
func notifyEmailSharesOnAdmission(app core.App, user *core.Record) error {
	if user == nil {
		return nil
	}
	shares := []struct {
		ID          string `db:"id"`
		WorkspaceID string `db:"workspace_id"`
		PageID      string `db:"page_id"`
		ActorID     string `db:"created_by"`
		Message     string `db:"invite_message"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[created_by]],[[invite_message]] FROM {{leafen_page_permissions}}
		WHERE [[principal_type]]='email' AND lower(trim([[principal_id]])) IN (` + verifiedAccountEmailSQL("userId") + `)
		ORDER BY [[created_at]] DESC, [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"userId": user.Id, "limit": maxEmailShareNotificationsOnAdmission}).All(&shares); err != nil {
		return err
	}
	now := time.Now()
	for _, share := range shares {
		if err := notifyPageShareInvite(app, share.WorkspaceID, share.PageID, share.ActorID, user.Id, share.ID, now, share.Message); err != nil {
			return err
		}
	}
	return nil
}
