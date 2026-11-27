package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A stream's updates are compacted into its stored state once they are
// ygoCompactionBatchSize and weigh an ygoCompactionStateShare of the state,
// or are ygoCompactionTurnUpdates or ygoCompactionByteBudget whatever the
// state: compacted every 64, a long page typed into was merged whole every
// few seconds, and fell behind (2026-10-15: 100 people typing for minutes).
// A large one is also compacted once left alone (ygo_compaction_schedule.go).
// A turn takes as many as either bound lets it.
const (
	ygoCompactionBatchSize   = 64
	ygoCompactionByteBudget  = 1 << 20
	ygoCompactionStateShare  = 8
	ygoCompactionTurnUpdates = 1024
	ygoMaximumPendingUpdates = 4096
)

// ygoStateBytesSQL is the size of a room's stored document, read without the
// document itself.
func ygoStateBytesSQL(stream ygoStreamTables) string {
	return `(SELECT length([[state]]) FROM {{` + stream.states + `}} WHERE [[room]] = {:room})`
}

var ygoCompactionDueParams = dbx.Params{
	"batchSize": ygoCompactionBatchSize, "byteBudget": ygoCompactionByteBudget,
	"stateShare": ygoCompactionStateShare, "turnUpdates": ygoCompactionTurnUpdates,
}

type ygoStreamTables struct {
	head           string
	states         string
	updates        string
	recoverUnowned bool
}

var (
	ygoCollaborationStream = ygoStreamTables{
		head: "leafen_ygo_documents", states: "leafen_ygo_document_states", updates: "leafen_ygo_updates",
	}
	ygoStructuralStream = ygoStreamTables{
		head: "leafen_ygo_structural_documents", states: "leafen_ygo_structural_document_states",
		updates:        "leafen_ygo_structural_updates",
		recoverUnowned: true,
	}
)

type storedYgoUpdate struct {
	Sequence int64  `db:"sequence"`
	Update   []byte `db:"update_blob"`
	Bytes    int    `db:"bytes"`
}

func appendYgoUpdate(app core.App, room string, update []byte) error {
	_, err := appendYgoUpdateIfChanged(app, room, update)
	return err
}

func appendYgoUpdateIfChanged(app core.App, room string, update []byte) (bool, error) {
	return appendYgoUpdateIfChangedContext(context.Background(), app, room, update)
}

func appendYgoUpdateIfChangedContext(ctx context.Context, app core.App, room string, update []byte) (bool, error) {
	// The collaboration adapter receives Ygo Doc.OnUpdate payloads after
	// integration, which carry only what the transaction added and deleted,
	// so an already-integrated delivery reaches this boundary empty. Raw
	// structural updates do not have that callback contract and never use
	// this no-op admission rule.
	return appendYgoStreamUpdateIfChanged(ctx, app, ygoCollaborationStream, room, update, true)
}

func appendYgoStructuralUpdate(app core.App, room string, update []byte) error {
	return appendYgoStreamUpdate(app, ygoStructuralStream, room, update)
}

func appendYgoStreamUpdate(app core.App, stream ygoStreamTables, room string, update []byte) error {
	_, err := appendYgoStreamUpdateIfChanged(context.Background(), app, stream, room, update, false)
	return err
}

func appendYgoStreamUpdateIfChanged(
	ctx context.Context,
	app core.App,
	stream ygoStreamTables,
	room string,
	update []byte,
	dropIntegrated bool,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if room == "" || len(update) == 0 || len(update) > maxYgoUpdateBytes {
		return false, errors.New(errorCodeYgoUpdateTooLarge)
	}
	probe := crdt.New()
	if err := crdt.ApplyUpdateV1(probe, update, nil); err != nil {
		return false, err
	}
	if dropIntegrated {
		changes, err := inspectYgoUpdateChanges(update)
		if err != nil {
			return false, err
		}
		if !changes.hasStructs && !changes.hasDeletes {
			return false, nil
		}
	}
	if err := ensureYgoStreamHeadContext(ctx, app, stream, room); err != nil {
		return false, err
	}
	if stream.recoverUnowned {
		if err := initializeStructuralAuthorityCutover(app, room); err != nil {
			return false, err
		}
	}
	var head struct {
		FormatVersion    int   `db:"format_version"`
		SnapshotSequence int64 `db:"snapshot_sequence"`
		NextSequence     int64 `db:"next_sequence"`
		PendingCount     int   `db:"pending_count"`
		PendingBytes     int   `db:"pending_bytes"`
	}
	if err := app.DB().NewQuery(`
		SELECT [[format_version]], [[snapshot_sequence]],
			[[next_sequence]], [[pending_count]], [[pending_bytes]]
		FROM {{` + stream.head + `}} WHERE [[room]] = {:room}
	`).WithContext(ctx).Bind(dbx.Params{"room": room}).One(&head); err != nil {
		return false, err
	}
	if head.FormatVersion != ygoDocumentFormatVersion || head.PendingCount < 0 || head.PendingBytes < 0 {
		return false, errors.New(errorCodeYgoFormatCutoverFailed)
	}
	if head.PendingCount >= ygoMaximumPendingUpdates {
		return false, errors.New(errorCodeYgoPendingBacklogFull)
	}
	now := time.Now().UTC().UnixMilli()
	columns := "[[room]], [[sequence]], [[update_blob]], [[bytes]], [[created]]"
	values := "{:room}, {:sequence}, {:update}, {:bytes}, {:created}"
	if stream.recoverUnowned {
		columns += ", [[authority_committed]]"
		values += ", 1"
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{` + stream.updates + `}} (` + columns + `)
		VALUES (` + values + `)
	`).Bind(dbx.Params{
		"room": room, "sequence": head.NextSequence, "update": update,
		"bytes": len(update), "created": now,
	}).WithContext(ctx).Execute(); err != nil {
		return false, err
	}
	params := ygoCompactionScheduleParams(dbx.Params{"room": room, "sequence": head.NextSequence, "bytes": len(update)}, now)
	result, err := app.DB().NewQuery(`
		UPDATE {{` + stream.head + `}}
		SET [[next_sequence]] = [[next_sequence]] + 1,
			[[pending_count]] = [[pending_count]] + 1,
			[[pending_bytes]] = [[pending_bytes]] + {:bytes},
			` + ygoCompactionScheduleSQL("[[pending_count]] + 1", "[[pending_bytes]] + {:bytes}", ygoStateBytesSQL(stream), "[[compaction_status]]") + `,
			[[updated]] = strftime('%Y-%m-%d %H:%M:%fZ')
		WHERE [[room]] = {:room} AND [[next_sequence]] = {:sequence}
	`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return false, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return false, err
	} else if affected != 1 {
		return false, errors.New(errorCodeYgoPersistence)
	}
	notifyYgoCompactionAfterCommit(app)
	return true, nil
}

func ensureYgoStreamHead(app core.App, stream ygoStreamTables, room string) error {
	return ensureYgoStreamHeadContext(context.Background(), app, stream, room)
}

func ensureYgoStreamHeadContext(ctx context.Context, app core.App, stream ygoStreamTables, room string) error {
	empty := crdt.EncodeStateAsUpdateV1(crdt.New(), nil)
	columns, values := "[[room]], [[format_version]], [[updated]]", "{:room}, {:formatVersion}, strftime('%Y-%m-%d %H:%M:%fZ')"
	params := dbx.Params{"room": room, "formatVersion": ygoDocumentFormatVersion}
	result, err := app.DB().NewQuery(`
		INSERT INTO {{` + stream.head + `}} (` + columns + `) VALUES (` + values + `)
		ON CONFLICT ([[room]]) DO NOTHING
	`).Bind(params).WithContext(ctx).Execute()
	if err != nil {
		return err
	}
	// A head that was there has its document; a new one starts empty.
	if added, err := result.RowsAffected(); err != nil || added == 0 {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{` + stream.states + `}} ([[room]], [[state]]) VALUES ({:room}, {:state})
		ON CONFLICT ([[room]]) DO NOTHING
	`).Bind(dbx.Params{"room": room, "state": empty}).WithContext(ctx).Execute()
	return err
}

func loadMaterializedYgoDocument(app core.App, room string) (storedYgoDocument, bool, error) {
	return loadMaterializedYgoStream(app, ygoCollaborationStream, room)
}

func loadMaterializedYgoStructuralDocument(app core.App, room string) (storedYgoDocument, bool, error) {
	return loadMaterializedYgoStream(app, ygoStructuralStream, room)
}

func loadMaterializedYgoStream(
	app core.App,
	stream ygoStreamTables,
	room string,
) (storedYgoDocument, bool, error) {
	stored, parts, found, err := loadYgoStreamParts(app, stream, room)
	if err != nil || !found {
		return stored, found, err
	}
	if len(parts) == 1 {
		return stored, true, validateYgoState(stored.State)
	}
	materialized, err := crdt.MergeUpdatesV1(parts...)
	if err != nil {
		return storedYgoDocument{}, false, err
	}
	if err := validateYgoState(materialized); err != nil {
		return storedYgoDocument{}, false, err
	}
	stored.State = materialized
	return stored, true, nil
}

// loadYgoDoc reads a page's text document as stored — its snapshot and the
// updates after it — into a new document, empty where there is none, and
// whether one is stored. Applying them is the check that they hold a
// document: checked on a document of its own and applied again, it was
// decoded twice (2026-10-13: adding a page under a parent with a thousand
// sub-pages; 2026-10-20: indexing a page being typed read its 750 KB twice a
// change).
func loadYgoDoc(app core.App, room string) (*crdt.Doc, bool, error) {
	doc, _, found, err := loadYgoStreamDoc(app, ygoCollaborationStream, room)
	return doc, found, err
}

// loadYgoStructuralDoc is loadYgoDoc for a page's structural document.
func loadYgoStructuralDoc(app core.App, room string) (*crdt.Doc, bool, error) {
	doc, _, found, err := loadYgoStreamDoc(app, ygoStructuralStream, room)
	return doc, found, err
}

// loadYgoStreamDoc is loadYgoDoc for either stream, with the stored state the
// document was made from.
func loadYgoStreamDoc(app core.App, stream ygoStreamTables, room string) (*crdt.Doc, []byte, bool, error) {
	doc := crdt.New()
	_, parts, found, err := loadYgoStreamParts(app, stream, room)
	if err != nil || !found {
		return doc, nil, found, err
	}
	state := parts[0]
	if len(parts) > 1 {
		if state, err = crdt.MergeUpdatesV1(parts...); err != nil {
			return nil, nil, false, err
		}
	}
	if len(state) > 0 {
		if err := crdt.ApplyUpdateV1(doc, state, nil); err != nil {
			return nil, nil, false, err
		}
	}
	return doc, state, true, nil
}

// loadYgoStreamParts reads a document's stored snapshot and the updates
// after it, checking they are the ones its head counts.
func loadYgoStreamParts(
	app core.App,
	stream ygoStreamTables,
	room string,
) (storedYgoDocument, [][]byte, bool, error) {
	stored, found, err := loadYgoStreamHead(app, stream, room)
	if err != nil || !found {
		return stored, nil, found, err
	}
	if stored.FormatVersion != ygoDocumentFormatVersion || stored.PendingCount < 0 ||
		stored.PendingCount > ygoMaximumPendingUpdates || stored.PendingBytes < 0 {
		return storedYgoDocument{}, nil, false, errors.New(errorCodeYgoFormatCutoverFailed)
	}
	updates := []storedYgoUpdate{}
	if err := app.DB().NewQuery(`
		SELECT [[sequence]], [[update_blob]], [[bytes]] FROM {{` + stream.updates + `}}
		WHERE [[room]] = {:room} AND [[sequence]] > {:snapshotSequence}
			AND [[sequence]] < {:nextSequence}
		ORDER BY [[sequence]]
	`).Bind(dbx.Params{
		"room": room, "snapshotSequence": stored.SnapshotSequence,
		"nextSequence": stored.NextSequence,
	}).All(&updates); err != nil {
		return storedYgoDocument{}, nil, false, err
	}
	parts := make([][]byte, 1, len(updates)+1)
	parts[0] = stored.State
	bytes := 0
	expectedSequence := stored.SnapshotSequence + 1
	for _, update := range updates {
		if update.Sequence != expectedSequence || update.Bytes != len(update.Update) || update.Bytes <= 0 {
			return storedYgoDocument{}, nil, false, errors.New(errorCodeYgoPersistence)
		}
		expectedSequence++
		bytes += update.Bytes
		parts = append(parts, update.Update)
	}
	if len(updates) != stored.PendingCount || bytes != stored.PendingBytes ||
		expectedSequence != stored.NextSequence {
		return storedYgoDocument{}, nil, false, errors.New(errorCodeYgoPersistence)
	}
	return stored, parts, true, nil
}

func loadYgoStreamHead(app core.App, stream ygoStreamTables, room string) (storedYgoDocument, bool, error) {
	var stored storedYgoDocument
	err := app.DB().NewQuery(`
		SELECT [[stored]].[[state]], [[stored]].[[revision]] AS [[state_revision]],
			[[format_version]], [[snapshot_sequence]], [[next_sequence]],
			[[pending_count]], [[pending_bytes]], [[compaction_status]]
		FROM {{` + stream.head + `}} JOIN {{` + stream.states + `}} AS [[stored]] USING ([[room]])
		WHERE [[room]] = {:room}
	`).Bind(dbx.Params{"room": room}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedYgoDocument{}, false, nil
	}
	return stored, err == nil, err
}

func validateYgoState(state []byte) error {
	document := crdt.New()
	return crdt.ApplyUpdateV1(document, state, nil)
}

// replaceYgoSnapshot stores a page's text as the server rewrote it, and an
// open room takes the rewrite once it is committed.
func replaceYgoSnapshot(app core.App, room string, state []byte) error {
	if err := replaceYgoStreamSnapshot(app, ygoCollaborationStream, room, state); err != nil {
		return err
	}
	publishStoredYgoUpdateAfterCommit(app, room, state)
	return nil
}

func replaceYgoStructuralSnapshot(app core.App, room string, state []byte) error {
	return replaceYgoStreamSnapshot(app, ygoStructuralStream, room, state)
}

func replaceYgoStreamSnapshot(app core.App, stream ygoStreamTables, room string, state []byte) error {
	if room == "" {
		return errors.New(errorCodeYgoPersistence)
	}
	if err := validateYgoState(state); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{` + stream.updates + `}} WHERE [[room]] = {:room}
	`).Bind(dbx.Params{"room": room}).Execute(); err != nil {
		return err
	}
	params := dbx.Params{"room": room, "formatVersion": ygoDocumentFormatVersion}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{` + stream.head + `}}
			([[room]], [[format_version]], [[snapshot_sequence]], [[next_sequence]],
			 [[pending_count]], [[pending_bytes]], [[compaction_status]], [[updated]])
		VALUES ({:room}, {:formatVersion}, 0, 1, 0, 0, 'idle', strftime('%Y-%m-%d %H:%M:%fZ'))
		ON CONFLICT ([[room]]) DO UPDATE SET
			[[format_version]] = excluded.[[format_version]],
			[[snapshot_sequence]] = 0, [[next_sequence]] = 1,
			[[pending_count]] = 0, [[pending_bytes]] = 0,
			[[compaction_status]] = 'idle',
			[[updated]] = excluded.[[updated]]
	`).Bind(params).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{` + stream.states + `}} ([[room]], [[state]]) VALUES ({:room}, {:state})
		ON CONFLICT ([[room]]) DO UPDATE SET [[state]] = excluded.[[state]], [[revision]] = [[revision]] + 1
	`).Bind(dbx.Params{"room": room, "state": state}).Execute()
	return err
}

func (p *pocketBaseYgoPersistence) compactRoomTurn(
	ctx context.Context,
	stream ygoStreamTables,
	room string,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	stripe := p.stripe(room)
	stripe.Lock()
	defer stripe.Unlock()
	if stream.recoverUnowned {
		var recovered *ygoStructuralRecovery
		if err := p.runOperationTransaction(ctx, func(txApp core.App) error {
			var err error
			recovered, err = recoverTrailingUnownedStructuralUpdate(txApp, room)
			return err
		}); err != nil {
			return false, err
		}
		if recovered != nil {
			logDiagnostic(
				p.app, 409, errorCodeGraphStructureRecovered, recovered.Reference,
				"ygo.compaction", "structural-recovery",
				graphStructureOrphanRecoveredCauseCode, nil, recovered.SourcePosition,
			)
			return false, nil
		}
	}
	// The state and its updates are merged before the write, which then
	// only swaps them: merged inside it, a long page's merge held the one
	// write connection (2026-10-15). The room's own writes wait for it
	// here; changes made to the page otherwise meanwhile either come after
	// the merged updates, which stay, or replace the state, and the swap
	// finds the state it merged gone.
	stored, found, err := loadYgoStreamHead(p.app, stream, room)
	if err != nil || !found {
		return false, err
	}
	updates, err := loadYgoCompactionBatch(p.app, stream, room, stored)
	if err != nil || len(updates) == 0 {
		return false, err
	}
	parts := make([][]byte, 1, len(updates)+1)
	parts[0] = stored.State
	bytes := 0
	for _, update := range updates {
		parts = append(parts, update.Update)
		bytes += update.Bytes
	}
	state, err := compactedYgoState(parts)
	if err != nil {
		return false, err
	}
	remaining := false
	// A page's document kept in memory stays good for the compacted parts,
	// which hold the same (ygo_structural_document_cache.go).
	var rekey func()
	err = p.runOperationTransaction(ctx, func(txApp core.App) error {
		cache := ygoDocumentCacheFor(stream)
		before, beforeMarked := int64(0), false
		if cache.holds(room) {
			var err error
			if before, beforeMarked, err = loadYgoStreamMark(txApp, stream, room); err != nil {
				return err
			}
		}
		lastSequence := updates[len(updates)-1].Sequence
		// A document replaced meanwhile has another revision.
		result, err := txApp.DB().NewQuery(`
			UPDATE {{` + stream.states + `}} SET [[state]] = {:state}, [[revision]] = [[revision]] + 1
			WHERE [[room]] = {:room} AND [[revision]] = {:priorRevision}
		`).Bind(dbx.Params{"state": state, "room": room, "priorRevision": stored.StateRevision}).Execute()
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return fmt.Errorf("%s: compaction compare-and-swap failed", errorCodeYgoPersistence)
		}
		params := ygoCompactionScheduleParams(dbx.Params{
			"stateBytes": len(state), "snapshotSequence": lastSequence, "count": len(updates), "bytes": bytes,
			"room": room, "priorSnapshotSequence": stored.SnapshotSequence,
		}, time.Now().UTC().UnixMilli())
		result, err = txApp.DB().NewQuery(`
			UPDATE {{` + stream.head + `}} SET
				[[snapshot_sequence]] = {:snapshotSequence}, [[pending_count]] = [[pending_count]] - {:count},
				[[pending_bytes]] = [[pending_bytes]] - {:bytes},
				` + ygoCompactionScheduleSQL(
			"[[pending_count]] - {:count}", "[[pending_bytes]] - {:bytes}", "{:stateBytes}", "'idle'") + `,
				[[updated]] = strftime('%Y-%m-%d %H:%M:%fZ')
			WHERE [[room]] = {:room} AND [[snapshot_sequence]] = {:priorSnapshotSequence}
				AND [[next_sequence]] > {:snapshotSequence}
		`).Bind(params).Execute()
		if err != nil {
			return err
		}
		affected, err = result.RowsAffected()
		if err != nil || affected != 1 {
			return fmt.Errorf("%s: compaction compare-and-swap failed", errorCodeYgoPersistence)
		}
		result, err = txApp.DB().NewQuery(`
			DELETE FROM {{` + stream.updates + `}}
			WHERE [[room]] = {:room} AND [[sequence]] > {:priorSnapshotSequence}
				AND [[sequence]] <= {:snapshotSequence}
		`).Bind(dbx.Params{
			"room": room, "priorSnapshotSequence": stored.SnapshotSequence,
			"snapshotSequence": lastSequence,
		}).Execute()
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil || deleted != int64(len(updates)) {
			return fmt.Errorf("%s: compaction delete mismatch", errorCodeYgoPersistence)
		}
		if beforeMarked {
			after, afterMarked, err := loadYgoStreamMark(txApp, stream, room)
			if err != nil {
				return err
			}
			if afterMarked {
				delta := len(state) - len(stored.State) - bytes
				rekey = func() { cache.rekey(room, before, after, delta) }
			}
		}
		var status string
		if err := txApp.DB().NewQuery(`SELECT [[compaction_status]] FROM {{` + stream.head + `}} WHERE [[room]] = {:room}`).
			Bind(dbx.Params{"room": room}).Row(&status); err != nil {
			return err
		}
		remaining = status == "pending"
		return nil
	})
	if err == nil && rekey != nil {
		rekey()
	}
	return remaining, err
}

func loadYgoCompactionBatch(
	app core.App,
	stream ygoStreamTables,
	room string,
	stored storedYgoDocument,
) ([]storedYgoUpdate, error) {
	rows := []storedYgoUpdate{}
	if err := app.DB().NewQuery(`
		SELECT [[sequence]], [[update_blob]], [[bytes]] FROM {{` + stream.updates + `}}
		WHERE [[room]] = {:room} AND [[sequence]] > {:snapshotSequence}
			AND [[sequence]] < {:nextSequence}
		ORDER BY [[sequence]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"room": room, "snapshotSequence": stored.SnapshotSequence,
		"nextSequence": stored.NextSequence, "limit": ygoCompactionTurnUpdates,
	}).All(&rows); err != nil {
		return nil, err
	}
	bytes := 0
	limit := len(rows)
	for index, row := range rows {
		if row.Sequence != stored.SnapshotSequence+int64(index)+1 ||
			row.Bytes != len(row.Update) || row.Bytes <= 0 {
			return nil, errors.New(errorCodeYgoPersistence)
		}
		if index > 0 && bytes+row.Bytes > ygoCompactionByteBudget {
			limit = index
			break
		}
		bytes += row.Bytes
	}
	return rows[:limit], nil
}
