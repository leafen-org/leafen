package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database item's tree is the item and every sub-item under it, read a
// level at a time, parents before their sub-items and each level in the
// items' order. A sub-item met again is not followed twice, so the walk
// ends whatever the stored parent links hold; no depth or count stops it.

type databaseRowTreeRow struct {
	ID       string `db:"id"`
	ParentID string `db:"subitem_parent_id"`
	Position string `db:"position"`
	Depth    int    `db:"-"`
}

// databaseRowTreeRows reads the item's tree. With skipTrashed, a sub-item in
// trash is left out with the sub-items under it.
func databaseRowTreeRows(
	app core.App, authority databaseRowPageAuthority, skipTrashed bool,
) ([]databaseRowTreeRow, error) {
	root := databaseRowTreeRow{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[subitem_parent_id]], [[position]] FROM {{leafen_database_rows}}
		WHERE [[id]] = {:rowId} AND [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[data_source_id]] = {:sourceId}
	`).Bind(dbx.Params{
		"rowId": authority.RowID, "owner": authority.OwnerID,
		"pageId": authority.ContainerPageID, "sourceId": authority.DataSourceID,
	}).One(&root)
	if err != nil {
		return nil, err
	}
	trashed := ""
	if skipTrashed {
		trashed = ` AND ` + databaseRowNotTrashedSQL("[[id]]")
	}
	tree := []databaseRowTreeRow{root}
	seen := map[string]struct{}{root.ID: {}}
	level := []string{root.ID}
	for depth := 1; len(level) > 0; depth++ {
		parents, err := json.Marshal(level)
		if err != nil {
			return nil, err
		}
		children := []databaseRowTreeRow{}
		// The index that keeps a source's rows by their parent is named:
		// SQLite's own choice read the source's every row in order.
		if err := app.DB().NewQuery(`
			SELECT [[id]], [[subitem_parent_id]], [[position]] FROM {{leafen_database_rows}}
			INDEXED BY [[idx_leafen_database_rows_subitem_order]]
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:sourceId}
				AND [[subitem_parent_id]] IN (SELECT [[value]] FROM json_each({:parents}))` + trashed + `
			ORDER BY [[position]], [[id]]
		`).Bind(dbx.Params{
			"owner": authority.OwnerID, "pageId": authority.ContainerPageID,
			"sourceId": authority.DataSourceID, "parents": string(parents),
		}).All(&children); err != nil {
			return nil, err
		}
		level = level[:0]
		for _, child := range children {
			if _, again := seen[child.ID]; again {
				continue
			}
			seen[child.ID] = struct{}{}
			child.Depth = depth
			tree = append(tree, child)
			level = append(level, child.ID)
		}
	}
	return tree, nil
}

// databaseRowTreeRead reads the tree for an action, refusing an item that is
// gone as a conflict, the way the actions answer a changed item.
func databaseRowTreeRead(
	app core.App, authority databaseRowPageAuthority, skipTrashed bool,
) ([]databaseRowTreeRow, error) {
	tree, err := databaseRowTreeRows(app, authority, skipTrashed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errDatabaseRowPageConflict
	}
	return tree, err
}

// storedRowParent is a row's sub-item parent as kept, for a change to its
// values that leaves its place as it is: a row's change names its parent.
func storedRowParent(app core.App, rowID string) *string {
	var parent string
	if err := app.DB().NewQuery(`SELECT [[subitem_parent_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": rowID}).Row(&parent); err != nil || parent == "" {
		return nil
	}
	return &parent
}
