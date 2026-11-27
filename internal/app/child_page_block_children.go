package app

import (
	"context"
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A block with blocks inside it turned into a page takes those blocks into
// the page, as in Notion: the page's body holds them in their order, and the
// block stays where it stood as the page's block. They move as blocks moved
// to another page do, with their comments, files, databases and sub-pages,
// once the change is saved. The change answers once they are there, unless
// they take long; then they finish moving in the background.

const childPageBlockChildrenSettleTime = 5 * time.Second

// moveChildPageBlockChildren starts moving the blocks inside the blocks a
// change made pages of into those pages, and returns the moves.
func moveChildPageBlockChildren(
	app core.App, actorID, pageID string, doc *crdt.Doc, mutationID string, made []string,
) ([]string, error) {
	moves := []string{}
	for _, blockID := range made {
		children, err := structuralOrder(doc, blockID)
		if err != nil {
			return nil, err
		}
		if len(children) == 0 {
			continue
		}
		admission, err := admitBlockChildrenMove(app, actorID, pageID, blockID, blockMoveInput{
			RequestID:         "page-block-children:" + childPageBlockDigest(mutationID, blockID),
			DestinationPageID: blockID,
		})
		var failure *blockMoveFailure
		if errors.As(err, &failure) {
			return nil, errChildPageBlockPageChange
		}
		if err != nil {
			return nil, err
		}
		moves = append(moves, admission.Job.ID)
	}
	return moves, nil
}

// settleChildPageBlockMoves moves the blocks of the blocks a saved change
// made pages of, and leaves what is left of a long move to the job runner —
// a turn the server's stop ended among it (ygo_persistence_follow_ups.go).
func (p *pocketBaseYgoPersistence) settleChildPageBlockMoves(ctx context.Context, jobIDs []string) {
	deadline := time.Now().Add(childPageBlockChildrenSettleTime)
	for _, jobID := range jobIDs {
		for time.Now().Before(deadline) {
			job, err := loadBlockMoveJobByID(p.app, jobID, "")
			if err != nil || job.Status != durableJobStatusQueued {
				break
			}
			if err := advanceBlockMove(ctx, p.followUpApp(nil), jobID); err != nil {
				break
			}
		}
	}
	p.notifyJobsAfterCommit()
}
