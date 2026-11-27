package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a run again takes from its source (decision "설계 우선 Notion 이사"
// 3절 원칙 4: only what changed during the move). The ledger keeps each row
// and each block as the source gave it to the run that last took it; a run
// again takes a row's cells and a block's content where the source's now
// differs from that, whatever its times say: a tool's bundle, read through
// Leafen's own API, gives every row and block the time it was put in, not
// when the tool changed it (seen 2026-11-15: the next bundle's changed row
// kept its old value, and taken by its time it would have taken every row,
// a person's changes and all), and Notion gives its times to the minute.
// Where the ledger keeps nothing yet (a run before it kept them), the
// source's time decides as before (notionMigrationChangedSince).

// notionMigrationLedgerBlockSource keeps a block's source, as a digest, by
// its source ID beside the block's own entry.
const notionMigrationLedgerBlockSource = "block_source"

// notionMigrationRowChange is what a run again takes of a row made before.
type notionMigrationRowChange struct {
	Changed bool
	// The cells the source changed since, by property source; nil for all.
	Cells map[string]bool
	// Of those, the ones the source emptied, as the run that last took them
	// was given them (notion_migration_rerun_emptied.go).
	Emptied map[string]notionDatabaseRowValuePlan
}

// notionMigrationRowChanged says which of a row's cells the source changed
// since the run that last took it; only a run again asks. The ledger keeps
// the row as staged, and both it and the source's now are taken as the run's
// plan takes a row (run, nil for none): a formula or rollup the plan preserves
// is a cell of the text Notion shows (keepRowShownAsText).
func notionMigrationRowChanged(
	app core.App, payload notionImportJobPayload, run *notionMigrationRunPlan, source storedNotionSource,
) (notionMigrationRowChange, error) {
	if payload.MigrationRerunSince <= 0 {
		return notionMigrationRowChange{}, nil
	}
	kept, err := notionMigrationLedgerState(app, payload.MigrationID, source.SourceID, "row")
	if err != nil || kept == "" {
		return notionMigrationRowChange{Changed: notionMigrationChangedSince(payload, notionMigrationRowEdited(source))}, err
	}
	before, now := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	if json.Unmarshal([]byte(kept), &before) != nil || json.Unmarshal([]byte(source.Text), &now) != nil {
		return notionMigrationRowChange{Changed: notionMigrationChangedSince(payload, notionMigrationRowEdited(source))}, nil
	}
	run.keepRowShownAsText(before)
	run.keepRowShownAsText(now)
	cells, emptied := map[string]bool{}, map[string]notionDatabaseRowValuePlan{}
	for property, value := range now {
		if !notionImportRowSpecialKey(property) && !bytes.Equal(before[property], value) {
			cells[property] = true
		}
	}
	// A cell the source emptied since is one it gives no value of now
	// (notionImportRowValue): it changed too, and is emptied as Notion did.
	for property, value := range before {
		plan := notionDatabaseRowValuePlan{}
		if _, given := now[property]; given || notionImportRowSpecialKey(property) || json.Unmarshal(value, &plan) != nil {
			continue
		}
		cells[property], emptied[property] = true, plan
	}
	return notionMigrationRowChange{Changed: len(cells) > 0, Cells: cells, Emptied: emptied}, nil
}

// notionMigrationBlockChanged says whether the source changed a block since
// the run that last took it; only a run again asks.
func notionMigrationBlockChanged(app core.App, payload notionImportJobPayload, sourceID string, plan notionParagraphSourcePlan) (bool, error) {
	if payload.MigrationRerunSince <= 0 {
		return false, nil
	}
	kept, err := notionMigrationLedgerState(app, payload.MigrationID, sourceID, notionMigrationLedgerBlockSource)
	if err != nil || kept == "" {
		return notionMigrationChangedSince(payload, plan.EditedAt), err
	}
	digest, err := notionMigrationBlockSourceDigest(plan)
	return err == nil && digest != kept, err
}

// notionMigrationBlockSourceDigest is a block's source as a run took it,
// without the time it was changed.
func notionMigrationBlockSourceDigest(plan notionParagraphSourcePlan) (string, error) {
	plan.EditedAt = ""
	encoded, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// keepNotionMigrationBlockSource keeps in the ledger the block's source as
// this run took it, when the run staged it.
func keepNotionMigrationBlockSource(app core.App, job storedJob, migrationID, sourceID, nativeID string) error {
	source, found, err := notionImportStagedSource(app, job.ID, sourceID)
	if err != nil || !found {
		return err
	}
	return keepNotionMigrationBlockSourceText(app, migrationID, sourceID, nativeID, source.Text)
}

// keepNotionMigrationBlockSourceText keeps a block's staged source as a run
// takes it. A migration's run keeps it as it makes or changes the block, not
// only at its end, so a run after one that stopped before its end tells what
// the source changed since by the source, not by its time (2026-11-16: the
// run again after a stopped one wrote again the blocks the stopped one made).
func keepNotionMigrationBlockSourceText(app core.App, migrationID, sourceID, nativeID, text string) error {
	if migrationID == "" {
		return nil
	}
	plan, err := decodeNotionParagraphSourceText(text)
	if err != nil {
		return nil
	}
	digest, err := notionMigrationBlockSourceDigest(plan)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_migration_ledger}}
			([[migration_id]],[[workspace_id]],[[source_id]],[[kind]],[[native_id]],[[page_id]],[[state]])
		SELECT [[id]], [[workspace_id]], {:sourceId}, {:kind}, {:nativeId}, '', {:digest}
		FROM {{leafen_notion_migrations}} WHERE [[id]]={:migrationId}
		ON CONFLICT ([[migration_id]],[[source_id]],[[kind]]) DO UPDATE SET
			[[native_id]]=excluded.[[native_id]], [[state]]=excluded.[[state]]
	`).Bind(dbx.Params{
		"migrationId": migrationID, "sourceId": sourceID, "kind": notionMigrationLedgerBlockSource,
		"nativeId": nativeID, "digest": digest,
	}).Execute()
	return err
}

// notionMigrationLedgerState is what the ledger keeps of a source object of
// a kind, "" when nothing.
func notionMigrationLedgerState(app core.App, migrationID, sourceID, kind string) (string, error) {
	state := ""
	err := app.DB().NewQuery(`
		SELECT [[state]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]={:kind}
	`).Bind(dbx.Params{"migrationId": migrationID, "sourceId": sourceID, "kind": kind}).Row(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return state, err
}
