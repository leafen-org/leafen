package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A move may name where its block lands among its destination's top blocks,
// as restoring a page version puts a database back where the version had it,
// and the moves to make after it, one at a time, as a page takes back
// databases from other pages while a move holds the pages it touches. Both
// ride in the move's job payload.

// blockMoveReturn is a block to move back to a page, and where it lands.
type blockMoveReturn struct {
	SourcePageID string `json:"sourcePageId"`
	BlockID      string `json:"blockId"`
	// After are the destination's top blocks it goes after, nearest first:
	// it lands after the first of them there, or first when none is.
	After []string `json:"after"`
	// AfterSource, when set, names them instead as the blocks before it in a
	// Notion migration's source, read when the move runs.
	AfterSource *notionMigrationSiblingsBefore `json:"afterSource,omitempty"`
	// DestinationPageID is the page it goes to, when not the page the moves
	// before it went to.
	DestinationPageID string `json:"destinationPageId,omitempty"`
}

type blockMovePlacement struct {
	// A native button copies the children of its draft container. This is a
	// discovery-only root, and its roots land together at the chosen spot.
	CopyChildrenParent string               `json:"copyChildrenParent,omitempty"`
	CopyParent         string               `json:"copyParent,omitempty"`
	CopyAfter          string               `json:"copyAfter,omitempty"`
	CopyUndo           *nativeBlockCopyUndo `json:"copyUndo,omitempty"`
	// A paste's copy lands after CopyAfter, under CopyParent (pastedBlockCopySpot).
	CopyPlaced bool `json:"copyPlaced,omitzero"`
	// BodyCopy is the page whose whole body the copy copies (admitPageBodyCopy).
	BodyCopy string `json:"bodyCopy,omitempty"`
	// KeepName keeps a copied database's name: a page made from a database
	// as its template is a copy of it (notion_page_template_database.go).
	KeepName bool `json:"keepName,omitzero"`
	// A conversion return puts body roots back inside the restored block.
	ConversionReturnParent string `json:"conversionReturnParent,omitempty"`
	// Placed marks a move that lands where After and AfterSource say rather
	// than last.
	Placed      bool                           `json:"placed,omitempty"`
	After       []string                       `json:"after,omitempty"`
	AfterSource *notionMigrationSiblingsBefore `json:"afterSource,omitempty"`
	// Then are the moves that follow this one, to the same page unless they
	// name another, made under request IDs from ThenRequestID.
	Then          []blockMoveReturn `json:"then,omitempty"`
	ThenRequestID string            `json:"thenRequestId,omitempty"`
	// Ending marks a move whose every block has gone, which only ends
	// (markBlockMoveEnding).
	Ending bool `json:"ending,omitzero"`
}

func loadBlockMovePlacement(app core.App, jobID string) (blockMovePlacement, error) {
	raw := ""
	err := app.DB().NewQuery(`SELECT [[payload_json]] FROM {{leafen_jobs}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": jobID}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return blockMovePlacement{}, nil
	}
	placement := blockMovePlacement{}
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &placement)
	}
	return placement, err
}

// blockMoveDestinationSpot is the container and preceding block a moved
// block lands at. Conversion returns land inside the restored block;
// ordinary moves land among the destination's top blocks.
func blockMoveDestinationSpot(
	app core.App, job storedBlockMoveJob, item storedBlockMoveItem, document *blockMoveDocument, placement blockMovePlacement,
) (string, *string, error) {
	if placement.ConversionReturnParent != "" && item.Depth == 0 {
		after, err := document.last(placement.ConversionReturnParent)
		return placement.ConversionReturnParent, after, err
	}
	if item.BlockID == job.RootBlockID && placement.Placed {
		entry, err := document.orders.entry("")
		if err != nil {
			return "", nil, err
		}
		order := entry.ids
		for _, candidate := range placement.After {
			if structuralIndex(order, candidate) >= 0 {
				after := candidate
				return "", &after, nil
			}
		}
		if placement.AfterSource != nil {
			after, found, err := placement.AfterSource.nearestIn(app, order)
			if err != nil || found {
				return "", &after, err
			}
		}
		return "", nil, nil
	}
	after, err := document.last("")
	return "", after, err
}

// admitBlockMoveReturns starts the first of the moves back to a page that
// can start, with the rest to follow it. One that can't — its block or page
// gone, or not this person's to move — stays where it is.
func admitBlockMoveReturns(
	app core.App, actorID, destinationPageID, requestID string, returns []blockMoveReturn,
) (bool, error) {
	for index, next := range returns {
		destination := destinationPageID
		if next.DestinationPageID != "" {
			destination = next.DestinationPageID
		}
		admission, err := admitBlockMove(app, actorID, next.SourcePageID, next.BlockID, blockMoveInput{
			RequestID: requestID + ":" + next.BlockID, DestinationPageID: destination,
			Operation: blockMoveOperationMove,
		})
		failure := &blockMoveFailure{}
		if errors.As(err, &failure) || isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !admission.Admitted {
			continue
		}
		payload, err := json.Marshal(blockMovePlacement{
			Placed: true, After: next.After, AfterSource: next.AfterSource, Then: returns[index+1:], ThenRequestID: requestID,
		})
		if err != nil {
			return false, err
		}
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[payload_json]]={:payload} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"payload": string(payload), "id": admission.Job.ID}).Execute(); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// continueBlockMoveReturns starts the moves that follow a move that ended.
func continueBlockMoveReturns(app core.App, job storedBlockMoveJob) error {
	placement, err := loadBlockMovePlacement(app, job.ID)
	if err != nil {
		return err
	}
	if placement.ConversionReturnParent != "" && job.Phase == blockMovePhaseFailed {
		// A rollback leaves its body in the recoverable page. Bring its page
		// block back, rather than leave an apparently restored empty block.
		return recoverChildPageConversion(app, job)
	}
	if placement.ConversionReturnParent != "" {
		// The page's body is in its block now; its discussions follow.
		return moveConvertedPageComments(app, job.SourcePageID, job.DestinationPageID, true)
	}
	if len(placement.Then) == 0 {
		return nil
	}
	_, err = admitBlockMoveReturns(
		app, job.ActorID, job.DestinationPageID, placement.ThenRequestID, placement.Then,
	)
	return err
}
