package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionAdminLegalHoldUsersMutationKind uint8

const (
	notionAdminLegalHoldUsersMutationAdd notionAdminLegalHoldUsersMutationKind = iota + 1
	notionAdminLegalHoldUsersMutationRemove
)

func mutateNotionAdminLegalHoldUsers(
	ctx context.Context,
	app core.App,
	secret string,
	expected leafen.NotionAdminIdentity,
	legalHoldID string,
	userIDs []string,
	kind notionAdminLegalHoldUsersMutationKind,
) (notionAdminLegalHoldResponse, error) {
	response := notionAdminLegalHoldResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.AuthenticateNotionAdminToken(ctx, txApp, secret, time.Now().UTC())
		if err != nil {
			return err
		}
		if current.OrganizationID != expected.OrganizationID ||
			current.TokenID != expected.TokenID || current.CreatedBy != expected.CreatedBy ||
			!current.HasCapability(leafen.NotionAdminCapabilityLegalHoldWrite) {
			return errNotionAdminLegalHoldForbidden
		}
		if !current.CanAccessLegalHold(legalHoldID) {
			return errLegalHoldNotFound
		}
		if err := assertNotionAdminLegalHoldUsersMutationTarget(
			txApp, current.OrganizationID, legalHoldID,
		); err != nil {
			return err
		}
		if err := validateLegalHoldScopeTargets(
			txApp, current.OrganizationID,
			leafen.LegalHoldScopeResponse{CustodianUserIDs: userIDs},
		); err != nil {
			return err
		}
		changed, auditAction, err := applyNotionAdminLegalHoldUsersMutation(
			txApp, current.OrganizationID, legalHoldID, userIDs, kind,
		)
		if err != nil {
			return err
		}
		if err := assertNotionAdminLegalHoldScopeBound(
			txApp, current.OrganizationID, legalHoldID,
		); err != nil {
			return err
		}
		if changed > 0 {
			now := time.Now().UTC().UnixMilli()
			if err := leafen.BumpOrganizationPolicy(txApp, current.OrganizationID, now); err != nil {
				return errors.Join(leafen.ErrLegalHoldUnavailable, err)
			}
			if err := recordLegalHoldAudit(
				txApp, current.OrganizationID, current.CreatedBy, auditAction, legalHoldID, now,
			); err != nil {
				return errors.Join(leafen.ErrLegalHoldUnavailable, err)
			}
		}
		response, err = projectNotionAdminLegalHoldAfterUsersMutation(
			ctx, txApp, current, legalHoldID,
		)
		return err
	})
	return response, err
}

func applyNotionAdminLegalHoldUsersMutation(
	app core.App,
	organizationID, legalHoldID string,
	userIDs []string,
	kind notionAdminLegalHoldUsersMutationKind,
) (int, string, error) {
	switch kind {
	case notionAdminLegalHoldUsersMutationAdd:
		changed, err := insertNotionAdminLegalHoldUsers(
			app, organizationID, legalHoldID, userIDs,
		)
		return changed, "notion_admin.legal_hold.users.add", err
	case notionAdminLegalHoldUsersMutationRemove:
		if len(userIDs) != 1 {
			return 0, "", leafen.NotionAdminInvalid(errLegalHoldInvalid, "Remove one user at a time.")
		}
		changed, err := removeNotionAdminLegalHoldUser(
			app, organizationID, legalHoldID, userIDs[0],
		)
		return changed, "notion_admin.legal_hold.users.remove", err
	default:
		return 0, "", leafen.ErrLegalHoldUnavailable
	}
}

func assertNotionAdminLegalHoldUsersMutationTarget(
	app core.App,
	organizationID, legalHoldID string,
) error {
	var status string
	err := app.DB().NewQuery(`
		SELECT [[status]] FROM {{leafen_organization_legal_holds}}
		WHERE [[organization_id]] = {:organizationId} AND [[id]] = {:legalHoldId}
	`).Bind(dbx.Params{
		"organizationId": organizationID,
		"legalHoldId":    legalHoldID,
	}).Row(&status)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		return errLegalHoldNotFound
	}
	if status != leafen.LegalHoldStatusActive {
		return leafen.NotionAdminInvalid(errLegalHoldInvalid, "The legal hold is no longer active.")
	}
	return nil
}

func insertNotionAdminLegalHoldUsers(
	app core.App,
	organizationID, legalHoldID string,
	userIDs []string,
) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	params := dbx.Params{}
	values := make([]string, 0, len(userIDs))
	for index, userID := range userIDs {
		prefix := fmt.Sprintf("user%d", index)
		params[prefix+"Hold"] = legalHoldID
		params[prefix+"Organization"] = organizationID
		params[prefix+"Kind"] = leafen.LegalHoldScopeCustodian
		params[prefix+"Target"] = userID
		values = append(values, fmt.Sprintf(
			"({:%sHold}, {:%sOrganization}, {:%sKind}, {:%sTarget})",
			prefix, prefix, prefix, prefix,
		))
	}
	result, err := app.DB().NewQuery(`
		INSERT OR IGNORE INTO {{leafen_organization_legal_hold_scopes}}
			([[hold_id]], [[organization_id]], [[kind]], [[target_id]])
		VALUES ` + strings.Join(values, ", ")).Bind(params).Execute()
	if err != nil {
		return 0, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected < 0 || affected > int64(len(userIDs)) {
		return 0, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	return int(affected), nil
}

func removeNotionAdminLegalHoldUser(
	app core.App,
	organizationID, legalHoldID, userID string,
) (int, error) {
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_organization_legal_hold_scopes}}
		WHERE [[hold_id]] = {:legalHoldId} AND [[organization_id]] = {:organizationId}
			AND [[kind]] = {:kind} AND [[target_id]] = {:userId}
			AND EXISTS (
				SELECT 1 FROM {{leafen_organization_legal_hold_scopes}} AS [[sibling]]
				WHERE [[sibling]].[[hold_id]] = {:legalHoldId}
					AND [[sibling]].[[organization_id]] = {:organizationId}
					AND ([[sibling]].[[kind]] != {:kind} OR [[sibling]].[[target_id]] != {:userId})
				LIMIT 1
			)
	`).Bind(dbx.Params{
		"organizationId": organizationID,
		"legalHoldId":    legalHoldID,
		"kind":           leafen.LegalHoldScopeCustodian,
		"userId":         userID,
	}).Execute()
	if err != nil {
		return 0, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected < 0 || affected > 1 {
		return 0, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if affected == 1 {
		return 1, nil
	}
	var targetCount int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_organization_legal_hold_scopes}}
		WHERE [[hold_id]] = {:legalHoldId} AND [[organization_id]] = {:organizationId}
			AND [[kind]] = {:kind} AND [[target_id]] = {:userId}
	`).Bind(dbx.Params{
		"organizationId": organizationID,
		"legalHoldId":    legalHoldID,
		"kind":           leafen.LegalHoldScopeCustodian,
		"userId":         userID,
	}).Row(&targetCount); err != nil {
		return 0, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if targetCount == 0 {
		return 0, nil
	}
	if targetCount == 1 {
		return 0, leafen.NotionAdminInvalid(errLegalHoldInvalid, "A legal hold keeps at least one user; release it instead.")
	}
	return 0, leafen.ErrLegalHoldUnavailable
}

func assertNotionAdminLegalHoldScopeBound(
	app core.App,
	organizationID, legalHoldID string,
) error {
	var count int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_organization_legal_hold_scopes}}
		WHERE [[organization_id]] = {:organizationId} AND [[hold_id]] = {:legalHoldId}
	`).Bind(dbx.Params{
		"organizationId": organizationID,
		"legalHoldId":    legalHoldID,
	}).Row(&count); err != nil {
		return errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if count < 1 {
		return leafen.NotionAdminInvalid(errLegalHoldInvalid, "The legal hold has nothing in its scope.")
	}
	return nil
}

func projectNotionAdminLegalHoldAfterUsersMutation(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
	legalHoldID string,
) (notionAdminLegalHoldResponse, error) {
	rows, err := loadStoredLegalHolds(
		app, identity.OrganizationID, "", 0, legalHoldID,
	)
	if err != nil {
		return notionAdminLegalHoldResponse{}, err
	}
	if len(rows) != 1 {
		return notionAdminLegalHoldResponse{}, errLegalHoldNotFound
	}
	items, err := hydrateLegalHolds(app, rows)
	if err != nil {
		return notionAdminLegalHoldResponse{}, err
	}
	workspaceIDs := []string{}
	_, adminCreated, err := notionAdminLegalHoldMetadataFromStored(rows[0])
	if err != nil {
		return notionAdminLegalHoldResponse{}, err
	}
	if adminCreated {
		workspaceIDs, err = loadNotionAdminWorkspaceIDs(ctx, app, identity)
		if err != nil {
			return notionAdminLegalHoldResponse{}, err
		}
	}
	return notionAdminLegalHoldProjection(items[0], rows[0], identity, workspaceIDs)
}

func notionAdminLegalHoldUsersMutationError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, leafen.ErrNotionAdminTokenUnauthorized):
		return leafen.NotionAdminAPIError(
			event, http.StatusUnauthorized, leafen.NotionAdminErrorUnauthorized,
			leafen.NotionAdminUnauthorizedMessage, err,
		)
	case errors.Is(err, errNotionAdminLegalHoldForbidden):
		return leafen.NotionAdminAPIError(
			event, http.StatusForbidden, leafen.NotionAdminErrorForbidden,
			leafen.NotionAdminRefusalReason(err, leafen.NotionAdminForbiddenMessage), err,
		)
	case errors.Is(err, errLegalHoldTargetNotFound), errors.Is(err, errLegalHoldNotFound):
		return leafen.NotionAdminAPIError(
			event, http.StatusNotFound, leafen.NotionAdminErrorNotFound,
			leafen.NotionAdminNotFoundMessage, err,
		)
	case errors.Is(err, errLegalHoldInvalid):
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			leafen.NotionAdminValidationReason(err), err,
		)
	default:
		return leafen.NotionAdminAPIError(
			event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
			leafen.NotionAdminInternalErrorMessage, err,
		)
	}
}
