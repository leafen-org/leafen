package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionMappingKey struct {
	SourceID string `json:"sourceId"`
	Kind     string `json:"kind"`
}

type storedNotionMapping struct {
	SourceID string `db:"source_id"`
	NativeID string `db:"native_id"`
	Kind     string `db:"kind"`
}

type pendingNotionMapping struct {
	SourceID string `json:"sourceId"`
	NativeID string `json:"nativeId"`
	Kind     string `json:"kind"`
}

type notionApplyMappings struct {
	nativeIDs map[notionMappingKey]string
	pending   []pendingNotionMapping
}

func loadNotionApplyMappings(
	app core.App,
	jobID string,
	sources []storedNotionSource,
) (*notionApplyMappings, error) {
	return loadNotionApplyMappingsSkipping(app, jobID, sources, nil, nil)
}

// loadNotionApplyMappingsSkipping is loadNotionApplyMappings without the
// dependencies a migration's plan makes later, in the link phase, or the run
// left out (skip; notion_import_left_out.go),
// and those of the sources this pass of the window doesn't make (waits):
// what a merged database holds waits for the second pass, which makes it
// in its target (notion_migration_merge.go).
func loadNotionApplyMappingsSkipping(
	app core.App,
	jobID string,
	sources []storedNotionSource,
	skip func(sourceID string) bool,
	waits func(source storedNotionSource) bool,
) (*notionApplyMappings, error) {
	requested := map[notionMappingKey]struct{}{}
	producers := map[notionMappingKey]int{}
	dependencies := make([][]notionMappingKey, len(sources))
	for index, source := range sources {
		if kind := notionSourceMappingKind(source.Kind); kind != "" {
			key := notionMappingKey{SourceID: source.SourceID, Kind: kind}
			requested[key] = struct{}{}
			producers[key] = index
		}
		if waits != nil && waits(source) {
			continue
		}
		keys, err := notionSourceDependencyMappingKeys(source)
		if err != nil {
			return nil, err
		}
		if skip != nil {
			kept := keys[:0]
			for _, key := range keys {
				if !skip(key.SourceID) {
					kept = append(kept, key)
				}
			}
			keys = kept
		}
		dependencies[index] = keys
		for _, key := range keys {
			requested[key] = struct{}{}
		}
	}
	keys := make([]notionMappingKey, 0, len(requested))
	for key := range requested {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].Kind != keys[right].Kind {
			return keys[left].Kind < keys[right].Kind
		}
		return keys[left].SourceID < keys[right].SourceID
	})
	mappings := &notionApplyMappings{nativeIDs: map[notionMappingKey]string{}}
	if len(keys) == 0 {
		return mappings, nil
	}
	encoded, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	stored := []storedNotionMapping{}
	err = app.DB().NewQuery(`
		SELECT [[mappings]].[[source_id]], [[mappings]].[[native_id]], [[mappings]].[[kind]]
		FROM json_each({:mappingKeys}) AS [[requested]]
		CROSS JOIN {{leafen_notion_import_mappings}} AS [[mappings]]
			ON [[mappings]].[[job_id]] = {:jobId}
			AND [[mappings]].[[source_id]] = json_extract([[requested]].[[value]], '$.sourceId')
			AND [[mappings]].[[kind]] = json_extract([[requested]].[[value]], '$.kind')
		ORDER BY [[mappings]].[[kind]], [[mappings]].[[source_id]]
	`).Bind(dbx.Params{"jobId": jobID, "mappingKeys": string(encoded)}).All(&stored)
	if err != nil {
		return nil, err
	}
	for _, mapping := range stored {
		mappings.nativeIDs[notionMappingKey{SourceID: mapping.SourceID, Kind: mapping.Kind}] = mapping.NativeID
	}
	for index, keys := range dependencies {
		for _, key := range keys {
			if _, found := mappings.nativeIDs[key]; found {
				continue
			}
			producerIndex, produced := producers[key]
			if !produced || producerIndex >= index {
				// It names what it was for and what wasn't made (notion_import_left_out.go).
				return nil, notionImportAt(sources[index].Kind, sources[index].SourceID,
					fmt.Errorf("%w: the %s %s it depends on isn't made", sql.ErrNoRows, key.Kind, key.SourceID))
			}
		}
	}
	return mappings, nil
}

func notionSourceMappingKind(kind string) string {
	switch kind {
	case "page":
		return "page"
	case "paragraph", "template", "file", "pdf", "table", "table_row", "bookmark", "link_preview",
		"block", "image", "video", "audio":
		return "block"
	case "database":
		return "data_source"
	case "property", "row":
		return kind
	default:
		return ""
	}
}

func notionSourceDependencyMappingKeys(source storedNotionSource) ([]notionMappingKey, error) {
	var keys []notionMappingKey
	switch source.Kind {
	case "page":
		if source.ParentKind == "block" || source.ParentKind == "row" {
			keys = append(keys, notionMappingKey{SourceID: source.ParentSourceID, Kind: source.ParentKind})
		} else if source.ParentSourceID != "" {
			keys = append(keys, notionMappingKey{SourceID: source.ParentSourceID, Kind: "page"})
		}
	case "paragraph", "template", "file", "pdf", "table", "table_row", "bookmark", "link_preview",
		"block", "image", "video", "audio", "database":
		if notionImportOwnedFileSource(source.SourceID) {
			// A page, database or row takes this file; it is not a block.
			return nil, nil
		}
		switch source.ParentKind {
		case "page", "row":
			keys = append(keys, notionMappingKey{SourceID: source.ParentSourceID, Kind: source.ParentKind})
		case "block":
			keys = append(keys, notionMappingKey{SourceID: source.ParentSourceID, Kind: "block"})
		case "data_source":
			// A database's other data source joins the database.
			if source.Kind != "database" {
				return nil, errors.New(errorCodeNotionImportCreateFailed)
			}
			keys = append(keys, notionMappingKey{SourceID: source.ParentSourceID, Kind: "data_source"})
		default:
			return nil, errors.New(errorCodeNotionImportCreateFailed)
		}
	case "property", "row":
		keys = append(keys, notionMappingKey{SourceID: source.ParentSourceID, Kind: "data_source"})
	}
	if source.Kind != "row" {
		return keys, nil
	}
	values := map[string]notionDatabaseRowValuePlan{}
	if err := json.Unmarshal([]byte(source.Text), &values); err != nil {
		return nil, err
	}
	propertySources := make([]string, 0, len(values))
	for propertySource, value := range values {
		// Related rows are linked after every source has been applied.
		if !notionImportRowSpecialKey(propertySource) && value.Type != graphValueTypeRelation {
			propertySources = append(propertySources, propertySource)
		}
	}
	sort.Strings(propertySources)
	for _, propertySource := range propertySources {
		keys = append(keys, notionMappingKey{SourceID: propertySource, Kind: "property"})
	}
	return keys, nil
}

func (mappings *notionApplyMappings) nativeID(sourceID, kind string) (string, bool) {
	nativeID, found := mappings.nativeIDs[notionMappingKey{SourceID: sourceID, Kind: kind}]
	return nativeID, found
}

// remember keeps a mapping already stored, for the window's lookups, without
// storing it again.
func (mappings *notionApplyMappings) remember(sourceID, nativeID, kind string) {
	mappings.nativeIDs[notionMappingKey{SourceID: sourceID, Kind: kind}] = nativeID
}

func (mappings *notionApplyMappings) insert(sourceID, nativeID, kind string) error {
	key := notionMappingKey{SourceID: sourceID, Kind: kind}
	if current, found := mappings.nativeIDs[key]; found {
		if current != nativeID {
			return errors.New(errorCodeNotionImportCreateFailed)
		}
		return nil
	}
	mappings.nativeIDs[key] = nativeID
	mappings.pending = append(mappings.pending, pendingNotionMapping{
		SourceID: sourceID, NativeID: nativeID, Kind: kind,
	})
	return nil
}

func (mappings *notionApplyMappings) flush(app core.App, jobID string) error {
	if len(mappings.pending) == 0 {
		return nil
	}
	encoded, err := json.Marshal(mappings.pending)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_mappings}}
			([[job_id]], [[source_id]], [[native_id]], [[kind]], [[created]])
		SELECT {:jobId}, json_extract([[value]], '$.sourceId'),
			json_extract([[value]], '$.nativeId'), json_extract([[value]], '$.kind'), {:created}
		FROM json_each({:mappings})
	`).Bind(dbx.Params{
		"jobId": jobID, "mappings": string(encoded), "created": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}
