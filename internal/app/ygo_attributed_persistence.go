package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"
)

type ygoPersistenceActor struct {
	ActorID string
	// The connection a key's socket writes through, named in the versions.
	Connection pageHistoryConnection
}

type attributedYgoUpdate struct {
	update      []byte
	actorID     string
	connection  pageHistoryConnection
	textChanges []crdt.TextChange
	// at is when the room took the update; zero for one stored as it came.
	at time.Time
}

func (p *pocketBaseYgoPersistence) StoreUpdate(room string, update []byte) error {
	return p.StoreUpdateContext(context.Background(), room, update)
}

func (p *pocketBaseYgoPersistence) StoreUpdateContext(
	ctx context.Context,
	room string,
	update []byte,
) error {
	return p.storeAttributedUpdateBatchContext(ctx, room, []attributedYgoUpdate{{update: update}})
}

func (p *pocketBaseYgoPersistence) StoreUpdateBatchContext(
	ctx context.Context,
	room string,
	updates []ygowebsocket.PersistenceUpdate,
) error {
	attributed := make([]attributedYgoUpdate, len(updates))
	for index, update := range updates {
		attributed[index].update = update.Update
		attributed[index].textChanges = update.TextChanges
		attributed[index].at = update.At
		if update.Metadata == nil {
			continue
		}
		actor, ok := update.Metadata.(ygoPersistenceActor)
		if !ok || actor.ActorID == "" {
			return fmt.Errorf("%s: invalid persistence actor", errorCodeYgoPersistence)
		}
		attributed[index].actorID = actor.ActorID
		attributed[index].connection = actor.Connection
	}
	attributed = mergeAttributedYgoUpdates(nil, attributed)
	return p.storeAttributedUpdateBatchContext(ctx, room, attributed)
}

func (p *pocketBaseYgoPersistence) storeAttributedUpdateBatchContext(
	parent context.Context,
	room string,
	updates []attributedYgoUpdate,
) error {
	ctx, cancel, startedBeforeShutdown, operationErr := p.operationContext(parent, room)
	if operationErr != nil {
		return operationErr
	}
	defer cancel()
	err := p.storeAttributedUpdateBatchDurably(ctx, room, updates)
	if err != nil && startedBeforeShutdown && ctx.Err() != nil && p.shutdownContext.Err() != nil {
		p.markShutdownAborted(room)
	}
	if err == nil || ctx.Err() != nil || !p.settlement.admitBatch(room, updates, err) {
		return err
	}
	return nil
}

func (p *pocketBaseYgoPersistence) storeUpdateDurably(
	ctx context.Context,
	room string,
	update []byte,
) error {
	return p.storeAttributedUpdateBatchDurably(ctx, room, []attributedYgoUpdate{{update: update}})
}

func (p *pocketBaseYgoPersistence) storeAttributedUpdateBatchDurably(
	ctx context.Context,
	room string,
	updates []attributedYgoUpdate,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stripe := p.stripe(room)
	stripe.Lock()
	defer stripe.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	// Its statements that bring no context of their own — the page's
	// history, its last edit, its interactions — stop with it as those that
	// do (runOperationTransaction): a closing server stops the whole write.
	var stored ygoTextStored
	err := p.runOperationTransaction(ctx, func(txApp core.App) error {
		var err error
		stored, err = storeAttributedYgoUpdatesIn(ctx, txApp, room, updates, false)
		return err
	})
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return fmt.Errorf("%s: %w", errorCodeYgoPersistence, err)
	}
	p.settleStoredText(room, stored)
	return nil
}

// settleStoredText is what follows a page's text stored once it is
// committed (its compaction the append itself asks for): the sub-pages
// hanging on the lines written following their mentions
// (mention_anchored_pages.go).
func (p *pocketBaseYgoPersistence) settleStoredText(room string, stored ygoTextStored) {
	if !stored.changed {
		return
	}
	kept := mentionAnchorsKept(stored.actorID, room, stored.textBlocks, stored.wholePage)
	settleMentionAnchoredPagesAfterText(p.followUpApp(&kept), stored.actorID, room, stored.textBlocks, stored.wholePage)
}

// ygoTextStored is what storing a page's text updates changed.
type ygoTextStored struct {
	changed bool
	// The last writer named, or else the first: whose the sub-pages follow.
	actorID string
	// The blocks whose text the saved updates changed, for search to read
	// those alone (workspace_search_dirty_blocks.go); any other change has
	// the whole page read.
	textBlocks []string
	wholePage  bool
}

// storeAttributedYgoUpdatesIn stores a page's text updates in a transaction,
// as its room's socket does: after its history baseline, in its history, as
// their writers' edits, and for search. With read, a page whose text
// document isn't kept between changes has it read from storage, so an
// update whose text changes aren't given has them read as it applies (one
// sent with a block change, content_graph_mutation_words.go).
func storeAttributedYgoUpdatesIn(
	ctx context.Context, txApp core.App, room string, updates []attributedYgoUpdate, read bool,
) (ygoTextStored, error) {
	stored := ygoTextStored{}
	changed := false
	lastActorID := ""
	// When the last of them came in, which the page keeps as its last edit:
	// the batch is stored seconds after (2026-11-09, a page made in between
	// went below it).
	var lastEditAt time.Time
	effectiveInteractions := []pageInteraction{}
	textBlocks, wholePage := []string{}, false
	err := func() error {
		if rowPage, err := isDatabaseRowPageRoom(txApp, room); err != nil {
			return err
		} else if rowPage {
			changed, err = storeDatabaseRowBodyUpdates(ctx, txApp, room, updates)
			wholePage = true
			return err
		}
		if len(updates) > 0 {
			if err := ensurePageHistoryBaseline(txApp, room); err != nil {
				return err
			}
		}
		// The page's text document kept between changes takes the updates
		// too, so a change after reads it rather than merging the stored
		// parts again (ygo_structural_document_cache.go): kept only while
		// it was kept already, known by its mark (ygo_document_marks.go) —
		// told by the stored parts' digest, each person's typing on a long
		// page read the page's whole document to tell (2026-10-18).
		var document *crdt.Doc
		bytes := 0
		if read {
			// Read whole, with its block texts known as texts, so each
			// change names the blocks it changed; a kept one may not know them.
			_, parts, _, err := loadYgoStreamParts(txApp, ygoCollaborationStream, room)
			if err != nil {
				return err
			}
			state, err := ygoStreamState(parts)
			if err != nil {
				return err
			}
			document = crdt.New(crdt.WithTextRoots(ygoIsBlockTextRoot))
			if err := applyYgoState(document, state); err != nil {
				return err
			}
			bytes = ygoStreamPartsBytes(parts)
		} else if ygoTextDocuments.holds(room) {
			mark, marked, err := loadYgoStreamMark(txApp, ygoCollaborationStream, room)
			if err != nil {
				return err
			}
			if marked {
				document, bytes = ygoTextDocuments.take(room, mark)
			}
		}
		if document != nil {
			defer func() {
				transaction := txApp.TxInfo()
				if transaction == nil {
					return
				}
				mark, marked, err := loadYgoStreamMark(txApp, ygoCollaborationStream, room)
				if err != nil || !marked {
					return
				}
				transaction.OnComplete(func(txErr error) error {
					if txErr == nil {
						ygoTextDocuments.put(room, mark, document, bytes)
					}
					return nil
				})
			}()
		}
		for _, update := range updates {
			effective, err := appendYgoUpdateIfChangedContext(ctx, txApp, room, update.update)
			if err != nil {
				return err
			}
			if !effective {
				continue
			}
			if document != nil {
				var details crdt.UpdateDetails
				stop := document.OnUpdateDetails(func(value crdt.UpdateDetails) { details = value })
				err := crdt.ApplyUpdateV1(document, update.update, nil)
				stop()
				if err != nil {
					return err
				}
				if update.textChanges == nil {
					if update.textChanges, err = ygoStoredTextChanges(document, update.update, details.TextChanges); err != nil {
						return err
					}
				}
				bytes += len(update.update)
			}
			if len(update.textChanges) == 0 {
				wholePage = true
			}
			for _, change := range update.textChanges {
				blockID, blockText := strings.CutPrefix(change.Name, ygoBlockTextRootPrefix)
				if !blockText || blockID == "" {
					wholePage = true
				} else if !slices.Contains(textBlocks, blockID) {
					textBlocks = append(textBlocks, blockID)
				}
			}
			if err := withPageHistoryConnection(txApp, update.connection, func() error {
				return recordPageHistoryCollaborationUpdate(txApp, room, update.actorID, update.update, update.textChanges)
			}); err != nil {
				return err
			}
			changed = true
			if update.actorID != "" {
				lastActorID = update.actorID
				lastEditAt = laterYgoEditTime(lastEditAt, update.at)
				// One edit a person a clock minute: one for every update the
				// realtime editor sent was three index entries a keystroke
				// (2026-10-15).
				if !slices.ContainsFunc(effectiveInteractions, func(known pageInteraction) bool { return known.UserID == update.actorID }) {
					effectiveInteractions = append(effectiveInteractions, pageInteraction{
						PageID:          room,
						UserID:          update.actorID,
						InteractionType: pageInteractionEdited,
					})
				}
			}
		}
		if !changed {
			return nil
		}
		if lastActorID != "" {
			if err := setPageLastEditorAt(ctx, txApp, room, lastActorID, lastEditAt); err != nil {
				return err
			}
			scope, err := loadPageInteractionScope(txApp, room)
			if err != nil {
				return err
			}
			minute := time.Now().UTC().UnixMilli() / 60000
			for index := range effectiveInteractions {
				interaction := &effectiveInteractions[index]
				interaction.EventID = pageInteractionEventID(
					pageInteractionEdited, "ygo-minute", room, interaction.UserID, strconv.FormatInt(minute, 10),
				)
				interaction.OrganizationID = scope.OrganizationID
				interaction.WorkspaceID = scope.WorkspaceID
				interaction.OccurredAt = minute * 60000
			}
			if err := recordNotionAdminLegalHoldPageInteraction(
				txApp, effectiveInteractions...,
			); err != nil {
				return err
			}
		}
		if wholePage {
			return markSearchDirtyByPageIDContext(ctx, txApp, room)
		}
		return markSearchBlocksDirtyContext(ctx, txApp, room, textBlocks)
	}()
	if err != nil {
		return stored, err
	}
	stored.changed, stored.actorID, stored.textBlocks, stored.wholePage = changed, lastActorID, textBlocks, wholePage
	for _, update := range updates {
		if stored.actorID == "" && update.actorID != "" {
			stored.actorID = update.actorID
		}
	}
	return stored, nil
}

// ygoStoredTextChanges is the text an update changed, applied to the page's
// stored document: the texts the document saw change, and, while the
// document holds structs it can't place yet, the texts the update writes,
// read from the update itself. An editor's words sent with a block change
// (content_graph_mutation_words.go) reach storage at once, while the
// earlier typing they build on waits in the page's room to be stored a few
// seconds later: the document holds them until then, and named nothing, so
// the page's history showed the lines typed fast after Enter without their
// text (2026-11-13).
func ygoStoredTextChanges(document *crdt.Doc, update []byte, seen []crdt.TextChange) ([]crdt.TextChange, error) {
	if document.PendingStats().Items == 0 {
		return seen, nil
	}
	roots, err := crdt.UpdateRootsV1(document, update)
	if err != nil {
		return nil, err
	}
	changes := slices.Clone(seen)
	for _, root := range roots {
		if !slices.ContainsFunc(changes, func(change crdt.TextChange) bool { return change.Name == root }) {
			changes = append(changes, crdt.TextChange{Name: root})
		}
	}
	return changes, nil
}
