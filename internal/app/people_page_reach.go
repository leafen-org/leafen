package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What People shows a reader follows the person pages they open
// (authorizePeopleDatabasePage): a person's profile (/people/<person>) is
// their page, and the people list and People's database leave out the people
// whose page the reader doesn't open, as Notion's People database, a
// database, lists only the rows its reader may open. A restricted member
// opens only their own; a person's page lowered on its share window before
// unlinking one was refused (people_page_linked.go) is the only kind a member
// may not open, so only those are asked about one by one.

// peoplePageReach is which people's pages a reader of a workspace's People opens.
type peoplePageReach struct {
	closedPeople map[string]bool
	closedRows   map[string]bool
}

// opensPerson is whether the reader opens the person's page, and so their profile and row.
func (reach peoplePageReach) opensPerson(userID string) bool { return !reach.closedPeople[userID] }

// opensRow is whether the reader opens a row of People's database.
func (reach peoplePageReach) opensRow(rowID string) bool { return !reach.closedRows[rowID] }

// loadPeoplePageReach reads it for a reader of the workspace's People.
func loadPeoplePageReach(app core.App, authorization workspaceAuthorization) (peoplePageReach, error) {
	reach := peoplePageReach{closedPeople: map[string]bool{}, closedRows: map[string]bool{}}
	if authorization.Role == workspaceRoleOwner {
		return reach, nil
	}
	lowered := []struct {
		UserID string `db:"user_id"`
		RowID  string `db:"row_id"`
	}{}
	// A restricted member is asked about every row but their own's closed.
	asked := `JOIN {{leafen_page_share_unlinks}} [[unlink]] ON [[unlink]].[[page_id]] = [[person]].[[row_id]]`
	if authorization.Restricted {
		asked = ""
	}
	if err := app.DB().NewQuery(`SELECT [[person]].[[user_id]], [[person]].[[row_id]] FROM {{leafen_workspace_people_rows}} [[person]]
		` + asked + ` WHERE [[person]].[[workspace_id]] = {:workspaceId}`).
		Bind(dbx.Params{"workspaceId": authorization.WorkspaceID}).All(&lowered); err != nil {
		return reach, err
	}
	for _, person := range lowered {
		if authorization.Restricted && person.UserID != authorization.PrincipalID {
			reach.closedPeople[person.UserID], reach.closedRows[person.RowID] = true, true
			continue
		}
		page, err := app.FindRecordById(pagesCollection, person.RowID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return reach, err
		}
		if _, err := authorizePeopleDatabasePage(app, authorization.PrincipalID, page, person.UserID, true, pageAccessRoleView); isPermissionDenied(err) {
			reach.closedPeople[person.UserID], reach.closedRows[person.RowID] = true, true
		} else if err != nil {
			return reach, err
		}
	}
	return reach, nil
}

// pageInheritedShareRoles is the widest role the pages above a page give each
// principal, People's rule among them for a person's page: everyone in the
// workspace reads it as by a share on People's database page, which their
// page lowers or takes off by unlinking, as a page lowers what a page above
// gives (page_share_inheritance.go).
func pageInheritedShareRoles(app core.App, page *core.Record, workspaceID string, ancestorIDs []string) (map[sharePrincipalKey]pageAccessRole, error) {
	roles, err := inheritedShareRoles(app, workspaceID, ancestorIDs)
	if err != nil || !isDatabaseRowPageRecord(page) {
		return roles, err
	}
	if _, _, people, err := peopleDatabasePageOf(app, page); err != nil || !people {
		return roles, err
	}
	everyone := sharePrincipalKey{publicMutationPrincipalWorkspace, workspaceID}
	roles[everyone] = max(roles[everyone], pageAccessRoleView)
	return roles, nil
}

// keep leaves out of a list of people those whose page the reader doesn't open.
func (reach peoplePageReach) keep(people []peopleDirectoryRow) []peopleDirectoryRow {
	if len(reach.closedPeople) == 0 {
		return people
	}
	kept := make([]peopleDirectoryRow, 0, len(people))
	for _, person := range people {
		if reach.opensPerson(person.UserID) {
			kept = append(kept, person)
		}
	}
	return kept
}

// peoplePageOpens is nil when the reader opens the person's page, which their
// profile is; a profile they don't is not found, as a page they don't open.
func peoplePageOpens(app core.App, authorization workspaceAuthorization, personID string) error {
	reach, err := loadPeoplePageReach(app, authorization)
	if err == nil && !reach.opensPerson(personID) {
		err = errPersonProfileNotFound
	}
	return err
}

// authorizeRowPageByOwnRule decides a row's page that doesn't open by its
// database's page: a person's page, by People's rule (authorizePeopleDatabasePage)
// — its owners hold it lowered or not, as Notion's person page keeps
// "<workspace>의 소유자 — 전체 허용" over a lowered everyone row (seen
// 2026-11-16) — and one whose sharing is unlinked, by its own shares. own is
// false for any other.
func authorizeRowPageByOwnRule(app core.App, principalID string, rowPage *core.Record, minimum pageAccessRole) (pageAuthorization, bool, error) {
	if _, personID, people, err := peopleDatabasePageOf(app, rowPage); err != nil || people {
		if err != nil {
			return pageAuthorization{}, true, err
		}
		authorization, err := authorizePeopleDatabasePage(app, principalID, rowPage, personID, true, minimum)
		return authorization, true, err
	}
	unlinked, err := pageShareUnlinked(app, rowPage.Id)
	if err != nil || !unlinked {
		return pageAuthorization{}, err != nil, err
	}
	authorization, err := authorizeUnlinkedRowPage(app, principalID, rowPage, minimum)
	return authorization, true, err
}
