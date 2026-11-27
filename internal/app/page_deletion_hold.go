package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// The 30 days after a permanent deletion (the page trash decision's
// 2026-09-30 addition, as Notion keeps a deleted page): a page someone deletes
// for good, or whose 30 days in the trash are up, leaves everyone's sight at
// once, its fence keeping every read and write out as a deletion under way
// does, and goes for good only 30 days later. Until then the workspace's owner
// sees it under 보안 › 데이터 보존 and can bring it back or delete it at once.
// An active legal hold keeps it past the 30 days.

const (
	pageDeletionHoldPeriod      = 30 * 24 * time.Hour
	pageDeletionHoldFencePrefix = "held:"
	// The owner's list reads this many at a time, newest first, and goes
	// on with its nextCursor to the oldest.
	pageDeletionHoldListWindow = 200

	pageDeletionAuditHeld      = "page.permanently_deleted"
	pageDeletionAuditRecovered = "page.deletion_recovered"
	pageDeletionAuditPurged    = "page.purged"
)

var errPageDeletionJobExists = errors.New("a deletion job already has this page")

type storedPageDeletionHold struct {
	PageID      string `db:"page_id"`
	WorkspaceID string `db:"workspace_id"`
	ActorID     string `db:"actor_id"`
	RequestID   string `db:"request_id"`
	HeldAt      int64  `db:"held_at"`
	PurgeAt     int64  `db:"purge_at"`
	RetryAt     int64  `db:"retry_at"`
}

// pageNotHeldForDeletionSQL says a sidebar page is neither in its 30 days nor
// under a page that is. The holds are few and leave quickly from a workspace's
// lists, so the walk costs next to nothing when there are none.
func pageNotHeldForDeletionSQL(alias, workspace string) string {
	return alias + `.[[id]] NOT IN (
		WITH RECURSIVE [[held_pages]] ([[id]]) AS (
			SELECT [[page_id]] FROM {{leafen_page_deletion_holds}} WHERE [[workspace_id]] = ` + workspace + `
			UNION
			SELECT [[child]].[[id]] FROM {{leafen_pages}} AS [[child]]
			JOIN [[held_pages]] ON [[child]].[[parent_id]] = [[held_pages]].[[id]]
		)
		SELECT [[id]] FROM [[held_pages]])`
}

// pageOutsideDeletionFenceSQL says the page or database page the expression
// names is outside every deletion's fence, as the reads behind the fence see
// it: neither in its 30 days or being deleted itself, nor under a page,
// database page or database that is. Every export keeps to it, so a deletion
// leaves exported files at once as it leaves everyone's sight. A page's
// sub-pages and databases come under it by their parent, a database's pages
// by the page holding the database. The fences are few, so the walk costs
// next to nothing when there are none; workspace is a bound value. Each step
// looks its children up from the page it is on (CROSS JOIN keeps that order:
// joined the other way, SQLite read every page of the workspace for each
// fenced page, 2026-11-12).
func pageOutsideDeletionFenceSQL(pageID, workspace string) string {
	return pageID + ` NOT IN (
		WITH RECURSIVE [[fenced_pages]] ([[id]]) AS (
			SELECT [[id]] FROM {{leafen_pages}} WHERE [[workspace]] = ` + workspace + ` AND [[deletion_job_id]] != ''
			UNION
			SELECT [[child]].[[id]] FROM [[fenced_pages]]
			CROSS JOIN {{leafen_pages}} AS [[child]] ON [[child]].[[workspace]] = ` + workspace + `
				AND [[child]].[[parent_id]] = [[fenced_pages]].[[id]]
			UNION
			SELECT [[fenced_row]].[[id]] FROM [[fenced_pages]]
			CROSS JOIN {{leafen_database_rows}} AS [[fenced_row]] ON [[fenced_row]].[[page_id]] = [[fenced_pages]].[[id]]
		)
		SELECT [[id]] FROM [[fenced_pages]])`
}

// pageWithoutOwnDeletionFenceSQL says the page or database page the
// expression names keeps no deletion fence of its own, as a row deleted for
// good keeps for its 30 days. It stands for pageOutsideDeletionFenceSQL where
// what holds the page is already known to be outside every fence: a page
// reached from an export's root, or a database page whose database's page
// the export takes.
func pageWithoutOwnDeletionFenceSQL(pageID string) string {
	return `NOT EXISTS (SELECT 1 FROM {{leafen_pages}} AS [[fenced_page]]
		WHERE [[fenced_page]].[[id]] = ` + pageID + ` AND [[fenced_page]].[[deletion_job_id]] != '')`
}

// pageDeletionFence is the fence of a page or a row in its 30 days; a row's
// longer ID doesn't fit the fence's field, so its digest stands in.
func pageDeletionFence(pageID string) string {
	if len(pageID) <= 27 {
		return pageDeletionHoldFencePrefix + pageID
	}
	digest := sha256.Sum256([]byte(pageID))
	return pageDeletionHoldFencePrefix + hex.EncodeToString(digest[:])[:27]
}

func pageDeletionHoldFence(fence string) bool {
	return strings.HasPrefix(fence, pageDeletionHoldFencePrefix)
}

func loadPageDeletionHold(app core.App, pageID string) (storedPageDeletionHold, error) {
	hold := storedPageDeletionHold{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]], [[workspace_id]], [[actor_id]], [[request_id]], [[held_at]], [[purge_at]], [[retry_at]]
		FROM {{leafen_page_deletion_holds}} WHERE [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).One(&hold)
	return hold, err
}

// holdPageForPermanentDeletion takes a page in the trash out of everyone's
// sight for its 30 days. errPageDeletionJobExists says a deletion job already
// has it, one begun before these 30 days existed or one a request goes on with.
func holdPageForPermanentDeletion(
	app core.App, subject pageAccessSubject, pageID, requestID string,
) (storedPageDeletionHold, bool, error) {
	hold, duplicate, actorID := storedPageDeletionHold{}, false, subject.actorID()
	err := app.RunInTransaction(func(txApp core.App) error {
		existing, err := loadPageDeletionHold(txApp, pageID)
		if err == nil {
			if _, err := subject.authorizeWorkspace(txApp, existing.WorkspaceID, permissionWorkspaceOwner); err != nil {
				return err
			}
			hold, duplicate = existing, true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, load := range []func() (storedPagePermanentDeleteJob, error){
			func() (storedPagePermanentDeleteJob, error) {
				return loadPagePermanentDeleteByRequest(txApp, actorID, requestID)
			},
			func() (storedPagePermanentDeleteJob, error) {
				return loadPagePermanentDeleteByRoot(txApp, actorID, pageID)
			},
		} {
			if _, err := load(); err == nil {
				return errPageDeletionJobExists
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		authorization, err := subject.authorizePage(txApp, pageID, permissionWorkspaceOwner)
		if err != nil {
			return err
		}
		leased, err := workspaceMarkdownReplacePageLeased(txApp, pageID, "")
		if err != nil {
			return err
		}
		if leased {
			return errPagePermanentDeleteConflict
		}
		// A page under a page in the trash goes on its own too
		// (page_permanent_delete_storage.go).
		if trashed, err := pageInTrashPath(txApp, pageID); err != nil {
			return err
		} else if !trashed {
			return errPagePermanentDeleteNotTrashed
		}
		if err := assertNoActiveLegalHoldForPermanentDelete(
			txApp, authorization.Workspace.Organization.OrganizationID,
			legalHoldDeletionTarget{
				WorkspaceID: authorization.Workspace.WorkspaceID,
				PageIDs:     []string{pageID}, CustodianUserIDs: []string{authorization.Page.GetString("owner")},
			},
		); err != nil {
			return err
		}
		now := time.Now().UTC()
		hold = storedPageDeletionHold{
			PageID: pageID, WorkspaceID: authorization.Workspace.WorkspaceID, ActorID: actorID, RequestID: requestID,
			HeldAt: now.UnixMilli(), PurgeAt: now.Add(pageDeletionHoldPeriod).UnixMilli(),
		}
		if _, err := txApp.DB().Insert("leafen_page_deletion_holds", dbx.Params{
			"page_id": hold.PageID, "workspace_id": hold.WorkspaceID, "actor_id": hold.ActorID,
			"request_id": hold.RequestID, "held_at": hold.HeldAt, "purge_at": hold.PurgeAt, "retry_at": 0,
		}).Execute(); err != nil {
			return err
		}
		authorization.Page.Set("deletion_job_id", pageDeletionFence(pageID))
		if err := txApp.Save(authorization.Page); err != nil {
			return err
		}
		return auditPageDeletionHold(txApp, hold.WorkspaceID, actorID, pageDeletionAuditHeld, pageID)
	})
	return hold, duplicate, err
}

// auditPageDeletionHold records what happened to a page in its 30 days on the
// organization's audit log.
func auditPageDeletionHold(app core.App, workspaceID, actorID, action, pageID string) error {
	organizationID, err := loadLegalHoldWorkspaceOrganization(app, workspaceID)
	if err != nil {
		return err
	}
	return recordOrganizationAudit(app, organizationID, actorID, action, "page", pageID)
}

// pagePermanentDeleteResponseForHold answers the deletion's request as done:
// to the person who asked, and to everyone else, the page has gone.
func pagePermanentDeleteResponseForHold(hold storedPageDeletionHold, duplicate bool) pagePermanentDeleteResponse {
	return pagePermanentDeleteResponse{
		JobID: pageDeletionHoldFencePrefix + hold.PageID, RootPageID: hold.PageID,
		Status: pagePermanentDeleteStatusCompleted, Phase: pagePermanentDeletePhaseCompleted,
		Progress: pagePermanentDeleteProgress{Revision: 1, Discovered: 1, Authorized: 1, Cleaned: 1},
		Counts:   pagePermanentDeleteCounts{Pages: 1}, Duplicate: duplicate,
	}
}

// endPageDeletionHold lifts a page's fence, or the fence of the rows trashed
// with a row, and forgets its 30 days.
func endPageDeletionHold(app core.App, workspaceID, pageID string) error {
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_deletion_holds}} WHERE [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[deletion_job_id]] = ''
		WHERE [[workspace]] = {:workspaceId} AND ` + partialIndexKey(`[[deletion_job_id]]`, `={:fence}`) + `
	`).Bind(dbx.Params{"workspaceId": workspaceID, "fence": pageDeletionFence(pageID)}).Execute()
	return err
}

// heldRowTrashEvent says whether a hold keeps a trashed row and the rows that
// went with it, not a page.
func heldRowTrashEvent(app core.App, pageID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_row_trash}}
		INDEXED BY [[idx_leafen_database_row_trash_root]] WHERE [[root_row_id]] = {:rootId} AND [[deleting]] = 0
	`).Bind(dbx.Params{"rootId": pageID}).Row(&count)
	return count > 0, err
}

// holdDatabaseRowTrashEvent keeps a row deleted for good, or whose 30 days in
// the trash are up, for its 30 days, with the rows trashed with it: a row is
// a page too. Its fence takes it out of the row trash and every read of its page.
func holdDatabaseRowTrashEvent(app core.App, rootRowID, actorID, workspaceID, requestID string) error {
	rowIDs := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]] FROM {{leafen_database_row_trash}}
		INDEXED BY [[idx_leafen_database_row_trash_root]] WHERE [[root_row_id]] = {:rootId} AND [[deleting]] = 0
		ORDER BY [[row_id]]
	`).Bind(dbx.Params{"rootId": rootRowID}).Column(&rowIDs); err != nil {
		return err
	}
	if len(rowIDs) == 0 {
		return nil
	}
	organizationID, err := loadLegalHoldWorkspaceOrganization(app, workspaceID)
	if err != nil {
		return err
	}
	if err := assertNoActiveLegalHoldForPermanentDelete(app, organizationID, legalHoldDeletionTarget{
		WorkspaceID: workspaceID, PageIDs: rowIDs,
	}); err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := app.DB().Insert("leafen_page_deletion_holds", dbx.Params{
		"page_id": rootRowID, "workspace_id": workspaceID, "actor_id": actorID, "request_id": requestID,
		"held_at": now.UnixMilli(), "purge_at": now.Add(pageDeletionHoldPeriod).UnixMilli(), "retry_at": 0,
	}).Execute(); err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[deletion_job_id]] = {:fence}
		WHERE [[workspace]] = {:workspaceId} AND [[id]] IN (
			SELECT [[row_id]] FROM {{leafen_database_row_trash}} WHERE [[root_row_id]] = {:rootId} AND [[deleting]] = 0)
	`).Bind(dbx.Params{"fence": pageDeletionFence(rootRowID), "workspaceId": workspaceID, "rootId": rootRowID}).Execute()
	if err != nil {
		return err
	}
	return auditPageDeletionHold(app, workspaceID, actorID, pageDeletionAuditHeld, rootRowID)
}

// endDeletionHoldAndDelete ends a page's or a row's 30 days and deletes it.
func endDeletionHoldAndDelete(txApp core.App, workspaceID, pageID string, subject pageAccessSubject, requestID string) (storedPagePermanentDeleteJob, error) {
	row, err := heldRowTrashEvent(txApp, pageID)
	if err != nil {
		return storedPagePermanentDeleteJob{}, err
	}
	if err := endPageDeletionHold(txApp, workspaceID, pageID); err != nil {
		return storedPagePermanentDeleteJob{}, err
	}
	if row {
		_, err := deleteDatabaseRowTrashEventBy(txApp, pageID, subject)
		return storedPagePermanentDeleteJob{}, err
	}
	admission, err := admitPagePermanentDelete(txApp, subject, pageID, requestID)
	return admission.Job, err
}

// releaseDuePageDeletionHold starts the deletion of one page whose 30 days
// are up; the trash retention runner goes on with it. A hold found only as it
// starts asks again a day later.
func releaseDuePageDeletionHold(app core.App, now int64) (bool, error) {
	due := struct {
		PageID      string `db:"page_id"`
		WorkspaceID string `db:"workspace_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[kept]].[[page_id]], [[kept]].[[workspace_id]] FROM {{leafen_page_deletion_holds}} AS [[kept]]
		WHERE [[kept]].[[purge_at]] <= {:now} AND [[kept]].[[retry_at]] <= {:now}
			AND NOT ` + legalHoldRetainsPageSQL("[[kept]].[[page_id]]", "[[kept]].[[workspace_id]]") + `
		ORDER BY [[kept]].[[purge_at]], [[kept]].[[page_id]] LIMIT 1
	`).Bind(dbx.Params{"now": now}).One(&due)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		workspace, err := txApp.FindRecordById(workspacesCollection, due.WorkspaceID)
		if err != nil {
			return err
		}
		_, err = endDeletionHoldAndDelete(
			txApp, due.WorkspaceID, due.PageID, systemJobSubjectFor(workspace),
			pageTrashRetentionRequestPrefix+due.PageID+":kept:"+strconv.FormatInt(now, 10),
		)
		return err
	})
	_, pageErr := app.FindRecordById(pagesCollection, due.PageID)
	if errors.Is(err, sql.ErrNoRows) || isPermissionDenied(err) && errors.Is(pageErr, sql.ErrNoRows) {
		// Its page, database or workspace went first: nothing is left to keep.
		_, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_page_deletion_holds}} WHERE [[page_id]] = {:pageId}
		`).Bind(dbx.Params{"pageId": due.PageID}).Execute()
		return true, err
	}
	if err != nil {
		if _, retryErr := app.DB().NewQuery(`
			UPDATE {{leafen_page_deletion_holds}} SET [[retry_at]] = {:retry} WHERE [[page_id]] = {:pageId}
		`).Bind(dbx.Params{"retry": now + pageTrashRetentionHoldRetry.Milliseconds(), "pageId": due.PageID}).Execute(); retryErr != nil {
			return false, retryErr
		}
		app.Logger().Warn("leafen page deletion hold couldn't end", "page", due.PageID, "error", err)
	}
	return true, nil
}

func registerPageDeletionHoldRoutes(event *core.ServeEvent, rooms workspaceRoomInvalidator) {
	event.Router.GET(apiRoot+pageDeletedPagesPath, readDeletedPages)
	event.Router.POST(apiRoot+pageDeletedPageRecoverPath, recoverDeletedPage(rooms)).
		Bind(apis.BodyLimit(pagePermanentDeleteMaxBodyBytes))
	event.Router.POST(apiRoot+pageDeletedPagePurgePath, purgeDeletedPage(rooms)).
		Bind(apis.BodyLimit(pagePermanentDeleteMaxBodyBytes))
}

// recoverDeletedPage puts a page back in the trash, for its own 30 days there;
// the page's restore takes it from there.
func recoverDeletedPage(rooms workspaceRoomInvalidator) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
		pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
		err := event.App.RunInTransaction(func(txApp core.App) error {
			if _, err := deletedPageRequest(txApp, user.Id, workspaceID, pageID); err != nil {
				return err
			}
			if err := auditPageDeletionHold(txApp, workspaceID, user.Id, pageDeletionAuditRecovered, pageID); err != nil {
				return err
			}
			row, err := heldRowTrashEvent(txApp, pageID)
			if err != nil {
				return err
			}
			if err := endPageDeletionHold(txApp, workspaceID, pageID); err != nil {
				return err
			}
			if row {
				_, err := txApp.DB().NewQuery(`
					UPDATE {{leafen_database_row_trash}} SET [[expires_at]] = {:expires} WHERE [[root_row_id]] = {:rootId}
				`).Bind(dbx.Params{
					"expires": time.Now().UTC().Add(recoverableBlockTrashRetention).UnixMilli(), "rootId": pageID,
				}).Execute()
				return err
			}
			page, err := txApp.FindRecordById(pagesCollection, pageID)
			if err != nil {
				return err
			}
			applyPageTrashState(page, true, time.Now().UTC().Format(time.RFC3339Nano))
			return txApp.Save(page)
		})
		if err != nil {
			return pagePermanentDeleteError(event, err, "deleted-page-recover")
		}
		if err := invalidatePermanentPageRooms(rooms, workspaceID); err != nil {
			return pagePermanentDeleteError(event, errors.Join(errPagePermanentDeleteUnavailable, err), "deleted-page-recover-invalidate")
		}
		// A row's restore asks for its authority revision.
		response := pageDeletedPageRecoverResponse{}
		if authority, err := loadDatabaseRowPageAuthority(event.App, pageID); err == nil {
			response.AuthorityRevision = authority.AuthorityRevision
		}
		return event.JSON(http.StatusOK, response)
	}
}

// purgeDeletedPage deletes a page at once, before its 30 days are up, as a
// person's request to have their data gone asks. An active legal hold refuses
// it, as any permanent deletion. The deletion runner finishes what one step
// here leaves.
func purgeDeletedPage(rooms workspaceRoomInvalidator) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
		pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
		input := pagePermanentDeleteInput{}
		if err := event.BindBody(&input); err != nil {
			return pagePermanentDeleteError(event, errPagePermanentDeleteInvalid, "deleted-page-purge-bind")
		}
		input.RequestID = strings.TrimSpace(input.RequestID)
		if input.RequestID == "" || len([]rune(input.RequestID)) > maxPagePermanentDeleteRequestIDRunes {
			return pagePermanentDeleteError(event, errPagePermanentDeleteInvalid, "deleted-page-purge-validate")
		}
		job := storedPagePermanentDeleteJob{}
		purge := func(txApp core.App) error {
			if _, err := deletedPageRequest(txApp, user.Id, workspaceID, pageID); err != nil {
				return err
			}
			if err := auditPageDeletionHold(txApp, workspaceID, user.Id, pageDeletionAuditPurged, pageID); err != nil {
				return err
			}
			var err error
			job, err = endDeletionHoldAndDelete(txApp, workspaceID, pageID, personAccessSubject(user.Id), input.RequestID)
			return err
		}
		// A key's call asks its person only for a purge that would happen
		// (user_key_acting_ask.go).
		if err := rehearseBeforeAsking(event, purge); err != nil {
			return pagePermanentDeleteError(event, err, "deleted-page-purge")
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
		job = storedPagePermanentDeleteJob{}
		err := event.App.RunInTransaction(purge)
		if err != nil {
			return pagePermanentDeleteError(event, err, "deleted-page-purge")
		}
		if err := invalidatePermanentPageRooms(rooms, workspaceID); err != nil {
			return pagePermanentDeleteError(event, errors.Join(errPagePermanentDeleteUnavailable, err), "deleted-page-purge-invalidate")
		}
		if job.ID != "" {
			if _, err := advancePagePermanentDelete(event.App, job.ID); err != nil {
				return pagePermanentDeleteError(event, err, "deleted-page-purge-advance")
			}
		}
		return event.NoContent(http.StatusNoContent)
	}
}
