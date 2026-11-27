package app

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const graphStructureOrphanRecoveredCauseCode = "GRAPH_STRUCTURE_ORPHAN_RECOVERED"

type ygoStructuralRecoveryHead struct {
	SnapshotSequence         int64 `db:"snapshot_sequence"`
	NextSequence             int64 `db:"next_sequence"`
	PendingCount             int   `db:"pending_count"`
	PendingBytes             int   `db:"pending_bytes"`
	AuthorityCutoverSequence int64 `db:"authority_cutover_sequence"`
}

type unownedStructuralUpdate struct {
	Sequence int64  `db:"sequence"`
	Update   []byte `db:"update_blob"`
	Bytes    int    `db:"bytes"`
	Created  int64  `db:"created"`
}

type ygoStructuralRecovery struct {
	Reference      string
	Sequence       int64
	SourcePosition string
}

func initializeStructuralAuthorityCutover(app core.App, room string) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_ygo_structural_documents}}
		SET [[authority_cutover_sequence]] = [[next_sequence]]
		WHERE [[room]] = {:room} AND [[authority_cutover_sequence]] = 0
	`).Bind(dbx.Params{"room": room}).Execute()
	return err
}

func recoverTrailingUnownedStructuralUpdate(
	app core.App,
	room string,
) (*ygoStructuralRecovery, error) {
	if err := initializeStructuralAuthorityCutover(app, room); err != nil {
		return nil, err
	}
	head := ygoStructuralRecoveryHead{}
	if err := app.DB().NewQuery(`
		SELECT [[snapshot_sequence]], [[next_sequence]], [[pending_count]],
			[[pending_bytes]], [[authority_cutover_sequence]]
		FROM {{leafen_ygo_structural_documents}} WHERE [[room]] = {:room}
	`).Bind(dbx.Params{"room": room}).One(&head); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	updates := []unownedStructuralUpdate{}
	if err := app.DB().NewQuery(`
		SELECT [[sequence]], [[update_blob]], [[bytes]], [[created]]
		FROM {{leafen_ygo_structural_updates}}
		WHERE [[room]] = {:room} AND [[sequence]] >= {:cutover}
			AND [[authority_committed]] = 0
		ORDER BY [[sequence]] LIMIT 2
	`).Bind(dbx.Params{
		"room": room, "cutover": head.AuthorityCutoverSequence,
	}).All(&updates); err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return nil, nil
	}
	if len(updates) != 1 {
		return nil, graphStructureAuthorityError()
	}
	update := updates[0]
	if update.Sequence != head.NextSequence-1 ||
		update.Sequence <= head.SnapshotSequence ||
		update.Bytes <= 0 || update.Bytes != len(update.Update) ||
		head.PendingCount < 1 || head.PendingBytes < update.Bytes {
		return nil, graphStructureAuthorityError()
	}

	reference := newDiagnosticReference()
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_ygo_structural_quarantine}}
			([[reference]], [[room]], [[sequence]], [[update_blob]], [[bytes]],
			 [[reason_code]], [[created]], [[quarantined_at]])
		VALUES
			({:reference}, {:room}, {:sequence}, {:update}, {:bytes},
			 {:reasonCode}, {:created}, {:quarantinedAt})
	`).Bind(dbx.Params{
		"reference": reference, "room": room, "sequence": update.Sequence,
		"update": update.Update, "bytes": update.Bytes,
		"reasonCode": graphStructureOrphanRecoveredCauseCode,
		"created":    update.Created, "quarantinedAt": now,
	}).Execute(); err != nil {
		return nil, err
	}
	deleted, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_ygo_structural_updates}}
		WHERE [[room]] = {:room} AND [[sequence]] = {:sequence}
			AND [[authority_committed]] = 0
	`).Bind(dbx.Params{"room": room, "sequence": update.Sequence}).Execute()
	if err != nil {
		return nil, err
	}
	deletedCount, err := deleted.RowsAffected()
	if err != nil || deletedCount != 1 {
		return nil, errors.New(errorCodeYgoPersistence)
	}

	pendingCount := head.PendingCount - 1
	pendingBytes := head.PendingBytes - update.Bytes
	status := "idle"
	if pendingCount >= ygoCompactionBatchSize || pendingBytes >= ygoCompactionByteBudget {
		status = "pending"
	}
	updated, err := app.DB().NewQuery(`
		UPDATE {{leafen_ygo_structural_documents}}
		SET [[next_sequence]] = {:nextSequence}, [[pending_count]] = {:pendingCount},
			[[pending_bytes]] = {:pendingBytes}, [[compaction_status]] = {:status},
			[[next_attempt_at]] = CASE WHEN {:status} = 'idle' THEN 0 ELSE [[next_attempt_at]] END,
			[[updated]] = strftime('%Y-%m-%d %H:%M:%fZ')
		WHERE [[room]] = {:room} AND [[next_sequence]] = {:expectedNextSequence}
			AND [[pending_count]] = {:expectedPendingCount}
			AND [[pending_bytes]] = {:expectedPendingBytes}
	`).Bind(dbx.Params{
		"nextSequence": update.Sequence, "pendingCount": pendingCount,
		"pendingBytes": pendingBytes, "status": status, "room": room,
		"expectedNextSequence": head.NextSequence,
		"expectedPendingCount": head.PendingCount,
		"expectedPendingBytes": head.PendingBytes,
	}).Execute()
	if err != nil {
		return nil, err
	}
	updatedCount, err := updated.RowsAffected()
	if err != nil || updatedCount != 1 {
		return nil, errors.New(errorCodeYgoPersistence)
	}
	return &ygoStructuralRecovery{
		Reference: reference, Sequence: update.Sequence,
		SourcePosition: diagnosticSourcePosition(0),
	}, nil
}

func graphStructuralRecoveryError(recovery *ygoStructuralRecovery) error {
	return &graphRequestError{
		status: http.StatusConflict, code: errorCodeGraphStructureRecovered,
		message:   "An incomplete content structure record was quarantined. Retry the same change.",
		reference: recovery.Reference, operation: "content_graph",
		stage: "structural-recovery", causeCode: graphStructureOrphanRecoveredCauseCode,
		sourcePosition: recovery.SourcePosition,
	}
}
