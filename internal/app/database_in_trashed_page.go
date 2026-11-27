package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// An inline database of one source in a page in the trash, or in a page under
// one, is a line of the trash's databases of its own, as Notion's trash lists
// it beside the page holding it (seen 2026-11-12: "T inline" with a table
// icon, "… / W682 T page"; "T sub inline", "… / T sub"); one of several
// sources lists as its pages alone ("T m one row 1 · … / T m one", no line
// for the database). Restoring one takes
// it out of that page as a full-page database at the top of the reader's
// private pages, with its sources, rows and views, as Notion's does after
// asking first, warning that the page goes to private pages and loses the
// access its deleted parent gave; deleting one forever takes
// its block out of that page and deletes it with its rows, as a deleted
// database's deletion forever does (deleted_databases.go). A full-page
// database under such a page is a page of the trash of its own
// (AuthenticatedTrashView.tsx), and a wiki's database goes with its page.

// trashedPageTreeCTE names, for a WITH clause, a workspace's pages in the
// trash and the pages under them as [[trashed_tree]], each once with when it
// went there in milliseconds: a page sent there on its own from its own
// time, a page under it from the time of the page above it.
func trashedPageTreeCTE() string {
	return `RECURSIVE [[trashed_tree]]([[id]], [[deleted_at]]) AS (
			SELECT [[trashed_page]].[[id]],
				CAST(ROUND((julianday([[trashed_page]].[[trashed_at]]) - 2440587.5) * 86400000) AS INTEGER)
			FROM {{leafen_pages}} AS [[trashed_page]]
			WHERE [[trashed_page]].[[workspace]] = {:workspaceId} AND [[trashed_page]].[[in_trash]] = 1
				AND [[trashed_page]].[[trashed_at]] != '' AND [[trashed_page]].[[deletion_job_id]] = ''
				AND [[trashed_page]].[[data_source_id]] = ''
			UNION
			-- A page under it sent to the trash on its own lists from its own time.
			SELECT [[child]].[[id]], [[trashed_tree]].[[deleted_at]] FROM [[trashed_tree]]
			CROSS JOIN {{leafen_pages}} AS [[child]] ON [[child]].[[parent_id]] = [[trashed_tree]].[[id]]
			WHERE [[child]].[[data_source_id]] = '' AND [[child]].[[in_trash]] = 0 AND [[child]].[[deletion_job_id]] = ''
		)`
}

// databasesInTrashedPagesListSQL selects, as the trash lists its databases
// (deleted_database_containers.go), the source databases of one source held
// in the pages of [[trashed_tree]], each by its block, from when its page
// went to the trash, kept there as long as that page is.
func databasesInTrashedPagesListSQL() string {
	return `
	SELECT [[held]].[[block_id]] AS [[root_block_id]], [[held]].[[page_id]], [[trashed_tree]].[[deleted_at]],
		[[trashed_tree]].[[deleted_at]] + {:pageTrashRetention} AS [[expires_at]], COALESCE((
			SELECT [[held_source]].[[data_source_id]] FROM {{leafen_database_container_sources}} AS [[held_source]]
			WHERE [[held_source]].[[container_id]] = [[held]].[[id]] AND [[held_source]].[[source_container_id]] = ''
			ORDER BY [[held_source]].[[position]], [[held_source]].[[data_source_id]] LIMIT 1
		), '') AS [[data_source_id]], 0 AS [[in_place]], [[held]].[[id]] AS [[container_id]], 1 AS [[in_trashed_page]]
	FROM [[trashed_tree]]
	JOIN {{leafen_pages}} AS [[holder]] ON [[holder]].[[id]] = [[trashed_tree]].[[id]]
	JOIN {{leafen_database_containers}} AS [[held]] ON [[held]].[[page_id]] = [[holder]].[[id]] AND [[held]].[[kind]] = 'source'
	WHERE [[holder]].[[kind]] != '` + pageKindDatabase + `' AND [[holder]].[[is_wiki]] = 0
		AND (SELECT COUNT(*) FROM {{leafen_database_container_sources}} AS [[held_own]]
			WHERE [[held_own]].[[container_id]] = [[held]].[[id]] AND [[held_own]].[[source_container_id]] = '') = 1
		AND NOT ` + wikiDatabaseContainerSQL("[[held]]") + `
		AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} AS [[held_trash]]
			WHERE [[held_trash]].[[container_id]] = [[held]].[[id]])
		AND ({:rootBlockId} = '' OR [[held]].[[block_id]] = {:rootBlockId})`
}

// restoreDatabaseFromTrashedPage makes the database a full-page database at
// the top of the person's private pages: a page there, named after its
// source, as Notion's comes back ("T inline"), that
// the database's block moves into, as an inline database turned into a page
// does (notion_database_turn.go). A move that can't be made takes the page
// back to the trash; one still under way when the wait ends goes on.
func restoreDatabaseFromTrashedPage(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, userID, workspaceID string, item trashedDatabaseContainer,
) (string, error) {
	identity := notionOAuthBearerIdentity{PrincipalID: userID, WorkspaceID: workspaceID}
	title := item.Title
	pageID := ""
	err := event.App.RunInTransaction(func(txApp core.App) error {
		created, status, err := admitPageCreationTransactionWithLock(txApp, userID, pageCreationAdmissionInput{
			RequestID: "trashed-page-database-restore:" + newDiagnosticReference(), WorkspaceID: workspaceID,
			Title: title, ContentInBody: true,
		}, pageLockIgnored)
		if err != nil {
			return err
		}
		if status != pageCreationApplied {
			return graphConflictError(errorCodeGraphPageCursorConflict, "The page the database comes back as could not be made.")
		}
		pageID = created.Page.ID
		// At the top of private pages, as Notion's restore puts it.
		first, err := firstPageSiblingPosition(txApp, workspaceID, "")
		if err != nil {
			return err
		}
		input := pageMetadataInput{
			Title: title, Position: &first, MutationID: "trashed-page-database-restore-place:" + newDiagnosticReference(),
			BaseCursor: created.Page.Cursor,
		}
		_, placed, err := applyPageMetadataTransaction(txApp, userID, pageID, input, pageMetadataRequestHash(pageID, input))
		if err == nil && placed != pageMetadataApplied {
			err = graphConflictError(errorCodeGraphPageCursorConflict, "The page the database comes back as could not be placed.")
		}
		return err
	})
	if err != nil {
		return "", err
	}
	container := notionDatabaseContainer{ID: item.ContainerID, PageID: item.PageID, BlockID: item.RootBlockID}
	if err := moveNotionInlineDatabase(event.Request.Context(), event.App, identity, container, pageID); err != nil {
		var request *graphRequestError
		if !errors.As(err, &request) || !strings.Contains(request.message, "still moving") {
			_ = trashNotionTurnPage(event.App, identity, pageID)
		}
		return "", err
	}
	return pageID, setNotionTurnPageKind(event.Request.Context(), ygo, identity, pageID, pageKindDatabase)
}

// deleteDatabaseInTrashedPageForever takes the database's block out of the
// page in the trash, which keeps it as a deleted database
// (deleted_databases.go), and deletes that forever in the same change; one
// another database relates to stays where it is.
func deleteDatabaseInTrashedPageForever(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, userID string, item trashedDatabaseContainer,
) error {
	_, err := ygo.applyContentGraphMutationContext(
		event.Request.Context(), userID, item.PageID,
		graphMutationInput{
			MutationID: "trashed-page-database-delete:" + newDiagnosticReference(),
			Operations: []graphOperation{{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: item.RootBlockID}}},
		},
		contentGraphMutationHooks{
			AllowTrashedPage: true,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return authorizePageBody(txApp, userID, item.PageID, permissionWorkspaceWrite)
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				stored := storedDeletedDatabase{}
				err := txApp.DB().NewQuery(`
					SELECT [[block_id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_ids]],[[block_content]],[[title]],
						[[deleted_at]],[[expires_at]]
					FROM {{leafen_deleted_databases}} WHERE [[block_id]]={:blockId}
				`).Bind(dbx.Params{"blockId": item.RootBlockID}).One(&stored)
				if errors.Is(err, sql.ErrNoRows) {
					return nil
				}
				if err != nil {
					return err
				}
				return purgeDeletedDatabase(txApp, stored)
			},
		},
	)
	return err
}
