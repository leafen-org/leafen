package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/reearth/ygo/crdt"
)

// Deleting either property of the dependency pair while dependencies are on
// deletes both and turns dependencies off, as Notion's does (seen
// 2026-11-08: 선행 작업 deleted, 선행 작업 and 후속 작업 both went to 삭제한
// 속성 and 종속성 offered "종속성 켜기" again). The pair stays bound, as
// dependencies turned off keeping their properties are, so turning them on
// again makes a new pair (database_dependency_pair.go). Restoring either
// brings both back with their links, as Notion's restored 선행 작업 did.

// databaseDependencyPartnerOn names the other property of the dependency pair
// a property belongs to while dependencies are on, as the timeline reads them
// (projectTimelineDependencies): as the settings say, else while bound; ""
// for none.
func databaseDependencyPartnerOn(app core.App, dataSourceID, propertyID string) (string, error) {
	partner := ""
	err := app.DB().NewQuery(`
		SELECT CASE WHEN [[binding]].[[predecessor_property_id]] = {:propertyId}
			THEN [[binding]].[[successor_property_id]] ELSE [[binding]].[[predecessor_property_id]] END
		FROM {{leafen_database_dependency_bindings}} AS [[binding]]
		LEFT JOIN {{leafen_database_task_features}} AS [[features]]
			ON [[features]].[[data_source_id]] = [[binding]].[[data_source_id]]
		WHERE [[binding]].[[data_source_id]] = {:sourceId}
			AND {:propertyId} IN ([[binding]].[[predecessor_property_id]], [[binding]].[[successor_property_id]])
			AND (json_type([[features]].[[dependencies_json]], '$.enabled') IS NULL
				OR COALESCE(json_extract([[features]].[[dependencies_json]], '$.enabled'), 0) = 1)
		LIMIT 1
	`).Bind(dbx.Params{"sourceId": dataSourceID, "propertyId": propertyID}).Row(&partner)
	if errors.Is(err, sql.ErrNoRows) || partner == propertyID {
		return "", nil
	}
	return partner, err
}

// turnDatabaseDependenciesOffKeepingPair turns a source's dependencies off
// with their pair kept, as their settings' "종속성 끄기 › 속성 유지" does.
func turnDatabaseDependenciesOffKeepingPair(app core.App, target databasePropertyTrashTarget, now time.Time) error {
	stamp := now.UTC().Format(time.RFC3339Nano)
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_task_features}}
			([[data_source_id]], [[owner]], [[workspace_id]], [[page_id]], [[authority_revision]],
			 [[dependencies_json]], [[created_at]], [[updated_at]])
		VALUES ({:sourceId}, {:owner}, {:workspaceId}, {:pageId}, 1,
			json_object('enabled', json('false'), 'propertyDisposition', 'keep'), {:now}, {:now})
		ON CONFLICT ([[data_source_id]]) DO UPDATE SET
			[[authority_revision]] = [[authority_revision]] + 1,
			[[dependencies_json]] = CASE WHEN json_type([[dependencies_json]]) = 'object'
				THEN json_set([[dependencies_json]], '$.enabled', json('false'), '$.propertyDisposition', 'keep')
				ELSE json_object('enabled', json('false'), 'propertyDisposition', 'keep') END,
			[[updated_at]] = excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"sourceId": target.DataSourceID, "owner": target.Owner, "workspaceId": target.WorkspaceID,
		"pageId": target.PageID, "now": stamp,
	}).Execute()
	return err
}

// databasePropertyPairDeletedWith names the other relation of a pair — in
// its own database or another — when it was deleted at the same moment as
// the property and is still in Deleted properties; found is false otherwise.
func databasePropertyPairDeletedWith(app core.App, target databasePropertyTrashTarget) (databaseRelationPartner, bool, error) {
	partner := databaseRelationPartner{}
	err := app.DB().NewQuery(`
		SELECT [[pair]].[[id]], [[pair]].[[data_source_id]], [[pair]].[[page_id]]
		FROM {{leafen_database_properties}} AS [[property]]
		JOIN {{leafen_database_properties}} AS [[pair]]
			ON [[pair]].[[id]] = json_extract([[property]].[[config]], '$.relatedPropertyId')
			AND [[pair]].[[owner]] = [[property]].[[owner]]
			AND [[pair]].[[data_source_id]] = json_extract([[property]].[[config]], '$.relationDatabaseId')
		JOIN {{leafen_database_property_trash}} AS [[trash]] ON [[trash]].[[property_id]] = [[pair]].[[id]]
		WHERE [[property]].[[id]] = {:propertyId} AND [[property]].[[type]] = {:relation}
			AND [[trash]].[[deleted_at]] = {:deletedAt} AND [[trash]].[[deleting]] = 0
		LIMIT 1
	`).Bind(dbx.Params{
		"propertyId": target.ID, "relation": graphValueTypeRelation, "deletedAt": target.DeletedAt,
	}).One(&partner)
	if errors.Is(err, sql.ErrNoRows) {
		return partner, false, nil
	}
	return partner, err == nil, err
}

// A two-way relation to another database asks what happens to its other
// side when it is deleted, as Notion's does (seen 2026-11-11): "관련 속성도
// 함께 삭제" (the default) takes the other side to its own Deleted
// properties at the same moment, so restoring either — or ⌘Z — brings both
// back still two-way with their values; "이 속성만 삭제" ends the two-way sync
// first, leaving the other side a one-way relation with its values, and the
// deleted one comes back two-way with it again — ⌘Z or restore, as Notion's
// ⌘Z (seen 2026-11-11) — while that side is still one-way back to it, else
// one-way. A request that doesn't say keeps the pair as it is, as before.
const (
	databasePropertyRelatedDelete = "delete"
	databasePropertyRelatedKeep   = "keep"
)

// databaseRelationPartner is the other side of a two-way relation to
// another database while the two name each other.
type databaseRelationPartner struct {
	ID           string `db:"id"`
	DataSourceID string `db:"data_source_id"`
	PageID       string `db:"page_id"`
}

// loadDatabaseRelationPartner names a relation's other side in another
// database, with the property as it is stored; found is false for a one-way
// relation, one to its own database, or another type.
func loadDatabaseRelationPartner(app core.App, target databasePropertyTrashTarget) (databaseRelationPartner, databaseRelationPairTargetProperty, map[string]any, bool, error) {
	partner := databaseRelationPartner{}
	property, config, exists, err := loadDatabaseRelationPairTargetProperty(app, target.Owner, target.ID)
	if err != nil || !exists {
		return partner, property, config, false, err
	}
	pair := databaseRelationPairOf(property.ID, property.DataSourceID, property.Type, config)
	if pair == nil || pair.TargetDataSourceID == property.DataSourceID {
		return partner, property, config, false, nil
	}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[page_id]] FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[id]] = {:pairId} AND [[data_source_id]] = {:sourceId}
			AND [[type]] = {:relation} AND json_extract([[config]], '$.relatedPropertyId') = {:propertyId}
	`).Bind(dbx.Params{
		"owner": target.Owner, "pairId": pair.PairPropertyID, "sourceId": pair.TargetDataSourceID,
		"relation": graphValueTypeRelation, "propertyId": property.ID,
	}).One(&partner)
	if errors.Is(err, sql.ErrNoRows) {
		return partner, property, config, false, nil
	}
	return partner, property, config, err == nil, err
}

// databaseRelationFormerPairField names, on a relation "이 속성만 삭제" made
// one-way, the other side it had, which taking it out of Deleted properties
// pairs it with again.
const databaseRelationFormerPairField = "relatedPropertyIdBeforeDelete"

// databaseRelationUnpairOperation is the relation, one-way now, that "이 속성만
// 삭제" keeps in Deleted properties, naming the other side it had.
func databaseRelationUnpairOperation(property databaseRelationPairTargetProperty, config map[string]any) ([]graphOperation, error) {
	next := make(map[string]any, len(config))
	for key, value := range config {
		if key != "relatedPropertyId" {
			next[key] = value
		}
	}
	if former, _ := config["relatedPropertyId"].(string); former != "" {
		next[databaseRelationFormerPairField] = former
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	return []graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
		ID: property.ID, DataSourceID: property.DataSourceID, Type: property.Type,
		Name: property.Name, Position: property.Position, Config: encoded,
	}}}, nil
}

// trashDatabaseRelationPartner takes a deleted relation's other side along
// ("delete"), deleted at the same moment, or ends their sync ("keep"), its
// other side made one-way by the pair runner as the workspace's permission
// owner. The other database's page, when another, records the change, so its
// open views read it again.
func trashDatabaseRelationPartner(
	app core.App, target databasePropertyTrashTarget, partner databaseRelationPartner,
	choice, actorID, requestID string, now time.Time,
) error {
	if choice == databasePropertyRelatedKeep {
		return admitDatabaseRelationPairWork(app, target.Owner, target.WorkspaceID, actorID, "property-trash-keep:"+requestID,
			[]databaseRelationPairWork{{Kind: databaseRelationPairDelete, Payload: databaseRelationPairPayload{
				SourceDataSourceID: target.DataSourceID, PropertyID: target.ID,
				TargetDataSourceID: partner.DataSourceID, PairPropertyID: partner.ID, Unpair: true,
			}}})
	}
	other, err := loadDatabasePropertyTrashTarget(app, partner.PageID, partner.ID)
	if errors.Is(err, errDatabaseMutationNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if other.DeletedAt == 0 {
		if err := trashDatabaseProperty(app, other, actorID, now); err != nil {
			return err
		}
	}
	if other.PageID == target.PageID {
		return nil
	}
	return recordDatabasePropertyTrashOn(app, other, "pair:"+requestID)
}

// recordDatabasePropertyTrashOn records a property's trash change on its own
// page when another page's request made it.
func recordDatabasePropertyTrashOn(app core.App, property databasePropertyTrashTarget, requestID string) error {
	return recordServerGraphChange(app, property.Owner, property.PageID,
		"database-property-trash:"+requestID+":"+security.RandomString(12),
		crdt.EncodeStateAsUpdateV1(crdt.New(), nil), graphMutationCounts{})
}

// databaseRelationRepairOperation brings a relation "이 속성만 삭제" made
// one-way back two-way, as Notion's ⌘Z does (seen 2026-11-11: the other
// side named it again): while its former other side is still a one-way
// relation back to this one's source, out of the trash, this one names it
// again and the pair runner makes it name this one. Otherwise it comes back
// one-way. A relation deleted otherwise needs no change.
func databaseRelationRepairOperation(app core.App, target databasePropertyTrashTarget) ([]graphOperation, error) {
	if target.Type != graphValueTypeRelation {
		return nil, nil
	}
	property, config, exists, err := loadDatabaseRelationPairTargetProperty(app, target.Owner, target.ID)
	if err != nil || !exists {
		return nil, err
	}
	former, _ := config[databaseRelationFormerPairField].(string)
	if former == "" {
		return nil, nil
	}
	next := make(map[string]any, len(config))
	for key, value := range config {
		if key != databaseRelationFormerPairField {
			next[key] = value
		}
	}
	related, _ := config["relationDatabaseId"].(string)
	if paired, _ := config["relatedPropertyId"].(string); paired == "" && related != "" && related != property.DataSourceID {
		var free int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_database_properties}} AS [[pair]]
			WHERE [[pair]].[[owner]] = {:owner} AND [[pair]].[[id]] = {:former}
				AND [[pair]].[[data_source_id]] = {:related} AND [[pair]].[[type]] = {:relation}
				AND json_extract([[pair]].[[config]], '$.relationDatabaseId') = {:sourceId}
				AND COALESCE(json_extract([[pair]].[[config]], '$.relatedPropertyId'), '') = ''
				AND ` + databasePropertyNotTrashedSQL("[[pair]].[[id]]") + `
				AND ` + databaseSourceNotTrashedSQL("[[pair]].[[data_source_id]]") + `
		`).Bind(dbx.Params{
			"owner": target.Owner, "former": former, "related": related,
			"relation": graphValueTypeRelation, "sourceId": property.DataSourceID,
		}).Row(&free); err != nil {
			return nil, err
		}
		if free == 1 {
			next["relatedPropertyId"] = former
		}
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	return []graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
		ID: property.ID, DataSourceID: property.DataSourceID, Type: property.Type,
		Name: property.Name, Position: property.Position, Config: encoded,
	}}}, nil
}
