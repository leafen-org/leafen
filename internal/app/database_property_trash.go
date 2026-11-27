package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Deleting a database property moves it to the database's Deleted
// properties, as in Notion: the column and its values leave the database's
// views, queries, forms and exports, and come back unchanged on restore. The
// views that group or date by it keep naming it until it is deleted for good
// (database_property_trash_views.go).
// Deleting it from Deleted properties hands it to the database deletion
// lifecycle. A deleted property stays until a person deletes it for good or
// the database goes; however many there are, none leaves on its own (Notion
// keeps them too, and asks for room only past its 1.5 MB bin).

var errDatabasePropertyInUse = errors.New(errorCodeDatabasePropertyInUse)

// A task database's status, assignee and due date stay and keep their type
// until it stops being one, as Notion refuses (seen 2026-11-05).
var errDatabasePropertyInTaskRole = graphInputError(errorCodeGraphInvalid, "This property belongs to the database's type, so it can't be deleted.")

// errDatabaseSprintRelationPair refuses turning a sprint's relation one-way or
// pointing it elsewhere: the sprints keep their tasks through it.
var errDatabaseSprintRelationPair = graphInputError(errorCodeGraphInvalid, "A sprint's relation stays two-way to its database.")

// databasePropertyNotTrashedSQL narrows a query to live properties.
func databasePropertyNotTrashedSQL(propertyExpression string) string {
	return `NOT EXISTS (SELECT 1 FROM {{leafen_database_property_trash}} AS [[property_trash_marker]]
		WHERE [[property_trash_marker]].[[property_id]] = ` + propertyExpression + `)`
}

type databasePropertyTrashTarget struct {
	ID           string `db:"id"`
	Type         string `db:"type"`
	Name         string `db:"name"`
	DataSourceID string `db:"data_source_id"`
	PageID       string `db:"page_id"`
	Owner        string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	DeletedAt    int64  `db:"deleted_at"`
	Deleting     int    `db:"deleting"`
}

func loadDatabasePropertyTrashTarget(app core.App, pageID, propertyID string) (databasePropertyTrashTarget, error) {
	target := databasePropertyTrashTarget{}
	err := app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[type]], [[property]].[[name]],
			[[property]].[[data_source_id]], [[property]].[[page_id]], [[property]].[[owner]],
			[[property]].[[workspace_id]], COALESCE([[trash]].[[deleted_at]], 0) AS [[deleted_at]],
			COALESCE([[trash]].[[deleting]], 0) AS [[deleting]]
		FROM {{leafen_database_properties}} AS [[property]]
		LEFT JOIN {{leafen_database_property_trash}} AS [[trash]] ON [[trash]].[[property_id]] = [[property]].[[id]]
		WHERE [[property]].[[id]] = {:propertyId} AND [[property]].[[page_id]] = {:pageId}
			AND ` + databaseSourceNotTrashedSQL("[[property]].[[data_source_id]]") + `
	`).Bind(dbx.Params{"propertyId": propertyID, "pageId": pageID}).One(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return target, errDatabaseMutationNotFound
	}
	return target, err
}

// trashDatabaseProperty moves a live property to Deleted properties. A title
// stays, as Notion's has no "속성 삭제". The views grouped or dated by it keep
// naming it (database_property_trash_views.go).
func trashDatabaseProperty(app core.App, target databasePropertyTrashTarget, actorID string, now time.Time) error {
	if target.DeletedAt != 0 {
		return errDatabaseMutationConflict
	}
	if target.Type == graphValueTypeTitle {
		return errDatabasePropertyInUse
	}
	if role, err := databaseTaskRoleProperty(app, target.DataSourceID, target.ID); err != nil {
		return err
	} else if role {
		return errDatabasePropertyInTaskRole
	}
	// A sprint's status stays while its sprints do, as Notion's (seen 2026-11-07: 🔒 속성 삭제).
	if kind, err := databaseSprintManagedProperty(app, target.DataSourceID, target.ID); err != nil {
		return err
	} else if kind == databaseSprintManagedStatus {
		return errDatabasePropertyInTaskRole
	}
	fenced, err := databaseMutationDeletionTargetFenced(
		app, target.Owner, target.PageID, target.DataSourceID, databaseMutationTargetProperty, target.ID,
	)
	if err != nil || fenced {
		if err == nil {
			err = errDatabaseMutationConflict
		}
		return err
	}
	// A property of the dependency pair takes the other with it and turns
	// dependencies off (database_property_trash_pairs.go).
	partner, err := databaseDependencyPartnerOn(app, target.DataSourceID, target.ID)
	if err != nil {
		return err
	}
	for _, propertyID := range []string{target.ID, partner} {
		if propertyID == "" {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_property_trash}}
				([[property_id]], [[owner]], [[workspace_id]], [[deleted_by]], [[deleted_at]], [[deleting]])
			VALUES ({:propertyId}, {:owner}, {:workspaceId}, {:actorId}, {:now}, 0)
			ON CONFLICT ([[property_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"propertyId": propertyID, "owner": target.Owner, "workspaceId": target.WorkspaceID,
			"actorId": actorID, "now": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
	}
	if partner != "" {
		return turnDatabaseDependenciesOffKeepingPair(app, target, now)
	}
	return nil
}

// restoreDatabaseProperty brings a deleted property back, with the other of
// its relation pair when the two were deleted together.
func restoreDatabaseProperty(app core.App, target databasePropertyTrashTarget) error {
	if target.DeletedAt == 0 || target.Deleting != 0 {
		return errDatabaseMutationConflict
	}
	partner, paired, err := databasePropertyPairDeletedWith(app, target)
	if err != nil {
		return err
	}
	restored := []databaseRelationPartner{{ID: target.ID, DataSourceID: target.DataSourceID, PageID: target.PageID}}
	if paired {
		restored = append(restored, partner)
	}
	for _, property := range restored {
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_database_property_trash}} WHERE [[property_id]] = {:propertyId} AND [[deleting]] = 0
		`).Bind(dbx.Params{"propertyId": property.ID}).Execute(); err != nil {
			return err
		}
		if err := distinguishDatabaseSourcePropertyNames(app, property.DataSourceID, property.ID); err != nil {
			return err
		}
	}
	// The other database's page records its side coming back.
	if paired && partner.PageID != target.PageID {
		return recordDatabasePropertyTrashOn(app, databasePropertyTrashTarget{Owner: target.Owner, PageID: partner.PageID},
			"restore:"+target.ID)
	}
	return nil
}

// deleteDatabasePropertyPermanently hands a deleted property to the database
// deletion lifecycle; it stays hidden until the lifecycle removes it.
func deleteDatabasePropertyPermanently(app core.App, pageID, propertyID, actorID string) error {
	requestID := databasePropertyDeletionRequestID(propertyID)
	if _, err := admitDatabaseMutationDeletionInTransaction(app, actorID, pageID, databaseMutationInput{
		RequestID: requestID, MutationID: requestID,
		TargetKind: databaseMutationTargetProperty, TargetID: propertyID,
	}); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_property_trash}} SET [[deleting]] = 1 WHERE [[property_id]] = {:propertyId}
	`).Bind(dbx.Params{"propertyId": propertyID}).Execute(); err != nil {
		return err
	}
	notifyDatabaseMutationDeletionAfterCommit(app)
	return nil
}

// databasePropertyDeletionRequestID names a property's deletion for good,
// whose job its answer names (database_property_trash_routes.go).
func databasePropertyDeletionRequestID(propertyID string) string {
	return "property-trash-delete:" + propertyID
}

// listDatabaseDeletedProperties lists a window of a source's deleted
// properties, newest first, from the cursor on; next goes on from there, ""
// once all are listed.
func listDatabaseDeletedProperties(app core.App, pageID, dataSourceID, cursor string) ([]databaseDeletedProperty, string, error) {
	after, err := decodeTimeIDCursor(cursor)
	if err != nil {
		return nil, "", errDatabaseDeletedPropertyCursor
	}
	stored := []struct {
		ID        string `db:"id"`
		Name      string `db:"name"`
		Type      string `db:"type"`
		DeletedAt int64  `db:"deleted_at"`
		DeletedBy string `db:"deleted_by"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[name]], [[property]].[[type]],
			[[trash]].[[deleted_at]], [[trash]].[[deleted_by]]
		FROM {{leafen_database_property_trash}} AS [[trash]]
		JOIN {{leafen_database_properties}} AS [[property]] ON [[property]].[[id]] = [[trash]].[[property_id]]
		WHERE [[property]].[[page_id]] = {:pageId} AND [[property]].[[data_source_id]] = {:sourceId}
			AND [[trash]].[[deleting]] = 0
			AND ({:afterId} = '' OR [[trash]].[[deleted_at]] < {:afterAt}
				OR ([[trash]].[[deleted_at]] = {:afterAt} AND [[property]].[[id]] > {:afterId}))
		ORDER BY [[trash]].[[deleted_at]] DESC, [[property]].[[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"pageId": pageID, "sourceId": dataSourceID, "afterAt": after.At, "afterId": after.ID,
		"limit": databaseDeletedPropertiesWindow + 1,
	}).All(&stored); err != nil {
		return nil, "", err
	}
	next := ""
	if len(stored) > databaseDeletedPropertiesWindow {
		stored = stored[:databaseDeletedPropertiesWindow]
		last := stored[len(stored)-1]
		next = encodeTimeIDCursor(last.DeletedAt, last.ID)
	}
	items := make([]databaseDeletedProperty, 0, len(stored))
	for _, property := range stored {
		items = append(items, databaseDeletedProperty{
			ID: property.ID, DataSourceID: dataSourceID, Name: property.Name, Type: property.Type,
			DeletedAt: databaseRowTrashTime(property.DeletedAt), DeletedBy: property.DeletedBy,
		})
	}
	return items, next, nil
}

var errDatabaseDeletedPropertyCursor = errListWindowCursor

// enforceDatabasePropertyTrashBoundary keeps a deleted property as it was
// deleted until it is restored.
func enforceDatabasePropertyTrashBoundary(app core.App, operations []graphOperation) error {
	propertyIDs := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil {
			propertyIDs[operation.Property.ID] = struct{}{}
		}
	}
	if len(propertyIDs) == 0 {
		return nil
	}
	encoded, err := databaseMutationIDJSON(propertyIDs)
	if err != nil {
		return err
	}
	var trashed int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_property_trash}}
		WHERE [[property_id]] IN (SELECT [[value]] FROM json_each({:propertyIds}))
	`).Bind(dbx.Params{"propertyIds": encoded}).Row(&trashed); err != nil {
		return err
	}
	if trashed != 0 {
		return graphPreconditionError(errorCodeDatabasePropertyInUse, "A deleted property can be changed after it's restored.")
	}
	return nil
}

// importDatabasePropertyTrashState keeps an imported property in Deleted
// properties as the archive had it; the importer becomes who deleted it.
func importDatabasePropertyTrashState(app core.App, propertyID, actorID, deletedAt string) error {
	deleted, err := time.Parse(time.RFC3339Nano, deletedAt)
	if err != nil {
		return workspaceImportInputError()
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_database_property_trash}}
			([[property_id]], [[owner]], [[workspace_id]], [[deleted_by]], [[deleted_at]], [[deleting]])
		SELECT [[id]], [[owner]], [[workspace_id]], {:actorId}, {:deletedAt}, 0
		FROM {{leafen_database_properties}} WHERE [[id]] = {:propertyId}
		ON CONFLICT ([[property_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"propertyId": propertyID, "actorId": actorID, "deletedAt": deleted.UnixMilli(),
	}).Execute()
	return err
}

// copyDatabasePropertyTrashState keeps a copied property in Deleted
// properties when its source property is there.
func copyDatabasePropertyTrashState(app core.App, jobID, actorID string, operations []graphOperation) error {
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertProperty || operation.Property == nil {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_property_trash}}
				([[property_id]], [[owner]], [[workspace_id]], [[deleted_by]], [[deleted_at]], [[deleting]])
			SELECT [[target]].[[id]], [[target]].[[owner]], [[target]].[[workspace_id]], {:actorId}, [[trash]].[[deleted_at]], 0
			FROM {{leafen_page_duplication_identity_map}} AS [[mapping]]
			JOIN {{leafen_database_property_trash}} AS [[trash]] ON [[trash]].[[property_id]] = [[mapping]].[[source_id]]
			JOIN {{leafen_database_properties}} AS [[target]] ON [[target]].[[id]] = [[mapping]].[[target_id]]
			WHERE [[mapping]].[[job_id]] = {:jobId} AND [[mapping]].[[kind]] = 'property'
				AND [[mapping]].[[target_id]] = {:propertyId} AND [[trash]].[[deleting]] = 0
			ON CONFLICT ([[property_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"jobId": jobID, "actorId": actorID, "propertyId": operation.Property.ID,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func databaseTaskRoleProperty(app core.App, dataSourceID, propertyID string) (bool, error) {
	roles, err := loadDatabaseTaskRoles(app, dataSourceID)
	if err != nil || !roles.Enabled {
		return false, err
	}
	return propertyID == roles.StatusPropertyID || propertyID == roles.AssigneePropertyID || propertyID == roles.DueDatePropertyID, nil
}

// databaseTaskRoleTypeLocked reports whether a task database's property
// keeps its type, as Notion's roles do. The one change a role takes is the
// one turning into a task database finishes: the select chosen as its status
// made a status, which the roles name with statusWasSelect before it is
// (useTaskDatabaseConversion, and an agent the same way).
func databaseTaskRoleTypeLocked(app core.App, dataSourceID, propertyID, from, to string) (bool, error) {
	roles, err := loadDatabaseTaskRoles(app, dataSourceID)
	if err != nil || !roles.Enabled {
		return false, err
	}
	if propertyID == roles.StatusPropertyID && roles.StatusWasSelect &&
		from == graphValueTypeSelect && to == graphValueTypeStatus {
		return false, nil
	}
	return propertyID == roles.StatusPropertyID || propertyID == roles.AssigneePropertyID || propertyID == roles.DueDatePropertyID, nil
}

const (
	databaseSprintManagedTaskSprint  = "taskSprint"
	databaseSprintManagedSprintTasks = "sprintTasks"
	databaseSprintManagedStatus      = "sprintStatus"
)

// databaseSprintManagedProperty names what a property is to a task database's
// sprints while they are on: a task's Sprint, a sprint's Tasks, or a sprint's
// status ("" for none). As Notion's (seen 2026-11-07), these keep their type
// and their two-way relation, and the status stays.
func databaseSprintManagedProperty(app core.App, dataSourceID, propertyID string) (string, error) {
	kind := ""
	err := app.DB().NewQuery(`SELECT CASE
			WHEN [[data_source_id]] = {:source} AND [[task_sprint_property_id]] = {:property} THEN 'taskSprint'
			WHEN [[sprints_data_source_id]] = {:source} AND [[sprint_tasks_property_id]] = {:property} THEN 'sprintTasks'
			WHEN [[sprints_data_source_id]] = {:source} AND [[sprint_status_property_id]] = {:property} THEN 'sprintStatus'
			ELSE '' END
		FROM {{leafen_database_sprints}}
		WHERE ([[data_source_id]] = {:source} OR [[sprints_data_source_id]] = {:source})
			AND {:property} IN ([[task_sprint_property_id]], [[sprint_tasks_property_id]], [[sprint_status_property_id]])
		LIMIT 1`).Bind(dbx.Params{"source": dataSourceID, "property": propertyID}).Row(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return kind, err
}

// databaseSprintManagedProperties names, by property, what each property of
// the data sources is to their sprints while they are on, as
// databaseSprintManagedProperty does for one.
func databaseSprintManagedProperties(app core.App, dataSourceIDs []string) (map[string]string, error) {
	managed := map[string]string{}
	if len(dataSourceIDs) == 0 {
		return managed, nil
	}
	encoded, err := json.Marshal(dataSourceIDs)
	if err != nil {
		return nil, err
	}
	sprints := []struct {
		TaskSprint  string `db:"task_sprint_property_id"`
		SprintTasks string `db:"sprint_tasks_property_id"`
		Status      string `db:"sprint_status_property_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[task_sprint_property_id]], [[sprint_tasks_property_id]], [[sprint_status_property_id]]
		FROM {{leafen_database_sprints}}
		WHERE [[data_source_id]] IN (SELECT value FROM json_each({:sources}))
			OR [[sprints_data_source_id]] IN (SELECT value FROM json_each({:sources}))`).
		Bind(dbx.Params{"sources": string(encoded)}).All(&sprints); err != nil {
		return nil, err
	}
	for _, sprint := range sprints {
		for id, kind := range map[string]string{
			sprint.TaskSprint: databaseSprintManagedTaskSprint, sprint.SprintTasks: databaseSprintManagedSprintTasks,
			sprint.Status: databaseSprintManagedStatus,
		} {
			if id != "" {
				managed[id] = kind
			}
		}
	}
	return managed, nil
}
