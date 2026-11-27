package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The link phase makes each staged Notion view once every property of its
// database exists, in the database's tab order. The first view that is a tab
// replaces the table the database was made with and becomes the view its
// block shows. A dashboard lays out the widgets the import brought over; a
// widget whose dashboard did not come over becomes a tab. A setting Leafen
// refuses as the view API would leaves the view with the rest of its place.

// notionImportViewNativeID is the ID a staged view takes; a dashboard and its
// widgets name each other by it before both are made.
func notionImportViewNativeID(jobID, sourceID string) string {
	return notionImportLinkedPropertyID(jobID, sourceID)
}

// stagedView reads a staged view's plan, or nil when the import did not
// stage the view.
func (linker *notionImportLinker) stagedView(sourceID string) (*notionImportViewPlan, error) {
	if plan, found := linker.views[sourceID]; found {
		return plan, nil
	}
	texts := []string{}
	if err := linker.app.DB().NewQuery(`
		SELECT [[text]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]='view'
	`).Bind(dbx.Params{"jobId": linker.job.ID, "sourceId": sourceID}).Column(&texts); err != nil {
		return nil, err
	}
	var plan *notionImportViewPlan
	if len(texts) == 1 {
		plan = &notionImportViewPlan{}
		if json.Unmarshal([]byte(texts[0]), plan) != nil {
			plan = nil
		}
	}
	linker.views[sourceID] = plan
	return plan, nil
}

func (linker *notionImportLinker) viewConverter(dataSourceID, dataSourceKey, pageID string) (*notionImportViewConverter, error) {
	if converter, found := linker.converters[dataSourceID]; found {
		return converter, nil
	}
	converter, err := newNotionImportViewConverter(linker.app, dataSourceID, pageID, func(id string) (string, bool) {
		rowID, err := linker.lookup(id, "row")
		return rowID, err == nil && rowID != ""
	})
	if err != nil {
		return nil, err
	}
	staged := []struct {
		SourceID string `db:"source_id"`
		Title    string `db:"title"`
	}{}
	if err := linker.app.DB().NewQuery(`
		SELECT [[source_id]], [[title]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]] IN ('property','issue') AND [[parent_source_id]]={:key}
			AND instr([[source_id]], ':property:')>0
	`).Bind(dbx.Params{"jobId": linker.job.ID, "key": dataSourceKey}).All(&staged); err != nil {
		return nil, err
	}
	converter.sourceProperties = make(map[string]bool, 2*len(staged))
	for _, property := range staged {
		_, notionID, _ := strings.Cut(property.SourceID, ":property:")
		notionID, _, _ = strings.Cut(notionID, "#")
		converter.sourceProperties[notionImportPropertyKey(notionID)] = true
		converter.sourceProperties[notionImportPropertyKey(property.Title)] = true
	}
	if err := linker.gatheredViewProperties(converter, dataSourceKey); err != nil {
		return nil, err
	}
	linker.converters[dataSourceID] = converter
	return converter, nil
}

// view makes the native view a staged Notion view stands for.
func (linker *notionImportLinker) view(source storedNotionSource) error {
	plan := notionImportViewPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return err
	}
	linker.views[source.SourceID] = &plan
	// A merged database's views aren't made; its rows show in its target's.
	if _, merged := linker.run.mergeTarget(source.ParentSourceID); merged {
		return nil
	}
	// A view made before, by a migration's earlier run, stays as it is,
	// unless Notion changed it since: then it takes Notion's settings again,
	// as a row takes its values (decision 3절 원칙 4).
	existing, err := linker.lookup(source.SourceID, "view")
	if err != nil {
		return err
	}
	if existing != "" {
		if changed, err := linker.viewChangedSince(source); err != nil || !changed {
			return err
		}
	}
	dataSourceKey := source.ParentSourceID
	if plan.DataSourceKey != "" {
		dataSourceKey = plan.DataSourceKey
	}
	dataSourceID, err := linker.lookup(dataSourceKey, "data_source")
	if err != nil {
		return err
	}
	reasons := notionImportViewReasons{}
	var linked *linkedViewPlace
	if plan.LinkedBlock != "" {
		if linked, err = linker.linkedViewPlace(plan); err != nil || linked == nil {
			return err
		}
		if linked.gone {
			return linker.goneLinkedBlock(plan, linked)
		}
		if linked.dataSourceID == "" {
			return linker.dropLinkedBlock(plan, linked, source.Sequence)
		}
		// A linked database holds what its source can show on a page of its own.
		if plan.Type == graphViewTypeForm || plan.Type == graphViewTypeDashboard {
			plan.Type = graphViewTypeTable
			reasons.add(notionImportIssueViewSettingOmitted)
		}
	}
	if dataSourceID == "" {
		return nil
	}
	pageID, err := linker.dataSourcePage(dataSourceID)
	if err != nil {
		return err
	}
	converter, err := linker.viewConverter(dataSourceID, dataSourceKey, pageID)
	if err != nil {
		return err
	}
	base := importViewBase(nil)
	if existing != "" {
		if err := linker.keepRowOrder(existing, base); err != nil {
			return err
		}
	}
	if plan.DashboardViewID != "" {
		dashboardSource := notionImportViewSourceID(source.ParentSourceID, plan.DashboardViewID)
		dashboard, err := linker.stagedView(dashboardSource)
		if err != nil {
			return err
		}
		if dashboard != nil && dashboard.Type == graphViewTypeDashboard {
			if base["dashboardViewId"], err = linker.viewID(dashboardSource); err != nil {
				return err
			}
		} else {
			reasons.add(notionImportIssueViewSettingOmitted)
		}
	}
	config, converted, err := converter.config(plan, base)
	if err != nil {
		return err
	}
	for reason := range converted {
		reasons.add(reason)
	}
	if plan.Type == graphViewTypeDashboard {
		if config, err = linker.dashboardLayout(source, plan, config, reasons); err != nil {
			return err
		}
	}
	name, named := notionImportViewOwnName(plan.Name)
	if named {
		if config, err = withViewNameGiven(config, name); err != nil {
			return err
		}
	}
	view := &graphDatabaseView{
		ID: notionImportViewNativeID(linker.job.ID, source.SourceID), DataSourceID: dataSourceID, Type: plan.Type,
		Name: name, Position: fmt.Sprintf("p%010d", source.Position), Config: config,
	}
	if existing != "" {
		position, kept, err := linker.keptView(existing, dataSourceID)
		if err != nil || !kept {
			return err
		}
		view.ID, view.Position = existing, position
	}
	operations, references := []graphOperation{}, notionGraphApplyReferences{}
	if linked != nil {
		view.ContainerID, pageID = linked.block.ID, linked.block.PageID
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: view})
		sourceTitle, err := linker.sourceTitle(dataSourceID)
		if err != nil {
			return err
		}
		if references, err = linked.references(linker.app, view); err != nil {
			return err
		}
		if block, first := linked.blockOperation(view.ID, sourceTitle); first {
			operations = append([]graphOperation{block}, operations...)
		} else if block, members, err := linked.memberOperation(linker.app, linker.payload.AuthorityOwnerID, linker.job.WorkspaceID); err != nil {
			return err
		} else if block != nil {
			// A view of another source makes it one of the block's members.
			operations = append([]graphOperation{*block}, operations...)
			maps.Copy(references.sources, members)
		}
	} else if existing != "" {
		operations = []graphOperation{{Kind: graphOperationKindUpsertView, View: view}}
	} else if operations, err = linker.viewOperations(source, plan, view); err != nil {
		return err
	}
	_, err = validateGraphMutationWithAllReferenceCachesMode(
		linker.app, linker.payload.AuthorityOwnerID, linker.job.WorkspaceID, pageID, operations,
		nil, references.sources, nil, references.properties, nil, nil, false, false,
	)
	var refused *graphRequestError
	if errors.As(err, &refused) && refused.status == http.StatusBadRequest {
		view.Config = notionImportJSON(base)
		reasons.add(notionImportIssueViewSettingOmitted)
	} else if err != nil {
		return err
	}
	batch, err := loadStructuralMutationBatch(linker.app, pageID, linker.documents)
	if err != nil {
		return err
	}
	if err := applyNotionGraphOperationsWith(
		linker.app, batch, linker.payload.AuthorityOwnerID, linker.job.WorkspaceID, pageID, linker.job.Owner,
		operations, references,
	); err != nil {
		rejected := &notionImportRefusedError{}
		if linked != nil && errors.As(err, &rejected) {
			// One Leafen won't take is reported where it stood, as one whose
			// source did not come over.
			return linker.dropLinkedBlock(plan, linked, source.Sequence)
		}
		return err
	}
	for _, reason := range reasons.sorted() {
		if err := recordNotionImportViewIssue(linker.app, linker.job.ID, source.SourceID, reason); err != nil {
			return err
		}
	}
	if existing != "" {
		// Made again, it answers for this run's problems alone (notion_import_late.go).
		return forgetNotionMigrationViewIssues(linker.app, linker.payload.MigrationID, source.SourceID)
	}
	return linker.mappings.insert(source.SourceID, view.ID, "view")
}

// viewID is the view a staged view is: the one an earlier run made of it, or
// the one this run makes.
func (linker *notionImportLinker) viewID(sourceID string) (string, error) {
	if id, err := linker.lookup(sourceID, "view"); err != nil || id != "" {
		return id, err
	}
	return notionImportViewNativeID(linker.job.ID, sourceID), nil
}

// viewChangedSince reports whether Notion changed a view an earlier run made:
// the ledger keeps what the run before staged of it (notion_migration_rerun.go).
// One the ledger knows nothing of stays.
func (linker *notionImportLinker) viewChangedSince(source storedNotionSource) (bool, error) {
	if linker.payload.MigrationRerunSince <= 0 {
		return false, nil
	}
	// One showing a property made since takes it (notion_import_late.go).
	if notionImportViewShowsLate(linker.payload, source) {
		return true, nil
	}
	known := ""
	err := linker.app.DB().NewQuery(`
		SELECT [[state]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]='view'
	`).Bind(dbx.Params{"migrationId": linker.payload.MigrationID, "sourceId": source.SourceID}).Row(&known)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && known != "" && known != source.Text, err
}

// keptView reads where a view an earlier run made stands; one a person
// deleted stays gone, and one moved to another database stays as it is.
func (linker *notionImportLinker) keptView(viewID, dataSourceID string) (string, bool, error) {
	stored := struct {
		DataSourceID string `db:"data_source_id"`
		Position     string `db:"position"`
	}{}
	err := linker.app.DB().NewQuery(`SELECT [[data_source_id]],[[position]] FROM {{leafen_database_views}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": viewID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return stored.Position, stored.DataSourceID == dataSourceID, nil
}

// keepRowOrder keeps in base the row order of a view an earlier run made,
// which Notion's settings don't hold: the rows placed in it stay placed, and
// one made before views kept their own order keeps the database's.
func (linker *notionImportLinker) keepRowOrder(viewID string, base map[string]any) error {
	config := ""
	err := linker.app.DB().NewQuery(`SELECT [[config]] FROM {{leafen_database_views}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": viewID}).Row(&config)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	kept := struct {
		RowOrder []any `json:"rowOrder"`
	}{}
	if json.Unmarshal([]byte(config), &kept) != nil || kept.RowOrder == nil {
		delete(base, "rowOrder")
		return nil
	}
	base["rowOrder"] = kept.RowOrder
	return nil
}

// viewOperations makes a view. The first tab also becomes the view the
// database block shows, and the first tab of each data source removes the
// table the data source was made with; a plan staged before data sources
// had their own first tab removes the database's with its first tab.
func (linker *notionImportLinker) viewOperations(
	source storedNotionSource, plan notionImportViewPlan, view *graphDatabaseView,
) ([]graphOperation, error) {
	operations := []graphOperation{{Kind: graphOperationKindUpsertView, View: view}}
	sourceFirst := plan.SourceFirst || plan.Primary && plan.DataSourceKey == ""
	if !plan.Primary && !sourceFirst {
		return operations, nil
	}
	defaultID, err := linker.lookup(notionImportDefaultViewSourceID(source.ParentSourceID), "view")
	if err != nil {
		return nil, err
	}
	ownDefaultID := defaultID
	if plan.DataSourceKey != "" {
		if ownDefaultID, err = linker.lookup(notionImportDefaultViewSourceID(plan.DataSourceKey), "view"); err != nil {
			return nil, err
		}
	}
	removeOwnDefault := func() []graphOperation {
		if !sourceFirst || ownDefaultID == "" {
			return operations
		}
		return append(operations, graphOperation{
			Kind: graphOperationKindDeleteView, ViewDeletion: &graphDatabaseDelete{ID: ownDefaultID},
		})
	}
	if !plan.Primary {
		return removeOwnDefault(), nil
	}
	blockID, err := linker.lookup(source.ParentSourceID+":block", "block")
	if err != nil || defaultID == "" || blockID == "" {
		return removeOwnDefault(), err
	}
	blocks := []struct {
		ParentID string `db:"parent_id"`
		Position string `db:"position"`
		Content  string `db:"content"`
	}{}
	if err := linker.app.DB().NewQuery(`
		SELECT [[parent_id]], [[position]], [[content]] FROM {{leafen_blocks}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": blockID}).All(&blocks); err != nil {
		return nil, err
	}
	content := map[string]any{}
	if len(blocks) != 1 || json.Unmarshal([]byte(blocks[0].Content), &content) != nil || content["viewId"] != defaultID {
		return removeOwnDefault(), nil
	}
	content["viewId"] = view.ID
	var parentID *string
	if blocks[0].ParentID != "" {
		parentID = &blocks[0].ParentID
	}
	operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
		ID: blockID, ParentID: parentID, Position: blocks[0].Position,
		Type: graphBlockTypeInlineDatabase, Content: notionImportJSON(content),
	}})
	return removeOwnDefault(), nil
}

// dashboardLayout lays a dashboard's widgets out in Notion's rows, within
// Leafen's four widgets a row and twelve in all. A row keeps its height and
// each widget its share of the row.
func (linker *notionImportLinker) dashboardLayout(
	source storedNotionSource, plan notionImportViewPlan, config json.RawMessage, reasons notionImportViewReasons,
) (json.RawMessage, error) {
	settings := struct {
		Rows []struct {
			Height  float64 `json:"height"`
			Widgets []struct {
				ViewID string  `json:"view_id"`
				Width  float64 `json:"width"`
			} `json:"widgets"`
		} `json:"rows"`
	}{}
	if len(plan.Configuration) == 0 || json.Unmarshal(plan.Configuration, &settings) != nil {
		return config, nil
	}
	layout := dashboardViewConfig{}
	heights, widths := []float64{}, [][]float64{}
	sizedRows, sizedWidgets, placed := false, false, 0
	for _, row := range settings.Rows {
		ids, shares, total := []string{}, []float64{}, 0.0
		for _, widget := range row.Widgets {
			widgetSource := notionImportViewSourceID(source.ParentSourceID, widget.ViewID)
			staged, err := linker.stagedView(widgetSource)
			if err != nil {
				return nil, err
			}
			if staged == nil || !notionImportSameID(staged.DashboardViewID, plan.ID) ||
				len(ids) == maxDashboardWidgetsPerRow || placed == maxDashboardWidgets {
				reasons.add(notionImportIssueViewSettingOmitted)
				continue
			}
			widgetID, err := linker.viewID(widgetSource)
			if err != nil {
				return nil, err
			}
			ids = append(ids, widgetID)
			shares = append(shares, widget.Width)
			total += math.Max(widget.Width, 0)
			placed++
		}
		if len(ids) == 0 {
			continue
		}
		layout.DashboardRows = append(layout.DashboardRows, ids)
		height := 0.0
		if row.Height > 0 {
			height, sizedRows = math.Round(math.Min(math.Max(row.Height, minDashboardRowHeight), maxDashboardRowHeight)), true
		}
		heights = append(heights, height)
		rowWidths := make([]float64, 0, len(shares))
		for _, width := range shares {
			share := 1.0
			if width > 0 && total > 0 {
				share = math.Round(width*float64(len(shares))/total*1000) / 1000
				share = math.Min(math.Max(share, minDashboardWidgetWidth), maxDashboardWidgetWidth)
			}
			sizedWidgets = sizedWidgets || share != 1
			rowWidths = append(rowWidths, share)
		}
		widths = append(widths, rowWidths)
	}
	if sizedRows {
		layout.DashboardRowHeights = heights
	}
	if sizedWidgets {
		layout.DashboardWidgetWidths = widths
	}
	return dashboardConfigWithLayout(string(config), layout)
}

// recordNotionImportViewIssue reports what a view lost, under its database.
func recordNotionImportViewIssue(app core.App, jobID, viewSourceID, reason string) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_sources}}
			([[job_id]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]],
			 [[text]], [[blueprint]], [[discovery_status]], [[discovery_cursor]], [[discovery_position]], [[created]])
		SELECT [[job_id]], [[source_id]] || '#' || {:reason}, 'issue', [[parent_source_id]], {:parentKind},
			[[position]], [[title]], {:reason}, 0, 'complete', '', 0, {:created}
		FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]='view'
		ON CONFLICT ([[job_id]], [[source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "sourceId": viewSourceID, "reason": reason,
		"parentKind": notionImportViewIssueParentKind, "created": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}
