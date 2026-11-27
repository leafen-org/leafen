package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A relation names to its reader only the rows they may open, and a write
// keeps the rows its writer can't. A row is closed to someone on a page they
// can't read, left out of their views (an unlinked item they have no role
// in), and, for a Public API connection
// or an agent narrowed to some pages, on a page its grant or token doesn't
// reach. The screen shows such a row as "사용 권한 없음", with no way to take
// it out (PropertyRelationCellView); the Public API and MCP leave it out of
// what they read (the Public API the rows in the trash too, as Notion's API
// does with a trashed page, where Notion's MCP fetch gives one, seen
// 2026-11-10; database_row_trash_relations.go). So a write
// that leaves a closed row out of a value — a cleared cell, the Public API's
// PATCH, which replaces the whole value, or an agent's update of what fetch
// gave it — keeps it where the value held it: no one takes out what they
// can't open, as no one puts it in (authorizeRelationTargetReferences), and
// the row's own side of a two-way pair stays. What Notion's API does with a
// page its integration can't see couldn't be made under the test page; it
// was seen dropping a trashed page a PATCH left out (2026-11-10), where
// Leafen keeps it (keepTrashedRelationTargets).

// relationReach is whether an agent's token reaches a row's page; nil
// reaches every page its person may open.
type relationReach func(app core.App, row *core.Record) bool

// mcpRelationReach is an agent's token's reach.
func mcpRelationReach(authority mcpAuthority) relationReach {
	if !authority.PagesNarrowed {
		return nil
	}
	return func(app core.App, row *core.Record) bool { return mcpAuthorityReachesPage(app, authority, row) }
}

// relationRowOpener says whether its writer or reader may open a related
// row: a person as their screen does (authorizeDatabaseRowPage), a Public
// API connection or an agent as the Public API reads a row page
// (notionRelationRowOpen), within the agent's token's pages. It keeps what
// it learned for the one change or answer it serves.
type relationRowOpener func(rowID string) (bool, error)

// relationRowOpenerOf is the opener of whoever writes in ctx, in the
// workspace.
func relationRowOpenerOf(ctx context.Context, app core.App, principalID, workspaceID string) relationRowOpener {
	if ctx != nil {
		if authority, ok := ctx.Value(mcpAuthorityContextKey{}).(mcpAuthority); ok {
			return mcpRelationRowOpener(app, authority, workspaceID)
		}
		identity, ok := connectionAuthorFrom(ctx)
		if !ok {
			identity, ok = ctx.Value(personalAccessTokenContextKey{}).(notionOAuthBearerIdentity)
		}
		if ok {
			return memoRelationRowOpener(func(rowID string) (bool, error) {
				_, open, err := notionRelationRowOpen(app, identity, nil, rowID)
				return open, err
			})
		}
	}
	return memoRelationRowOpener(func(rowID string) (bool, error) {
		_, err := authorizeDatabaseRowPage(app, principalID, rowID, permissionWorkspaceRead)
		if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
			return false, nil
		}
		return err == nil, err
	})
}

// mcpRelationRowOpener is an agent's opener in a workspace.
func mcpRelationRowOpener(app core.App, authority mcpAuthority, workspaceID string) relationRowOpener {
	identity, reach := mcpNotionIdentity(authority, workspaceID), mcpRelationReach(authority)
	return memoRelationRowOpener(func(rowID string) (bool, error) {
		_, open, err := notionRelationRowOpen(app, identity, reach, rowID)
		return open, err
	})
}

func memoRelationRowOpener(open func(string) (bool, error)) relationRowOpener {
	known := map[string]bool{}
	return func(rowID string) (bool, error) {
		if answer, seen := known[rowID]; seen {
			return answer, nil
		}
		answer, err := open(rowID)
		if err != nil {
			return false, err
		}
		known[rowID] = answer
		return answer, nil
	}
}

// notionRelationRowOpen is a related row as a connection opens it: its row
// page read as the Public API reads one — a row on a page its person can't
// read, one their views leave out, or one on a page its grant doesn't reach
// is not found — within the reach of an agent narrowed to some pages. A row
// in the trash opens: a value may name it, as Notion's API takes a trashed
// page (seen 2026-11-10), and the Public API's read leaves it out.
//
// Within a read scope a row is judged once for the connection, however many
// of a row's values show it (access_memo.go): a row showing a relation and
// two rollups over the same 100 rows judged each three times (2026-11-12).
func notionRelationRowOpen(
	app core.App, identity notionOAuthBearerIdentity, reach relationReach, rowID string,
) (notionDatabaseRowPageAuthorization, bool, error) {
	target, err := remembered(app, notionRelationRowMemoKey{
		principalID: identity.PrincipalID, clientID: identity.ClientID, grantID: identity.GrantID,
		grantGeneration: identity.GrantGeneration, workspaceID: identity.WorkspaceID, pageScoped: identity.PageScoped, rowID: rowID,
	}, func() (notionDatabaseRowPageAuthorization, error) {
		return authorizeNotionDatabaseRowPageCurrent(app, identity, rowID, permissionWorkspaceRead)
	})
	if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
		return target, false, nil
	}
	if err != nil {
		return target, false, err
	}
	if reach != nil {
		row, err := app.FindRecordById(pagesCollection, rowID)
		if err != nil || !reach(app, row) {
			return target, false, nil
		}
	}
	return target, true, nil
}

// keepClosedRelationTargets adds back to each written relation cell the
// rows its stored cell holds that the writer can't open, saying how many
// cells it changed. A server's own write, with no writer, keeps what its
// values say.
func keepClosedRelationTargets(
	ctx context.Context, app core.App, principalID, workspaceID string, operations []graphOperation,
) (int, error) {
	if principalID == "" {
		return 0, nil
	}
	rowIDs := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for _, value := range operation.Row.Values {
			if value.Type == graphValueTypeRelation {
				rowIDs[operation.Row.ID] = struct{}{}
				break
			}
		}
	}
	if len(rowIDs) == 0 {
		return 0, nil
	}
	encodedRows, err := json.Marshal(workspaceExportSortedKeys(rowIDs))
	if err != nil {
		return 0, err
	}
	stored := []storedGraphRowValue{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]], [[property_id]], [[type]], [[value]] FROM {{leafen_database_row_values}}
		WHERE [[type]] = 'relation' AND [[row_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))
	`).Bind(dbx.Params{"rowIds": string(encodedRows)}).All(&stored); err != nil || len(stored) == 0 {
		return 0, err
	}
	storedCells := make(map[[2]string][]string, len(stored))
	for _, cell := range stored {
		storedCells[[2]string{cell.RowID, cell.PropertyID}] = graphRelationRowIDs(json.RawMessage(cell.Value))
	}
	// Only a row a cell leaves out is asked about, once for the change.
	left := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for propertyID, value := range operation.Row.Values {
			targets, found := storedCells[[2]string{operation.Row.ID, propertyID}]
			if !found || value.Type != graphValueTypeRelation {
				continue
			}
			present := relationValueSet(value.Value)
			for _, id := range targets {
				if _, kept := present[id]; !kept {
					left[id] = struct{}{}
				}
			}
		}
	}
	if len(left) == 0 {
		return 0, nil
	}
	// A row deleted for good is no one's to keep.
	there, err := loadRelationTargetRows(app, left)
	if err != nil {
		return 0, err
	}
	opens := relationRowOpenerOf(ctx, app, principalID, workspaceID)
	closed := map[string]bool{}
	for _, row := range there {
		open, err := opens(row.ID)
		if err != nil {
			return 0, err
		}
		closed[row.ID] = !open
	}
	changed := 0
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for propertyID, value := range operation.Row.Values {
			targets, found := storedCells[[2]string{operation.Row.ID, propertyID}]
			if !found || value.Type != graphValueTypeRelation {
				continue
			}
			next := []string{}
			if !graphRowValueIsCleared(value.Value) {
				next = graphRelationRowIDs(value.Value)
			}
			present := relationValueSet(value.Value)
			kept := false
			for _, id := range targets {
				if _, named := present[id]; !named && closed[id] {
					next = append(next, id)
					present[id] = struct{}{}
					kept = true
				}
			}
			if !kept {
				continue
			}
			encoded, err := json.Marshal(next)
			if err != nil {
				return 0, err
			}
			operation.Row.Values[propertyID] = graphRowValue{Type: graphValueTypeRelation, Value: encoded}
			changed++
		}
	}
	return changed, nil
}

// relationValueSet is the rows a written relation value names.
func relationValueSet(value json.RawMessage) map[string]struct{} {
	present := map[string]struct{}{}
	if graphRowValueIsCleared(value) {
		return present
	}
	for _, id := range graphRelationRowIDs(value) {
		present[id] = struct{}{}
	}
	return present
}

type notionRelationRowMemoKey struct {
	principalID, clientID, grantID, workspaceID, rowID string
	grantGeneration                                    int
	pageScoped                                         bool
}
