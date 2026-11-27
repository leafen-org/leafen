package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// graphRowPayloadColumn is a graph row's payload, read from the row aliased
// row and its page records (graphRowPayloadFrom). A row whose database page
// is locked says so; the reader who unlocked it for themselves has that
// taken off (markRowLocksUnlockedFor). A row whose page is full width or in
// small text says so, as its icon and cover, for the screen a public read
// shows while that page opens to lay its head as the page will
// (web lib/publicOpeningShape.ts, as public_opening_shape.go says a page's);
// each is left out while unset (json_patch drops a null). Whether its page's body holds any
// block is what its icon shows without an icon of its own, as a page's does.
// A Notion person or integration a migration kept stands as its maker, and
// as its last editor while it is as the migration left it, for the account
// its record keeps (notion_imported_authors.go).
const graphRowPayloadColumn = `json_patch(json_object('id', [[row]].[[id]], 'dataSourceId', [[row]].[[data_source_id]],
					'subitemParentId', NULLIF([[row]].[[subitem_parent_id]], ''),
					'position', [[row]].[[position]], 'icon', COALESCE([[row_record]].[[icon]], ''),
					'iconType', COALESCE([[row_record]].[[icon_type]], 'none'),
					'cover', COALESCE([[row_record]].[[cover]], ''),
					'coverPosition', COALESCE([[row_record]].[[cover_position]], 0),
					'archivedAt', COALESCE([[row_record]].[[archived_at]], ''),
					'archivedBy', COALESCE([[row_record]].[[archived_by]], ''),
					'createdAt', strftime('%Y-%m-%dT%H:%M:%fZ', COALESCE([[page]].[[created]], [[row]].[[created]]) / 1000.0, 'unixepoch'),
					'createdBy', COALESCE((SELECT NULLIF([[imported]].[[created_source]], '') FROM {{leafen_notion_imported_authors}} [[imported]]
						WHERE [[imported]].[[page_id]] = [[row]].[[id]]), [[row_record]].[[created_by]], ''),
					'updatedAt', strftime('%Y-%m-%dT%H:%M:%fZ', COALESCE([[page]].[[updated]], [[row]].[[updated]]) / 1000.0, 'unixepoch'),
					'lastEditedBy', COALESCE((SELECT NULLIF([[imported]].[[edited_source]], '') FROM {{leafen_notion_imported_authors}} [[imported]]
						WHERE [[imported]].[[page_id]] = [[row]].[[id]] AND [[imported]].[[edited_at]] = COALESCE([[page]].[[updated]], [[row]].[[updated]])),
						[[row_record]].[[last_edited_by]], ''),
					'hasContent', json(CASE WHEN EXISTS (
						SELECT 1 FROM {{leafen_blocks}} [[content_block]] INDEXED BY [[idx_leafen_blocks_page_order]]
						WHERE [[content_block]].[[owner]] = [[row]].[[owner]] AND [[content_block]].[[page_id]] = [[row]].[[id]]
					) THEN 'true' ELSE 'false' END)),
					json_object('lockedForMe', CASE WHEN COALESCE([[row_record]].[[is_locked]], FALSE) THEN json('true') END,
						'fullWidth', CASE WHEN COALESCE([[row_record]].[[full_width]], FALSE) THEN json('true') END,
						'smallText', CASE WHEN COALESCE([[row_record]].[[small_text]], FALSE) THEN json('true') END))`

// graphRowPayloadFrom is the row and its page records a row's payload reads.
const graphRowPayloadFrom = `FROM {{leafen_database_rows}} [[row]]
			LEFT JOIN {{leafen_database_row_pages}} [[page]] ON [[page]].[[row_id]]=[[row]].[[id]]
				AND [[page]].[[owner]]=[[row]].[[owner]] AND [[page]].[[workspace_id]]=[[row]].[[workspace_id]]
			LEFT JOIN {{leafen_pages}} [[row_record]] ON [[row_record]].[[id]]=[[row]].[[id]]`

// graphRowPayloadSQL is a row's payload with what it reads.
const graphRowPayloadSQL = graphRowPayloadColumn + "\n" + graphRowPayloadFrom

// graphReadItemsWithRowValues decodes stored graph items and gives each row
// its values.
func graphReadItemsWithRowValues(app core.App, owner string, stored []storedGraphReadItem) ([]graphReadItem, error) {
	items := make([]graphReadItem, 0, len(stored))
	rowValues := map[string]map[string]any{}
	rowIDs := make([]string, 0)
	for _, item := range stored {
		var value map[string]any
		if err := json.Unmarshal([]byte(item.Payload), &value); err != nil {
			return nil, err
		}
		if item.Kind == graphReadKindRow {
			rowID, _ := value["id"].(string)
			rowIDs = append(rowIDs, rowID)
			rowValues[rowID] = map[string]any{}
		}
		// A property stored in older keys reads in the documented ones
		// (database_property_config_keys.go).
		if item.Kind == graphReadKindProperty {
			canonicalGraphReadPropertyConfig(value)
		}
		items = append(items, graphReadItem{Kind: item.Kind, Value: value})
	}
	if err := projectTimelineDependencies(app, owner, items); err != nil {
		return nil, err
	}
	if err := showPeopleRowPictures(app, items); err != nil {
		return nil, err
	}
	if len(rowIDs) == 0 {
		return items, nil
	}
	storedValues, err := loadGraphRowValuesByIDs(app, owner, rowIDs)
	if err != nil {
		return nil, err
	}
	if err := hideTrashedRelationTargets(app, storedValues); err != nil {
		return nil, err
	}
	for _, storedValue := range storedValues {
		var value any
		if err := json.Unmarshal([]byte(storedValue.Value), &value); err != nil {
			return nil, err
		}
		cell := map[string]any{"type": storedValue.Type, "value": value}
		if spans := databaseStoredSpans(storedValue.Spans); len(spans) > 0 {
			cell["spans"] = spans
		}
		rowValues[storedValue.RowID][storedValue.PropertyID] = cell
	}
	for index := range items {
		if items[index].Kind == graphReadKindRow {
			rowID, _ := items[index].Value["id"].(string)
			items[index].Value["values"] = rowValues[rowID]
		}
	}
	return items, nil
}

func loadGraphRowValues(app core.App, owner, pageID string, rowIDs []string) ([]storedGraphRowValue, error) {
	return loadGraphRowValuesWithPageFilter(app, owner, pageID, rowIDs)
}

func loadGraphRowValuesByIDs(app core.App, owner string, rowIDs []string) ([]storedGraphRowValue, error) {
	return loadGraphRowValuesWithPageFilter(app, owner, "", rowIDs)
}

func loadGraphRowValuesWithPageFilter(
	app core.App, owner, pageID string, rowIDs []string,
) ([]storedGraphRowValue, error) {
	sort.Strings(rowIDs)
	params := dbx.Params{"owner": owner}
	placeholders := make([]string, len(rowIDs))
	for index, rowID := range rowIDs {
		key := fmt.Sprintf("row%d", index)
		params[key] = rowID
		placeholders[index] = "{:" + key + "}"
	}
	// Without a page the rows' own key leads: an owner alone let SQLite read
	// the values of every database of the workspace for each window of rows
	// (2026-11-11: some 150 ms a window beside 100,000 rows, the page drawn
	// minutes after its import).
	ownerTerm, pageClause := "+[[owner]] = {:owner}", ""
	if pageID != "" {
		params["pageId"] = pageID
		ownerTerm, pageClause = "[[owner]] = {:owner}", " AND [[page_id]] = {:pageId}"
	}
	query := fmt.Sprintf(`
		SELECT [[row_id]], [[property_id]], [[type]], [[value]], [[spans]]
		FROM {{leafen_database_row_values}}
		WHERE %s%s AND [[row_id]] IN (%s)
		ORDER BY [[row_id]], [[property_id]]
	`, ownerTerm, pageClause, strings.Join(placeholders, ", "))
	var values []storedGraphRowValue
	err := app.DB().NewQuery(query).Bind(params).All(&values)
	return values, err
}

func encodeGraphCursor(sortKey string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(sortKey))
}

func decodeGraphCursor(cursor string) (string, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(decoded) == 0 || len(decoded) > maxGraphCursorBytes {
		return "", errors.New(errorCodeGraphCursorInvalid)
	}
	// An earlier server's cursor, its keys joined by colons, is refused.
	if _, err := parseGraphWindowCursor(string(decoded)); err != nil {
		return "", err
	}
	return string(decoded), nil
}
