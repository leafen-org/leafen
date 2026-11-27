package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A public page's links to other pages, as the visitor reads them: each
// one's target opens on the terms the reach walk decides
// (public_content_page_links.go), with the name, icon and empty or lined
// document of the page it opens.

// storedPublicLinkedPage is a page a published page links to or mentions:
// its name, its icon and whether its body holds anything, which its icon
// shows without one of its own (pageHasContentSQL), and its kind, which
// names it while it has no title (page_name_kind.go) — an untitled
// full-page database of several sources by their titles, as its visitor
// reads them (publicLinkedPagesAsRead).
type storedPublicLinkedPage struct {
	ID           string                `db:"id"`
	Title        string                `db:"title"`
	Icon         string                `db:"icon"`
	IconType     string                `db:"icon_type"`
	HasContent   bool                  `db:"has_content"`
	Kind         string                `db:"kind"`
	ViewBlock    bool                  `db:"view_block"`
	SourceTitles []databaseSourceTitle `db:"-"`
}

// publicPageLinkTarget reads a page link's target, preferring childPageId.
func publicPageLinkTarget(content map[string]any) string {
	for _, key := range []string{"childPageId", "pageId"} {
		if value, ok := content[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// projectPublicPageLinks shows each page link on a public page with its
// target's current title and icon when the target opens through the same
// authority, and as "No access" without the target otherwise. A link to a
// database or a comment opens the page that holds it on the same terms.
func projectPublicPageLinks(
	app core.App,
	authorization publicVisibilityAuthorization,
	pageID string,
	items []graphReadItem,
	now time.Time,
) ([]graphReadItem, error) {
	targets := map[int]string{}
	held := map[int]publicLinkTarget{}
	targetIDs := []string{}
	for index, item := range items {
		if item.Kind != graphReadKindBlock {
			continue
		}
		blockType, _ := item.Value["type"].(string)
		if _, ok := publicPageLinkBlockTypes[blockType]; !ok {
			continue
		}
		content, _ := item.Value["content"].(map[string]any)
		kind, id := publicLinkBlockTarget(content)
		switch kind {
		case "":
			continue
		case "page":
			targets[index] = id
			targetIDs = append(targetIDs, id)
			continue
		}
		target, found, err := resolvePublicLinkTarget(app, authorization, kind, id)
		if err != nil {
			return nil, err
		}
		// A gone or trashed database or comment has no page to open.
		held[index] = target
		targets[index] = target.PageID
		if found {
			targetIDs = append(targetIDs, target.PageID)
		}
	}
	if len(targets) == 0 {
		return items, nil
	}
	pages, err := loadPublicLinkedPages(app, authorization, targetIDs)
	if err != nil {
		return nil, err
	}
	reach := newPublicLinkedReach(app, authorization, now, pageID)
	opens := map[string]bool{}
	for _, target := range targetIDs {
		if _, checked := opens[target]; checked {
			continue
		}
		if _, live := pages[target]; !live || target == "" {
			opens[target] = false
			continue
		}
		reachable, err := reach.reachable(target)
		if err != nil {
			return nil, err
		}
		opens[target] = reachable
	}

	// What they name, as the visitor reads it (public_content_names.go).
	closer := publicReaderCloser(app, reach)
	publicLinkedPagesAsRead(app, closer, pages)
	opened := make([]string, 0, len(opens))
	for target, open := range opens {
		if open {
			opened = append(opened, target)
		}
	}
	titleSpans := publicTitlesSpansAsRead(app, closer, opened)
	projected := append([]graphReadItem(nil), items...)
	for index, target := range targets {
		content, _ := items[index].Value["content"].(map[string]any)
		next := map[string]any{publicPageLinkUnavailableKey: true}
		if linked, other := held[index]; other {
			if opens[target] && linked.FullPage {
				// A full-page database's own is named by its page's title, and
				// untitled with several sources by them, as a link to its page is.
				page := pages[target]
				linked.Title = page.Title
				next = publicLinkTargetContent(content, linked)
				if spans := titleSpans[target]; len(spans) > 0 {
					next[publicPageLinkTitleSpansKey] = publicSpanValues(spans)
				}
				if len(page.SourceTitles) > 1 {
					next["childPageTitle"], next[publicPageLinkSourceTitlesKey] = "", page.SourceTitles
				}
			} else if opens[target] {
				spans := publicNameSpansAsRead(closer, linked.Title, linked.Spans)
				linked.Title = publicNameAsRead(closer, linked.Title, linked.Spans)
				next = publicLinkTargetContent(content, linked)
				if linked.Kind == publicLinkKindDB && len(spans) > 0 {
					next[publicPageLinkTitleSpansKey] = publicSpanValues(spans)
				}
				// One held inline without a name of several is named by them.
				if len(linked.Sources) > 1 {
					sources := make([]databaseSourceTitle, len(linked.Sources))
					for at, source := range linked.Sources {
						sources[at] = databaseSourceTitle{Title: publicNameAsRead(closer, source.Title, source.Spans), Linked: source.Linked}
					}
					next["childPageTitle"], next[publicPageLinkSourceTitlesKey] = "", sources
				}
			}
		} else if opens[target] {
			next = make(map[string]any, len(content)+3)
			for key, value := range content {
				next[key] = value
			}
			page := pages[target]
			delete(next, "pageId")
			delete(next, "childPageIcon")
			delete(next, publicPageLinkTitleSpansKey)
			next["childPageId"] = target
			next["childPageTitle"] = page.Title
			if spans := titleSpans[target]; len(spans) > 0 {
				next[publicPageLinkTitleSpansKey] = publicSpanValues(spans)
			}
			next["childPageIconType"] = page.IconType
			if page.Icon != "" {
				next["childPageIcon"] = page.Icon
			}
			// Without an icon of its own it is an empty document or a lined
			// one, as a member's link to it shows (PageIcon.tsx).
			next[publicPageLinkHasContentKey] = page.HasContent
			// A full-page database's is named and drawn as one, as a member's
			// link to it is by the page list, whatever its block was written
			// with ("새 데이터베이스" untitled, not "새 페이지"); untitled with
			// several sources, it is named by them.
			next["childPageKind"] = readPageKind(page.Kind)
			delete(next, publicPageLinkSourceTitlesKey)
			if len(page.SourceTitles) > 1 {
				next[publicPageLinkSourceTitlesKey] = page.SourceTitles
			}
		}
		value := make(map[string]any, len(items[index].Value))
		for key, item := range items[index].Value {
			value[key] = item
		}
		value["content"] = next
		projected[index].Value = value
	}
	return projected, nil
}

func loadPublicLinkedPages(
	app core.App, authorization publicVisibilityAuthorization, pageIDs []string,
) (map[string]storedPublicLinkedPage, error) {
	pages := make(map[string]storedPublicLinkedPage, len(pageIDs))
	for start := 0; start < len(pageIDs); start += publicLinkedPageWindow {
		window := pageIDs[start:min(start+publicLinkedPageWindow, len(pageIDs))]
		params := dbx.Params{
			"workspaceId": authorization.WorkspaceID, "ownerId": authorization.Workspace.GetString("owner"),
		}
		placeholders := make([]string, 0, len(window))
		for index, pageID := range window {
			key := fmt.Sprintf("linkedPage%d", index)
			params[key] = pageID
			placeholders = append(placeholders, "{:"+key+"}")
		}
		rows := []storedPublicLinkedPage{}
		if err := app.DB().NewQuery(`
			SELECT [[page]].[[id]], [[page]].[[title]], [[page]].[[icon]], [[page]].[[icon_type]],
				` + pageHasContentSQL + ` AS [[has_content]], COALESCE([[page]].[[kind]], '') AS [[kind]],
				` + viewBlockDatabasePageSQL + ` AS [[view_block]]
			FROM {{leafen_pages}} [[page]]
			WHERE [[page]].[[id]] IN (` + joinSQL(placeholders) + `)
				AND [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
				AND [[page]].[[notion_import_staging]] = 0 AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
		`).Bind(params).All(&rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			pages[row.ID] = row
		}
	}
	return pages, nil
}

// publicContentLinkedFileVisible reports whether a page reached through
// published page links displays the file.
func publicContentLinkedFileVisible(
	app core.App, authorization publicVisibilityAuthorization, fileID string, kinds []string, now time.Time,
) (bool, error) {
	params := dbx.Params{
		"fileId": fileID, "workspaceId": authorization.WorkspaceID,
		"ownerId": authorization.Workspace.GetString("owner"), "limit": publicLinkedPageWindow,
	}
	placeholders := make([]string, 0, len(kinds))
	for index, kind := range kinds {
		key := fmt.Sprintf("linkedKind%d", index)
		params[key] = kind
		placeholders = append(placeholders, "{:"+key+"}")
	}
	// Every page that holds the file, a window at a time, until one is reached.
	reach := newPublicLinkedReach(app, authorization, now)
	params["after"] = ""
	for {
		holders := []struct {
			PageID string `db:"page_id"`
			Shown  string `db:"shown"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT DISTINCT [[reference]].[[page_id]] AS [[page_id]], ` + publicContentShownPageSQL + ` AS [[shown]]
			FROM {{leafen_file_references}} [[reference]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[reference]].[[page_id]]
			WHERE [[reference]].[[file_id]] = {:fileId} AND [[reference]].[[workspace_id]] = {:workspaceId}
				AND [[reference]].[[owner_kind]] IN (` + joinSQL(placeholders) + `)
				AND [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
				AND [[page]].[[notion_import_staging]] = 0 AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
				AND [[reference]].[[page_id]] > {:after}
			ORDER BY [[reference]].[[page_id]] LIMIT {:limit}
		`).Bind(params).All(&holders); err != nil {
			return false, err
		}
		for _, holder := range holders {
			reachable, err := reach.reachable(holder.Shown)
			if err != nil || reachable {
				return reachable, err
			}
		}
		if len(holders) < publicLinkedPageWindow {
			return false, nil
		}
		params["after"] = holders[len(holders)-1].PageID
	}
}
