package app

import (
	"context"
	"encoding/json"

	"github.com/pocketbase/dbx"
)

// Deleting a synced block original in the app lets its copies go first,
// as decided for Leafen: with at most
// ten copies each becomes ordinary blocks holding the original's content, and
// with more every copy is removed, which can't be undone. Notion's own
// deletion offers to "unsync" its copies but leaves each one a notice of the
// deleted original (seen 2026-10-15); keeping their content is Leafen's
// choice. Notion's API keeps copies as they are, and so does Leafen's
// (deleteNotionBlock). It runs before the change that deletes the original, for someone
// who may make that change, and a copy on a page they can't change stays —
// counted, so the person who deleted the original is told (2026-10-13: such
// copies stayed without a word).

// syncedOriginalDeletionCopies is how many copies one read takes; a deletion
// lets every copy go, a window at a time.
const syncedOriginalDeletionCopies = 100

type syncedDeletedOriginalCopy struct {
	ID     string `db:"id"`
	PageID string `db:"page_id"`
}

// letDeletedSyncedOriginalsGo lets go of the copies of each synced block
// original the operations delete on the page, blocks deleted with the
// original's parent included, and says how many it had to leave.
func (p *pocketBaseYgoPersistence) letDeletedSyncedOriginalsGo(
	ctx context.Context, actorID, pageID string, operations []graphOperation,
) (int, error) {
	deleted := []string{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindDeleteBlock && operation.Deletion != nil {
			deleted = append(deleted, operation.Deletion.BlockID)
		}
	}
	if len(deleted) == 0 {
		return 0, nil
	}
	authorization, err := authorizePageBody(p.app, actorID, pageID, permissionWorkspaceWrite)
	if err != nil {
		return 0, nil
	}
	if locked, err := pageLockedFor(p.app, authorization.Page, actorID); err != nil || locked {
		return 0, err
	}
	owner := authorization.Workspace.OwnerID
	encoded, err := json.Marshal(deleted)
	if err != nil {
		return 0, err
	}
	gone := []string{}
	if err := p.app.DB().NewQuery(`
		WITH RECURSIVE [[tree]]([[id]]) AS (
			SELECT [[id]] FROM {{leafen_blocks}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:page} AND [[id]] IN (SELECT [[value]] FROM json_each({:deleted}))
			UNION
			-- CROSS JOIN steps from the tree by key (owner, page, parent): SQLite's
			-- own order read the page's every block for each block (2026-11-08).
			SELECT [[child]].[[id]] FROM [[tree]]
			CROSS JOIN {{leafen_blocks}} AS [[child]] ON [[child]].[[parent_id]] = [[tree]].[[id]]
			WHERE [[child]].[[owner]] = {:owner} AND [[child]].[[page_id]] = {:page}
		)
		SELECT [[id]] FROM [[tree]]
	`).Bind(dbx.Params{"owner": owner, "page": pageID, "deleted": string(encoded)}).Column(&gone); err != nil {
		return 0, err
	}
	goneSet := make(map[string]bool, len(gone))
	for _, id := range gone {
		goneSet[id] = true
	}
	originals := []string{}
	if len(gone) > 0 {
		encodedGone, err := json.Marshal(gone)
		if err != nil {
			return 0, err
		}
		if err := p.app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_blocks}}
			WHERE [[owner]] = {:owner} AND [[id]] IN (SELECT [[value]] FROM json_each({:gone}))
				AND [[type]] = 'synced_block' AND COALESCE(` + syncedBlockOriginalSQL + `, '') = ''
			ORDER BY [[id]]
		`).Bind(dbx.Params{"owner": owner, "gone": string(encodedGone)}).Column(&originals); err != nil {
			return 0, err
		}
	}
	kept := 0
	for _, originalID := range originals {
		// Past ten copies every one is removed; the first window tells which.
		remove := false
		afterPage, afterID := "", ""
		for first := true; ; first = false {
			copies := []syncedDeletedOriginalCopy{}
			if err := p.app.DB().NewQuery(`
				SELECT [[id]], [[page_id]] FROM {{leafen_blocks}}
				WHERE [[owner]] = {:owner} AND [[type]] = 'synced_block' AND ` + syncedBlockOriginalSQL + ` = {:original}
					AND ([[page_id]] > {:afterPage} OR ([[page_id]] = {:afterPage} AND [[id]] > {:afterId}))
				ORDER BY [[page_id]], [[id]] LIMIT {:limit}
			`).Bind(dbx.Params{
				"owner": owner, "original": originalID, "limit": syncedOriginalDeletionCopies,
				"afterPage": afterPage, "afterId": afterID,
			}).All(&copies); err != nil {
				return 0, err
			}
			if first {
				remove = len(copies) > syncedBlockUnsyncLimit
			}
			if remove {
				kept += p.removeSyncedCopies(ctx, actorID, copies, goneSet)
			} else {
				for _, copied := range copies {
					if goneSet[copied.ID] {
						continue
					}
					// A copy that can't be unsynced stays as it is.
					if _, _, err := p.unsyncSyncedCopy(ctx, p.app, actorID, copied.PageID, copied.ID); err != nil {
						kept++
					}
				}
			}
			if len(copies) < syncedOriginalDeletionCopies {
				break
			}
			afterPage, afterID = copies[len(copies)-1].PageID, copies[len(copies)-1].ID
		}
	}
	return kept, nil
}

// syncedBlockUnsyncLimit is the most copies an original lets go by
// unsyncing them; with more they are removed, as in Notion.
const syncedBlockUnsyncLimit = 10

// removeSyncedCopies deletes copies on pages the actor can change, one
// change for each page's copies; a page they can't change keeps its copies,
// which it counts.
func (p *pocketBaseYgoPersistence) removeSyncedCopies(
	ctx context.Context, actorID string, copies []syncedDeletedOriginalCopy, gone map[string]bool,
) int {
	byPage := map[string][]graphOperation{}
	pages := []string{}
	for _, copied := range copies {
		if gone[copied.ID] {
			continue
		}
		if _, seen := byPage[copied.PageID]; !seen {
			pages = append(pages, copied.PageID)
		}
		byPage[copied.PageID] = append(byPage[copied.PageID],
			graphOperation{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: copied.ID}})
	}
	kept := 0
	for _, pageID := range pages {
		if _, err := p.applyContentGraphMutationContext(ctx, actorID, pageID, graphMutationInput{
			MutationID: "synced-original-removed:" + newDiagnosticReference(), Operations: byPage[pageID],
		}, contentGraphMutationHooks{EnforcePageLock: true}); err != nil {
			kept += len(byPage[pageID])
		}
	}
	return kept
}
