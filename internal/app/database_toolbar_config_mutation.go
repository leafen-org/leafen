package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type databaseToolbarConfigReceipt struct {
	WorkspaceID  string `db:"workspace_id"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
	RequestHash  string `db:"request_hash"`
	ResponseJSON string `db:"response_json"`
}

type databaseToolbarMutationState struct {
	// ActorID is who makes the change, recorded on template bodies.
	ActorID                   string
	ConfigurationRevision     int
	TemplateAuthorityRevision int
	// InTrash lets a template change reach a template in the trash, which
	// only a migration's run again does: it gives one Notion changed what
	// Notion has, as a row in the trash takes it (notion_import_templates.go).
	InTrash bool
}

// mutateDatabaseToolbarConfig changes a data source's settings; sub-items
// and dependencies turned on get their pair of properties once the change is
// made, and turned off with their properties removed lose it.
func mutateDatabaseToolbarConfig(ygo *pocketBaseYgoPersistence) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		return mutateDatabaseToolbarConfigWith(event, ygo)
	}
}

func mutateDatabaseToolbarConfigWith(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, ok, err := databaseToolbarConfigActor(event)
	if !ok {
		return err
	}
	pageID, dataSourceID, _ := databaseToolbarConfigIdentity(event)
	input := databaseToolbarConfigMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return writeDatabaseToolbarConfigError(event, bodyRefusal(errDatabaseToolbarConfigInvalid, "database settings change", err))
	}
	if pageID == "" || dataSourceID == "" {
		return writeDatabaseToolbarConfigError(event, databaseToolbarRefused("the page and data source IDs in the path are empty"))
	}
	if err := validateDatabaseToolbarMutationInput(input); err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	// Dependencies off with their pair kept and turned on by this change get
	// a new pair; whether they were is read before the change.
	keptDependencies, keptErr := databaseDependencyPairKept(event.App, pageID, dataSourceID)
	if keptErr != nil {
		logDurableJobDiagnostic(event.App, errorCodeGraphInvalid, newDiagnosticReference(), "",
			"database.dependencies", "kept", keptErr)
	}
	// The properties turning sub-items or dependencies on makes are named in
	// the language the request prefers, for an account following the system.
	ctx := withRequestLanguage(event.Request.Context(), event.Request)
	var response databaseToolbarConfigMutationResponse
	var mutationErr error
	if conversion := databaseTaskConversionOf(input); conversion != nil && ygo != nil {
		// A task database's properties come with its settings, in one change (database_task_conversion.go).
		response, mutationErr = ygo.convertTaskDatabase(ctx, user.Id, pageID, dataSourceID, input, *conversion)
		if mutationErr != nil && errors.As(mutationErr, new(*graphRequestError)) {
			return writeGraphChangeError(event, mutationErr, "task-database", errorCodeDatabaseToolbarConfigFailed, "Couldn't process the database settings.")
		}
	} else if conversion != nil {
		mutationErr = databaseToolbarRefused("madeProperties can't be made on this server")
	} else {
		response, mutationErr = applyDatabaseToolbarConfigMutation(event.App, user.Id, pageID, dataSourceID, input)
	}
	if mutationErr != nil {
		return writeDatabaseToolbarConfigError(event, mutationErr)
	}
	if ygo != nil {
		if err := ygo.ensureDatabaseSubitemPair(ctx, user.Id, pageID, dataSourceID, nil); err != nil {
			// The rows nest by their sub-item parent until the pair is made.
			logDurableJobDiagnostic(event.App, errorCodeGraphInvalid, newDiagnosticReference(), "",
				"database.subitems", "pair", err)
		}
		// Another pair named, the rows nest by it at once, before the job moving old sub-items looks.
		if databaseToolbarMutationConfiguresTasks(input) {
			if err := ygo.renestDatabaseSubitems(ctx, user.Id, pageID, dataSourceID); err != nil {
				logDurableJobDiagnostic(event.App, errorCodeGraphInvalid, newDiagnosticReference(), "",
					"database.subitems", "renest", err)
			}
		}
	}
	if ygo != nil {
		if err := ygo.ensureDatabaseDependencyPair(ctx, user.Id, pageID, dataSourceID, nil, keptDependencies); err != nil {
			logDurableJobDiagnostic(event.App, errorCodeGraphInvalid, newDiagnosticReference(), "",
				"database.dependencies", "pair", err)
		}
		if err := ygo.rebindDatabaseDependencyPair(ctx, user.Id, pageID, dataSourceID); err != nil {
			logDurableJobDiagnostic(event.App, errorCodeGraphInvalid, newDiagnosticReference(), "",
				"database.dependencies", "rebind", err)
		}
	}
	if err := dropDatabaseSubitemPair(event.App, user.Id, pageID, dataSourceID); err != nil {
		logDurableJobDiagnostic(event.App, errorCodeGraphInvalid, newDiagnosticReference(), "",
			"database.subitems", "drop", err)
	}
	if err := dropDatabaseDependencyPair(event.App, user.Id, pageID, dataSourceID); err != nil {
		logDurableJobDiagnostic(event.App, errorCodeGraphInvalid, newDiagnosticReference(), "",
			"database.dependencies", "drop", err)
	}
	return event.JSON(http.StatusOK, response)
}

func databaseToolbarMutationConfiguresTasks(input databaseToolbarConfigMutationInput) bool {
	for _, operation := range input.Operations {
		if operation.Kind == "configureTaskFeatures" {
			return true
		}
	}
	return false
}

// maxDatabaseTemplateIconBytes is the longest icon a template keeps, of any
// kind, as it is set here or comes back from an archive.
const maxDatabaseTemplateIconBytes = 2000

func validDatabaseToolbarTemplateIdentity(operation databaseToolbarConfigMutationOperation) bool {
	return operation.TemplateID != nil && operation.ExpectedRevision != nil &&
		strings.TrimSpace(*operation.TemplateID) != "" && len(*operation.TemplateID) <= 255 &&
		*operation.ExpectedRevision >= 1
}

func databaseToolbarMutationHash(input databaseToolbarConfigMutationInput) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func applyDatabaseToolbarConfigMutation(
	app core.App,
	principalID, pageID, dataSourceID string,
	input databaseToolbarConfigMutationInput,
) (databaseToolbarConfigMutationResponse, error) {
	response := databaseToolbarConfigMutationResponse{}
	requestHash, err := databaseToolbarMutationHash(input)
	if err != nil {
		return response, err
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		authority, err := authorizeDatabaseToolbarConfig(
			txApp, principalID, pageID, dataSourceID, permissionWorkspaceWrite,
		)
		if err != nil {
			return err
		}
		replayed, err := replayDatabaseToolbarConfigReceipt(
			txApp, principalID, input.RequestID, requestHash, authority,
		)
		if err != nil {
			return err
		}
		if replayed != nil {
			response = *replayed
			response.Duplicate = true
			return nil
		}
		// Each operation is held to the revision of what it changes (the task
		// settings', a template's), so content changed elsewhere in the
		// workspace since the settings were read doesn't refuse a change, and
		// a change made meanwhile to the same setting does. The workspace
		// revision the request names is not compared.
		state := databaseToolbarMutationState{ActorID: principalID}
		for index, operation := range input.Operations {
			if err := applyDatabaseToolbarOperation(txApp, authority, operation, &state); err != nil {
				return refusalAt(err, fmt.Sprintf("operations[%d].", index))
			}
		}
		if state.ConfigurationRevision > 0 {
			// Source task settings affect graph presentation without changing
			// stored views. Advance the existing graph cursor atomically so open
			// and linked readers refresh through their normal collaboration path.
			digest := sha256.Sum256([]byte(principalID + "\x00" + input.RequestID))
			mutationID := "database-task-features:" + hex.EncodeToString(digest[:])
			if err := recordServerGraphChange(txApp, authority.OwnerID, authority.PageID,
				mutationID, crdt.EncodeStateAsUpdateV1(crdt.New(), nil), graphMutationCounts{}); err != nil {
				return err
			}
		}
		workspaceRevision, err := databaseToolbarWorkspaceRevision(txApp, authority.WorkspaceID)
		if err != nil {
			return err
		}
		if state.ConfigurationRevision == 0 {
			state.ConfigurationRevision, err = databaseToolbarConfigurationRevision(txApp, authority.DataSourceID)
			if err != nil {
				return err
			}
		}
		state.TemplateAuthorityRevision, err = databaseToolbarMaxTemplateRevision(txApp, authority)
		if err != nil {
			return err
		}
		response = databaseToolbarConfigMutationResponse{
			RequestID: input.RequestID, WorkspaceContentRevision: workspaceRevision,
			ConfigurationRevision:     state.ConfigurationRevision,
			TemplateAuthorityRevision: state.TemplateAuthorityRevision,
			SnapshotIdentity: databaseToolbarSnapshotIdentity(
				authority, workspaceRevision, state.ConfigurationRevision,
			),
		}
		if err := storeDatabaseToolbarConfigReceipt(
			txApp, principalID, requestHash, authority, response,
		); err != nil {
			return err
		}
		return pruneDatabaseToolbarConfigReceipts(txApp)
	})
	return response, err
}

func replayDatabaseToolbarConfigReceipt(
	app core.App,
	principalID, requestID, requestHash string,
	authority databaseToolbarConfigAuthority,
) (*databaseToolbarConfigMutationResponse, error) {
	receipt := databaseToolbarConfigReceipt{}
	err := app.DB().NewQuery(`SELECT [[workspace_id]], [[page_id]], [[data_source_id]],
		[[request_hash]], [[response_json]] FROM {{leafen_database_toolbar_mutation_receipts}}
		WHERE [[principal_id]]={:principalId} AND [[request_id]]={:requestId}`).Bind(dbx.Params{
		"principalId": principalID, "requestId": requestID,
	}).One(&receipt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if receipt.RequestHash != requestHash || receipt.WorkspaceID != authority.WorkspaceID ||
		receipt.PageID != authority.PageID || receipt.DataSourceID != authority.DataSourceID {
		return nil, errDatabaseToolbarConfigRequestReused
	}
	response := databaseToolbarConfigMutationResponse{}
	if json.Unmarshal([]byte(receipt.ResponseJSON), &response) != nil {
		return nil, errDatabaseToolbarConfigConflict
	}
	return &response, nil
}

func applyDatabaseToolbarOperation(
	app core.App,
	authority databaseToolbarConfigAuthority,
	operation databaseToolbarConfigMutationOperation,
	state *databaseToolbarMutationState,
) error {
	switch operation.Kind {
	case "configureTaskFeatures":
		return configureDatabaseToolbarTaskFeatures(app, authority, operation, state)
	case "createTemplate":
		return createDatabaseToolbarTemplate(app, authority, operation, state)
	case "updateTemplate":
		return updateDatabaseToolbarTemplate(app, authority, operation, state)
	case "duplicateTemplate":
		return duplicateDatabaseToolbarTemplate(app, authority, operation, state)
	case "deleteTemplate":
		return setDatabaseToolbarTemplateDeleted(app, authority, operation, true, state)
	case "restoreTemplate":
		return setDatabaseToolbarTemplateDeleted(app, authority, operation, false, state)
	case "setDefaultTemplate":
		return setDatabaseToolbarDefaultTemplate(app, authority, operation, state)
	case "setTemplateRepeat":
		return setDatabaseToolbarTemplateRepeat(app, authority, operation, state)
	default:
		return errDatabaseToolbarConfigInvalid
	}
}

// configureDatabaseToolbarTaskFeatures stores a database's task settings. A
// wiki's Sub-pages are a pair of their own, which leaves its pages' places in
// the wiki as they are (wiki_subpage_pair.go).
func configureDatabaseToolbarTaskFeatures(
	app core.App,
	authority databaseToolbarConfigAuthority,
	operation databaseToolbarConfigMutationOperation,
	state *databaseToolbarMutationState,
) error {
	current, err := databaseToolbarConfigurationRevision(app, authority.DataSourceID)
	if err != nil || operation.ExpectedRevision == nil || current != *operation.ExpectedRevision {
		if err != nil {
			return err
		}
		return errDatabaseToolbarConfigConflict
	}
	// What the task settings' screens never leave out (database_task_features_complete.go).
	if err := completeDatabaseTaskFeatures(app, authority, operation.TaskFeatures); err != nil {
		return err
	}
	next := current + 1
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// A client that does not send task roles keeps the stored ones.
	tasks, keepTasks := string(operation.TaskFeatures.Tasks), len(operation.TaskFeatures.Tasks) == 0
	if keepTasks {
		tasks = "null"
	}
	result, err := app.DB().NewQuery(`INSERT INTO {{leafen_database_task_features}}
		([[data_source_id]], [[owner]], [[workspace_id]], [[page_id]], [[authority_revision]],
		 [[subitems_json]], [[dependencies_json]], [[tasks_json]], [[created_at]], [[updated_at]])
		VALUES ({:dataSourceId}, {:owner}, {:workspaceId}, {:pageId}, {:revision},
		 {:subitems}, {:dependencies}, {:tasks}, {:now}, {:now})
		ON CONFLICT ([[data_source_id]]) DO UPDATE SET
		 [[authority_revision]]=excluded.[[authority_revision]],
		 [[subitems_json]]=CASE WHEN json_type(excluded.[[subitems_json]]) = 'object'
			AND json_type(excluded.[[subitems_json]], '$.parentPropertyId') IS NULL
			AND json_type({{leafen_database_task_features}}.[[subitems_json]], '$.parentPropertyId') = 'text'
		 THEN json_set(excluded.[[subitems_json]],
			'$.parentPropertyId', json_extract({{leafen_database_task_features}}.[[subitems_json]], '$.parentPropertyId'),
			'$.subitemsPropertyId', json_extract({{leafen_database_task_features}}.[[subitems_json]], '$.subitemsPropertyId'))
		 ELSE excluded.[[subitems_json]] END,
		 [[dependencies_json]]=excluded.[[dependencies_json]],
		 [[tasks_json]]=CASE WHEN {:keepTasks} THEN [[tasks_json]] ELSE excluded.[[tasks_json]] END,
		 [[updated_at]]=excluded.[[updated_at]]
		WHERE [[authority_revision]]={:current}`).Bind(dbx.Params{
		"dataSourceId": authority.DataSourceID, "owner": authority.OwnerID,
		"workspaceId": authority.WorkspaceID, "pageId": authority.PageID,
		"revision": next, "current": current, "subitems": string(operation.TaskFeatures.Subitems),
		"dependencies": string(operation.TaskFeatures.Dependencies), "tasks": tasks,
		"keepTasks": keepTasks, "now": now,
	}).Execute()
	if err = requireDatabaseToolbarMutationApplied(result, err); err == nil {
		state.ConfigurationRevision = next
	}
	return err
}

func requireDatabaseToolbarMutationApplied(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errDatabaseToolbarConfigConflict
	}
	return nil
}

func databaseToolbarConfigurationRevision(app core.App, dataSourceID string) (int, error) {
	var revision int
	err := app.DB().NewQuery(`SELECT [[authority_revision]] FROM {{leafen_database_task_features}}
		WHERE [[data_source_id]]={:dataSourceId}`).Bind(dbx.Params{"dataSourceId": dataSourceID}).Row(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return revision, err
}

func databaseToolbarWorkspaceRevision(app core.App, workspaceID string) (int, error) {
	var revision int
	err := app.DB().NewQuery(`SELECT [[revision]] FROM {{leafen_workspace_content_revisions}}
		WHERE [[workspace_id]]={:workspaceId}`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&revision)
	return revision, err
}

func storeDatabaseToolbarConfigReceipt(
	app core.App,
	principalID, requestHash string,
	authority databaseToolbarConfigAuthority,
	response databaseToolbarConfigMutationResponse,
) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`INSERT INTO {{leafen_database_toolbar_mutation_receipts}}
		([[principal_id]], [[request_id]], [[workspace_id]], [[page_id]], [[data_source_id]],
		 [[request_hash]], [[response_json]], [[completed_at]])
		VALUES ({:principalId}, {:requestId}, {:workspaceId}, {:pageId}, {:dataSourceId},
		 {:requestHash}, {:response}, {:completedAt})`).Bind(dbx.Params{
		"principalId": principalID, "requestId": response.RequestID,
		"workspaceId": authority.WorkspaceID, "pageId": authority.PageID,
		"dataSourceId": authority.DataSourceID, "requestHash": requestHash,
		"response": string(encoded), "completedAt": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

func pruneDatabaseToolbarConfigReceipts(app core.App) error {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour).UnixMilli()
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_toolbar_mutation_receipts}}
		WHERE ([[principal_id]], [[request_id]]) IN (
			SELECT [[principal_id]], [[request_id]] FROM {{leafen_database_toolbar_mutation_receipts}}
			WHERE [[completed_at]]<{:cutoff} ORDER BY [[completed_at]], [[principal_id]], [[request_id]]
			LIMIT 100
		)`).Bind(dbx.Params{"cutoff": cutoff}).Execute()
	return err
}
