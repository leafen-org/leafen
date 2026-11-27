package app

import (
	"bytes"

	"github.com/reearth/ygo/crdt"
)

func attributedYgoUpdatesBytes(updates []attributedYgoUpdate) int {
	total := 0
	for _, update := range updates {
		total += len(update.update)
	}
	return total
}

func cloneAttributedYgoUpdates(updates []attributedYgoUpdate) []attributedYgoUpdate {
	cloned := make([]attributedYgoUpdate, len(updates))
	for index, update := range updates {
		cloned[index] = attributedYgoUpdate{
			update: bytes.Clone(update.update), actorID: update.actorID, connection: update.connection,
			textChanges: cloneYgoTextChanges(update.textChanges), at: update.at,
		}
	}
	return cloned
}

func cloneYgoTextChanges(changes []crdt.TextChange) []crdt.TextChange {
	return append([]crdt.TextChange(nil), changes...)
}

func mergeAttributedYgoUpdates(
	prior []attributedYgoUpdate,
	incoming []attributedYgoUpdate,
) []attributedYgoUpdate {
	merged := cloneAttributedYgoUpdates(prior)
	for _, update := range incoming {
		candidate := attributedYgoUpdate{
			update: bytes.Clone(update.update), actorID: update.actorID, connection: update.connection,
			textChanges: cloneYgoTextChanges(update.textChanges), at: update.at,
		}
		last := len(merged) - 1
		if last >= 0 && merged[last].actorID == candidate.actorID && merged[last].connection == candidate.connection {
			combined, err := crdt.MergeUpdatesV1(merged[last].update, candidate.update)
			if err == nil && len(combined) <= maxYgoUpdateBytes {
				merged[last].update = combined
				merged[last].textChanges = append(merged[last].textChanges, candidate.textChanges...)
				merged[last].at = laterYgoEditTime(merged[last].at, candidate.at)
				continue
			}
		}
		merged = append(merged, candidate)
	}
	return merged
}

func (owner *ygoPersistenceSettlementOwner) admit(
	room string,
	update []byte,
	cause error,
) bool {
	return owner.admitBatch(room, []attributedYgoUpdate{{update: update}}, cause)
}

func (owner *ygoPersistenceSettlementOwner) admitBatch(
	room string,
	updates []attributedYgoUpdate,
	cause error,
) bool {
	causeCode, supportedCause := ygoSettlementCauseCode(cause)
	bytes := attributedYgoUpdatesBytes(updates)
	if owner == nil || !supportedCause || room == "" || len(updates) == 0 ||
		bytes == 0 || bytes > maxYgoUpdateBytes {
		return false
	}
	owner.mu.Lock()
	if owner.closeRoom == nil {
		owner.mu.Unlock()
		return false
	}
	settlement, exists := owner.pendingRooms[room]
	firstFailure := !exists || settlement.state == collaborationStorageStatusRecovered
	if firstFailure && !owner.ensureRoomCapacityLocked(room) {
		firstOverflowFailure := owner.overflow == nil || owner.overflow.state == collaborationStorageStatusRecovered
		if firstOverflowFailure {
			generation := uint64(1)
			if owner.overflow != nil {
				generation = owner.overflow.generation + 1
			}
			owner.nextSequence++
			owner.overflow = &ygoPersistenceSettlement{
				diagnosticReference: newDiagnosticReference(),
				state:               collaborationStorageStatusError, generation: generation,
				sequence: owner.nextSequence,
			}
		}
		reference := owner.overflow.diagnosticReference
		generation := owner.overflow.generation
		owner.mu.Unlock()
		if firstOverflowFailure {
			logYgoSettlementDiagnostic(owner.app, reference, causeCode, cause)
		}
		owner.Start()
		owner.notify()
		go owner.isolate(room, generation)
		return true
	}
	if firstFailure {
		owner.nextSequence++
		settlement = &ygoPersistenceSettlement{
			diagnosticReference: newDiagnosticReference(),
			state:               collaborationStorageStatusError, generation: 1,
			sequence: owner.nextSequence,
		}
		if exists {
			settlement.generation = owner.pendingRooms[room].generation + 1
		}
		owner.pendingRooms[room] = settlement
	}
	owner.replacePendingUpdateBatchLocked(room, settlement, updates)
	if owner.pendingBytes > owner.peakPendingBytes {
		owner.peakPendingBytes = owner.pendingBytes
	}
	if len(owner.pendingRooms) > owner.peakPendingRooms {
		owner.peakPendingRooms = len(owner.pendingRooms)
	}
	requestIsolation := !settlement.isolationInFlight
	settlement.isolationInFlight = true
	reference := settlement.diagnosticReference
	owner.mu.Unlock()

	if firstFailure {
		logYgoSettlementDiagnostic(owner.app, reference, causeCode, cause)
	}
	owner.Start()
	owner.notify()
	if requestIsolation {
		go owner.isolate(room, settlement.generation)
	}
	return true
}

func (owner *ygoPersistenceSettlementOwner) replacePendingUpdateBatchLocked(
	room string,
	settlement *ygoPersistenceSettlement,
	updates []attributedYgoUpdate,
) {
	merged := mergeAttributedYgoUpdates(settlement.updates, updates)
	owner.pendingBytes -= attributedYgoUpdatesBytes(settlement.updates)
	mergedBytes := attributedYgoUpdatesBytes(merged)
	for owner.pendingBytes+mergedBytes > ygoSettlementMaxPendingBytes {
		oldestRoom := owner.oldestPendingRoomLocked(room)
		if oldestRoom == "" {
			break
		}
		oldest := owner.pendingRooms[oldestRoom]
		owner.pendingBytes -= attributedYgoUpdatesBytes(oldest.updates)
		oldest.updates = nil
	}
	if owner.pendingBytes+mergedBytes <= ygoSettlementMaxPendingBytes {
		settlement.updates = merged
		owner.pendingBytes += mergedBytes
		return
	}
	settlement.updates = nil
}
