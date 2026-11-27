package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Reading a page's history back: its frontier, the checkpoint at or before
// a version, the events after it and its versions, each as it was written
// (page_history_compression.go).

func pageHistorySnapshotFrontier(app core.App, pageID string) (int, error) {
	// The immutable maximum is the stable version frontier captured by list and snapshot cursors.
	var frontier int
	err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[version_cursor]]),0) FROM {{leafen_page_history_versions}}
		WHERE [[page_id]]={:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).Row(&frontier)
	return frontier, err
}

func loadPageHistoryCheckpoint(
	app core.App, pageID string, versionCursor int,
) (pageHistoryCheckpoint, error) {
	checkpoint := pageHistoryCheckpoint{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]],[[version_cursor]],[[page_json]],[[blocks_json]],
			[[structural_state]],[[collaboration_state]],[[databases_json]]
		FROM {{leafen_page_history_checkpoints}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]<={:versionCursor}
		ORDER BY [[version_cursor]] DESC LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "versionCursor": versionCursor}).One(&checkpoint)
	if err != nil {
		return checkpoint, err
	}
	return checkpoint, checkpoint.unpack()
}

// pageHistoryCheckpointPage is the checkpoint at or before a version read
// for its page alone: its version and its page's title, icon and values,
// without the blocks and documents a replay starts from (a version preview
// unpacked a 3,900-block page's 8 MB to read these, 2026-11-18).
func pageHistoryCheckpointPage(
	app core.App, pageID string, versionCursor int,
) (int, string, error) {
	checkpoint := pageHistoryCheckpoint{}
	err := app.DB().NewQuery(`
		SELECT [[version_cursor]],[[page_json]] FROM {{leafen_page_history_checkpoints}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]<={:versionCursor}
		ORDER BY [[version_cursor]] DESC LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "versionCursor": versionCursor}).One(&checkpoint)
	if err != nil {
		return 0, "", err
	}
	pageJSON, err := unpackPageHistoryText(checkpoint.PageJSON)
	return checkpoint.VersionCursor, pageJSON, err
}

// loadPageHistoryCheckpointParts is loadPageHistoryCheckpoint with the
// columns a replay of parts reads (page_history_replay.go): the others stay
// empty, neither read nor unpacked.
func loadPageHistoryCheckpointParts(
	app core.App, pageID string, versionCursor int, parts pageHistoryReplayParts,
) (pageHistoryCheckpoint, error) {
	var columns string
	switch parts {
	case pageHistoryReplayStructure:
		columns = "[[structural_state]],[[databases_json]]"
	case pageHistoryReplayText:
		columns = "[[collaboration_state]]"
	default:
		return loadPageHistoryCheckpoint(app, pageID, versionCursor)
	}
	checkpoint := pageHistoryCheckpoint{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]],[[version_cursor]],[[page_json]],` + columns + `
		FROM {{leafen_page_history_checkpoints}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]<={:versionCursor}
		ORDER BY [[version_cursor]] DESC LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "versionCursor": versionCursor}).One(&checkpoint)
	if err != nil {
		return checkpoint, err
	}
	return checkpoint, checkpoint.unpack()
}

// unpack is a checkpoint as it was written (page_history_compression.go).
func (checkpoint *pageHistoryCheckpoint) unpack() error {
	var err error
	for _, text := range []*string{&checkpoint.PageJSON, &checkpoint.BlocksJSON, &checkpoint.DatabasesJSON} {
		if *text, err = unpackPageHistoryText(*text); err != nil {
			return err
		}
	}
	for _, state := range []*[]byte{&checkpoint.StructuralState, &checkpoint.CollaborationState} {
		if *state, err = unpackPageHistory(*state); err != nil {
			return err
		}
	}
	return nil
}

func loadPageHistoryEvents(
	app core.App, pageID string, afterCursor, throughCursor int,
) ([]storedPageHistoryEvent, error) {
	return loadPageHistoryEventsUpTo(app, pageID, afterCursor, throughCursor, pageHistoryMaxReplayEvents)
}

// loadPageHistoryEventsUpTo is the events after one version through
// another, one more than limit at most.
func loadPageHistoryEventsUpTo(
	app core.App, pageID string, afterCursor, throughCursor, limit int,
) ([]storedPageHistoryEvent, error) {
	events := []storedPageHistoryEvent{}
	err := app.DB().NewQuery(`
		SELECT [[version_cursor]],[[kind]],[[payload]],[[created_at]]
		FROM {{leafen_page_history_events}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]>{:afterCursor}
			AND [[version_cursor]]<={:throughCursor}
		ORDER BY [[version_cursor]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"pageId": pageID, "afterCursor": afterCursor, "throughCursor": throughCursor,
		"limit": limit + 1,
	}).All(&events)
	if err != nil {
		return events, err
	}
	for index := range events {
		if events[index].Payload, err = unpackPageHistory(events[index].Payload); err != nil {
			return events, err
		}
	}
	return events, nil
}

func loadPageHistoryVersion(
	app core.App, pageID, versionID string,
) (storedPageHistoryVersion, error) {
	version := storedPageHistoryVersion{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[version_cursor]],[[actor_id]],[[source]],[[created_at]],
			[[connection_name]],[[connection_id]]
		FROM {{leafen_page_history_versions}} WHERE [[page_id]]={:pageId} AND [[id]]={:versionId}
	`).Bind(dbx.Params{"pageId": pageID, "versionId": versionID}).One(&version)
	return version, err
}

func pageHistoryVersionID(pageID string, versionCursor int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("page-history\x00%s\x00%d", pageID, versionCursor)))
	return hex.EncodeToString(digest[:16])
}

func pageHistoryVersionMissing(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
