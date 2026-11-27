package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// objectOwnPublicID is the public ID an object answers the API with when it
// has one of its own, the one in its address: a database page's
// (rowPageOwnPublicID), a database's (databaseOwnPublicID) or a view's
// (viewOwnPublicID), or the one the MCP names it by, a comment's
// (commentOwnPublicID). Other objects, and these without one, answer with
// the identity made for the API.
func objectOwnPublicID(app core.App, objectKind, internalID string) (string, bool, error) {
	switch objectKind {
	case notionObjectIdentityRowPage:
		own, ok := rowPageOwnPublicID(internalID)
		return own, ok, nil
	case notionObjectIdentityDatabase:
		return databaseOwnPublicID(app, internalID)
	case notionObjectIdentityView:
		own, ok := viewOwnPublicID(internalID)
		return own, ok, nil
	case notionObjectIdentityComment:
		own, ok := commentOwnPublicID(internalID)
		return own, ok, nil
	}
	return "", false, nil
}

// notionObjectIdentityByOwnID finds the object of a kind that answers with
// its own ID (objectOwnPublicID) by that ID, when no identity made for the
// API names it.
func notionObjectIdentityByOwnID(app core.App, objectKind, publicID string) (notionObjectIdentity, error) {
	switch objectKind {
	case notionObjectIdentityDatabase:
		return notionDatabaseIdentityByOwnID(app, publicID)
	case notionObjectIdentityView:
		return notionViewIdentityByOwnID(app, publicID)
	case notionObjectIdentityComment:
		return notionCommentIdentityByOwnID(app, publicID)
	}
	return notionObjectIdentity{}, errInvalidNotionObjectIdentity
}

// databaseOwnPublicID is the public ID a database answers with: the ID of its
// container, which is its block's and the one in its address (/p/<ID>,
// databaseAddress.link), when that is a UUID other than the identities made
// for the API (version 8, ownPagePublicID) — as Notion's database has one ID
// in its address, its export's names, its API and its page's Markdown (seen
// 2026-11-16: /p/<ID>, <database url> and the 400 "is a database" its ID gets
// from the page routes). Its child_database block answers with it too, as
// the block's own ID. Databases answered with an identity made for the API
// (UUIDv8) before 2026-11-16; one named so still resolves to its database
// (resolveNotionObjectIdentity), and a database whose ID is no UUID answers
// with it still. A wiki's database answers with its wiki page's ID, the one
// in the wiki's address, as Notion's wiki has its page's ID
// (notion_wiki_identity.go).
func databaseOwnPublicID(app core.App, containerID string) (string, bool, error) {
	containerID = strings.TrimSpace(containerID)
	if wikiPageID, err := wikiPageOfDatabase(app, containerID); err != nil || wikiPageID != "" {
		if err != nil {
			return "", false, err
		}
		own, err := notionPagePublicID(app, wikiPageID)
		return own, err == nil, err
	}
	return containerID, ownPagePublicID(containerID), nil
}

// notionDatabaseIdentityByOwnID finds the database its own ID names
// (databaseOwnPublicID): a wiki's by its page's ID, one that stands, or one
// the trash of deleted databases holds, which Notion's API reads by that ID
// until it is deleted forever (keepDeletedDatabasePublicID).
func notionDatabaseIdentityByOwnID(app core.App, publicID string) (notionObjectIdentity, error) {
	if wiki, found, err := notionWikiDatabaseIdentity(app, publicID); err != nil || found {
		return wiki, err
	}
	if !ownPagePublicID(publicID) {
		return notionObjectIdentity{}, errInvalidNotionObjectIdentity
	}
	workspaceID := ""
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]] FROM {{leafen_database_containers}} WHERE [[id]]={:id}
		UNION ALL
		SELECT [[workspace_id]] FROM {{leafen_deleted_databases}} WHERE [[block_id]]={:id}
		LIMIT 1
	`).Bind(dbx.Params{"id": publicID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && workspaceID == "" {
		return notionObjectIdentity{}, errInvalidNotionObjectIdentity
	}
	if err != nil {
		return notionObjectIdentity{}, err
	}
	return notionObjectIdentity{
		PublicID: publicID, ObjectKind: notionObjectIdentityDatabase, Workspace: workspaceID, InternalID: publicID,
	}, nil
}

// viewOwnPublicID is the public ID a view answers with, a dashboard's widget
// too: its own ID, the one in its database's address (?v=<ID>), when that is
// a UUID other than the identities made for the API — as Notion's view has
// one ID in its address and its API (seen 2026-11-16: a database made
// through the API listed its view with the ID its address bar's ?v= showed).
// Views answered with an identity made for the API (UUIDv8) before
// 2026-11-16; one named so still resolves to its view, and a view whose ID
// is no UUID answers with it still.
func viewOwnPublicID(viewID string) (string, bool) {
	viewID = strings.TrimSpace(viewID)
	return viewID, ownPagePublicID(viewID)
}

// notionViewIdentityByOwnID finds the view its own ID names (viewOwnPublicID).
func notionViewIdentityByOwnID(app core.App, publicID string) (notionObjectIdentity, error) {
	if !ownPagePublicID(publicID) {
		return notionObjectIdentity{}, errInvalidNotionObjectIdentity
	}
	workspaceID := ""
	err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_database_views}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": publicID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && workspaceID == "" {
		return notionObjectIdentity{}, errInvalidNotionObjectIdentity
	}
	if err != nil {
		return notionObjectIdentity{}, err
	}
	return notionObjectIdentity{
		PublicID: publicID, ObjectKind: notionObjectIdentityView, Workspace: workspaceID, InternalID: publicID,
	}, nil
}
