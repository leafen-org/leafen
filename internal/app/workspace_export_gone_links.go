package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What an archive carries of a database out of everyone's sight — in its 30
// days after a permanent deletion, being deleted, or gone for good
// (page_deletion_hold.go): nothing, and nothing that names it, as its purge
// leaves it (page_permanent_delete_cleanup_lanes.go) and as a copy into
// another workspace leaves what it doesn't take
// (page_duplication_workspaces.go). A database that shows it loses the
// member it links from it and that member's views, as the live graph's
// does (goneDatabaseLinks.remaining): one of its own sources or other linked
// ones shows those, a linked one becoming a linked database of the first
// left; one with nothing left — the empty link the screen keeps, for which no
// archive could carry a source — goes with its block and its views, the
// comments on its block staying on its page as the page's own. A relation to
// it goes, with the rollups through it and what the views, templates and
// rows of their database name of them; a relation keeps only the rows the
// archive holds. An archive made in the 30 days is then the one made after
// the purge, and both come back. Markdown & CSV and HTML show what the
// screen shows, in the 30 days as after: a database without the members out
// of sight, an empty link not at all, the relation and its rollups with none
// of its rows.

type workspaceExportGoneLinks struct {
	// What is out of sight, judged as the live graph judges it.
	*goneDatabaseLinks
	// archive leaves out the relations to it and what names them.
	archive    bool
	rowCarried map[string]bool
	leftOut    map[string]map[string]bool
	dropped    map[string]bool
}

func newWorkspaceExportGoneLinks(app core.App, workspaceID string, archive bool) *workspaceExportGoneLinks {
	return &workspaceExportGoneLinks{
		goneDatabaseLinks: newGoneDatabaseLinks(app, workspaceID), archive: archive, rowCarried: map[string]bool{},
		leftOut: map[string]map[string]bool{}, dropped: map[string]bool{},
	}
}

// withoutWorkspaceExportGoneLinks takes out of an export's window what
// names a database out of sight.
func withoutWorkspaceExportGoneLinks(
	app core.App, workspaceID string, archive bool, items []workspaceExportInventoryItem,
) ([]workspaceExportInventoryItem, error) {
	links := newWorkspaceExportGoneLinks(app, workspaceID, archive)
	kept := items[:0]
	for _, item := range items {
		keep, err := links.keep(item)
		if err != nil {
			return nil, err
		}
		if keep {
			kept = append(kept, item)
		}
	}
	return kept, nil
}

func (links *workspaceExportGoneLinks) keep(item workspaceExportInventoryItem) (bool, error) {
	value := item.Value
	switch item.Kind {
	case workspaceExportItemBlock, workspaceExportItemRowPageBlock:
		if value["type"] != graphBlockTypeInlineDatabase {
			return true, nil
		}
		id, _ := value["id"].(string)
		content, _ := value["content"].(map[string]any)
		next, keep, err := links.database(id, content)
		if keep && next != nil {
			value["content"] = next
		}
		return keep, err
	case workspaceExportItemDatabaseContainer:
		return links.container(value)
	case workspaceExportItemBlockTrash, workspaceExportItemRowBlockTrash:
		return links.trashedBlocks(value)
	case workspaceExportItemView:
		dataSourceID, _ := value["dataSourceId"].(string)
		containerID, _ := value["containerId"].(string)
		if seen, err := links.sourceInSight(dataSourceID); err != nil || !seen {
			return false, err
		}
		if containerID != "" {
			if dropped, err := links.databaseDropped(containerID); err != nil || dropped {
				return false, err
			}
		}
		return true, links.viewConfig(value, dataSourceID)
	case workspaceExportItemProperty:
		dataSourceID, _ := value["dataSourceId"].(string)
		id, _ := value["id"].(string)
		left, err := links.leftOutProperties(dataSourceID)
		return !left[id], err
	case workspaceExportItemRow:
		dataSourceID, _ := value["dataSourceId"].(string)
		values, _ := value["values"].(map[string]any)
		return true, links.cells(dataSourceID, values)
	case workspaceExportItemTemplate:
		dataSourceID, _ := value["dataSourceId"].(string)
		values, _ := value["properties"].(map[string]any)
		return true, links.cells(dataSourceID, values)
	case workspaceExportItemComment:
		blockID, _ := value["blockId"].(string)
		if blockID == "" {
			return true, nil
		}
		dropped, err := links.databaseDropped(blockID)
		if dropped {
			value["blockId"] = ""
		}
		return true, err
	}
	return true, nil
}

func (links *workspaceExportGoneLinks) sourceInSight(id string) (bool, error) {
	err := links.load([]string{id})
	return links.inSight[id], err
}

// database is what a database block keeps, as the live graph shows it
// (goneDatabaseLinks.remaining): its content without the members out of
// sight (nil when unchanged), or none when it is an empty link.
func (links *workspaceExportGoneLinks) database(id string, content map[string]any) (map[string]any, bool, error) {
	if dropped, known := links.dropped[id]; known && dropped {
		return nil, false, nil
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, false, err
	}
	parsed, err := parseDatabaseContainerBlockContent(encoded)
	if err != nil || len(parsed.memberDataSourceIDs()) == 0 {
		return nil, true, nil
	}
	own, kept, changed, err := links.remaining(parsed)
	if err != nil {
		return nil, false, err
	}
	empty := changed && len(own) == 0 && len(kept) == 0
	links.dropped[id] = empty
	if !changed || empty {
		return nil, !empty, nil
	}
	next := map[string]any{}
	if err := workspaceExportAsJSON(withDatabaseContainerMembers(content, own, kept), &next); err != nil {
		return nil, false, err
	}
	viewID, _ := next["viewId"].(string)
	if shown, err := links.viewOfKeptMember(viewID); err != nil {
		return nil, false, err
	} else if !shown {
		if next["viewId"], err = links.firstView(id); err != nil {
			return nil, false, err
		}
	}
	return next, true, nil
}

// databaseDropped says whether the database a block holds leaves the archive.
func (links *workspaceExportGoneLinks) databaseDropped(blockID string) (bool, error) {
	if dropped, known := links.dropped[blockID]; known {
		return dropped, nil
	}
	stored := []struct {
		Type    string `db:"type"`
		Content string `db:"content"`
	}{}
	if err := links.app.DB().NewQuery(`
		SELECT [[type]], [[content]] FROM {{leafen_blocks}} WHERE [[id]] = {:id} LIMIT 1
	`).Bind(dbx.Params{"id": blockID}).All(&stored); err != nil {
		return false, err
	}
	if len(stored) == 1 {
		content := map[string]any{}
		if stored[0].Type != graphBlockTypeInlineDatabase || json.Unmarshal([]byte(stored[0].Content), &content) != nil {
			links.dropped[blockID] = false
			return false, nil
		}
		_, keep, err := links.database(blockID, content)
		return !keep, err
	}
	// A linked database whose block is in the trash goes by its members, as
	// its block's content does: once none of them is left in sight.
	members := []string{}
	if err := links.app.DB().NewQuery(`
		SELECT [[membership]].[[data_source_id]] FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_database_container_sources}} AS [[membership]] ON [[membership]].[[container_id]] = [[container]].[[id]]
		WHERE [[container]].[[id]] = {:id} AND [[container]].[[kind]] = 'linked'
	`).Bind(dbx.Params{"id": blockID}).Column(&members); err != nil {
		return false, err
	}
	if err := links.load(members); err != nil {
		return false, err
	}
	dropped := len(members) > 0 && !slices.ContainsFunc(members, func(id string) bool { return links.inSight[id] })
	links.dropped[blockID] = dropped
	return dropped, nil
}

// viewOfKeptMember says whether a view shows a source in sight.
func (links *workspaceExportGoneLinks) viewOfKeptMember(viewID string) (bool, error) {
	sources := []string{}
	if err := links.app.DB().NewQuery(`
		SELECT [[data_source_id]] FROM {{leafen_database_views}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": viewID}).Column(&sources); err != nil || len(sources) == 0 {
		return false, err
	}
	return links.sourceInSight(sources[0])
}

// firstView is the first view a database keeps, in the order its archive
// lists them.
func (links *workspaceExportGoneLinks) firstView(containerID string) (string, error) {
	views := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
	}{}
	if err := links.app.DB().NewQuery(`
		SELECT [[view]].[[id]], [[view]].[[data_source_id]]
		FROM {{leafen_database_container_views}} AS [[membership]]
		JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]] = [[membership]].[[view_id]]
		JOIN {{leafen_database_container_sources}} AS [[source]]
			ON [[source]].[[container_id]] = [[membership]].[[container_id]]
			AND [[source]].[[data_source_id]] = [[view]].[[data_source_id]]
		WHERE [[membership]].[[container_id]] = {:id}
		ORDER BY [[source]].[[position]], [[source]].[[data_source_id]], [[view]].[[position]], [[view]].[[id]]
	`).Bind(dbx.Params{"id": containerID}).All(&views); err != nil {
		return "", err
	}
	for _, view := range views {
		if seen, err := links.sourceInSight(view.DataSourceID); err != nil || seen {
			return view.ID, err
		}
	}
	return "", nil
}
