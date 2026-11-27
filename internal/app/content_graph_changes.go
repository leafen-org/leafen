package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type storedStructuralGraphChange struct {
	Cursor     int    `db:"cursor"`
	MutationID string `db:"mutation_id"`
	Update     []byte `db:"ygo_update"`
}

func insertStructuralGraphChange(
	app core.App,
	owner, pageID, mutationID, requestHash string,
	cursor int,
	update []byte,
	created int64,
) error {
	return insertStructuralGraphChangeContext(
		context.Background(), app, owner, pageID, mutationID, requestHash,
		cursor, update, created,
	)
}

func insertStructuralGraphChangeContext(
	ctx context.Context,
	app core.App,
	owner, pageID, mutationID, requestHash string,
	cursor int,
	update []byte,
	created int64,
) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_graph_changes}}
			([[owner]], [[page_id]], [[cursor]], [[mutation_id]], [[request_hash]], [[ygo_update]], [[created]])
		VALUES
			({:owner}, {:pageId}, {:cursor}, {:mutationId}, {:requestHash}, {:update}, {:created})
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "cursor": cursor,
		"mutationId": mutationID, "requestHash": requestHash,
		"update": update, "created": created,
	}).WithContext(ctx).Execute()
	if err == nil {
		announceGraphCursorAfterCommit(app, pageID, cursor)
		announceGraphReadersAfterCommit(app, pageID)
		noteGraphChangeWritten(owner, pageID, cursor)
	}
	return err
}

// graphChangeStateVector is the state vector a page's change is kept with:
// the clocks the change itself reaches, which the page's document holds once
// it holds the change. The whole document's state vector, kept with every
// change, grew with each writer the page ever had (2026-10-16).
func graphChangeStateVector(update []byte) (string, error) {
	vector, err := crdt.EncodeStateVectorFromUpdate(update)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(vector), nil
}

// recordServerGraphChange records a change the server made to a page's
// blocks outside a reader's request, so the page's readers take it up as
// they take up one another's changes.
func recordServerGraphChange(
	app core.App,
	ownerID, pageID, mutationID string,
	update []byte,
	countsValue graphMutationCounts,
) error {
	return recordServerGraphChangeReceipt(app, ownerID, pageID, mutationID, update, countsValue)
}

// recordServerGraphRowsChange records a change the server made to some of
// a page's database rows alone — what a view draws of their own pages —
// saying which, so a page behind it reads those rows again, not its whole
// graph (content_graph_changed_rows.go).
func recordServerGraphRowsChange(app core.App, ownerID, pageID, mutationID string, rowIDs []string) error {
	return recordServerGraphChangeReceipt(app, ownerID, pageID, mutationID,
		crdt.EncodeStateAsUpdateV1(crdt.New(), nil),
		storedGraphMutationReceipt{graphChangedParts: graphChangedParts{OnlyRows: rowIDs}})
}

// recordServerGraphChangeReceipt records a server's change with the receipt
// it is kept with: its counts, and what alone it changed when it says.
func recordServerGraphChangeReceipt(
	app core.App,
	ownerID, pageID, mutationID string,
	update []byte,
	receipt any,
) error {
	stateVector, err := graphChangeStateVector(update)
	if err != nil {
		return err
	}
	var cursor int
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]),0)+1 FROM {{leafen_graph_mutations}}
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{"ownerId": ownerID, "pageId": pageID}).Row(&cursor); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(mutationID))
	requestHash := hex.EncodeToString(digest[:])
	counts, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	created := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_graph_mutations}}
			([[owner]],[[page_id]],[[mutation_id]],[[request_hash]],[[cursor]],
			 [[counts_json]],[[state_vector]],[[created]])
		VALUES ({:ownerId},{:pageId},{:mutationId},{:requestHash},{:cursor},
			{:counts},{:stateVector},{:created})
	`).Bind(dbx.Params{
		"ownerId": ownerID, "pageId": pageID, "mutationId": mutationID,
		"requestHash": requestHash, "cursor": cursor, "counts": string(counts),
		"stateVector": stateVector, "created": created,
	}).Execute(); err != nil {
		return err
	}
	return insertStructuralGraphChange(
		app, ownerID, pageID, mutationID, requestHash, cursor, update, created,
	)
}

func loadStructuralGraphChanges(
	app core.App,
	owner, pageID string,
	after, limit int,
) (graphChangeResponse, error) {
	var latest int
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]), 0) FROM {{leafen_graph_changes}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).Row(&latest); err != nil {
		return graphChangeResponse{}, err
	}
	if after > latest {
		return graphChangeResponse{}, graphInputError(
			errorCodeGraphChangeInvalid,
			"Sync the content change position again.",
		)
	}
	stored := []storedStructuralGraphChange{}
	err := app.DB().NewQuery(`
		SELECT [[cursor]], [[mutation_id]], [[ygo_update]]
		FROM {{leafen_graph_changes}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[cursor]] > {:after}
		ORDER BY [[cursor]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "after": after, "limit": limit + 1,
	}).All(&stored)
	if err != nil {
		return graphChangeResponse{}, err
	}

	// A window fills to about 1 MB, and always carries its first change
	// however large (up to one update's most, maxYgoUpdateBytes), so no
	// change stops the ones after it.
	response := graphChangeResponse{PageID: pageID, Changes: []graphChange{}}
	totalBytes := 0
	for _, change := range stored {
		if len(response.Changes) == limit ||
			len(response.Changes) > 0 && totalBytes+len(change.Update) > maxGraphChangeBytes {
			response.HasMore = true
			break
		}
		totalBytes += len(change.Update)
		response.Changes = append(response.Changes, graphChange{
			Cursor: change.Cursor, MutationID: change.MutationID,
			Update: base64.StdEncoding.EncodeToString(change.Update),
		})
	}
	if response.HasMore && len(response.Changes) > 0 {
		next := response.Changes[len(response.Changes)-1].Cursor
		response.NextCursor = &next
	}
	return response, nil
}

func (p *pocketBaseYgoPersistence) readContentGraphChanges(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	after := 0
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("after")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return apiError(event, http.StatusBadRequest, errorCodeGraphChangeInvalid, "Check the content change position.")
		}
		after = parsed
	}
	limit := maxGraphChangeItems
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxGraphChangeItems {
			return apiError(event, http.StatusBadRequest, errorCodeGraphChangeInvalid, "Check the content change range.")
		}
		limit = parsed
	}

	response := graphChangeResponse{}
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		authorization, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead)
		if isPermissionDenied(err) {
			return graphNotFoundError()
		}
		if err != nil {
			return err
		}
		response, err = loadStructuralGraphChanges(txApp, authorization.Workspace.OwnerID, pageID, after, limit)
		return err
	})
	if err != nil {
		return writeGraphError(event, err, "change-catchup", errorCodeGraphChangeFailed, "Couldn't load the content changes.")
	}
	return event.JSON(http.StatusOK, response)
}
