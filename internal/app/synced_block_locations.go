package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A synced block is where its original and its copies are, as Notion lists
// the pages a synced block is edited in: the reader sees the original's page
// and the copies on pages they can open, out of the trash with every page
// above them. Someone who can't open the original's page sees none of it.

const syncedBlockLocationsLimit = 100

type syncedBlockLocation struct {
	BlockID string `json:"blockId" db:"id"`
	PageID  string `json:"pageId" db:"page_id"`
	// InTrash marks an original that is a page's body while the page is in
	// the trash (synced_block_page_original.go).
	InTrash bool `json:"inTrash,omitempty" db:"-"`
}

type syncedBlockLocations struct {
	Original syncedBlockLocation   `json:"original"`
	Copies   []syncedBlockLocation `json:"copies"`
	HasMore  bool                  `json:"hasMore"`
	// NextAfter reads the copies after these ("<pageId> <blockId>").
	NextAfter string `json:"nextAfter,omitempty"`
}

func readSyncedBlockLocations(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	locations, err := loadSyncedBlockLocations(event.App, user.Id,
		strings.TrimSpace(event.Request.PathValue("id")), strings.TrimSpace(event.Request.PathValue("blockId")),
		strings.TrimSpace(event.Request.URL.Query().Get("after")))
	if err != nil {
		return writeGraphError(event, err, "synced-block-locations", errorCodeGraphReadFailed, "Couldn't read where the synced block is.")
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, locations)
}

// loadSyncedBlockLocations reads where the synced block on the page is, the
// block being the original or one of its copies: its copies a window at a
// time, after the one `after` names.
func loadSyncedBlockLocations(app core.App, principalID, pageID, blockID, after string) (syncedBlockLocations, error) {
	result := syncedBlockLocations{Copies: []syncedBlockLocation{}}
	page, err := authorizePageBody(app, principalID, pageID, permissionWorkspaceRead)
	if err != nil {
		return result, graphNotFoundError()
	}
	owner := page.Workspace.OwnerID
	block := storedSyncedBlockSource{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]], [[position]], [[type]], [[content]] FROM {{leafen_blocks}}
		WHERE [[id]] = {:id} AND [[page_id]] = {:pageId} AND [[owner]] = {:owner}
	`).Bind(dbx.Params{"id": blockID, "pageId": pageID, "owner": owner}).One(&block)
	if errors.Is(err, sql.ErrNoRows) || err == nil && block.Type != graphBlockTypeSyncedBlock {
		return result, graphNotFoundError()
	}
	if err != nil {
		return result, err
	}
	content := map[string]any{}
	_ = json.Unmarshal([]byte(block.Content), &content)
	result.Original = syncedBlockLocation{BlockID: blockID, PageID: pageID}
	if page := syncedCopyPage(content); page != "" {
		// A copy of a page's body: the page is its original.
		if result.Original, err = loadSyncedPageOriginal(app, principalID, page); err != nil {
			return result, err
		}
	} else if originalID, _ := content["syncedBlockId"].(string); strings.TrimSpace(originalID) != "" {
		result.Original.BlockID = strings.TrimSpace(originalID)
		if err := app.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_blocks}}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[type]] = 'synced_block'
		`).Bind(dbx.Params{"id": result.Original.BlockID, "owner": owner}).Row(&result.Original.PageID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, errSyncedBlockUnsyncUnavailable
			}
			return result, err
		}
		if _, err := authorizePageBody(app, principalID, result.Original.PageID, permissionWorkspaceRead); err != nil {
			return result, refusedUnlessStorage(errSyncedBlockUnsyncUnavailable, err)
		}
	}
	afterPage, afterBlock, _ := strings.Cut(after, " ")
	copies := []syncedBlockLocation{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[page_id]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[type]] = 'synced_block' AND ` + syncedBlockOriginalSQL + ` = {:original}
			AND ([[page_id]] > {:afterPage} OR ([[page_id]] = {:afterPage} AND [[id]] > {:afterBlock}))
		ORDER BY [[page_id]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": owner, "original": result.Original.BlockID, "limit": syncedBlockLocationsLimit + 1,
		"afterPage": afterPage, "afterBlock": afterBlock,
	}).All(&copies); err != nil {
		return result, err
	}
	if len(copies) > syncedBlockLocationsLimit {
		copies, result.HasMore = copies[:syncedBlockLocationsLimit], true
		last := copies[len(copies)-1]
		result.NextAfter = last.PageID + " " + last.BlockID
	}
	// A copy on a page in the trash, or under a page there, isn't listed
	// (loadPageTrashEffects).
	copyPages := make([]string, 0, len(copies))
	for _, copied := range copies {
		copyPages = append(copyPages, copied.PageID)
	}
	trashed, err := loadPageTrashEffects(app, copyPages)
	if err != nil {
		return result, err
	}
	for _, copied := range copies {
		if _, inTrash := trashed[copied.PageID]; inTrash {
			continue
		}
		authorization, err := authorizePageBody(app, principalID, copied.PageID, permissionWorkspaceRead)
		if err != nil || authorization.Page == nil {
			continue
		}
		result.Copies = append(result.Copies, copied)
	}
	return result, nil
}
