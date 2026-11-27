package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Restoring a conversion changes the property back to its original type and
// settings. That change starts a reverse conversion linked to the undone one,
// which reads each cell from the value it keeps through changes of type
// (database_property_conversion_stored.go): a cell still holding what the
// conversion produced gets its original value back, an edited cell converts
// back from its own value, and a cell the conversion cleared is filled again.

var errDatabasePropertyConversionNotRestorable = errors.New("database property conversion is not restorable")

// markDatabasePropertyConversionReturn finds the conversion that took a
// property from the type it is changed back to: walking its conversions from
// the newest, each converted to the type the one after it converted from, and
// each is finished and still kept. A conversion that restored an earlier one,
// by an undo or a change back, is passed over with all it undid, which left
// the type as it was. It marks the one found restored, as an undo of it
// would be, so it is neither undone nor returned to again.
func markDatabasePropertyConversionReturn(
	app core.App, propertyID, currentType, nextType string,
) (storedDatabasePropertyConversion, bool, error) {
	rows, err := app.DB().NewQuery(`
		SELECT [[id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_id]],[[property_id]],[[actor_id]],
			[[from_type]],[[from_config]],[[to_type]],[[status]],[[processed]],[[restore_of]],[[expires_at]]
		FROM {{leafen_database_property_conversions}}
		WHERE [[property_id]]={:propertyId}
		ORDER BY [[created_at]] DESC,[[id]] DESC`,
	).Bind(dbx.Params{"propertyId": propertyID}).Rows()
	if err != nil {
		return storedDatabasePropertyConversion{}, false, err
	}
	now, expect, passing := time.Now().UTC().UnixMilli(), currentType, ""
	found, returned := false, storedDatabasePropertyConversion{}
	for rows.Next() {
		row := struct {
			storedDatabasePropertyConversion
			ExpiresAt sql.NullInt64 `db:"expires_at"`
		}{}
		if err := rows.ScanStruct(&row); err != nil {
			_ = rows.Close()
			return storedDatabasePropertyConversion{}, false, err
		}
		if passing != "" {
			if row.ID == passing {
				passing = ""
			}
			continue
		}
		if row.Status != databasePropertyConversionCompleted || !row.ExpiresAt.Valid || row.ExpiresAt.Int64 <= now || row.ToType != expect {
			break
		}
		if row.FromType == nextType {
			found, returned = true, row.storedDatabasePropertyConversion
			break
		}
		if row.RestoreOf.Valid {
			passing = row.RestoreOf.String
			continue
		}
		expect = row.FromType
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil || !found {
		return storedDatabasePropertyConversion{}, false, err
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_database_property_conversions}} SET [[status]]='restored'
		WHERE [[id]]={:id} AND [[status]]='completed'`).Bind(dbx.Params{"id": returned.ID}).Execute()
	if err := requireDatabaseMutationJobUpdate(result, err); err != nil {
		return storedDatabasePropertyConversion{}, false, err
	}
	return returned, true, nil
}

func databasePropertyConversionSameJSON(left, right json.RawMessage) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	encodedA, errA := json.Marshal(a)
	encodedB, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(encodedA, encodedB)
}

// latestDatabasePropertyConversion is the property's most recent conversion
// that is still running or still restorable.
func latestDatabasePropertyConversion(app core.App, pageID, propertyID string) (storedDatabasePropertyConversion, int64, int64, error) {
	row := struct {
		storedDatabasePropertyConversion
		CreatedAt int64         `db:"created_at"`
		ExpiresAt sql.NullInt64 `db:"expires_at"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_id]],[[property_id]],[[actor_id]],
			[[from_type]],[[from_config]],[[to_type]],[[status]],[[processed]],[[restore_of]],[[created_at]],[[expires_at]]
		FROM {{leafen_database_property_conversions}}
		WHERE [[page_id]]={:pageId} AND [[property_id]]={:propertyId}
			AND ([[status]]='pending' OR ([[status]]='completed' AND [[expires_at]]>{:now}))
		ORDER BY [[created_at]] DESC,[[id]] DESC LIMIT 1`,
	).Bind(dbx.Params{"pageId": pageID, "propertyId": propertyID, "now": time.Now().UTC().UnixMilli()}).One(&row)
	return row.storedDatabasePropertyConversion, row.CreatedAt, row.ExpiresAt.Int64, err
}

func registerDatabasePropertyConversionRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.GET(apiRoot+databasePropertyConversionReadPath, readDatabasePropertyConversion)
	event.Router.POST(apiRoot+databasePropertyConversionRestorePath, func(event *core.RequestEvent) error {
		return restoreDatabasePropertyConversion(event, ygo)
	}).Bind(apis.BodyLimit(databasePropertyConversionMaxBodyBytes))
}

func databasePropertyConversionResponseFor(app core.App, pageID, propertyID string) (databasePropertyConversionResponse, error) {
	conversion, created, expires, err := latestDatabasePropertyConversion(app, pageID, propertyID)
	if errors.Is(err, sql.ErrNoRows) {
		return databasePropertyConversionResponse{}, nil
	}
	if err != nil {
		return databasePropertyConversionResponse{}, err
	}
	wire := &databasePropertyConversionWire{
		ID: conversion.ID, PropertyID: conversion.PropertyID, FromType: conversion.FromType, ToType: conversion.ToType,
		Status: conversion.Status, Processed: conversion.Processed, CreatedAt: created,
	}
	if conversion.Status == databasePropertyConversionCompleted {
		wire.ExpiresAt = &expires
	}
	return databasePropertyConversionResponse{Conversion: wire}, nil
}

func readDatabasePropertyConversion(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	propertyID := strings.TrimSpace(event.Request.PathValue("propertyId"))
	if _, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead); err != nil || propertyID == "" {
		return apiError(event, http.StatusNotFound, errorCodeDatabasePropertyConversionNotFound, "Property not found.")
	}
	response, err := databasePropertyConversionResponseFor(event.App, pageID, propertyID)
	if err != nil {
		return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodeDatabasePropertyConversionInvalid,
			"Couldn't read the property type conversion record.", "database_property_conversion", "read", errorCodeDatabasePropertyConversionInvalid, err)
	}
	return event.JSON(http.StatusOK, response)
}

func restoreDatabasePropertyConversion(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	propertyID := strings.TrimSpace(event.Request.PathValue("propertyId"))
	input := struct {
		ConversionID string `json:"conversionId"`
	}{}
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.ConversionID) == "" || propertyID == "" {
		return apiError(event, http.StatusBadRequest, errorCodeDatabasePropertyConversionInvalid, "Check the conversion to undo.")
	}
	conversion, _, _, err := latestDatabasePropertyConversion(event.App, pageID, propertyID)
	switch {
	case errors.Is(err, sql.ErrNoRows) || err == nil && conversion.ID != strings.TrimSpace(input.ConversionID):
		return apiError(event, http.StatusNotFound, errorCodeDatabasePropertyConversionNotFound, "There's no conversion to undo.")
	case err != nil:
		return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodeDatabasePropertyConversionInvalid,
			"Couldn't read the property type conversion record.", "database_property_conversion", "restore", errorCodeDatabasePropertyConversionInvalid, err)
	case conversion.Status == databasePropertyConversionPending:
		return apiError(event, http.StatusConflict, errorCodeDatabasePropertyConversionPending, "You can undo the conversion after it finishes.")
	}
	current := struct {
		Type     string `db:"type"`
		Name     string `db:"name"`
		Position string `db:"position"`
	}{}
	if err := event.App.DB().NewQuery(`SELECT [[type]],[[name]],[[position]] FROM {{leafen_database_properties}}
		WHERE [[id]]={:id} AND [[page_id]]={:pageId}`).Bind(dbx.Params{"id": propertyID, "pageId": pageID}).One(&current); err != nil ||
		current.Type != conversion.ToType {
		return apiError(event, http.StatusNotFound, errorCodeDatabasePropertyConversionNotFound, "There's no conversion to undo.")
	}
	_, err = ygo.applyContentGraphMutationContext(event.Request.Context(), user.Id, pageID, graphMutationInput{
		MutationID: "property-conversion-restore:" + conversion.ID,
		Operations: []graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: propertyID, DataSourceID: conversion.DataSourceID, Type: conversion.FromType,
			Name: current.Name, Position: current.Position, Config: json.RawMessage(conversion.FromConfig),
		}}},
	}, contentGraphMutationHooks{
		EnforcePageLock: true,
		BeforeApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
			result, err := txApp.DB().NewQuery(`UPDATE {{leafen_database_property_conversions}} SET [[status]]='restored'
				WHERE [[id]]={:id} AND [[status]]='completed'`).Bind(dbx.Params{"id": conversion.ID}).Execute()
			if err := requireDatabaseMutationJobUpdate(result, err); err != nil {
				return errors.Join(errDatabasePropertyConversionNotRestorable, err)
			}
			return nil
		},
	})
	if errors.Is(err, errDatabasePropertyConversionNotRestorable) {
		return apiError(event, http.StatusConflict, errorCodeDatabasePropertyConversionPending, "The conversion state changed. Try again.")
	}
	if err != nil {
		return writeGraphError(event, err, "property-conversion-restore", errorCodeGraphMutationFailed, "Couldn't revert the property type.")
	}
	response, err := databasePropertyConversionResponseFor(event.App, pageID, propertyID)
	if err != nil {
		return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodeDatabasePropertyConversionInvalid,
			"Couldn't read the property type conversion record.", "database_property_conversion", "restore-read", errorCodeDatabasePropertyConversionInvalid, err)
	}
	return event.JSON(http.StatusOK, response)
}
