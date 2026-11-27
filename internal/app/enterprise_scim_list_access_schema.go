package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	scimListAccessStateTable           = "leafen_scim_list_access_state"
	scimListAccessEntriesTable         = "leafen_scim_list_access_entries"
	scimListAccessTreeTable            = "leafen_scim_list_access_tree"
	scimListAccessResourceTypeUser     = "user"
	scimListAccessResourceTypeGroup    = "group"
	scimListAccessAllFilterKey         = "all"
	scimListAccessExternalFilterPrefix = "external:"
)

func activateSCIMListResource(
	ctx context.Context,
	app core.App,
	organizationID, resourceType, filterKey, filterValue, resourceID string,
) error {
	if app.IsTransactional() {
		return activateSCIMListResourceInTransaction(
			ctx, app, organizationID, resourceType, filterKey, filterValue, resourceID,
		)
	}
	return app.RunInTransaction(func(txApp core.App) error {
		return activateSCIMListResourceInTransaction(
			ctx, txApp, organizationID, resourceType, filterKey, filterValue, resourceID,
		)
	})
}

func activateSCIMListResourceInTransaction(
	ctx context.Context,
	app core.App,
	organizationID, resourceType, filterKey, filterValue, resourceID string,
) error {
	if resourceID == "" {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	var listID int64
	if err := app.DB().NewQuery(`
		INSERT INTO {{leafen_scim_list_access_state}}
			([[organization_id]], [[resource_type]], [[filter_key]], [[filter_value]], [[total]])
		VALUES ({:organizationId}, {:resourceType}, {:filterKey}, {:filterValue}, 0)
		ON CONFLICT ([[organization_id]], [[resource_type]], [[filter_key]])
		DO UPDATE SET [[filter_value]] = {{leafen_scim_list_access_state}}.[[filter_value]]
		WHERE {{leafen_scim_list_access_state}}.[[filter_value]] = excluded.[[filter_value]]
		RETURNING [[list_id]]
	`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID, "resourceType": resourceType,
		"filterKey": filterKey, "filterValue": filterValue,
	}).Row(&listID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New(errorCodeSchemaMigrationHistoryInvalid)
		}
		return err
	}
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_scim_list_access_entries}}
			([[list_id]], [[resource_id]])
		VALUES ({:listId}, {:resourceId})
		ON CONFLICT ([[list_id]], [[resource_id]]) DO NOTHING
	`).WithContext(ctx).Bind(dbx.Params{
		"listId": listID, "resourceId": resourceID,
	}).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_scim_list_access_state}} SET [[total]] = [[total]] + 1
		WHERE [[list_id]] = {:listId}
	`).WithContext(ctx).Bind(dbx.Params{"listId": listID}).Execute(); err != nil {
		return err
	}
	return addSCIMListAccessTreePath(ctx, app, listID, resourceID)
}

func addSCIMListAccessTreePath(
	ctx context.Context,
	app core.App,
	listID int64,
	resourceID string,
) error {
	params := dbx.Params{"listId": listID}
	values := make([]string, 0, len(resourceID))
	for depth := 1; depth <= len(resourceID); depth++ {
		prefixKey, parentKey := fmt.Sprintf("prefix%d", depth), fmt.Sprintf("parent%d", depth)
		params[prefixKey], params[parentKey] = resourceID[:depth], resourceID[:depth-1]
		terminalKey := fmt.Sprintf("terminal%d", depth)
		params[terminalKey] = boolInt(depth == len(resourceID))
		values = append(values, "({:listId}, {:"+prefixKey+"}, {:"+parentKey+"}, 1, {:"+terminalKey+"})")
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_scim_list_access_tree}} (
			[[list_id]], [[prefix]], [[parent_prefix]], [[active_count]], [[terminal_count]]
		) VALUES ` + strings.Join(values, ", ") + `
		ON CONFLICT ([[list_id]], [[prefix]])
		DO UPDATE SET [[active_count]] = [[active_count]] + 1,
			[[terminal_count]] = [[terminal_count]] + excluded.[[terminal_count]]
	`).WithContext(ctx).Bind(params).Execute()
	return err
}

func deactivateSCIMListResourceInTransaction(
	ctx context.Context,
	app core.App,
	organizationID, resourceType, filterKey, filterValue, resourceID string,
) error {
	var listID int64
	err := app.DB().NewQuery(`
		SELECT [[list_id]] FROM {{leafen_scim_list_access_state}}
		WHERE [[organization_id]] = {:organizationId} AND [[resource_type]] = {:resourceType}
			AND [[filter_key]] = {:filterKey} AND [[filter_value]] = {:filterValue}
	`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID, "resourceType": resourceType,
		"filterKey": filterKey, "filterValue": filterValue,
	}).Row(&listID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_scim_list_access_entries}}
		WHERE [[list_id]] = {:listId} AND [[resource_id]] = {:resourceId}
	`).WithContext(ctx).Bind(dbx.Params{
		"listId": listID, "resourceId": resourceID,
	}).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return err
	}
	prefixes, params := scimListAccessPrefixParams(listID, resourceID)
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_scim_list_access_tree}}
		SET [[active_count]] = [[active_count]] - 1,
			[[terminal_count]] = [[terminal_count]] - CASE WHEN [[prefix]] = {:resourceId} THEN 1 ELSE 0 END
		WHERE [[list_id]] = {:listId} AND [[prefix]] IN (` + strings.Join(prefixes, ", ") + `)
	`).WithContext(ctx).Bind(params).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_scim_list_access_state}} SET [[total]] = [[total]] - 1
		WHERE [[list_id]] = {:listId};
		DELETE FROM {{leafen_scim_list_access_tree}}
		WHERE [[list_id]] = {:listId} AND [[active_count]] = 0;
		DELETE FROM {{leafen_scim_list_access_state}}
		WHERE [[list_id]] = {:listId} AND [[total]] = 0;
	`).WithContext(ctx).Bind(params).Execute(); err != nil {
		return err
	}
	return nil
}

func scimListAccessPrefixParams(
	listID int64,
	resourceID string,
) ([]string, dbx.Params) {
	params := dbx.Params{"listId": listID, "resourceId": resourceID}
	prefixes := make([]string, 0, len(resourceID))
	for depth := 1; depth <= len(resourceID); depth++ {
		key := fmt.Sprintf("prefix%d", depth)
		params[key] = resourceID[:depth]
		prefixes = append(prefixes, "{:"+key+"}")
	}
	return prefixes, params
}

func syncSCIMGroupListAccessInTransaction(
	ctx context.Context,
	app core.App,
	organizationID, groupID, priorExternalID, externalID string,
	deleted bool,
) error {
	oldKey := scimListAccessExternalFilterKey(priorExternalID)
	newKey := scimListAccessExternalFilterKey(externalID)
	if deleted {
		if err := deactivateSCIMListResourceInTransaction(
			ctx, app, organizationID, scimListAccessResourceTypeGroup,
			scimListAccessAllFilterKey, "", groupID,
		); err != nil {
			return err
		}
		return deactivateSCIMListResourceInTransaction(
			ctx, app, organizationID, scimListAccessResourceTypeGroup,
			oldKey, priorExternalID, groupID,
		)
	}
	if err := activateSCIMListResourceInTransaction(
		ctx, app, organizationID, scimListAccessResourceTypeGroup,
		scimListAccessAllFilterKey, "", groupID,
	); err != nil {
		return err
	}
	if priorExternalID != externalID {
		if err := deactivateSCIMListResourceInTransaction(
			ctx, app, organizationID, scimListAccessResourceTypeGroup,
			oldKey, priorExternalID, groupID,
		); err != nil {
			return err
		}
	}
	return activateSCIMListResourceInTransaction(
		ctx, app, organizationID, scimListAccessResourceTypeGroup,
		newKey, externalID, groupID,
	)
}

func scimListAccessExternalFilterKey(value string) string {
	digest := sha256.Sum256([]byte(value))
	return scimListAccessExternalFilterPrefix + hex.EncodeToString(digest[:])
}
