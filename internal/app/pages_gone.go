package app

import (
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const dataRestoredAtStartStoreKey = "leafen.dataRestoredAtStart"

// DataRestoredAtStart says the server's data came in another history as it
// started: a restore, its undo, or one the command line made.
func DataRestoredAtStart(app core.App) bool {
	return app.Store().Has(dataRestoredAtStartStoreKey)
}

// PagesGone are those of the addresses' ids — a page's, or an inline
// database's block — the server's data holds no more: after a restore took
// them away, the app's tabs kept on them open home
// (desktop/window_tabs_restored.go).
func PagesGone(app core.App, ids []string) (map[string]bool, error) {
	gone := map[string]bool{}
	if len(ids) == 0 {
		return gone, nil
	}
	params := dbx.Params{}
	names := make([]string, 0, len(ids))
	for index, id := range ids {
		name := "id" + strconv.Itoa(index)
		params[name] = id
		names = append(names, "{:"+name+"}")
		gone[id] = true
	}
	found := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_pages}} WHERE [[id]] IN (` + strings.Join(names, ", ") + `)
		UNION SELECT [[id]] FROM {{leafen_blocks}} WHERE [[id]] IN (` + strings.Join(names, ", ") + `)`).Bind(params).Column(&found); err != nil {
		return nil, err
	}
	for _, id := range found {
		delete(gone, id)
	}
	return gone, nil
}
