package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database row page shared on its own opens by its share for someone the
// page holding the database does not open to, or opens to with a lower role:
// the higher role counts. The share reaches that row page only, not the
// database's other rows. Only active members of the workspace's organization
// hold shares, and people with full access manage them.

var errDatabaseRowPagePermissionInvalid = errors.New("database row page permission invalid")

// authorizeDatabaseRowPageRecord authorizes a person on a row page through
// its page record: the database's page role or the row page's own shares.
func authorizeDatabaseRowPageRecord(
	app core.App, principalID string, authority databaseRowPageAuthority, minimum pageAccessRole,
) (pageAuthorization, error) {
	rowPage, err := findRecordRemembered(app, pagesCollection, authority.RowID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!isDatabaseRowPageRecord(rowPage) ||
		rowPage.GetString("owner") != authority.OwnerID || rowPage.GetString("workspace") != authority.WorkspaceID) {
		return pageAuthorization{}, permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	fenced, err := pagePermanentDeleteFenceActive(app, authority.ContainerPageID)
	if err != nil {
		return pageAuthorization{}, err
	}
	if fenced {
		return pageAuthorization{}, permissionDenied()
	}
	return authorizeRowPageRecord(app, principalID, rowPage, minimum)
}

// authorizeDatabaseRowPageGrant authorizes a person on a row page through
// its page record, carrying the database's page record for work stored
// there.
func authorizeDatabaseRowPageGrant(
	app core.App, principalID string, authority databaseRowPageAuthority, minimum pageAccessRole,
) (pageAuthorization, error) {
	return authorizeDatabaseRowPageRecord(app, principalID, authority, minimum)
}

type storedDatabaseRowPagePermission struct {
	ID          string `db:"id"`
	PrincipalID string `db:"principal_id"`
	Role        string `db:"role"`
	CreatedBy   string `db:"created_by"`
	CreatedAt   int64  `db:"created_at"`
}

// loadDatabaseRowPagePermissions reads a row page's shares, and the people
// it could be shared with, a window of each from its cursor on; someone who
// cannot manage them reads none.
func loadDatabaseRowPagePermissions(app core.App, principalID, rowID, cursor, optionsCursor string) (databaseRowPagePermissionsResponse, error) {
	after, err := decodeTimeIDCursor(cursor)
	if err != nil {
		return databaseRowPagePermissionsResponse{}, rowShareRefused("cursor is the nextCursor of the last list, or none to start")
	}
	// People only: a user ID is letters and digits, so "0" starts before all.
	optionsAfter := "user:0"
	if optionsCursor = strings.TrimSpace(optionsCursor); optionsCursor != "" {
		optionsAfter = "user:" + optionsCursor
	}
	authority, err := authorizeDatabaseRowPage(app, principalID, rowID, permissionWorkspaceRead)
	if err != nil {
		return databaseRowPagePermissionsResponse{}, err
	}
	response := databaseRowPagePermissionsResponse{
		RowID: authority.RowID, Permissions: []databaseRowPagePermissionWire{}, PrincipalOptions: []databaseRowPagePrincipalOption{},
	}
	if _, err := authorizeDatabaseRowPage(app, principalID, rowID, permissionWorkspaceOwner); err != nil {
		if errors.Is(err, errDatabaseRowPageNotFound) || errors.Is(err, errDatabaseRowPageInTrash) {
			return response, nil
		}
		return databaseRowPagePermissionsResponse{}, err
	}
	response.CanManage = true
	rows := []storedDatabaseRowPagePermission{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[principal_id]], [[role]], [[created_by]], [[created_at]]
		FROM {{leafen_page_permissions}}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:rowId} AND [[principal_type]]='user'
			AND ({:afterId}='' OR [[created_at]]>{:afterAt} OR ([[created_at]]={:afterAt} AND [[id]]>{:afterId}))
		ORDER BY [[created_at]], [[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": authority.WorkspaceID, "rowId": authority.RowID, "afterAt": after.At, "afterId": after.ID,
		"limit": databaseRowPagePermissionWindow + 1,
	}).All(&rows); err != nil {
		return databaseRowPagePermissionsResponse{}, err
	}
	if len(rows) > databaseRowPagePermissionWindow {
		rows = rows[:databaseRowPagePermissionWindow]
		last := rows[len(rows)-1]
		response.NextCursor = encodeTimeIDCursor(last.CreatedAt, last.ID)
	}
	options, err := loadWorkspacePrincipalOptions(context.Background(), app, authority.WorkspaceID, principalID, optionsAfter, databaseRowPagePermissionWindow+1)
	if errors.Is(err, errWorkspacePrincipalDirectoryInvalid) {
		return databaseRowPagePermissionsResponse{}, rowShareRefused("optionsCursor is the optionsNextCursor of the last list, or none to start")
	}
	if err != nil {
		return databaseRowPagePermissionsResponse{}, err
	}
	if len(options) > databaseRowPagePermissionWindow {
		options = options[:databaseRowPagePermissionWindow]
		response.OptionsNextCursor = options[len(options)-1].PrincipalID
	}
	for _, option := range options {
		if option.PrincipalType == "user" {
			response.PrincipalOptions = append(response.PrincipalOptions, databaseRowPagePrincipalOption{
				PrincipalID: option.PrincipalID, Label: option.Label,
			})
		}
	}
	for _, row := range rows {
		label, email := row.PrincipalID, ""
		if user, err := app.FindRecordById("users", row.PrincipalID); err == nil {
			label, email = user.GetString("name"), user.Email()
			if strings.TrimSpace(label) == "" {
				label = email
			}
		}
		response.Permissions = append(response.Permissions, databaseRowPagePermissionWire{
			ID: row.ID, PrincipalID: row.PrincipalID, Label: label, Email: email, Role: row.Role, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt,
		})
	}
	return response, nil
}

// databaseRowPageGrantee reads whom a share names: a user ID, or the email
// of an account.
func databaseRowPageGrantee(app core.App, reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if !strings.Contains(reference, "@") {
		return reference, nil
	}
	user, err := findAccountByEmail(app, reference)
	if errors.Is(err, sql.ErrNoRows) {
		return "", rowShareOutsideRefused(true, "principalId's email is no account on this server; a row's page is shared with people who have one")
	}
	if err != nil {
		return "", err
	}
	return user.Id, nil
}

func validDatabaseRowPagePermissionRole(role string) bool {
	switch role {
	case "view", "comment", "edit", "full_access":
		return true
	}
	return false
}

// changeDatabaseRowPagePermission grants, changes or removes one share of a
// row page for someone with full access to it.
func changeDatabaseRowPagePermission(
	app core.App, principalID, rowID string, input databaseRowPagePermissionInput, now time.Time,
) error {
	return changeDatabaseRowPagePermissionNotifying(app, principalID, rowID, input, now, true)
}

// changeDatabaseRowPagePermissionNotifying is changeDatabaseRowPagePermission
// telling the person a new share is theirs, or not, for a change that tells
// them of the page itself (database_row_send.go).
func changeDatabaseRowPagePermissionNotifying(
	app core.App, principalID, rowID string, input databaseRowPagePermissionInput, now time.Time, invite bool,
) error {
	if err := app.RunInTransaction(databaseRowPagePermissionChange(principalID, rowID, input, now, invite)); err != nil {
		return err
	}
	// A share of an unlinked item changes who its database's views show it to.
	return announceUnlinkedRowShareChange(app, rowID)
}

// databaseRowPagePermissionChange is the change of who a row's page is
// shared with, made in one transaction.
func databaseRowPagePermissionChange(
	principalID, rowID string, input databaseRowPagePermissionInput, now time.Time, invite bool,
) func(txApp core.App) error {
	return func(txApp core.App) error {
		authority, err := authorizeDatabaseRowPage(txApp, principalID, rowID, permissionWorkspaceOwner)
		if err != nil {
			return err
		}
		params := dbx.Params{
			"workspaceId": authority.WorkspaceID, "rowId": authority.RowID, "now": now.UnixMilli(),
			"role": input.Role, "permissionId": strings.TrimSpace(input.PermissionID),
		}
		switch input.Action {
		case "grant":
			grantee, err := databaseRowPageGrantee(txApp, input.PrincipalID)
			if err != nil {
				return err
			}
			if grantee == "" {
				return rowShareRefused("principalId is empty: it names whom the row's page is shared with, by id or email")
			}
			if !validDatabaseRowPagePermissionRole(input.Role) {
				return rowShareRefused("role is %s, not %q", publicMutationRoles, input.Role)
			}
			workspace, err := txApp.FindRecordById(workspacesCollection, authority.WorkspaceID)
			if err != nil {
				return err
			}
			if _, err := authorizeOrganization(
				txApp, grantee, workspace.GetString("organization"), permissionOrganizationActive,
			); err != nil {
				if isPermissionDenied(err) {
					return rowShareOutsideRefused(false, "principalId names no person of this workspace's organization")
				}
				return err
			}
			if err := enforceSharingSecurityShare(
				txApp, workspace.GetString("organization"), authority.WorkspaceID, publicMutationPrincipalUser, grantee,
			); err != nil {
				return err
			}
			if err := enforceTeamspaceShare(
				txApp, authority.RowID, workspace.GetString("organization"), authority.WorkspaceID, publicMutationPrincipalUser, grantee,
			); err != nil {
				return err
			}
			actorRestricted, err := workspaceMemberRestricted(txApp, authority.WorkspaceID, principalID)
			if err != nil {
				return err
			}
			if err := enforceRestrictedUserShare(txApp, workspace.GetString("organization"), authority.WorkspaceID, principalID,
				workspace.GetString("workspace_owner") == principalID, actorRestricted, grantee); err != nil {
				return err
			}
			if err := refusePersonPageRestrictedShare(txApp, authority.WorkspaceID, authority.RowID, grantee); errors.Is(err, errPersonPageRestrictedInvite) {
				return rowShareRefused("principalId names a restricted member of the workspace, with whom a person's page isn't shared")
			} else if err != nil {
				return err
			}
			stored, err := pageShareStoredPrincipal(txApp, authority.RowID, publicMutationPrincipalUser, grantee)
			existed := stored != ""
			if err != nil {
				return err
			}
			shareID := securityRandomID()
			params["id"], params["grantee"], params["actorId"] = shareID, grantee, principalID
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_page_permissions}}
					([[id]],[[workspace_id]],[[page_id]],[[principal_type]],[[principal_id]],[[role]],
					 [[created_by]],[[created_at]],[[updated_at]])
				VALUES ({:id},{:workspaceId},{:rowId},'user',{:grantee},{:role},{:actorId},{:now},{:now})
				ON CONFLICT ([[page_id]],[[principal_type]],[[principal_id]]) DO UPDATE SET
					[[role]]=excluded.[[role]],[[updated_at]]=excluded.[[updated_at]],[[rule_id]]=''
			`).Bind(params).Execute(); err != nil || existed || !invite {
				return err
			}
			return notifyPageShareInvite(txApp, authority.WorkspaceID, authority.RowID, principalID, grantee, shareID, now)
		case "update":
			if params["permissionId"] == "" {
				return rowShareRefused("permissionId is empty: it names the share to change")
			}
			if !validDatabaseRowPagePermissionRole(input.Role) {
				return rowShareRefused("role is %s, not %q", publicMutationRoles, input.Role)
			}
			return expectOneDatabaseRowPagePermissionChange(txApp.DB().NewQuery(`
				UPDATE {{leafen_page_permissions}} SET [[role]]={:role},[[updated_at]]={:now}
				WHERE [[id]]={:permissionId} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:rowId} AND [[rule_id]]=''
			`).Bind(params).Execute())
		case "remove":
			if params["permissionId"] == "" {
				return rowShareRefused("permissionId is empty: it names the share to remove")
			}
			if err := expectOneDatabaseRowPagePermissionChange(txApp.DB().NewQuery(`
				DELETE FROM {{leafen_page_permissions}}
				WHERE [[id]]={:permissionId} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:rowId} AND [[rule_id]]=''
			`).Bind(params).Execute()); err != nil {
				return err
			}
			// A rule that names the person gives its share back.
			return reconcileDatabasePageRuleGrantsOfPage(txApp, authority.RowID)
		}
		return rowShareRefused("action is grant, update or remove, not %q", input.Action)
	}
}

func expectOneDatabaseRowPagePermissionChange(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return rowShareRefused("permissionId names no share of this row's page; GET its shares for their ids")
	}
	return nil
}

// rowShareRefused refuses a change of who a row's page is shared with for
// the reason given (request_refusal.go).
func rowShareRefused(format string, args ...any) error {
	return refuse(errDatabaseRowPagePermissionInvalid, format, args...)
}

func databaseRowPagePermissionError(event *core.RequestEvent, err error, step string) error {
	switch {
	case errors.Is(err, errDatabaseRowPageNotFound):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Database row not found.")
	case errors.Is(err, errDatabaseRowPageInTrash):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageInTrash, "Restore the database row before sharing it.")
	case errors.Is(err, errDatabaseRowPagePermissionInvalid):
		// Someone it can't hold yet hears the way on (database_row_page_share_next.go).
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid,
			strings.TrimSpace(refusalMessage("Check who the database row is shared with.", refusalReason(err))+" "+rowShareNextWaysOf(event, err)))
	case errors.Is(err, errOrganizationSharingSecurityBlocked):
		return writeSharingSecurityBlockedError(event)
	case errors.Is(err, errGuestInviteRequiresApproval):
		return apiError(event, http.StatusForbidden, errorCodeGuestInviteRequiresApproval,
			"Only the workspace's owners invite guests. Ask them to add this guest.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeDatabaseRowPageFailed,
		"Couldn't share the database row.", "database_row_page_permissions", step, errorCodeDatabaseRowPageFailed, err)
}

func readDatabaseRowPagePermissions(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	query := event.Request.URL.Query()
	response, err := loadDatabaseRowPagePermissions(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("rowId")),
		query.Get("cursor"), query.Get("optionsCursor"))
	if err != nil {
		return databaseRowPagePermissionError(event, err, "read")
	}
	return event.JSON(http.StatusOK, response)
}

func mutateDatabaseRowPagePermission(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := databaseRowPagePermissionInput{}
	if err := event.BindBody(&input); err != nil {
		return databaseRowPagePermissionError(event, bodyRefusal(errDatabaseRowPagePermissionInvalid, "row share change", err), "bind")
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	now := time.Now().UTC()
	// A key's call asks its person only for a change that would be made
	// (user_key_acting_ask.go).
	if err := rehearseBeforeAsking(event, databaseRowPagePermissionChange(user.Id, rowID, input, now, true)); err != nil {
		return databaseRowPagePermissionError(event, err, "change")
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	if err := changeDatabaseRowPagePermission(event.App, user.Id, rowID, input, now); err != nil {
		return databaseRowPagePermissionError(event, err, "change")
	}
	response, err := loadDatabaseRowPagePermissions(event.App, user.Id, rowID, "", "")
	if err != nil {
		return databaseRowPagePermissionError(event, err, "read")
	}
	return event.JSON(http.StatusOK, response)
}

// databaseRowPageAccessRole reads a person's role on a row page: the higher
// of the database page's role and the row page's own shares.
func databaseRowPageAccessRole(app core.App, principalID string, authority databaseRowPageAuthority) (pageAccessRole, error) {
	page, err := authorizeDatabaseRowPageRecord(app, principalID, authority, pageAccessRoleView)
	if isPermissionDenied(err) {
		return pageAccessRoleNone, nil
	}
	if err != nil {
		return pageAccessRoleNone, err
	}
	return page.AccessRole, nil
}

// databaseRowPageDatabaseWritable reports whether the row page's reader may
// change its database's properties, as its property menu and "속성 추가" do:
// they may write the page that holds the database and the database's own
// lock (databaseLockedFor) doesn't hold it for them; another page's lock
// leaves them be (page_lock_database_structure.go). `held` is that lock
// holding it for any reader, someone a row page alone is shared with too,
// whose values then take only the options its properties have; `locked` is
// the reader who may write it but whom its lock holds, who may still change
// which comments its pages show, as Notion's.
func databaseRowPageDatabaseWritable(app core.App, principalID string, authority databaseRowPageAuthority) (writable, locked, held bool, err error) {
	if held, err = databaseRowPageDatabaseLockedFor(app, principalID, authority); err != nil {
		return false, false, false, err
	}
	_, err = authorizePage(app, principalID, authority.ContainerPageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return false, false, held, nil
	}
	if err != nil {
		return false, false, false, err
	}
	return !held, held, held, nil
}

func pageAccessRoleName(role pageAccessRole) string {
	switch role {
	case pageAccessRoleFullAccess:
		return "full_access"
	case pageAccessRoleEdit:
		return "edit"
	case pageAccessRoleComment:
		return "comment"
	}
	return "view"
}

// loadDatabaseRowPagePropertyWires reads the properties a row page shows,
// for someone who opens the row page without its database.
func loadDatabaseRowPagePropertyWires(app core.App, authority databaseRowPageAuthority) ([]databaseRowPagePropertyWire, error) {
	properties, err := loadDatabaseRowPageProperties(app, authority, authority.ContainerPageID, authority.DataSourceID)
	if err != nil {
		return nil, err
	}
	wires := make([]databaseRowPagePropertyWire, 0, len(properties))
	for _, property := range properties {
		config := json.RawMessage(property.Config)
		if !json.Valid(config) {
			config = json.RawMessage(`{}`)
		}
		wires = append(wires, databaseRowPagePropertyWire{
			ID: property.ID, Name: property.Name, Type: property.Type, Config: config, Position: property.Position,
		})
	}
	return wires, nil
}
