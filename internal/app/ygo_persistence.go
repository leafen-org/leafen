package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	errorCodeYgoPersistence         = "LEAFEN_YGO_PERSISTENCE_FAILED"
	errorCodeYgoShutdown            = "LEAFEN_YGO_SHUTDOWN_FAILED"
	errorCodeYgoUpdateTooLarge      = "LEAFEN_YGO_UPDATE_TOO_LARGE"
	errorCodeYgoPendingBacklogFull  = "LEAFEN_YGO_PENDING_BACKLOG_FULL"
	errorCodeYgoLegacyTextInvalid   = "LEAFEN_YGO_LEGACY_TEXT_INVALID"
	errorCodeYgoFormatCutoverFailed = "LEAFEN_YGO_FORMAT_CUTOVER_FAILED"
	ygoPersistenceStripes           = 64
	ygoLegacySeedWindow             = 256
	maxYgoUpdateBytes               = 64 << 20
	ygoBlockTextRootPrefix          = "block:"
	ygoPersistenceStoreKey          = "leafen.ygo-persistence"
)

type storedYgoDocument struct {
	State            []byte `db:"state"`
	StateRevision    int64  `db:"state_revision"`
	FormatVersion    int    `db:"format_version"`
	SnapshotSequence int64  `db:"snapshot_sequence"`
	NextSequence     int64  `db:"next_sequence"`
	PendingCount     int    `db:"pending_count"`
	PendingBytes     int    `db:"pending_bytes"`
	CompactionStatus string `db:"compaction_status"`
}

// pageHistoryTextDeltas names the blocks whose text an update changed; the
// change itself is the update, from which the history reads their text.
func pageHistoryTextDeltas(changes []crdt.TextChange) []pageHistoryTextDelta {
	deltas := make([]pageHistoryTextDelta, 0, len(changes))
	named := map[string]bool{}
	for _, change := range changes {
		if !strings.HasPrefix(change.Name, ygoBlockTextRootPrefix) {
			continue
		}
		blockID := strings.TrimPrefix(change.Name, ygoBlockTextRootPrefix)
		if blockID == "" || named[blockID] {
			continue
		}
		named[blockID] = true
		deltas = append(deltas, pageHistoryTextDelta{BlockID: blockID})
	}
	return deltas
}

// recordPageHistoryCommentMarks records a change that only marked a page's
// text with a discussion as a version of its own that is no edit
// (pageHistoryVersionCommentMarks): never taken into the commenter's typing.
func recordPageHistoryCommentMarks(
	app core.App, pageID, actorID string, update []byte, changes []crdt.TextChange,
) error {
	payload, err := json.Marshal(pageHistoryEventEnvelope{
		Format: 1, Update: append([]byte(nil), update...), TextDeltas: pageHistoryTextDeltas(changes),
	})
	if err != nil {
		return err
	}
	_, err = recordPageHistoryEvent(app, pageID, actorID, pageHistoryVersionCommentMarks, payload)
	return err
}

func recordPageHistoryCollaborationUpdate(
	app core.App, pageID, actorID string, update []byte, changes []crdt.TextChange,
) error {
	deltas := pageHistoryTextDeltas(changes)
	// A person's typing goes on in their version of the minute where it can
	// (page_history_storage.go).
	_, extended, err := extendPageHistoryCollaborationVersion(app, pageID, actorID, update, deltas)
	if err != nil {
		return err
	}
	if !extended {
		payload, err := json.Marshal(pageHistoryEventEnvelope{
			Format: 1, Update: append([]byte(nil), update...), TextDeltas: deltas,
		})
		if err != nil {
			return err
		}
		if _, err = recordPageHistoryEvent(app, pageID, actorID, pageHistoryEventCollaboration, payload); err != nil {
			return err
		}
	}
	// Typing writes no history text of its own: the blocks it changed take
	// their text from the replayed versions when the history shows them
	// (page_history_text_replay.go), and every checkpoint settles the text
	// the history keeps (2026-10-15: written for every update, a block's
	// whole text was written again on each keystroke the realtime editor
	// sent; 100 people typing made 10.8 GB in 17 minutes and stopped writes).
	return nil
}

type legacyYgoParagraph struct {
	ID      string `db:"id"`
	Content string `db:"content"`
}

type pocketBaseYgoPersistence struct {
	app               core.App
	stripes           [ygoPersistenceStripes]sync.Mutex
	compactionNotify  func()
	jobsNotify        func()
	settlement        *ygoPersistenceSettlementOwner
	shutdownContext   context.Context
	shutdownCancel    context.CancelFunc
	shutdownMu        sync.Mutex
	closingStop       context.Context // the writes begun once closing end by it (beginShutdown)
	shutdownAborted   map[string]struct{}
	preShutdownActive int
	preShutdownIdle   chan struct{}
}

func (p *pocketBaseYgoPersistence) bindJobNotifier(notify func()) {
	p.jobsNotify = notify
}

// notifyJobsAfterCommit wakes the job runner for jobs a saved change made.
func (p *pocketBaseYgoPersistence) notifyJobsAfterCommit() {
	if p.jobsNotify != nil {
		p.jobsNotify()
	}
}

func newPocketBaseYgoPersistence(app core.App) *pocketBaseYgoPersistence {
	shutdownContext, shutdownCancel := context.WithCancel(context.Background())
	preShutdownIdle := make(chan struct{})
	close(preShutdownIdle)
	persistence := &pocketBaseYgoPersistence{
		app: app, shutdownContext: shutdownContext, shutdownCancel: shutdownCancel,
		shutdownAborted: map[string]struct{}{}, preShutdownIdle: preShutdownIdle,
	}
	persistence.settlement = newYgoPersistenceSettlementOwner(app, persistence)
	return persistence
}

func (p *pocketBaseYgoPersistence) LoadDoc(room string) ([]byte, error) {
	stripe := p.stripe(room)
	stripe.Lock()
	defer stripe.Unlock()

	stored, found, err := loadMaterializedYgoDocument(p.app, room)
	var recovery *ygoCorruptUpdateRecovery
	if err != nil {
		loadErr := err
		err = p.app.RunInTransaction(func(txApp core.App) error {
			candidate, recoveryErr := recoverTrailingCorruptYgoUpdate(txApp, room)
			if recoveryErr != nil {
				return recoveryErr
			}
			if candidate == nil {
				return loadErr
			}
			recovery = candidate
			stored = candidate.Document
			found = true
			return nil
		})
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errorCodeYgoPersistence, err)
	}
	if recovery != nil {
		p.settlement.recordRecovered(room, recovery.Reference)
		logYgoCorruptUpdateRecovery(p.app, recovery)
	}
	if !found {
		stored, err = p.seedBlockTextDocument(room, stored)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", errorCodeYgoPersistence, err)
		}
		// The paragraphs' words are the document's now. Cleared each time a
		// room opened, every paragraph of the page was read for words long
		// gone (2026-10-18 soak); a page's words left in its content since
		// are given to its document where it has none (migration 529, and
		// each change that writes them, liftGraphBlockStoredText).
		if err := p.clearLegacyParagraphText(room); err != nil {
			return nil, fmt.Errorf("%s: %w", errorCodeYgoPersistence, err)
		}
	}
	return append([]byte(nil), stored.State...), nil
}

func (p *pocketBaseYgoPersistence) seedBlockTextDocument(room string, legacy storedYgoDocument) (storedYgoDocument, error) {
	doc := crdt.New()
	if len(legacy.State) > 0 {
		if err := crdt.ApplyUpdateV1(doc, legacy.State, nil); err != nil {
			return storedYgoDocument{}, err
		}
	}

	cursor := ""
	for {
		paragraphs := []legacyYgoParagraph{}
		err := p.app.DB().NewQuery(`
			SELECT [[id]], [[content]]
			FROM {{leafen_blocks}}
			WHERE [[page_id]] = {:room} AND [[type]] = 'paragraph' AND [[id]] > {:cursor}
			ORDER BY [[id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"room": room, "cursor": cursor, "limit": ygoLegacySeedWindow,
		}).All(&paragraphs)
		if err != nil {
			return storedYgoDocument{}, err
		}
		if len(paragraphs) == 0 {
			break
		}

		var seedErr error
		doc.Transact(func(txn *crdt.Transaction) {
			for _, paragraph := range paragraphs {
				var content map[string]any
				if err := json.Unmarshal([]byte(paragraph.Content), &content); err != nil {
					seedErr = err
					return
				}
				value, exists := content["text"]
				if !exists {
					continue
				}
				text, ok := value.(string)
				if !ok {
					seedErr = errors.New(errorCodeYgoLegacyTextInvalid)
					return
				}
				sharedText := txn.GetText(ygoBlockTextRootPrefix + paragraph.ID)
				if sharedText.Len() == 0 && text != "" {
					sharedText.Insert(txn, 0, text, nil)
				}
			}
		})
		if seedErr != nil {
			return storedYgoDocument{}, seedErr
		}
		cursor = paragraphs[len(paragraphs)-1].ID
		if len(paragraphs) < ygoLegacySeedWindow {
			break
		}
	}
	state := crdt.EncodeStateAsUpdateV1(doc, nil)
	err := p.app.RunInTransaction(func(txApp core.App) error {
		return seedYgoSnapshotUnlessStored(txApp, room, state)
	})
	if err != nil {
		return storedYgoDocument{}, err
	}
	stored, found, err := loadMaterializedYgoDocument(p.app, room)
	if err != nil {
		return storedYgoDocument{}, err
	}
	if !found || stored.FormatVersion != ygoDocumentFormatVersion {
		return storedYgoDocument{}, errors.New(errorCodeYgoFormatCutoverFailed)
	}
	return stored, nil
}

func (p *pocketBaseYgoPersistence) clearLegacyParagraphText(room string) error {
	for {
		paragraphs := []legacyYgoParagraph{}
		err := p.app.DB().NewQuery(`
			SELECT [[id]]
			FROM {{leafen_blocks}}
			WHERE [[page_id]] = {:room} AND [[type]] = 'paragraph'
				AND CASE WHEN json_valid([[content]])
					THEN json_type([[content]], '$.text') IS NOT NULL ELSE 0 END
			ORDER BY [[id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{"room": room, "limit": ygoLegacySeedWindow}).All(&paragraphs)
		if err != nil {
			return err
		}
		if len(paragraphs) == 0 {
			return nil
		}

		params := dbx.Params{"room": room}
		bindings := make([]string, len(paragraphs))
		for index, paragraph := range paragraphs {
			key := fmt.Sprintf("block%d", index)
			params[key] = paragraph.ID
			bindings[index] = "{:" + key + "}"
		}
		err = p.app.RunInTransaction(func(txApp core.App) error {
			_, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_blocks}}
				SET [[content]] = json_remove([[content]], '$.text')
				WHERE [[page_id]] = {:room} AND [[id]] IN (` + strings.Join(bindings, ", ") + `)
			`).Bind(params).Execute()
			return err
		})
		if err != nil {
			return err
		}
	}
}

func (p *pocketBaseYgoPersistence) mutateRoomDocument(
	room string,
	mutate func(core.App, *crdt.Doc) (bool, error),
	publish func(core.App, []byte, []byte) error,
) error {
	return p.mutateRoomDocumentContext(
		context.Background(), room,
		func(_ context.Context, app core.App, doc *crdt.Doc) (bool, error) {
			return mutate(app, doc)
		},
		func(_ context.Context, app core.App, update, stateVector []byte) error {
			return publish(app, update, stateVector)
		},
	)
}

func (p *pocketBaseYgoPersistence) mutateRoomDocumentContext(
	parent context.Context,
	room string,
	mutate func(context.Context, core.App, *crdt.Doc) (bool, error),
	publish func(context.Context, core.App, []byte, []byte) error,
) error {
	// Asked for, the change is made whole (madeWhole).
	ctx, cancel, startedBeforeShutdown, err := p.operationContext(madeWhole(parent), room)
	if err != nil {
		return err
	}
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	stripe := p.stripe(room)
	stripe.Lock()
	defer stripe.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	var recovered *ygoStructuralRecovery
	err = p.runOperationTransaction(p.operationStop(startedBeforeShutdown), func(txApp core.App) (err error) {
		// The blocks a change writes raise their workspace's revisions once
		// (block_revision_hold_schema.go).
		release, err := holdBlockRevisions(txApp, room)
		if err != nil {
			return err
		}
		defer func() { err = release(err) }()
		// A change a connection makes names it in the page's history.
		defer namePageHistoryConnection(txApp, parent)()
		recovered, err = recoverTrailingUnownedStructuralUpdate(txApp, room)
		if err != nil || recovered != nil {
			return err
		}
		// The document a change just left, when nothing changed it since
		// (ygo_structural_document_cache.go), known by its mark
		// (ygo_document_marks.go): told by the stored parts' digest, every
		// change read the page's whole structure to tell (2026-10-18).
		mark, marked, err := loadYgoStreamMark(txApp, ygoStructuralStream, room)
		if err != nil {
			return err
		}
		writer := ygoServerWriters.lease(txApp, ygoStructuralStream, room)
		var doc *crdt.Doc
		bytes := 0
		if marked {
			doc, bytes = writer.take(ygoStructuralDocuments, room, mark)
		}
		found, empty := doc != nil, false
		if doc == nil {
			// The stored structure goes into the document once, which checks
			// it (2026-10-14: checked first in a document of its own, every
			// turn of a long page's import read it twice).
			_, parts, stored, err := loadYgoStreamParts(txApp, ygoStructuralStream, room)
			if err != nil {
				return err
			}
			found = stored
			if found {
				if doc, err = writer.document(parts); err != nil {
					return err
				}
				bytes, empty = ygoStreamPartsBytes(parts), len(parts[0]) == 0 && len(parts) == 1
			} else {
				doc = crdt.New()
			}
		}
		newStream := !found
		if newStream || empty {
			if err := seedStructuralGraphDocument(txApp, room, doc); err != nil {
				return err
			}
			seeded := crdt.EncodeStateAsUpdateV1(doc, nil)
			if err := replaceYgoStructuralSnapshot(txApp, room, seeded); err != nil {
				return err
			}
			bytes = len(seeded)
		}
		since := crdt.MarkChanges(doc)
		changed, err := mutate(ctx, txApp, doc)
		if err != nil {
			return err
		}
		// The document stays for the page's next change, once this one is
		// kept, under the mark the stored one has with it.
		keep := func(bytes int) error {
			transaction := txApp.TxInfo()
			if transaction == nil || !found {
				return nil
			}
			after, marked, err := loadYgoStreamMark(txApp, ygoStructuralStream, room)
			if err != nil || !marked {
				return err
			}
			transaction.OnComplete(func(txErr error) error {
				if txErr == nil {
					ygoStructuralDocuments.put(room, after, doc, bytes)
				}
				return nil
			})
			return nil
		}
		writer.keep(txApp, doc)
		if !changed {
			return keep(bytes)
		}
		// A new stream's change is its whole document; any other's, what
		// changed (2026-10-21: the whole was encoded for every change and set
		// aside, a long page's every block encoded whole while the change held
		// the one write connection).
		var update []byte
		if newStream {
			update = crdt.EncodeStateAsUpdateV1(doc, nil)
		} else {
			update = crdt.EncodeChangesSinceV1(doc, since)
		}
		if len(update) == 0 {
			return graphInputError(errorCodeGraphInvalid, "Reduce the content change size.")
		}
		if len(update) > maxYgoUpdateBytes {
			return graphInputError(errorCodeGraphChangeTooLarge, "This change is too large for one change (64 MB). Make it in parts.")
		}
		if err := appendYgoStructuralUpdate(txApp, room, update); err != nil {
			return err
		}
		if err := keep(bytes + len(update)); err != nil {
			return err
		}
		// The change is the page's once its document reaches the clocks the
		// change itself reaches: the whole document's state vector, kept with
		// every change, grew with each writer the page ever had.
		reached, err := crdt.EncodeStateVectorFromUpdate(update)
		if err != nil {
			return err
		}
		return publish(ctx, txApp, update, reached)
	})
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return fmt.Errorf("%s: %w", errorCodeYgoPersistence, err)
	}
	if recovered != nil {
		return graphStructuralRecoveryError(recovered)
	}
	return nil
}

func (p *pocketBaseYgoPersistence) setCompactionNotifier(notify func()) {
	p.compactionNotify = notify
}

func (p *pocketBaseYgoPersistence) notifyCompaction() {
	if p.compactionNotify != nil {
		p.compactionNotify()
	}
}

func (p *pocketBaseYgoPersistence) stripe(room string) *sync.Mutex {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(room))
	return &p.stripes[hasher.Sum32()%ygoPersistenceStripes]
}
