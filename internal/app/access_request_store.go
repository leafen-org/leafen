package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// upsertAccessRequest keeps one waiting request for the same thing. Asking
// again widens the requested role.
func upsertAccessRequest(app core.App, request storedAccessRequest) (storedAccessRequest, bool, error) {
	fresh := false
	err := app.RunInTransaction(func(txApp core.App) error {
		existing := storedAccessRequest{}
		err := txApp.DB().NewQuery(`SELECT ` + accessRequestColumns + ` FROM {{leafen_access_requests}}
			WHERE [[workspace_id]]={:workspaceId} AND [[kind]]={:kind} AND [[page_id]]={:pageId}
				AND [[requester_id]]={:requesterId} AND [[principal_id]]={:principalId} AND [[status]]='pending'`).Bind(dbx.Params{
			"workspaceId": request.WorkspaceID, "kind": request.Kind, "pageId": request.PageID,
			"requesterId": request.RequesterID, "principalId": request.PrincipalID,
		}).One(&existing)
		now := time.Now().UTC().UnixMilli()
		if err == nil {
			if accessRequestRoleFromName(request.Role) > accessRequestRoleFromName(existing.Role) {
				existing.Role = request.Role
			}
			existing.UpdatedAt = now
			// Asking again with a message says it in place of the earlier one.
			if request.Message != "" {
				existing.Message = request.Message
			}
			if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_access_requests}} SET [[role]]={:role},[[message]]={:message},[[updated_at]]={:now}
				WHERE [[id]]={:id}`).Bind(dbx.Params{"role": existing.Role, "message": existing.Message, "now": now, "id": existing.ID}).Execute(); err != nil {
				return err
			}
			request = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		request.ID = security.RandomString(15)
		request.Status = accessRequestPending
		request.CreatedAt, request.UpdatedAt = now, now
		fresh = true
		_, err = txApp.DB().Insert(accessRequestsTable, dbx.Params{
			"id": request.ID, "workspace_id": request.WorkspaceID, "kind": request.Kind,
			"requester_id": request.RequesterID, "page_id": request.PageID, "principal_type": request.PrincipalType,
			"principal_id": request.PrincipalID, "role": request.Role, "status": request.Status,
			"created_at": now, "updated_at": now, "message": request.Message,
		}).Execute()
		return err
	})
	return request, fresh, err
}

// listAccessRequests lists a window of a workspace's waiting requests the
// person may decide or made, newest first, from the cursor on. It looks at a
// bounded batch each time; the next window goes on from the last one looked at.
func listAccessRequests(app core.App, principalID, workspaceID, cursor string) (accessRequestListResponse, error) {
	after, err := decodeTimeIDCursor(cursor)
	if err != nil {
		return accessRequestListResponse{}, refuse(errAccessRequestInvalid, "cursor isn't one this list gave: leave it out to read from the start")
	}
	role, err := workspaceRoleOf(app, principalID, workspaceID)
	if err != nil {
		return accessRequestListResponse{}, err
	}
	if role == "" {
		return ownPendingAccessRequests(app, principalID, workspaceID, after)
	}
	rows := []storedAccessRequest{}
	scan := accessRequestListWindow * 4
	if err := app.DB().NewQuery(`SELECT ` + accessRequestColumns + ` FROM {{leafen_access_requests}} [[request]]
		WHERE [[workspace_id]]={:workspaceId} AND [[status]]='pending' AND ` + accessRequestListedSQL + `
			AND ({:afterId}='' OR [[created_at]]<{:afterAt} OR ([[created_at]]={:afterAt} AND [[id]]<{:afterId}))
		ORDER BY [[created_at]] DESC, [[id]] DESC LIMIT {:limit}`).Bind(dbx.Params{
		"workspaceId": workspaceID, "limit": scan + 1, "afterAt": after.At, "afterId": after.ID,
	}).All(&rows); err != nil {
		return accessRequestListResponse{}, err
	}
	more := len(rows) > scan
	if more {
		rows = rows[:scan]
	}
	response := accessRequestListResponse{Requests: []accessRequestResponse{}}
	for index, row := range rows {
		visible, err := accessRequestVisible(app, principalID, role, row)
		if err != nil {
			return accessRequestListResponse{}, err
		}
		if !visible {
			continue
		}
		item, err := accessRequestResponseFor(app, row, principalID)
		if err != nil {
			return accessRequestListResponse{}, err
		}
		response.Requests = append(response.Requests, item)
		if len(response.Requests) == accessRequestListWindow && (index+1 < len(rows) || more) {
			response.NextCursor = encodeTimeIDCursor(row.CreatedAt, row.ID)
			return response, nil
		}
	}
	if more {
		response.NextCursor = encodeTimeIDCursor(rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID)
	}
	return response, nil
}

// accessRequestListedSQL keeps, of the requests bound as [[request]], those
// Settings › 요청 lists: a guest's request for a page reaches those who
// decide it in their inbox alone, as Notion's (seen 2026-11-09), so the list
// holds members' requests.
const accessRequestListedSQL = `([[request]].[[kind]]!='page'
	OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[requester_member]]
		WHERE [[requester_member]].[[workspace_id]]=[[request]].[[workspace_id]] AND [[requester_member]].[[user_id]]=[[request]].[[requester_id]])
	OR EXISTS (SELECT 1 FROM {{leafen_workspaces}} [[requester_workspace]]
		WHERE [[requester_workspace]].[[id]]=[[request]].[[workspace_id]] AND [[requester_workspace]].[[workspace_owner]]=[[request]].[[requester_id]]))`

// accessRequestVisible reports whether the person may see a request: their
// own, a page's to those with full access to the page, and any other to the
// workspace's owner.
func accessRequestVisible(app core.App, principalID, workspaceRole string, row storedAccessRequest) (bool, error) {
	switch {
	case row.RequesterID == principalID:
		return true, nil
	case row.Kind == accessRequestKindPage:
		_, err := authorizePageAccessRole(app, principalID, row.PageID, pageAccessRoleFullAccess)
		if err != nil && !isPermissionDenied(err) {
			return false, err
		}
		return err == nil, nil
	}
	return workspaceRole == workspaceRoleOwner, nil
}

// readAccessRequests reads the named requests of a workspace as they stand,
// decided ones too, for the inbox's rows about them: each one the person may
// see, newest first. Ones they may not see or that don't exist are left out.
func readAccessRequests(app core.App, principalID, workspaceID string, rawIDs []string) (accessRequestListResponse, error) {
	ids := make([]string, 0, len(rawIDs))
	seen := map[string]struct{}{}
	for _, id := range rawIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if len(id) != 15 {
			return accessRequestListResponse{}, refuse(errAccessRequestInvalid, "ids holds %q, which isn't a request id (15 characters)", id)
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) > accessRequestListWindow {
		return accessRequestListResponse{}, refuse(errAccessRequestInvalid, "ids names %d requests, over %d", len(ids), accessRequestListWindow)
	}
	response := accessRequestListResponse{Requests: []accessRequestResponse{}}
	if len(ids) == 0 {
		return response, nil
	}
	role, err := workspaceRoleOf(app, principalID, workspaceID)
	if err != nil {
		return response, err
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return response, err
	}
	rows := []storedAccessRequest{}
	if err := app.DB().NewQuery(`SELECT ` + accessRequestColumns + ` FROM {{leafen_access_requests}}
		WHERE [[workspace_id]]={:workspaceId} AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[created_at]] DESC, [[id]] DESC`).Bind(dbx.Params{
		"workspaceId": workspaceID, "ids": string(encoded),
	}).All(&rows); err != nil {
		return response, err
	}
	for _, row := range rows {
		visible, err := accessRequestVisible(app, principalID, role, row)
		if err != nil {
			return response, err
		}
		if !visible {
			continue
		}
		item, err := accessRequestResponseFor(app, row, principalID)
		if err != nil {
			return response, err
		}
		// Someone outside the workspace reads their own request without a
		// page title a revoked share would reveal (ownPendingAccessRequests).
		if role == "" {
			item.PageTitle, item.PageKind, item.ViewBlock, item.SourceTitles = "", "", false, nil
		}
		response.Requests = append(response.Requests, item)
	}
	return response, nil
}

func loadAccessRequest(app core.App, requestID string) (storedAccessRequest, error) {
	row := storedAccessRequest{}
	err := app.DB().NewQuery(`SELECT ` + accessRequestColumns + ` FROM {{leafen_access_requests}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": requestID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return row, errAccessRequestNotFound
	}
	return row, err
}

// decideAccessRequest approves or declines a waiting request. A page's is
// decided by someone with full access to it, a guest's or a member's by the
// workspace's owner; approving grants it as they would themselves.
func decideAccessRequest(ctx context.Context, app core.App, deciderID, requestID string, approve bool) (accessRequestResponse, error) {
	return decideAccessRequestAs(ctx, app, deciderID, requestID, approve, "")
}

// decideAccessRequestAs decides as decideAccessRequest does; a page's or a
// guest's request may be approved with another role than the one asked for
// (role), as Notion's inbox row offers, and "" keeps the one asked for.
func decideAccessRequestAs(ctx context.Context, app core.App, deciderID, requestID string, approve bool, role string) (accessRequestResponse, error) {
	if deciderID == "" || requestID == "" || len(requestID) != 15 {
		return accessRequestResponse{}, errAccessRequestNotFound
	}

	release, err := lockAccessRequestDecision(app, requestID)
	if err != nil {
		return accessRequestResponse{}, err
	}
	defer release()
	response := accessRequestResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		var err error
		response, err = decideStoredAccessRequest(ctx, txApp, deciderID, requestID, approve, role)
		return err
	})
	return response, err
}

// lockAccessRequestDecision holds, for deciding a page's request, the
// page's sharing lock, as a change of its sharing holds it, until release.
func lockAccessRequestDecision(app core.App, requestID string) (func(), error) {
	request, err := loadAccessRequest(app, requestID)
	if err != nil || request.PageID == "" {
		return func() {}, err
	}
	page, err := app.FindRecordById(pagesCollection, request.PageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errAccessRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return acquireProductOperationLock(publicMutationLockNamespace, page.GetString("workspace")+"\x00"+request.PageID), nil
}

func decideStoredAccessRequest(ctx context.Context, app core.App, deciderID, requestID string, approve bool, role string) (accessRequestResponse, error) {
	request, err := loadAccessRequest(app, requestID)
	if err != nil {
		return accessRequestResponse{}, err
	}
	switch request.Kind {
	case accessRequestKindPage:
		// Its page, held a while by an operation, says so (page_held_refusal.go).
		if _, err := authorizePageAccessRole(app, deciderID, request.PageID, pageAccessRoleFullAccess); err != nil {
			return accessRequestResponse{}, refusalOnPage(err, request.PageID)
		}
	default:
		if _, err := authorizeWorkspace(app, deciderID, request.WorkspaceID, permissionWorkspaceOwner); err != nil {
			return accessRequestResponse{}, err
		}
	}
	if request.Status != accessRequestPending {
		return accessRequestResponse{}, errAccessRequestDecided
	}
	// The role given may differ from the one asked for; the request keeps
	// what was asked (a page's request asks for no more than editing).
	grant := request
	if approve && role != "" {
		if request.Kind != accessRequestKindPage && request.Kind != accessRequestKindGuest {
			return accessRequestResponse{}, refuse(errAccessRequestInvalid, "role goes only with a request for a page: a membership request has no role to give")
		}
		if accessRequestRoleFromName(role) == pageAccessRoleNone {
			return accessRequestResponse{}, refuse(errAccessRequestInvalid, "role is view, comment, edit or full_access, not %q", role)
		}
		grant.Role = role
	}
	if approve {
		if request.Kind == accessRequestKindMembership {
			role, err := workspaceRoleOf(app, request.RequesterID, request.WorkspaceID)
			if err != nil {
				return accessRequestResponse{}, err
			}
			if role == "" {
				allowed, err := canRequestWorkspaceMembership(app, request.RequesterID, request.WorkspaceID)
				if err == nil && !allowed {
					allowed, err = workspaceInviteLinkTakesRequests(app, request.WorkspaceID)
				}
				if err != nil {
					return accessRequestResponse{}, err
				}
				if !allowed {
					return accessRequestResponse{}, errAccessRequestNotAllowed
				}
			}
		}
		if err := grantAccessRequest(ctx, app, deciderID, grant); err != nil {
			return accessRequestResponse{}, err
		}
	}
	status := accessRequestDeclined
	if approve {
		status = accessRequestApproved
	}
	now := time.Now().UTC().UnixMilli()
	result, err := app.DB().NewQuery(`UPDATE {{leafen_access_requests}}
		SET [[status]]={:status},[[decided_by]]={:decider},[[decided_at]]={:now},[[updated_at]]={:now}
		WHERE [[id]]={:id} AND [[status]]='pending'`).Bind(dbx.Params{
		"status": status, "decider": deciderID, "now": now, "id": request.ID,
	}).Execute()
	if err != nil {
		return accessRequestResponse{}, err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return accessRequestResponse{}, err
	} else if changed == 0 {
		return accessRequestResponse{}, errAccessRequestDecided
	}
	// Decided anywhere — Settings › 요청 too — the decider's inbox row about
	// it is read, as deciding in the inbox makes it (InboxAccessRequest.tsx).
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_notifications}} SET [[read_at]]={:now},[[updated_at]]={:now}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:decider} AND [[read_at]]=0 AND [[activity_key]]={:key}`).Bind(dbx.Params{
		"now": now, "workspaceId": request.WorkspaceID, "decider": deciderID, "key": "access-request:" + request.ID,
	}).Execute(); err != nil {
		return accessRequestResponse{}, err
	}
	request.Status, request.DecidedBy, request.DecidedAt, request.UpdatedAt = status, deciderID, now, now
	return accessRequestResponseFor(app, request, deciderID)
}

func accessRequestResponseFor(app core.App, request storedAccessRequest, viewerID string) (accessRequestResponse, error) {
	response := accessRequestResponse{
		ID: request.ID, WorkspaceID: request.WorkspaceID, Kind: request.Kind, Status: request.Status,
		RequesterID: request.RequesterID, PageID: request.PageID, PrincipalType: request.PrincipalType,
		PrincipalID: request.PrincipalID, Role: request.Role, CreatedAt: request.CreatedAt,
		DecidedBy: request.DecidedBy, DecidedAt: request.DecidedAt, Mine: request.RequesterID == viewerID,
		Message: request.Message,
	}
	name := func(userID string) (string, error) {
		user, err := app.FindRecordById("users", userID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if label := strings.TrimSpace(user.GetString("name")); label != "" {
			return label, nil
		}
		return user.GetString("email"), nil
	}
	var err error
	if response.RequesterName, err = name(request.RequesterID); err != nil {
		return response, err
	}
	if request.DecidedBy != "" {
		if response.DecidedByName, err = name(request.DecidedBy); err != nil {
			return response, err
		}
	}
	if response.External, err = accessRequestExternal(app, request); err != nil {
		return response, err
	}
	switch request.PrincipalType {
	case publicMutationPrincipalUser:
		if response.PrincipalLabel, err = name(request.PrincipalID); err != nil {
			return response, err
		}
	case publicMutationPrincipalEmail:
		response.PrincipalLabel = request.PrincipalID
	}
	if request.PageID != "" {
		page, err := app.FindRecordById(pagesCollection, request.PageID)
		if err == nil {
			response.PageTitle = page.GetString("title")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return response, err
		}
		// Its kind names it while untitled where the reader's list doesn't
		// hold it (page_name_kind.go).
		kinds, err := loadPageNameKinds(app, []string{request.PageID})
		if err != nil {
			return response, err
		}
		if kind := kinds[request.PageID]; kind.Kind == pageKindDatabase {
			response.PageKind, response.ViewBlock, response.SourceTitles = kind.Kind, kind.ViewBlock, kind.SourceTitles
		}
	}
	return response, nil
}

// accessRequestExternal reports whether the person a page's or a guest's
// request would let in is outside the workspace — not one of its members —
// whom Notion's inbox row warns about, asking the reader to be sure they
// trust that outsider before sharing (seen 2026-11-09 for a guest's request).
func accessRequestExternal(app core.App, request storedAccessRequest) (bool, error) {
	switch request.Kind {
	case accessRequestKindPage:
		role, err := workspaceRoleOf(app, request.RequesterID, request.WorkspaceID)
		return role == "", err
	case accessRequestKindGuest:
		workspace, err := app.FindRecordById(workspacesCollection, request.WorkspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		member, err := sharePrincipalIsWorkspaceMember(app, workspace.GetString("organization"), request.WorkspaceID,
			request.PrincipalType, request.PrincipalID)
		return !member, err
	}
	return false, nil
}
