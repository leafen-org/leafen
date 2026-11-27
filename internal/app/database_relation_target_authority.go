package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A relation may reach a database on another page the actor can read, and
// keeps the rows there it already holds (database_relation_held_targets.go).
// The target data source and the rows a relation value names enter a
// mutation as read-only references; whatever the mutation writes, parents or
// links in a dependency stays on this page, so another page's rows are still
// refused there.
//
// A row a relation value names is one the actor reads, on this page or
// another: an item whose sharing is unlinked from its database, that their
// views leave out (database_row_unlinked_access.go), is
// refused as one on a page they can't read, as Notion's API refuses a page
// it can't see (seen 2026-11-09: 404, a page it finds none of), unless
// the value already holds it. A server rewrite of what is stored — a
// migration, a conversion, a relation pair's other side — keeps what the
// values say.

type relationTargetReference struct {
	ID           string `db:"id"`
	Owner        string `db:"owner"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
}

type relationTargetPageAccess struct {
	app         core.App
	principalID string
	current     pageAuthorization
	pageID      string
	allowed     map[string]bool
}

func newRelationTargetPageAccess(
	app core.App, principalID string, current pageAuthorization, pageID string,
) *relationTargetPageAccess {
	return &relationTargetPageAccess{
		app: app, principalID: principalID, current: current, pageID: pageID, allowed: map[string]bool{pageID: true},
	}
}

// readable reports whether the actor may read the page. Server rewrites
// without an actor already hold the workspace's authority, and a page an
// import is building reaches the other pages it builds (pagesStagedTogether),
// which no one reads until it publishes them.
func (access *relationTargetPageAccess) readable(pageID string) bool {
	if allowed, checked := access.allowed[pageID]; checked {
		return allowed
	}
	allowed := false
	if access.principalID == "" {
		page, err := access.app.FindRecordById(pagesCollection, pageID)
		allowed = err == nil && page.GetString("workspace") == access.current.Workspace.WorkspaceID
	} else {
		authorized, err := authorizePage(access.app, access.principalID, pageID, permissionWorkspaceRead)
		allowed = err == nil && authorized.Workspace.OwnerID == access.current.Workspace.OwnerID &&
			authorized.Workspace.WorkspaceID == access.current.Workspace.WorkspaceID
		if !allowed && pageStagedByImport(access.app, access.current.Page) {
			allowed, _ = pagesStagedTogether(access.app, access.current, access.pageID, pageID)
		}
	}
	access.allowed[pageID] = allowed
	return allowed
}

// authorizeRelationTargetReferences adds the readable other-page sources that
// relation properties target to `sources`, and returns the other-page rows
// that relation values may name: the readable ones, and the ones a property
// already relates (keepHeldRelationTargets), with how many values lost rows
// deleted for good. A row the actor can't read that a value doesn't hold yet
// is refused; rewrite (a server rewrite of stored values) leaves the rows'
// own sharing out of it.
func authorizeRelationTargetReferences(
	app core.App,
	principalID string,
	current pageAuthorization,
	pageID string,
	operations []graphOperation,
	sources map[string]graphEntityReference,
	rewrite bool,
) (map[string]graphEntityReference, int, error) {
	sourceIDs := map[string]struct{}{}
	// Every row a value names, and those of them the change doesn't write.
	targetIDs := map[string]struct{}{}
	rowIDs := map[string]struct{}{}
	written := map[string]struct{}{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			if target := graphRelationTarget(operation.Property.Type, operation.Property.Config); target != "" {
				sourceIDs[target] = struct{}{}
			}
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			written[operation.Row.ID] = struct{}{}
			if operation.Row.SubitemParentID != nil {
				written[*operation.Row.SubitemParentID] = struct{}{}
			}
			for _, value := range operation.Row.Values {
				if value.Type != graphValueTypeRelation {
					continue
				}
				for _, targetID := range graphRelationRowIDs(value.Value) {
					targetIDs[targetID] = struct{}{}
				}
			}
		case operation.Kind == graphOperationKindDeleteRow && operation.RowDeletion != nil:
			written[operation.RowDeletion.ID] = struct{}{}
		case operation.Kind == graphOperationKindUpsertDependencyEdge && operation.DependencyEdge != nil:
			written[operation.DependencyEdge.PredecessorRowID] = struct{}{}
			written[operation.DependencyEdge.SuccessorRowID] = struct{}{}
		}
	}
	for id := range targetIDs {
		if _, writes := written[id]; !writes {
			rowIDs[id] = struct{}{}
		}
	}
	access := newRelationTargetPageAccess(app, principalID, current, pageID)
	targets, err := loadRelationTargetReferences(app, "leafen_data_sources", "''", current.Workspace.OwnerID, pageID, sourceIDs)
	if err != nil {
		return nil, 0, err
	}
	for _, target := range targets {
		if access.readable(target.PageID) {
			sources[target.ID] = graphEntityReference{ID: target.ID, Owner: target.Owner, PageID: target.PageID}
		}
	}
	// A relation to a data source of another workspace is refused as such
	// (other_workspace_refusal.go), not as one not found.
	for id := range sourceIDs {
		if _, admitted := sources[id]; admitted || principalID == "" {
			continue
		}
		if workspaceID, sourcePage := dataSourceInOtherWorkspace(app, principalID, id, current.Workspace.WorkspaceID); workspaceID != "" {
			return nil, 0, otherWorkspaceRefused(app, "relationDatabaseId", workspaceCrossing{
				subject: "a relation made here", from: current.Workspace.WorkspaceID, place: "its data source " + id,
				to: workspaceID, link: true, whole: sourcePage,
			})
		}
	}
	// The rows named, wherever they are, and those named nowhere, gone for
	// good. Another page's rows the actor reads come in as references; this
	// page's are the mutation's own.
	named, err := loadRelationTargetRows(app, targetIDs)
	if err != nil {
		return nil, 0, err
	}
	hidden, err := relationTargetRowsHidden(app, principalID, named, rewrite)
	if err != nil {
		return nil, 0, err
	}
	rows, unreadable := map[string]graphEntityReference{}, map[string]graphEntityReference{}
	gone := make(map[string]struct{}, len(rowIDs))
	for id := range rowIDs {
		gone[id] = struct{}{}
	}
	for _, target := range named {
		delete(gone, target.ID)
		if target.Owner != current.Workspace.OwnerID {
			continue
		}
		_, unwritten := rowIDs[target.ID]
		elsewhere := unwritten && target.PageID != pageID
		reference := graphEntityReference{
			ID: target.ID, Owner: target.Owner, PageID: target.PageID, DataSourceID: target.DataSourceID,
		}
		switch {
		case hidden[target.ID] || elsewhere && !access.readable(target.PageID):
			unreadable[target.ID] = reference
		case elsewhere:
			rows[target.ID] = reference
		}
	}
	dropped, err := keepHeldRelationTargets(
		app, current.Workspace.OwnerID, pageID, operations, rows, unreadable, gone,
	)
	if err != nil {
		return nil, 0, err
	}
	// A row the actor can't read that a value doesn't hold yet is refused;
	// one of this page's that it holds stays the mutation's own.
	for id, reference := range unreadable {
		if _, admitted := rows[id]; !admitted {
			return nil, 0, relationTargetUnreadable(app, principalID, current, id)
		}
		if reference.PageID == pageID {
			delete(rows, id)
		}
	}
	return rows, dropped, nil
}

var errRelationTargetUnreadable = graphInputError(errorCodeGraphReferenceInvalid, "A relation names a page you can't open.")

// relationTargetUnreadable refuses a relation value naming a page the actor
// can't open here: one of another workspace they read as such.
func relationTargetUnreadable(app core.App, principalID string, current pageAuthorization, rowID string) error {
	workspaceID := ""
	if principalID != "" {
		workspaceID = pageInOtherWorkspace(app, principalID, rowID, current.Workspace.WorkspaceID)
	}
	if workspaceID == "" {
		return errRelationTargetUnreadable
	}
	return otherWorkspaceRefused(app, "values", workspaceCrossing{
		subject: "a relation value made here", from: current.Workspace.WorkspaceID, place: "the page " + rowID, to: workspaceID,
		link: true, whole: rowDatabasePage(app, rowID),
	})
}

// relationTargetRowsHidden are the named rows the actor's views leave out:
// unlinked items they have no role in.
func relationTargetRowsHidden(
	app core.App, principalID string, named []relationTargetReference, rewrite bool,
) (map[string]bool, error) {
	hidden := map[string]bool{}
	if principalID == "" || rewrite || len(named) == 0 {
		return hidden, nil
	}
	ids := make([]string, len(named))
	for index, target := range named {
		ids[index] = target.ID
	}
	access, err := unlinkedRowAccessFor(app, principalID, ids)
	if err != nil || access == nil {
		return hidden, err
	}
	for _, id := range ids {
		hidden[id] = access.hides(id)
	}
	return hidden, nil
}

// loadRelationTargetRows finds the named rows on any page.
func loadRelationTargetRows(app core.App, ids map[string]struct{}) ([]relationTargetReference, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(workspaceExportSortedKeys(ids))
	if err != nil {
		return nil, err
	}
	references := []relationTargetReference{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[page_id]], [[data_source_id]] FROM {{leafen_database_rows}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[id]]
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&references)
	return references, err
}

// loadRelationTargetReferences finds the named entities that live on
// another page of the same content owner.
func loadRelationTargetReferences(
	app core.App, table, dataSourceColumn, owner, pageID string, ids map[string]struct{},
) ([]relationTargetReference, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	params := dbx.Params{"owner": owner, "pageId": pageID}
	placeholders := make([]string, len(ordered))
	for index, id := range ordered {
		key := fmt.Sprintf("target%d", index)
		params[key] = id
		placeholders[index] = "{:" + key + "}"
	}
	references := []relationTargetReference{}
	err := app.DB().NewQuery(fmt.Sprintf(`
		SELECT [[id]], [[owner]], [[page_id]], %s AS [[data_source_id]]
		FROM {{%s}}
		WHERE [[owner]] = {:owner} AND [[page_id]] != {:pageId} AND [[id]] IN (%s)
		ORDER BY [[id]]
	`, dataSourceColumn, table, strings.Join(placeholders, ", "))).Bind(params).All(&references)
	return references, err
}
