package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The page a request names as a parent (parent.page_id) is a page or a
// database page: a database page's body holds sub-pages and databases as a
// page's does, so Notion's API makes, moves and puts databases in one (seen
// 2026-11-10) as this server's screen does. Each such parent is resolved and
// authorized here, as the app's own page making authorizes it.

// errNotionParentInTrash is a parent in the trash, itself or under a page in
// it, which Notion refuses as it refuses a change to it (seen 2026-11-11,
// notion_trash_refusal.go).
var errNotionParentInTrash = errors.New("the parent page is in the trash")

// notionParentAnswers are a request's answers to a parent it can't use: one
// out of reach, given its ID (nil answers as a page not found), and one in
// the trash. wikiNotPage refuses a wiki's own page as a page not found: its
// ID is the wiki's database's in Notion's API, which a page is made in by
// parent.database_id, not parent.page_id.
type notionParentAnswers struct {
	unreached   func(parentID string) string
	trashed     notionTrashedEditWords
	wikiNotPage bool
}

// A page made in a parent in the trash is refused as a change to it; a move
// names the parent it can't load or that is in the trash (seen 2026-11-10).
// A page made by parent.page_id in a wiki's own page isn't found, as
// Notion's says (seen 2026-11-19: 404, the wiki's ID a page it finds none
// of); one made in a page of a wiki is that wiki's page there, and
// a page moved to a wiki's own page goes into the wiki (seen 2026-11-19,
// notion_page_written.go).
var (
	notionPageCreateParentAnswers = notionParentAnswers{trashed: notionTrashedEdit, wikiNotPage: true}
	notionPageMoveParentAnswers   = notionParentAnswers{
		unreached: func(parentID string) string {
			return "The new parent " + parentID + " can't be found, or editing it isn't allowed"
		},
		trashed: notionTrashedEditWords{
			itself:   func(parentID string) string { return "The new parent is in the trash: " + parentID },
			ancestor: func(parentID string) string { return "The new parent is in the trash: " + parentID },
		},
	}
)

// resolveNotionParentPageRecordID resolves the page a request names as a
// parent: a page by any ID it answers to, or a database page by its row
// page ID.
func resolveNotionParentPageRecordID(app core.App, publicID string) (string, error) {
	publicID = strings.TrimSpace(publicID)
	if pageID, err := resolveNotionPageRecordID(app, publicID); err == nil {
		return pageID, nil
	}
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, publicID)
	if err != nil {
		return "", errInvalidNotionPageIdentity
	}
	return identity.InternalID, nil
}

// authorizeNotionParentPageRecord authorizes the parent a connection names
// to take a page or a database: the person reads it through the connection,
// it is not in the trash (errNotionParentInTrash), and its body takes
// changes under its edit access (authorizePageBody), a database page's under
// its row's. A parent the connection can't reach is permissionDenied.
func authorizeNotionParentPageRecord(
	app core.App, identity notionOAuthBearerIdentity, publicID string,
) (pageAuthorization, error) {
	pageID, err := resolveNotionParentPageRecordID(app, publicID)
	if err != nil {
		return pageAuthorization{}, permissionDenied()
	}
	reached := func(page pageAuthorization) bool {
		return page.Workspace.WorkspaceID == identity.WorkspaceID && notionOAuthIdentityAllowsPage(app, identity, page)
	}
	seen, err := authorizePage(app, identity.PrincipalID, pageID, permissionWorkspaceRead)
	if err == nil && !reached(seen) {
		err = permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, notionParentHeldOr(app, identity, pageID, err)
	}
	if trashed, err := pageInTrashPath(app, pageID); err != nil {
		return pageAuthorization{}, err
	} else if trashed {
		return seen, errNotionParentInTrash
	}
	page, err := authorizePageBody(app, identity.PrincipalID, pageID, permissionWorkspaceWrite)
	if err == nil && !reached(page) {
		err = permissionDenied()
	}
	return page, notionParentHeldOr(app, identity, pageID, err)
}

// notionParentHeldOr is a parent's refusal, told as the page busy when an
// operation holds it (notion_held_page.go), not as a parent out of reach.
func notionParentHeldOr(app core.App, identity notionOAuthBearerIdentity, pageID string, err error) error {
	if !isPermissionDenied(err) {
		return err
	}
	if busy := notionPageHeld(app, identity, pageID); busy != "" {
		return pageBusyError(busy)
	}
	return err
}

// authorizeNotionParentPage authorizes a request's parent page under the
// pages:write scope, answering a parent it can't use as answers says.
func authorizeNotionParentPage(
	event *core.RequestEvent, publicID string, answers notionParentAnswers,
) (notionPageAuthorization, bool, error) {
	noteNotionAskedObject(event, "page", notionDashedUUID(strings.TrimSpace(publicID)))
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
	if err != nil || !ok {
		return notionPageAuthorization{}, false, err
	}
	page, err := authorizeNotionParentPageRecord(event.App, workspace.Identity, publicID)
	if answers.wikiNotPage && (err == nil || errors.Is(err, errNotionParentInTrash)) &&
		page.Page != nil && page.Page.GetBool("is_wiki") {
		return notionPageAuthorization{}, false, notionPageNotFound(event)
	}
	if errors.Is(err, errNotionParentInTrash) {
		message, messageErr := notionTrashedEditMessage(event.App, page.Page.Id, true, false, answers.trashed)
		if messageErr == nil && message != "" {
			return notionPageAuthorization{}, false, notionPageValidationError(event, message)
		}
		err = messageErr
	}
	if isPermissionDenied(err) && answers.unreached != nil {
		given := strings.TrimSpace(publicID)
		if normalized, ok := normalizeNotionUUIDRequest(given); ok {
			given = normalized
		}
		return notionPageAuthorization{}, false, notionPageValidationError(event, answers.unreached(given))
	}
	if isPermissionDenied(err) {
		return notionPageAuthorization{}, false, notionPageNotFound(event)
	}
	if err != nil {
		return notionPageAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	return notionPageAuthorization{Identity: workspace.Identity, Page: page}, true, nil
}

// refuseNotionMoveIntoItself refuses a page moved into itself as Notion does
// (seen 2026-11-10: 400 naming the page's ID),
// a page and a database page alike.
func refuseNotionMoveIntoItself(event *core.RequestEvent, pageID, parentID string) (bool, error) {
	if pageID != parentID {
		return false, nil
	}
	publicID, err := notionPagePublicID(event.App, pageID)
	if err != nil {
		publicID = pageID
	}
	return true, notionPageValidationError(event, "A page can't be its own parent: "+publicID)
}

// notionDatabaseParentSource is the data source a request reaches through a
// database it names as a page's parent (parent.database_id, and a move's
// parent.page_id that names one), as Notion's takes it (seen 2026-11-10): a
// database of one data source of its own is that source; one of several is
// refused; an ID that names a page the connection reaches is refused as one;
// any other is a database not found, or unreached's answer when given. The
// source's public ID comes back, or false once the request is answered.
func notionDatabaseParentSource(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, publicID string, unreached func() error,
) (string, bool, error) {
	given := strings.TrimSpace(publicID)
	noteNotionAskedObject(event, "database", notionDashedUUID(given))
	notFound := func() error {
		if unreached != nil {
			return unreached()
		}
		return notionPageNotFound(event)
	}
	internalError := func(cause error) error {
		return notionRequestNotCompleted(event, cause)
	}
	database, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDatabase, notionDatabaseIDForBlock(event.App, given))
	if errors.Is(err, errInvalidNotionObjectIdentity) || errors.Is(err, sql.ErrNoRows) ||
		err == nil && database.Workspace != identity.WorkspaceID {
		if pageID, err := resolveNotionPageRecordID(event.App, given); err == nil {
			page, err := authorizePage(event.App, identity.PrincipalID, pageID, permissionWorkspaceRead)
			if err == nil && page.Workspace.WorkspaceID == identity.WorkspaceID && notionOAuthIdentityAllowsPage(event.App, identity, page) {
				shown, idErr := notionPagePublicID(event.App, pageID)
				if idErr != nil {
					return "", false, internalError(err)
				}
				return "", false, notionPageValidationError(event, "Provided database_id "+shown+
					" is a page, not a database: use the pages API, or give the database's own ID.")
			}
		}
		return "", false, notFound()
	}
	if err != nil {
		return "", false, internalError(err)
	}
	container, _, err := loadNotionDatabaseContainer(event.App, identity, database.InternalID, permissionWorkspaceRead)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return "", false, notFound()
	}
	if err != nil {
		return "", false, internalError(err)
	}
	sources := []string{}
	if err := event.App.DB().NewQuery(`
		SELECT [[member]].[[data_source_id]] FROM {{leafen_database_container_sources}} AS [[member]]
		WHERE [[member]].[[container_id]] = {:id} AND COALESCE([[member]].[[source_container_id]], '') = ''
		ORDER BY [[member]].[[position]], [[member]].[[data_source_id]]
	`).Bind(dbx.Params{"id": container.ID}).Column(&sources); err != nil {
		return "", false, internalError(err)
	}
	switch {
	case len(sources) == 0:
		return "", false, notFound()
	case len(sources) > 1:
		return "", false, notionPageValidationError(event, "This API version can't work with a database of more than one data source.")
	}
	source, err := ensureNotionObjectIdentity(event.App, notionObjectIdentityDataSource, container.WorkspaceID, sources[0])
	if err != nil {
		return "", false, internalError(err)
	}
	return source, true, nil
}

// notionMoveParent checks a move's new parent before the page it moves, as
// Notion's does (seen 2026-11-10: a page that isn't there, moved under a
// parent that isn't, is refused for the parent, named by its ID, a page or
// a data source alike). A page_id
// that names a database is its one data source, which Notion's moves a page
// into. It gives back the parent to move under, or the request's answer.
func notionMoveParent(event *core.RequestEvent, parent notionPageParentRequest) (notionPageParentRequest, bool, error) {
	unreached := func(publicID string) func() error {
		return func() error {
			given := strings.TrimSpace(publicID)
			if normalized, ok := normalizeNotionUUIDRequest(given); ok {
				given = normalized
			}
			return notionPageValidationError(event, notionPageMoveParentAnswers.unreached(given))
		}
	}
	switch notionPageParentKind(parent) {
	case "page":
		if _, err := resolveNotionParentPageRecordID(event.App, parent.PageID); err != nil {
			if _, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDatabase,
				notionDatabaseIDForBlock(event.App, strings.TrimSpace(parent.PageID))); err == nil {
				workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
				if err != nil || !ok {
					return parent, false, err
				}
				source, ok, err := notionDatabaseParentSource(event, workspace.Identity, parent.PageID, unreached(parent.PageID))
				if err != nil || !ok {
					return parent, false, err
				}
				return notionPageParentRequest{Type: "data_source_id", DataSourceID: source}, true, nil
			}
		}
		_, ok, err := authorizeNotionParentPage(event, parent.PageID, notionPageMoveParentAnswers)
		return parent, ok, err
	case "data_source":
		workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
		if err != nil || !ok {
			return parent, false, err
		}
		source, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDataSource, strings.TrimSpace(parent.DataSourceID))
		if err == nil && source.Workspace == workspace.Identity.WorkspaceID {
			_, err = authorizeNotionDataSourceParentCurrent(event.App, workspace.Identity, source.InternalID, permissionWorkspaceWrite)
		}
		if err == nil {
			return parent, true, nil
		}
		if errors.Is(err, errInvalidNotionObjectIdentity) || errors.Is(err, sql.ErrNoRows) ||
			errors.Is(err, errDatabaseRowPageNotFound) || source.Workspace != workspace.Identity.WorkspaceID {
			return parent, false, unreached(parent.DataSourceID)()
		}
		return parent, false, notionRequestNotCompleted(event, err)
	}
	return parent, true, nil
}
