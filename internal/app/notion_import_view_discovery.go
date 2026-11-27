package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// After a database's rows the import lists the database's views in their
// tab order and reads each one, one provider request a turn, staging it as a
// view of the database. The views are made once every property exists, in
// the link phase, so a view may name a relation or rollup. The first view
// that is a tab becomes the view the database block shows. When Notion does
// not give the views, or gives a view Leafen cannot show, the database keeps
// the one table view it was made with and the report says so.

// notionImportViewPlan is a Notion view as the import stages it.
type notionImportViewPlan struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Type            string          `json:"type"`
	DashboardViewID string          `json:"dashboardViewId,omitempty"`
	Primary         bool            `json:"primary,omitempty"`
	Filter          json.RawMessage `json:"filter,omitempty"`
	Sorts           json.RawMessage `json:"sorts,omitempty"`
	QuickFilters    json.RawMessage `json:"quickFilters,omitempty"`
	Configuration   json.RawMessage `json:"configuration,omitempty"`
	// Truncated marks a view whose settings were too large to stage.
	Truncated bool `json:"truncated,omitempty"`
	// DataSourceKey names the data source a view of a database of several
	// shows when it is not the first; SourceFirst marks the first tab of its
	// data source, which takes over the table the data source was made with.
	DataSourceKey string `json:"dataSourceKey,omitempty"`
	SourceFirst   bool   `json:"sourceFirst,omitzero"`
	// LinkedBlock is the block of the linked database that owns the view.
	LinkedBlock string `json:"linkedBlock,omitempty"`
}

// notionImportViewPlanBytes bounds the settings the import stages for one view.
const notionImportViewPlanBytes = 256 << 10

// notionImportViewNames name an unnamed view in the import's own lists.
var notionImportViewNames = map[string]string{
	graphViewTypeTable: "Table", graphViewTypeBoard: "Board", graphViewTypeList: "List",
	graphViewTypeCalendar: "Calendar", graphViewTypeTimeline: "Timeline", graphViewTypeGallery: "Gallery",
	graphViewTypeForm: "Form", graphViewTypeChart: "Chart", graphViewTypeMap: "Map",
	graphViewTypeDashboard: "Dashboard", graphViewTypeFeed: "Feed",
}

// notionImportManualOrderViews show rows in the order people arranged them
// when they have no sort; Notion's API does not give that order.
var notionImportManualOrderViews = map[string]struct{}{
	graphViewTypeTable: {}, graphViewTypeBoard: {}, graphViewTypeList: {}, graphViewTypeGallery: {}, graphViewTypeFeed: {},
}

// notionImportViewSourceID is the staged source of a database's view.
func notionImportViewSourceID(databaseID, viewID string) string {
	return databaseID + ":view:" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(viewID), "-", ""))
}

// notionImportViewIssueSource stages one issue about a view, or about the
// views of a database, under the database's place on its page.
func notionImportViewIssueSource(databaseID, subjectSourceID, title, reason string, position int) stagedNotionSource {
	return stagedNotionSource{
		SourceID: subjectSourceID + "#" + reason, Kind: "issue", ParentSourceID: databaseID,
		ParentKind: notionImportViewIssueParentKind, Position: position, Title: title, Text: reason,
	}
}

// View issues sit under the database the view belongs to.
const notionImportViewIssueParentKind = "view"

func (r *durableJobRunner) discoverNotionDatabaseViews(
	ctx context.Context, job storedJob, payload notionImportJobPayload, client notionProviderClient,
) error {
	databaseID := payload.ActiveDatabaseID
	if len(payload.DatabaseViewIDs) > 0 {
		view, err := client.view(ctx, payload.DatabaseViewIDs[0])
		if err != nil && !notionProviderRefused(err) {
			return err
		}
		payload.DatabaseViewIDs = payload.DatabaseViewIDs[1:]
		sources := []stagedNotionSource{}
		if err != nil {
			issue, issueErr := notionImportDatabaseViewsIssue(r.app, job.ID, databaseID, notionImportIssueViewsUnavailable)
			if issueErr != nil {
				return issueErr
			}
			sources = append(sources, issue)
		} else if sources, err = stageNotionImportView(r.app, job.ID, &payload, view); err != nil {
			return err
		}
		return r.commitNotionImportViews(ctx, client, job, payload, sources)
	}
	if !payload.DatabaseViewsListed {
		page, err := client.databaseViews(ctx, databaseID, payload.DatabaseViewCursor)
		if err != nil && !notionProviderRefused(err) {
			return err
		}
		sources := []stagedNotionSource{}
		if err != nil {
			issue, issueErr := notionImportDatabaseViewsIssue(r.app, job.ID, databaseID, notionImportIssueViewsUnavailable)
			if issueErr != nil {
				return issueErr
			}
			sources = append(sources, issue)
			payload.DatabaseViewsListed = true
		} else {
			payload.DatabaseViewIDs = page.IDs
			payload.DatabaseViewCursor = page.NextCursor
			payload.DatabaseViewsListed = !page.HasMore
		}
		return r.commitNotionImportViews(ctx, client, job, payload, sources)
	}
	return r.commitNotionImportViews(ctx, client, job, payload, nil)
}

// commitNotionImportViews stages what a views turn read, and moves discovery
// past the database once every view is read, reporting that Notion keeps its
// rows' arranged order when a view shows them in it and there is an order to
// keep: a database of one row or none has none.
func (r *durableJobRunner) commitNotionImportViews(
	ctx context.Context, client notionProviderClient, job storedJob, payload notionImportJobPayload, sources []stagedNotionSource,
) error {
	if payload.DatabaseViewsListed && len(payload.DatabaseViewIDs) == 0 {
		templates, templateFiles, err := notionImportTemplateSources(ctx, r.app, client, job.ID, &payload)
		if err != nil {
			return err
		}
		sources = append(sources, templates...)
		payload.PendingFileIDs = append(payload.PendingFileIDs, templateFiles...)
		if payload.DatabaseViewManualOrder && payload.DatabaseRowPosition > 1 {
			issue, err := notionImportDatabaseViewsIssue(
				r.app, job.ID, payload.ActiveDatabaseID, notionImportIssueViewRowOrderUnavailable,
			)
			if err != nil {
				return err
			}
			sources = append(sources, issue)
		}
		finishNotionImportDatabase(&payload)
	}
	return r.commitNotionDiscovery(job, payload, sources)
}

// stageNotionImportView stages a view the import can make, or the reason it
// cannot.
func stageNotionImportView(
	app core.App, jobID string, payload *notionImportJobPayload, view notionProviderView,
) ([]stagedNotionSource, error) {
	databaseID := payload.ActiveDatabaseID
	sourceID := notionImportViewSourceID(databaseID, view.ID)
	name := notionImportViewName(view.Name, view.Type)
	if _, known := graphViewTypes[view.Type]; !known {
		return []stagedNotionSource{notionImportViewIssueSource(
			databaseID, sourceID, name, notionImportIssueViewTypeUnsupported, payload.DatabaseViewPosition,
		)}, nil
	}
	dataSourceKey := ""
	if view.DataSourceID != nil && *view.DataSourceID != "" && len(payload.DatabaseSourceIDs) > 0 {
		shown := -1
		for index, id := range payload.DatabaseSourceIDs {
			if shown < 0 && notionImportSameID(id, *view.DataSourceID) {
				shown = index
			}
		}
		if shown < 0 {
			// The view shows a data source the import did not bring over.
			issue, err := notionImportDatabaseViewsIssue(app, jobID, databaseID, notionImportIssueViewsUnavailable)
			return []stagedNotionSource{issue}, err
		}
		if shown > 0 {
			dataSourceKey = notionImportDataSourceKey(databaseID, payload.DatabaseSourceIDs[shown])
		}
	}
	plan := notionImportViewPlan{
		ID: view.ID, Name: notionImportViewGivenName(view.Name), Type: view.Type, DataSourceKey: dataSourceKey,
		Filter: notionImportPresentJSON(view.Filter), Sorts: notionImportPresentJSON(view.Sorts),
		QuickFilters:  notionImportPresentJSON(view.QuickFilters),
		Configuration: notionImportPresentJSON(view.Configuration),
	}
	if view.DashboardViewID != nil && *view.DashboardViewID != "" && view.Type != graphViewTypeDashboard {
		plan.DashboardViewID = *view.DashboardViewID
	}
	if plan.DashboardViewID == "" && !payload.DatabaseViewPrimary {
		plan.Primary, payload.DatabaseViewPrimary = true, true
	}
	if seen := databaseID + dataSourceKey; plan.DashboardViewID == "" && !slices.Contains(payload.DatabaseViewSourcesSeen, seen) {
		plan.SourceFirst = true
		payload.DatabaseViewSourcesSeen = append(payload.DatabaseViewSourcesSeen, seen)
	}
	if _, manual := notionImportManualOrderViews[view.Type]; manual && plan.DashboardViewID == "" && len(plan.Sorts) == 0 {
		payload.DatabaseViewManualOrder = true
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	if len(encoded) > notionImportViewPlanBytes {
		plan.Filter, plan.Sorts, plan.QuickFilters, plan.Configuration, plan.Truncated = nil, nil, nil, nil, true
		if encoded, err = json.Marshal(plan); err != nil {
			return nil, err
		}
	}
	source := stagedNotionSource{
		SourceID: sourceID, Kind: "view", ParentSourceID: databaseID,
		Position: payload.DatabaseViewPosition, Title: name, Text: string(encoded),
	}
	payload.DatabaseViewPosition++
	return []stagedNotionSource{source}, nil
}

// notionImportPresentJSON drops a Notion field that is absent or null.
func notionImportPresentJSON(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" || trimmed == "{}" {
		return nil
	}
	return raw
}

// notionImportViewGivenName is Notion's name for a view as the import
// stages it: as given, spaces too, within a view name's length.
func notionImportViewGivenName(name string) string {
	if utf8.RuneCountInString(name) > maxGraphIdentifierLength {
		name = string([]rune(name)[:maxGraphIdentifierLength])
	}
	return name
}

// notionImportViewOwnName is the name a view brought over keeps, and whether
// it was given one: Notion's API reads a view never named as "Untitled",
// which leaves it unnamed, as a view made in Leafen is, and keeps any other
// name, "" too, as its own (database_view_name.go). A view named "Untitled"
// by hand reads the same through the API, so it comes over unnamed too.
func notionImportViewOwnName(staged string) (string, bool) {
	if staged == notionUnnamedViewName {
		return "", false
	}
	return staged, true
}

// notionImportViewName is a view's name in the import's own lists: its name,
// or its layout's for a view unnamed.
func notionImportViewName(name, viewType string) string {
	if name == notionUnnamedViewName {
		name = ""
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = notionImportViewNames[viewType]
	}
	if name == "" {
		name = "View"
	}
	if utf8.RuneCountInString(name) > maxGraphIdentifierLength {
		name = string([]rune(name)[:maxGraphIdentifierLength])
	}
	return name
}

// notionImportDatabaseViewsIssue stages an issue about a database's views,
// named after the database.
func notionImportDatabaseViewsIssue(app core.App, jobID, databaseID, reason string) (stagedNotionSource, error) {
	titles := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[title]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]='database'
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": databaseID}).Column(&titles); err != nil {
		return stagedNotionSource{}, err
	}
	title := ""
	if len(titles) == 1 {
		title = titles[0]
	}
	return notionImportViewIssueSource(databaseID, databaseID+":views", title, reason, 0), nil
}
