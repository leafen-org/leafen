package app

import (
	"bytes"
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// notionSyncedBlockUpdateTaken is a synced block update's content without a
// synced_from that names what the block already syncs from, so the block keeps
// it: Notion's API answers such an update 200 and leaves the block as it was —
// an original sent null, a copy sent its original's ID written exactly as its
// read shows it, with or without its type (seen 2026-11-12 and 2026-11-15; the
// same ID without dashes or in capitals is refused). Any other synced_from is
// still refused as a change (normalizeNotionSyncedBlockMutation).
func notionSyncedBlockUpdateTaken(
	app core.App,
	authorization notionBlockAuthorization,
	raw json.RawMessage,
) json.RawMessage {
	if authorization.Block.Type != graphBlockTypeSyncedBlock {
		return raw
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &fields) != nil {
		return raw
	}
	given, named := fields["synced_from"]
	if !named || !notionSyncedFromIsCurrent(app, authorization, given) {
		return raw
	}
	delete(fields, "synced_from")
	encoded, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return encoded
}

// notionSyncedFromIsCurrent says whether a synced_from given in an update is
// the one the block's read shows.
func notionSyncedFromIsCurrent(
	app core.App,
	authorization notionBlockAuthorization,
	given json.RawMessage,
) bool {
	content := map[string]any{}
	if json.Unmarshal([]byte(authorization.Block.Content), &content) != nil {
		return false
	}
	content = projectNotionSyncedBlockContent(app, authorization, content)
	_, payload := notionBlockPayload(graphBlockTypeSyncedBlock, content, "")
	current, shown := payload["synced_from"]
	if !shown {
		return false
	}
	if current == nil {
		return bytes.Equal(bytes.TrimSpace(given), []byte("null"))
	}
	shownSource, _ := current.(map[string]any)
	shownID, _ := shownSource["block_id"].(string)
	source := map[string]any{}
	if shownID == "" || json.Unmarshal(given, &source) != nil || source == nil {
		return false
	}
	for key, value := range source {
		if key != "block_id" && (key != "type" || value != "block_id") {
			return false
		}
	}
	givenID, _ := source["block_id"].(string)
	return givenID == shownID
}
