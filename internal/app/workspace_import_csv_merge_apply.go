package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// workspaceCSVMergeMutationHooks apply a CSV import's turn to its database:
// the options it adds join a property's options as they are now, and its
// new properties join what a table shows now, as other changes go on.
func workspaceCSVMergeMutationHooks(
	job storedJob,
	pageID string,
	operations []graphOperation,
) (contentGraphMutationHooks, error) {
	canonical, err := json.Marshal(operations)
	if err != nil {
		return contentGraphMutationHooks{}, err
	}
	digest := sha256.Sum256(canonical)
	return contentGraphMutationHooks{
		Authorize:        workspaceImportGraphAuthorization(job, pageID),
		AllowTrashedPage: true,
		ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
			return hex.EncodeToString(digest[:]), nil
		},
		Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
			prepared := make([]graphOperation, 0, len(operations))
			for _, operation := range operations {
				switch {
				case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil &&
					workspaceCSVMergeMarked(operation.Property.Config, workspaceCSVMergeOptionsKey):
					property, err := workspaceCSVMergeOptionsNow(txApp, pageID, *operation.Property)
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					operation.Property = &property
				case operation.Kind == graphOperationKindUpsertView && operation.View != nil &&
					workspaceCSVMergeMarked(operation.View.Config, workspaceCSVMergeShowKey):
					view, shows, err := workspaceCSVMergeTableNow(txApp, pageID, *operation.View)
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					if !shows {
						continue
					}
					operation.View = &view
				}
				prepared = append(prepared, operation)
			}
			return contentGraphMutationPreparation{Operations: prepared}, nil
		},
	}, nil
}

func workspaceCSVMergeMarked(config json.RawMessage, key string) bool {
	values := map[string]json.RawMessage{}
	if json.Unmarshal(config, &values) != nil {
		return false
	}
	_, marked := values[key]
	return marked
}

// workspaceCSVMergeOptionsNow is a property with the options the import
// adds after its own; a property changed so they no longer fit stops it.
func workspaceCSVMergeOptionsNow(app core.App, pageID string, staged graphProperty) (graphProperty, error) {
	stale := workspaceImportTargetNotFreshError(nil, "csv-merge-options")
	current := workspaceCSVMergeProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[name]], [[position]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[id]] = {:id} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
	`).Bind(dbx.Params{"id": staged.ID, "pageId": pageID, "dataSourceId": staged.DataSourceID}).One(&current)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Type != staged.Type) {
		return graphProperty{}, stale
	}
	if err != nil {
		return graphProperty{}, err
	}
	marker := struct {
		Options []notionDatabaseRowPageOption `json:"csvMergeOptions"`
	}{}
	config := map[string]any{}
	if json.Unmarshal(staged.Config, &marker) != nil ||
		(current.Config != "" && json.Unmarshal([]byte(current.Config), &config) != nil) {
		return graphProperty{}, stale
	}
	config, err = appendWorkspaceCSVMergeOptions(config, marker.Options)
	if err != nil {
		return graphProperty{}, stale
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return graphProperty{}, err
	}
	return graphProperty{
		ID: current.ID, DataSourceID: staged.DataSourceID, Type: current.Type,
		Name: current.Name, Position: current.Position, Config: encoded,
	}, nil
}

// appendWorkspaceCSVMergeOptions is a property's config with the options
// the import adds after its own; one whose name it has already refuses.
func appendWorkspaceCSVMergeOptions(config map[string]any, added []notionDatabaseRowPageOption) (map[string]any, error) {
	options, _ := config["options"].([]any)
	ids, names := map[string]bool{}, map[string]bool{}
	for _, raw := range options {
		if option, ok := raw.(map[string]any); ok {
			id, _ := option["id"].(string)
			name, _ := option["name"].(string)
			ids[id], names[strings.TrimSpace(name)] = true, true
		}
	}
	for _, option := range added {
		if ids[option.ID] {
			continue
		}
		if names[option.Name] {
			return nil, errors.New("csv merge option name taken")
		}
		entry := map[string]any{"id": option.ID, "name": option.Name, "color": option.Color}
		// A status's new option joins its first group.
		if _, status := config["statusGroups"]; status {
			if entry["group"] = databaseRowPageJoinStatusGroup(config, option.ID, ""); entry["group"] == "" {
				return nil, errors.New("csv merge status has no group")
			}
		}
		options = append(options, entry)
		ids[option.ID], names[option.Name] = true, true
	}
	next := make(map[string]any, len(config)+1)
	for key, value := range config {
		next[key] = value
	}
	next["options"] = options
	return next, nil
}

// workspaceCSVMergeTableNow is a table that shows the import's new
// properties after those it shows now; one that no longer lists what it
// shows already shows them.
func workspaceCSVMergeTableNow(app core.App, pageID string, staged graphDatabaseView) (graphDatabaseView, bool, error) {
	current, exists, err := loadDatabaseViewRecord(app, pageID, staged.ID)
	if err != nil || !exists || current.DataSourceID != staged.DataSourceID {
		return graphDatabaseView{}, false, err
	}
	visible, lists := current.Config["visibleProperties"].([]any)
	marker := struct {
		IDs []string `json:"csvMergeShow"`
	}{}
	if !lists || json.Unmarshal(staged.Config, &marker) != nil {
		return graphDatabaseView{}, false, nil
	}
	config := make(map[string]any, len(current.Config))
	for key, value := range current.Config {
		config[key] = value
	}
	order, ordered := current.Config["propertyOrder"].([]any)
	for _, id := range marker.IDs {
		if !slices.Contains(visible, any(id)) {
			visible = append(visible, id)
		}
		if ordered && !slices.Contains(order, any(id)) {
			order = append(order, id)
		}
	}
	config["visibleProperties"] = visible
	if ordered {
		config["propertyOrder"] = order
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return graphDatabaseView{}, false, err
	}
	return graphDatabaseView{
		ID: staged.ID, DataSourceID: current.DataSourceID, ContainerID: current.ContainerID,
		Type: current.Type, Name: current.Name, Position: current.Position, Config: encoded,
	}, true, nil
}

// cleanupWorkspaceCSVMerge takes back what an import that stopped short
// added, a turn at a time: its rows, then each new property no other row
// filled meanwhile, which the database deletion lifecycle deletes as it
// deletes a row from the trash. The options it added stay, as values may use
// them.
func (r *durableJobRunner) cleanupWorkspaceCSVMerge(job storedJob, payload workspaceImportJobPayload) error {
	next := 0
	if payload.AppendCleanupCursor != "" {
		parsed, err := strconv.Atoi(payload.AppendCleanupCursor)
		if err != nil || parsed < 0 {
			return workspaceImportStateErrorAt("csv-merge-cleanup-cursor")
		}
		next = parsed
	}
	// Rows go in after the import's other items, a turn at a time, so no row
	// past those its turns applied, and one turn more, can be there.
	rows := min(payload.MergeRows, job.Processed+workspaceImportMutationTurnItems)
	if next > rows || (next == rows && len(payload.MergeNewPropertyIDs) == 0) {
		return r.finishWorkspaceMarkdownAppendCleanup(job, payload)
	}
	page, err := r.app.FindRecordById(pagesCollection, job.PageID)
	if errors.Is(err, sql.ErrNoRows) {
		return r.finishWorkspaceMarkdownAppendCleanup(job, payload)
	}
	if err != nil {
		return err
	}
	// The page's owner deletes them, as the import's own cleanup acts for it.
	actorID := page.GetString("owner")
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.MergeDataSourceID == "" ||
			currentPayload.AppendCleanupCursor != payload.AppendCleanupCursor {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("csv-merge-cleanup-advance")
		}
		targets := map[string]string{}
		end := min(next+workspaceImportCleanupTurnItems, rows)
		if next < rows {
			for index := next; index < end; index++ {
				targets[workspaceCSVMergeJobRowID(job, payload, index)] = databaseMutationTargetRow
			}
		} else {
			end = rows + 1
			if targets, err = workspaceCSVMergeUnfilledProperties(txApp, job, payload); err != nil {
				return err
			}
		}
		admitted := false
		for _, id := range slices.Sorted(maps.Keys(targets)) {
			var exists int
			table := "leafen_database_rows"
			if targets[id] == databaseMutationTargetProperty {
				table = "leafen_database_properties"
			}
			if err := txApp.DB().Select("COUNT(*)").From(table).
				Where(dbx.HashExp{"id": id, "page_id": job.PageID}).Row(&exists); err != nil {
				return err
			}
			if exists == 0 {
				continue
			}
			requestID := "csv-merge-cleanup:" + id
			if _, err := admitDatabaseMutationDeletionInTransaction(txApp, actorID, job.PageID, databaseMutationInput{
				RequestID: requestID, MutationID: requestID, TargetKind: targets[id], TargetID: id,
			}); err != nil {
				return err
			}
			admitted = true
		}
		if admitted {
			notifyDatabaseMutationDeletionAfterCommit(txApp)
		}
		currentPayload.AppendCleanupCursor = strconv.Itoa(end)
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, current.Cursor, current.Processed, true,
		)
	})
}

// workspaceCSVMergeUnfilledProperties are the import's new properties that
// only its own rows filled.
func workspaceCSVMergeUnfilledProperties(
	app core.App,
	job storedJob,
	payload workspaceImportJobPayload,
) (map[string]string, error) {
	imported := make(map[string]struct{}, payload.MergeRows)
	for index := range payload.MergeRows {
		imported[workspaceCSVMergeJobRowID(job, payload, index)] = struct{}{}
	}
	targets := map[string]string{}
	for _, id := range payload.MergeNewPropertyIDs {
		filled := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[value]].[[row_id]] FROM {{leafen_database_properties}} AS [[property]]
			JOIN {{leafen_database_row_values}} AS [[value]] ON [[value]].[[owner]] = [[property]].[[owner]]
				AND [[value]].[[page_id]] = [[property]].[[page_id]] AND [[value]].[[property_id]] = [[property]].[[id]]
			WHERE [[property]].[[id]] = {:id}
		`).Bind(dbx.Params{"id": id}).Column(&filled); err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(filled, func(rowID string) bool {
			_, own := imported[rowID]
			return !own
		}) {
			targets[id] = databaseMutationTargetProperty
		}
	}
	return targets, nil
}
