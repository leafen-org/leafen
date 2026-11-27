package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const maxNotionDatabaseRowPageFiles = 100

func notionDatabaseRowPageFiles(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	stored storedNotionDatabaseRowPageProperty,
	issuer *notionBlockFileCapabilityIssuer,
) ([]map[string]any, error) {
	if !stored.Value.Valid {
		return []map[string]any{}, nil
	}
	items, err := parseGraphFileValueItems(json.RawMessage(stored.Value.String))
	if err != nil || len(items) > maxNotionDatabaseRowPageFiles {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item.Source == graphExternalFileSource {
			result = append(result, map[string]any{
				"name": item.Name, "type": "external",
				"external": map[string]any{"url": item.URL},
			})
			continue
		}
		if issuer == nil {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		payload, err := issuer.payload(
			app, authorization.Identity.WorkspaceID, graphBlockTypeFile, raw,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"name": item.Name, "type": "file", "file": payload["file"],
		})
	}
	return result, nil
}

func notionDatabaseRowPageRequestFiles(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	property storedNotionDatabaseRowPageProperty,
	raw json.RawMessage,
) (graphFilesValue, error) {
	items := []struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		FileUpload *struct {
			ID string `json:"id"`
		} `json:"file_upload"`
		External *struct {
			URL string `json:"url"`
		} `json:"external"`
		File json.RawMessage `json:"file"`
	}{}
	if decodeNotionDatabaseRowPageStructured(raw, &items) != nil ||
		len(items) > maxNotionDatabaseRowPageFiles {
		return nil, notionDatabaseRowPageRequestInvalid(
			"database property %q files are invalid", property.Name,
		)
	}
	result := make(graphFilesValue, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		// A file written without its type is the kind its key names, as
		// Notion reads one (seen 2026-11-10).
		if item.Type == "" {
			switch {
			case item.External != nil:
				item.Type = "external"
			case item.FileUpload != nil:
				item.Type = "file_upload"
			case len(item.File) != 0:
				item.Type = "file"
			}
		}
		// An upload's name may be left out, its file's then (seen 2026-11-10);
		// a linked file's is kept as given, spaces too (seen 2026-11-11).
		name := strings.TrimSpace(item.Name)
		if item.Type == "external" {
			name = item.Name
		}
		if name == "" && item.Type != "file_upload" || len([]rune(name)) > maxGraphExternalFileNameRunes {
			return nil, notionDatabaseRowPageRequestInvalid(
				"database property %q file name is invalid", property.Name,
			)
		}
		// Its address is any text, kept as written, as Notion's API keeps one
		// ("a b.com", "mailto:…", "   ", seen 2026-11-11).
		if item.Type == "external" {
			if item.External == nil || item.FileUpload != nil || len(item.File) != 0 ||
				!validGraphStoredFileAddress(item.External.URL) || notionHostedFileURL(item.External.URL) {
				return nil, notionDatabaseRowPageRequestInvalid(
					"database property %q external file is invalid", property.Name,
				)
			}
			result = append(result, graphFileValueItem{
				Source: graphExternalFileSource, URL: item.External.URL, Name: name,
			})
			continue
		}
		var canonical graphFileBlockContent
		switch {
		case item.Type == "file_upload" && item.FileUpload != nil && item.External == nil && len(item.File) == 0:
			var err error
			// One missing or unsent is said as a block's (seen 2026-11-10).
			canonical, err = resolveNotionCompletedFileUpload(
				app, authorization.Identity, strings.TrimSpace(item.FileUpload.ID),
			)
			if err != nil {
				return nil, err
			}
			// It takes the name given, and is held once, where it is first
			// named, as Notion's API holds it (seen 2026-11-10).
			if _, duplicate := seen[canonical.FileID]; duplicate {
				continue
			}
			if name != "" {
				canonical.Name = name
			}
		case item.Type == "file" && len(item.File) != 0 && item.FileUpload == nil && item.External == nil:
			// A Notion-hosted file copied from a response stays one of the files.
			var hosted any
			request, err := notionHostedFileRequest{}, json.Unmarshal(item.File, &hosted)
			if err == nil {
				request, err = parseNotionHostedFileRequest(hosted)
			}
			if err == nil {
				canonical, err = resolveNotionHostedFile(app, authorization.Identity, request)
			}
			if err != nil {
				return nil, notionDatabaseRowPageRequestInvalid(
					"database property %q contains an unavailable hosted file", property.Name,
				)
			}
			// It takes the name given, as Notion's API takes one: a file's
			// name in the value is its own, apart from the name the upload
			// keeps ("notes, v1.txt" beside an upload kept as notes_v1.txt),
			// so the value written back as read keeps it, and one written
			// under another name is renamed (measured 2026-11-26).
			canonical.Name = name
		default:
			return nil, notionDatabaseRowPageRequestInvalid(
				"database property %q accepts file_upload, file or external values", property.Name,
			)
		}
		if _, duplicate := seen[canonical.FileID]; duplicate {
			return nil, notionDatabaseRowPageRequestInvalid(
				"database property %q contains a duplicate file", property.Name,
			)
		}
		seen[canonical.FileID] = struct{}{}
		result = append(result, graphFileValueItemFromManaged(canonical))
	}
	return result, nil
}
