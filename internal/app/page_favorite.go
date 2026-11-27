package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errPageFavoriteConflict       = errors.New(errorCodePageFavoriteConflict)
	errPageFavoriteIdentityReused = errors.New(errorCodePageFavoriteIdentityReused)
)

const pageFavoriteStripes = 64

var pageFavoriteMutationStripes [pageFavoriteStripes]sync.Mutex

type storedPageFavoriteState struct {
	Favorite        bool   `db:"favorite"`
	Revision        int    `db:"revision"`
	LastRequestID   string `db:"last_request_id"`
	LastRequestHash string `db:"last_request_hash"`
}

func registerPageFavoriteRoutes(event *core.ServeEvent) {
	event.Router.PUT(apiRoot+pageFavoriteUpdatePath, updatePageFavoriteRoute).
		Bind(apis.BodyLimit(pageFavoriteMaxBodyBytes))
}

func updatePageFavoriteRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := pageFavoriteMutationInput{}
	if pageID == "" || event.BindBody(&input) != nil || !validPageFavoriteInput(input) {
		return apiError(event, http.StatusBadRequest, errorCodePageFavoriteInvalid, "Check the favorite change details.")
	}
	response, err := updatePageFavorite(event.App, user.Id, pageID, input)
	switch {
	case isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodePageFavoriteNotFound, "Page not found.")
	case errors.Is(err, errPageFavoriteConflict):
		return apiError(event, http.StatusConflict, errorCodePageFavoriteConflict, "The favorite state changed. Try again.")
	case errors.Is(err, errPageFavoriteIdentityReused):
		return apiError(event, http.StatusConflict, errorCodePageFavoriteIdentityReused, "The favorite change request ID can't be reused.")
	case err != nil:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodePageFavoriteFailed,
			"Couldn't change the favorite.", "page_favorite", "update",
			errorCodePageFavoriteFailed, err,
		)
	default:
		return event.JSON(http.StatusOK, response)
	}
}

func validPageFavoriteInput(input pageFavoriteMutationInput) bool {
	requestID := strings.TrimSpace(input.RequestID)
	return requestID != "" && len([]byte(requestID)) <= pageFavoriteRequestIdBytes &&
		requestID == input.RequestID && input.ExpectedRevision >= 0
}

func pageFavoriteRequestHash(input pageFavoriteMutationInput) string {
	digest := sha256.Sum256([]byte(strconv.FormatBool(input.Favorite) + ":" + strconv.Itoa(input.ExpectedRevision)))
	return hex.EncodeToString(digest[:])
}

func pageFavoriteStripe(principalID, pageID string) *sync.Mutex {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(principalID + "\x00" + pageID))
	return &pageFavoriteMutationStripes[hasher.Sum32()%pageFavoriteStripes]
}

func loadPageFavoriteState(app core.App, principalID, pageID string) (storedPageFavoriteState, error) {
	state := storedPageFavoriteState{}
	err := app.DB().NewQuery(`
		SELECT [[favorite]],[[revision]],[[last_request_id]],[[last_request_hash]]
		FROM {{leafen_page_favorites}}
		WHERE [[page_id]]={:pageId} AND [[principal_id]]={:principalId}
	`).Bind(dbx.Params{"pageId": pageID, "principalId": principalID}).One(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return storedPageFavoriteState{}, nil
	}
	return state, err
}

func updatePageFavorite(
	app core.App,
	principalID, pageID string,
	input pageFavoriteMutationInput,
) (pageFavoriteMutationResponse, error) {
	stripe := pageFavoriteStripe(principalID, pageID)
	stripe.Lock()
	defer stripe.Unlock()
	response := pageFavoriteMutationResponse{}
	workspaceID := ""
	requestHash := pageFavoriteRequestHash(input)
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizePage(txApp, principalID, pageID, permissionWorkspaceRead)
		if err != nil {
			return err
		}
		state, err := loadPageFavoriteState(txApp, principalID, pageID)
		if err != nil {
			return err
		}
		if state.LastRequestID == input.RequestID {
			if state.LastRequestHash != requestHash {
				return errPageFavoriteIdentityReused
			}
			response = pageFavoriteMutationResponse{
				PageID: pageID, Favorite: state.Favorite, Revision: state.Revision,
				RequestID: input.RequestID, Duplicate: true,
			}
			return nil
		}
		if state.Revision != input.ExpectedRevision {
			return errPageFavoriteConflict
		}
		nextRevision := state.Revision + 1
		if err := storePageFavorite(
			txApp, pageID, authorization.Workspace.WorkspaceID, principalID, input.Favorite,
			nextRevision, input.RequestID, requestHash,
		); err != nil {
			return err
		}
		response = pageFavoriteMutationResponse{
			PageID: pageID, Favorite: input.Favorite, Revision: nextRevision,
			RequestID: input.RequestID,
		}
		workspaceID = authorization.Workspace.WorkspaceID
		return nil
	})
	// The person's other screens take it (workspace_page_list_wait.go).
	if err == nil && workspaceID != "" {
		noteFavoriteChanges(workspaceID, principalID)
	}
	return response, err
}

// storePageFavorite records one person's favorite state for a page, database
// pages included, as its next revision.
func storePageFavorite(
	app core.App,
	pageID, workspaceID, principalID string,
	favorite bool,
	revision int,
	requestID, requestHash string,
) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_favorites}}
			([[page_id]],[[workspace_id]],[[principal_id]],[[favorite]],[[revision]],
			 [[last_request_id]],[[last_request_hash]],[[updated]])
		VALUES ({:pageId},{:workspaceId},{:principalId},{:favorite},{:revision},
			{:requestId},{:requestHash},{:updated})
		ON CONFLICT ([[page_id]],[[principal_id]]) DO UPDATE SET
			[[workspace_id]]=excluded.[[workspace_id]],[[favorite]]=excluded.[[favorite]],
			[[revision]]=excluded.[[revision]],[[last_request_id]]=excluded.[[last_request_id]],
			[[last_request_hash]]=excluded.[[last_request_hash]],[[updated]]=excluded.[[updated]]
	`).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": workspaceID, "principalId": principalID,
		"favorite": favorite, "revision": revision, "requestId": requestID,
		"requestHash": requestHash, "updated": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}
