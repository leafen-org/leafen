package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Who opens the pages of a workspace's People database (people_database.go),
// which no share or teamspace reaches: its owner changes it, as
// Notion's People lets only its owner add and write properties (seen
// 2026-11-15); its members read it; a restricted member reads only their own
// row's page, and not the database itself, so no list of its rows — a query,
// a relation's choices, an export — names anyone else to them; a guest reads
// none of it, but for an app People's page is connected to, which reads it
// as a member (people_database_connected_apps.go).

// peopleDatabasePageOf is the workspace's People database a page is, or
// whose row it is, and the person the row is ("" for the database itself).
func peopleDatabasePageOf(app core.App, page *core.Record) (database peopleDatabase, personID string, found bool, err error) {
	if page == nil {
		return peopleDatabase{}, "", false, nil
	}
	place, err := remembered(app, peopleDatabasePageMemoKey{page.Id}, func() (peopleDatabasePlace, error) {
		place := peopleDatabasePlace{}
		workspaceID := page.GetString("workspace")
		if source := page.GetString("data_source_id"); source != "" {
			// Most rows are no one's: their data source is looked for first.
			var people int
			if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspace_people_databases}} WHERE [[data_source_id]] = {:sourceId}`).
				Bind(dbx.Params{"sourceId": source}).Row(&people); err != nil || people == 0 {
				return place, err
			}
			// A row the server is still making is People's, of no one yet.
			var userID string
			err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_workspace_people_rows}} WHERE [[row_id]] = {:rowId}`).
				Bind(dbx.Params{"rowId": page.Id}).Row(&userID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return place, err
			}
			place.PersonID, place.Row = userID, true
		} else if page.GetString(privatePageOwnerField) != leafenSystemAuthorID {
			return place, nil
		}
		database, has, err := loadPeopleDatabase(app, workspaceID)
		if err != nil {
			return place, err
		}
		// The page the server is still making its database on is its already.
		if !has {
			database = newPeopleDatabase(workspaceID)
			if place.Row || database.PageID != page.Id {
				return place, nil
			}
		}
		if !place.Row && database.PageID != page.Id {
			return place, nil
		}
		place.Database, place.Found = database, true
		return place, nil
	})
	return place.Database, place.PersonID, place.Found, err
}

type peopleDatabasePageMemoKey struct{ pageID string }

type peopleDatabasePlace struct {
	Database peopleDatabase
	PersonID string
	Row      bool
	Found    bool
}

// authorizePeopleDatabasePage decides a page of People's database.
func authorizePeopleDatabasePage(app core.App, principalID string, page *core.Record, personID string, row bool, minimum pageAccessRole) (pageAuthorization, error) {
	authorization, err := authorizeWorkspace(app, principalID, page.GetString("workspace"), permissionWorkspaceRead)
	if isPermissionDenied(err) {
		// An app People's page is connected to reads it as a member (people_database_connected_apps.go).
		if connected, handled, err := authorizePeopleConnectedApp(app, principalID, page, row, minimum); handled || err != nil {
			return connected, err
		}
		return pageAuthorization{}, permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	role := pageAccessRoleView
	switch {
	case authorization.Role == workspaceRoleOwner:
		role = pageAccessRoleFullAccess
	case authorization.Restricted:
		// A restricted member reads their own person's page alone, as
		// Notion's restricted member finds only their own by "People" and
		// gets "사용 권한 없음" on People and on others' pages (seen
		// 2026-11-16 in Chrome, a free workspace's restricted member, in two
		// tabs at two times).
		role = pageAccessRoleNone
		if row && personID != "" && personID == principalID {
			role = pageAccessRoleView
		}
	}
	// A person's page lowered on its share window is unlinked from People's
	// rule, which then lets no one read it: as Notion's person page, whose
	// "<workspace>의 모든 사용자" row notes that its access comes from People
	// and that changing it narrows who reaches the page, and whose role menu,
	// "초대받은 사용자만" and "제거" change it as a page above's (seen 2026-11-16 in Chrome at 1440, in two tabs at
	// two times). The page's own shares, the lowered role among them
	// (pageInheritedShareRoles), say who opens it from then on.
	reads := role >= pageAccessRoleView
	if row && reads && role < pageAccessRoleFullAccess {
		lowered, err := pageShareUnlinked(app, page.Id)
		if err != nil {
			return pageAuthorization{}, err
		}
		if lowered {
			role = pageAccessRoleNone
		}
	}
	// A person's page raises the role of those People lets read it by a
	// share of its own — everyone in the workspace, a group, a person — as
	// Notion's person page lets its owners give everyone more than reading
	// (seen 2026-11-16: its "<workspace>의 모든 사용자" role offers 전체·편집·
	// 댓글·읽기 허용); before, the role chosen there changed nothing. Those
	// People keeps the page from stay out: a restricted member is not invited
	// to another's person page at all (people_page_invites.go).
	if row && reads && role < pageAccessRoleFullAccess {
		granted, err := effectiveUserPageAccessRole(app, authorization.WorkspaceID, page.Id, principalID)
		if err != nil {
			return pageAuthorization{}, err
		}
		role = max(role, granted)
	}
	if role < minimum {
		return pageAuthorization{}, permissionDenied()
	}
	return pageAuthorization{Workspace: authorization, Page: page, AccessRole: role}, nil
}

// authorizeIfPeoplePage decides a page that is People's database or one of
// its rows, read as a page (a row's page is written through its row page
// authority, which asks the same rule, authorizeRowPageByOwnRule); handled is
// false for any other.
func authorizeIfPeoplePage(app core.App, principalID string, page *core.Record, minimum pageAccessRole) (pageAuthorization, bool, error) {
	if isDatabaseRowPageRecord(page) && minimum > pageAccessRoleView {
		return pageAuthorization{}, false, nil
	}
	_, personID, found, err := peopleDatabasePageOf(app, page)
	if err != nil || !found {
		return pageAuthorization{}, err != nil, err
	}
	authorization, err := authorizePeopleDatabasePage(app, principalID, page, personID, isDatabaseRowPageRecord(page), minimum)
	return authorization, true, err
}

// peopleRowPersonOf is the person a row of a People database is, "" for a
// row of any other database or one the server is still making.
func peopleRowPersonOf(app core.App, dataSourceID, rowID string) (string, error) {
	var userID string
	err := app.DB().NewQuery(`SELECT [[person]].[[user_id]] FROM {{leafen_workspace_people_rows}} [[person]]
		JOIN {{leafen_workspace_people_databases}} [[database]] ON [[database]].[[workspace_id]] = [[person]].[[workspace_id]]
		WHERE [[person]].[[row_id]] = {:rowId} AND [[database]].[[data_source_id]] = {:sourceId}`).
		Bind(dbx.Params{"rowId": rowID, "sourceId": dataSourceID}).Row(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return userID, err
}

// peopleRowOfPerson is a person's row in a workspace's People, "" while the
// server has made none.
func peopleRowOfPerson(app core.App, workspaceID, userID string) (string, error) {
	var rowID string
	err := app.DB().NewQuery(`SELECT [[row_id]] FROM {{leafen_workspace_people_rows}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}`).
		Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID}).Row(&rowID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return rowID, err
}

// refusePeopleDatabaseGeneralAccessChange refuses opening People's database
// page wider: its owners hold it and its members read it by People's own
// rule, which its share window names and no one changes there, as Notion's
// (seen 2026-11-16). A share or a web link made there before is still taken
// off. A person's page raises its members' role by a share of its own, which
// People's rule reads, and lowers it by unlinking (authorizePeopleDatabasePage).
func refusePeopleDatabaseGeneralAccessChange(app core.App, authorization pageAuthorization, input publicMutationInput) error {
	if isDatabaseRowPageRecord(authorization.Page) || input.Action == publicMutationActionRemovePermission ||
		input.Action == publicMutationActionSetWebSharing && (input.Enabled == nil || !*input.Enabled) {
		return nil
	}
	if _, _, people, err := peopleDatabasePageOf(app, authorization.Page); err != nil || !people {
		return err
	}
	return publicMutationRefused("People's database is open to the workspace by its own rule")
}
