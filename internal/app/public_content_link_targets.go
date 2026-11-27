package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A link block may point at a database or a comment instead of a page. On a
// public page it opens what holds its target when that page opens through
// the same published links a page link would: a database's page at the
// database, with the database's current name (database_container_name.go),
// and a comment's page, since a public page shows no comments. The
// projection names the page to open and leaves out the database's and the
// comment's own ids.

const (
	publicLinkKindKey    = "publicLinkKind"
	publicLinkPageKey    = "publicLinkPageId"
	publicLinkBlockKey   = "publicLinkBlockId"
	publicLinkKindDB     = "database"
	publicLinkKindThread = "comment"
)

type publicLinkTarget struct {
	Kind     string
	PageID   string
	BlockID  string
	Title    string
	Spans    []graphTextSpan
	Icon     string
	IconType string
	// A database named by its page's title, or by its sources' names.
	FullPage bool
	Sources  []databaseNameRead
}

// publicLinkBlockTarget reads what a link block points at: a page, a
// database or a comment, and its id.
func publicLinkBlockTarget(content map[string]any) (string, string) {
	if target := publicPageLinkTarget(content); target != "" {
		return "page", target
	}
	for _, reference := range []struct{ key, kind string }{
		{"databaseContainerId", publicLinkKindDB}, {"commentId", publicLinkKindThread},
	} {
		if value, ok := content[reference.key].(string); ok && value != "" {
			return reference.kind, value
		}
	}
	return "", ""
}

// resolvePublicLinkTarget finds the page that holds a database or comment
// of the authority's workspace, or reports none when it is gone or trashed.
func resolvePublicLinkTarget(
	app core.App, authorization publicVisibilityAuthorization, kind, id string,
) (publicLinkTarget, bool, error) {
	target := publicLinkTarget{Kind: kind}
	var err error
	if kind == publicLinkKindDB {
		err = app.DB().NewQuery(`
			SELECT [[container]].[[page_id]], [[container]].[[block_id]] FROM {{leafen_database_containers}} AS [[container]]
			WHERE [[container]].[[id]] = {:id}
				AND NOT EXISTS (
					SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
					WHERE [[trash]].[[container_id]] = [[container]].[[id]]
				)
		`).Bind(dbx.Params{"id": id}).Row(&target.PageID, &target.BlockID)
	} else {
		err = app.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_comments}}
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[target_kind]] = 'page'
		`).Bind(dbx.Params{"id": id, "workspaceId": authorization.WorkspaceID}).Row(&target.PageID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return publicLinkTarget{}, false, nil
	}
	if err != nil {
		return publicLinkTarget{}, false, err
	}
	if kind == publicLinkKindDB {
		err = app.DB().NewQuery(`
			SELECT [[source]].[[icon]], [[source]].[[icon_type]]
			FROM {{leafen_database_container_sources}} AS [[member]]
			JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[member]].[[data_source_id]]
			WHERE [[member]].[[container_id]] = {:id}
			ORDER BY [[member]].[[position]], [[member]].[[data_source_id]] LIMIT 1
		`).Bind(dbx.Params{"id": id}).Row(&target.Icon, &target.IconType)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return publicLinkTarget{}, false, err
		}
		names, err := databaseContainerNames(app, []string{id})
		if err != nil {
			return publicLinkTarget{}, false, err
		}
		name := names[id]
		target.Title, target.Spans, target.FullPage, target.Sources = name.Title, name.Spans, name.FullPage, name.Sources
	}
	return target, target.PageID != "", nil
}

// publicLinkTargetContent is the content a database or comment link shows
// on a public page when its page opens there.
func publicLinkTargetContent(content map[string]any, target publicLinkTarget) map[string]any {
	next := make(map[string]any, len(content)+5)
	for key, value := range content {
		next[key] = value
	}
	for _, key := range []string{"databaseContainerId", "commentId", "childPageId", "pageId", "childPageIcon", publicPageLinkTitleSpansKey} {
		delete(next, key)
	}
	next[publicLinkKindKey] = target.Kind
	next[publicLinkPageKey] = target.PageID
	if target.Kind == publicLinkKindDB {
		next[publicLinkBlockKey] = target.BlockID
		next["childPageTitle"] = target.Title
		next["childPageIconType"] = target.IconType
		if target.Icon != "" {
			next["childPageIcon"] = target.Icon
		}
	}
	return next
}
