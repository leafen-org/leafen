package app

import (
	"database/sql"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A notice about a database item — a person property naming someone, a
// comment on the item's page, a mention or a reminder in its text, a change
// of it, a deadline of it — names the item, not the page holding its
// database: Notion's inbox says "〈사람〉 다음에서 나를 멘션함 〈item〉" and
// "〈사람〉님이 〈item〉에 댓글을 달았습니다." (seen 2026-11-09, a guest). It
// names the item as it is called now, and a person property's notice says
// that property's line as it stands now, its name and the people in it, as
// "Owner: @Ada" (seen 2026-11-09: the item renamed, the property
// renamed and a person taken out all showed at once). The item is the one
// the notice's address names (notificationItemOf); a notice another's name
// heads, an automation's, keeps its own.

// databasePersonActivityPrefix heads a person property's notice's key,
// "database-person:<item>:<property>:<change>".
const databasePersonActivityPrefix = "database-person:"

// noticeNamesItsItem is whether a notice names what it is about by its
// title: every one but an automation's, which its sender's name heads.
func noticeNamesItsItem(activityKey string) bool {
	return !strings.HasPrefix(activityKey, automationNotificationActivityPrefix)
}

// noticeItemsNow names each notice's item as it is called now, and puts a
// person property's notice's line as it stands now.
func noticeItemsNow(app core.App, notices []noticeNow) {
	wanted := map[string][]int{}
	ids := []any{}
	for index, notice := range notices {
		if !noticeNamesItsItem(notice.ActivityKey) {
			continue
		}
		item := notificationItemOf(notice.Target)
		if item == "" {
			continue
		}
		if _, seen := wanted[item]; !seen {
			ids = append(ids, item)
		}
		wanted[item] = append(wanted[item], index)
	}
	if len(ids) == 0 {
		return
	}
	rows := []struct {
		ID    string         `db:"id"`
		Title sql.NullString `db:"title"`
	}{}
	if app.DB().Select("row.id", "value.value AS title").From("leafen_database_rows row").
		LeftJoin("leafen_database_row_values value", dbx.NewExp("[[value.row_id]] = [[row.id]] AND [[value.type]] = 'title'")).
		Where(dbx.In("row.id", ids...)).All(&rows) != nil {
		return
	}
	for _, row := range rows {
		title := databaseRowTitleText(row.Title.String)
		for _, index := range wanted[row.ID] {
			*notices[index].ItemID, *notices[index].Title = row.ID, title
		}
	}
	databasePersonLinesNow(app, notices)
}

// databasePersonLinesNow puts each person property's notice's line as the
// property stands now; one whose property or item is gone keeps its words.
func databasePersonLinesNow(app core.App, notices []noticeNow) {
	type wantedLine struct{ row, property string }
	wanted := map[wantedLine][]int{}
	properties, rows := []any{}, []any{}
	for index, notice := range notices {
		rest, found := strings.CutPrefix(notice.ActivityKey, databasePersonActivityPrefix)
		parts := strings.Split(rest, ":")
		if !found || len(parts) < 3 || *notice.ItemID != parts[0] {
			continue
		}
		key := wantedLine{row: parts[0], property: parts[1]}
		if _, seen := wanted[key]; !seen {
			rows, properties = append(rows, key.row), append(properties, key.property)
		}
		wanted[key] = append(wanted[key], index)
	}
	if len(wanted) == 0 {
		return
	}
	stored := []struct {
		RowID      string         `db:"row_id"`
		PropertyID string         `db:"property_id"`
		Name       string         `db:"name"`
		Value      sql.NullString `db:"value"`
	}{}
	if app.DB().Select("row.id AS row_id", "property.id AS property_id", "property.name", "value.value").
		From("leafen_database_rows row").
		InnerJoin("leafen_database_properties property", dbx.NewExp("[[property.data_source_id]] = [[row.data_source_id]]")).
		LeftJoin("leafen_database_row_values value", dbx.NewExp("[[value.row_id]] = [[row.id]] AND [[value.property_id]] = [[property.id]]")).
		Where(dbx.In("row.id", rows...)).AndWhere(dbx.In("property.id", properties...)).All(&stored) != nil {
		return
	}
	for _, line := range stored {
		indexes := wanted[wantedLine{row: line.RowID, property: line.PropertyID}]
		if len(indexes) == 0 {
			continue
		}
		words, spans := databasePersonLine(app, line.Name, peopleInValue([]byte(line.Value.String)))
		for _, index := range indexes {
			*notices[index].Preview, *notices[index].Spans = words, spans
		}
	}
}

// databasePersonLine is a person property's line in a notice: its name,
// then each person it names as a mention, "Owner: @Ada, @Grace", in
// words and in spans.
func databasePersonLine(app core.App, propertyName string, people []string) (string, string) {
	spans := []graphTextSpan{{Text: strings.TrimSpace(propertyName) + ": "}}
	names := map[string]string{}
	if ids := personIDsAsAny(people); len(ids) > 0 {
		found := []struct {
			ID   string `db:"id"`
			Name string `db:"name"`
		}{}
		_ = app.DB().Select("id", accountNameSQL("users")+" AS name").From("users").Where(dbx.In("id", ids...)).All(&found)
		for _, person := range found {
			names[person.ID] = person.Name
		}
	}
	for index, userID := range people {
		if index > 0 {
			spans = append(spans, graphTextSpan{Text: ", "})
		}
		spans = append(spans, graphTextSpan{Text: "@" + names[userID], Mention: "person", UserID: userID})
	}
	words := strings.Builder{}
	for _, span := range spans {
		words.WriteString(span.Text)
	}
	return notificationLinePlain(words.String()), encodeNotificationLineSpans(spans)
}

func personIDsAsAny(people []string) []any {
	ids := make([]any, 0, len(people))
	for _, id := range people {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
