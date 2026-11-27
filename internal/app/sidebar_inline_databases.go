package app

import (
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// bootstrapInlineDatabases lists the databases the pages this bootstrap
// batch carries hold in their bodies — not a full-page database's own, nor a
// linked view of another's, nor one deleted with its block, nor a wiki's own,
// which the wiki is (Notion's sidebar lists a wiki's pages under it, seen
// 2026-11-11) — in their order on the page, each named and
// drawn by its first data source and carrying every source's title, as
// Notion's sidebar lists them under their page (seen 2026-11-02), one of
// several sources named by its own name, none while it has none
// (inline_database_own_name.go), a
// dashboard and a link waiting for its source among them, each said to be a
// view block, "제목 없음" while unnamed (database_view_block.go). Each name
// reads as its reader reads it (page_name_mentions.go).
func bootstrapInlineDatabases(app core.App, readerID string, pageIDs map[string]bool) ([]inlineDatabaseProjection, error) {
	ids := make([]any, 0, len(pageIDs))
	for id := range pageIDs {
		ids = append(ids, id)
	}
	type containerRow struct {
		ID          string `db:"id"`
		PageID      string `db:"page_id"`
		BlockID     string `db:"block_id"`
		WorkspaceID string `db:"workspace_id"`
		ViewBlock   bool   `db:"view_block"`
	}
	containers := []containerRow{}
	for start := 0; start < len(ids); start += 500 {
		batch := []containerRow{}
		if err := app.DB().NewQuery(`
			SELECT [[container]].[[id]], [[container]].[[page_id]], [[container]].[[block_id]], [[container]].[[workspace_id]],
				` + databaseViewBlockSQL("[[container]]") + ` AS [[view_block]]
			FROM {{leafen_database_containers}} [[container]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]]=[[container]].[[page_id]]
			WHERE [[container]].[[kind]]='source' AND [[page]].[[kind]]!='database'
				AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} [[trash]] WHERE [[trash]].[[container_id]]=[[container]].[[id]])
				AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_sources}} [[wiki_place]]
					JOIN {{leafen_data_sources}} [[wiki_source]] ON [[wiki_source]].[[id]]=[[wiki_place]].[[data_source_id]]
					WHERE [[wiki_place]].[[container_id]]=[[container]].[[id]] AND [[wiki_source]].[[wiki]]=1)
				AND [[container]].[[page_id]] IN (` + inlineDatabaseInList("page", ids[start:min(start+500, len(ids))]) + `)
			ORDER BY [[container]].[[page_id]], [[container]].[[position]], [[container]].[[id]]
		`).Bind(inlineDatabaseInParams("page", ids[start:min(start+500, len(ids))])).All(&batch); err != nil {
			return nil, err
		}
		containers = append(containers, batch...)
	}
	if len(containers) == 0 {
		return nil, nil
	}
	containerIDs := make([]any, 0, len(containers))
	for _, container := range containers {
		containerIDs = append(containerIDs, container.ID)
	}
	type sourceRow struct {
		ContainerID string `db:"container_id"`
		Title       string `db:"title"`
		Spans       string `db:"title_spans"`
		Icon        string `db:"icon"`
		IconType    string `db:"icon_type"`
		Gone        bool   `db:"gone"`
		Linked      bool   `db:"linked"`
		read        []graphTextSpan
	}
	sources := map[string][]sourceRow{}
	closer := readerCloser(app, readerID)
	for start := 0; start < len(containerIDs); start += 500 {
		batch := []sourceRow{}
		if err := app.DB().NewQuery(`
			SELECT [[place]].[[container_id]], [[source]].[[title]], [[source]].[[title_spans]],
				` + dataSourceIconSQL("[[source]]") + ` AS [[icon]], ` + dataSourceIconTypeSQL("[[source]]") + ` AS [[icon_type]],
				` + databaseSourceGoneWithViewsSQL("[[place]].[[container_id]]", "[[place]].[[data_source_id]]") + ` AS [[gone]],
				[[place]].[[source_container_id]] != '' AS [[linked]]
			FROM {{leafen_database_container_sources}} [[place]]
			JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]]=[[place]].[[data_source_id]]
			WHERE [[place]].[[container_id]] IN (` + inlineDatabaseInList("container", containerIDs[start:min(start+500, len(containerIDs))]) + `)
			ORDER BY [[place]].[[container_id]], [[place]].[[position]], [[place]].[[data_source_id]]
		`).Bind(inlineDatabaseInParams("container", containerIDs[start:min(start+500, len(containerIDs))])).All(&batch); err != nil {
			return nil, err
		}
		for _, row := range batch {
			if strings.Contains(row.Spans, `"mention"`) {
				// Its name and spans as its reader reads them: a plain title
				// says its mentions by them (closedMentions.ts).
				read := databaseNameSpansAsRead(closer, row.Title, decodeStoredTextSpans(row.Spans))
				row.Title, row.read = read.Title, read.Spans
			}
			sources[row.ContainerID] = append(sources[row.ContainerID], row)
		}
	}
	held := map[string]map[string]bool{}
	for _, container := range containers {
		if held[container.PageID] == nil {
			held[container.PageID] = map[string]bool{}
		}
		held[container.PageID][container.BlockID] = true
	}
	after, err := inlineDatabasesAfterPages(app, held)
	if err != nil {
		return nil, err
	}
	// A source in the trash with its views neither names nor counts
	// (databaseNamingSources).
	for containerID, own := range sources {
		sources[containerID] = databaseNamingSources(own, func(source sourceRow) bool { return source.Gone }, func(source sourceRow) bool { return source.Linked })
	}
	several := []string{}
	for containerID, own := range sources {
		if len(own) > 1 {
			several = append(several, containerID)
		}
	}
	ownNames, err := inlineDatabaseNamesOfSeveral(app, closer, several)
	if err != nil {
		return nil, err
	}
	rows := make([]inlineDatabaseProjection, 0, len(containers))
	for _, container := range containers {
		own := sources[container.ID]
		if len(own) == 0 {
			continue
		}
		row := inlineDatabaseProjection{
			ContainerID: container.ID, PageID: container.PageID, WorkspaceID: container.WorkspaceID,
			Title: own[0].Title, TitleSpans: own[0].read, Icon: own[0].Icon, IconType: own[0].IconType, ViewBlock: container.ViewBlock,
			AfterPageID: after[container.BlockID],
		}
		if len(own) > 1 {
			for _, source := range own {
				row.SourceTitles = append(row.SourceTitles, databaseSourceTitle{Title: source.Title, Linked: source.Linked})
			}
			if name, named := ownNames[container.ID]; named {
				row.Title, row.TitleSpans = name.Title, name.Spans
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// inlineDatabaseInList names a batch's values as bound parameters for an IN list.
func inlineDatabaseInList(prefix string, values []any) string {
	names := make([]string, len(values))
	for index := range values {
		names[index] = "{:" + prefix + strconv.Itoa(index) + "}"
	}
	return strings.Join(names, ",")
}

func inlineDatabaseInParams(prefix string, values []any) dbx.Params {
	params := dbx.Params{}
	for index, value := range values {
		params[prefix+strconv.Itoa(index)] = value
	}
	return params
}

// inlineDatabasesAfterPages names, for each database block a page holds,
// the sub-page whose block comes last before it in the page's body, read
// top to bottom through toggles and columns, so the sidebar lists a page's
// sub-pages and databases in its body's order, as Notion's does (seen
// 2026-11-07); "" for one before every sub-page. A sub-page hanging on the
// page's title comes before them all, and one hanging on a line, or with a
// block inside one that its words mention, where the line stands
// (childPageBodyOrder), as Notion's sidebar lists them (seen 2026-11-13: a
// title's page, a line's, then the database).
func inlineDatabasesAfterPages(app core.App, held map[string]map[string]bool) (map[string]string, error) {
	after := map[string]string{}
	for pageID, blocks := range held {
		rows := []struct {
			ID        string `db:"id"`
			ParentID  string `db:"parent_id"`
			Position  string `db:"position"`
			Type      string `db:"type"`
			ChildPage string `db:"child_page"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[block]].[[id]], COALESCE([[block]].[[parent_id]], '') AS [[parent_id]], [[block]].[[position]], [[block]].[[type]],
				CASE WHEN [[block]].[[type]] = 'child_page' THEN COALESCE(json_extract([[block]].[[content]], '$.childPageId'), '') ELSE '' END AS [[child_page]]
			FROM {{leafen_blocks}} [[block]] WHERE [[block]].[[page_id]] = {:page}
		`).Bind(dbx.Params{"page": pageID}).All(&rows); err != nil {
			return nil, err
		}
		children := map[string][]int{}
		for index, row := range rows {
			children[row.ParentID] = append(children[row.ParentID], index)
		}
		for _, list := range children {
			sort.SliceStable(list, func(left, right int) bool {
				a, b := rows[list[left]], rows[list[right]]
				if a.Position != b.Position {
					return a.Position < b.Position
				}
				return a.ID < b.ID
			})
		}
		anchors, err := loadMentionAnchors(app, pageID)
		if err != nil {
			return nil, err
		}
		holders, callouts := map[string]string{}, map[string]bool{}
		for _, row := range rows {
			if row.ChildPage != "" && row.ParentID != "" {
				holders[row.ChildPage] = row.ParentID
			}
			if row.Type == graphBlockTypeCallout {
				callouts[row.ID] = true
			}
		}
		lines, err := childPageLines(app, pageID, anchors, holders, callouts)
		if err != nil {
			return nil, err
		}
		last, listed := "", map[string]bool{}
		list := func(pages ...string) {
			for _, page := range pages {
				if !listed[page] {
					listed[page], last = true, page
				}
			}
		}
		list(lines[pageAnchorTitle]...)
		visited := map[string]bool{}
		var walk func(parent string)
		walk = func(parent string) {
			for _, index := range children[parent] {
				row := rows[index]
				if visited[row.ID] {
					continue
				}
				visited[row.ID] = true
				if blocks[row.ID] {
					after[row.ID] = last
				}
				if row.ChildPage != "" {
					list(row.ChildPage)
				}
				list(lines[row.ID]...)
				walk(row.ID)
			}
		}
		walk("")
	}
	return after, nil
}
