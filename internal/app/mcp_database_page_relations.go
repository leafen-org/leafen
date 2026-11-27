package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An agent names a database page's related pages by their addresses, their
// Leafen IDs or their Public API IDs. A page the value can't take — none the
// agent can reach here, one of another workspace, a page or database that is
// no database page, or a page of another database than the relation's — stops
// the write, each named as the agent wrote it with why, and the value stays
// as it was. Notion's API refuses a page it can't find and leaves out, saying
// nothing, a page of another database, emptying the value when nothing else
// was given (seen 2026-11-24, both API versions), and Notion's MCP leaves out
// both (seen 2026-11-10); taken at its word, an agent told the person a
// relation was made where it had emptied it (2026-11-24). What is no page's
// address or ID at all is refused as written (mcpInvalidReference). A page in
// the trash is taken, as Notion's API takes one, and rows the value held
// that the agent can't open stay (database_relation_closed_targets.go).

// mcpDatabasePageRelations is a relation's pages as the Public API's
// relation value, of those the agent names.
func mcpDatabasePageRelations(
	app core.App,
	authority mcpAuthority,
	authorization notionDatabaseRowPageAuthorization,
	property storedNotionDatabaseRowPageProperty,
	references []string,
	sent mcpSentReferences,
) ([]map[string]any, error) {
	target, err := notionDatabaseRowPageRelationTarget(property)
	if err != nil {
		return nil, err
	}
	reach := mcpRelationReach(authority)
	pages := make([]map[string]any, 0, len(references))
	named := map[string]bool{}
	// reachable stays while every page refused is none the agent can
	// reach, across while every other is a page of another workspace.
	refused, reachable, across := []string{}, true, true
	refuse := func(reference, why string, found bool) {
		refused = append(refused, strconv.Quote(strings.TrimSpace(reference))+" "+why)
		reachable = reachable && !found
	}
	for _, reference := range references {
		found, err := mcpRelationRowID(app, authorization, reference)
		if err != nil {
			return nil, err
		}
		if found.place != mcpRelationRowHere {
			switch {
			case found.place == mcpRelationNowhere || !mcpAccountOpens(app, authority, mcpPage(reference)):
				refuse(reference, mcpRelationPlaceWords[mcpRelationNowhere], false)
			case found.place == mcpRelationElsewhere:
				refuse(reference, "is in workspace "+mcpWorkspaceNamed(app, found.workspaceID)+
					"; a relation relates pages of its own workspace, so relate one here, or copy that page here first "+
					"(leafen_call POST /pages/"+found.rowID+"/duplicate with destinationWorkspaceId)", true)
			default:
				across = false
				refuse(reference, mcpRelationPlaceWords[found.place], true)
			}
			continue
		}
		row, open, err := notionRelationRowOpen(app, authorization.Identity, reach, found.rowID)
		if err != nil {
			return nil, err
		}
		if !open {
			refuse(reference, mcpRelationPlaceWords[mcpRelationNowhere], false)
			continue
		}
		if row.Authority.DataSourceID != target {
			across = false
			refuse(reference, "is a page of "+mcpRelationSourceNamed(app, row.Authority.DataSourceID), true)
			continue
		}
		// A page named twice, by its address and its ID, is related once.
		if named[found.rowID] {
			continue
		}
		named[found.rowID] = true
		publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, authorization.Identity.WorkspaceID, found.rowID)
		if err != nil {
			return nil, err
		}
		sent.note(publicID, reference)
		pages = append(pages, map[string]any{"id": publicID})
	}
	if len(refused) == 0 {
		return pages, nil
	}
	// Nothing the agent can reach is answered as a page not found, pages of
	// another workspace as such (mcp_other_workspace.go), and any other misfit
	// as a value given wrong.
	kind := errMCPToolInvalid
	switch {
	case reachable:
		kind = errMCPPageMissing
	case across:
		kind = errMCPOtherWorkspace
	}
	source, crossed := mcpRelationSourceNamed(app, target), ""
	if kind == errMCPOtherWorkspace {
		crossed = " " + otherWorkspaceSentence + ": create such a page here and copy its content over."
	}
	return nil, fmt.Errorf("%w: The relation %q takes pages of %s in this workspace, so nothing was changed: %s.%s "+
		"Its pages are listed by notion-fetch or notion-query-data-sources of %s.",
		kind, property.Name, source, strings.Join(refused, "; "), crossed, mcpDataSourceURL(app, target))
}

// mcpRelationPlace is where what a relation's reference names lies.
type mcpRelationPlace int

const (
	// mcpRelationNowhere is no page the agent can reach here.
	mcpRelationNowhere mcpRelationPlace = iota
	// mcpRelationRowHere is a database page of the agent's workspace.
	mcpRelationRowHere
	// mcpRelationNotRow is a page, database or data source that is no database page.
	mcpRelationNotRow
	// mcpRelationElsewhere is a database page of another workspace.
	mcpRelationElsewhere
)

// mcpRelationPlaceWords say why a reference doesn't fit, after it; a page
// of another workspace is said with its workspace.
var mcpRelationPlaceWords = map[mcpRelationPlace]string{
	mcpRelationNowhere: "names no page you can reach here",
	mcpRelationNotRow:  "isn't a page of a database",
}

type mcpRelationFound struct {
	rowID, workspaceID string
	place              mcpRelationPlace
}

// mcpRelationRowID is the Leafen ID of the database page a reference names
// and where that lies; a reference that is no page's address or ID is
// refused, named as written.
func mcpRelationRowID(
	app core.App, authorization notionDatabaseRowPageAuthorization, reference string,
) (mcpRelationFound, error) {
	workspaceID := authorization.Identity.WorkspaceID
	at := func(id, workspace string, row bool) mcpRelationFound {
		found := mcpRelationFound{rowID: id, workspaceID: workspace, place: mcpRelationRowHere}
		switch {
		case !row:
			found.place = mcpRelationNotRow
		case workspace != workspaceID:
			found.place = mcpRelationElsewhere
		}
		return found
	}
	id := mcpLocalID(app, reference)
	if sourceID, ok := parseMCPDataSourceURL(id); ok {
		return mcpRelationOtherObject(app, sourceID), nil
	}
	page, err := app.FindRecordById(pagesCollection, id)
	switch {
	case err == nil:
		return at(id, page.GetString("workspace"), isDatabaseRowPageRecord(page)), nil
	case !errors.Is(err, sql.ErrNoRows):
		return mcpRelationFound{}, err
	case validNotionCompatibleInternalID(id):
		return mcpRelationOtherObject(app, id), nil
	}
	publicID, ok := normalizeNotionUUIDRequest(id)
	if !ok {
		return mcpRelationFound{}, mcpInvalidReference(reference)
	}
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, publicID)
	if err == nil {
		return at(identity.InternalID, identity.Workspace, true), nil
	}
	for _, kind := range []string{notionObjectIdentityDatabase, notionObjectIdentityDataSource} {
		if identity, err := resolveNotionObjectIdentity(app, kind, publicID); err == nil {
			return at(identity.InternalID, identity.Workspace, false), nil
		}
	}
	return mcpRelationFound{}, nil
}

// mcpRelationOtherObject is a data source or a database's container, named
// by its own ID, wherever it lies: none of them is a database page.
func mcpRelationOtherObject(app core.App, id string) mcpRelationFound {
	var workspace string
	if err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": id}).Row(&workspace); err != nil {
		containerID, found := resolveMCPDatabaseID(app, id)
		if !found {
			return mcpRelationFound{}
		}
		location, err := loadMCPDatabaseContainerLocationWith(app, containerID, true)
		if err != nil {
			return mcpRelationFound{}
		}
		workspace = location.WorkspaceID
	}
	return mcpRelationFound{rowID: id, workspaceID: workspace, place: mcpRelationNotRow}
}

// mcpRelationSourceNamed is a data source as a relation's refusal names it:
// its collection:// address, with its title when it has one.
func mcpRelationSourceNamed(app core.App, dataSourceID string) string {
	named := mcpDataSourceURL(app, dataSourceID)
	var title string
	if err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": dataSourceID}).Row(&title); err == nil && strings.TrimSpace(title) != "" {
		named += " (" + strconv.Quote(strings.TrimSpace(title)) + ")"
	}
	return named
}
