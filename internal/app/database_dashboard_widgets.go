package app

import (
	"encoding/json"
	"math"
	"sort"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A dashboard view lays out widget views in rows, as Notion does: at most four
// widgets in a row and twelve in all. A widget is an ordinary view whose config
// names its dashboard in dashboardViewId; the dashboard's config lists widget
// IDs row by row in dashboardRows. Widgets are not tabs of their database, and
// deleting a dashboard deletes its widgets. A dashboard may also keep each
// row's height in pixels (0 is the default height, -1 its widgets' content —
// Notion's "Snap to view height") in dashboardRowHeights and each widget's
// share of its row in dashboardWidgetWidths; both follow the shape of
// dashboardRows.

const (
	maxDashboardWidgets       = 12
	maxDashboardWidgetsPerRow = 4
	// Notion's rows are 240–2140 high (read 2026-11-15); rows kept from
	// before at 160 or more stay valid.
	minDashboardRowHeight   = 160
	maxDashboardRowHeight   = 2140
	dashboardRowFitsContent = -1
	minDashboardWidgetWidth = 0.2
	maxDashboardWidgetWidth = 5
)

type dashboardViewConfig struct {
	DashboardViewID       string      `json:"dashboardViewId"`
	DashboardRows         [][]string  `json:"dashboardRows"`
	DashboardRowHeights   []float64   `json:"dashboardRowHeights"`
	DashboardWidgetWidths [][]float64 `json:"dashboardWidgetWidths"`
}

func parseDashboardViewConfig(raw []byte) dashboardViewConfig {
	config := dashboardViewConfig{}
	_ = json.Unmarshal(raw, &config)
	return config
}

// dashboardWidgetOwner is the dashboard a widget view belongs to, if any.
func dashboardWidgetOwner(raw []byte) string {
	return parseDashboardViewConfig(raw).DashboardViewID
}

func validateGraphDashboardView(projection graphProjection, view *graphDatabaseView, pageID string) error {
	config := dashboardViewConfig{}
	if len(view.Config) != 0 && json.Unmarshal(view.Config, &config) != nil {
		return graphInputError(errorCodeGraphInvalid, "Check the dashboard widget layout.")
	}
	if err := validateDashboardLayoutSizes(config); err != nil {
		return err
	}
	if len(config.DashboardRows) > 0 {
		if view.Type != graphViewTypeDashboard {
			return graphInputError(errorCodeGraphInvalid, "Only a dashboard can have widget rows.")
		}
		seen := map[string]struct{}{}
		for _, row := range config.DashboardRows {
			if len(row) == 0 || len(row) > maxDashboardWidgetsPerRow {
				return graphInputError(errorCodeGraphInvalid, "A dashboard row can hold 1 to 4 widgets.")
			}
			for _, id := range row {
				if _, duplicate := seen[id]; duplicate || id == "" || id == view.ID {
					return graphInputError(errorCodeGraphInvalid, "Check the dashboard widget layout.")
				}
				seen[id] = struct{}{}
			}
		}
		if len(seen) > maxDashboardWidgets {
			return graphInputError(errorCodeGraphInvalid, "A dashboard can hold up to 12 widgets.")
		}
	}
	if config.DashboardViewID == "" {
		return nil
	}
	if view.Type == graphViewTypeDashboard || config.DashboardViewID == view.ID {
		return graphInputError(errorCodeGraphInvalid, "A dashboard can't be a widget of another dashboard.")
	}
	// Copies and imports can write a widget before its dashboard; a widget
	// whose dashboard is absent shows nowhere until it arrives.
	dashboard, exists := projection.views[config.DashboardViewID]
	if exists && (dashboard.PageID != pageID || dashboard.Type != graphViewTypeDashboard) {
		return graphInputError(errorCodeGraphReferenceInvalid, "The widget's dashboard was not found.")
	}
	return nil
}

// validateDashboardLayoutSizes keeps row heights and widget widths in the
// shape of the rows and within their bounds.
func validateDashboardLayoutSizes(config dashboardViewConfig) error {
	invalid := graphInputError(errorCodeGraphInvalid, "Check the dashboard widget size.")
	if config.DashboardRowHeights != nil {
		if len(config.DashboardRowHeights) != len(config.DashboardRows) {
			return invalid
		}
		for _, height := range config.DashboardRowHeights {
			if height != 0 && height != dashboardRowFitsContent &&
				(height != math.Trunc(height) || height < minDashboardRowHeight || height > maxDashboardRowHeight) {
				return invalid
			}
		}
	}
	if config.DashboardWidgetWidths != nil {
		if len(config.DashboardWidgetWidths) != len(config.DashboardRows) {
			return invalid
		}
		for index, widths := range config.DashboardWidgetWidths {
			if len(widths) != len(config.DashboardRows[index]) {
				return invalid
			}
			for _, width := range widths {
				if math.IsNaN(width) || width < minDashboardWidgetWidth || width > maxDashboardWidgetWidth {
					return invalid
				}
			}
		}
	}
	return nil
}

// deleteOrphanedDashboardWidgets removes the widgets whose dashboard left the
// page in this mutation.
func deleteOrphanedDashboardWidgets(
	app core.App, doc *crdt.Doc, owner, workspaceID, pageID, actorID string, operations []graphOperation,
) error {
	deletes := false
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindDeleteView, graphOperationKindDeleteBlock, graphOperationKindDeleteDataSource:
			deletes = true
		}
	}
	if !deletes {
		return nil
	}
	views, err := loadDatabaseViewRecords(app, pageID)
	if err != nil {
		return err
	}
	orphaned := []graphOperation{}
	for id, stored := range views {
		dashboardID, _ := stored.Config["dashboardViewId"].(string)
		if dashboardID == "" {
			continue
		}
		if _, live := views[dashboardID]; live {
			continue
		}
		orphaned = append(orphaned, graphOperation{
			Kind: graphOperationKindDeleteView, ViewDeletion: &graphDatabaseDelete{ID: id},
		})
	}
	if len(orphaned) == 0 {
		return nil
	}
	sort.Slice(orphaned, func(left, right int) bool {
		return orphaned[left].ViewDeletion.ID < orphaned[right].ViewDeletion.ID
	})
	return applyDatabaseGraphMutation(app, doc, owner, workspaceID, pageID, actorID, orphaned)
}
