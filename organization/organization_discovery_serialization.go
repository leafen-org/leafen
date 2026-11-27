package organization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedOrganizationDiscoveryChunk struct {
	Index     int    `db:"chunk_index"`
	ItemCount int    `db:"item_count"`
	DataJSON  string `db:"data_json"`
	SHA256    string `db:"sha256"`
}

// json and jsonl artifacts share the same immutable UTF-8 chunk stream.
func serializeOrganizationDiscoveryFragment(
	items []discoveryItem, format string, existingItems int,
) (string, error) {
	if len(items) == 0 {
		return "", nil
	}
	rows := make([]string, 0, len(items))
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			return "", errOrganizationDiscoveryUnavailable
		}
		rows = append(rows, string(encoded))
	}
	switch format {
	case leafen.OrganizationDiscoveryExportFormatJSON:
		prefix := ","
		if existingItems == 0 {
			prefix = "["
		}
		return prefix + strings.Join(rows, ","), nil
	case leafen.OrganizationDiscoveryExportFormatJSONL:
		prefix := "\n"
		if existingItems == 0 {
			prefix = ""
		}
		return prefix + strings.Join(rows, "\n"), nil
	default:
		return "", errOrganizationDiscoveryUnavailable
	}
}

func organizationDiscoverySuffix(format string, itemCount int) (string, error) {
	switch format {
	case leafen.OrganizationDiscoveryExportFormatJSON:
		if itemCount == 0 {
			return "[]", nil
		}
		return "]", nil
	case leafen.OrganizationDiscoveryExportFormatJSONL:
		return "", nil
	default:
		return "", errOrganizationDiscoveryUnavailable
	}
}

func splitOrganizationDiscoveryUTF8(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	if !utf8.ValidString(value) {
		return nil, errOrganizationDiscoveryUnavailable
	}
	chunks := make([]string, 0, len(value)/leafen.OrganizationDiscoveryChunkMaxBytes+1)
	for start := 0; start < len(value); {
		end := min(start+leafen.OrganizationDiscoveryChunkMaxBytes, len(value))
		for end < len(value) && end > start && !utf8.RuneStart(value[end]) {
			end--
		}
		if end <= start {
			return nil, errOrganizationDiscoveryUnavailable
		}
		chunks = append(chunks, value[start:end])
		start = end
	}
	return chunks, nil
}

func insertOrganizationDiscoveryChunks(
	app core.App, exportID string, afterIndex int, fragment string, itemCount int, createdAt int64,
) (int, error) {
	parts, err := splitOrganizationDiscoveryUTF8(fragment)
	if err != nil {
		return 0, err
	}
	for offset, part := range parts {
		encoded, err := json.Marshal(part)
		if err != nil || len(part) > leafen.OrganizationDiscoveryChunkMaxBytes {
			return 0, errOrganizationDiscoveryUnavailable
		}
		count := 0
		if offset == 0 {
			count = itemCount
		}
		digest := sha256.Sum256([]byte(part))
		_, err = app.DB().NewQuery(`
			INSERT INTO {{leafen_organization_discovery_export_chunks}}
				([[export_id]],[[chunk_index]],[[item_count]],[[data_json]],[[sha256]],[[created_at]])
			VALUES ({:exportId},{:chunkIndex},{:itemCount},{:data},{:sha256},{:createdAt})
		`).Bind(dbx.Params{
			"exportId": exportID, "chunkIndex": afterIndex + offset + 1, "itemCount": count,
			"data": string(encoded), "sha256": hex.EncodeToString(digest[:]), "createdAt": createdAt,
		}).Execute()
		if err != nil {
			return 0, err
		}
	}
	return len(parts), nil
}

func streamOrganizationDiscoveryArtifact(
	writer io.Writer, app core.App, row storedOrganizationDiscoveryExport,
) error {
	hasher := sha256.New()
	output := io.MultiWriter(writer, hasher)
	written := int64(0)
	itemCount := 0
	expectedIndex := 1
	afterIndex := 0
	for {
		chunks := []storedOrganizationDiscoveryChunk{}
		err := app.DB().NewQuery(`
			SELECT [[chunk_index]],[[item_count]],[[data_json]],[[sha256]]
			FROM {{leafen_organization_discovery_export_chunks}}
			WHERE [[export_id]]={:exportId} AND [[chunk_index]]>{:afterIndex}
			ORDER BY [[chunk_index]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"exportId": row.ID, "afterIndex": afterIndex,
			"limit": leafen.OrganizationDiscoveryDownloadChunkWindow,
		}).All(&chunks)
		if err != nil {
			return err
		}
		if len(chunks) == 0 {
			break
		}
		for _, chunk := range chunks {
			fragment := ""
			if chunk.Index != expectedIndex || chunk.ItemCount < 0 ||
				json.Unmarshal([]byte(chunk.DataJSON), &fragment) != nil ||
				len(fragment) > leafen.OrganizationDiscoveryChunkMaxBytes {
				return errOrganizationDiscoveryUnavailable
			}
			digest := sha256.Sum256([]byte(fragment))
			if chunk.SHA256 != hex.EncodeToString(digest[:]) {
				return errOrganizationDiscoveryUnavailable
			}
			count, err := io.WriteString(output, fragment)
			if err != nil {
				return err
			}
			written += int64(count)
			itemCount += chunk.ItemCount
			afterIndex, expectedIndex = chunk.Index, expectedIndex+1
		}
	}
	if expectedIndex-1 != row.ChunkCount || itemCount != row.ItemCount ||
		written != row.ByteCount || hex.EncodeToString(hasher.Sum(nil)) != row.ContentSHA256 {
		return errOrganizationDiscoveryUnavailable
	}
	return nil
}
