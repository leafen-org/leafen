package app

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const mcpDatabaseURLPrefix = "database://"

type mcpDatabaseContainerLocation struct {
	ID                string `db:"id"`
	OwnerID           string `db:"owner"`
	WorkspaceID       string `db:"workspace_id"`
	PageID            string `db:"page_id"`
	BlockID           string `db:"block_id"`
	Kind              string `db:"kind"`
	SourceContainerID string `db:"source_container_id"`
	DataSourceID      string `db:"data_source_id"`
}

func parseMCPDatabaseIdentifier(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	// A database's address names it at its block (mcpDatabaseAddress).
	if databaseID, ok := mcpDatabaseLinkID(raw); ok {
		raw = databaseID
	}
	raw = mcpReferenceID(raw)
	if strings.HasPrefix(raw, mcpDatabaseURLPrefix) {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, mcpDatabaseURLPrefix))
	}
	return raw, raw != "" && len(raw) <= maxGraphIdentifierLength &&
		!strings.ContainsAny(raw, "/?#")
}

// resolveMCPDatabaseID is the database a database_id names, as notion-fetch
// gives it: a database's own ID or address, with its dashes or without them
// (mcpLocalID), or, for a full-page database, its page's, which is how fetch
// showed one, its page's left as it turned inline too, as Notion's (2026-11-06: a full-page database's page ID made
// notion-create-view "couldn't finish"). One that names no database here
// isn't one: its caller says so, not that it couldn't finish (2026-11-13).
func resolveMCPDatabaseID(app core.App, raw string) (string, bool) {
	databaseID, ok := parseMCPDatabaseIdentifier(raw)
	if !ok {
		return "", false
	}
	// A template's database, "starter-<hex>", whole (mcp_own_slug_shaped_id.go).
	if own, whole := mcpOwnSlugShapedID(app, raw); whole && mcpDatabaseContainerExists(app, own) {
		return own, true
	}
	if mcpDatabaseContainerExists(app, databaseID) {
		return databaseID, true
	}
	if local := mcpLocalID(app, databaseID); local != databaseID && mcpDatabaseContainerExists(app, local) {
		return local, true
	}
	if containerID := mcpPageAddressDatabaseContainer(app, databaseID); containerID != "" {
		return containerID, true
	}
	return "", false
}

// mcpLocalDataSourceID is the data source an ID or address names, as the
// data source tools take it: collection://ID or its bare ID, the public
// API's, or the address fetch gives a database that holds one data source —
// its own or one it links (2026-11-13: fetch's database addresses weren't
// taken where a data source was asked for). A database of several names
// none of them: its caller asks for one.
func mcpLocalDataSourceID(app core.App, raw string) (string, bool) {
	local := mcpLocalID(app, raw)
	if id, ok := parseMCPDataSourceURL(local); ok {
		return id, true
	}
	databaseID := ""
	if mcpDatabaseContainerExistsToRead(app, local) {
		databaseID = local
	} else if containerID := mcpPageAddressDatabaseContainer(app, local); containerID != "" {
		databaseID = containerID
	}
	if databaseID == "" {
		return parseMCPDataSourceIdentifier(local)
	}
	members, err := loadMCPDatabaseContainerShownMembers(app, databaseID)
	if err != nil || len(members) != 1 {
		return "", false
	}
	return members[0].DataSourceID, true
}

func mcpDatabaseContainerExists(app core.App, databaseID string) bool {
	return mcpDatabaseContainerFound(app, databaseID, false)
}

// mcpDatabaseContainerExistsToRead also finds a database in the trash, which
// fetch reads as Notion's does (mcp_fetch_trash.go).
func mcpDatabaseContainerExistsToRead(app core.App, databaseID string) bool {
	return mcpDatabaseContainerFound(app, databaseID, true)
}

func mcpDatabaseContainerFound(app core.App, databaseID string, trashed bool) bool {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_containers}} AS [[container]]
		WHERE [[container]].[[id]]={:id} AND ({:trashed} OR NOT EXISTS (
			SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
			WHERE [[trash]].[[container_id]]=[[container]].[[id]]
		))
	`).Bind(dbx.Params{"id": databaseID, "trashed": trashed}).Row(&count)
	return err == nil && count != 0
}

func loadMCPDatabaseContainerLocation(
	app core.App, databaseID string,
) (mcpDatabaseContainerLocation, error) {
	return loadMCPDatabaseContainerLocationWith(app, databaseID, false)
}

// loadMCPDatabaseContainerLocationWith with trashed also finds a database in
// the trash, for reading.
func loadMCPDatabaseContainerLocationWith(
	app core.App, databaseID string, trashed bool,
) (mcpDatabaseContainerLocation, error) {
	location := mcpDatabaseContainerLocation{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], [[container]].[[owner]], [[container]].[[workspace_id]],
			[[container]].[[page_id]], [[container]].[[block_id]], [[container]].[[kind]],
			[[container]].[[source_container_id]], [[membership]].[[data_source_id]]
		FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]]=[[container]].[[id]]
		WHERE [[container]].[[id]]={:id} AND ({:trashed} OR NOT EXISTS (
			SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
			WHERE [[trash]].[[container_id]]=[[container]].[[id]]
		))
		ORDER BY [[membership]].[[position]], [[membership]].[[data_source_id]] LIMIT 1
	`).Bind(dbx.Params{"id": databaseID, "trashed": trashed}).One(&location)
	return location, err
}

// mcpDatabaseContainerMember is one ordered member and, for a linked member,
// the database that owns its source.
type mcpDatabaseContainerMember struct {
	DataSourceID      string `db:"data_source_id"`
	SourceContainerID string `db:"source_container_id"`
}

func loadMCPDatabaseContainerMemberships(app core.App, containerID string) ([]mcpDatabaseContainerMember, error) {
	return loadMCPDatabaseContainerMembers(app, containerID, false)
}

// loadMCPDatabaseContainerShownMembers is the members a database lists to an
// agent: one sent to the trash with its views is none, as the API's list of
// a database's sources leaves it out (notion_database_api.go) and as
// Notion's (seen 2026-11-19).
func loadMCPDatabaseContainerShownMembers(app core.App, containerID string) ([]mcpDatabaseContainerMember, error) {
	return loadMCPDatabaseContainerMembers(app, containerID, true)
}

func loadMCPDatabaseContainerMembers(app core.App, containerID string, shown bool) ([]mcpDatabaseContainerMember, error) {
	members := []mcpDatabaseContainerMember{}
	err := app.DB().NewQuery(`
		SELECT [[data_source_id]], [[source_container_id]] FROM {{leafen_database_container_sources}} AS [[membership]]
		WHERE [[container_id]]={:id} AND (NOT {:shown} OR NOT ` +
		databaseSourceGoneWithViewsSQL("{:id}", "[[membership]].[[data_source_id]]") + `)
		ORDER BY [[position]], [[data_source_id]]
	`).Bind(dbx.Params{"id": containerID, "shown": shown}).All(&members)
	return members, err
}

// loadMCPDatabaseContainerOwnMembers returns the sources the database owns,
// leaving out the sources it links from other databases.
func loadMCPDatabaseContainerOwnMembers(app core.App, containerID string) ([]string, error) {
	return loadMCPDatabaseContainerOwn(app, containerID, false)
}

// loadMCPDatabaseContainerShownOwnMembers is those an agent is shown
// (loadMCPDatabaseContainerShownMembers).
func loadMCPDatabaseContainerShownOwnMembers(app core.App, containerID string) ([]string, error) {
	return loadMCPDatabaseContainerOwn(app, containerID, true)
}

func loadMCPDatabaseContainerOwn(app core.App, containerID string, shown bool) ([]string, error) {
	memberships, err := loadMCPDatabaseContainerMembers(app, containerID, shown)
	if err != nil {
		return nil, err
	}
	members := make([]string, 0, len(memberships))
	for _, member := range memberships {
		if member.SourceContainerID == "" {
			members = append(members, member.DataSourceID)
		}
	}
	return members, nil
}

// resolveMCPDatabaseContainerSource returns the source container that owns
// dataSourceID as one of its ordered members, following the member's one
// linked hop.
func resolveMCPDatabaseContainerSource(
	app core.App,
	container mcpDatabaseContainerLocation,
	dataSourceID string,
) (mcpDatabaseContainerLocation, error) {
	memberships, err := loadMCPDatabaseContainerMemberships(app, container.ID)
	if err != nil {
		return mcpDatabaseContainerLocation{}, err
	}
	index := slices.IndexFunc(memberships, func(member mcpDatabaseContainerMember) bool {
		return member.DataSourceID == dataSourceID
	})
	if index < 0 {
		return mcpDatabaseContainerLocation{}, mcpDenied("that data source isn't one of this database's")
	}
	source := container
	if sourceContainerID := memberships[index].SourceContainerID; sourceContainerID != "" {
		loaded, err := loadMCPDatabaseContainerLocation(app, sourceContainerID)
		if err != nil || loaded.OwnerID != container.OwnerID || loaded.WorkspaceID != container.WorkspaceID {
			return mcpDatabaseContainerLocation{}, mcpDenied("the database that holds that data source is gone or elsewhere")
		}
		owned, err := loadMCPDatabaseContainerOwnMembers(app, loaded.ID)
		if err != nil {
			return mcpDatabaseContainerLocation{}, err
		}
		if !slices.Contains(owned, dataSourceID) {
			return mcpDatabaseContainerLocation{}, mcpDenied("that data source isn't one of this database's")
		}
		source = loaded
	}
	if source.Kind != databaseContainerKindSource {
		return mcpDatabaseContainerLocation{}, mcpDenied("that is a database showing another's data, not a data source's own")
	}
	source.DataSourceID = dataSourceID
	return source, nil
}

func fetchMCPDatabaseContainer(
	ctx context.Context,
	app core.App,
	databaseID string,
) (*mcp.CallToolResult, notionMCPFetchOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPFetchOutput{}, err
	}
	output := notionMCPFetchOutput{}
	err = runNotionReadSnapshot(ctx, app, func(txApp core.App) error {
		read, err := readMCPDatabaseContainer(txApp, authority, databaseID, false)
		if err != nil {
			return err
		}
		output = notionMCPFetchOutput{
			Metadata: notionMCPFetchMetadata{Type: "database"}, Title: read.Title, URL: read.URL, Text: read.Text,
		}
		// A database is verified as a page is, on its first data source. Its
		// fetch gives no verification while it has none, as Notion's (seen
		// 2026-11-14: a database's and a data source's fetch carry no
		// "verification", a page's {"state":"unverified"}); one verified
		// gives its verification as a page's fetch does.
		verification, err := mcpVerificationFact(txApp, verificationTarget{DataSourceID: read.FirstSourceID}, time.Now(), authority.clock())
		if err != nil {
			return err
		}
		if verification["state"] != "unverified" {
			output.Verification = &verification
		}
		return nil
	})
	if err != nil {
		return nil, notionMCPFetchOutput{}, mcpDataSourceQueryError(err)
	}
	return nil, output, nil
}

// mcpReadDatabase is a database as the connection reads it: its title and
// address as fetch names them, its fetch text and its first data source.
type mcpReadDatabase struct {
	Title         string
	URL           string
	Text          string
	FirstSourceID string
}

// readMCPDatabaseContainer reads a database the connection may open, as
// fetch gives it; sourcesOnly leaves its views out, as the database a
// create or update answer gives (mcpDatabaseAnswer).
func readMCPDatabaseContainer(
	txApp core.App, authority mcpAuthority, databaseID string, sourcesOnly bool,
) (mcpReadDatabase, error) {
	// One in the trash reads too, marked deleted (mcp_fetch_trash.go).
	location, err := loadMCPDatabaseContainerLocationWith(txApp, databaseID, true)
	if errors.Is(err, sql.ErrNoRows) {
		return mcpReadDatabase{}, mcpDenied("that database is gone")
	}
	if err != nil {
		return mcpReadDatabase{}, err
	}
	page, err := authorizePageBody(
		txApp, authority.PrincipalID, location.PageID, permissionWorkspaceRead,
	)
	if err != nil || page.Workspace.OwnerID != location.OwnerID ||
		page.Workspace.WorkspaceID != location.WorkspaceID {
		return mcpReadDatabase{}, mcpRefusedUnlessStorage(errMCPCantRead, err)
	}
	if !mcpAuthorityReachesPage(txApp, authority, page.Page) {
		return mcpReadDatabase{}, errMCPOutsideTokenPages
	}
	workspace, err := selectMCPWorkspace(txApp, authority, mcpWorkspaceInput{
		WorkspaceID: location.WorkspaceID,
	}, permissionWorkspaceRead)
	if err != nil || workspace.WorkspaceID != location.WorkspaceID {
		return mcpReadDatabase{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	memberships, err := loadMCPDatabaseContainerShownMembers(txApp, location.ID)
	if err != nil {
		return mcpReadDatabase{}, err
	}
	// Each member is read through the database that owns its source, so a
	// linked member also needs read access to its original page.
	dataSources := make([]string, 0, len(memberships))
	schemas := map[string][]notionMCPDataSourceProperty{}
	title, firstSourceID, names, sourceIDs := "", "", []string{}, []string{}
	for index, membership := range memberships {
		memberSource, err := resolveMCPDatabaseContainerSource(txApp, location, membership.DataSourceID)
		if err != nil {
			return mcpReadDatabase{}, err
		}
		member, err := loadMCPDataSourceLocationToRead(txApp, memberSource.DataSourceID)
		if err != nil || member.OwnerID != memberSource.OwnerID ||
			member.WorkspaceID != memberSource.WorkspaceID || member.PageID != memberSource.PageID {
			return mcpReadDatabase{}, mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
		}
		if memberSource.ID != location.ID {
			sourcePage, err := authorizePageBody(
				txApp, authority.PrincipalID, memberSource.PageID, permissionWorkspaceRead,
			)
			if err != nil || sourcePage.Workspace.OwnerID != location.OwnerID ||
				sourcePage.Workspace.WorkspaceID != location.WorkspaceID {
				return mcpReadDatabase{}, mcpRefusedUnlessStorage(errMCPCantRead, err)
			}
			// A token narrowed to chosen pages reads no database outside them,
			// its name and properties included.
			if !mcpAuthorityReachesPage(txApp, authority, sourcePage.Page) {
				return mcpReadDatabase{}, errMCPOutsideTokenPages
			}
		}
		properties, _, err := loadMCPDataSourceSchema(txApp, member, authority.PrincipalID)
		if err != nil {
			return mcpReadDatabase{}, err
		}
		templates, err := loadMCPDataSourceTemplates(txApp, member)
		if err != nil {
			return mcpReadDatabase{}, err
		}
		element, err := mcpDataSourceElement(txApp, authority, member, properties, templates)
		if err != nil {
			return mcpReadDatabase{}, err
		}
		dataSources = append(dataSources, element)
		schemas[member.ID] = properties
		// Its sources' names as the agent reads them (page_name_mentions.go).
		names, sourceIDs = append(names, mcpSourceNameAsRead(txApp, authority, member.ID, member.Title)), append(sourceIDs, member.ID)
		if index == 0 {
			title, firstSourceID = names[0], member.ID
		}
	}
	if len(dataSources) == 0 {
		return mcpReadDatabase{}, mcpDenied("none of that database's data sources is one this account can open")
	}
	fetchedViews := []mcpFetchedView{}
	if !sourcesOnly {
		views, err := loadMCPDatabaseContainerViews(txApp, location)
		if err != nil {
			return mcpReadDatabase{}, err
		}
		for _, view := range views {
			config, err := mcpFetchedViewConfig(txApp, authority.PrincipalID, view, view.DataSourceID, schemas[view.DataSourceID])
			if err != nil {
				return mcpReadDatabase{}, err
			}
			fetchedViews = append(fetchedViews, mcpFetchedView{URL: mcpViewURLPrefix + view.ID, Config: config})
		}
	}
	ancestors, err := mcpDatabaseAncestors(txApp, authority, location.PageID, location.ID)
	if err != nil {
		return mcpReadDatabase{}, err
	}
	holder, err := txApp.FindRecordById(pagesCollection, location.PageID)
	if err != nil {
		return mcpReadDatabase{}, mcpDenied("the page that database is on is gone")
	}
	// A full-page database is its page, and so is a wiki's database.
	wikiPageID, err := wikiPageOfDatabase(txApp, location.ID)
	if err != nil {
		return mcpReadDatabase{}, err
	}
	inline := holder.GetString("kind") != pageKindDatabase && wikiPageID == ""
	url := mcpDatabaseAddress(authority, location.ID)
	locked := holder.GetBool("is_locked")
	if inline {
		if locked, err = databaseBlockLocked(txApp, location.OwnerID, location.PageID, location.BlockID); err != nil {
			return mcpReadDatabase{}, err
		}
	}
	// One left alone by the trash goes by its one source's name, as the API's
	// title (notion_database_api.go).
	all, err := loadMCPDatabaseContainerMemberships(txApp, location.ID)
	if err != nil {
		return mcpReadDatabase{}, err
	}
	leftAlone := len(memberships) == 1 && memberships[0].SourceContainerID == "" && len(all) > 1
	// Its name reads as Notion's fetch names a title (mcpDatabaseNameAsShown).
	fullPage := ""
	if !inline {
		fullPage = location.PageID
	}
	if own, spans, named := databaseOwnName(txApp, location.OwnerID, location.PageID, location.BlockID, holder); named && !leftAlone {
		own = mcpDatabaseOwnNameAsRead(txApp, authority, location.PageID, own, spans)
		title = mcpDatabaseShownTitle(txApp, authority.PrincipalID, mcpDatabaseNameAsShown(txApp, authority, location.WorkspaceID, "", fullPage, spans, own))
	} else if location.Kind == databaseContainerKindLinked {
		// A linked one without a name of its own (mcp_linked_database_name.go).
		title = mcpLinkedDatabaseShownTitle(txApp, authority.PrincipalID, title)
	} else {
		// One of several sources by their names (mcp_database_several_names.go),
		// each as it shows, a full-page one alone by its page's too.
		if len(names) > 1 {
			fullPage = ""
		}
		// One it shows from another database by its linked view's name.
		listed := make([]databaseSourceTitle, len(names))
		for index := range names {
			listed[index] = databaseSourceTitle{
				Title:  mcpDatabaseNameAsShown(txApp, authority, location.WorkspaceID, sourceIDs[index], fullPage, nil, names[index]),
				Linked: memberships[index].SourceContainerID != "",
			}
		}
		title = mcpDatabaseSourcesShownTitle(txApp, authority.PrincipalID, listed)
	}
	deleted, err := mcpDatabaseDeleted(txApp, location.ID, location.PageID)
	if err != nil {
		return mcpReadDatabase{}, err
	}
	text, err := mcpDatabaseFetchText(mcpFetchedDatabase{
		URL: url, Title: title, Inline: inline, Deleted: deleted, Locked: locked,
		Ancestors: ancestors, DataSources: dataSources, Views: fetchedViews, SourcesOnly: sourcesOnly,
	})
	if err != nil {
		return mcpReadDatabase{}, err
	}
	return mcpReadDatabase{Title: title, URL: url, Text: text, FirstSourceID: firstSourceID}, nil
}

// notionMCPDatabaseAnswer is notion-create-database's and
// notion-update-data-source's answer, as Notion's (seen 2026-11-14): what
// was done, then the database as fetch gives it, its data sources with their
// configurable state and SQLite tables but not its views ("Created database:
// <database …>", "Updated data source: <database …>"). Warnings name the
// page_layout modules Leafen doesn't draw, left out.
type notionMCPDatabaseAnswer struct {
	Result   string   `json:"result"`
	Warnings []string `json:"warnings,omitempty"`
}

const (
	mcpCreatedDatabaseLead   = "Created database: "
	mcpUpdatedDataSourceLead = "Updated data source: "
)

// mcpDatabaseAnswer reads a database just made or changed for its answer.
func mcpDatabaseAnswer(ctx context.Context, app core.App, containerID, lead string) (notionMCPDatabaseAnswer, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return notionMCPDatabaseAnswer{}, err
	}
	read := mcpReadDatabase{}
	err = runNotionReadSnapshot(ctx, app, func(txApp core.App) error {
		var readErr error
		read, readErr = readMCPDatabaseContainer(txApp, authority, containerID, true)
		return readErr
	})
	if err != nil {
		return notionMCPDatabaseAnswer{}, mcpDataSourceQueryError(err)
	}
	return notionMCPDatabaseAnswer{Result: lead + read.Text}, nil
}
