package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's history names the rows of its databases each change added,
// edited or removed, as Notion's "버전의 모든 변경 사항 보기" lists them (seen
// 2026-10-01, a temporary page's inline database): "입력 값 추가함" for a row
// made, "입력 값 편집함" with a line for each property changed ("Stage: A →
// B", "Flag Done: — → Yes"), and "입력 값 제거함" for a row taken away. A row
// page's own body is its own history's, not the page's.

const (
	pageHistoryChangeRowAdded   = "rowAdded"
	pageHistoryChangeRowEdited  = "rowEdited"
	pageHistoryChangeRowRemoved = "rowRemoved"
	// A row sent to the trash or back from it, which reads as removed or added
	// and whose webhook events go out with the trash's own.
	pageHistoryChangeRowTrashed  = "rowTrashed"
	pageHistoryChangeRowRestored = "rowRestored"
)

// pageHistoryRowChange is a row a change added, edited or removed, with the
// values it changed as they read then.
type pageHistoryRowChange struct {
	ID     string                          `json:"id"`
	Change string                          `json:"change"`
	Values []pageHistoryRowValueChangeWire `json:"values,omitempty"`
	// Before holds the values of Values as they were, for reverting the change;
	// one the row had none of is an empty value.
	Before map[string]pageHistoryRowValue `json:"before,omitempty"`
}

type pageHistoryCapturedRowValue struct {
	Type  string `db:"type"`
	Value string `db:"value"`
	Spans string `db:"spans"`
}

// pageHistoryRowCapture holds the rows a graph change writes as they were
// before it.
type pageHistoryRowCapture struct {
	pageID         string
	ids            []string
	before         map[string]map[string]pageHistoryCapturedRowValue
	activityBefore map[string]workspaceActivityRowState
}

// The rows a change touches outside its operations — a row put in the
// trash or back by the change's own step after them — are held too, so the
// workspace updates read their state before and after as any row's
// (2026-11-11: a row's trash and restore left no "삭제"·"복원").
func beginPageHistoryRowCapture(app core.App, pageID string, operations []graphOperation, also ...pageHistoryRowChange) (*pageHistoryRowCapture, error) {
	seen := map[string]struct{}{}
	ids := []string{}
	named := graphMutationRowIDs(operations)
	for _, change := range also {
		named = append(named, change.ID)
	}
	for _, id := range named {
		if _, done := seen[id]; !done {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	before, err := loadPageHistoryRowValues(app, pageID, ids)
	if err != nil {
		return nil, err
	}
	activityBefore, err := captureWorkspaceActivityRows(app, ids)
	if err != nil {
		return nil, err
	}
	return &pageHistoryRowCapture{pageID: pageID, ids: ids, before: before, activityBefore: activityBefore}, nil
}

// finish names the rows the change added, edited or removed, in the order
// it wrote them.
func (capture *pageHistoryRowCapture) finish(app core.App) ([]pageHistoryRowChange, error) {
	if capture == nil {
		return nil, nil
	}
	after, err := loadPageHistoryRowValues(app, capture.pageID, capture.ids)
	if err != nil {
		return nil, err
	}
	changes := []pageHistoryRowChange{}
	edited := map[string][]string{}
	for _, id := range capture.ids {
		was, existed := capture.before[id]
		now, exists := after[id]
		switch {
		case !existed && exists:
			changes = append(changes, pageHistoryRowChange{ID: id, Change: pageHistoryChangeRowAdded})
		case existed && !exists:
			changes = append(changes, pageHistoryRowChange{ID: id, Change: pageHistoryChangeRowRemoved})
		case existed && exists:
			properties := []string{}
			for propertyID := range pageHistoryRowValueKeys(was, now) {
				if was[propertyID] != now[propertyID] {
					properties = append(properties, propertyID)
				}
			}
			if len(properties) > 0 {
				edited[id] = properties
				changes = append(changes, pageHistoryRowChange{ID: id, Change: pageHistoryChangeRowEdited})
			}
		}
	}
	if len(edited) == 0 {
		return changes, nil
	}
	properties, err := loadPageHistoryRowProperties(app, capture.pageID)
	if err != nil {
		return nil, err
	}
	for index, change := range changes {
		ids, found := edited[change.ID]
		if !found {
			continue
		}
		sort.Slice(ids, func(left, right int) bool {
			return properties[ids[left]].Position < properties[ids[right]].Position ||
				properties[ids[left]].Position == properties[ids[right]].Position && ids[left] < ids[right]
		})
		for _, propertyID := range ids {
			property, known := properties[propertyID]
			if !known {
				continue
			}
			from := pageHistoryRowValueText(capture.before[change.ID][propertyID], property.Config)
			to := pageHistoryRowValueText(after[change.ID][propertyID], property.Config)
			if from == to {
				continue
			}
			changes[index].Values = append(changes[index].Values, pageHistoryRowValueChangeWire{
				PropertyID: propertyID, PropertyName: property.Name, Type: property.Type, From: from, To: to,
				FromSpans: pageHistoryRowValueMentionSpans(capture.before[change.ID][propertyID], from),
				ToSpans:   pageHistoryRowValueMentionSpans(after[change.ID][propertyID], to),
			})
			if changes[index].Before == nil {
				changes[index].Before = map[string]pageHistoryRowValue{}
			}
			changes[index].Before[propertyID] = pageHistoryRowValue{}
			if was, had := capture.before[change.ID][propertyID]; had {
				cell := pageHistoryRowValue{Type: was.Type, Value: json.RawMessage(was.Value)}
				if was.Spans != "" {
					cell.Spans = json.RawMessage(was.Spans)
				}
				changes[index].Before[propertyID] = cell
			}
		}
	}
	return changes, nil
}

func pageHistoryRowValueKeys(maps ...map[string]pageHistoryCapturedRowValue) map[string]struct{} {
	keys := map[string]struct{}{}
	for _, values := range maps {
		for key := range values {
			keys[key] = struct{}{}
		}
	}
	return keys
}

// loadPageHistoryRowValues reads the page's rows of the given IDs that
// exist, each with its stored values. The rows are found by their IDs, the
// page only checked: by the page first, every change of a big database read
// all its rows and values (2026-10-15: a 50,000-row CSV import, a turn of
// 50 rows took longer as the database grew, holding the one write
// connection while people's edits waited).
func loadPageHistoryRowValues(
	app core.App, pageID string, ids []string,
) (map[string]map[string]pageHistoryCapturedRowValue, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{"pageId": pageID, "rowIDs": string(encoded)}
	in := "SELECT value FROM json_each({:rowIDs})"
	rows := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_rows}} WHERE +[[page_id]]={:pageId} AND [[id]] IN (` + in + `)
	`).Bind(params).Column(&rows); err != nil {
		return nil, err
	}
	result := make(map[string]map[string]pageHistoryCapturedRowValue, len(rows))
	for _, id := range rows {
		result[id] = map[string]pageHistoryCapturedRowValue{}
	}
	values := []struct {
		RowID      string `db:"row_id"`
		PropertyID string `db:"property_id"`
		pageHistoryCapturedRowValue
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]],[[property_id]],[[type]],[[value]],[[spans]] FROM {{leafen_database_row_values}}
		WHERE +[[page_id]]={:pageId} AND [[row_id]] IN (` + in + `)
	`).Bind(params).All(&values); err != nil {
		return nil, err
	}
	for _, value := range values {
		if row, found := result[value.RowID]; found {
			row[value.PropertyID] = value.pageHistoryCapturedRowValue
		}
	}
	return result, nil
}

type pageHistoryRowProperty struct {
	Name     string `db:"name"`
	Type     string `db:"type"`
	Position string `db:"position"`
	Config   string `db:"config"`
}

// loadPageHistoryRowProperties reads the page's databases' properties as
// they are named now.
func loadPageHistoryRowProperties(app core.App, pageID string) (map[string]pageHistoryRowProperty, error) {
	rows := []struct {
		ID string `db:"id"`
		pageHistoryRowProperty
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[name]],[[type]],[[position]],[[config]] FROM {{leafen_database_properties}}
		WHERE [[page_id]]={:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).All(&rows); err != nil {
		return nil, err
	}
	properties := make(map[string]pageHistoryRowProperty, len(rows))
	for _, row := range rows {
		properties[row.ID] = row.pageHistoryRowProperty
	}
	return properties, nil
}

// pageHistoryRowPropertyIcon is the built-in icon a property was given, if any.
func pageHistoryRowPropertyIcon(config string) string {
	own := struct {
		Icon string `json:"icon"`
	}{}
	if json.Unmarshal([]byte(config), &own) != nil || !validNativeIconValue(own.Icon) {
		return ""
	}
	return own.Icon
}

// pageHistoryRowValueText is a value as the change's line reads it: text as
// written, numbers as digits, the names of chosen options, "Yes" for a
// checked box and a date's start and end; "" for none and for kinds the line
// doesn't spell.
func pageHistoryRowValueText(value pageHistoryCapturedRowValue, config string) string {
	switch value.Type {
	case graphValueTypeCheckbox:
		var checked bool
		if json.Unmarshal([]byte(value.Value), &checked) == nil && checked {
			return "Yes"
		}
		return ""
	case graphValueTypeDate:
		var date any
		if json.Unmarshal([]byte(value.Value), &date) != nil {
			return ""
		}
		return workspaceExportDateText(date)
	}
	return databaseRowSearchValueText(databaseRowSearchValue{Type: value.Type, Config: config, Value: value.Value})
}

// pageHistoryRowValueMentionSpans keeps, with a cell's words as its change
// line says them, its spans while they hold a mention and spell those words,
// for the line to say each mention as its reader reads it ("@〈name〉",
// historyChangesAsRead); none otherwise.
func pageHistoryRowValueMentionSpans(value pageHistoryCapturedRowValue, text string) []graphTextSpan {
	spans := databaseStoredSpans(value.Spans)
	if !querySpansHoldMention(spans) || spelledName(spans) != text {
		return nil
	}
	return spans
}
