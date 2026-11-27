package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
)

// A run again takes the source's values only for what the source changed
// since the last run (notion_migration_rerun.go), by each page's, block's
// and row's last edit time. A tool's next bundle is all new to Leafen, so
// every edit time is the import's; the transport shows instead the time an
// object's content was first seen as it is, so what the tool didn't change
// keeps its time and what people changed in Leafen since stays.

type notionBundleVersion struct {
	ObjectID    string `db:"object_id"`
	ContentHash string `db:"content_hash"`
	Edited      string `db:"edited"`
}

// versions gives each page, block and row in an answer the time its
// content was first seen as it is now.
func (ids notionBundleIDs) versions(value any) error {
	objects := map[string]map[string]any{}
	notionBundleObjects(value, func(object map[string]any) {
		id, _ := object["id"].(string)
		edited, _ := object["last_edited_time"].(string)
		if id != "" && edited != "" && (object["object"] == "page" || object["object"] == "block") {
			objects[id] = object
		}
	})
	if len(objects) == 0 {
		return nil
	}
	keys := make([]string, 0, len(objects))
	hashes := make(map[string]string, len(objects))
	for id, object := range objects {
		keys = append(keys, id)
		hashes[id] = ids.contentHash(object)
	}
	stored := map[string]notionBundleVersion{}
	err := notionBundleIDBatch(keys, func(placeholders string, params dbx.Params) error {
		rows := []notionBundleVersion{}
		params["connection"] = ids.connectionID
		if err := ids.app.DB().NewQuery(`
			SELECT [[object_id]], [[content_hash]], [[edited]] FROM {{leafen_bundle_import_versions}}
			WHERE [[connection_id]] = {:connection} AND [[object_id]] IN (` + placeholders + `)
		`).Bind(params).All(&rows); err != nil {
			return err
		}
		for _, row := range rows {
			stored[row.ObjectID] = row
		}
		return nil
	})
	if err != nil {
		return err
	}
	for id, object := range objects {
		if seen, found := stored[id]; found && seen.ContentHash == hashes[id] {
			object["last_edited_time"] = seen.Edited
			continue
		}
		if _, err := ids.app.DB().NewQuery(`
			INSERT OR REPLACE INTO {{leafen_bundle_import_versions}} ([[connection_id]], [[object_id]], [[content_hash]], [[edited]])
			VALUES ({:connection}, {:object}, {:hash}, {:edited})
		`).Bind(dbx.Params{"connection": ids.connectionID, "object": id, "hash": hashes[id],
			"edited": object["last_edited_time"]}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func notionBundleObjects(value any, visit func(map[string]any)) {
	switch typed := value.(type) {
	case map[string]any:
		visit(typed)
		for _, child := range typed {
			notionBundleObjects(child, visit)
		}
	case []any:
		for _, child := range typed {
			notionBundleObjects(child, visit)
		}
	}
}

// contentHash is an object's content without what changes on its own:
// times, authors, its address and its files' expiring addresses, a file
// standing for its bytes.
func (ids notionBundleIDs) contentHash(object map[string]any) string {
	kept := map[string]any{}
	for key, child := range object {
		switch key {
		case "last_edited_time", "created_time", "last_edited_by", "created_by", "url", "public_url", "request_id":
			continue
		}
		kept[key] = ids.stableContent(child)
	}
	encoded, _ := json.Marshal(kept)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (ids notionBundleIDs) stableContent(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		_, expiring := typed["expiry_time"]
		kept := map[string]any{}
		for key, child := range typed {
			if expiring && key == "expiry_time" {
				continue
			}
			if expiring && key == "url" {
				address, _ := child.(string)
				kept["sha256"] = ids.fileDigest(address)
				continue
			}
			kept[key] = ids.stableContent(child)
		}
		return kept
	case []any:
		kept := make([]any, len(typed))
		for index, child := range typed {
			kept[index] = ids.stableContent(child)
		}
		return kept
	}
	return value
}

// fileDigest is the SHA-256 of the file a Leafen file address hands out.
func (ids notionBundleIDs) fileDigest(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	prefix := apiRoot + strings.Replace(fileTransferDownloadCapabilityPath, "{token}", "", 1)
	if !strings.HasPrefix(parsed.Path, prefix) {
		return ""
	}
	secret, err := fileDownloadCapabilitySecret(ids.app)
	if err != nil {
		return ""
	}
	token, err := url.PathUnescape(strings.TrimPrefix(parsed.Path, prefix))
	if err != nil {
		return ""
	}
	payload, err := openFileDownloadCapabilityToken(secret, token, time.Now().UTC())
	if err != nil {
		return ""
	}
	record, err := ids.app.FindRecordById(filesCollection, payload.FileID)
	if err != nil {
		return ""
	}
	return record.GetString("sha256")
}
