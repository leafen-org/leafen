package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// peopleRowPictureIcon is the icon a People row without one of its own
// shows: its person's picture, as Notion's People row's page icon is its
// person's photo — a relation's chip naming that row shows the photo, 17.76
// px rounded 4, and one without a photo the page icon (seen 2026-11-15 twice,
// a free workspace's owner in Chrome at 1440). It is read, not kept: the
// picture is whatever the person shows when the row is read, so a new photo
// reaches every place the row shows at once, and the app draws it from its
// list of people (PageIcon.tsx, "person:〈user〉"): a photo as a page's
// picture, anything else as the page icon. A row its owner gave an icon of
// its own keeps it.
const peopleRowPictureIcon = "person:"

// showPeopleRowPictures gives a read's People rows without an icon of their
// own their person's picture as their icon, for each person who has one.
func showPeopleRowPictures(app core.App, items []graphReadItem) error {
	bare := map[string][]int{}
	for index, item := range items {
		if item.Kind != graphReadKindRow {
			continue
		}
		if icon, _ := item.Value["icon"].(string); strings.TrimSpace(icon) != "" {
			continue
		}
		source, _ := item.Value["dataSourceId"].(string)
		bare[source] = append(bare[source], index)
	}
	if len(bare) == 0 {
		return nil
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(bare))
	for source := range bare {
		key := fmt.Sprintf("source%d", len(placeholders))
		params[key] = source
		placeholders = append(placeholders, "{:"+key+"}")
	}
	sources := []string{}
	if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_workspace_people_databases}}
		WHERE [[data_source_id]] IN (` + strings.Join(placeholders, ",") + `)`).Bind(params).Column(&sources); err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}
	rows := map[string]int{}
	params = dbx.Params{}
	placeholders = placeholders[:0]
	for _, source := range sources {
		for _, index := range bare[source] {
			id, _ := items[index].Value["id"].(string)
			key := fmt.Sprintf("row%d", len(placeholders))
			params[key] = id
			placeholders = append(placeholders, "{:"+key+"}")
			rows[id] = index
		}
	}
	people := []struct {
		RowID  string `db:"row_id"`
		UserID string `db:"user_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[person]].[[row_id]], [[person]].[[user_id]] FROM {{leafen_workspace_people_rows}} [[person]]
		JOIN {{users}} [[user]] ON [[user]].[[id]] = [[person]].[[user_id]]
		WHERE [[person]].[[row_id]] IN (` + strings.Join(placeholders, ",") + `) AND TRIM([[user]].[[profile_avatar]]) != ''`).
		Bind(params).All(&people); err != nil {
		return err
	}
	for _, person := range people {
		item := items[rows[person.RowID]]
		item.Value["icon"], item.Value["iconType"] = peopleRowPictureIcon+person.UserID, "image"
	}
	return nil
}

// peopleRowPictureOf is the icon a People row without one of its own shows
// where its person is known, as showPeopleRowPictures gives a read's rows:
// their picture, "" for a person without a photo — a favourite person in the
// sidebar and the library shows their photo, as Notion's (seen 2026-11-16, a
// free workspace's owner at 1440: the photo, 18px rounded 4).
func peopleRowPictureOf(app core.App, userID string) (string, error) {
	var avatar string
	if err := app.DB().NewQuery(`SELECT [[profile_avatar]] FROM {{users}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": userID}).Row(&avatar); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if strings.TrimSpace(avatar) == "" {
		return "", nil
	}
	return peopleRowPictureIcon + userID, nil
}
