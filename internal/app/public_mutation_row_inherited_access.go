package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/reearth/ygo/crdt"
)

// A database item's page lists everyone its database's page and the pages
// above that give it as one group named for its database, as Notion's (seen
// 2026-10-13: "Rows에 초대된 사용자" with a table icon, though the person was
// shared the page above it), each person at the widest role those pages
// give and naming the page that gives it. Lowering or removing one unlinks
// the item's sharing from its database's (database_row_unlinked_access.go),
// as a page's does from its parent's.

type storedRowInheritedShare struct {
	PageID         string `db:"page_id"`
	PrincipalType  string `db:"principal_type"`
	PrincipalID    string `db:"principal_id"`
	PrincipalLabel string `db:"principal_label"`
	PrincipalEmail string `db:"principal_email"`
	PrincipalUser  string `db:"principal_user_id"`
	Guest          bool   `db:"principal_guest"`
	Restricted     bool   `db:"principal_restricted"`
	TopRole        int    `db:"top_role"`
	FirstAt        int64  `db:"first_at"`
}

func (share storedRowInheritedShare) key() string {
	return share.PrincipalType + ":" + share.PrincipalID
}

// loadRowInheritedShares reads a window of the people the pages above a
// database item's page give, in the order they were first shared, after the
// given place in it.
func loadRowInheritedShares(
	ctx context.Context, app core.App, workspaceID, pageID, ownerID string, ancestorIDs []string,
	afterAt int64, afterKey string, limit int,
) ([]storedRowInheritedShare, error) {
	rows := []storedRowInheritedShare{}
	if len(ancestorIDs) == 0 {
		return rows, nil
	}
	encoded, err := json.Marshal(ancestorIDs)
	if err != nil {
		return nil, err
	}
	err = app.DB().NewQuery(`
		SELECT * FROM (
			SELECT [[grouped]].*, (
				SELECT MIN([[first]].[[created_at]]) FROM {{leafen_page_permissions}} [[first]]
				WHERE [[first]].[[workspace_id]]={:workspaceId} AND [[first]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
					AND [[first]].[[principal_type]]=[[grouped]].[[principal_type]] AND [[first]].[[principal_id]]=[[grouped]].[[principal_id]]
			) AS [[first_at]]
			FROM (
				SELECT [[permission]].[[principal_type]],[[permission]].[[principal_id]],
					` + publicMutationPrincipalColumnsSQL + `,
					MAX(CASE [[permission]].[[role]] WHEN 'view' THEN 1 WHEN 'comment' THEN 2
						WHEN 'edit' THEN 3 WHEN 'full_access' THEN 4 ELSE 0 END) AS [[top_role]],
					[[permission]].[[page_id]] AS [[page_id]]
				FROM {{leafen_page_permissions}} [[permission]]
				` + publicMutationPrincipalJoinsSQL + `
				WHERE [[permission]].[[workspace_id]]={:workspaceId}
					AND [[permission]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
					AND ` + inheritedShareFilterSQL + `
				GROUP BY [[permission]].[[principal_type]],[[permission]].[[principal_id]]
			) [[grouped]]
		)
		WHERE ([[first_at]], [[principal_type]] || ':' || [[principal_id]]) > ({:afterAt}, {:afterKey})
		ORDER BY [[first_at]], [[principal_type]] || ':' || [[principal_id]]
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": pageID, "ownerId": ownerID, "pages": string(encoded),
		"afterAt": afterAt, "afterKey": afterKey, "limit": limit,
	}).All(&rows)
	return rows, err
}

// rowInheritedPeople names the people of a window, each with the page giving
// them their role.
func rowInheritedPeople(ctx context.Context, app core.App, workspaceID string, rows []storedRowInheritedShare) ([]publicMutationInheritedPerson, error) {
	pages := []string{}
	for _, row := range rows {
		pages = append(pages, row.PageID)
	}
	titles, err := loadPageTitles(ctx, app, workspaceID, pages)
	if err != nil {
		return nil, err
	}
	people := make([]publicMutationInheritedPerson, 0, len(rows))
	for _, row := range rows {
		people = append(people, publicMutationInheritedPerson{
			PrincipalType: row.PrincipalType, PrincipalID: row.PrincipalID, Label: row.PrincipalLabel,
			Email: row.PrincipalEmail, UserID: row.PrincipalUser, Role: publicMutationRoleFor(pageAccessRole(row.TopRole)), Guest: row.Guest,
			Restricted: row.Restricted,
			FromTitle:  titles[row.PageID],
		})
	}
	return people, nil
}

// populatePublicMutationRowInheritedAccess lists a database item's page's
// inherited people as one group, or says which database an unlinked one's
// sharing left.
func populatePublicMutationRowInheritedAccess(
	ctx context.Context, app core.App, authorization pageAuthorization, state *publicMutationState,
) error {
	database, err := databaseRowShareTitle(app, authorization.Page)
	if err != nil {
		return err
	}
	unlinked, err := pageShareUnlinked(app, state.PageID)
	if err != nil {
		return err
	}
	if unlinked {
		if err := populatePublicMutationShareUnlink(ctx, app, authorization, state); err != nil || state.ShareUnlink == nil {
			return err
		}
		if err := withPeopleRuleRelinkChange(app, authorization.Page, state); err != nil {
			return err
		}
		if wiki, err := databaseRowInWiki(app, authorization.Page); err != nil || wiki {
			return err
		}
		state.ShareUnlink.ParentTitle, state.ShareUnlink.ParentDatabase = database, true
		return nil
	}
	ids, err := loadPageAccessAncestorIDs(app, authorization.Page.GetString("workspace"), authorization.Page.Id)
	if err != nil || len(ids) < 2 {
		return err
	}
	owner := state.AccessOwner.PrincipalID
	rows, err := loadRowInheritedShares(ctx, app, state.WorkspaceID, state.PageID, owner, ids[1:], -1, "", publicMutationInheritedPeopleWindow)
	if err != nil || len(rows) == 0 {
		return err
	}
	var count, top int
	encoded, err := json.Marshal(ids[1:])
	if err != nil {
		return err
	}
	// The roles are found by page; the workspace is read after (+), or
	// SQLite, which keeps no statistics, reads the workspace's every role.
	if err := app.DB().NewQuery(`
		SELECT COUNT(*), COALESCE(MAX([[top_role]]), 0) FROM (
			SELECT MAX(CASE [[permission]].[[role]] WHEN 'view' THEN 1 WHEN 'comment' THEN 2
				WHEN 'edit' THEN 3 WHEN 'full_access' THEN 4 ELSE 0 END) AS [[top_role]]
			FROM {{leafen_page_permissions}} [[permission]]
			WHERE +[[permission]].[[workspace_id]]={:workspaceId}
				AND [[permission]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
				AND `+inheritedShareFilterSQL+`
			GROUP BY [[permission]].[[principal_type]],[[permission]].[[principal_id]]
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": state.WorkspaceID, "pageId": state.PageID, "ownerId": owner, "pages": string(encoded),
	}).Row(&count, &top); err != nil {
		return err
	}
	people, err := rowInheritedPeople(ctx, app, state.WorkspaceID, rows)
	if err != nil {
		return err
	}
	group := publicMutationInheritedGroup{
		PageID: ids[1], PageTitle: database, Count: count, Role: publicMutationRoleFor(pageAccessRole(top)),
		People: people, Database: true,
	}
	// A wiki's page sits under the page above it, which names the group.
	if parentID, parentTitle, _, err := publicMutationPageParent(ctx, app, state.WorkspaceID, state.PageID); err != nil {
		return err
	} else if wiki, err := databaseRowInWiki(app, authorization.Page); err != nil {
		return err
	} else if wiki && parentID == ids[1] {
		group.PageTitle, group.Database = parentTitle, false
	}
	state.InheritedGroups = append(state.InheritedGroups, group)
	return nil
}

// databaseRowInWiki is whether a database item is a page of a wiki.
func databaseRowInWiki(app core.App, rowPage *core.Record) (bool, error) {
	var wiki bool
	err := app.DB().NewQuery(`SELECT [[wiki]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": rowPage.GetString("data_source_id")}).Row(&wiki)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return wiki, err
}

// readRowInheritedSharePeople is a window of a database item's page's
// inherited people past the first ones its share window shows.
func readRowInheritedSharePeople(
	ctx context.Context, app core.App, authorization pageAuthorization, ancestors []string, afterAt int64, afterKey string,
) (publicMutationInheritedPeopleResponse, error) {
	response := publicMutationInheritedPeopleResponse{People: []publicMutationInheritedPerson{}}
	workspaceID := authorization.Workspace.WorkspaceID
	holder, _, err := publicMutationFullAccessHolder(ctx, app, authorization, ancestors)
	if err != nil {
		return response, err
	}
	rows, err := loadRowInheritedShares(ctx, app, workspaceID, authorization.Page.Id, holder,
		ancestors[1:], afterAt, afterKey, publicMutationInheritedPeoplePage+1)
	if err != nil {
		return response, err
	}
	if len(rows) > publicMutationInheritedPeoplePage {
		rows = rows[:publicMutationInheritedPeoplePage]
		last := rows[len(rows)-1]
		response.NextCursor = strconv.FormatInt(last.FirstAt, 10) + ":" + last.key()
	}
	response.People, err = rowInheritedPeople(ctx, app, workspaceID, rows)
	return response, err
}

// databaseRowShareTitle is the name of a database item's database, which its
// share window names as the page above it.
func databaseRowShareTitle(app core.App, rowPage *core.Record) (string, error) {
	var title string
	err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": rowPage.GetString("data_source_id")}).Row(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return title, err
}

// announceRowShareChange records a change on the page holding a database
// item's database when who may see the item changes there — its sharing
// unlinked, linked again, or changed while unlinked — so open views and
// queries read it again, as a property access change does. They read the
// page whole, not the item alone: the page's other items may mention the
// item — another row's title or text, a block, a database's name — and
// each reads as its reader may open it now (graphItemsAsRead).
func announceRowShareChange(app core.App, rowPage *core.Record) error {
	containerID, err := databaseRowPageContainer(app, rowPage)
	if err != nil {
		return err
	}
	return recordServerGraphChange(app, rowPage.GetString("owner"), containerID,
		"database-row-access:"+security.RandomString(20), crdt.EncodeStateAsUpdateV1(crdt.New(), nil), graphMutationCounts{})
}

// announceUnlinkedRowShareChange announces a share change of a database
// item whose sharing is unlinked.
func announceUnlinkedRowShareChange(app core.App, rowID string) error {
	unlinked, err := pageShareUnlinked(app, rowID)
	if err != nil || !unlinked {
		return err
	}
	rowPage, err := app.FindRecordById(pagesCollection, rowID)
	if err != nil {
		return err
	}
	return announceRowShareChange(app, rowPage)
}
