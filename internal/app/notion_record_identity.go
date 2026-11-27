package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page record answers the Notion API with one public ID for its life, the
// ID in its address: the page codec's for a 15-character ID, or its own ID,
// a UUID, a database page's too (rowPageOwnPublicID). A record that turned
// into the other kind keeps the kind of ID it had, as
// leafen_page_public_identities records — the same ID now, but for a
// database page whose ID has no page ID of its own, which answers with its
// row identity. Handlers are chosen by what the record is now, never by the
// shape of its ID.

type pageRecordKeptIdentity struct {
	Kind        string `db:"kept_kind"`
	WorkspaceID string `db:"workspace_id"`
}

func loadPageRecordKeptIdentity(app core.App, recordID string) (pageRecordKeptIdentity, error) {
	kept := pageRecordKeptIdentity{}
	err := app.DB().NewQuery(`
		SELECT [[kept_kind]], [[workspace_id]] FROM {{leafen_page_public_identities}} WHERE [[page_id]] = {:id}
	`).Bind(dbx.Params{"id": recordID}).One(&kept)
	if errors.Is(err, sql.ErrNoRows) {
		return pageRecordKeptIdentity{}, nil
	}
	return kept, err
}

// keepPageRecordPublicIdentity records, as a record turns into the other
// kind, the kind of ID it goes on answering with: the one it had, or none
// when it returns to the kind it began as.
func keepPageRecordPublicIdentity(app core.App, workspaceID, recordID, nowKind string) error {
	kept, err := loadPageRecordKeptIdentity(app, recordID)
	if err != nil {
		return err
	}
	if kept.Kind != "" {
		_, err = app.DB().NewQuery(`DELETE FROM {{leafen_page_public_identities}} WHERE [[page_id]] = {:id}`).
			Bind(dbx.Params{"id": recordID}).Execute()
		return err
	}
	keep := notionObjectIdentityRowPage
	if nowKind == notionObjectIdentityRowPage {
		// A page with no page ID of its own never had one to keep.
		if _, err := pageOwnPublicID(recordID); err != nil {
			return nil
		}
		keep = "page"
	} else if _, err := ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, workspaceID, recordID); err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_page_public_identities}} ([[page_id]], [[workspace_id]], [[kept_kind]])
		VALUES ({:id}, {:workspaceId}, {:kind})
	`).Bind(dbx.Params{"id": recordID, "workspaceId": workspaceID, "kind": keep}).Execute()
	return err
}

// notionPagePublicID is the public ID a page answers with: the one a record
// kept as it turned into the other kind, or else a database page's and a
// page's own public ID.
func notionPagePublicID(app core.App, pageID string) (string, error) {
	kept, err := loadPageRecordKeptIdentity(app, pageID)
	if err != nil {
		return "", err
	}
	switch kept.Kind {
	case notionObjectIdentityRowPage:
		return ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, kept.WorkspaceID, pageID)
	case "page":
		return pageOwnPublicID(pageID)
	}
	var workspaceID, dataSourceID string
	err = app.DB().NewQuery(`SELECT [[workspace]], [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&workspaceID, &dataSourceID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if dataSourceID != "" {
		return ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, workspaceID, pageID)
	}
	return pageOwnPublicID(pageID)
}

// notionPageAnswerID is the ID an answer names a page by, whatever ID it
// was asked by — without dashes, or a database page's former row identity:
// the one it answers with (notionPagePublicID), as Notion's answer names a
// page asked by its ID without dashes by the dashed one (seen 2026-11-16).
func notionPageAnswerID(app core.App, pageID, asked string) string {
	if publicID, err := notionPagePublicID(app, pageID); err == nil {
		return publicID
	}
	return asked
}

// pageOwnPublicID is the public ID a page answers with as a page: the page
// codec's for a page with a 15-character ID, and its own ID for a page made
// from a block, whose ID is a UUID as its block's is — as in Notion, a page
// and its child_page block share an ID. UUIDv8 IDs are the ones this server
// makes for its own identities, so a page never answers with one.
func pageOwnPublicID(pageID string) (string, error) {
	if ownPagePublicID(pageID) {
		return pageID, nil
	}
	return notionCompatiblePageID(pageID)
}

func ownPagePublicID(value string) bool {
	normalized, ok := normalizeNotionUUIDRequest(value)
	return ok && normalized == value && value[14] != '8'
}

// resolvePageOwnPublicID resolves a page's own public ID into the ID of the
// record it would name, without knowing whether that record exists.
func resolvePageOwnPublicID(publicID string) (string, error) {
	if pageID, err := resolveNotionCompatiblePageID(publicID); err == nil {
		return pageID, nil
	}
	normalized, ok := normalizeNotionUUIDRequest(publicID)
	if !ok || !ownPagePublicID(normalized) {
		return "", errInvalidNotionPageIdentity
	}
	return normalized, nil
}

// resolveNotionPageRecordID resolves a page's public ID: a codec ID, the ID
// in its address, or the row identity a page that was a database page keeps.
func resolveNotionPageRecordID(app core.App, publicID string) (string, error) {
	if pageID, err := resolveNotionCompatiblePageID(publicID); err == nil {
		return pageID, nil
	}
	if pageID, found, err := pageRecordAtAddress(app, publicID); err != nil || found {
		return pageID, err
	}
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, publicID)
	if err != nil {
		return "", errInvalidNotionPageIdentity
	}
	kept, err := loadPageRecordKeptIdentity(app, identity.InternalID)
	if err != nil {
		return "", err
	}
	if kept.Kind != notionObjectIdentityRowPage {
		return "", errInvalidNotionPageIdentity
	}
	return identity.InternalID, nil
}

// pageRecordAtAddress finds the page the ID in its address names: its own
// ID, as the browser's address bar, this server's links and MCP give it — 15
// characters for a page made before, a UUID (with or without its dashes) for
// one made since. It names the page whatever ID the page answers the API
// with, a database page's row identity included, as Notion's page has one ID
// in its address and its API (2026-11-14: a database page whose ID is a UUID
// was not found by the ID in its address, while one of 15 characters was).
func pageRecordAtAddress(app core.App, publicID string) (string, bool, error) {
	id := publicID
	if normalized, isUUID := normalizeNotionUUIDRequest(id); isUUID {
		id = normalized
	}
	if !validPageRecordID(id) {
		return "", false, nil
	}
	var found int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id}).Row(&found); err != nil {
		return "", false, err
	}
	return id, found == 1, nil
}

// notionPublicIDNamesDatabase reports whether a public ID asked for as a
// page names a database instead: a database's own ID, or the page of a
// full-page database.
func notionPublicIDNamesDatabase(app core.App, publicID string) bool {
	if _, err := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, publicID); err == nil {
		return true
	}
	pageID, err := resolveNotionPageRecordID(app, publicID)
	if err != nil {
		return false
	}
	var kind string
	if err := app.DB().NewQuery(`SELECT [[kind]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&kind); err != nil {
		return false
	}
	return kind == pageKindDatabase
}

// notionPublicIDNamesPage reports whether a public ID names a page that is
// not a database page now, whatever kind of ID it keeps.
func notionPublicIDNamesPage(app core.App, publicID string) bool {
	pageID, err := resolveNotionPageRecordID(app, publicID)
	if err != nil {
		return false
	}
	var dataSourceID string
	if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&dataSourceID); err != nil {
		// A codec ID of no record still names a page that is gone.
		return errors.Is(err, sql.ErrNoRows)
	}
	return dataSourceID == ""
}
