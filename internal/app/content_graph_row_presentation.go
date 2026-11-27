package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A row a content change writes (upsertRow) is its place and its values. Its
// icon, cover, full width and small text are its page's, changed on the page
// (PUT /database-rows/{rowId}/page/presentation); a row read and sent back
// may carry them as it reads, and they are left as they are
// (normalizeGraphRow). One carrying others is refused, naming the field, so
// none is dropped unsaid: a new row's are its page's defaults. What a read
// tells of a row besides — when and by whom it was made and edited, its trash
// stamps, whether its page holds content, and what the reader may do with
// it — is the server's to keep, and a row sent back leaves it so. The web
// sends a row without them (web lib/graphRowWire.ts).

// refuseRowPresentationChanges refuses a person's change whose rows carry an
// icon, cover or style other than the row's own, unless the change was
// taken already (its replay answers as it did).
func refuseRowPresentationChanges(app core.App, pageID string, input graphMutationInput) error {
	carried := map[string]bool{}
	ids := []string{}
	for _, operation := range input.Operations {
		row := operation.Row
		if operation.Kind != graphOperationKindUpsertRow || row == nil {
			continue
		}
		if row.Icon == "" && row.IconType == "" && row.Cover == "" && row.CoverPosition == 0 && !row.FullWidth && !row.SmallText {
			continue
		}
		id := strings.TrimSpace(row.ID)
		if !carried[id] {
			carried[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	current, err := graphRowPresentations(app, ids)
	if err != nil {
		return err
	}
	for index, operation := range input.Operations {
		row := operation.Row
		if operation.Kind != graphOperationKindUpsertRow || row == nil {
			continue
		}
		id := strings.TrimSpace(row.ID)
		if !carried[id] {
			continue
		}
		field := graphRowPresentationDifference(*row, current[id])
		if field == "" {
			continue
		}
		taken := 0
		if err := app.DB().NewQuery(`SELECT EXISTS(SELECT 1 FROM {{leafen_graph_mutations}}
			WHERE [[page_id]] = {:page} AND [[mutation_id]] = {:mutation})`).
			Bind(dbx.Params{"page": pageID, "mutation": strings.TrimSpace(input.MutationID)}).Row(&taken); err != nil {
			return err
		}
		if taken == 1 {
			return nil
		}
		return graphRefused(errorCodeGraphInvalid, "Change a row's icon, cover or style on its page.",
			"operations[%d].row.%s isn't the row's own: a row's icon, cover, full width and small text change with "+
				"PUT /database-rows/{rowId}/page/presentation, and a row sent back carries them as it reads or leaves them out",
			index, field)
	}
	return nil
}

// graphRowPresentation is a row's icon, cover and style as a read gives them.
type graphRowPresentation struct {
	Icon, IconType, Cover string
	CoverPosition         float64
	FullWidth, SmallText  bool
}

// graphRowPresentations reads rows' icons, covers and styles as the graph's
// read gives them (graphRowPayloadSQL), a People row's picture as its icon.
// A row not made yet has its page's defaults.
func graphRowPresentations(app core.App, ids []string) (map[string]graphRowPresentation, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	stored := []struct {
		Payload string `db:"payload"`
	}{}
	if err := app.DB().NewQuery(`SELECT ` + graphRowPayloadColumn + ` AS [[payload]] ` + graphRowPayloadFrom + `
		WHERE [[row]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).Bind(dbx.Params{"ids": string(encoded)}).All(&stored); err != nil {
		return nil, err
	}
	items := make([]graphReadItem, 0, len(stored))
	for _, row := range stored {
		value := map[string]any{}
		if err := json.Unmarshal([]byte(row.Payload), &value); err != nil {
			return nil, err
		}
		items = append(items, graphReadItem{Kind: graphReadKindRow, Value: value})
	}
	if err := showPeopleRowPictures(app, items); err != nil {
		return nil, err
	}
	presentations := map[string]graphRowPresentation{}
	for _, id := range ids {
		presentations[id] = graphRowPresentation{IconType: graphDataSourceIconNone}
	}
	for _, item := range items {
		id, _ := item.Value["id"].(string)
		presentation := graphRowPresentation{}
		presentation.Icon, _ = item.Value["icon"].(string)
		presentation.IconType, _ = item.Value["iconType"].(string)
		presentation.Cover, _ = item.Value["cover"].(string)
		presentation.CoverPosition, _ = item.Value["coverPosition"].(float64)
		presentation.FullWidth, _ = item.Value["fullWidth"].(bool)
		presentation.SmallText, _ = item.Value["smallText"].(bool)
		presentations[id] = presentation
	}
	return presentations, nil
}

// graphRowPresentationDifference names the first icon, cover or style field
// a row carries other than the row's own; one it leaves out is none.
func graphRowPresentationDifference(row graphDatabaseRow, current graphRowPresentation) string {
	switch {
	case row.Icon != "" && row.Icon != current.Icon:
		return "icon"
	case row.IconType != "" && row.IconType != current.IconType:
		return "iconType"
	case row.Cover != "" && row.Cover != current.Cover:
		return "cover"
	case row.CoverPosition != 0 && float64(row.CoverPosition) != current.CoverPosition:
		return "coverPosition"
	case row.FullWidth && !current.FullWidth:
		return "fullWidth"
	case row.SmallText && !current.SmallText:
		return "smallText"
	}
	return ""
}
