package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page mention names a page or, as in Notion, a database row page. Both
// are read through this owner: a request's public ID resolves to the one it
// names, and every write and read authorizes the target again for the
// identity at hand.

type notionPageMentionTarget struct {
	InternalID string
	PublicID   string
	Title      string
	RowPage    bool
}

// A request names a row page by its stored public ID, and a page made from a
// block by its own ID, which only the mention owner can resolve; until then
// the mention carries it behind this prefix, which no internal ID has.
const notionPageMentionRowPrefix = "notion-row-page:"

// resolveNotionPageMentionID turns a request's page mention ID, internal or
// a pending row public ID, into the internal page or row ID.
func resolveNotionPageMentionID(app core.App, pageID string) (string, bool) {
	publicID, pending := strings.CutPrefix(pageID, notionPageMentionRowPrefix)
	if !pending {
		return pageID, pageID != ""
	}
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, publicID)
	if err == nil {
		return identity.InternalID, true
	}
	// A page made from a block answers with its own ID.
	if pageID, err := resolveNotionPageRecordID(app, publicID); err == nil {
		return pageID, true
	}
	return "", false
}

// authorizeNotionPageMentionTarget finds the page or row page the identity
// may read in its workspace; `allowed` is false for anything else.
func authorizeNotionPageMentionTarget(
	app core.App,
	identity notionOAuthBearerIdentity,
	ownerID, workspaceID, internalID string,
) (notionPageMentionTarget, bool, error) {
	return authorizePageMentionTarget(app, identity.PrincipalID, ownerID, workspaceID, internalID,
		func(page pageAuthorization) bool { return notionOAuthIdentityAllowsPage(app, identity, page) })
}

// notionIdentityCloses is the mentions a Public API connection's reading
// leaves unnamed: a page, or a database through the page holding it, it
// can't reach. Its words read "Untitled", as Notion's API gives a mention of
// a page an integration has no access to (mcp_reader_mentions.go).
func notionIdentityCloses(app core.App, identity notionOAuthBearerIdentity) searchMentionCloser {
	return memoMentionCloser(func(pageID string) bool {
		_, allowed, err := authorizeNotionPageMentionTarget(app, identity, "", identity.WorkspaceID, pageID)
		return err != nil || !allowed
	}, app)
}

// notionDatabaseMentionPublicID is the ID a database mention names its
// database by to a connection: its public ID in the connection's workspace,
// or the ID written for one that isn't there or not in it.
func notionDatabaseMentionPublicID(app core.App, workspaceID, databaseID string) (string, error) {
	database, found, _ := loadNotionMentionedDatabase(app, databaseID)
	if !found || database.WorkspaceID != workspaceID {
		return databaseID, nil
	}
	return ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, workspaceID, databaseID)
}

// notionMentionedDatabase is where a mentioned database stands: the owner
// and workspace it is in, the page holding it, and whether it is in the
// trash — itself, or as a source database whose block left its page, which
// the app's trash keeps and restores (deleted_databases.go) and Notion's
// keeps as a database in the trash (seen 2026-11-13: a database a page
// Markdown write deleted reads in_trash, its mentions by kind and name).
type notionMentionedDatabase struct {
	Owner       string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
	Trashed     bool   `db:"trashed"`
}

// loadNotionMentionedDatabase finds a source database by its own ID, in the
// trash or out of it; false for one neither holds.
func loadNotionMentionedDatabase(app core.App, containerID string) (notionMentionedDatabase, bool, error) {
	found := []notionMentionedDatabase{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[owner]], [[container]].[[workspace_id]], [[container]].[[page_id]], EXISTS (
			SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
			WHERE [[trash]].[[container_id]] = [[container]].[[id]]
		) AS [[trashed]]
		FROM {{leafen_database_containers}} AS [[container]]
		WHERE [[container]].[[id]] = {:id} AND [[container]].[[kind]] = 'source'
			AND EXISTS (
				SELECT 1 FROM {{leafen_database_container_sources}} AS [[membership]]
				WHERE [[membership]].[[container_id]] = [[container]].[[id]]
			)
		UNION ALL
		SELECT [[owner]], [[workspace_id]], [[page_id]], 1 AS [[trashed]]
		FROM {{leafen_deleted_databases}} WHERE [[block_id]] = {:id}
		LIMIT 1
	`).Bind(dbx.Params{"id": containerID}).All(&found)
	if err != nil || len(found) == 0 {
		return notionMentionedDatabase{}, false, err
	}
	return found[0], true, nil
}

// authorizePageMentionTarget finds the page or row page the principal may
// read in the workspace (and content owner, when given); `allows` narrows
// the pages a scoped credential may reach.
func authorizePageMentionTarget(
	app core.App,
	principalID, ownerID, workspaceID, internalID string,
	allows func(pageAuthorization) bool,
) (notionPageMentionTarget, bool, error) {
	visible := func(page pageAuthorization) bool {
		return (ownerID == "" || page.Workspace.OwnerID == ownerID) && page.Workspace.WorkspaceID == workspaceID &&
			(allows == nil || allows(page))
	}
	page, err := authorizePage(app, principalID, internalID, permissionWorkspaceRead)
	if err == nil {
		if !visible(page) {
			return notionPageMentionTarget{}, false, nil
		}
		publicID, err := notionPagePublicID(app, internalID)
		if err != nil {
			return notionPageMentionTarget{}, false, nil
		}
		title := strings.TrimSpace(page.Page.GetString("title"))
		// A database's row page reads by its title cell, as below.
		if page.Page.GetString("data_source_id") != "" {
			title = databaseRowPageTitleAsMentioned(app, principalID, internalID)
		}
		return notionPageMentionTarget{InternalID: internalID, PublicID: publicID, Title: title}, true, nil
	}
	if !isPermissionDenied(err) {
		return notionPageMentionTarget{}, false, err
	}
	row, err := authorizeDatabaseRowPage(app, principalID, internalID, permissionWorkspaceRead)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return notionPageMentionTarget{}, false, nil
	}
	if err != nil {
		return notionPageMentionTarget{}, false, err
	}
	pending, err := notionDatabaseRowPageDeletionPending(app, row)
	if err != nil || pending {
		return notionPageMentionTarget{}, false, err
	}
	container, err := authorizePage(app, principalID, row.ContainerPageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return notionPageMentionTarget{}, false, nil
	}
	if err != nil {
		return notionPageMentionTarget{}, false, err
	}
	if container.Page.GetBool("in_trash") || !visible(container) {
		return notionPageMentionTarget{}, false, nil
	}
	publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, workspaceID, internalID)
	if err != nil {
		return notionPageMentionTarget{}, false, err
	}
	return notionPageMentionTarget{
		InternalID: internalID, PublicID: publicID, Title: databaseRowPageTitleAsMentioned(app, principalID, internalID), RowPage: true,
	}, true, nil
}

// pageMentionTargetNow is the title now of a page or database page, by its
// ID, and the workspace it is in; false for one nothing answers to. It reads
// for whom the mention's reader or writer was already checked.
func pageMentionTargetNow(app core.App, pageID string) (string, string, bool) {
	stored := struct {
		Workspace    string `db:"workspace"`
		Title        string `db:"title"`
		DataSourceID string `db:"data_source_id"`
	}{}
	if pageID == "" || app.DB().NewQuery(`
		SELECT [[workspace]], [[title]], [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": pageID}).One(&stored) != nil {
		return "", "", false
	}
	if stored.DataSourceID != "" {
		return databaseRowPageTitle(app, pageID), stored.Workspace, true
	}
	return strings.TrimSpace(stored.Title), stored.Workspace, true
}

// databaseRowPageTitleAsMentioned is a row page's title as a mention of it
// reads for its reader: a page or database mentioned in that title by "@"
// and its name now, as Notion's Markdown reads it (seen 2026-11-10: "See
// @Target renamed row"), and one closed to the reader as a title read for
// them closes it; a reading for no one opens every one.
func databaseRowPageTitleAsMentioned(app core.App, readerID, rowID string) string {
	spans, mentions := rowTitleSpans(app, rowID)
	if !mentions {
		return databaseRowPageTitle(app, rowID)
	}
	closer := readerCloser(app, readerID)
	var title strings.Builder
	for _, span := range spans {
		if span.Mention != "page" && span.Mention != "database" {
			title.WriteString(span.Text)
			continue
		}
		// A reading for no one (a server administrator's) opens every one.
		if readerID != "" {
			if reach := searchMentionReach(closer(), span.Mention, span.PageID, span.DatabaseID); reach != mentionOpens {
				title.WriteString(unnamedMentionWords(reach, span.Text, true))
				continue
			}
		}
		name := ""
		if span.Mention == "database" {
			name, _, _ = databaseContainerTitle(app, span.DatabaseID)
		} else {
			name, _, _ = pageMentionTargetNow(app, span.PageID)
		}
		if name == "" {
			name = strings.TrimPrefix(strings.TrimSpace(span.Text), "@")
		}
		title.WriteString("@" + name)
	}
	return strings.TrimSpace(title.String())
}

// databaseRowPageTitle is the plain text of a row's title cell. A database's
// template is a page of its database with no row (database_template_pages_schema.go),
// its title the template's page record's, which follows the template: it
// reads that, as Notion's mention and API name a template by its title (seen
// 2026-11-23: a page's mention of a template read "w2397 템플릿" in Notion's
// screen and in its API's plain_text, where Leafen's read empty).
func databaseRowPageTitle(app core.App, rowID string) string {
	raw := ""
	err := app.DB().NewQuery(`
		SELECT [[value]] FROM {{leafen_database_row_values}}
		WHERE [[row_id]] = {:rowId} AND [[type]] = 'title' LIMIT 1
	`).Bind(dbx.Params{"rowId": rowID}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		title := ""
		_ = app.DB().NewQuery(`
			SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]] = {:rowId} AND [[database_template]]
		`).Bind(dbx.Params{"rowId": rowID}).Row(&title)
		return strings.TrimSpace(title)
	}
	if err != nil {
		return ""
	}
	return databaseRowTitleText(raw)
}

// databaseRowTitleText is the plain text of a stored title value: text, or
// its spans' text.
func databaseRowTitleText(raw string) string {
	var text string
	if json.Unmarshal([]byte(raw), &text) == nil {
		return strings.TrimSpace(text)
	}
	spans := []graphTextSpan{}
	if json.Unmarshal([]byte(raw), &spans) != nil {
		return ""
	}
	var title strings.Builder
	for _, span := range spans {
		title.WriteString(span.Text)
	}
	return strings.TrimSpace(title.String())
}
