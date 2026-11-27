package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// moveDatabaseRowPageOut moves a database page out of its database: under a
// page, or to the top of the place chosen — a teamspace, or the mover's
// private pages — or, none named, of the place its database is in. Its
// values wait for it, so a preview names none lost. It leaves its database
// only when the request says so (leaveDatabase), as the Move to menu's
// choice of a page does, and the answer says it did (databaseRowLeftWarning):
// Notion's API makes a database page moved under a page a page of its own
// without a word and drops its values (measured 2026-11-16 through its move
// endpoint), and an agent that moved one so lost it from its database
// unawares (2026-11-16).
func (p *pocketBaseYgoPersistence) moveDatabaseRowPageOut(
	event *core.RequestEvent, userID, rowID string, input databaseRowPageActionInput, preliminary databaseRowPageAuthority,
) error {
	if input.Preview {
		if preliminary.AuthorityRevision != input.ExpectedAuthorityRevision {
			return writeDatabaseRowPageActionError(event, errDatabaseRowPageConflict, "to-page-preview")
		}
		// A parent it can't go under is refused, with its reason, first.
		request := databaseRowToPageRequest{
			PrincipalID: userID, RowID: rowID, ParentID: input.TargetPageID, Place: databaseRowTopPlace(userID, input), OutOfTrash: input.FromTrash,
		}
		if err := errors.Join(rowToPageTrashRefused(request, preliminary), checkDatabaseRowToPageParent(event.App, request, preliminary)); err != nil {
			return writeDatabaseRowPageActionError(event, err, "to-page-preview")
		}
		// Its values wait for it as a page (database_row_kept_values.go): none is lost.
		return event.JSON(http.StatusOK, databaseRowPageActionResponse{
			RowID: rowID, ContainerPageID: preliminary.ContainerPageID,
			AuthorityRevision: preliminary.AuthorityRevision, DroppedProperties: []databaseRowPageMoveDroppedProperty{},
			Warning: databaseRowLeftWarning(rowID, true),
		})
	}
	if !input.LeaveDatabase {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the move details: "+
			"a move under a page or to the top of a place takes this database page out of its database, as a page of its own; "+
			"send leaveDatabase true to do it, or targetDataSourceId to move it to another database.")
	}
	// Sent as the row is held (asHeld, a move kept and sent as its page went), it isn't held to a revision.
	if !input.AsHeld && preliminary.AuthorityRevision != input.ExpectedAuthorityRevision {
		return writeDatabaseRowPageActionError(event, errDatabaseRowPageConflict, "to-page")
	}
	receipt, err := p.convertDatabaseRowToPage(event.Request.Context(), databaseRowToPageRequest{
		PrincipalID: userID, RowID: rowID, ContainerPageID: preliminary.ContainerPageID, ParentID: input.TargetPageID,
		Place: databaseRowTopPlace(userID, input), OutOfTrash: input.FromTrash,
		MutationID:  databaseRowPageActionIdentity("to-page", userID, rowID, input.RequestID),
		RequestHash: databaseRowPageActionRequestHash("to-page", rowID, input),
	})
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "to-page")
	}
	return event.JSON(http.StatusOK, databaseRowPageActionResponse{
		RowID: rowID, ContainerPageID: input.TargetPageID, Duplicate: receipt.Duplicate,
		Warning: databaseRowLeftWarning(rowID, false),
	})
}

// databaseRowLeftWarning says that a database page moved out left its
// database: it is read as a page now, by the same ID, and its values wait for
// it should it go back (database_row_kept_values.go).
func databaseRowLeftWarning(pageID string, preview bool) string {
	done := "This page left its database and is a page of its own now"
	if preview {
		done = "Moved so, this page would leave its database and be a page of its own"
	}
	return done + ": read it at /pages/" + pageID + "; its values are kept should it go back into a database (POST /pages/" +
		pageID + "/database-row)."
}

// databaseRowTopPlace is the place at whose top a database page moved out
// as a page goes: the teamspace named, or the mover's private pages when it
// names none; nil keeps the place its database is in.
func databaseRowTopPlace(userID string, input databaseRowPageActionInput) *pagePlace {
	if !input.TargetTopLevel || input.TargetTeamspaceID == nil {
		return nil
	}
	if teamspaceID := strings.TrimSpace(*input.TargetTeamspaceID); teamspaceID != "" {
		return &pagePlace{TeamspaceID: teamspaceID}
	}
	return &pagePlace{PrivateOwnerID: userID}
}

// pageIntoDatabaseRequestWords say what a move of a page into a database sends.
const pageIntoDatabaseRequestWords = "Check the move details: send requestId (1 to 1,024 characters) and dataSourceId, " +
	"the data source of the database the page goes into; first puts it before its first row, or position at a place among them, not both."

// moveDatabaseRowPageIn moves a page into a database as its row.
func (p *pocketBaseYgoPersistence) moveDatabaseRowPageIn(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := databaseRowPageFromPageInput{}
	if pageID == "" || event.BindBody(&input) != nil {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, pageIntoDatabaseRequestWords)
	}
	input.RequestID, input.DataSourceID = strings.TrimSpace(input.RequestID), strings.TrimSpace(input.DataSourceID)
	if input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > 1024 || input.DataSourceID == "" ||
		len(input.Position) > maxGraphPositionLength || input.Position != "" && input.First {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, pageIntoDatabaseRequestWords)
	}
	var containerPageID string
	err := event.App.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": input.DataSourceID}).Row(&containerPageID)
	if errors.Is(err, sql.ErrNoRows) {
		return writeDatabaseRowPageActionError(event, errDatabaseRowPageNotFound, "from-page-target")
	}
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "from-page-target")
	}
	request := "from-page\x00" + pageID + "\x00" + input.DataSourceID
	if input.First {
		request += "\x00first"
	} else if input.Position != "" {
		request += "\x00position\x00" + input.Position
	}
	input.CheckedPropertyID = strings.TrimSpace(input.CheckedPropertyID)
	if input.Checked != nil {
		request += "\x00checked:" + strconv.FormatBool(*input.Checked) + "\x00" + input.CheckedPropertyID
	}
	digest := sha256.Sum256([]byte(request))
	receipt, err := p.movePageIntoDatabase(event.Request.Context(), pageToDatabaseRowRequest{
		PrincipalID: user.Id, PageID: pageID, DataSourceID: input.DataSourceID, ContainerPageID: containerPageID,
		MutationID:  databaseRowPageActionIdentity("from-page", user.Id, pageID, input.RequestID),
		RequestHash: hex.EncodeToString(digest[:]), First: input.First, Position: input.Position,
		Checked: input.Checked, CheckedPropertyID: input.CheckedPropertyID,
	})
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "from-page")
	}
	authority, err := authorizeDatabaseRowPage(event.App, user.Id, pageID, permissionWorkspaceRead)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "from-page-response")
	}
	return event.JSON(http.StatusOK, databaseRowPageActionResponse{
		RowID: pageID, ContainerPageID: authority.ContainerPageID,
		AuthorityRevision: authority.AuthorityRevision, Duplicate: receipt.Duplicate,
	})
}
