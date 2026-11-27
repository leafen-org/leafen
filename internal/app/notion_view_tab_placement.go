package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A new view takes its place among all its database's tabs, whatever their
// data source, as Notion's API places one (seen 2026-11-12: a view put after
// a tab of the database's other source stands right after it, and one put
// first or last is first or last of them all): the API's position and MCP's
// alike. A dashboard's widgets stand in their dashboard, not among the tabs.

// notionDatabaseTabs are a database's tabs in order, every source's, feeds
// too.
func notionDatabaseTabs(app core.App, workspaceID, containerID string) ([]notionViewLocation, error) {
	tabs := []notionViewLocation{}
	if err := app.DB().NewQuery(notionViewTabSelect + `
		AND [[membership]].[[container_id]] = {:containerId}
		ORDER BY [[membership]].[[position]], [[view]].[[id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "containerId": containerID}).All(&tabs); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(tabs, func(tab notionViewLocation) bool {
		return dashboardWidgetOwner([]byte(tab.Config)) != ""
	}), nil
}

// notionTabPlacement is where a new tab goes: "start", "end", or
// "after_view" the tab whose own ID is anchorID.
type notionTabPlacement struct {
	kind, anchorID string
}

var errNotionTabAnchorMissing = errors.New("the tab a new view goes after is none of its database's")

// notionPlacedTabOperations add created where placement puts it among its
// database's tabs: after them all as it is, or first or after a tab with
// every tab numbered afresh in that order. A database opens on its first
// tab, as Notion's does, so one put first is what it opens on.
func notionPlacedTabOperations(
	app core.App, workspaceID, containerID string, created graphDatabaseView, placement notionTabPlacement,
) ([]graphOperation, error) {
	tabs, err := notionDatabaseTabs(app, workspaceID, containerID)
	if err != nil {
		return nil, err
	}
	at := len(tabs)
	switch placement.kind {
	case "start":
		at = 0
	case "after_view":
		at = slices.IndexFunc(tabs, func(tab notionViewLocation) bool { return tab.ID == placement.anchorID }) + 1
		if placement.anchorID == "" || at == 0 {
			return nil, errNotionTabAnchorMissing
		}
	}
	if at == len(tabs) {
		last := ""
		for _, tab := range tabs {
			last = max(last, tab.Position)
		}
		if created.Position, err = databasePositionAfter(last); err != nil {
			return nil, err
		}
		return []graphOperation{{Kind: graphOperationKindUpsertView, View: &created}}, nil
	}
	operations := make([]graphOperation, 0, len(tabs)+2)
	for index := 0; index <= len(tabs); index++ {
		number := fmt.Sprintf("p%010d", index)
		if index == at {
			created.Position = number
			operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &created})
			continue
		}
		kept := index
		if index > at {
			kept = index - 1
		}
		tab := tabs[kept]
		if tab.Position != number {
			operations = append(operations, notionViewOperation(tab, tab.Name, number, json.RawMessage(tab.Config)))
		}
	}
	if at == 0 && len(tabs) > 0 {
		shown, err := notionDatabaseBlockShowing(app, containerID, created.ID, "")
		if err != nil {
			return nil, err
		}
		if shown != nil {
			operations = append(operations, *shown)
		}
	}
	return operations, nil
}

// notionBodyIDShown is an ID from a request's body as Notion's view API
// names it back: lowercase, a UUID with its dashes (seen 2026-11-12, given
// in capitals or without dashes).
func notionBodyIDShown(id string) string {
	return strings.ToLower(notionDashedUUID(strings.TrimSpace(id)))
}

// notionToolRefusal is a refusal Notion's view API says with its tool's
// code and words beside it, the words' template with what they name left
// in braces (seen 2026-11-12).
type notionToolRefusal struct {
	message, code, template string
}

func (refusal notionToolRefusal) Error() string { return refusal.message }

func (refusal notionToolRefusal) data() map[string]any {
	return notionToolErrorData(refusal.code, refusal.template)
}

// notionToolErrorData is what a tool's refusal carries beside its words in
// Notion's API; a refusal said one way only names no template (seen
// 2026-11-11).
func notionToolErrorData(code, template string) map[string]any {
	data := map[string]any{"tool_error_code": code, "tool_error_class": "validation", "tool_error_retryable": "false"}
	if template != "" {
		data["tool_error_message_template"] = template
	}
	return data
}

// notionViewNotInDatabase refuses a new view put after one that is none of
// its database's tabs, a view it can't find too.
func notionViewNotInDatabase(viewID string) notionToolRefusal {
	return notionToolRefusal{
		message:  `View "` + viewID + `" isn't in this database.`,
		code:     "invalid_position",
		template: `View "{afterViewId}" isn't in this database.`,
	}
}

// notionViewNotDashboard refuses a widget whose view_id names a view that is
// no dashboard.
func notionViewNotDashboard(viewID string) notionToolRefusal {
	return notionToolRefusal{
		message:  `view_id "` + viewID + `" must name a dashboard view.`,
		code:     "invalid_target",
		template: `view_id "{dashboardId}" must name a dashboard view.`,
	}
}
