package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/pocketbase/core"
)

type pageDuplicationDatabaseReferenceSet map[string]map[string]struct{}

// remapPageDuplicationDatabaseCells names cells by the copy's rows and
// properties; a property the copy leaves out, as one into another workspace
// does (dropLeftOut), leaves its cells out.
func remapPageDuplicationDatabaseCells(
	app core.App,
	jobID string,
	cells []pageDuplicationSourceDatabaseCell,
	dropLeftOut bool,
) (map[string]map[string]graphRowValue, error) {
	raw := make([]json.RawMessage, len(cells))
	spans := make([][]graphTextSpan, len(cells))
	relationIDs := map[string]struct{}{}
	propertyIDs := map[string]struct{}{}
	for index, cell := range cells {
		raw[index] = json.RawMessage(cell.Value)
		spans[index] = databaseStoredSpans(cell.Spans)
		propertyIDs[cell.PropertyID] = struct{}{}
		if cell.Type == graphValueTypeRelation {
			for _, rowID := range graphRelationRowIDs(raw[index]) {
				relationIDs[rowID] = struct{}{}
			}
		}
	}
	remapped, err := remapPageDuplicationDatabaseJSON(app, jobID, raw)
	if err != nil {
		return nil, err
	}
	if err := remapPageDuplicationDatabaseFiles(app, jobID, cells, remapped); err != nil {
		return nil, err
	}
	// A cell's formatting and mentions go with its text, as a copied row's do
	// (planDatabaseRowDuplicate): left out, a copy's cells were plain text.
	if err := remapPageDuplicationSpans(app, jobID, spans); err != nil {
		return nil, err
	}
	properties, err := pageDuplicationReferenceMappings(app, jobID, "property", referenceIDs(propertyIDs))
	if err != nil {
		return nil, err
	}
	rows, err := pageDuplicationReferenceMappings(app, jobID, "row", referenceIDs(relationIDs))
	if err != nil {
		return nil, err
	}
	trashed, err := trashedDatabaseRowIDs(app, referenceIDs(relationIDs))
	if err != nil {
		return nil, err
	}
	result := map[string]map[string]graphRowValue{}
	for index, cell := range cells {
		propertyID, found := properties[cell.PropertyID]
		if !found && dropLeftOut {
			continue
		}
		if !found {
			return nil, errPageDuplicationConflict
		}
		value := remapped[index]
		if cell.Type == graphValueTypeRelation {
			value, err = remapPageDuplicationRelationValue(value, rows, trashed)
			if err != nil {
				return nil, err
			}
		}
		if result[cell.RowID] == nil {
			result[cell.RowID] = map[string]graphRowValue{}
		}
		result[cell.RowID][propertyID] = graphRowValue{Type: cell.Type, Value: value, Spans: spans[index]}
	}
	return result, nil
}

func remapPageDuplicationDatabaseFiles(
	app core.App,
	jobID string,
	cells []pageDuplicationSourceDatabaseCell,
	values []json.RawMessage,
) error {
	items := make([]graphFilesValue, len(cells))
	fileIDs := map[string]struct{}{}
	for index, cell := range cells {
		if cell.Type != graphValueTypeFiles {
			continue
		}
		parsed, err := parseGraphFileValueItems(values[index])
		if err != nil {
			return err
		}
		items[index] = parsed
		for _, item := range parsed {
			if item.FileID != "" {
				fileIDs[item.FileID] = struct{}{}
			}
		}
	}
	mappings, err := pageDuplicationReferenceMappings(
		app, jobID, "file", referenceIDs(fileIDs),
	)
	if err != nil {
		return err
	}
	for index := range items {
		if items[index] == nil {
			continue
		}
		for itemIndex := range items[index] {
			if items[index][itemIndex].FileID == "" {
				continue
			}
			targetID := mappings[items[index][itemIndex].FileID]
			if targetID == "" {
				return errPageDuplicationConflict
			}
			items[index][itemIndex].FileID = targetID
		}
		values[index], err = json.Marshal(items[index])
		if err != nil {
			return err
		}
	}
	return nil
}

func remapPageDuplicationRelationValue(
	raw json.RawMessage,
	mappings map[string]string,
	trashed map[string]struct{},
) (json.RawMessage, error) {
	values := []string{}
	if json.Unmarshal(raw, &values) != nil {
		return nil, errPageDuplicationConflict
	}
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if target, found := mappings[value]; found {
			kept = append(kept, target)
		} else if _, inTrash := trashed[value]; !inTrash {
			// A copy relates to the rows it copied; a trashed row stays out.
			kept = append(kept, value)
		}
	}
	return json.Marshal(kept)
}

// pageDuplicationRelationsOneWayOutside makes a copied two-way relation whose
// other side stays outside the copy relate one way, keeping its pages, as
// Notion's copy does (seen 2026-11-14: a page's copy, its database's own and
// the API's duplicate each leave the copy's relation "양방향 관계형 꺼짐" and
// the other database as it was). Naming that side, the copy claimed a side
// that names the original only (database_relation_half_pairs.go).
func pageDuplicationRelationsOneWayOutside(
	app core.App,
	jobID string,
	types []string,
	configs []json.RawMessage,
) error {
	pairs := make([]string, len(configs))
	ids := map[string]struct{}{}
	for index, raw := range configs {
		config := map[string]any{}
		if types[index] != graphValueTypeRelation || json.Unmarshal(raw, &config) != nil {
			continue
		}
		if pair, _ := config["relatedPropertyId"].(string); pair != "" {
			pairs[index] = pair
			ids[pair] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	inside, err := pageDuplicationReferenceMappings(app, jobID, "property", referenceIDs(ids))
	if err != nil {
		return err
	}
	for index, pair := range pairs {
		if _, copied := inside[pair]; pair == "" || copied {
			continue
		}
		config := map[string]any{}
		if err := json.Unmarshal(configs[index], &config); err != nil {
			return err
		}
		delete(config, "relatedPropertyId")
		delete(config, "reciprocalName")
		if configs[index], err = json.Marshal(config); err != nil {
			return err
		}
	}
	return nil
}

func newPageDuplicationDatabaseReferenceSet() pageDuplicationDatabaseReferenceSet {
	return pageDuplicationDatabaseReferenceSet{
		"page": {}, "block": {}, "data_source": {}, "view": {}, "property": {}, "row": {},
	}
}

func collectPageDuplicationDatabaseReferences(
	field string,
	value any,
	references pageDuplicationDatabaseReferenceSet,
) {
	switch typed := value.(type) {
	case string:
		if kind := pageDuplicationDatabaseReferenceKind(field); kind != "" && typed != "" {
			references[kind][typed] = struct{}{}
		} else if databaseViewPropertyFields[field] && typed != "" {
			references["property"][typed] = struct{}{}
		}
	case []any:
		for _, child := range typed {
			collectPageDuplicationDatabaseReferences(field, child, references)
		}
	case map[string]any:
		for _, name := range databaseViewSettingProperties(field, typed) {
			references["property"][name] = struct{}{}
		}
		for key, child := range typed {
			collectPageDuplicationDatabaseReferences(key, child, references)
		}
	}
}

func loadPageDuplicationDatabaseReferenceSet(
	app core.App,
	jobID string,
	references pageDuplicationDatabaseReferenceSet,
) (map[string]map[string]string, error) {
	kinds := make([]string, 0, len(references))
	for kind := range references {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	result := map[string]map[string]string{}
	for _, kind := range kinds {
		mapping, err := pageDuplicationReferenceMappings(
			app, jobID, kind, referenceIDs(references[kind]),
		)
		if err != nil {
			return nil, err
		}
		result[kind] = mapping
	}
	return result, nil
}

func rewritePageDuplicationDatabaseReferences(
	field string,
	value any,
	mappings map[string]map[string]string,
) {
	kindOf := func(field string) string {
		if kind := pageDuplicationDatabaseReferenceKind(field); kind != "" {
			return kind
		}
		if databaseViewPropertyFields[field] {
			return "property"
		}
		return ""
	}
	switch typed := value.(type) {
	case []any:
		for index, child := range typed {
			if source, ok := child.(string); ok {
				if target, found := mappings[kindOf(field)][source]; found {
					typed[index] = target
				}
			}
			rewritePageDuplicationDatabaseReferences(field, typed[index], mappings)
		}
	case map[string]any:
		renameDatabaseViewSettingProperties(field, typed, func(source string) (string, bool) {
			target, found := mappings["property"][source]
			return target, found
		})
		for key, child := range typed {
			if source, ok := child.(string); ok {
				if target, found := mappings[kindOf(key)][source]; found {
					typed[key] = target
				}
			}
			rewritePageDuplicationDatabaseReferences(key, typed[key], mappings)
		}
	}
}

// pageDuplicationDatabaseReferenceKind is what a database setting names by
// its field: a copy and an archive's import name the copied ones by it. A
// rollup names the relation it goes through and the property it reads there
// by either pair of fields (databaseComputedConfig.rollupFields).
func pageDuplicationDatabaseReferenceKind(field string) string {
	switch field {
	case "pageId":
		return "page"
	case "sourceContainerId", "sourceBlockId":
		return "block"
	case "dataSourceId", "targetDataSourceId", "relationDataSourceId", "relationDatabaseId", "databaseId":
		return "data_source"
	case "viewId", "databaseViewId", "databaseViewIds", "dashboardViewId", "dashboardRows":
		return "view"
	case "propertyId", "groupBy", "calendarBy", "startDatePropertyId", "endDatePropertyId",
		"relationPropertyId", "rollupPropertyId", "targetPropertyId", "titlePropertyId",
		"rollupRelationPropertyId", "rollupTargetPropertyId",
		"predecessorPropertyId", "successorPropertyId", "relatedPropertyId", databaseRelationFormerPairField,
		"parentPropertyId", "subitemsPropertyId", "datePropertyId",
		"statusPropertyId", "assigneePropertyId", "dueDatePropertyId",
		"pinned":
		return "property"
	// A view's own order of its rows (`rowOrder`, viewRowOrder.ts) names the
	// copies, so a copied view shows them as its source showed its rows, as
	// Notion's copy does (seen 2026-11-14); a row the copy leaves out (in
	// the trash) or a linked view's row, not copied, keeps its ID.
	case "rowId", "rowOrder":
		return "row"
	default:
		return ""
	}
}
