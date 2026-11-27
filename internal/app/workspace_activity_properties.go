package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a person's edit of a database page changed of its properties, as
// Notion's entry lists them under the edit (seen 2026-10-18: "〈속성〉 A →
// B"). A database page's history keeps its values with its state, a version
// each change (database_row_page_history.go): the person's versions of it in
// the group's ten minutes say what changed — its values before the first and
// after the last, spelled as the page's history spells them. Its title is one,
// "Aa 〈its name〉" over the new title (workspace_activity_title_change.go). A
// text value keeps its formatting.
// They are its record's update, apart from its body's, as Notion's
// (workspaceActivityRowPart).

// workspaceActivityPropertySlack is how far around a group's times its page's
// versions are looked for: the database's change and the page's version are
// written apart.
const workspaceActivityPropertySlack = 2000

// workspaceActivityValue is a value as stored, sent as it is.
type workspaceActivityValue = json.RawMessage

var (
	// The kinds whose value is words, shown by them.
	workspaceActivityTextTypes = map[string]bool{graphValueTypeTitle: true, graphValueTypeRichText: true, graphValueTypeURL: true,
		graphValueTypeEmail: true, graphValueTypePhone: true}
	// The kinds whose value is drawn by the property's own options or format.
	workspaceActivityFormattedTypes = map[string]bool{graphValueTypeSelect: true, graphValueTypeStatus: true,
		graphValueTypeMultiSelect: true, graphValueTypeNumber: true, graphValueTypeDate: true}
)

func workspaceActivityPropertyChanges(app core.App, readerID, pageID string, row storedWorkspaceActivity) ([]workspaceActivityChange, error) {
	var container string
	err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:row}`).Bind(dbx.Params{"row": pageID}).Row(&container)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	versions := []struct {
		Cursor  int    `db:"version_cursor"`
		ActorID string `db:"actor_id"`
		Source  string `db:"source"`
		Created int64  `db:"created_at"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[version_cursor]],[[actor_id]],[[source]],[[created_at]]
		FROM {{leafen_page_history_versions}} INDEXED BY [[idx_leafen_page_history_versions_page_frontier]]
		WHERE [[page_id]] = {:page} ORDER BY [[version_cursor]] DESC LIMIT {:limit}`).
		Bind(dbx.Params{"page": pageID, "limit": workspaceActivityChangeVersions}).All(&versions); err != nil {
		return nil, err
	}
	first, last := 0, 0
	for _, version := range versions {
		if version.ActorID != row.ActorID || version.Source != pageHistoryEventMetadata ||
			version.Created < row.Created-workspaceActivityPropertySlack || version.Created > row.LastAt+workspaceActivityPropertySlack {
			continue
		}
		if first == 0 || version.Cursor < first {
			first = version.Cursor
		}
		last = max(last, version.Cursor)
	}
	start, err := pageHistoryStartCursor(app, pageID)
	if err != nil || first == 0 || first-1 < start {
		return nil, err
	}
	before, err := readPageHistorySnapshotPageState(app, pageID, first-1)
	if err != nil {
		return nil, err
	}
	after, err := readPageHistorySnapshotPageState(app, pageID, last)
	if err != nil {
		return nil, err
	}
	// Edits made while it was a page, before a database took it in, are a
	// page's: its new title, as Notion's line for a page a wiki took in keeps
	// "Aa 제목" (seen 2026-11-17); its making there is none of them
	// (pageHistoryVersionRowMade).
	if after.Values == nil {
		title, found, err := workspaceActivityTitleChange(app, pageID, row)
		if err != nil || !found {
			return nil, err
		}
		return []workspaceActivityChange{title}, nil
	}
	properties, err := loadPageHistoryRowProperties(app, container)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for id := range properties {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool {
		return properties[ids[left]].Position < properties[ids[right]].Position ||
			properties[ids[left]].Position == properties[ids[right]].Position && ids[left] < ids[right]
	})
	access, err := newPageHistoryPropertyAccess(app, readerID, ids)
	if err != nil {
		return nil, err
	}
	changes := []workspaceActivityChange{}
	text := func(values map[string]pageHistoryRowValue, id string) string {
		value := values[id]
		return pageHistoryRowValueText(pageHistoryCapturedRowValue{Type: value.Type, Value: string(value.Value), Spans: string(value.Spans)}, properties[id].Config)
	}
	for _, id := range ids {
		from, to := text(before.Values, id), text(after.Values, id)
		if from == to {
			continue
		}
		if hidden, err := access.hidesValue(pageID, id, after.Values); err != nil {
			return nil, err
		} else if hidden {
			continue
		}
		change := workspaceActivityChange{Kind: workspaceActivityChangeProperty, BlockID: id,
			Text: workspaceActivityClip(properties[id].Name), Before: workspaceActivityClip(from), After: workspaceActivityClip(to),
			PropertyType: properties[id].Type, PropertyIcon: pageHistoryRowPropertyIcon(properties[id].Config)}
		// The value shown — the new one, or the old one struck when it was
		// cleared — keeps its marks, equations and mentions, as Notion's
		// (seen 2026-11-13), while its spans spell it whole.
		shown, values := to, after.Values
		if to == "" {
			shown, values = from, before.Values
		}
		// And it is drawn as the page's updates tab draws it, as its cell
		// shows it — an option as its chip, a number in its format
		// (PageHistoryProperties HistoryValue) — by its value and the
		// property's formats, one it replaced before it ("완료 › 진행 중",
		// Notion's both, seen 2026-11-14); a text by its words, an added one
		// marked.
		if !workspaceActivityTextTypes[properties[id].Type] {
			change.Value = workspaceActivityValue(values[id].Value)
			if from != "" && to != "" {
				change.PreviousValue = workspaceActivityValue(before.Values[id].Value)
			}
			if workspaceActivityFormattedTypes[properties[id].Type] && json.Valid([]byte(properties[id].Config)) {
				change.PropertyConfig = workspaceActivityValue(properties[id].Config)
			}
		}
		if spans := databaseStoredSpans(string(values[id].Spans)); len(spans) > 0 && spelledName(spans) == shown &&
			workspaceActivityClip(shown) == shown {
			change.Spans = spans
		}
		changes = append(changes, change)
	}
	return changes, nil
}
