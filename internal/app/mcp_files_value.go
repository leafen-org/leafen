package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A files value as the MCP tools give it — notion-fetch's <properties>,
// rows, SQL, a version's rows — and take it back (mcpDatabasePageFiles): an
// uploaded file by its ID, as Notion's MCP names one, and a file linked from
// elsewhere as Notion's API gives one, {"type": "external", "name": …,
// "external": {"url": …}}, which carries its name and address. A linked
// file was left out of the value, so written back as read it went
// (2026-11-26, with a link "spec, v2.pdf" beside an upload, and SQL's null
// for a row holding only the link).

// mcpFilesValue is a stored files value as the tools give it; nil for none.
func mcpFilesValue(property storedNotionDatabaseRowPageProperty) any {
	files := []map[string]any{}
	if !property.Value.Valid || json.Unmarshal([]byte(property.Value.String), &files) != nil || len(files) == 0 {
		return nil
	}
	listed := make([]any, 0, len(files))
	for _, file := range files {
		id, _ := file["fileId"].(string)
		if id == "" {
			id, _ = file["id"].(string)
		}
		if id != "" {
			listed = append(listed, id)
			continue
		}
		if source, _ := file["source"].(string); source == graphExternalFileSource {
			address, _ := file["url"].(string)
			name, _ := file["name"].(string)
			listed = append(listed, mcpExternalFile(name, address))
		}
	}
	if len(listed) == 0 {
		return nil
	}
	return listed
}

// mcpExternalFile is a linked file as Notion's API writes it.
func mcpExternalFile(name, address string) map[string]any {
	return map[string]any{"type": "external", "name": name, "external": map[string]any{"url": address}}
}

// mcpExternalFileWritten is a files value's item naming a linked file, as
// the API's request takes it, which checks its name and address
// (notionDatabaseRowPageRequestFiles); ok is whether the item names one.
func mcpExternalFileWritten(item map[string]any) (map[string]any, bool) {
	external, named := item["external"].(map[string]any)
	if kind, _ := item["type"].(string); !named || kind != "" && kind != "external" {
		return nil, false
	}
	name, _ := item["name"].(string)
	address, _ := external["url"].(string)
	return mcpExternalFile(name, address), true
}

// mcpFilesNamesHeld are the names the row's files value holds its uploaded
// files under, by file: one given when it was written, as Notion's API
// takes one ("notes, v1.txt" beside the upload's own notes_v1.txt), or
// since changed in the app. A file written back by its ID alone, as fetch
// and SQL give it, keeps that name (2026-11-26: each took the upload's own
// name again); one the value doesn't hold yet takes the upload's.
func mcpFilesNamesHeld(app core.App, rowID, propertyID string) (map[string]string, error) {
	held := map[string]string{}
	raw, found, err := rowValueHeld(app, rowID, propertyID)
	if err != nil || !found {
		return held, err
	}
	items, err := parseGraphFileValueItems(json.RawMessage(raw))
	if err != nil {
		return held, nil
	}
	for _, item := range items {
		if item.FileID != "" {
			held[item.FileID] = item.Name
		}
	}
	return held, nil
}

// mcpFilesValueListed is a files value written as Notion's API writes a
// page's files property, {"files": [...]} (with its "type" or "id" beside
// it), or as one file alone, as the list of files the tools take
// (2026-11-26: an agent attaching an upload to a row wrote the API's form
// first and was refused); any other value as it is.
func mcpFilesValueListed(value any) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if files, wrapped := object["files"].([]any); wrapped {
		return files
	}
	if _, upload := object["file_upload"]; upload {
		return []any{object}
	}
	if _, external := object["external"]; external {
		return []any{object}
	}
	return value
}
