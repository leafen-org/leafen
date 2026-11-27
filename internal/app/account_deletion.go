package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person deletes their own account as Notion lets them: the workspaces
// they alone administer are deleted with it, which in Leafen is every
// workspace they own, since a Leafen workspace has one owner, and they leave
// the others. They type their email to confirm. Two cases Notion doesn't
// have wait for someone else first: a server's only administrator, and an
// owner whose organization holds workspaces other people own, whom a server
// administrator's account deletion hands to a successor. The deletion is the
// account cleanup a server administrator starts
// (administrator_account_cleanup.go), deleting the owned workspaces where
// that one hands them to successors; the person is signed out at once.

var (
	errAccountDeletionInvalid              = errors.New(errorCodeAccountDeletionInvalid)
	errAccountDeletionConfirmationMismatch = errors.New(errorCodeAccountDeletionConfirmationMismatch)
	errAccountDeletionBlocked              = errors.New(errorCodeAccountDeletionBlocked)
)

// accountDeletionBlockedError is what keeps a person's account from being
// deleted yet, by the preview's blocker.
type accountDeletionBlockedError struct{ blocker string }

func (blocked accountDeletionBlockedError) Error() string { return errorCodeAccountDeletionBlocked }
func (blocked accountDeletionBlockedError) Is(target error) bool {
	return target == errAccountDeletionBlocked ||
		(blocked.blocker == accountDeletionBlockerLegalHold || blocked.blocker == accountDeletionBlockerLegalHoldCustodian) &&
			target == errLegalHoldPreventsPermanentDelete
}

// accountDeletionRefusal is how a deletion the blocker keeps from going
// ahead is refused, in the words the settings window's deletion says it
// (workspaceSettingsDialog's accountDeletionBlocked…), whoever asks: the
// person signed in, or their agent's key before the person is asked
// (mcp_action_current_values.go). It is no refusal when nothing blocks it.
func accountDeletionRefusal(blocker string) (status int, code, message string) {
	switch blocker {
	case accountDeletionBlockerLastAdministrator:
		return http.StatusConflict, errorCodeAccountDeletionBlocked,
			"You're this server's only administrator. Make someone else an administrator before you delete your account."
	case accountDeletionBlockerSharedOrganization:
		return http.StatusConflict, errorCodeAccountDeletionBlocked,
			"Your organization holds workspaces other people own. Ask a server administrator to delete your account; " +
				"they choose who takes over ownership."
	case accountDeletionBlockerLegalHold:
		return http.StatusPreconditionFailed, errorCodeLegalHoldPreventsPermanentDelete,
			"A legal hold keeps one of your workspaces. Ask your organization to release it first."
	case accountDeletionBlockerLegalHoldCustodian:
		return http.StatusPreconditionFailed, errorCodeLegalHoldPreventsPermanentDelete,
			"A legal hold keeps your account. Ask your organization to release it first."
	}
	return 0, "", ""
}

type storedAccountDeletionWorkspace struct {
	ID          string `db:"id"`
	Name        string `db:"name"`
	Icon        string `db:"icon"`
	MemberCount int    `db:"member_count"`
}

// accountOwnedWorkspaceSQL is true of a workspace the user owns.
const accountOwnedWorkspaceSQL = `([[workspace]].[[workspace_owner]]={:userId}
	OR ([[workspace]].[[workspace_owner]]='' AND [[workspace]].[[owner]]={:userId}))`

func registerAccountDeletionRoutes(event *core.ServeEvent, rooms workspaceRoomInvalidator) {
	event.Router.GET(apiRoot+"/account/deletion", readAccountDeletion)
	event.Router.POST(apiRoot+"/account/deletion", startAccountDeletion(rooms))
}

func readAccountDeletion(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	preview, err := accountDeletionPreview(event.App, user.Id)
	if err != nil {
		return accountDeletionError(event, err, "preview")
	}
	preview.Email = user.Email()
	return event.JSON(http.StatusOK, preview)
}

// accountDeletionPreview lists the workspaces a user's account deletion
// deletes and those they leave, and what keeps them from deleting it.
func accountDeletionPreview(app core.App, userID string) (accountDeletionPreviewResponse, error) {
	preview := accountDeletionPreviewResponse{
		DeletedWorkspaces: []accountDeletionWorkspace{}, LeftWorkspaces: []accountDeletionWorkspace{},
		Blocker: accountDeletionBlockerNone,
	}
	params := dbx.Params{"userId": userID, "limit": accountDeletionWorkspaceListLimit}
	owned := `FROM {{leafen_workspaces}} AS [[workspace]]
		WHERE ` + accountOwnedWorkspaceSQL + ` AND [[workspace]].[[deletion_job_id]]=''`
	left := `FROM {{leafen_workspace_memberships}} AS [[membership]]
		JOIN {{leafen_workspaces}} AS [[workspace]] ON [[workspace]].[[id]]=[[membership]].[[workspace_id]]
		WHERE [[membership]].[[user_id]]={:userId} AND [[workspace]].[[deletion_job_id]]=''
			AND NOT ` + accountOwnedWorkspaceSQL
	for _, list := range []struct {
		from     string
		items    *[]accountDeletionWorkspace
		count    *int
		distinct string
	}{
		{owned, &preview.DeletedWorkspaces, &preview.DeletedWorkspaceCount, ""},
		{left, &preview.LeftWorkspaces, &preview.LeftWorkspaceCount, "DISTINCT "},
	} {
		rows := []storedAccountDeletionWorkspace{}
		if err := app.DB().NewQuery(`SELECT ` + list.distinct + `[[workspace]].[[id]], [[workspace]].[[name]], [[workspace]].[[icon]],
			` + workspaceMemberCountSQL("[[workspace]].[[id]]") + ` AS [[member_count]] ` + list.from + `
			ORDER BY [[workspace]].[[name]], [[workspace]].[[id]] LIMIT {:limit}`).Bind(params).All(&rows); err != nil {
			return preview, err
		}
		for _, row := range rows {
			*list.items = append(*list.items, accountDeletionWorkspace{ID: row.ID, Name: row.Name, Icon: row.Icon, MemberCount: row.MemberCount})
		}
		if err := app.DB().NewQuery(`SELECT COUNT(` + list.distinct + `[[workspace]].[[id]]) ` + list.from).
			Bind(params).Row(list.count); err != nil {
			return preview, err
		}
	}
	var administrator, administrators int
	if err := app.DB().NewQuery(`SELECT
			EXISTS (SELECT 1 FROM {{leafen_administrators}} WHERE [[user_id]]={:userId}),
			(SELECT COUNT(*) FROM {{leafen_administrators}})`).Bind(params).Row(&administrator, &administrators); err != nil {
		return preview, err
	}
	if administrator == 1 && administrators == 1 {
		preview.Blocker = accountDeletionBlockerLastAdministrator
		return preview, nil
	}
	if err := assertNoLegalHoldOnAccount(app, userID); err != nil {
		if !errors.Is(err, errLegalHoldPreventsPermanentDelete) {
			return preview, err
		}
		// Which hold it is, for the words: one over a workspace of theirs (or
		// the whole organization), or one naming the person as custodian
		// (2026-11-13: a custodian hold read "one of your workspaces").
		preview.Blocker = accountDeletionBlockerLegalHold
		if workspaces := assertNoLegalHoldOnAccountAs(app, userID, false); workspaces == nil {
			preview.Blocker = accountDeletionBlockerLegalHoldCustodian
		} else if !errors.Is(workspaces, errLegalHoldPreventsPermanentDelete) {
			return preview, workspaces
		}
		return preview, nil
	}
	var shared int
	if err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_workspaces}} AS [[workspace]]
		JOIN {{leafen_organizations}} AS [[organization]] ON [[organization]].[[id]]=[[workspace]].[[organization]]
		WHERE [[organization]].[[owner]]={:userId} AND [[workspace]].[[deletion_job_id]]=''
			AND NOT ` + accountOwnedWorkspaceSQL + `)`).Bind(params).Row(&shared); err != nil {
		return preview, err
	}
	if shared == 1 {
		preview.Blocker = accountDeletionBlockerSharedOrganization
	}
	return preview, nil
}

func startAccountDeletion(rooms workspaceRoomInvalidator) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		input := accountDeletionInput{}
		if err := event.BindBody(&input); err != nil {
			return accountDeletionError(event, errAccountDeletionInvalid, "bind")
		}
		input.RequestID = strings.TrimSpace(input.RequestID)
		if input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > maxAccountDeletionRequestIDRunes ||
			strings.TrimSpace(input.Email) == "" || len(input.Email) > maxAuthEmailLength {
			return accountDeletionError(event, errAccountDeletionInvalid, "validate")
		}
		if !accountDeletionConfirmed(user, input.Email) {
			return accountDeletionError(event, errAccountDeletionConfirmationMismatch, "confirm")
		}
		// A table that keeps rows by person and says nothing of deleting an
		// account refuses the deletion before it changes anything
		// (account_deletion_inventory.go).
		if err := validateAccountDeletionInventory(event.App); err != nil {
			return accountDeletionError(event, err, "inventory")
		}
		release := acquireProductOperationLock(administratorAccountLockNamespace, user.Id)
		defer release()
		deleted := []string{}
		change := func(txApp core.App) error {
			preview, err := accountDeletionPreview(txApp, user.Id)
			if err != nil {
				return err
			}
			if preview.Blocker != accountDeletionBlockerNone {
				return accountDeletionBlockedError{blocker: preview.Blocker}
			}
			state, err := loadAdministratorAccountState(txApp, user.Id)
			if err != nil {
				return err
			}
			if state.Status != administratorAccountStatusActive {
				return errAdministratorAccountRevision
			}
			deleted, err = admitAccountWorkspaceDeletions(txApp, user.Id, input.RequestID)
			if err != nil {
				return err
			}
			return admitAccountDeletion(txApp, user.Id, input.RequestID, state, len(deleted))
		}
		// A key's call asks its person only for a deletion that would start
		// (user_key_acting_ask.go).
		err := rehearseBeforeAsking(event, change)
		if err == nil {
			if asked, askErr := askBeforeActing(event); asked {
				return askErr
			}
			err = event.App.RunInTransaction(change)
		}
		if err != nil {
			return accountDeletionError(event, err, "admit")
		}
		// The runner shuts the workspaces' rooms again before deleting each.
		for _, workspaceID := range deleted {
			if rooms != nil {
				_ = rooms.InvalidateWorkspace(workspaceID)
			}
		}
		notifyAdministratorAccountCleanupRunner(event.App)
		return event.JSON(http.StatusAccepted, accountDeletionResponse{
			RequestID: input.RequestID, DeletedWorkspaceCount: len(deleted),
		})
	}
}

// assertNoLegalHoldOnAccount fails with errLegalHoldPreventsPermanentDelete
// when an active legal hold covers a person: their organization or a
// workspace of theirs whole, or them as custodian. Their account goes only
// after the hold is released, whoever asks.
func assertNoLegalHoldOnAccount(app core.App, userID string) error {
	return assertNoLegalHoldOnAccountAs(app, userID, true)
}

// assertNoLegalHoldOnAccountAs is assertNoLegalHoldOnAccount, the person as
// custodian counted or, without it, only their organization and workspaces.
func assertNoLegalHoldOnAccountAs(app core.App, userID string, custodian bool) error {
	custodians := []string{}
	if custodian {
		custodians = []string{userID}
	}
	rows := []struct {
		WorkspaceID    string `db:"id"`
		OrganizationID string `db:"organization"`
	}{}
	if err := app.DB().NewQuery(`SELECT DISTINCT [[workspace]].[[id]], [[workspace]].[[organization]]
		FROM {{leafen_workspaces}} AS [[workspace]]
		WHERE [[workspace]].[[deletion_job_id]]='' AND (` + accountOwnedWorkspaceSQL + ` OR EXISTS (
			SELECT 1 FROM {{leafen_workspace_memberships}} AS [[membership]]
			WHERE [[membership]].[[workspace_id]]=[[workspace]].[[id]] AND [[membership]].[[user_id]]={:userId}))`).
		Bind(dbx.Params{"userId": userID}).All(&rows); err != nil {
		return err
	}
	for _, row := range rows {
		if err := assertNoActiveLegalHoldForPermanentDelete(app, row.OrganizationID, legalHoldDeletionTarget{
			WorkspaceID: row.WorkspaceID, CustodianUserIDs: custodians,
		}); err != nil {
			return err
		}
	}
	return nil
}

// admitAccountWorkspaceDeletions starts the deletion of every workspace a
// user owns, as its owner deleting it does, and returns them.
func admitAccountWorkspaceDeletions(app core.App, userID, requestID string) ([]string, error) {
	owned := []storedAccountDeletionWorkspace{}
	if err := app.DB().NewQuery(`SELECT [[workspace]].[[id]], [[workspace]].[[name]]
		FROM {{leafen_workspaces}} AS [[workspace]]
		WHERE ` + accountOwnedWorkspaceSQL + ` AND [[workspace]].[[deletion_job_id]]=''
		ORDER BY [[workspace]].[[id]]`).Bind(dbx.Params{"userId": userID}).All(&owned); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(owned))
	for _, workspace := range owned {
		if _, err := admitWorkspaceDeletion(
			app, userID, workspace.ID, accountWorkspaceDeletionRequestID(requestID, workspace.ID), workspace.Name,
		); err != nil {
			return nil, err
		}
		ids = append(ids, workspace.ID)
	}
	return ids, nil
}

func accountWorkspaceDeletionRequestID(requestID, workspaceID string) string {
	return "account-deletion:" + requestID + ":" + workspaceID
}

// admitAccountDeletion marks the account as being deleted, signs it out, and
// queues its cleanup.
func admitAccountDeletion(
	app core.App, userID, requestID string, state storedAdministratorAccountState, workspaces int,
) error {
	return admitAccountDeletionBy(app, userID, userID, requestID, state, workspaces)
}

// admitAccountDeletionBy is admitAccountDeletion asked by someone: the
// account itself, or the organization administrator who deleted the managed
// user (organization_managed_user_deletion.go).
func admitAccountDeletionBy(
	app core.App, userID, actorID, requestID string, state storedAdministratorAccountState, workspaces int,
) error {
	jobID := core.GenerateDefaultRandomId()
	payload, err := json.Marshal(administratorAccountCleanupPayload{
		SchemaVersion: administratorAccountCleanupPayloadFormat, TargetUserID: userID,
		Phase: administratorAccountCleanupPhaseWorkspaces, Revision: 1, SelfDeletion: true,
	})
	if err != nil {
		return err
	}
	cleanupItems, err := countAdministratorAccountCleanupItems(app, userID)
	if err != nil {
		return err
	}
	hash := administratorAccountCleanupRequestHash(userID, state.Revision)
	now := time.Now().UTC().UnixMilli()
	if _, err := revokeAllBrowserSessionsCount(app, userID); err != nil {
		return err
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_administrator_account_states}}
		SET [[status]]='deleting',[[revision]]=[[revision]]+1,[[request_id]]={:requestId},[[request_hash]]={:requestHash},
			[[cleanup_job_id]]={:jobId},[[updated_by]]={:actorId},[[updated_at]]={:updated}
		WHERE [[user_id]]={:userId} AND [[revision]]={:revision} AND [[status]]='active'`).Bind(dbx.Params{
		"requestId": requestID, "requestHash": hash, "jobId": jobID, "userId": userID, "actorId": actorID,
		"updated": now, "revision": state.Revision,
	}).Execute()
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return refusedUnlessStorage(errAdministratorAccountRevision, err)
	}
	if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_jobs}}
		([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],[[status]],
		 [[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],[[payload_json]],
		 [[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],[[chunk_count]])
		VALUES ({:id},{:userId},'',{:requestId},{:requestHash},{:kind},{:userId},'queued','','',{:now},
		 {:now},{:now},{:payload},'',0,{:total},0,3,0)`).Bind(dbx.Params{
		"id": jobID, "userId": userID, "requestId": requestID, "requestHash": hash,
		"kind": administratorAccountCleanupJobKind, "now": now, "payload": string(payload),
		"total": workspaces + cleanupItems,
	}).Execute(); err != nil {
		return err
	}
	_, err = app.DB().Insert(administratorAccountAuditTable, dbx.Params{
		"id": core.GenerateDefaultRandomId(), "actor_id": actorID, "target_user_id": userID,
		"action": "delete-start", "from_status": state.Status, "to_status": administratorAccountStatusDeleting,
		"revision": state.Revision + 1, "request_id": requestID, "created_at": now, "target_email": deletionAuditEmail(app, userID),
	}).Execute()
	return err
}

func accountDeletionError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusServiceUnavailable, errorCodeAccountDeletionFailed, "Couldn't delete the account."
	blocked := accountDeletionBlockedError{}
	switch {
	case errors.As(err, &blocked):
		if refusal, refusedCode, said := accountDeletionRefusal(blocked.blocker); refusal != 0 {
			status, code, message = refusal, refusedCode, said
		}
	case errors.Is(err, errAccountDeletionInvalid):
		status, code, message = http.StatusBadRequest, errorCodeAccountDeletionInvalid, "Check the account deletion request."
	case errors.Is(err, errAccountDeletionConfirmationMismatch):
		status, code, message = http.StatusBadRequest,
			errorCodeAccountDeletionConfirmationMismatch, "The email doesn't match your account."
	case errors.Is(err, errAccountDeletionBlocked):
		status, code, message = http.StatusConflict,
			errorCodeAccountDeletionBlocked, "Someone else must take over before you delete your account."
	case errors.Is(err, errWorkspaceDeleteConflict), errors.Is(err, errAdministratorAccountRevision):
		status, code, message = http.StatusConflict,
			errorCodeWorkspaceDeleteConflict, "Another operation is in progress. Try again shortly."
	case errors.Is(err, errLegalHoldPreventsPermanentDelete):
		status, code, message = http.StatusPreconditionFailed,
			errorCodeLegalHoldPreventsPermanentDelete, "Release the active legal hold, then delete."
	}
	return writeAPIError(
		event, status, code, message, "account_delete", stage, code, err, diagnosticSourcePosition(1),
	)
}

// deleteAccountWorkspace takes the next step deleting a workspace a person's
// own account deletion deletes; once none is left, their account is cleaned
// up, as an administrator's deletion of it ends.
func (r *durableJobRunner) deleteAccountWorkspace(
	ctx context.Context, job storedJob, payload administratorAccountCleanupPayload,
) error {
	next := struct {
		ID         string `db:"id"`
		Name       string `db:"name"`
		DeletionID string `db:"deletion_job_id"`
	}{}
	err := r.app.DB().NewQuery(`SELECT [[workspace]].[[id]], [[workspace]].[[name]], [[workspace]].[[deletion_job_id]]
		FROM {{leafen_workspaces}} AS [[workspace]] WHERE ` + accountOwnedWorkspaceSQL + `
		ORDER BY [[workspace]].[[id]] LIMIT 1`).Bind(dbx.Params{"userId": payload.TargetUserID}).One(&next)
	if errors.Is(err, sql.ErrNoRows) {
		return advanceAdministratorAccountCleanupPhase(r.app, job, payload, administratorAccountCleanupPhaseAccount)
	}
	if err != nil {
		return err
	}
	if r.rooms == nil {
		return r.deferAdministratorAccountCleanup(ctx, job, errors.New("workspace room invalidator is unavailable"))
	}
	if next.DeletionID == "" {
		// A workspace that became theirs after the deletion began.
		admission, err := admitWorkspaceDeletion(
			r.app, payload.TargetUserID, next.ID, accountWorkspaceDeletionRequestID(job.RequestID, next.ID), next.Name,
		)
		if err != nil {
			return r.deferAdministratorAccountCleanup(ctx, job, err)
		}
		next.DeletionID = admission.Job.ID
	}
	if err := r.rooms.InvalidateWorkspace(next.ID); err != nil {
		return r.deferAdministratorAccountCleanup(ctx, job, err)
	}
	deletion, err := advanceWorkspaceDeletion(r.app, next.DeletionID)
	if err != nil {
		return r.deferAdministratorAccountCleanup(ctx, job, err)
	}
	if deletion.Status != workspaceDeletionStatusCompleted {
		return nil
	}
	_, err = r.app.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[processed_items]]=[[processed_items]]+1
		WHERE [[id]]={:id} AND [[kind]]={:kind} AND [[status]]='queued'`).Bind(dbx.Params{
		"id": job.ID, "kind": administratorAccountCleanupJobKind,
	}).Execute()
	return err
}

// accountDeletionConfirmed says the person typed their email to delete the
// account, or their name where it has no email: an empty email never
// confirms.
func accountDeletionConfirmed(user *core.Record, typed string) bool {
	typed = strings.TrimSpace(typed)
	if email := strings.TrimSpace(user.Email()); email != "" {
		return strings.EqualFold(typed, email)
	}
	name := strings.TrimSpace(user.GetString("name"))
	return name != "" && typed == name
}
