package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	errorCodeYgoCorruptUpdateRecovered = "LEAFEN_YGO_CORRUPT_UPDATE_RECOVERED"
	ygoCorruptUpdateRecoveredCauseCode = "YGO_CORRUPT_UPDATE_RECOVERED"
)

type corruptYgoUpdate struct {
	Sequence int64  `db:"sequence"`
	Update   []byte `db:"update_blob"`
	Bytes    int    `db:"bytes"`
	Created  int64  `db:"created"`
}

type ygoCorruptUpdateRecovery struct {
	Reference      string
	Sequence       int64
	Document       storedYgoDocument
	SourcePosition string
}

func recoverTrailingCorruptYgoUpdate(
	app core.App,
	room string,
) (*ygoCorruptUpdateRecovery, error) {
	stored, found, err := loadYgoStreamHead(app, ygoCollaborationStream, room)
	if err != nil || !found {
		return nil, err
	}
	if stored.FormatVersion != ygoDocumentFormatVersion || stored.PendingCount < 1 ||
		stored.PendingCount > ygoMaximumPendingUpdates || stored.PendingBytes < 1 ||
		stored.NextSequence <= stored.SnapshotSequence+1 {
		return nil, nil
	}
	if err := validateYgoState(stored.State); err != nil {
		return nil, nil
	}

	updates := []corruptYgoUpdate{}
	if err := app.DB().NewQuery(`
		SELECT [[sequence]], [[update_blob]], [[bytes]], [[created]]
		FROM {{leafen_ygo_updates}}
		WHERE [[room]] = {:room} AND [[sequence]] > {:snapshotSequence}
			AND [[sequence]] < {:nextSequence}
		ORDER BY [[sequence]]
	`).Bind(dbx.Params{
		"room": room, "snapshotSequence": stored.SnapshotSequence,
		"nextSequence": stored.NextSequence,
	}).All(&updates); err != nil {
		return nil, err
	}
	if len(updates) != stored.PendingCount {
		return nil, nil
	}
	parts := make([][]byte, 1, len(updates))
	parts[0] = stored.State
	expectedSequence := stored.SnapshotSequence + 1
	totalBytes := 0
	invalidIndex := -1
	for index, update := range updates {
		if update.Sequence != expectedSequence || update.Bytes <= 0 ||
			update.Bytes != len(update.Update) {
			return nil, nil
		}
		expectedSequence++
		totalBytes += update.Bytes
		if err := crdt.ApplyUpdateV1(crdt.New(), update.Update, nil); err != nil {
			if invalidIndex >= 0 {
				return nil, nil
			}
			invalidIndex = index
			continue
		}
		if index < len(updates)-1 {
			parts = append(parts, update.Update)
		}
	}
	if expectedSequence != stored.NextSequence || totalBytes != stored.PendingBytes ||
		invalidIndex != len(updates)-1 {
		return nil, nil
	}

	materialized := stored.State
	if len(parts) > 1 {
		materialized, err = crdt.MergeUpdatesV1(parts...)
		if err != nil {
			return nil, nil
		}
	}
	if err := validateYgoState(materialized); err != nil {
		return nil, nil
	}
	candidate := updates[len(updates)-1]
	reference := newDiagnosticReference()
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_ygo_update_quarantine}}
			([[reference]], [[room]], [[sequence]], [[update_blob]], [[bytes]],
			 [[reason_code]], [[created]], [[quarantined_at]])
		VALUES ({:reference}, {:room}, {:sequence}, {:update}, {:bytes},
			{:reasonCode}, {:created}, {:quarantinedAt})
	`).Bind(dbx.Params{
		"reference": reference, "room": room, "sequence": candidate.Sequence,
		"update": candidate.Update, "bytes": candidate.Bytes,
		"reasonCode": ygoCorruptUpdateRecoveredCauseCode,
		"created":    candidate.Created, "quarantinedAt": now,
	}).Execute(); err != nil {
		return nil, err
	}
	deleted, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_ygo_updates}}
		WHERE [[room]] = {:room} AND [[sequence]] = {:sequence}
	`).Bind(dbx.Params{"room": room, "sequence": candidate.Sequence}).Execute()
	if err != nil {
		return nil, err
	}
	deletedCount, err := deleted.RowsAffected()
	if err != nil || deletedCount != 1 {
		return nil, errors.New(errorCodeYgoPersistence)
	}

	pendingCount := stored.PendingCount - 1
	pendingBytes := stored.PendingBytes - candidate.Bytes
	status := "idle"
	if pendingCount >= ygoCompactionBatchSize || pendingBytes >= ygoCompactionByteBudget {
		status = "pending"
	}
	updated, err := app.DB().NewQuery(`
		UPDATE {{leafen_ygo_documents}}
		SET [[next_sequence]] = {:nextSequence}, [[pending_count]] = {:pendingCount},
			[[pending_bytes]] = {:pendingBytes}, [[compaction_status]] = {:status},
			[[next_attempt_at]] = CASE WHEN {:status} = 'idle' THEN 0 ELSE [[next_attempt_at]] END,
			[[updated]] = strftime('%Y-%m-%d %H:%M:%fZ')
		WHERE [[room]] = {:room} AND [[snapshot_sequence]] = {:snapshotSequence}
			AND [[next_sequence]] = {:expectedNextSequence}
			AND [[pending_count]] = {:expectedPendingCount}
			AND [[pending_bytes]] = {:expectedPendingBytes}
	`).Bind(dbx.Params{
		"nextSequence": candidate.Sequence, "pendingCount": pendingCount,
		"pendingBytes": pendingBytes, "status": status, "room": room,
		"snapshotSequence":     stored.SnapshotSequence,
		"expectedNextSequence": stored.NextSequence,
		"expectedPendingCount": stored.PendingCount,
		"expectedPendingBytes": stored.PendingBytes,
	}).Execute()
	if err != nil {
		return nil, err
	}
	updatedCount, err := updated.RowsAffected()
	if err != nil || updatedCount != 1 {
		return nil, errors.New(errorCodeYgoPersistence)
	}
	stored.State = materialized
	stored.NextSequence = candidate.Sequence
	stored.PendingCount = pendingCount
	stored.PendingBytes = pendingBytes
	stored.CompactionStatus = status
	return &ygoCorruptUpdateRecovery{
		Reference: reference, Sequence: candidate.Sequence, Document: stored,
		SourcePosition: diagnosticSourcePosition(0),
	}, nil
}

func logYgoCorruptUpdateRecovery(app core.App, recovery *ygoCorruptUpdateRecovery) {
	if recovery == nil {
		return
	}
	logDiagnostic(
		app, http.StatusOK, errorCodeYgoCorruptUpdateRecovered, recovery.Reference,
		"ygo.persistence.load", "corrupt-update-recovery",
		ygoCorruptUpdateRecoveredCauseCode, nil, recovery.SourcePosition,
	)
}
