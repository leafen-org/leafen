package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Restoring a version with its databases brings each database the page holds
// back as Notion's restore does (seen 2026-10-01, a temporary page's inline
// database): its views as the version had them and, for one the page made,
// its name, its properties' names, settings and order, a property deleted
// since back and one added since deleted, and its rows' values as they were
// then, a row taken away since back from the trash and one made since to the
// trash. A property whose type changed since takes the version's type back
// as a type change does, its cells converting while its rows take their
// values then (seen 2026-10-01: a number made text comes back a number with
// its numbers); one no type change converts to (title, place, relation)
// keeps its type and values. Each database comes back in changes of its own,
// the first with its views and properties and its rows in as many more as
// they need. A change already made is not made again, so asking again
// finishes what an earlier restore left. A full-page database's restore panel
// can leave its properties, its rows' values or its rows' names as they are
// (Notion's three switches, seen 2026-11-05). A restore can take back one
// database alone (databaseId), leaving the page and its other databases as
// they are, as Notion's restore-version does for an inline database.

type pageHistoryDatabaseRestore struct {
	sourceID string
	schema   []graphOperation
	// Properties in Deleted properties the version had, and those added since.
	propertiesBack, propertiesAway []string
	// Trash events holding rows the version had, and rows made since.
	rowsBack []string
	rowsAway []databaseRowPageAuthority
	rows     []graphOperation
}

func (plan pageHistoryDatabaseRestore) hasEffects() bool {
	return len(plan.propertiesBack)+len(plan.propertiesAway)+len(plan.rowsBack)+len(plan.rowsAway) > 0
}

func (p *pocketBaseYgoPersistence) restorePageHistoryDatabaseContents(
	ctx context.Context, actorID, pageID, versionID string, input pageHistoryRestoreRequest,
) error {
	version, err := loadPageHistoryVersion(p.app, pageID, versionID)
	if err != nil {
		return err
	}
	state, err := materializePageHistoryVersion(p.app, pageID, version.VersionCursor)
	if err != nil {
		return err
	}
	owner, err := pageHistoryDatabaseOwner(p.app, pageID)
	if err != nil {
		return err
	}
	scopes, err := pageHistoryDatabaseScopes(p.app, pageID, state, input.DatabaseID)
	if err != nil {
		return err
	}
	sourceIDs := make([]string, 0, len(scopes))
	for sourceID := range scopes {
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Strings(sourceIDs)
	var failed error
	for _, sourceID := range sourceIDs {
		plan, err := planPageHistoryDatabaseRestore(
			p.app, owner, pageID, sourceID, state.Databases[sourceID], version.CreatedAt, input, scopes[sourceID],
		)
		if err == nil {
			err = p.applyPageHistoryDatabaseRestore(ctx, actorID, pageID, input.RequestID, plan)
		}
		failed = errors.Join(failed, err)
	}
	return failed
}

func (p *pocketBaseYgoPersistence) applyPageHistoryDatabaseRestore(
	ctx context.Context, actorID, pageID, requestID string, plan pageHistoryDatabaseRestore,
) error {
	effects, operations, rows := plan, append([]graphOperation(nil), plan.schema...), plan.rows
	for {
		taken := min(len(rows), max(maxGraphMutationOperations-len(operations), 0))
		operations, rows = append(operations, rows[:taken]...), rows[taken:]
		if len(operations) > 0 || effects.hasEffects() {
			if err := p.applyPageHistoryDatabaseRestoreChange(ctx, actorID, pageID, requestID, effects, operations); err != nil {
				return err
			}
		}
		if len(rows) == 0 {
			return nil
		}
		effects, operations = pageHistoryDatabaseRestore{sourceID: plan.sourceID}, nil
	}
}

// applyPageHistoryDatabaseRestoreChange makes one change of a database's
// restore, known by what it makes.
func (p *pocketBaseYgoPersistence) applyPageHistoryDatabaseRestoreChange(
	ctx context.Context, actorID, pageID, requestID string,
	effects pageHistoryDatabaseRestore, operations []graphOperation,
) error {
	encoded, err := json.Marshal(struct {
		Source, Request      string
		Operations           []graphOperation
		PropertiesBack, Away []string
		RowsBack             []string
		RowsAway             []string
	}{
		effects.sourceID, requestID, operations, effects.propertiesBack, effects.propertiesAway,
		effects.rowsBack, pageHistoryRowIDs(effects.rowsAway),
	})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(append([]byte("page-history-restore-databases\x00"+pageID+"\x00"), encoded...))
	requestHash := hex.EncodeToString(digest[:])
	historyRows := make([]pageHistoryRowChange, 0, len(effects.rowsBack)+len(effects.rowsAway))
	for _, root := range effects.rowsBack {
		historyRows = append(historyRows, pageHistoryRowChange{ID: root, Change: pageHistoryChangeRowRestored})
	}
	for _, row := range effects.rowsAway {
		historyRows = append(historyRows, pageHistoryRowChange{ID: row.RowID, Change: pageHistoryChangeRowTrashed})
	}
	_, err = p.applyContentGraphMutationContext(ctx, actorID, pageID,
		graphMutationInput{MutationID: "page-history-restore-databases:" + requestHash[:40]},
		contentGraphMutationHooks{
			HistoryRows: historyRows,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return requestHash, nil
			},
			// What comes back from the trash does before its changes.
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				now := time.Now().UTC()
				for _, propertyID := range effects.propertiesBack {
					target, err := loadDatabasePropertyTrashTarget(txApp, pageID, propertyID)
					if err == nil && target.DeletedAt != 0 && target.Deleting == 0 {
						err = restoreDatabaseProperty(txApp, target)
					}
					if err != nil && !errors.Is(err, errDatabaseMutationNotFound) {
						return contentGraphMutationPreparation{}, err
					}
				}
				for _, root := range effects.rowsBack {
					if _, err := restoreDatabaseRowEvent(txApp, root, actorID, now); err != nil &&
						!errors.Is(err, errDatabaseRowPageConflict) {
						return contentGraphMutationPreparation{}, err
					}
				}
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
			// What the version didn't have goes once the views no longer use it.
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				now := time.Now().UTC()
				for _, propertyID := range effects.propertiesAway {
					target, err := loadDatabasePropertyTrashTarget(txApp, pageID, propertyID)
					if err == nil && target.DeletedAt == 0 {
						if kept, keptErr := wikiPropertyKept(txApp, target.ID); keptErr != nil || kept {
							err = keptErr
						} else {
							err = trashDatabaseProperty(txApp, target, actorID, now)
						}
					}
					if err != nil && !errors.Is(err, errDatabaseMutationNotFound) &&
						!errors.Is(err, errDatabasePropertyInUse) && !errors.Is(err, errDatabaseMutationConflict) {
						return err
					}
				}
				for _, row := range effects.rowsAway {
					trashed, err := trashedDatabaseRowIDs(txApp, []string{row.RowID})
					if err != nil {
						return err
					}
					if _, gone := trashed[row.RowID]; gone {
						continue
					}
					if _, err := trashDatabaseRowEvent(txApp, row, actorID, now); err != nil &&
						!errors.Is(err, errDatabaseRowPageConflict) {
						return err
					}
				}
				return nil
			},
		},
	)
	return err
}

func pageHistoryRowIDs(rows []databaseRowPageAuthority) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.RowID)
	}
	return ids
}

// planPageHistoryDatabaseRestore lists what differs from the version in one
// database the page holds now.
func planPageHistoryDatabaseRestore(
	app core.App, owner, pageID, sourceID string, kept pageHistoryDatabaseState, at int64, input pageHistoryRestoreRequest,
	scope pageHistoryDatabaseScope,
) (pageHistoryDatabaseRestore, error) {
	plan := pageHistoryDatabaseRestore{sourceID: sourceID}
	params := dbx.Params{"owner": owner, "pageId": pageID, "sourceId": sourceID}
	var held int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_data_sources}} WHERE [[id]]={:sourceId} AND [[owner]]={:owner} AND [[page_id]]={:pageId}
	`).Bind(params).Row(&held); err != nil {
		return plan, err
	}
	// A database the page held then and holds no longer keeps what it has
	// where it is now.
	if kept.Source != nil && held == 0 {
		return plan, nil
	}
	views := []struct {
		ID          string `db:"id"`
		ContainerID string `db:"container_id"`
		Type        string `db:"type"`
		Name        string `db:"name"`
		Position    string `db:"position"`
		Config      string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[container_id]],[[type]],[[name]],[[position]],[[config]] FROM {{leafen_database_views}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:sourceId}
	`).Bind(params).All(&views); err != nil {
		return plan, err
	}
	if scope.ContainerID != "" {
		mine := views[:0]
		for _, view := range views {
			if scope.holdsView(view.ContainerID) {
				mine = append(mine, view)
			}
		}
		views = mine
		keptMine := []pageHistoryDatabaseView{}
		for _, view := range kept.Views {
			if scope.holdsView(view.ContainerID) {
				keptMine = append(keptMine, view)
			}
		}
		kept.Views = keptMine
	}
	keptViews := map[string]struct{}{}
	for _, view := range kept.Views {
		keptViews[view.ID] = struct{}{}
		same := false
		for _, current := range views {
			if current.ID == view.ID {
				same = current.ContainerID == view.ContainerID && current.Type == view.Type && current.Name == view.Name &&
					current.Position == view.Position && pageHistorySameJSON(current.Config, string(view.Config))
			}
		}
		if !same {
			plan.schema = append(plan.schema, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
				ID: view.ID, DataSourceID: sourceID, ContainerID: view.ContainerID, Type: view.Type,
				Name: view.Name, Position: view.Position, Config: view.Config,
			}})
		}
	}
	for _, current := range views {
		if _, found := keptViews[current.ID]; !found && len(kept.Views) > 0 {
			plan.schema = append(plan.schema, graphOperation{
				Kind: graphOperationKindDeleteView, ViewDeletion: &graphDatabaseDelete{ID: current.ID},
			})
		}
	}
	if kept.Source == nil || scope.ViewsOnly {
		return plan, nil
	}
	if err := planPageHistoryDataSourceRestore(app, &plan, kept); err != nil {
		return plan, err
	}
	// A version that doesn't know its properties leaves them and the values.
	if len(kept.Properties) == 0 {
		return plan, planPageHistoryRowRestore(app, &plan, owner, pageID, nil, at)
	}
	restoredTypes, err := planPageHistoryPropertyRestore(app, &plan, pageID, kept, input.KeepDatabaseProperties)
	if err != nil {
		return plan, err
	}
	for propertyID, valueType := range restoredTypes {
		if valueType == graphValueTypeTitle && input.KeepRowNames || valueType != graphValueTypeTitle && input.KeepRowValues {
			delete(restoredTypes, propertyID)
		}
	}
	return plan, planPageHistoryRowRestore(app, &plan, owner, pageID, restoredTypes, at)
}

func planPageHistoryDataSourceRestore(app core.App, plan *pageHistoryDatabaseRestore, kept pageHistoryDatabaseState) error {
	current := pageHistoryDatabaseSource{}
	if err := app.DB().NewQuery(`
		SELECT [[title]],[[title_spans]],[[icon]],[[icon_type]],[[description]],[[description_spans]],[[description_hidden]],[[cover]],[[cover_position]]
		FROM {{leafen_data_sources}} WHERE [[id]]={:sourceId}
	`).Bind(dbx.Params{"sourceId": plan.sourceID}).Row(
		&current.Title, &current.TitleSpans, &current.Icon, &current.IconType, &current.Description, &current.DescriptionSpans,
		&current.DescriptionHidden, &current.Cover, &current.CoverPosition,
	); err != nil {
		return err
	}
	source := *kept.Source
	if current == source {
		return nil
	}
	// The version's name comes back as it was formatted, or plain.
	titleSpans := decodeStoredTextSpans(source.TitleSpans)
	if titleSpans == nil {
		titleSpans = []graphTextSpan{}
	}
	plan.schema = append(plan.schema, graphOperation{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{
		ID: plan.sourceID, Title: source.Title, TitleSpans: titleSpans, Icon: &source.Icon, IconType: &source.IconType,
		Description: &source.Description, DescriptionSpans: decodeStoredTextSpans(source.DescriptionSpans),
		DescriptionHidden: &source.DescriptionHidden, Cover: &source.Cover, CoverPosition: &source.CoverPosition,
	}})
	return nil
}

// planPageHistoryPropertyRestore plans the version's properties back and
// reports the type each property the version had takes.
func planPageHistoryPropertyRestore(
	app core.App, plan *pageHistoryDatabaseRestore, pageID string, kept pageHistoryDatabaseState, keepProperties bool,
) (map[string]string, error) {
	properties := []struct {
		ID       string `db:"id"`
		Type     string `db:"type"`
		Name     string `db:"name"`
		Position string `db:"position"`
		Config   string `db:"config"`
		Deleted  int64  `db:"deleted_at"`
		Deleting int    `db:"deleting"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[id]],[[property]].[[type]],[[property]].[[name]],[[property]].[[position]],[[property]].[[config]],
			COALESCE([[trash]].[[deleted_at]],0) AS [[deleted_at]], COALESCE([[trash]].[[deleting]],0) AS [[deleting]]
		FROM {{leafen_database_properties}} AS [[property]]
		LEFT JOIN {{leafen_database_property_trash}} AS [[trash]] ON [[trash]].[[property_id]]=[[property]].[[id]]
		WHERE [[property]].[[page_id]]={:pageId} AND [[property]].[[data_source_id]]={:sourceId}
		ORDER BY [[property]].[[position]],[[property]].[[id]]
	`).Bind(dbx.Params{"pageId": pageID, "sourceId": plan.sourceID}).All(&properties); err != nil {
		return nil, err
	}
	keptByID := map[string]pageHistoryDatabaseProperty{}
	for _, property := range kept.Properties {
		keptByID[property.ID] = property
	}
	types := map[string]string{}
	for _, current := range properties {
		property, inVersion := keptByID[current.ID]
		if keepProperties {
			// They stay as they are; those that read the version's values take them.
			if inVersion && current.Deleted == 0 && current.Deleting == 0 && current.Type == property.Type {
				types[property.ID] = property.Type
			}
			continue
		}
		if !inVersion {
			if current.Deleted == 0 && current.Type != graphValueTypeTitle {
				plan.propertiesAway = append(plan.propertiesAway, current.ID)
			}
			continue
		}
		if current.Deleting != 0 || !pageHistoryPropertyTypeRestorable(current.Type, property.Type) {
			continue
		}
		types[property.ID] = property.Type
		if current.Deleted != 0 {
			plan.propertiesBack = append(plan.propertiesBack, property.ID)
		} else if current.Type == property.Type && current.Name == property.Name &&
			current.Position == property.Position && pageHistorySamePropertyConfig(current.Config, string(property.Config)) {
			continue
		}
		plan.schema = append(plan.schema, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: property.ID, DataSourceID: plan.sourceID, Type: property.Type, Name: property.Name,
			Position: property.Position, Config: property.Config,
		}})
	}
	return types, nil
}

// pageHistoryPropertyTypeRestorable tells whether a property can take back
// the type a version had: one its cells read the same as, or one a type
// change converts its cells to.
func pageHistoryPropertyTypeRestorable(current, then string) bool {
	if graphValueTypesShareRepresentation(current, then) {
		return true
	}
	_, err := newDatabasePropertyConversion(current, then, nil, nil, databasePropertyConversionNames{})
	return err == nil
}

// pageHistorySamePropertyConfig compares two settings of a property apart
// from the former names the server keeps on its own.
func pageHistorySamePropertyConfig(left, right string) bool {
	var a, b map[string]any
	if json.Unmarshal([]byte(left), &a) != nil || json.Unmarshal([]byte(right), &b) != nil {
		return pageHistorySameJSON(left, right)
	}
	delete(a, databasePropertyFormerNamesKey)
	delete(b, databasePropertyFormerNamesKey)
	return reflect.DeepEqual(a, b)
}

// planPageHistoryRowRestore plans each row the version had back to its
// values then, and the rows made since to the trash.
func planPageHistoryRowRestore(
	app core.App, plan *pageHistoryDatabaseRestore, owner, pageID string, types map[string]string, at int64,
) error {
	rows := []struct {
		databaseRowPageAuthority
		TrashedAt int64  `db:"trashed_at"`
		Root      string `db:"root_row_id"`
		Deleting  int    `db:"deleting"`
		AwayThen  bool   `db:"away_then"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[id]] AS [[row_id]], [[row]].[[owner]], [[row]].[[workspace_id]],
			[[row]].[[page_id]] AS [[container_page_id]], [[row]].[[data_source_id]], [[row]].[[position]],
			[[row]].[[subitem_parent_id]], [[row]].[[created]],
			COALESCE([[trash]].[[trashed_at]],0) AS [[trashed_at]], COALESCE([[trash]].[[root_row_id]],'') AS [[root_row_id]],
			COALESCE([[trash]].[[deleting]],0) AS [[deleting]],
			` + databaseRowAwayAtSQL("[[row]].[[id]]") + ` AS [[away_then]]
		FROM {{leafen_database_rows}} AS [[row]]
		LEFT JOIN {{leafen_database_row_trash}} AS [[trash]] ON [[trash]].[[row_id]]=[[row]].[[id]]
		WHERE [[row]].[[owner]]={:owner} AND [[row]].[[page_id]]={:pageId} AND [[row]].[[data_source_id]]={:sourceId}
		ORDER BY [[row]].[[position]],[[row]].[[id]]
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "sourceId": plan.sourceID, "at": at}).All(&rows); err != nil {
		return err
	}
	current := map[string]map[string]pageHistoryRowValue{}
	cells := []struct {
		RowID      string `db:"row_id"`
		PropertyID string `db:"property_id"`
		Type       string `db:"type"`
		Value      string `db:"value"`
		Spans      string `db:"spans"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[value]].[[row_id]],[[value]].[[property_id]],[[value]].[[type]],[[value]].[[value]],[[value]].[[spans]]
		FROM {{leafen_database_row_values}} AS [[value]]
		JOIN {{leafen_database_rows}} AS [[row]] ON [[row]].[[id]]=[[value]].[[row_id]]
		WHERE [[row]].[[page_id]]={:pageId} AND [[row]].[[data_source_id]]={:sourceId}
	`).Bind(dbx.Params{"pageId": pageID, "sourceId": plan.sourceID}).All(&cells); err != nil {
		return err
	}
	for _, cell := range cells {
		if current[cell.RowID] == nil {
			current[cell.RowID] = map[string]pageHistoryRowValue{}
		}
		current[cell.RowID][cell.PropertyID] = pageHistoryRowValue{
			Type: cell.Type, Value: json.RawMessage(cell.Value), Spans: json.RawMessage(cell.Spans),
		}
	}
	backRoots := map[string]struct{}{}
	for _, row := range rows {
		if row.CreatedAt > at {
			if row.TrashedAt == 0 {
				plan.rowsAway = append(plan.rowsAway, row.databaseRowPageAuthority)
			}
			continue
		}
		if row.TrashedAt != 0 && row.TrashedAt <= at {
			continue
		}
		// In trash at the version and back since: the version had it away.
		if row.AwayThen {
			if row.TrashedAt == 0 {
				plan.rowsAway = append(plan.rowsAway, row.databaseRowPageAuthority)
			}
			continue
		}
		if row.TrashedAt != 0 {
			if row.Deleting != 0 {
				continue
			}
			if _, listed := backRoots[row.Root]; !listed {
				backRoots[row.Root] = struct{}{}
				plan.rowsBack = append(plan.rowsBack, row.Root)
			}
		}
		then, err := pageHistoryRowValuesAt(app, row.RowID, at)
		if err != nil {
			return err
		}
		values, changed, err := pageHistoryRowRestoreValues(types, then, current[row.RowID])
		if err != nil {
			return err
		}
		if changed {
			plan.rows = append(plan.rows, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
				ID: row.RowID, DataSourceID: plan.sourceID, SubitemParentID: optionalDatabaseRowParent(row.SubitemParentID),
				Position: row.Position, Values: values,
			}})
		}
	}
	return nil
}

// pageHistoryRowRestoreValues are a row's values for the properties the
// version had, as they were then: none where it had none.
func pageHistoryRowRestoreValues(
	types map[string]string, then, now map[string]pageHistoryRowValue,
) (map[string]graphRowValue, bool, error) {
	values := map[string]graphRowValue{}
	changed := false
	for propertyID, valueType := range types {
		if databaseRowPageComputedValueTypes[valueType] {
			continue
		}
		cell, found := then[propertyID]
		value := graphRowValue{Type: valueType, Value: graphRowValueCleared}
		if found && cell.Type == valueType {
			value.Value = cell.Value
			if len(cell.Spans) > 0 && json.Unmarshal(cell.Spans, &value.Spans) != nil {
				return nil, false, errDatabaseRowPageValuesInvalid
			}
		}
		values[propertyID] = value
		current, has := now[propertyID]
		if !pageHistoryRowValueSame(value, current, has) {
			changed = true
		}
	}
	return values, changed, nil
}

func pageHistoryRowValueSame(value graphRowValue, current pageHistoryRowValue, has bool) bool {
	empty := func(raw json.RawMessage) bool {
		return len(raw) == 0 || pageHistorySameJSON(string(raw), "null") || pageHistorySameJSON(string(raw), `""`) ||
			pageHistorySameJSON(string(raw), "[]")
	}
	if !has || empty(current.Value) {
		return empty(value.Value)
	}
	if current.Type != value.Type || !pageHistorySameJSON(string(current.Value), string(value.Value)) {
		return false
	}
	spans, _ := json.Marshal(value.Spans)
	if len(value.Spans) == 0 {
		spans = nil
	}
	return empty(current.Spans) && len(spans) == 0 || pageHistorySameJSON(string(current.Spans), string(spans))
}
