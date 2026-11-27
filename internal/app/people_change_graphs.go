package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/reearth/ygo/crdt"
)

// A change of people — who is in a permission group, the group gone, who is
// in a teamspace or what it gives, a person joining or leaving a workspace or
// made restricted there — changes what they see without changing any page.
// The pages and rows' own pages open to them follow through their sockets
// (ygo_access_recheck.go) and their access read again
// (useLocalFirstContentGraph). A database's views read rows, and two kinds
// of them read by who reads them: an item whose sharing is unlinked from its
// database's shows to each reader by their own role in it — its shares, a
// group's among them, its teamspace's role, their place in the workspace
// (database_row_unlinked_access.go) — and a property access rule may name a
// group (database_property_access.go). Nothing told the open views of such
// a change: a table kept an item someone was taken out of the group it was
// shared to until a reload or its next change, and someone added saw it
// only after a reload (2026-11-25). The change now records a change on the
// pages holding those databases, as another person's change is taken up:
// the items it reaches read again alone (recordServerGraphRowsChange) — one
// closed to its reader now leaves their views as gone
// (redactGraphReadForReader), one open to them now joins them — and a
// database whose rule names a changed group reads whole, its properties'
// access with it, as a rule's own change does. The pages showing those
// databases elsewhere follow (announceGraphReadersAfterCommit).

// announcePeopleChange records a change of people of a workspace on the
// pages holding its databases whose reads it changes: with groups, those
// the groups reach — items shared to one or in a teamspace one is in, and
// databases whose rules name one — and without, every item of the
// workspace whose sharing is unlinked, which a change of a teamspace's
// people or of a person's place there (advanceWorkspacePermissionCursor)
// may reach; only those items read again. Run in the change's transaction,
// the open views hear of it once that is committed.
func announcePeopleChange(app core.App, workspaceID string, groups ...string) error {
	if workspaceID == "" {
		return nil
	}
	encoded, err := json.Marshal(append([]string{}, groups...))
	if err != nil {
		return err
	}
	params := dbx.Params{"workspaceId": workspaceID, "groups": string(encoded)}
	reached := ""
	if len(groups) > 0 {
		reached = `AND (EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[share]]
				WHERE [[share]].[[page_id]] = [[unlink]].[[page_id]] AND [[share]].[[principal_type]] = 'group'
					AND [[share]].[[principal_id]] IN (SELECT [[value]] FROM json_each({:groups})))
			OR COALESCE([[page]].[[teamspace_id]], '') != '' AND EXISTS (SELECT 1 FROM {{leafen_teamspace_members}} [[seat]]
				WHERE [[seat]].[[teamspace_id]] = [[page]].[[teamspace_id]] AND [[seat]].[[principal_type]] = 'group'
					AND [[seat]].[[deleted_at]] = 0 AND [[seat]].[[principal_id]] IN (SELECT [[value]] FROM json_each({:groups}))))`
	}
	items := []struct {
		Owner string `db:"owner"`
		Page  string `db:"page_id"`
		Row   string `db:"row_id"`
	}{}
	// From the workspace's unlinked items to the pages holding their
	// databases, as databaseRowPageContainer finds them.
	if err := app.DB().NewQuery(`
		SELECT [[source]].[[owner]], [[source]].[[page_id]], [[unlink]].[[page_id]] AS [[row_id]]
		FROM {{leafen_page_share_unlinks}} [[unlink]]
		CROSS JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[unlink]].[[page_id]]
		CROSS JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[page]].[[data_source_id]]
			AND [[source]].[[owner]] = [[page]].[[owner]] AND [[source]].[[workspace_id]] = [[unlink]].[[workspace_id]]
		WHERE [[unlink]].[[workspace_id]] = {:workspaceId} AND [[source]].[[page_id]] != '' ` + reached + `
		ORDER BY [[source]].[[page_id]], [[unlink]].[[page_id]]
	`).Bind(params).All(&items); err != nil {
		return err
	}
	type holder struct{ owner, page string }
	whole := map[holder]bool{}
	order := []holder{}
	if len(groups) > 0 {
		ruled := []struct {
			Owner string `db:"owner"`
			Page  string `db:"page_id"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT DISTINCT [[source]].[[owner]], [[source]].[[page_id]]
			FROM {{leafen_database_property_access_exceptions}} [[exception]]
			CROSS JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[exception]].[[data_source_id]]
			WHERE [[exception]].[[workspace_id]] = {:workspaceId} AND [[exception]].[[principal_type]] = 'group'
				AND [[exception]].[[principal_id]] IN (SELECT [[value]] FROM json_each({:groups}))
				AND [[source]].[[page_id]] != ''
			ORDER BY [[source]].[[page_id]]
		`).Bind(params).All(&ruled); err != nil {
			return err
		}
		for _, database := range ruled {
			key := holder{database.Owner, database.Page}
			if !whole[key] {
				whole[key] = true
				order = append(order, key)
			}
		}
	}
	rows := map[holder][]string{}
	for _, item := range items {
		key := holder{item.Owner, item.Page}
		if whole[key] {
			continue
		}
		if rows[key] == nil {
			order = append(order, key)
		}
		rows[key] = append(rows[key], item.Row)
	}
	for _, key := range order {
		mutationID := "people-access:" + security.RandomString(20)
		// Past what a page reads as its changes, it reads whole in any case.
		if changed := rows[key]; !whole[key] && len(changed) <= maxGraphChangedRows {
			err = recordServerGraphRowsChange(app, key.owner, key.page, mutationID, changed)
		} else {
			err = recordServerGraphChange(app, key.owner, key.page, mutationID,
				crdt.EncodeStateAsUpdateV1(crdt.New(), nil), graphMutationCounts{})
		}
		if err != nil {
			return err
		}
	}
	return nil
}
