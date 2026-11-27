package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database row's page changes by its page id as any page does (2026-11-22,
// a public issue: an agent locking a row's page took five calls — the lock
// read, the row's whole snapshot for its revision, the row's own lock route
// and a read again — as the page's path refused the lock and named the
// row's). PATCH /pages/{id}/metadata on a row's page goes on, inside, to its
// row's own route for a lock, an archive, the trash, a restore or a move,
// with the row's revision, expectedAuthorityRevision, which GET /pages/{id}
// and GET /pages/{id}/lock read beside the page (authorityRevision, and the
// row's own state, pageResponseAsItsRow): the
// revision stays the check that no other change came between, as on the
// row's route, whose refusals it passes on. Its icon, cover and style went on
// there before (page_metadata_row_page.go), its favorite is the page's
// (PUT /pages/{pageId}/favorite), and the row's routes stay as they are.

// rowPageChange is a change a row's own route makes: run is its handler, and
// body what it takes beside the row's revision.
type rowPageChange struct {
	run  func(*core.RequestEvent) error
	body map[string]any
}

const rowPageRevisionMessage = "A database row's page changes with its row's revision: send expectedAuthorityRevision as " +
	"GET /pages/{id} or GET /pages/{id}/lock reads it (authorityRevision)."

const rowPageOneChangeMessage = "Change a database row's page's lock, archive, trash or place one at a time, " +
	"and its icon, cover and style in a change of their own."

const rowPageRevisionConflictMessage = "The database row changed since its revision was read: read authorityRevision again " +
	"with GET /pages/{id} and send the change with it."

// changeRowPageOnItsRoutes makes a metadata change that sends the row's
// revision on its row's routes; changed is false where it asks nothing of
// them, which the look's path answers.
func changeRowPageOnItsRoutes(
	event *core.RequestEvent, principalID string, page *core.Record, input pageMetadataInput, sent pageMetadataSentFields,
	rooms workspaceRoomInvalidator, ygo *pocketBaseYgoPersistence,
) (changed bool, err error) {
	// A title, a place among pages or a display has no row route by the page's id.
	if sent.Title != nil && input.Title != page.GetString("title") || input.Position != nil ||
		input.BacklinksDisplay != nil || input.PageCommentsDisplay != nil || input.InlineCommentsDisplay != nil {
		return true, apiError(event, http.StatusBadRequest, errorCodeInvalidInput, rowPageMetadataRefusal)
	}
	authority, err := authorizeDatabaseRowOrTemplatePage(event.App, principalID, page.Id, permissionWorkspaceRead)
	if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
		return true, answerPageMetadataMissing(event, principalID, page.Id)
	}
	if err != nil {
		return true, apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't save the page details.", err)
	}
	if sent.ParentID != nil {
		if crossing := rowPageParentCrossing(event.App, principalID, authority, input.ParentID); crossing != nil {
			return true, apiErrorFrom(event, http.StatusBadRequest, errorCodeInvalidInput, "Check parentId.", crossing)
		}
	}
	changes, refusal := rowPageChangesAsked(event.App, page, authority, input, sent, rooms, ygo)
	looks := input.Icon != nil || input.IconType != nil || input.Cover != nil || input.CoverPosition != nil || input.Font != nil ||
		input.SmallText != nil || input.FullWidth != nil || input.TableOfContentsDisplay != nil
	switch {
	case refusal != "":
		return true, apiError(event, http.StatusBadRequest, errorCodeInvalidInput, refusal)
	case len(changes) == 0:
		return false, nil
	case len(changes) > 1 || looks:
		return true, apiError(event, http.StatusBadRequest, errorCodeInvalidInput, rowPageOneChangeMessage)
	}
	return true, runRowPageChange(event, principalID, page.Id, input, changes[0])
}

// pageResponseAsItsRow is a database row's page as its row has it: in the
// trash when its row is — a row's trash is kept with the row
// (database_row_trash.go), which its page record doesn't say, so a page read
// said a trashed row's page wasn't in the trash (2026-11-22) — and with its
// row's revision, which a change of it sends. Its lock and archive are its
// page record's.
func pageResponseAsItsRow(app core.App, response pageResponse) pageResponse {
	if !response.DatabaseRow {
		return response
	}
	authority, err := loadDatabaseRowPageAuthority(app, response.ID)
	if err != nil {
		return response
	}
	response.AuthorityRevision = authority.AuthorityRevision
	if authority.TrashedAt != 0 && !response.InTrash {
		trashedAt := time.UnixMilli(authority.TrashedAt).UTC().Format(time.RFC3339Nano)
		response.InTrash, response.TrashedAt = true, &trashedAt
	}
	return response
}

// rowPageAuthorityRevision is a row's page's revision as its reader reads it,
// 0 where the page is no row's or the reader can't open it.
func rowPageAuthorityRevision(app core.App, principalID, pageID string) int {
	authority, err := authorizeDatabaseRowOrTemplatePage(app, principalID, pageID, permissionWorkspaceRead)
	if err != nil {
		return 0
	}
	return authority.AuthorityRevision
}

// rowPageChangesAsked are the changes a metadata change asks of a row's own
// routes, each differing from the row as it is; refusal names a change
// asked that has none, a wiki's row's place among its pages.
func rowPageChangesAsked(
	app core.App, page *core.Record, authority databaseRowPageAuthority, input pageMetadataInput, sent pageMetadataSentFields,
	rooms workspaceRoomInvalidator, ygo *pocketBaseYgoPersistence,
) (changes []rowPageChange, refusal string) {
	if input.IsLocked != nil && *input.IsLocked != authority.Locked {
		changes = append(changes, rowPageChange{
			run:  func(event *core.RequestEvent) error { return setDatabaseRowPageLock(event, rooms) },
			body: map[string]any{"locked": *input.IsLocked},
		})
	}
	if input.IsArchived != nil && *input.IsArchived != (authority.ArchivedAt != "") {
		changes = append(changes, rowPageChange{run: setDatabaseRowPageArchive, body: map[string]any{"archived": *input.IsArchived}})
	}
	// Out of the trash and under a page, or to the top of a place, it is one
	// move that takes it out of the trash as it goes, as a cut row's paste
	// (page_row_conversion_out_of_trash.go); into a database, one at a time.
	restoring := sent.InTrash != nil && !input.InTrash && authority.TrashedAt != 0
	if sent.InTrash != nil && input.InTrash != (authority.TrashedAt != 0) && ygo != nil {
		run := ygo.restoreDatabaseRowPage
		if input.InTrash {
			run = ygo.trashDatabaseRowPage
		}
		changes = append(changes, rowPageChange{run: run, body: map[string]any{"requestId": input.MutationID}})
	}
	// Its parent as a read gives it: its data source; named as an agent's
	// tools name one too, without dashes or as collection://.
	parentID := notionDashedUUID(strings.TrimPrefix(strings.TrimSpace(input.ParentID), mcpDataSourceURLPrefix))
	if (sent.ParentID != nil && parentID != page.GetString("parent_id") || input.TeamspaceID != nil) && ygo != nil {
		var wiki bool
		_ = app.DB().NewQuery(`SELECT [[wiki]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": authority.DataSourceID}).Row(&wiki)
		if wiki {
			return nil, rowPageMetadataRefusal
		}
		body := map[string]any{"requestId": input.MutationID}
		switch sources := rowPageMoveSources(app, authority, parentID); {
		case len(sources) == 1 && sources[0] == authority.DataSourceID && input.TeamspaceID == nil:
			// Its own database, named by its data source, its full page or its container.
			return changes, ""
		case parentID == "":
			body["targetTopLevel"], body["leaveDatabase"] = true, input.LeaveDatabase
			if input.TeamspaceID != nil {
				body["targetTeamspaceId"] = *input.TeamspaceID
			}
		case len(sources) == 1:
			body["targetDataSourceId"] = sources[0]
		case len(sources) > 1:
			return nil, "That database has more than one data source: move the row with POST /database-rows/{rowId}/page/move and targetDataSourceId."
		default:
			// The page it goes under decides its teamspace; it leaves its
			// database as its route has it, when the change says so.
			body["targetPageId"], body["leaveDatabase"] = parentID, input.LeaveDatabase
		}
		if _, out := body["leaveDatabase"]; out && restoring {
			body["fromTrash"], changes = true, changes[:len(changes)-1]
		}
		changes = append(changes, rowPageChange{run: ygo.moveDatabaseRowPage, body: body})
	}
	return changes, ""
}

// rowPageParentCrossing refuses a row's page's new parent in another
// workspace — a page there, or a database there by its data source — as such
// (other_workspace_refusal.go), before its row's route reads it as a parent
// it can't find.
func rowPageParentCrossing(app core.App, principalID string, authority databaseRowPageAuthority, parentID string) error {
	parentID = notionDashedUUID(strings.TrimPrefix(strings.TrimSpace(parentID), mcpDataSourceURLPrefix))
	if parentID == "" {
		return nil
	}
	to := pageInOtherWorkspace(app, principalID, parentID, authority.WorkspaceID)
	if to == "" {
		to, _ = dataSourceInOtherWorkspace(app, principalID, databaseRowPageMoveSource(app, parentID), authority.WorkspaceID)
	}
	if to == "" {
		return nil
	}
	return otherWorkspaceRefused(app, "parentId", workspaceCrossing{
		subject: "this row", from: authority.WorkspaceID, place: "its new parent " + parentID, to: to, whole: authority.ContainerPageID,
	})
}

// rowPageMoveSources are the data sources a parent named for a row's page
// is: itself, or those of the database it names by its full page or its
// container (the address an agent's tools give a database). A page holding
// databases in its body is a page the row goes under, not its databases
// (2026-11-27: a row's page given the page holding its database answered 200
// and stayed, and one given a page holding two was said to name a database
// of two data sources).
func rowPageMoveSources(app core.App, authority databaseRowPageAuthority, parentID string) []string {
	sources := []string{}
	if parentID == "" {
		return sources
	}
	// A data source's public API ID, as the MCP tools give it.
	if identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDataSource, parentID); err == nil {
		parentID = identity.InternalID
	}
	_ = app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_data_sources}}
		WHERE ([[id]] = {:parent}
			OR [[page_id]] = {:parent} AND EXISTS (SELECT 1 FROM {{leafen_pages}} WHERE [[id]] = {:parent} AND [[kind]] = {:database})
			OR [[id]] IN (SELECT [[data_source_id]] FROM {{leafen_database_container_sources}} WHERE [[container_id]] = {:parent}))
			AND [[owner]] = {:owner} AND [[workspace_id]] = {:workspace}
		ORDER BY [[id]]
	`).Bind(dbx.Params{"parent": parentID, "owner": authority.OwnerID, "workspace": authority.WorkspaceID, "database": pageKindDatabase}).Column(&sources)
	return sources
}

// rowRouteAnswer keeps what a row's route answered.
type rowRouteAnswer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (answer *rowRouteAnswer) Header() http.Header { return answer.header }

func (answer *rowRouteAnswer) WriteHeader(status int) {
	if answer.status == 0 {
		answer.status = status
	}
}

func (answer *rowRouteAnswer) Write(data []byte) (int, error) {
	answer.WriteHeader(http.StatusOK)
	return answer.body.Write(data)
}

// revisionConflict is a refusal for a revision that changed meanwhile, said
// with where to read it again.
func (answer *rowRouteAnswer) revisionConflict() (map[string]any, bool) {
	refused := map[string]map[string]any{}
	if json.Unmarshal(answer.body.Bytes(), &refused) != nil || refused["error"]["code"] != errorCodeDatabaseRowPageConflict {
		return nil, false
	}
	refused["error"]["message"] = rowPageRevisionConflictMessage
	return map[string]any{"error": refused["error"]}, true
}

// runRowPageChange runs the change on its row's route, as if it were asked
// there with the row's id and revision, and answers as a page metadata change
// does: with the page as it is after it and the row's revision; the route's
// refusal is passed on, a revision that changed meanwhile said with where to
// read it.
func runRowPageChange(event *core.RequestEvent, principalID, pageID string, input pageMetadataInput, change rowPageChange) error {
	change.body["expectedAuthorityRevision"] = input.ExpectedAuthorityRevision
	raw, err := json.Marshal(change.body)
	if err != nil {
		return err
	}
	request := event.Request.Clone(event.Request.Context())
	request.Body, request.ContentLength = io.NopCloser(bytes.NewReader(raw)), int64(len(raw))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("rowId", pageID)
	response, original := event.Response, event.Request
	answer := &rowRouteAnswer{header: http.Header{}}
	event.Response, event.Request = answer, request
	err = change.run(event)
	event.Response, event.Request = response, original
	if answer.status == 0 {
		return err
	}
	if answer.status >= http.StatusBadRequest {
		if conflict, ok := answer.revisionConflict(); ok {
			return event.JSON(answer.status, conflict)
		}
		for name, values := range answer.header {
			event.Response.Header()[name] = values
		}
		event.Response.WriteHeader(answer.status)
		_, err := event.Response.Write(answer.body.Bytes())
		return err
	}
	updated, err := event.App.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't read the page after the change.", err)
	}
	page, err := pageRecordResponseWithWiki(event.App, updated)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't read the page after the change.", err)
	}
	pageResponseAsRead(event.App, principalID, &page)
	return event.JSON(http.StatusOK, pageMetadataResponse{
		Page:    page,
		Receipt: pageMetadataReceipt{MutationID: input.MutationID, Cursor: updated.GetInt("cursor")},
	})
}
