package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

// A view a program makes through the API starts as Notion's API makes it,
// not as a person's new view on the screen (seen 2026-11-12, a board,
// gallery, list, calendar and timeline made with no configuration): its
// cards, lines and bars show the title alone, a gallery shows no preview, and
// a board shows no group's count, its later groups' and those by people or
// values too (Notion's board has one count for the view, which an API board
// starts without, seen 2026-11-12) and, grouped by its default property,
// hides its empty groups — a board given its grouping shows them (Notion
// keeps hideEmptyGroups only on the grouping it picked). It names no
// grouping until it is shown: the API reads its configuration null, as
// Notion's, and the screen draws its default and keeps it once shown
// (databaseViewShownConfig) — the first of a kind in the order the API lists
// them (seen 2026-11-12), its options' groups by name as a person's new board
// starts them (seen 2026-11-12: options made 시작 전, 진행 중, 완료 showed
// 시작 전, 완료, 진행 중 and read back sort "manual"); its order and hidden
// empty groups are set from the start, so it draws them alike before. A
// calendar or timeline keeps its date once shown too, not a timeline's zoom
// or centre. A board wraps all its content, with its grouping given or not, where a
// table, gallery and calendar wrap nothing (seen 2026-11-12: "모든 콘텐츠
// 줄바꿈" on only for the board; the API takes no setting for it). What the
// request's configuration set stays as set.

// notionAPITitleAloneLayouts are the layouts an API view starts showing the
// title alone in; a table shows every property, as Notion's does.
var notionAPITitleAloneLayouts = []string{
	graphViewTypeBoard, graphViewTypeGallery, graphViewTypeList, graphViewTypeCalendar, graphViewTypeTimeline,
}

// notionAPICreatedViewConfig is config, the settings an API request gave a
// new view, with the first settings of an API view of its type where the
// request set none.
//
// The reader is the caller's; a board left to its default reads its source's properties.
func notionAPICreatedViewConfig(app core.App, _ string, location notionViewLocation, config json.RawMessage) (json.RawMessage, error) {
	if !slices.Contains(notionAPITitleAloneLayouts, location.Type) {
		return config, nil
	}
	settings := map[string]any{}
	if err := json.Unmarshal(config, &settings); err != nil {
		return nil, err
	}
	listed := false
	for _, key := range []string{"visibleProperties", "hiddenProperties", "propertyOrder"} {
		if _, set := settings[key]; set {
			listed = true
		}
	}
	if !listed {
		settings["visibleProperties"] = []any{}
	}
	switch location.Type {
	case graphViewTypeGallery:
		if _, set := settings["coverProperty"]; !set {
			settings["coverProperty"] = "__none"
		}
	case graphViewTypeBoard:
		if _, set := settings["wrap"]; !set {
			settings["wrap"] = true
		}
		if _, given := settings["groupBy"].(string); !given {
			properties, err := databaseSourceLiveProperties(app, location.DataSourceID, nil, nil)
			if err != nil {
				return nil, err
			}
			regroupDatabaseBoardWithoutProperty(settings, properties)
			delete(settings, "groupBy")
			settings["hideEmptyGroups"] = true
		}
		if _, set := settings["hiddenGroupCounts"]; !set {
			settings["groupCountsHidden"] = true
		}
	}
	return json.Marshal(settings)
}
