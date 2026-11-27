package app

import (
	"encoding/json"
	"strings"
)

// A data source request deletes an option on purpose by sending it, by its
// id or name, with "delete": true — Leafen's own, Notion's API having no
// such word: there an option left out goes, and the pages holding it lose
// it (measured 2026-11-26). Leafen refuses a change that leaves out an
// option pages hold (database_option_removal.go); one sent so is deleted,
// cleared from its pages as Notion clears it.

// takeNotionOptionDeletes takes the options a request's options send with
// "delete": true out of it (object's "options"), each found among the
// property's options (current) by its id or name, and gives them as the
// marks the change's check reads (databaseOptionDeleteKey).
func takeNotionOptionDeletes(propertyName string, object map[string]json.RawMessage, current map[string]any) ([]map[string]any, error) {
	encoded, present := object["options"]
	if !present {
		return nil, nil
	}
	items := []map[string]json.RawMessage{}
	if json.Unmarshal(encoded, &items) != nil {
		return nil, nil
	}
	existing, _ := current["options"].([]any)
	marks, kept := []map[string]any{}, make([]map[string]json.RawMessage, 0, len(items))
	for _, item := range items {
		deleted := false
		if json.Unmarshal(item[databaseOptionDeleteKey], &deleted) != nil || !deleted {
			delete(item, databaseOptionDeleteKey)
			kept = append(kept, item)
			continue
		}
		id, name := "", ""
		_ = json.Unmarshal(item["id"], &id)
		_ = json.Unmarshal(item["name"], &name)
		id, name = strings.TrimSpace(id), strings.TrimSpace(name)
		found := ""
		for _, raw := range existing {
			option, _ := raw.(map[string]any)
			heldID, _ := option["id"].(string)
			heldName, _ := option["name"].(string)
			if id != "" && heldID == id || id == "" && name != "" && strings.EqualFold(strings.TrimSpace(heldName), name) {
				found = heldID
				break
			}
		}
		if found == "" {
			return nil, notionDatabaseRowPageRequestInvalid(
				"database property %q has no option %s to delete", propertyName, strconvQuoteJS(id+name))
		}
		marks = append(marks, map[string]any{"id": found, databaseOptionDeleteKey: true})
	}
	rest, err := json.Marshal(kept)
	if err != nil {
		return nil, err
	}
	object["options"] = rest
	return marks, nil
}

// withNotionOptionDeletes adds the deleted options' marks to the options a
// request gives.
func withNotionOptionDeletes(options []map[string]any, marks []map[string]any) []map[string]any {
	return append(options, marks...)
}
