package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A move's plan: what it carries, kept with the job as its items
// (leafen_page_workspace_move_items), and what it will cut, counted from
// them. A dry run maps the same items in a transaction it then lets go, so
// the counts it shows are the ones the move goes by.

// pageWorkspaceMoveItemPerson is someone who opens the page in the old
// workspace before the move, whom its last step asks again (the plan's
// people losing access).
const pageWorkspaceMoveItemPerson = "person"

// pageWorkspaceMovePeopleAsked is how many of the old workspace's people
// the plan asks about, one by one, each a decision of who may open the page;
// past it the count is of those asked and says so (PeopleLosingAccessAtLeast),
// "at least that many", where the screen and an agent read it.
const pageWorkspaceMovePeopleAsked = 2000

// mapPageWorkspaceMoveItems writes what the move carries: the page and every
// page under it, their databases' pages and the pages under those, as a
// place move takes them (movePageTreeToPlace); their data sources, rows,
// properties, views, templates and forms; and the files only they show.
func mapPageWorkspaceMoveItems(app core.App, move storedPageWorkspaceMove) error {
	params := dbx.Params{
		"jobId": move.JobID, "rootId": move.RootPageID,
		"workspaceId": move.SourceWorkspaceID, "ownerId": move.SourceOwnerID,
	}
	statements := []string{`
		WITH RECURSIVE [[tree]]([[id]]) AS (
			SELECT {:rootId}
			UNION
			SELECT [[page]].[[id]] FROM [[tree]]
			CROSS JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[parent_id]] = [[tree]].[[id]]
			UNION
			SELECT [[source]].[[id]] FROM [[tree]]
			CROSS JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[owner]] = {:ownerId} AND [[source]].[[page_id]] = [[tree]].[[id]]
		)
		INSERT OR IGNORE INTO {{leafen_page_workspace_move_items}} ([[job_id]], [[kind]], [[item_id]])
		SELECT {:jobId}, 'page', [[page]].[[id]] FROM [[tree]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[tree]].[[id]] AND [[page]].[[workspace]] = {:workspaceId}`,
	}
	for _, owned := range []struct{ kind, table string }{
		{pageWorkspaceMoveItemSource, "leafen_data_sources"},
		{pageWorkspaceMoveItemRow, "leafen_database_rows"},
		{pageWorkspaceMoveItemProperty, "leafen_database_properties"},
		{pageWorkspaceMoveItemView, "leafen_database_views"},
		{pageWorkspaceMoveItemTemplate, "leafen_database_templates"},
		{pageWorkspaceMoveItemForm, "leafen_form_links"},
	} {
		statements = append(statements, `
		INSERT OR IGNORE INTO {{leafen_page_workspace_move_items}} ([[job_id]], [[kind]], [[item_id]])
		SELECT {:jobId}, '`+owned.kind+`', [[owned]].[[id]] FROM {{leafen_page_workspace_move_items}} AS [[item]]
		JOIN {{`+owned.table+`}} AS [[owned]] ON [[owned]].[[page_id]] = [[item]].[[item_id]]
			AND [[owned]].[[workspace_id]] = {:workspaceId}
		WHERE [[item]].[[job_id]] = {:jobId} AND [[item]].[[kind]] = 'page'`)
	}
	// A file another page or the workspace itself shows stays where it is
	// (the plan counts it as shared).
	statements = append(statements, `
		INSERT OR IGNORE INTO {{leafen_page_workspace_move_items}} ([[job_id]], [[kind]], [[item_id]])
		SELECT DISTINCT {:jobId}, 'file', [[reference]].[[file_id]]
		FROM {{leafen_page_workspace_move_items}} AS [[item]]
		JOIN {{leafen_file_references}} AS [[reference]] ON [[reference]].[[page_id]] = [[item]].[[item_id]]
			AND [[reference]].[[workspace_id]] = {:workspaceId}
		JOIN {{leafen_files}} AS [[file]] ON [[file]].[[id]] = [[reference]].[[file_id]] AND [[file]].[[workspace]] = {:workspaceId}
		WHERE [[item]].[[job_id]] = {:jobId} AND [[item]].[[kind]] = 'page'
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_file_references}} AS [[other]]
				WHERE [[other]].[[file_id]] = [[reference]].[[file_id]] AND NOT EXISTS (
					SELECT 1 FROM {{leafen_page_workspace_move_items}} AS [[moved]]
					WHERE [[moved]].[[job_id]] = {:jobId} AND [[moved]].[[kind]] = 'page'
						AND [[moved]].[[item_id]] = [[other]].[[page_id]]))
			AND NOT EXISTS (SELECT 1 FROM {{leafen_custom_emojis}} AS [[emoji]] WHERE [[emoji]].[[file_id]] = [[reference]].[[file_id]])`)
	for _, statement := range statements {
		if _, err := app.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// pageWorkspaceMoveInBundle is SQL naming the move's items of a kind, for
// an IN (…).
func pageWorkspaceMoveInBundle(kind string) string {
	return `(SELECT [[item_id]] FROM {{leafen_page_workspace_move_items}} WHERE [[job_id]] = {:jobId} AND [[kind]] = '` + kind + `')`
}

// countPageWorkspaceMovePlan counts what the move takes and cuts, from its
// items, and keeps who opens the page now (pageWorkspaceMoveItemPerson), as
// read before (readPageWorkspaceMoveAudience).
func countPageWorkspaceMovePlan(app core.App, move storedPageWorkspaceMove, audience pageWorkspaceMoveAudience) (pageWorkspaceMovePlan, error) {
	plan := pageWorkspaceMovePlan{}
	params := dbx.Params{
		"jobId": move.JobID, "workspaceId": move.SourceWorkspaceID, "targetId": move.TargetWorkspaceID,
		"actorId": move.ActorID,
	}
	pages, sources, files := pageWorkspaceMoveInBundle(pageWorkspaceMoveItemPage),
		pageWorkspaceMoveInBundle(pageWorkspaceMoveItemSource), pageWorkspaceMoveInBundle(pageWorkspaceMoveItemFile)
	counts := []struct {
		into  *int
		query string
	}{
		{&plan.Pages, `SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] IN ` + pages + ` AND [[data_source_id]] = ''`},
		{&plan.Databases, `SELECT COUNT(*) FROM ` + sources},
		{&plan.Rows, `SELECT COUNT(*) FROM ` + pageWorkspaceMoveInBundle(pageWorkspaceMoveItemRow)},
		{&plan.Comments, `SELECT COUNT(*) FROM {{leafen_comments}} WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] IN ` + pages},
		{&plan.Files, `SELECT COUNT(*) FROM ` + files},
		{&plan.Cut.Shares, `SELECT COUNT(*) FROM {{leafen_page_permissions}} WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] IN ` + pages},
		{&plan.Cut.PublicLinks, `SELECT
			(SELECT COUNT(*) FROM {{leafen_public_share_links}} WHERE [[root_page_id]] IN ` + pages + `)
			+ (SELECT COUNT(DISTINCT [[share_link_id]]) FROM {{leafen_public_share_graph_memberships}}
				WHERE [[page_id]] IN ` + pages + ` AND [[root_page_id]] NOT IN ` + pages + `)
			+ (SELECT COUNT(*) FROM {{leafen_view_links}} WHERE [[page_id]] IN ` + pages + ` AND [[revoked_at]] = 0)
			+ (SELECT COUNT(*) FROM {{leafen_form_links}} WHERE [[page_id]] IN ` + pages + ` AND [[enabled]] = 1)`},
		{&plan.Cut.GuestLinks, `SELECT COUNT(*) FROM {{leafen_page_guest_links}} AS [[link]]
			WHERE [[link]].[[workspace_id]] = {:workspaceId} AND [[link]].[[revoked_at]] = 0
				AND EXISTS (SELECT 1 FROM json_each([[link]].[[page_ids]]) AS [[named]] WHERE [[named]].[[value]] IN ` + pages + `)`},
		{&plan.Across.MentionsOut, `SELECT COUNT(*) FROM {{leafen_block_page_references}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[source_page_id]] IN ` + pages + ` AND [[target_page_id]] NOT IN ` + pages},
		{&plan.Across.MentionsIn, `SELECT COUNT(*) FROM {{leafen_block_page_references}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[target_page_id]] IN ` + pages + ` AND [[source_page_id]] NOT IN ` + pages},
		// Only a linked view showing a database left behind: one already
		// waiting for its source (a copy's from another workspace, or one of
		// a database gone) loses nothing.
		{&plan.Across.LinkedViewsOut, `SELECT COUNT(DISTINCT [[container_id]]) FROM {{leafen_database_container_sources}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] IN ` + pages + ` AND [[data_source_id]] NOT IN ` + sources + `
				AND [[data_source_id]] IN (SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[workspace_id]] = {:workspaceId})`},
		{&plan.Across.LinkedViewsIn, `SELECT COUNT(DISTINCT [[container_id]]) FROM {{leafen_database_container_sources}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] NOT IN ` + pages + ` AND [[data_source_id]] IN ` + sources},
		{&plan.Across.SharedFiles, `SELECT COUNT(DISTINCT [[file_id]]) FROM {{leafen_file_references}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] IN ` + pages + ` AND [[file_id]] NOT IN ` + files},
	}
	var err error
	for _, count := range counts {
		if err = app.DB().NewQuery(count.query).Bind(params).Row(count.into); err != nil {
			return plan, err
		}
	}
	if plan.Cut.Favorites, err = countPageWorkspaceMoveFavoritesGone(app, move); err != nil {
		return plan, err
	}
	relations, err := pageWorkspaceMoveRelationCuts(app, move)
	if err != nil {
		return plan, err
	}
	plan.Cut.RelationProperties, plan.Cut.Rollups, plan.Cut.RelationValues, err = relations.counts(app)
	if err != nil {
		return plan, err
	}
	plan.Cut.PeopleLosingAccess, plan.Cut.PeopleLosingAccessAtLeast, err = notePageWorkspaceMoveAudience(app, move, audience)
	return plan, err
}

// countPageWorkspaceMoveFavoritesGone counts the favorites of the moved
// pages the move lets go: those of people other than the mover who aren't
// members of the workspace it goes to.
func countPageWorkspaceMoveFavoritesGone(app core.App, move storedPageWorkspaceMove) (int, error) {
	params := dbx.Params{"jobId": move.JobID, "workspaceId": move.SourceWorkspaceID, "targetId": move.TargetWorkspaceID, "actorId": move.ActorID}
	gone := 0
	for _, table := range pageWorkspaceMoveTables {
		if table.Rule != pageWorkspaceMoveRuleFavorite || table.Favorite == "" {
			continue
		}
		var count int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{` + table.Table + `}} WHERE [[` + table.Key + `]] IN ` +
			pageWorkspaceMoveInBundle(table.Kind) + ` AND [[workspace_id]] = {:workspaceId} AND ` + table.Favorite +
			` AND ` + pageWorkspaceMoveStrangerSQL("[["+table.Principal+"]]")).Bind(params).Row(&count); err != nil {
			return 0, err
		}
		gone += count
	}
	return gone, nil
}

// pageWorkspaceMoveStrangerSQL says a person isn't the mover and isn't a
// member of the workspace the move goes to.
func pageWorkspaceMoveStrangerSQL(person string) string {
	return person + ` != {:actorId} AND NOT EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} AS [[member]]
		WHERE [[member]].[[workspace_id]] = {:targetId} AND [[member]].[[user_id]] = ` + person + `)`
}

// pageWorkspaceMoveAudience is who, other than the mover, opens the page in
// the old workspace now or has a page of it shared with them, and how many of
// them the move's new place won't let in: the people losing access the plan
// shows. Asked about only so many (pageWorkspaceMovePeopleAsked), it says the
// count is of them alone (capped).
type pageWorkspaceMoveAudience struct {
	opens  []string
	losing int
	capped bool
}

// readPageWorkspaceMoveAudience asks, before the move's write begins, one
// decision of who may open the page a person: in a workspace of thousands
// that is a second's work (2026-11-23: 2,100 members, about 1 s), which a
// write would hold every other edit through.
func readPageWorkspaceMoveAudience(app core.App, move storedPageWorkspaceMove) (pageWorkspaceMoveAudience, error) {
	audience := pageWorkspaceMoveAudience{}
	candidates := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]] FROM (
			SELECT [[user_id]] FROM {{leafen_workspace_memberships}} WHERE [[workspace_id]] = {:workspaceId}
			UNION
			SELECT [[principal_id]] FROM {{leafen_page_permissions}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[principal_type]] = 'user'
				AND [[page_id]] IN ` + pageWorkspaceMoveTreePagesSQL + `
		) WHERE [[user_id]] != {:actorId} AND [[user_id]] != ''
		ORDER BY [[user_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"rootId": move.RootPageID, "workspaceId": move.SourceWorkspaceID, "ownerId": move.SourceOwnerID,
		"actorId": move.ActorID, "limit": pageWorkspaceMovePeopleAsked + 1,
	}).Column(&candidates); err != nil {
		return audience, err
	}
	audience.capped = len(candidates) > pageWorkspaceMovePeopleAsked
	for _, person := range candidates[:min(len(candidates), pageWorkspaceMovePeopleAsked)] {
		opens, err := pageWorkspaceMovePersonOpens(app, move, person)
		if err != nil || !opens {
			if err != nil {
				return audience, err
			}
			continue
		}
		audience.opens = append(audience.opens, person)
		after, err := pageWorkspaceMoveDestinationOpens(app, move, person)
		if err != nil {
			return audience, err
		}
		if !after {
			audience.losing++
		}
	}
	return audience, nil
}

// pageWorkspaceMoveTreePagesSQL names the page and the pages under it, as
// the move's items will (mapPageWorkspaceMoveItems), before they are written.
const pageWorkspaceMoveTreePagesSQL = `(
	WITH RECURSIVE [[tree]]([[id]]) AS (
		SELECT {:rootId}
		UNION
		SELECT [[page]].[[id]] FROM [[tree]]
		CROSS JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[parent_id]] = [[tree]].[[id]]
		UNION
		SELECT [[source]].[[id]] FROM [[tree]]
		CROSS JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[owner]] = {:ownerId} AND [[source]].[[page_id]] = [[tree]].[[id]]
	)
	SELECT [[id]] FROM [[tree]])`

// notePageWorkspaceMoveAudience keeps, with the move, who opened the page
// before it (the people its last step asks again), and says how many lose it.
func notePageWorkspaceMoveAudience(app core.App, move storedPageWorkspaceMove, audience pageWorkspaceMoveAudience) (int, bool, error) {
	for _, person := range audience.opens {
		if _, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_page_workspace_move_items}} ([[job_id]], [[kind]], [[item_id]])
			VALUES ({:jobId}, 'person', {:person})
		`).Bind(dbx.Params{"jobId": move.JobID, "person": person}).Execute(); err != nil {
			return 0, false, err
		}
	}
	return audience.losing, audience.capped, nil
}

// pageWorkspaceMovePersonOpens says whether someone opens the page, or has
// a page under it shared with them, in the old workspace now.
func pageWorkspaceMovePersonOpens(app core.App, move storedPageWorkspaceMove, person string) (bool, error) {
	if _, err := pageAccessDespiteHold(authorizePageAccessRole(app, person, move.RootPageID, pageAccessRoleView)); err == nil {
		return true, nil
	} else if !isPermissionDenied(err) {
		return false, err
	}
	var shared int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_permissions}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[principal_type]] = 'user' AND [[principal_id]] = {:person}
				AND [[page_id]] IN ` + pageWorkspaceMoveTreePagesSQL + `)
	`).Bind(dbx.Params{
		"rootId": move.RootPageID, "workspaceId": move.SourceWorkspaceID, "ownerId": move.SourceOwnerID, "person": person,
	}).Row(&shared)
	return shared == 1, err
}

// pageWorkspaceMoveDestinationOpens says whether the place the page goes
// to lets someone open it: the teamspace's members, whoever opens the page
// it goes under; the top of the mover's private pages lets in no one else.
func pageWorkspaceMoveDestinationOpens(app core.App, move storedPageWorkspaceMove, person string) (bool, error) {
	var err error
	switch {
	case move.DestinationParentID != "":
		_, err = pageAccessDespiteHold(authorizePageAccessRole(app, person, move.DestinationParentID, pageAccessRoleView))
	case move.DestinationTeamspaceID != "":
		var role pageAccessRole
		_, role, err = effectiveTeamspacePageAccessRole(app, person, move.TargetWorkspaceID, move.DestinationTeamspaceID)
		if err == nil && role < pageAccessRoleView {
			return false, nil
		}
	default:
		return false, nil
	}
	if isPermissionDenied(err) {
		return false, nil
	}
	return err == nil, err
}

// storePageWorkspaceMovePlan keeps the plan with the job.
func storePageWorkspaceMovePlan(app core.App, jobID string, plan pageWorkspaceMovePlan) error {
	encoded, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_page_workspace_moves}} SET [[plan_json]] = {:plan} WHERE [[job_id]] = {:jobId}`).
		Bind(dbx.Params{"plan": string(encoded), "jobId": jobID}).Execute()
	return err
}
