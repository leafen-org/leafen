package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Turning a database into a task database, and back, is one change, as
// Notion's is (seen 2026-11-02): the properties the window makes for the
// roles ("새로 만들기" — assignee, due date, status), a select chosen for the
// status becoming a status, and the task settings naming them go in one
// request and one transaction. Refused anywhere — the settings changed
// meanwhile, a property the change can't make — nothing of it stays
// (2026-11-15: made one by one before the settings, a refused conversion
// left its 담당자, 마감일 and 상태 behind). Turned back, the status a select
// became is a select again in the same change. The properties are the
// screen's, made as it makes any (named in its language, placed after the
// last); the server holds them to what the change's roles name.

// databaseTaskConversionMost is the most properties one conversion makes or
// changes: the three roles.
const databaseTaskConversionMost = 3

var databaseTaskConversionKinds = []string{graphValueTypePerson, graphValueTypeDate, graphValueTypeStatus, graphValueTypeSelect}

// databaseTaskConversionShapeReason is why a change's made properties can't
// go as they are, field first, or "": the task settings alone carry them, in
// a request of that one change.
func databaseTaskConversionShapeReason(input databaseToolbarConfigMutationInput) string {
	for index, operation := range input.Operations {
		made := operation.MadeProperties
		if len(made) == 0 {
			continue
		}
		field := fmt.Sprintf("operations[%d].madeProperties", index)
		switch {
		case operation.Kind != "configureTaskFeatures":
			return field + " goes with configureTaskFeatures alone"
		case len(input.Operations) != 1:
			return field + " goes in a request of that one change"
		case len(made) > databaseTaskConversionMost:
			return fmt.Sprintf("%s holds %d properties, over %d", field, len(made), databaseTaskConversionMost)
		case operation.TaskFeatures == nil || len(operation.TaskFeatures.Tasks) == 0:
			return field + " goes with taskFeatures.tasks, whose roles name them"
		}
		seen := map[string]bool{}
		for at, property := range made {
			switch {
			case strings.TrimSpace(property.ID) == "" || len(property.ID) > 255:
				return fmt.Sprintf("%s[%d].id names the property, 1–255 characters", field, at)
			case seen[property.ID]:
				return fmt.Sprintf("%s[%d].id %q is named twice", field, at, property.ID)
			case !slices.Contains(databaseTaskConversionKinds, property.Type):
				return fmt.Sprintf("%s[%d].type is %s, not %q", field, at, strings.Join(databaseTaskConversionKinds, ", "), property.Type)
			}
			seen[property.ID] = true
		}
	}
	return ""
}

// databaseTaskConversionOf is the one change of a request that makes
// properties with the task settings, or nil.
func databaseTaskConversionOf(input databaseToolbarConfigMutationInput) *databaseToolbarConfigMutationOperation {
	for index := range input.Operations {
		if len(input.Operations[index].MadeProperties) > 0 {
			return &input.Operations[index]
		}
	}
	return nil
}

// convertTaskDatabase makes the conversion's properties and stores its task
// settings in one content change of the database's page; the settings'
// change answers as it would alone, and a request sent again answers as its
// first did.
func (p *pocketBaseYgoPersistence) convertTaskDatabase(
	ctx context.Context, actorID, pageID, sourceID string,
	input databaseToolbarConfigMutationInput, operation databaseToolbarConfigMutationOperation,
) (databaseToolbarConfigMutationResponse, error) {
	response := databaseToolbarConfigMutationResponse{}
	operations := make([]graphOperation, 0, len(operation.MadeProperties))
	for index := range operation.MadeProperties {
		property := operation.MadeProperties[index]
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &property})
	}
	// Turned on, the settings name the properties once they are made; turned
	// off, they go first, as a task role keeps its type while tasks are on
	// (databaseTaskRoleTypeLocked).
	tasks := map[string]any{}
	_ = json.Unmarshal(operation.TaskFeatures.Tasks, &tasks)
	settingsFirst := tasks["enabled"] != true
	settle := func(txApp core.App) error {
		var err error
		response, err = applyDatabaseToolbarConfigMutation(txApp, actorID, pageID, sourceID, input)
		return err
	}
	digest := sha256.Sum256([]byte("leafen:task-database\x00" + actorID + "\x00" + input.RequestID))
	_, err := p.applyContentGraphMutationContext(ctx, actorID, pageID,
		graphMutationInput{MutationID: "task-database:" + hex.EncodeToString(digest[:16]), Operations: operations},
		contentGraphMutationHooks{
			BeforeApplyOperations: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
				if err := checkDatabaseTaskConversion(txApp, pageID, sourceID, operation); err != nil || !settingsFirst {
					return err
				}
				return settle(txApp)
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if settingsFirst {
					return nil
				}
				return settle(txApp)
			},
		})
	if err != nil || response.RequestID != "" {
		return response, err
	}
	// Made before, the content change answered from its receipt: so do the settings.
	return applyDatabaseToolbarConfigMutation(p.app, actorID, pageID, sourceID, input)
}

// checkDatabaseTaskConversion holds the properties a conversion makes to
// what its task settings name, before any is made (and after a request sent
// again was answered from its receipt): turned on, each is of
// this database and named by the role of its kind; turned off, the one is
// the stored status turned back into the select it was.
func checkDatabaseTaskConversion(app core.App, pageID, sourceID string, operation databaseToolbarConfigMutationOperation) error {
	refuse := func(format string, args ...any) error {
		return &databaseTaskFeaturesError{reason: "madeProperties: " + fmt.Sprintf(format, args...)}
	}
	tasks := map[string]any{}
	if err := json.Unmarshal(operation.TaskFeatures.Tasks, &tasks); err != nil {
		return refuse("tasks is an object")
	}
	stored := map[string]string{}
	rows := []struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[type]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND [[page_id]] = {:page}`).
		Bind(dbx.Params{"source": sourceID, "page": pageID}).All(&rows); err != nil {
		return err
	}
	for _, row := range rows {
		stored[row.ID] = row.Type
	}
	if tasks["enabled"] != true {
		var kept sql.NullString
		if err := app.DB().NewQuery(`SELECT [[tasks_json]] FROM {{leafen_database_task_features}} WHERE [[data_source_id]] = {:source}`).
			Bind(dbx.Params{"source": sourceID}).Row(&kept); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		was := map[string]any{}
		_ = json.Unmarshal([]byte(kept.String), &was)
		made := operation.MadeProperties[0]
		if len(operation.MadeProperties) != 1 || was["statusWasSelect"] != true || made.ID != was["statusPropertyId"] ||
			made.Type != graphValueTypeSelect || made.DataSourceID != sourceID || stored[made.ID] != graphValueTypeStatus {
			return refuse("turning tasks off changes only the status back into the select it was")
		}
		return nil
	}
	roles := map[string]string{"statusPropertyId": graphValueTypeStatus, "assigneePropertyId": graphValueTypePerson, "dueDatePropertyId": graphValueTypeDate}
	for _, made := range operation.MadeProperties {
		named := ""
		for role, kind := range roles {
			if id, _ := tasks[role].(string); id == made.ID && kind == made.Type {
				named = role
			}
		}
		switch kind, found := stored[made.ID]; {
		case made.DataSourceID != sourceID:
			return refuse("%q is a property of another database", made.ID)
		case named == "":
			return refuse("%q is no role's %s property", made.ID, made.Type)
		case found && !(named == "statusPropertyId" && kind == graphValueTypeSelect && tasks["statusWasSelect"] == true):
			return refuse("%q is a property already; only a select chosen for the status becomes one", made.ID)
		}
	}
	return nil
}
