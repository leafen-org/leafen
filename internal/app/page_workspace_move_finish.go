package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A move's last step, at once, once every row it carries is in the new
// workspace: it cuts what the plan said it would (relations to databases
// left behind, the old workspace's shares and links, other people's
// favorites where they aren't members of the new one), puts the page where
// it was asked to go — out of its old parent's body, into its new parent's
// or at the top of its new place — keeps the names of people its values name
// who aren't the new workspace's, has search take its pages in the new
// workspace, and lets them go. What it cut, counted as it cut it, is kept
// beside the plan.

func finishPageWorkspaceMove(app core.App, move storedPageWorkspaceMove) error {
	now := time.Now().UTC()
	cut := pageWorkspaceMoveCuts{}
	params := dbx.Params{
		"jobId": move.JobID, "workspaceId": move.SourceWorkspaceID, "targetId": move.TargetWorkspaceID,
		"actorId": move.ActorID, "now": now.UnixMilli(),
	}
	pages := pageWorkspaceMoveInBundle(pageWorkspaceMoveItemPage)
	// Counted before they go.
	for _, count := range []struct {
		into  *int
		query string
	}{
		{&cut.Shares, `SELECT COUNT(*) FROM {{leafen_page_permissions}} WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] IN ` + pages},
		{&cut.PublicLinks, `SELECT
			(SELECT COUNT(*) FROM {{leafen_public_share_links}} WHERE [[root_page_id]] IN ` + pages + `)
			+ (SELECT COUNT(DISTINCT [[share_link_id]]) FROM {{leafen_public_share_graph_memberships}}
				WHERE [[page_id]] IN ` + pages + ` AND [[root_page_id]] NOT IN ` + pages + `)
			+ (SELECT COUNT(*) FROM {{leafen_view_links}} WHERE [[page_id]] IN ` + pages + ` AND [[revoked_at]] = 0)
			+ (SELECT COUNT(*) FROM {{leafen_form_links}} WHERE [[page_id]] IN ` + pages + ` AND [[enabled]] = 1)`},
	} {
		if err := app.DB().NewQuery(count.query).Bind(params).Row(count.into); err != nil {
			return err
		}
	}
	relations, err := pageWorkspaceMoveRelationCuts(app, move)
	if err != nil {
		return err
	}
	if cut.RelationProperties, cut.Rollups, cut.RelationValues, err = relations.counts(app); err != nil {
		return err
	}
	if err := relations.cut(app, move, now); err != nil {
		return err
	}
	// A public link of the old workspace stops showing the moved pages.
	for _, statement := range []string{
		`DELETE FROM {{leafen_public_share_graph_memberships}} WHERE [[page_id]] IN ` + pages + ` OR [[root_page_id]] IN ` + pages,
		`DELETE FROM {{leafen_public_share_graph_jobs}} WHERE [[page_id]] IN ` + pages,
	} {
		if _, err := app.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
			return err
		}
	}
	for _, table := range pageWorkspaceMoveTables {
		if table.Rule != pageWorkspaceMoveRuleCut {
			continue
		}
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{` + table.Table + `}} WHERE [[` + table.Key + `]] IN ` + pageWorkspaceMoveInBundle(table.Kind) + `
				AND [[` + table.Workspace + `]] = {:workspaceId}
		`).Bind(params).Execute(); err != nil {
			return err
		}
	}
	if cut.GuestLinks, err = leavePageWorkspaceMoveGuestLinks(app, move, now); err != nil {
		return err
	}
	if cut.Favorites, err = movePageWorkspaceMoveFavorites(app, move); err != nil {
		return err
	}
	if err := placePageWorkspaceMoveRoot(app, move); err != nil {
		return err
	}
	// The people its person values name who aren't the new workspace's keep
	// their names there by the moved page, as a copy's do (workspace_carried_people.go).
	if err := keepCarriedPeople(app, move.SourceWorkspaceID, move.TargetWorkspaceID, move.RootPageID,
		pageWorkspaceMoveInBundle(pageWorkspaceMoveItemPage), dbx.Params{"jobId": move.JobID}); err != nil {
		return err
	}
	if err := clearStalePageWorkspaceMoveSearch(app, move); err != nil {
		return err
	}
	if err := reindexPageWorkspaceMovePages(app, move, move.TargetWorkspaceID); err != nil {
		return err
	}
	if err := notePageWorkspaceMoveListed(app, move, move.TargetWorkspaceID); err != nil {
		return err
	}
	if cut.PeopleLosingAccess, err = countPageWorkspaceMovePeopleLost(app, move); err != nil {
		return err
	}
	// Counted again among those the plan asked about: at least as many when it stopped there.
	planned := pageWorkspaceMovePlan{}
	_ = json.Unmarshal([]byte(move.PlanJSON), &planned)
	cut.PeopleLosingAccessAtLeast = planned.Cut.PeopleLosingAccessAtLeast
	encoded, err := json.Marshal(cut)
	if err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_workspace_moves}}
		SET [[phase]] = 'completed', [[cut_json]] = {:cut}, [[revision]] = [[revision]] + 1,
			[[updated_at]] = {:now}, [[completed_at]] = {:now}
		WHERE [[job_id]] = {:jobId} AND [[phase]] = 'moving'
	`).Bind(dbx.Params{"cut": string(encoded), "now": now.UnixMilli(), "jobId": move.JobID}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]]='completed',[[processed_items]]={:count},[[total_items]]={:count},
			[[updated]]={:now},[[chunk_count]]=[[chunk_count]]+1
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"count": move.PageCount, "now": now.UnixMilli(), "jobId": move.JobID}).Execute(); err != nil {
		return err
	}
	if root, err := app.FindRecordById(pagesCollection, move.RootPageID); err == nil {
		if isFullPageDatabase(root) {
			recordFullPageDatabaseWebhookEvent(app, eventWebhookDatabaseMoved, root, move.ActorID)
		} else {
			recordPageWebhookEvent(app, eventWebhookPageMoved, root, move.ActorID)
		}
	}
	recheckCollaborationAccessAfterCommit(app)
	return nil
}

// leavePageWorkspaceMoveGuestLinks takes the moved pages out of the old
// workspace's guest invite links, and ends a link left naming none.
func leavePageWorkspaceMoveGuestLinks(app core.App, move storedPageWorkspaceMove, now time.Time) (int, error) {
	links := []struct {
		ID      string `db:"id"`
		PageIDs string `db:"page_ids"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[page_ids]] FROM {{leafen_page_guest_links}} AS [[link]]
		WHERE [[workspace_id]] = {:workspaceId} AND [[revoked_at]] = 0
			AND EXISTS (SELECT 1 FROM json_each([[link]].[[page_ids]]) AS [[named]]
				WHERE [[named]].[[value]] IN ` + pageWorkspaceMoveInBundle(pageWorkspaceMoveItemPage) + `)
	`).Bind(dbx.Params{"jobId": move.JobID, "workspaceId": move.SourceWorkspaceID}).All(&links); err != nil {
		return 0, err
	}
	for _, link := range links {
		named := []string{}
		if err := json.Unmarshal([]byte(link.PageIDs), &named); err != nil {
			return 0, err
		}
		kept := []string{}
		for _, pageID := range named {
			held, err := pageWorkspaceMoveCarries(app, move.JobID, pageWorkspaceMoveItemPage, pageID)
			if err != nil {
				return 0, err
			}
			if !held {
				kept = append(kept, pageID)
			}
		}
		encoded, err := json.Marshal(kept)
		if err != nil {
			return 0, err
		}
		revoked := int64(0)
		if len(kept) == 0 {
			revoked = now.UnixMilli()
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_page_guest_links}} SET [[page_ids]] = {:pageIds}, [[revoked_at]] = {:revoked}
			WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"pageIds": string(encoded), "revoked": revoked, "id": link.ID}).Execute(); err != nil {
			return 0, err
		}
	}
	return len(links), nil
}

// pageWorkspaceMoveCarries says whether the move carries an item.
func pageWorkspaceMoveCarries(app core.App, jobID, kind, id string) (bool, error) {
	var carried int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_workspace_move_items}}
			WHERE [[job_id]] = {:jobId} AND [[kind]] = {:kind} AND [[item_id]] = {:id})
	`).Bind(dbx.Params{"jobId": jobID, "kind": kind, "id": id}).Row(&carried)
	return carried == 1, err
}

// movePageWorkspaceMoveFavorites keeps a favorite or a visit of a moved
// page for the mover and the new workspace's members, in the new workspace,
// and lets the others' go: they no longer reach the page there. It says how
// many favorites went.
func movePageWorkspaceMoveFavorites(app core.App, move storedPageWorkspaceMove) (int, error) {
	params := dbx.Params{"jobId": move.JobID, "workspaceId": move.SourceWorkspaceID, "targetId": move.TargetWorkspaceID, "actorId": move.ActorID}
	gone := 0
	for _, table := range pageWorkspaceMoveTables {
		if table.Rule != pageWorkspaceMoveRuleFavorite {
			continue
		}
		scope := `[[` + table.Key + `]] IN ` + pageWorkspaceMoveInBundle(table.Kind) + ` AND [[workspace_id]] = {:workspaceId}`
		if table.Favorite != "" {
			var count int
			if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{` + table.Table + `}} WHERE ` + scope +
				` AND ` + table.Favorite + ` AND ` + pageWorkspaceMoveStrangerSQL("[["+table.Principal+"]]")).Bind(params).Row(&count); err != nil {
				return 0, err
			}
			gone += count
		}
		if _, err := app.DB().NewQuery(`DELETE FROM {{` + table.Table + `}} WHERE ` + scope + ` AND ` +
			pageWorkspaceMoveStrangerSQL("[["+table.Principal+"]]")).Bind(params).Execute(); err != nil {
			return 0, err
		}
		if _, err := app.DB().NewQuery(`UPDATE {{` + table.Table + `}} SET [[workspace_id]] = {:targetId} WHERE ` + scope).
			Bind(params).Execute(); err != nil {
			return 0, err
		}
	}
	return gone, nil
}

// placePageWorkspaceMoveRoot takes the page out of its old parent's body and
// puts it where it goes: under a page of the new workspace, at its body's
// end, or last at the top of a teamspace or the mover's private pages, its
// pages taking that place.
func placePageWorkspaceMoveRoot(app core.App, move storedPageWorkspaceMove) error {
	if move.RootParentID != "" {
		if err := removeChildPageBlock(app, move.SourceOwnerID, move.SourceWorkspaceID, move.RootPageID, move.ActorID, false); err != nil {
			return err
		}
	}
	place := pagePlace{TeamspaceID: move.DestinationTeamspaceID}
	if move.DestinationParentID != "" {
		parent, err := app.FindRecordById(pagesCollection, move.DestinationParentID)
		if err != nil {
			return err
		}
		place = pagePlaceOf(parent)
	} else if move.DestinationTeamspaceID == "" {
		place = pagePlace{PrivateOwnerID: move.ActorID}
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[parent_id]] = {:parentId} WHERE [[id]] = {:pageId}
	`).Bind(dbx.Params{"parentId": move.DestinationParentID, "pageId": move.RootPageID}).Execute(); err != nil {
		return err
	}
	if err := movePageTreeToPlace(app, move.TargetOwnerID, move.TargetWorkspaceID, move.RootPageID, place); err != nil {
		return err
	}
	if move.DestinationParentID == "" {
		return placePageLastAmongSiblings(app, move.TargetOwnerID, move.TargetWorkspaceID, move.RootPageID, "")
	}
	return putChildPageBlock(app, move.TargetOwnerID, move.TargetWorkspaceID, move.DestinationParentID, move.RootPageID,
		move.ActorID, false, func(doc *crdt.Doc, _ map[string]structuralBlockMetadata) (childPageBlockSpot, error) {
			return endOfChildPageContainer(doc, "")
		})
}

// clearStalePageWorkspaceMoveSearch takes out of the old workspace's search
// what an index under way wrote of the moved pages while they moved.
func clearStalePageWorkspaceMoveSearch(app core.App, move storedPageWorkspaceMove) error {
	stale := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_search_pages}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] IN ` + pageWorkspaceMoveInBundle(pageWorkspaceMoveItemPage) + `
	`).Bind(dbx.Params{"jobId": move.JobID, "workspaceId": move.SourceWorkspaceID}).Column(&stale); err != nil {
		return err
	}
	for _, pageID := range stale {
		for {
			more, err := deleteWorkspaceSearchFTSPage(app, pageID)
			if err != nil {
				return err
			}
			if !more {
				break
			}
		}
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_search_pages}} WHERE [[page_id]] = {:pageId}`).
			Bind(dbx.Params{"pageId": pageID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// countPageWorkspaceMovePeopleLost says how many of those who opened the
// page in the old workspace before the move don't open it now.
func countPageWorkspaceMovePeopleLost(app core.App, move storedPageWorkspaceMove) (int, error) {
	people := []string{}
	if err := app.DB().NewQuery(`SELECT [[item_id]] FROM ` + pageWorkspaceMoveInBundle(pageWorkspaceMoveItemPerson) + ` AS [[person]]`).
		Bind(dbx.Params{"jobId": move.JobID}).Column(&people); err != nil {
		return 0, err
	}
	lost := 0
	for _, person := range people {
		_, err := pageAccessDespiteHold(authorizePageAccessRole(app, person, move.RootPageID, pageAccessRoleView))
		if isPermissionDenied(err) {
			lost++
		} else if err != nil {
			return 0, err
		}
	}
	return lost, nil
}

// notePageWorkspaceMoveListed has screens following a workspace's sidebar
// read the moved pages again once the move lets them go, there: one that
// caught up while they were held found them hidden and would keep them so
// (bootstrap_page_changes.go). The page itself is the root of a subtree
// read, as a moved page is.
func notePageWorkspaceMoveListed(app core.App, move storedPageWorkspaceMove, workspaceID string) error {
	params := dbx.Params{"jobId": move.JobID, "rootId": move.RootPageID, "workspaceId": workspaceID,
		"sourceId": move.SourceWorkspaceID, "targetId": move.TargetWorkspaceID}
	for _, statement := range []string{
		`UPDATE {{leafen_page_list_change_sequence}} SET [[seq]] = [[seq]] + 1 WHERE [[singleton]] = 'pages'`,
		`INSERT INTO {{leafen_page_list_changes}} ([[workspace_id]], [[page_id]], [[seq]], [[subtree_seq]])
		SELECT {:workspaceId}, [[page]].[[id]], [[sequence]].[[seq]],
			CASE WHEN [[page]].[[id]] = {:rootId} THEN [[sequence]].[[seq]] ELSE 0 END
		FROM {{leafen_pages}} AS [[page]]
		JOIN {{leafen_page_list_change_sequence}} AS [[sequence]] ON [[sequence]].[[singleton]] = 'pages'
		WHERE [[page]].[[id]] IN ` + pageWorkspaceMoveInBundle(pageWorkspaceMoveItemPage) + `
			AND [[page]].[[workspace]] = {:workspaceId}
			AND ([[page]].[[data_source_id]] = '' OR EXISTS (SELECT 1 FROM {{leafen_data_sources}} AS [[source]]
				WHERE [[source]].[[id]] = [[page]].[[data_source_id]] AND [[source]].[[wiki]] = 1))
		ON CONFLICT ([[workspace_id]], [[page_id]]) DO UPDATE SET
			[[seq]] = excluded.[[seq]], [[subtree_seq]] = MAX([[subtree_seq]], excluded.[[subtree_seq]])`,
		`UPDATE {{leafen_workspace_content_revisions}} SET [[page_list_revision]] = [[page_list_revision]] + 1
		WHERE [[workspace_id]] IN ({:sourceId}, {:targetId})`,
	} {
		if _, err := app.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
			return err
		}
	}
	return nil
}
