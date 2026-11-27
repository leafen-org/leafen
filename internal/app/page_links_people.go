package app

import (
	"net/url"

	"github.com/pocketbase/pocketbase/core"
)

// peoplePageLink is where an address naming the workspace's People database
// — its page, or the database by its own ID — or one of its rows opens, for
// someone who may read it: People's own screen, /people, and a row its
// person's profile, /people/<person>, where the app opens People's page and
// its rows from everywhere else (usePersonRowPage), as Notion's People is one
// screen and a person's page that person (seen 2026-11-15); opened as a
// database the address showed People's rows with "새로 만들기" and "+ 새 페이지".
// handled is false for any other address, and for one its reader may not
// read, which the other routes answer.
func peoplePageLink(app core.App, principalID, id string) (map[string]string, bool, error) {
	pageID, err := resolveNotionPageRecordID(app, id)
	if err != nil {
		// The database by its own ID is held by its page.
		address, err := loadDatabaseAddress(app, principalID, id)
		if err != nil || address.pageID == "" {
			return nil, false, err
		}
		pageID = address.pageID
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return nil, false, nil
	}
	_, personID, found, err := peopleDatabasePageOf(app, page)
	if err != nil || !found {
		return nil, false, err
	}
	row := isDatabaseRowPageRecord(page)
	// A row the server is still making names no one yet.
	if row && personID == "" {
		return nil, false, nil
	}
	if _, err := authorizePeopleDatabasePage(app, principalID, page, personID, row, pageAccessRoleView); isPermissionDenied(err) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}
	if row {
		return map[string]string{"path": "/people/" + url.PathEscape(personID)}, true, nil
	}
	return map[string]string{"path": "/people"}, true, nil
}
