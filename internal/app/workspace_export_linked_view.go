package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's Markdown & CSV export of a linked database opened as a page
// (seen 2026-11-11) names the view's CSV after the linked database itself,
// its own name — "제목 없음" while it has none — and its ID, at the top of
// the archive, and writes its source's "_all" CSV where the source stands:
// under the folders of the pages holding it ("<test page>/<page>/<source>
// <id>_all.csv"). A database export asked for by a linked database — its
// block, or a full-page one's page — exports its first source, named so.

// workspaceExportLinkedView is the linked database a database export was
// asked for by.
type workspaceExportLinkedView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// workspaceExportUntitledName names what has no name in an export, as
// Notion's export does in the language it is made in ("제목 없음" in Korean,
// seen 2026-11-11).
func workspaceExportUntitledName(language string) string {
	if language == accountLanguageKo {
		return "제목 없음"
	}
	return "Untitled"
}

// resolveWorkspaceExportLinkedView reads a database export's target: a data
// source stands for itself; a linked database the person may view stands
// for its first source, with the view it was asked for by. Anything else is
// left to the scope's own authorization, which refuses it.
func resolveWorkspaceExportLinkedView(
	app core.App, actorID, workspaceID string, scope workspaceExportScope,
) (workspaceExportScope, *workspaceExportLinkedView, error) {
	if scope.Kind != workspaceExportScopeDatabase {
		return scope, nil, nil
	}
	var sources int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_data_sources}}
		WHERE [[id]] = {:target} AND [[workspace_id]] = {:workspace}`).
		Bind(dbx.Params{"target": scope.TargetID, "workspace": workspaceID}).Row(&sources); err != nil || sources > 0 {
		return scope, nil, err
	}
	linked := struct {
		Owner    string `db:"owner"`
		PageID   string `db:"page_id"`
		BlockID  string `db:"block_id"`
		SourceID string `db:"source_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[owner]], [[container]].[[page_id]], [[container]].[[block_id]],
			COALESCE((SELECT [[place]].[[data_source_id]] FROM {{leafen_database_container_sources}} [[place]]
				WHERE [[place]].[[container_id]] = [[container]].[[id]]
				ORDER BY [[place]].[[position]], [[place]].[[data_source_id]] LIMIT 1), '') AS [[source_id]]
		FROM {{leafen_database_containers}} [[container]]
		JOIN {{leafen_pages}} [[holder]] ON [[holder]].[[id]] = [[container]].[[page_id]]
		WHERE [[container]].[[workspace_id]] = {:workspace} AND [[container]].[[kind]] = '` + databaseContainerKindLinked + `'
			AND ([[container]].[[block_id]] = {:target} OR ([[container]].[[page_id]] = {:target}
				AND [[container]].[[parent_block_id]] = '' AND [[holder]].[[kind]] = '` + pageKindDatabase + `'))
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} [[trash]]
				WHERE [[trash]].[[container_id]] = [[container]].[[id]])
		ORDER BY [[container]].[[position]], [[container]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"target": scope.TargetID, "workspace": workspaceID}).One(&linked)
	if errors.Is(err, sql.ErrNoRows) || err == nil && linked.SourceID == "" {
		return scope, nil, nil
	}
	if err != nil {
		return scope, nil, err
	}
	holder, err := pageAccessDespiteHold(authorizePageAccessRole(app, actorID, linked.PageID, pageAccessRoleView))
	if err != nil {
		return scope, nil, err
	}
	view := &workspaceExportLinkedView{ID: linked.BlockID}
	// A full-page linked database is its page.
	if top, found, err := loadDatabasePageBlock(app, linked.Owner, linked.PageID); err != nil {
		return scope, nil, err
	} else if found && top.ID == linked.BlockID && isFullPageDatabase(holder.Page) {
		view.ID = linked.PageID
	}
	view.Title, _, _ = databaseOwnName(app, linked.Owner, linked.PageID, linked.BlockID, holder.Page)
	return workspaceExportScope{Kind: scope.Kind, TargetID: linked.SourceID}, view, nil
}

// workspaceExportSourceFolder is the folder a database's own files stand in
// within the pages holding it: each page above it, named by its title as
// Notion's export names a page's folder, from the highest the person
// exporting may view, the database's own page among them unless the
// database is that page, however deep (parent_chain.go).
func workspaceExportSourceFolder(app core.App, actorID, sourceID, untitled string) (string, error) {
	pageID := ""
	if err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": sourceID}).Row(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	folders := []string{}
	chain := parentChain[string]{}
	for depth := 0; pageID != "" && chain.climb(pageID); depth++ {
		page := struct {
			Title    string `db:"title"`
			Kind     string `db:"kind"`
			ParentID string `db:"parent_id"`
		}{}
		err := app.DB().NewQuery(`SELECT [[title]], [[kind]], [[parent_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": pageID}).One(&page)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return "", err
		}
		if _, err := pageAccessDespiteHold(authorizePageAccessRole(app, actorID, pageID, pageAccessRoleView)); err != nil {
			if isPermissionDenied(err) {
				break
			}
			return "", err
		}
		if depth > 0 || page.Kind != pageKindDatabase {
			folders = append(folders, workspaceExportFolderName(page.Title, untitled))
		}
		pageID = page.ParentID
	}
	var folder strings.Builder
	for index := len(folders) - 1; index >= 0; index-- {
		folder.WriteString(folders[index])
	}
	return folder.String(), nil
}
