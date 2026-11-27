package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The official database object is a native database container. Its title and
// icon are those of its first source, as the editor shows them, while it has
// one source of its own; with more, its title is its own (its page's for a
// full-page one, notionInlineDatabaseName for an inline one). A full-page
// database is a page of that kind whose body is the database, and a wiki's
// database is shown as its wiki's page: such a database reports is_inline
// false, the page's parent (a page or the block its page block sits in) as
// its parent, and the page's cover and lock. Any other database is inline in
// its page or block.

const notionAPIDatabasesPath = notionAPIRoot + "/databases"

func serveNotionDatabaseCompatibility(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) (bool, error) {
	if event.Request.URL.Path == notionAPIDatabasesPath && event.Request.Method == http.MethodPost {
		return true, serveNotionDatabaseCreate(event, ygo)
	}
	if !strings.HasPrefix(event.Request.URL.Path, notionAPIDatabasesPath+"/") {
		return false, nil
	}
	publicID := strings.TrimPrefix(event.Request.URL.Path, notionAPIDatabasesPath+"/")
	if publicID == "" || strings.Contains(publicID, "/") {
		return false, nil
	}
	publicID = notionDatabaseIDForBlock(event.App, publicID)
	switch event.Request.Method {
	case http.MethodGet:
		return true, serveNotionDatabaseRetrieve(event, publicID)
	case http.MethodPatch:
		return true, serveNotionDatabaseUpdate(event, ygo, publicID)
	}
	return false, nil
}

func serveNotionDatabaseRetrieve(event *core.RequestEvent, publicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	identity, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDatabase, publicID)
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && identity.Workspace != workspace.Identity.WorkspaceID {
		return notionPageNotFound(event)
	}
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	return serveNotionDatabaseObject(event, workspace.Identity, identity.InternalID)
}

func serveNotionDatabaseObject(event *core.RequestEvent, identity notionOAuthBearerIdentity, containerID string) error {
	object, err := loadNotionDatabaseObject(event.App, event.Request, identity, containerID)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return writeNotionJSON(event, http.StatusOK, object)
}

type notionDatabaseContainer struct {
	ID            string `db:"id"`
	Owner         string `db:"owner"`
	WorkspaceID   string `db:"workspace_id"`
	PageID        string `db:"page_id"`
	BlockID       string `db:"block_id"`
	ParentBlockID string `db:"parent_block_id"`
	Created       int64  `db:"created"`
	Updated       int64  `db:"updated"`
	Trashed       bool   `db:"trashed"`
	TopLevel      bool   `db:"top_level"`
	Wiki          bool   `db:"wiki"`
	PageKind      string `db:"page_kind"`
	// Deleted is a database whose block left its page, which the trash of
	// deleted databases keeps with its block's content
	// (notion_deleted_database_reads.go).
	Deleted        bool   `db:"-"`
	DeletedContent string `db:"-"`
}

// loadNotionDatabaseContainer reads a database container the caller can open
// with the given permission.
func loadNotionDatabaseContainer(
	app core.App, identity notionOAuthBearerIdentity, containerID string, requirement permissionRequirement,
) (notionDatabaseContainer, pageAuthorization, error) {
	container, err := loadNotionDatabaseContainerRecord(app, identity.WorkspaceID, containerID)
	if errors.Is(err, sql.ErrNoRows) {
		return notionDatabaseContainer{}, pageAuthorization{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return notionDatabaseContainer{}, pageAuthorization{}, err
	}
	page, err := authorizePageBody(app, identity.PrincipalID, container.PageID, requirement)
	if isPermissionDenied(err) || err == nil && (page.Workspace.WorkspaceID != identity.WorkspaceID ||
		page.Workspace.OwnerID != container.Owner || !notionOAuthIdentityAllowsPage(app, identity, page)) {
		return notionDatabaseContainer{}, pageAuthorization{}, errDatabaseRowPageNotFound
	}
	return container, page, err
}

// loadNotionDatabaseContainerRecord reads a workspace's database container,
// or one a deleted database left (notion_deleted_database_reads.go).
func loadNotionDatabaseContainerRecord(app core.App, workspaceID, containerID string) (notionDatabaseContainer, error) {
	container := notionDatabaseContainer{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], [[container]].[[owner]], [[container]].[[workspace_id]], [[container]].[[page_id]],
			[[container]].[[block_id]], [[container]].[[parent_block_id]], [[container]].[[created]], [[container]].[[updated]],
			EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] = [[container]].[[id]]) AS [[trashed]],
			EXISTS (SELECT 1 FROM {{leafen_blocks}} AS [[own]]
				WHERE [[own]].[[owner]] = [[container]].[[owner]] AND [[own]].[[page_id]] = [[container]].[[page_id]]
					AND [[own]].[[id]] = [[container]].[[block_id]] AND [[own]].[[parent_id]] = '') AS [[top_level]],
			EXISTS (SELECT 1 FROM {{leafen_database_container_sources}} AS [[member]]
				JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[member]].[[data_source_id]]
				WHERE [[member]].[[container_id]] = [[container]].[[id]] AND [[source]].[[wiki]] = 1) AS [[wiki]],
			COALESCE((SELECT [[holder]].[[kind]] FROM {{leafen_pages}} AS [[holder]]
				WHERE [[holder]].[[id]] = [[container]].[[page_id]]), '') AS [[page_kind]]
		FROM {{leafen_database_containers}} AS [[container]]
		WHERE [[container]].[[id]] = {:id} AND [[container]].[[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"id": containerID, "workspaceId": workspaceID}).One(&container)
	if errors.Is(err, sql.ErrNoRows) {
		return loadNotionDeletedDatabaseContainer(app, workspaceID, containerID)
	}
	return container, err
}

// fullPage reports whether the database is a full-page database: the
// database of a page of that kind, its page's own top-level block. A wiki's
// database is its page, as a Notion wiki is a database, whatever else the
// wiki's Home holds.
func (container notionDatabaseContainer) fullPage() bool {
	return container.Wiki || container.PageKind == pageKindDatabase && container.ParentBlockID == "" && container.TopLevel
}

func loadNotionDatabaseObject(
	app core.App, request *http.Request, identity notionOAuthBearerIdentity, containerID string,
) (map[string]any, error) {
	container, page, err := loadNotionDatabaseContainer(app, identity, containerID, permissionWorkspaceRead)
	if err != nil {
		return nil, err
	}
	members := []struct {
		ID          string `db:"id"`
		Title       string `db:"title"`
		Icon        string `db:"icon"`
		IconType    string `db:"icon_type"`
		Description string `db:"description"`
		Spans       string `db:"description_spans"`
		TitleSpans  string `db:"title_spans"`
		Cover       string `db:"cover"`
		Linked      string `db:"source_container_id"`
	}{}
	if container.Deleted {
		err = notionDeletedDatabaseMembers(app, container.ID, &members)
	} else {
		err = app.DB().NewQuery(`
		SELECT [[source]].[[id]], [[source]].[[title]], [[source]].[[title_spans]], [[source]].[[icon]], [[source]].[[icon_type]],
			[[source]].[[description]], [[source]].[[description_spans]], [[source]].[[cover]], [[membership]].[[source_container_id]]
		FROM {{leafen_database_container_sources}} AS [[membership]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[membership]].[[data_source_id]]
		WHERE [[membership]].[[container_id]] = {:id} AND NOT ` + databaseSourceGoneWithViewsSQL("{:id}", "[[membership]].[[data_source_id]]") + `
		ORDER BY [[membership]].[[position]], [[membership]].[[data_source_id]]
	`).Bind(dbx.Params{"id": container.ID}).All(&members)
	}
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, errDatabaseRowPageNotFound
	}
	// A database lists its own data sources, not one another database owns that it still shows or one in the trash
	// with its views — as Notion's leaves out a source moved away (seen 2026-10-31) or so trashed (11-19).
	sources := make([]map[string]any, 0, len(members))
	names := make([]string, 0, len(members))
	for _, member := range members {
		if member.Linked != "" {
			continue
		}
		publicSourceID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDataSource, container.WorkspaceID, member.ID)
		if err != nil {
			return nil, err
		}
		names = append(names, notionNameAsRead(app, identity, member.Title, decodeStoredTextSpans(member.TitleSpans)))
		sources = append(sources, map[string]any{"id": publicSourceID})
	}
	// Its sources in the trash are its own still, as Notion lists them (seen
	// 2026-11-11), those that left it as databases of their own in the trash
	// after the rest (notion_data_source_trash.go).
	detached, err := loadNotionDatabaseDetachedSources(app, container)
	if err != nil {
		return nil, err
	}
	for _, member := range detached {
		publicSourceID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDataSource, container.WorkspaceID, member.ID)
		if err != nil {
			return nil, err
		}
		names = append(names, notionNameAsRead(app, identity, member.Title, decodeStoredTextSpans(member.TitleSpans)))
		sources = append(sources, map[string]any{"id": publicSourceID})
	}
	for index, name := range names {
		sources[index]["name"] = notionSourceShownName(name, len(names))
	}
	// A linked database shows another's source, which Notion's API leaves out
	// of it, and has its own name and icon (seen 2026-10-30).
	first := members[0]
	title, description := notionDatabaseShownTitle(first.Title), first.Description
	// The name and the description read with their formatting and mentions,
	// as Notion's, for their reader.
	fileIssuer := newNotionBlockFileCapabilityIssuer(request, identity)
	reader := notionBlockAuthorization{Identity: identity, Page: page}
	titleText, err := notionFormattedTitle(app, reader, fileIssuer, title, decodeStoredTextSpans(first.TitleSpans))
	if err != nil {
		return nil, err
	}
	descriptionText := notionPageTitleRichText(description)
	if first.Linked == "" {
		if descriptionText, err = notionFormattedTitle(app, reader, fileIssuer, description, decodeStoredTextSpans(first.Spans)); err != nil {
			return nil, err
		}
	}
	// Its name is the one rule's (notion_database_api_name.go): of several sources of its own, an inline one goes by
	// the name it was given apart from theirs, or by their names listed, as Notion's API calls it (seen 2026-11-11);
	// its screen heads it with its view's source.
	name := databaseContainerName{}
	if first.Linked == "" {
		if name, err = notionDatabaseNamed(app, container); err != nil {
			return nil, err
		}
	}
	pageNamed := container.fullPage() && (first.Linked != "" || name.Several)
	if !container.fullPage() && name.Several {
		if titleText, err = notionInlineDatabaseName(app, reader, fileIssuer, name); err != nil {
			return nil, err
		}
	}
	// A database of its own sources has an icon of its own apart from theirs (full_page_database_icon.go).
	if first.Linked == "" {
		first.Icon, first.IconType = databaseOwnIconOf(app, container, page.Page, len(sources), first.Icon, first.IconType)
	}
	if first.Linked != "" {
		sources = []map[string]any{}
		name, err := loadLinkedDatabaseName(app, container)
		if err != nil {
			return nil, err
		}
		if titleText, err = notionFormattedTitle(app, reader, fileIssuer, name.Title, decodeStoredTextSpans(name.TitleSpans)); err != nil {
			return nil, err
		}
		descriptionText = notionPageTitleRichText("")
		first.Icon, first.IconType = name.Icon, name.IconType
		// A full-page one's icon is its page's (database_turn_linked_name.go).
		if container.fullPage() {
			first.Icon, first.IconType = page.Page.GetString("icon"), page.Page.GetString("icon_type")
			if first.Icon == "" || first.IconType == "" {
				first.Icon, first.IconType = "", graphDataSourceIconNone
			}
		}
	}
	// A full-page database its page names, of several sources of its own or a linked one, is called by its page's
	// title, with that name's formatting (database_page_name_spans.go).
	if pageNamed {
		if titleText, err = notionDatabasePageName(app, reader, fileIssuer, container, page.Page, first.Linked != "", name); err != nil {
			return nil, err
		}
	}
	publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, container.WorkspaceID, container.ID)
	if err != nil {
		return nil, err
	}
	createdTime, err := notionDatabaseObjectTime(container.Created)
	if err != nil {
		return nil, err
	}
	updatedTime, err := notionDatabaseObjectTime(container.Updated)
	if err != nil {
		return nil, err
	}
	icon, err := notionPageIconWithManagedFile(app, fileIssuer, container.WorkspaceID, first.IconType, first.Icon)
	if err != nil {
		return nil, err
	}
	origin, err := outwardServerOrigin(request)
	if err != nil {
		return nil, err
	}
	// A database's page is the page that holds it: the database itself when
	// it is full page, otherwise the page it sits in.
	publicURL, err := notionPagePublicURL(app, origin, container.WorkspaceID, container.PageID)
	if err != nil {
		return nil, err
	}
	parent, err := notionDatabaseParent(app, container, page.Page)
	if err != nil {
		return nil, err
	}
	var cover any
	locked := false
	if container.fullPage() {
		if cover, err = notionPageCoverWithManagedFile(app, fileIssuer, container.WorkspaceID, page.Page.GetString("cover")); err != nil {
			return nil, err
		}
		locked = page.Page.GetBool("is_locked")
	} else {
		// An inline database's cover is its own.
		if cover, err = notionPageCoverWithManagedFile(app, fileIssuer, container.WorkspaceID, members[0].Cover); err != nil {
			return nil, err
		}
		if container.Deleted {
			locked = notionDeletedDatabaseLocked(container.DeletedContent)
		} else if locked, err = databaseBlockLocked(app, container.Owner, container.PageID, container.BlockID); err != nil {
			return nil, err
		}
	}
	trashed := container.Trashed || page.Page.GetBool("in_trash")
	return map[string]any{
		"object":                 "database",
		"id":                     publicID,
		"data_sources":           sources,
		"title":                  titleText,
		"description":            descriptionText,
		"icon":                   icon,
		"cover":                  cover,
		"parent":                 parent,
		"url":                    notionDatabasePublicURL(origin, publicID),
		"public_url":             publicURL,
		"in_trash":               trashed,
		"is_inline":              !container.fullPage(),
		"is_locked":              locked,
		"database_type":          notionStoredDatabaseType(app, first.ID),
		"show_data_source_title": notionDatabaseShowsSourceTitle(app, container),
		"created_time":           createdTime,
		"last_edited_time":       updatedTime,
	}, nil
}

// notionListedSourceNames is an inline database's sources' names as Notion's
// API names a database of several sources without a name of its own (seen
// 2026-11-11): "A and B", "A, B, and C".
func notionListedSourceNames(names []string) string {
	if len(names) < 3 {
		return strings.Join(names, " and ")
	}
	last := len(names) - 1
	return strings.Join(names[:last], ", ") + ", and " + names[last]
}

// notionDatabasePublicURL is a database's address, its data sources' too, as
// Notion's gives either (seen 2026-11-11, full page or inline): /p/ and the
// database's ID's 32 hexadecimal digits, as a mention of it leads
// (notionMentionURL). The screen opens it as the database: a full-page one's
// page, one in a page over that page (page_links.go).
func notionDatabasePublicURL(origin, databaseID string) string {
	return notionMentionURL(origin, databaseID)
}

// notionDatabaseShowsSourceTitle is whether a database's first view shows its
// source's title, which it does until set not to.
func notionDatabaseShowsSourceTitle(app core.App, container notionDatabaseContainer) bool {
	raw := ""
	// Its first tab, as its views are its tabs (leafen_database_container_views;
	// a view's own container_id is empty for views made since); a deleted
	// one's, the view its block names.
	query := app.DB().NewQuery(`
		SELECT [[view]].[[config]] FROM {{leafen_database_container_views}} AS [[membership]]
		JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]] = [[membership]].[[view_id]]
		WHERE [[membership]].[[container_id]] = {:container} AND [[membership]].[[owner]] = {:owner}
		ORDER BY [[membership]].[[position]], [[view]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"owner": container.Owner, "container": container.ID})
	if container.Deleted {
		query = app.DB().NewQuery(`
			SELECT [[config]] FROM {{leafen_database_views}} WHERE [[id]] = json_extract({:content}, '$.viewId')
		`).Bind(dbx.Params{"content": container.DeletedContent})
	}
	if err := query.Row(&raw); err != nil {
		return true
	}
	config := struct {
		Shows *bool `json:"showDataSourceTitle"`
	}{}
	return json.Unmarshal([]byte(raw), &config) != nil || config.Shows == nil || *config.Shows
}

// notionSourceShownName is a source's name as Notion's API lists a
// database's sources: an unnamed one is "New database" alone, "New data
// source" among two or more (seen 2026-11-12), as the screen's "새 데이터
// 소스" (dataSourceDisplayTitle).
func notionSourceShownName(title string, sources int) string {
	if sources > 1 && strings.TrimSpace(title) == "" {
		return "New data source"
	}
	return notionDatabaseShownTitle(title)
}

// notionDatabaseShownTitle is a database's or source's title as Notion's API
// gives it: "New database" for one without a name (seen 2026-10-30).
func notionDatabaseShownTitle(title string) string {
	if strings.TrimSpace(title) == "" {
		return "New database"
	}
	return title
}

type linkedDatabaseName struct {
	Title, Icon, IconType string
	// TitleSpans is the name's formatting, as stored beside it.
	TitleSpans string
}

// loadLinkedDatabaseName reads a linked database's own name and icon from its
// block (notionDatabaseBlockContent); without a name Notion's API calls it
// "Untitled" (seen 2026-10-30).
func loadLinkedDatabaseName(app core.App, container notionDatabaseContainer) (linkedDatabaseName, error) {
	content, err := notionDatabaseBlockContent(app, container)
	if err != nil {
		return linkedDatabaseName{}, err
	}
	return linkedDatabaseNameOf(content), nil
}

func linkedDatabaseNameOf(content string) linkedDatabaseName {
	fields := struct {
		Title    string          `json:"linkedTitle"`
		Spans    json.RawMessage `json:"linkedTitleSpans"`
		Icon     string          `json:"linkedIcon"`
		IconType string          `json:"linkedIconType"`
	}{}
	_ = json.Unmarshal([]byte(content), &fields)
	name := linkedDatabaseName{Title: strings.TrimSpace(fields.Title), Icon: fields.Icon, IconType: fields.IconType}
	if len(fields.Spans) > 0 && string(fields.Spans) != "null" {
		name.TitleSpans = string(fields.Spans)
	}
	if name.Title == "" {
		name.Title = "Untitled"
	}
	if name.IconType == "" {
		name.IconType = graphDataSourceIconNone
	}
	return name
}
