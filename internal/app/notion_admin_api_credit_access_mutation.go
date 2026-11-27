package app

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Changing credit access policies and assignments, one workspace at a time,
// each change checked again against the token inside its transaction. A
// policy's or an assignment's version goes up by one with each change; an
// update or a delete names the version it read and is refused with a
// conflict when it changed since, as Notion's are.

const notionCreditAccessMutationLockNamespace = "notion-admin-credit-access-mutation"

var (
	errNotionCreditAccessNameTaken = notionAdminInvalid(errNotionCreditAccessInvalid, "body.name is already the name of another policy in this workspace.")
	errNotionCreditAccessStale     = notionAdminInvalid(errNotionCreditAccessConflict, "It changed after you read it: get it again and retry with its current version.")
	// A state the deletion needs first is a validation error, as Notion's API
	// keeps conflict_error for data collisions (a version read before).
	errNotionCreditAccessDefault = notionAdminInvalid(errNotionCreditAccessInvalid, "This policy is the workspace default, assigned to all_space_members: move that assignment to another policy first.")
)

// notionCreditAccessChange runs one change of a workspace's credit access
// under its lock, with the token checked again inside the transaction.
func notionCreditAccessChange(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID string,
	change func(txApp core.App, current notionAdminIdentity, now int64) error,
) error {
	release := acquireProductOperationLock(notionCreditAccessMutationLockNamespace, workspaceID)
	defer release()
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := authenticateNotionAdminToken(ctx, txApp, secret, time.Now().UTC())
		if err != nil {
			return err
		}
		if current.OrganizationID != expected.OrganizationID || current.TokenID != expected.TokenID ||
			current.CreatedBy != expected.CreatedBy ||
			!current.HasCapability(notionAdminCapabilityCreditLimitPolicyWrite) ||
			!current.CanAccessWorkspace(workspaceID) {
			return errNotionCreditAccessForbidden
		}
		if _, err := loadNotionCreditAccessRevision(ctx, txApp, current.OrganizationID, workspaceID); err != nil {
			return err
		}
		return change(txApp, current, time.Now().UTC().UnixMilli())
	})
}

func createNotionCreditAccessPolicy(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, name string,
	limit notionCreditAccessLimit,
) (notionCreditAccessPolicyResponse, error) {
	response := notionCreditAccessPolicyResponse{}
	err := notionCreditAccessChange(ctx, app, secret, expected, workspaceID, func(txApp core.App, current notionAdminIdentity, now int64) error {
		if err := assertNotionCreditAccessNameFree(ctx, txApp, workspaceID, "", name); err != nil {
			return err
		}
		stored := storedNotionCreditAccessPolicy{
			ID: core.GenerateDefaultRandomId(), WorkspaceID: workspaceID, Name: name, LimitKind: limit.Kind, Version: 1,
		}
		if limit.Credits != nil {
			stored.LimitCredits = sql.NullInt64{Int64: *limit.Credits, Valid: true}
		}
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_notion_credit_access_policies}}
				([[id]], [[workspace_id]], [[name]], [[limit_kind]], [[limit_credits]], [[version]], [[created_at]], [[updated_at]])
			VALUES ({:id}, {:workspaceId}, {:name}, {:kind}, {:credits}, 1, {:now}, {:now})
		`).WithContext(ctx).Bind(dbx.Params{
			"id": stored.ID, "workspaceId": workspaceID, "name": name, "kind": limit.Kind,
			"credits": stored.LimitCredits, "now": now,
		}).Execute(); err != nil {
			return err
		}
		if err := recordNotionCreditAccessChange(txApp, current, now, "credit_access_policy", "create", stored.ID); err != nil {
			return err
		}
		var err error
		response, err = projectNotionCreditAccessPolicy(stored)
		return err
	})
	return response, err
}

func updateNotionCreditAccessPolicy(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, policyID string,
	change notionCreditAccessPolicyChange,
) (notionCreditAccessPolicyResponse, error) {
	response := notionCreditAccessPolicyResponse{}
	err := notionCreditAccessChange(ctx, app, secret, expected, workspaceID, func(txApp core.App, current notionAdminIdentity, now int64) error {
		stored, err := loadStoredNotionCreditAccessPolicy(ctx, txApp, workspaceID, policyID)
		if err != nil {
			return err
		}
		if strconv.FormatInt(stored.Version, 10) != change.ExpectedVersion {
			return errNotionCreditAccessStale
		}
		next := stored
		if change.Name != nil {
			next.Name = *change.Name
		}
		if change.Limit != nil {
			next.LimitKind, next.LimitCredits = change.Limit.Kind, sql.NullInt64{}
			if change.Limit.Credits != nil {
				next.LimitCredits = sql.NullInt64{Int64: *change.Limit.Credits, Valid: true}
			}
		}
		if next == stored {
			response, err = projectNotionCreditAccessPolicy(stored)
			return err
		}
		if next.Name != stored.Name {
			if err := assertNotionCreditAccessNameFree(ctx, txApp, workspaceID, policyID, next.Name); err != nil {
				return err
			}
		}
		next.Version = stored.Version + 1
		if err := expectOneNotionCreditAccessRow(txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_credit_access_policies}}
			SET [[name]] = {:name}, [[limit_kind]] = {:kind}, [[limit_credits]] = {:credits},
				[[version]] = {:version}, [[updated_at]] = max([[created_at]], {:now})
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[version]] = {:read}
		`).WithContext(ctx).Bind(dbx.Params{
			"name": next.Name, "kind": next.LimitKind, "credits": next.LimitCredits, "version": next.Version,
			"now": now, "id": policyID, "workspaceId": workspaceID, "read": stored.Version,
		}).Execute()); err != nil {
			return err
		}
		if err := recordNotionCreditAccessChange(txApp, current, now, "credit_access_policy", "update", policyID); err != nil {
			return err
		}
		response, err = projectNotionCreditAccessPolicy(next)
		return err
	})
	return response, err
}

// deleteNotionCreditAccessPolicy deletes a policy and with it its
// assignments, but not the workspace default.
func deleteNotionCreditAccessPolicy(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, policyID, expectedVersion string,
) (notionCreditAccessDeletedResponse, error) {
	response := notionCreditAccessDeletedResponse{}
	err := notionCreditAccessChange(ctx, app, secret, expected, workspaceID, func(txApp core.App, current notionAdminIdentity, now int64) error {
		stored, err := loadStoredNotionCreditAccessPolicy(ctx, txApp, workspaceID, policyID)
		if err != nil {
			return err
		}
		if strconv.FormatInt(stored.Version, 10) != expectedVersion {
			return errNotionCreditAccessStale
		}
		var defaults int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_notion_credit_access_assignments}}
			WHERE [[policy_id]] = {:policyId} AND [[selector_type]] = 'all_space_members'
		`).WithContext(ctx).Bind(dbx.Params{"policyId": policyID}).Row(&defaults); err != nil {
			return err
		}
		if defaults != 0 {
			return errNotionCreditAccessDefault
		}
		if err := expectOneNotionCreditAccessRow(txApp.DB().NewQuery(`
			DELETE FROM {{leafen_notion_credit_access_policies}}
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[version]] = {:read}
		`).WithContext(ctx).Bind(dbx.Params{
			"id": policyID, "workspaceId": workspaceID, "read": stored.Version,
		}).Execute()); err != nil {
			return err
		}
		if err := recordNotionCreditAccessChange(txApp, current, now, "credit_access_policy", "delete", policyID); err != nil {
			return err
		}
		publicID, err := notionCreditAccessPublicID(notionCreditAccessPolicyIdentity, policyID)
		response = notionCreditAccessDeletedResponse{Object: notionCreditAccessPolicyObject, ID: publicID, Deleted: true}
		return err
	})
	return response, err
}

// assignNotionCreditAccessPolicy gives a selector the policy: a new
// assignment, or the selector's assignment moved here from another policy.
func assignNotionCreditAccessPolicy(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, policyID string,
	selector notionCreditAccessSelector,
) (notionCreditAccessAssignmentResponse, error) {
	response := notionCreditAccessAssignmentResponse{}
	err := notionCreditAccessChange(ctx, app, secret, expected, workspaceID, func(txApp core.App, current notionAdminIdentity, now int64) error {
		if _, err := loadStoredNotionCreditAccessPolicy(ctx, txApp, workspaceID, policyID); err != nil {
			return err
		}
		if err := assertNotionCreditAccessSelectorTarget(ctx, txApp, current.OrganizationID, workspaceID, selector); err != nil {
			return err
		}
		stored := storedNotionCreditAccessAssignment{}
		err := txApp.DB().NewQuery(`
			SELECT ` + notionCreditAccessAssignmentRow + ` FROM {{leafen_notion_credit_access_assignments}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[selector_type]] = {:type}
				AND ifnull([[user_id]], '') = {:userId} AND ifnull([[group_id]], '') = {:groupId}
				AND ifnull([[scim_attribute]], '') = {:attribute} AND ifnull([[scim_value]], '') = {:value}
		`).WithContext(ctx).Bind(dbx.Params{
			"workspaceId": workspaceID, "type": selector.Type, "userId": selector.UserID,
			"groupId": selector.GroupID, "attribute": selector.Attribute, "value": selector.Value,
		}).One(&stored)
		switch {
		case err == nil && stored.PolicyID == policyID:
			response, err = projectNotionCreditAccessAssignment(stored)
			return err
		case err == nil:
			stored, err = moveStoredNotionCreditAccessAssignment(ctx, txApp, current, now, stored, policyID)
		case errors.Is(err, sql.ErrNoRows):
			stored, err = insertNotionCreditAccessAssignment(ctx, txApp, current, now, workspaceID, policyID, selector)
		}
		if err != nil {
			return err
		}
		response, err = projectNotionCreditAccessAssignment(stored)
		return err
	})
	return response, err
}

func moveNotionCreditAccessAssignment(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, assignmentID, policyID string,
) (notionCreditAccessAssignmentResponse, error) {
	response := notionCreditAccessAssignmentResponse{}
	err := notionCreditAccessChange(ctx, app, secret, expected, workspaceID, func(txApp core.App, current notionAdminIdentity, now int64) error {
		stored, err := loadStoredNotionCreditAccessAssignment(ctx, txApp, workspaceID, assignmentID)
		if err != nil {
			return err
		}
		if _, err := loadStoredNotionCreditAccessPolicy(ctx, txApp, workspaceID, policyID); err != nil {
			return err
		}
		if stored.PolicyID != policyID {
			if stored, err = moveStoredNotionCreditAccessAssignment(ctx, txApp, current, now, stored, policyID); err != nil {
				return err
			}
		}
		response, err = projectNotionCreditAccessAssignment(stored)
		return err
	})
	return response, err
}

func deleteNotionCreditAccessAssignment(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, assignmentID, expectedVersion string,
) (notionCreditAccessDeletedResponse, error) {
	response := notionCreditAccessDeletedResponse{}
	err := notionCreditAccessChange(ctx, app, secret, expected, workspaceID, func(txApp core.App, current notionAdminIdentity, now int64) error {
		stored, err := loadStoredNotionCreditAccessAssignment(ctx, txApp, workspaceID, assignmentID)
		if err != nil {
			return err
		}
		if strconv.FormatInt(stored.Version, 10) != expectedVersion {
			return errNotionCreditAccessStale
		}
		if err := expectOneNotionCreditAccessRow(txApp.DB().NewQuery(`
			DELETE FROM {{leafen_notion_credit_access_assignments}}
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[version]] = {:read}
		`).WithContext(ctx).Bind(dbx.Params{
			"id": assignmentID, "workspaceId": workspaceID, "read": stored.Version,
		}).Execute()); err != nil {
			return err
		}
		if err := recordNotionCreditAccessChange(txApp, current, now, "credit_access_assignment", "delete", assignmentID); err != nil {
			return err
		}
		publicID, err := notionCreditAccessPublicID(notionCreditAccessAssignmentIdentity, assignmentID)
		response = notionCreditAccessDeletedResponse{Object: notionCreditAccessAssignmentObject, ID: publicID, Deleted: true}
		return err
	})
	return response, err
}

func insertNotionCreditAccessAssignment(
	ctx context.Context,
	app core.App,
	current notionAdminIdentity,
	now int64,
	workspaceID, policyID string,
	selector notionCreditAccessSelector,
) (storedNotionCreditAccessAssignment, error) {
	stored := storedNotionCreditAccessAssignment{
		ID: core.GenerateDefaultRandomId(), WorkspaceID: workspaceID, PolicyID: policyID,
		SelectorType: selector.Type, UserID: selector.UserID, GroupID: selector.GroupID,
		Attribute: selector.Attribute, Value: selector.Value, Version: 1,
	}
	orNull := func(value string) any {
		if value == "" {
			return nil
		}
		return value
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_credit_access_assignments}} (
			[[id]], [[workspace_id]], [[policy_id]], [[selector_type]], [[user_id]], [[group_id]],
			[[scim_attribute]], [[scim_value]], [[version]], [[created_at]], [[updated_at]]
		) VALUES ({:id}, {:workspaceId}, {:policyId}, {:type}, {:userId}, {:groupId}, {:attribute}, {:value}, 1, {:now}, {:now})
	`).WithContext(ctx).Bind(dbx.Params{
		"id": stored.ID, "workspaceId": workspaceID, "policyId": policyID, "type": selector.Type,
		"userId": orNull(selector.UserID), "groupId": orNull(selector.GroupID),
		"attribute": orNull(selector.Attribute), "value": orNull(selector.Value), "now": now,
	}).Execute(); err != nil {
		return stored, err
	}
	return stored, recordNotionCreditAccessChange(app, current, now, "credit_access_assignment", "create", stored.ID)
}

func moveStoredNotionCreditAccessAssignment(
	ctx context.Context,
	app core.App,
	current notionAdminIdentity,
	now int64,
	stored storedNotionCreditAccessAssignment,
	policyID string,
) (storedNotionCreditAccessAssignment, error) {
	if err := expectOneNotionCreditAccessRow(app.DB().NewQuery(`
		UPDATE {{leafen_notion_credit_access_assignments}}
		SET [[policy_id]] = {:policyId}, [[version]] = [[version]] + 1, [[updated_at]] = max([[created_at]], {:now})
		WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[version]] = {:read}
	`).WithContext(ctx).Bind(dbx.Params{
		"policyId": policyID, "now": now, "id": stored.ID, "workspaceId": stored.WorkspaceID, "read": stored.Version,
	}).Execute()); err != nil {
		return stored, err
	}
	stored.PolicyID, stored.Version = policyID, stored.Version+1
	return stored, recordNotionCreditAccessChange(app, current, now, "credit_access_assignment", "move", stored.ID)
}

// assertNotionCreditAccessSelectorTarget finds a user selector's member and
// a group selector's permission group in the workspace; either missing is
// not found, as Notion answers.
func assertNotionCreditAccessSelectorTarget(
	ctx context.Context,
	app core.App,
	organizationID, workspaceID string,
	selector notionCreditAccessSelector,
) error {
	var count int
	var err error
	switch selector.Type {
	case notionCreditAccessSelectorUser:
		err = app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspaces}} [[workspace]]
			JOIN {{leafen_organization_memberships}} [[organization_member]]
				ON [[organization_member]].[[organization]] = [[workspace]].[[organization]]
				AND [[organization_member]].[[user]] = {:userId} AND [[organization_member]].[[status]] = {:active}
			WHERE [[workspace]].[[id]] = {:workspaceId} AND [[workspace]].[[organization]] = {:organizationId}
				AND ([[workspace]].[[workspace_owner]] = {:userId} OR EXISTS (
					SELECT 1 FROM {{leafen_workspace_memberships}} [[workspace_member]]
					WHERE [[workspace_member]].[[workspace_id]] = [[workspace]].[[id]]
						AND [[workspace_member]].[[user_id]] = {:userId} AND [[workspace_member]].[[role]] = {:member}))
		`).WithContext(ctx).Bind(dbx.Params{
			"organizationId": organizationID, "workspaceId": workspaceID, "userId": selector.UserID,
			"active": organizationMemberStatusActive, "member": workspaceRoleMember,
		}).Row(&count)
	case notionCreditAccessSelectorGroup:
		err = app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspace_permission_groups}}
			WHERE [[id]] = {:groupId} AND [[workspace_id]] = {:workspaceId} AND [[deleted_at]] IS NULL
		`).WithContext(ctx).Bind(dbx.Params{"groupId": selector.GroupID, "workspaceId": workspaceID}).Row(&count)
	default:
		return nil
	}
	if err == nil && count != 1 {
		err = errNotionCreditAccessNotFound
	}
	return err
}

func assertNotionCreditAccessNameFree(ctx context.Context, app core.App, workspaceID, policyID, name string) error {
	var taken int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_credit_access_policies}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[name]] = {:name} AND [[id]] != {:policyId}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "name": name, "policyId": policyID,
	}).Row(&taken); err != nil {
		return err
	}
	if taken != 0 {
		return errNotionCreditAccessNameTaken
	}
	return nil
}

// expectOneNotionCreditAccessRow turns a write that changed no row — its
// row changed under it — into a conflict.
func expectOneNotionCreditAccessRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return refusedUnlessStorage(errNotionCreditAccessStale, err)
	}
	return nil
}

func recordNotionCreditAccessChange(app core.App, identity notionAdminIdentity, now int64, subjectType, action, subjectID string) error {
	if err := bumpOrganizationPolicy(app, identity.OrganizationID, now); err != nil {
		return err
	}
	return recordOrganizationAudit(
		app, identity.OrganizationID, identity.CreatedBy, "notion_admin."+subjectType+"."+action, subjectType, subjectID,
	)
}
