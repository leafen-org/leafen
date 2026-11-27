package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/reearth/ygo/crdt"
)

// A block's archived Ygo text keeps its formatting and references. Staging
// keeps the plain text and, when the text is more than one unformatted run,
// its delta; the transform remaps the delta's page, database, custom emoji and
// comment references to the imported objects. A reference to something the
// archive does not carry stays as it was, so a mention still resolves where
// that object exists and otherwise shows as inaccessible.

const workspaceImportArchiveTextDeltaTable = `
	CREATE TABLE IF NOT EXISTS text_deltas (block_id TEXT PRIMARY KEY, delta_json TEXT NOT NULL);
`

type workspaceImportArchiveExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// stageWorkspaceImportArchiveBlockText stages one block's text from its page
// document.
func stageWorkspaceImportArchiveBlockText(db workspaceImportArchiveExecer, document *crdt.Doc, blockID string) error {
	text := document.GetText(ygoBlockTextRootPrefix + blockID)
	plain := text.ToString()
	if plain == "" {
		return nil
	}
	if _, err := db.Exec(`INSERT INTO texts (block_id, value) VALUES (?, ?)`, blockID, plain); err != nil {
		return err
	}
	deltas := text.ToDelta()
	if len(deltas) == 1 && len(deltas[0].Attributes) == 0 {
		if _, isText := deltas[0].Insert.(string); isText {
			return nil
		}
	}
	encoded, err := json.Marshal(deltas)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO text_deltas (block_id, delta_json) VALUES (?, ?)`, blockID, string(encoded))
	return err
}

// workspaceImportArchiveBlockText returns a staged block's text and its
// remapped delta, if it has one.
func workspaceImportArchiveBlockText(db *sql.DB, sourceID string) (string, json.RawMessage, error) {
	var text, encoded string
	err := db.QueryRow(`SELECT value FROM texts WHERE block_id = ?`, sourceID).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, workspaceImportStorageError(err)
	}
	err = db.QueryRow(`SELECT delta_json FROM text_deltas WHERE block_id = ?`, sourceID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return text, nil, nil
	}
	if err != nil {
		return "", nil, workspaceImportStorageError(err)
	}
	deltas := []crdt.Delta{}
	if json.Unmarshal([]byte(encoded), &deltas) != nil || len(deltas) == 0 {
		return "", nil, workspaceImportInputError()
	}
	for index := range deltas {
		if deltas[index].Op != crdt.DeltaOpInsert {
			return "", nil, workspaceImportInputError()
		}
		for attribute := range workspaceImportArchiveRichTextKinds {
			sourceID, _ := deltas[index].Attributes[attribute].(string)
			if sourceID == "" {
				continue
			}
			targetID, err := workspaceImportArchiveMentionReference(db, attribute, sourceID)
			if err != nil {
				return "", nil, err
			}
			deltas[index].Attributes[attribute] = targetID
		}
		if deltas[index].Attributes != nil {
			if err := workspaceImportArchiveMention(db, deltas[index].Attributes); err != nil {
				return "", nil, err
			}
		}
	}
	remappedJSON, err := json.Marshal(deltas)
	if err != nil {
		return "", nil, workspaceImportInputError()
	}
	return text, remappedJSON, nil
}

// workspaceImportArchiveTextDeltas decodes the deltas of the staged items
// that carry one, by imported block ID.
func workspaceImportArchiveTextDeltas(items []workspaceImportStagedItem) (map[string][]crdt.Delta, error) {
	result := map[string][]crdt.Delta{}
	for _, item := range items {
		if len(item.TextDelta) == 0 || item.Operation == nil || item.Operation.Block == nil {
			continue
		}
		deltas := []crdt.Delta{}
		if json.Unmarshal(item.TextDelta, &deltas) != nil || len(deltas) == 0 {
			return nil, workspaceImportInputError()
		}
		for _, delta := range deltas {
			if delta.Op != crdt.DeltaOpInsert {
				return nil, workspaceImportInputError()
			}
		}
		result[item.Operation.Block.ID] = deltas
	}
	return result, nil
}

// A bundle another tool made can't carry Leafen's page documents, so a
// block may carry its text itself: runs of text, each with the formatting
// Leafen's editor keeps (docs/bundle.md). It is staged as text from a page
// document is.
var workspaceImportArchiveTextAttributes = map[string]bool{
	"bold": true, "italic": true, "underline": true, "strikethrough": true, "code": true, "color": true,
	"link": true, "equation": true, "mention": true, "pageId": true, "databaseId": true, "userId": true,
	"date": true, "customEmojiId": true, "commentId": true,
}

// workspaceImportArchiveAuthoredText reads a block's "text": its plain
// text and its runs, whether it has one, and whether it is well formed.
func workspaceImportArchiveAuthoredText(kind string, value map[string]any) (string, []crdt.Delta, bool, bool) {
	raw, exists := value["text"]
	if !exists {
		return "", nil, false, true
	}
	runs, isList := raw.([]any)
	// A page's block, or a database page's: another tool gives either its text so.
	if (kind != workspaceExportItemBlock && kind != workspaceExportItemRowPageBlock) || !isList || len(runs) == 0 {
		return "", nil, true, false
	}
	plain := strings.Builder{}
	deltas := make([]crdt.Delta, 0, len(runs))
	for _, entry := range runs {
		run, isRun := entry.(map[string]any)
		insert, isText := run["insert"].(string)
		if !isRun || !isText || insert == "" || len(run) > 2 {
			return "", nil, true, false
		}
		delta := crdt.Delta{Op: crdt.DeltaOpInsert, Insert: insert}
		if rawAttributes, has := run["attributes"]; has {
			attributes, isMap := rawAttributes.(map[string]any)
			if !isMap {
				return "", nil, true, false
			}
			for name, attribute := range attributes {
				switch attribute.(type) {
				case string, bool, float64:
				case map[string]any:
					if name != "date" {
						return "", nil, true, false
					}
				default:
					return "", nil, true, false
				}
				if !workspaceImportArchiveTextAttributes[name] {
					return "", nil, true, false
				}
			}
			if len(attributes) > 0 {
				delta.Attributes = crdt.Attributes(attributes)
			}
		} else if len(run) > 1 {
			return "", nil, true, false
		}
		plain.WriteString(insert)
		deltas = append(deltas, delta)
	}
	if plain.Len() > workspaceImportArchiveEntityJSONBytes {
		return "", nil, true, false
	}
	return plain.String(), deltas, true, true
}

// stageWorkspaceImportArchiveAuthoredText stages a block's own text.
func stageWorkspaceImportArchiveAuthoredText(db workspaceImportArchiveExecer, blockID, plain string, deltas []crdt.Delta) error {
	if _, err := db.Exec(`INSERT INTO texts (block_id, value) VALUES (?, ?)`, blockID, plain); err != nil {
		return err
	}
	if len(deltas) == 1 && len(deltas[0].Attributes) == 0 {
		return nil
	}
	encoded, err := json.Marshal(deltas)
	if err != nil {
		return err
	}
	if _, err := db.Exec(workspaceImportArchiveTextDeltaTable); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO text_deltas (block_id, delta_json) VALUES (?, ?)`, blockID, string(encoded))
	return err
}
