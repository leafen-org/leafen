package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

var errYgoMaterializedBatchLimit = errors.New("ygo materialized batch exceeds its declared bound")

type ygoMaterializedBatchMetadata struct {
	Room             string `db:"room"`
	FormatVersion    int    `db:"format_version"`
	SnapshotSequence int64  `db:"snapshot_sequence"`
	NextSequence     int64  `db:"next_sequence"`
	PendingCount     int    `db:"pending_count"`
	PendingBytes     int    `db:"pending_bytes"`
	CompactionStatus string `db:"compaction_status"`
	StateBytes       int    `db:"state_bytes"`
}

type ygoMaterializedBatchState struct {
	Room  string `db:"room"`
	State []byte `db:"state"`
}

type ygoMaterializedBatchUpdate struct {
	Room     string `db:"room"`
	Sequence int64  `db:"sequence"`
	Update   []byte `db:"update_blob"`
	Bytes    int    `db:"bytes"`
}

// loadMaterializedYgoDocumentsBounded owns batch materialization for consumers
// that already have a bounded room window. Metadata is read before BLOBs so an
// oversized batch is refused without allocating its authoritative state.
func loadMaterializedYgoDocumentsBounded(
	app core.App, roomIDs []string, roomLimit, byteBudget int,
) (map[string]storedYgoDocument, error) {
	rooms := uniqueSortedYgoBatchRooms(roomIDs)
	if roomLimit < 1 || byteBudget < 1 || len(rooms) > roomLimit {
		return nil, errYgoMaterializedBatchLimit
	}
	result := make(map[string]storedYgoDocument, len(rooms))
	if len(rooms) == 0 {
		return result, nil
	}
	params, placeholders := ygoBatchRoomParams(rooms)
	metadataRows := []ygoMaterializedBatchMetadata{}
	if err := app.DB().NewQuery(`
		SELECT [[room]],[[format_version]],[[snapshot_sequence]],[[next_sequence]],
			[[pending_count]],[[pending_bytes]],[[compaction_status]],length([[stored]].[[state]]) AS [[state_bytes]]
		FROM {{leafen_ygo_documents}} JOIN {{leafen_ygo_document_states}} AS [[stored]] USING ([[room]])
		WHERE [[room]] IN (` + placeholders + `)
		ORDER BY [[room]]
	`).Bind(params).All(&metadataRows); err != nil {
		return nil, err
	}
	metadata := make(map[string]ygoMaterializedBatchMetadata, len(metadataRows))
	totalBytes := 0
	totalPending := 0
	for _, row := range metadataRows {
		if row.Room == "" || row.FormatVersion != ygoDocumentFormatVersion ||
			row.SnapshotSequence < 0 || row.NextSequence < 1 || row.PendingCount < 0 ||
			row.PendingCount > ygoMaximumPendingUpdates || row.PendingBytes < 0 || row.StateBytes < 0 ||
			row.NextSequence-row.SnapshotSequence-1 != int64(row.PendingCount) {
			return nil, errors.New(errorCodeYgoPersistence)
		}
		if _, duplicate := metadata[row.Room]; duplicate {
			return nil, errors.New(errorCodeYgoPersistence)
		}
		if row.StateBytes > byteBudget-totalBytes || row.PendingBytes > byteBudget-totalBytes-row.StateBytes {
			return nil, errYgoMaterializedBatchLimit
		}
		totalBytes += row.StateBytes + row.PendingBytes
		totalPending += row.PendingCount
		metadata[row.Room] = row
	}
	stateRows := []ygoMaterializedBatchState{}
	if err := app.DB().NewQuery(`
		SELECT [[room]],[[stored]].[[state]]
		FROM {{leafen_ygo_documents}} JOIN {{leafen_ygo_document_states}} AS [[stored]] USING ([[room]])
		WHERE [[room]] IN (` + placeholders + `) ORDER BY [[room]]
	`).Bind(params).All(&stateRows); err != nil {
		return nil, err
	}
	for _, row := range stateRows {
		meta, ok := metadata[row.Room]
		if !ok || len(row.State) != meta.StateBytes {
			return nil, errors.New(errorCodeYgoPersistence)
		}
		result[row.Room] = storedYgoDocument{
			State:         row.State,
			FormatVersion: meta.FormatVersion, SnapshotSequence: meta.SnapshotSequence,
			NextSequence: meta.NextSequence, PendingCount: meta.PendingCount,
			PendingBytes: meta.PendingBytes, CompactionStatus: meta.CompactionStatus,
		}
	}
	if len(result) != len(metadata) {
		return nil, errors.New(errorCodeYgoPersistence)
	}
	if totalPending > 0 {
		if err := mergeYgoMaterializedBatchUpdates(app, placeholders, params, metadata, result); err != nil {
			return nil, err
		}
	}
	materializedBytes := 0
	for room, document := range result {
		if metadata[room].PendingCount == 0 {
			if err := validateYgoState(document.State); err != nil {
				return nil, err
			}
		}
		if len(document.State) > byteBudget-materializedBytes {
			return nil, errYgoMaterializedBatchLimit
		}
		materializedBytes += len(document.State)
	}
	return result, nil
}

func mergeYgoMaterializedBatchUpdates(
	app core.App,
	placeholders string,
	params dbx.Params,
	metadata map[string]ygoMaterializedBatchMetadata,
	documents map[string]storedYgoDocument,
) error {
	rows, err := app.DB().NewQuery(`
		SELECT [[update]].[[room]],[[update]].[[sequence]],[[update]].[[update_blob]],[[update]].[[bytes]]
		FROM {{leafen_ygo_updates}} [[update]]
		JOIN {{leafen_ygo_documents}} [[document]] ON [[document]].[[room]]=[[update]].[[room]]
		WHERE [[update]].[[room]] IN (` + placeholders + `)
			AND [[update]].[[sequence]]>[[document]].[[snapshot_sequence]]
			AND [[update]].[[sequence]]<[[document]].[[next_sequence]]
		ORDER BY [[update]].[[room]],[[update]].[[sequence]]
	`).Bind(params).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	processed := make(map[string]struct{}, len(metadata))
	currentRoom := ""
	expectedSequence := int64(0)
	updateCount, updateBytes := 0, 0
	parts := [][]byte(nil)
	finishRoom := func() error {
		if currentRoom == "" {
			return nil
		}
		meta := metadata[currentRoom]
		if updateCount != meta.PendingCount || updateBytes != meta.PendingBytes ||
			expectedSequence != meta.NextSequence {
			return errors.New(errorCodeYgoPersistence)
		}
		materialized, err := crdt.MergeUpdatesV1(parts...)
		if err != nil {
			return err
		}
		if err := validateYgoState(materialized); err != nil {
			return err
		}
		document := documents[currentRoom]
		document.State = materialized
		documents[currentRoom] = document
		processed[currentRoom] = struct{}{}
		return nil
	}
	for rows.Next() {
		row := ygoMaterializedBatchUpdate{}
		if err := rows.ScanStruct(&row); err != nil {
			return err
		}
		meta, ok := metadata[row.Room]
		if !ok || meta.PendingCount == 0 {
			return errors.New(errorCodeYgoPersistence)
		}
		if row.Room != currentRoom {
			if err := finishRoom(); err != nil {
				return err
			}
			currentRoom = row.Room
			expectedSequence = meta.SnapshotSequence + 1
			updateCount, updateBytes = 0, 0
			parts = [][]byte{documents[row.Room].State}
		}
		if row.Sequence != expectedSequence || row.Bytes <= 0 || row.Bytes != len(row.Update) {
			return errors.New(errorCodeYgoPersistence)
		}
		expectedSequence++
		updateCount++
		updateBytes += row.Bytes
		parts = append(parts, row.Update)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := finishRoom(); err != nil {
		return err
	}
	for room, meta := range metadata {
		_, found := processed[room]
		if (meta.PendingCount > 0) != found {
			return errors.New(errorCodeYgoPersistence)
		}
	}
	return nil
}

func uniqueSortedYgoBatchRooms(roomIDs []string) []string {
	unique := make(map[string]struct{}, len(roomIDs))
	for _, room := range roomIDs {
		if room != "" {
			unique[room] = struct{}{}
		}
	}
	rooms := make([]string, 0, len(unique))
	for room := range unique {
		rooms = append(rooms, room)
	}
	sort.Strings(rooms)
	return rooms
}

func ygoBatchRoomParams(roomIDs []string) (dbx.Params, string) {
	params := dbx.Params{}
	placeholders := make([]string, 0, len(roomIDs))
	for index, room := range roomIDs {
		key := fmt.Sprintf("room%d", index)
		params[key] = room
		placeholders = append(placeholders, "{:"+key+"}")
	}
	return params, strings.Join(placeholders, ",")
}
